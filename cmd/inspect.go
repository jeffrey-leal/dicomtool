package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/tag"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect input:<file> [input:<file>...] [tag:<tag>...] [all:true] [verbose:true]",
	Short: "Print DICOM metadata from one or more files",
	Long: `Parse one or more DICOM files and print their tag values to stdout.

Parameters:
  input:<file>   Path to a DICOM file to inspect. Repeatable. Bare path
                 arguments (without the input: prefix) are also accepted.
  tag:<tag>      Tag to display, as GGGG,EEEE or a defined alias. Repeatable.
  all:true       Display every tag present in the file.
  verbose:true   Print additional diagnostic information.

Either all:true or at least one tag: parameter is required.

Output format:
  (GGGG,EEEE)  VR  Tag Name                              = value

Sequence (SQ) elements are expanded inline, indented four spaces per level.
Binary fields longer than 16 bytes are shown as <binary, N bytes>.
Pixel data is always reported as a summary line.

Examples:
  dicomtool inspect scan.dcm all:true
  dicomtool inspect input:scan.dcm tag:0010,0010 tag:0010,0020
  dicomtool inspect input:scan.dcm tag:PatientName tag:PatientID
  dicomtool inspect input:a.dcm input:b.dcm all:true`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInspect()
	},
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}

func runInspect() error {
	showAll := boolParam("all", false)
	rawTags := param("tag")
	tags := make([]string, len(rawTags))
	for i, t := range rawTags {
		tags[i] = Opts.TagAliases.Resolve(t)
	}

	if len(Opts.Inputs) == 0 {
		return errors.New("at least one input:<file> is required")
	}
	if !showAll && len(tags) == 0 {
		return errors.New("specify all:true to dump every tag, or one or more tag:<value> pairs")
	}

	for _, path := range Opts.Inputs {
		fmt.Printf("File: %s\n", path)
		if err := inspectFile(path, showAll, tags); err != nil {
			fmt.Printf("  error: %v\n", err)
		}
	}
	return nil
}

// inspectFile parses a DICOM file and prints the requested elements.
func inspectFile(path string, showAll bool, tags []string) error {
	// Skip pixel data when dumping all tags — pixel data is reported as a
	// summary line to avoid allocating large buffers for header inspection.
	var parseOpts []dicom.ParseOption
	if showAll {
		parseOpts = append(parseOpts, dicom.SkipPixelData())
	}

	ds, err := dicom.ParseFile(path, nil, parseOpts...)
	if err != nil {
		return err
	}

	if showAll {
		for _, elem := range ds.Elements {
			printElement(elem, "  ")
		}
		return nil
	}

	for _, tagStr := range tags {
		t, err := parseTagString(tagStr)
		if err != nil {
			fmt.Printf("  invalid tag %q: %v\n", tagStr, err)
			continue
		}
		elem, err := ds.FindElementByTag(t)
		if err != nil {
			fmt.Printf("  (%04X,%04X)  not found\n", t.Group, t.Element)
			continue
		}
		printElement(elem, "  ")
	}
	return nil
}

// printElement formats and prints a single DICOM element. Sequence (SQ) elements
// are expanded recursively with each nesting level indented by four spaces.
func printElement(elem *dicom.Element, indent string) {
	tagStr := fmt.Sprintf("(%04X,%04X)", elem.Tag.Group, elem.Tag.Element)

	name := ""
	if info, err := tag.Find(elem.Tag); err == nil {
		name = info.Keyword
	}

	vr := elem.RawValueRepresentation
	if vr == "" {
		vr = "--"
	}

	// Sequences are expanded inline rather than summarised.
	if elem.Value != nil && elem.Value.ValueType() == dicom.Sequences {
		items, ok := elem.Value.GetValue().([]*dicom.SequenceItemValue)
		if !ok {
			fmt.Printf("%s%s  %-2s  %-40s [sequence]\n", indent, tagStr, vr, name)
			return
		}
		fmt.Printf("%s%s  %-2s  %-40s [sequence: %d item(s)]\n", indent, tagStr, vr, name, len(items))
		childIndent := indent + "    "
		for i, item := range items {
			elems, ok := item.GetValue().([]*dicom.Element)
			if !ok {
				continue
			}
			fmt.Printf("%s  Item %d:\n", indent, i+1)
			for _, nested := range elems {
				printElement(nested, childIndent)
			}
		}
		return
	}

	value := formatElementValue(elem)
	fmt.Printf("%s%s  %-2s  %-40s = %s\n", indent, tagStr, vr, name, value)
}

// formatElementValue returns a human-readable string for an element's value.
func formatElementValue(elem *dicom.Element) string {
	if elem.Value == nil {
		return "<empty>"
	}

	switch elem.Value.ValueType() {
	case dicom.Strings:
		vals, ok := elem.Value.GetValue().([]string)
		if !ok || len(vals) == 0 {
			return ""
		}
		return strings.Join(vals, `\`)

	case dicom.Ints:
		vals, ok := elem.Value.GetValue().([]int)
		if !ok || len(vals) == 0 {
			return ""
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = strconv.Itoa(v)
		}
		return strings.Join(parts, `\`)

	case dicom.Floats:
		vals, ok := elem.Value.GetValue().([]float64)
		if !ok || len(vals) == 0 {
			return ""
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = strconv.FormatFloat(v, 'g', -1, 64)
		}
		return strings.Join(parts, `\`)

	case dicom.Bytes:
		b, ok := elem.Value.GetValue().([]byte)
		if !ok {
			return "<bytes>"
		}
		if len(b) == 0 {
			return ""
		}
		const maxDisplay = 16
		if len(b) <= maxDisplay {
			return fmt.Sprintf("% X", b)
		}
		return fmt.Sprintf("<binary, %d bytes>", len(b))

	case dicom.PixelData:
		info := dicom.MustGetPixelDataInfo(elem.Value)
		if info.IntentionallySkipped {
			return "[pixel data: skipped]"
		}
		if info.IsEncapsulated {
			return fmt.Sprintf("[pixel data: %d frame(s), compressed]", len(info.Frames))
		}
		if len(info.Frames) > 0 {
			f := info.Frames[0]
			if !f.Encapsulated && f.NativeData != nil {
				nd := f.NativeData
				return fmt.Sprintf("[pixel data: %d frame(s), %dx%d, %d bpp]",
					len(info.Frames), nd.Rows(), nd.Cols(), nd.BitsPerSample())
			}
		}
		return fmt.Sprintf("[pixel data: %d frame(s)]", len(info.Frames))

	case dicom.Sequences:
		items, ok := elem.Value.GetValue().([]*dicom.SequenceItemValue)
		if !ok {
			return "[sequence]"
		}
		return fmt.Sprintf("[sequence: %d item(s)]", len(items))

	case dicom.SequenceItem:
		items, ok := elem.Value.GetValue().([]*dicom.Element)
		if !ok {
			return "[sequence item]"
		}
		return fmt.Sprintf("[sequence item: %d element(s)]", len(items))

	default:
		return elem.Value.String()
	}
}
