package cmd

import (
	"archive/zip"
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/tag"
)

var modifyCmd = &cobra.Command{
	Use:   "modify input:<dir> output:<dir> [options...]",
	Short: "Overwrite specific DICOM tags in all files under a directory tree",
	Long: `Read every DICOM file under an input directory tree, apply the specified
modifications, and write results to an output directory preserving the
original folder structure.

Parameters:
  input:<dir>               Source directory (required).
  output:<dir>              Destination directory (required). Created if absent.
                            A relative path is resolved against the input directory.
  set:<tag>=<value>         Set a tag to a value. Repeatable.
  remove:<tag>              Remove a tag entirely. Repeatable.
  dob:<mask>                Mask the Patient Birth Date (0010,0030). The mask
                            must be 8 chars (YYYYMMDD); digit characters replace
                            the corresponding position, others preserve it.
  uid:<suffix>              Append .<suffix> to every UID field. Suffix must be
                            digits only [1-9]. Transfer Syntax UIDs are excluded.
  noprivate:true            Remove all private (odd-group) tags.
  ignoretype:<values>       Skip files whose Image Type (0008,0008) contains any
                            of the comma-delimited values. Case-insensitive.
  ignoremodality:<values>   Skip files whose Modality (0008,0060) matches any of
                            the comma-delimited values. Case-insensitive.
  maskrows:<n>              Zero the first n pixel rows of each image frame.
                            Native (uncompressed) pixel data only.
  fixvr:correct|skip|passthrough
                            Handle tags with a non-standard Value Representation.
                            correct  - re-encode under the standard VR (default).
                            skip     - remove the offending tag.
                            passthrough - write as-is, suppressing the error.
  workers:<n>               Number of concurrent workers (default: CPU count).
  zip:true                  Package output into a ZIP archive instead of a directory.
  dicomdir:true             Generate a DICOMDIR index in the output directory.
  profile:<name>            Apply a named profile from ~/.dicomtool/profiles.json.
  verbose:true              Print per-file progress and a completion summary.

Processing order per file:
  1. Parse   2. fixvr   3. ignoretype   4. ignoremodality   5. noprivate
  6. remove  7. dob     8. uid          9. maskrows          10. set   11. write

Examples:
  dicomtool modify input:C:\in output:C:\out set:PatientName=ANON noprivate:true
  dicomtool modify input:C:\in output:C:\out profile:base-anon
  dicomtool modify input:C:\in output:C:\out.zip zip:true set:PatientName=ANON
  dicomtool modify input:C:\in output:C:\out fixvr:correct verbose:true`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runModify()
	},
}

func init() {
	rootCmd.AddCommand(modifyCmd)
}

// tagEdit holds a parsed tag and its replacement string value.
type tagEdit struct {
	tag   tag.Tag
	value string
}

func runModify() error {
	if len(Opts.Inputs) != 1 {
		return errors.New("exactly one input:<dir> is required")
	}
	inputDir := Opts.Inputs[0]

	info, err := os.Stat(inputDir)
	if err != nil {
		return fmt.Errorf("input %q: %w", inputDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("input %q is not a directory", inputDir)
	}

	// Resolve a relative output path against the input directory.
	if !filepath.IsAbs(Opts.Output) {
		Opts.Output = filepath.Join(inputDir, Opts.Output)
	}

	rawSets := param("set")
	rawRemoves := param("remove")
	dobMask := paramOne("dob")
	uidSuffix := paramOne("uid")
	fixvrMode := strings.ToLower(paramOne("fixvr"))
	if fixvrMode != "" && fixvrMode != "correct" && fixvrMode != "skip" && fixvrMode != "passthrough" {
		return fmt.Errorf("fixvr %q: must be correct, skip, or passthrough", fixvrMode)
	}

	maskRows := 0
	if s := paramOne("maskrows"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return fmt.Errorf("maskrows %q must be a positive integer", s)
		}
		maskRows = n
	}

	numWorkers := runtime.NumCPU()
	if s := paramOne("workers"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return fmt.Errorf("workers %q must be a non-negative integer", s)
		}
		if n > 0 {
			numWorkers = n
		}
	}

	// Require at least one actionable parameter. Profile values are already
	// merged into parsed by this point, so this check covers profile-only runs.
	hasAction := len(rawSets) > 0 || len(rawRemoves) > 0 ||
		dobMask != "" || uidSuffix != "" || maskRows > 0 ||
		boolParam("noprivate", false) || fixvrMode != "" ||
		paramOne("ignoretype") != "" || paramOne("ignoremodality") != "" ||
		len(Opts.PerModality) > 0
	if !hasAction {
		return errors.New("at least one actionable parameter is required (set, remove, dob, uid, maskrows, noprivate, ignoretype, ignoremodality) — or specify a profile that contains one")
	}

	if dobMask != "" && len(dobMask) != 8 {
		return fmt.Errorf("dob mask must be exactly 8 characters (YYYYMMDD format), got %d", len(dobMask))
	}

	if uidSuffix != "" {
		for _, c := range uidSuffix {
			if c < '1' || c > '9' {
				return fmt.Errorf("uid suffix %q must contain digits in the set [1..9] only", uidSuffix)
			}
		}
	}

	removals := make([]tag.Tag, 0, len(rawRemoves))
	for _, r := range rawRemoves {
		t, err := parseTagString(Opts.TagAliases.Resolve(r))
		if err != nil {
			return fmt.Errorf("invalid remove tag %q: %w", r, err)
		}
		removals = append(removals, t)
	}

	edits := make([]tagEdit, 0, len(rawSets))
	for _, s := range rawSets {
		tagStr, value, ok := strings.Cut(s, "=")
		if !ok || tagStr == "" {
			return fmt.Errorf("invalid set value %q: expected <tag>=<value>", s)
		}
		t, err := parseTagString(Opts.TagAliases.Resolve(tagStr))
		if err != nil {
			return fmt.Errorf("invalid tag %q: %w", tagStr, err)
		}
		edits = append(edits, tagEdit{tag: t, value: value})
	}

	removePrivate := boolParam("noprivate", false)
	zipOutput := boolParam("zip", false)

	var ignoreTypes []string
	if s := paramOne("ignoretype"); s != "" {
		for _, v := range strings.Split(s, ",") {
			if v = strings.TrimSpace(v); v != "" {
				ignoreTypes = append(ignoreTypes, v)
			}
		}
	}

	var ignoreModalities []string
	if s := paramOne("ignoremodality"); s != "" {
		for _, v := range strings.Split(s, ",") {
			if v = strings.TrimSpace(v); v != "" {
				ignoreModalities = append(ignoreModalities, v)
			}
		}
	}

	if zipOutput && boolParam("dicomdir", false) {
		return errors.New("zip:true and dicomdir:true cannot be combined")
	}

	// Set up zip writer when zip:true. The output path is used as the zip file
	// path; a .zip extension is appended if not already present.
	var zw *zip.Writer
	var zf *os.File
	zipPath := ""
	if zipOutput {
		p := Opts.Output
		if !strings.HasSuffix(strings.ToLower(p), ".zip") {
			p += ".zip"
		}
		zipPath = p
		if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
		var zerr error
		zf, zerr = os.Create(zipPath)
		if zerr != nil {
			return fmt.Errorf("creating zip %q: %w", zipPath, zerr)
		}
		zw = zip.NewWriter(zf)
	}

	// Pre-compute per-modality override parameter sets from the profile.
	perModOverrides := buildModalityOverrides(Opts.PerModality)

	// Phase 1: collect DICOM file paths (fast, serial walk).
	type fileJob struct{ path, rel string }
	var jobs []fileJob
	if werr := filepath.WalkDir(inputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ok, ferr := isDICOMFile(path)
		if ferr != nil {
			return fmt.Errorf("checking %q: %w", path, ferr)
		}
		if !ok {
			return nil
		}
		rel, rerr := filepath.Rel(inputDir, path)
		if rerr != nil {
			return rerr
		}
		jobs = append(jobs, fileJob{path, rel})
		return nil
	}); werr != nil {
		if zw != nil {
			zw.Close()
		}
		if zf != nil {
			zf.Close()
		}
		return werr
	}

	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}

	// Phase 2: process files using a worker pool.
	var (
		mu        sync.Mutex
		firstErr  error
		fileCount int
		spinIdx   int
	)
	spinChars := []byte{'|', '/', '-', '\\', '|', '/', '-', '\\'}
	if !Opts.Verbose {
		fmt.Printf("%c", spinChars[0])
	}

	recordErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	hasErr := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return firstErr != nil
	}

	jobCh := make(chan fileJob, numWorkers)
	var zipMu sync.Mutex // serialises zip entry creation + write
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if hasErr() {
					continue
				}
				srcFile, ferr := openDICOMFile(job.path)
				if ferr != nil {
					recordErr(fmt.Errorf("opening %q: %w", job.path, ferr))
					continue
				}
				if srcFile == nil {
					continue
				}
				skipped, ds, perr := processFile(srcFile, edits, removals, dobMask, uidSuffix, removePrivate, maskRows, ignoreTypes, ignoreModalities, fixvrMode, perModOverrides)
				if perr != nil {
					recordErr(fmt.Errorf("processing %q: %w", job.path, perr))
					continue
				}
				if skipped {
					if Opts.Verbose {
						fmt.Printf("skipped (secondary capture): %s\n", job.path)
					}
					continue
				}
				if zipOutput {
					zipMu.Lock()
					hdr := &zip.FileHeader{
						Name:     filepath.ToSlash(job.rel),
						Method:   zip.Deflate,
						Modified: time.Now(),
					}
					w, zerr := zw.CreateHeader(hdr)
					if zerr == nil {
						zerr = dicom.Write(w, ds, fixvrWriteOpts(fixvrMode)...)
					}
					zipMu.Unlock()
					if zerr != nil {
						recordErr(fmt.Errorf("zip %q: %w", job.rel, zerr))
						continue
					}
					if Opts.Verbose {
						for _, e := range edits {
							fmt.Printf("  set %s = %q\n", e.tag, e.value)
						}
						fmt.Printf("zipped: %s\n", job.rel)
					}
				} else {
					outFile := filepath.Join(Opts.Output, job.rel)
					if merr := os.MkdirAll(filepath.Dir(outFile), 0o755); merr != nil {
						recordErr(fmt.Errorf("create output dir: %w", merr))
						continue
					}
					f, cerr := os.Create(outFile)
					if cerr != nil {
						recordErr(fmt.Errorf("create output file: %w", cerr))
						continue
					}
					bw := bufio.NewWriterSize(f, 1<<20)
					werr := dicom.Write(bw, ds, fixvrWriteOpts(fixvrMode)...)
					ferr = bw.Flush()
					cerr = f.Close()
					if werr != nil {
						recordErr(fmt.Errorf("write: %w", werr))
						continue
					}
					if ferr != nil {
						recordErr(fmt.Errorf("flush: %w", ferr))
						continue
					}
					if cerr != nil {
						recordErr(fmt.Errorf("close: %w", cerr))
						continue
					}
					if Opts.Verbose {
						for _, e := range edits {
							fmt.Printf("  set %s = %q\n", e.tag, e.value)
						}
						fmt.Printf("written: %s\n", outFile)
					}
				}
				mu.Lock()
				fileCount++
				if !Opts.Verbose {
					spinIdx = (spinIdx + 1) % len(spinChars)
					fmt.Printf("\b%c", spinChars[spinIdx])
				}
				mu.Unlock()
			}
		}()
	}

	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)
	wg.Wait()

	if firstErr != nil {
		if zw != nil {
			zw.Close()
		}
		if zf != nil {
			zf.Close()
		}
		return firstErr
	}

	if zipOutput {
		if err := zw.Close(); err != nil {
			zf.Close()
			return fmt.Errorf("finalising zip: %w", err)
		}
		if err := zf.Close(); err != nil {
			return fmt.Errorf("closing zip: %w", err)
		}
		if Opts.Verbose {
			fmt.Printf("%d file(s) written to: %s\n", fileCount, zipPath)
		} else {
			fmt.Printf("\b%d file(s) written to: %s\n", fileCount, zipPath)
		}
	} else {
		if Opts.Verbose {
			fmt.Printf("%d file(s) processed\n", fileCount)
		} else {
			fmt.Printf("\b%d file(s) processed\n", fileCount)
		}
	}

	if boolParam("dicomdir", false) {
		if err := WriteDICOMDIR(Opts.Output); err != nil {
			return err
		}
	}
	return nil
}

// modalityTag is (0008,0060) — Modality.
var modalityTag = tag.Tag{Group: 0x0008, Element: 0x0060}

// imageTypeTag is (0008,0008) — Image Type.
var imageTypeTag = tag.Tag{Group: 0x0008, Element: 0x0008}

// elemStringComponents extracts all string components from a DICOM element,
// handling the three ways the library may store a CS/LO/SH value:
//   - []string  — each element may itself be a backslash-delimited multi-value
//   - []byte    — raw bytes (implicit-VR files); split on backslash
//   - anything else — stringify via fmt, strip brackets, split on backslash
//
// Each component is trimmed of ASCII spaces and null-byte padding.
func elemStringComponents(elem *dicom.Element) []string {
	if elem == nil || elem.Value == nil {
		return nil
	}
	var raw string
	switch v := elem.Value.GetValue().(type) {
	case []string:
		raw = strings.Join(v, `\`)
	case []byte:
		raw = string(v)
	default:
		// Fallback: use the library's own string representation and strip any
		// surrounding brackets that some versions add (e.g. "[VAL1 VAL2]").
		raw = strings.Trim(fmt.Sprintf("%v", v), "[]")
	}
	var components []string
	for _, part := range strings.Split(raw, `\`) {
		part = strings.Trim(part, " \x00")
		if part != "" {
			components = append(components, part)
		}
	}
	return components
}

// fixvrWriteOpts returns WriteOptions that match the fixvr mode.
// correct/skip: applyFixVR pre-processes top-level elements, but nested
// sequence elements are not recursed into, so we still need SkipVRVerification
// at write time to prevent the writer from erroring on those.
// passthrough: additionally skips value-type verification since elements are
// written as-is without any pre-processing.
func fixvrWriteOpts(mode string) []dicom.WriteOption {
	switch mode {
	case "correct", "skip":
		return []dicom.WriteOption{dicom.SkipVRVerification()}
	case "passthrough":
		return []dicom.WriteOption{dicom.SkipVRVerification(), dicom.SkipValueTypeVerification()}
	default:
		return nil
	}
}

// modalityOverride holds the pre-computed parameter overrides for one modality.
type modalityOverride struct {
	edits         []tagEdit
	removals      []tag.Tag
	keep          []tag.Tag
	dobMask       string
	uidSuffix     string
	fixvrMode     string
	maskRows      int
	removePrivate bool
	keepPrivate   bool
}

// buildModalityOverrides converts the per-modality Profile map (already keyed
// in uppercase) into pre-parsed modalityOverride values ready for runtime use.
func buildModalityOverrides(perMod map[string]Profile) map[string]modalityOverride {
	if len(perMod) == 0 {
		return nil
	}
	result := make(map[string]modalityOverride, len(perMod))
	for mod, p := range perMod {
		var ov modalityOverride
		for _, s := range p.Sets {
			tagStr, value, ok := strings.Cut(s, "=")
			if !ok || tagStr == "" {
				continue
			}
			t, err := parseTagString(Opts.TagAliases.Resolve(tagStr))
			if err != nil {
				continue
			}
			ov.edits = append(ov.edits, tagEdit{tag: t, value: value})
		}
		for _, r := range p.Removes {
			t, err := parseTagString(Opts.TagAliases.Resolve(r))
			if err != nil {
				continue
			}
			ov.removals = append(ov.removals, t)
		}
		for _, k := range p.Keep {
			t, err := parseTagString(Opts.TagAliases.Resolve(k))
			if err != nil {
				continue
			}
			ov.keep = append(ov.keep, t)
		}
		ov.dobMask = p.DOB
		ov.uidSuffix = p.UIDSuffix
		ov.fixvrMode = p.FixVR
		ov.maskRows = p.MaskRows
		ov.removePrivate = p.Priv
		ov.keepPrivate = p.KeepPrivate
		result[mod] = ov
	}
	return result
}

// mergeEdits combines two tagEdit slices; override entries win on tag collision.
func mergeEdits(base, override []tagEdit) []tagEdit {
	if len(override) == 0 {
		return base
	}
	overrideTags := make(map[tag.Tag]bool, len(override))
	for _, e := range override {
		overrideTags[e.tag] = true
	}
	result := make([]tagEdit, 0, len(base)+len(override))
	for _, e := range base {
		if !overrideTags[e.tag] {
			result = append(result, e)
		}
	}
	return append(result, override...)
}

// filterKeep returns removals with any tag present in keep removed.
func filterKeep(removals []tag.Tag, keep []tag.Tag) []tag.Tag {
	if len(keep) == 0 {
		return removals
	}
	keepSet := make(map[tag.Tag]bool, len(keep))
	for _, t := range keep {
		keepSet[t] = true
	}
	out := make([]tag.Tag, 0, len(removals))
	for _, t := range removals {
		if !keepSet[t] {
			out = append(out, t)
		}
	}
	return out
}

// processFile parses src, optionally skips secondary-capture files, and applies
// all edits. It returns (true, zero, nil) when the file should be skipped, or
// (false, transformed dataset, nil) on success. The caller is responsible for
// writing the returned dataset to its destination.
func processFile(src *os.File, edits []tagEdit, removals []tag.Tag, dobMask, uidSuffix string, removePrivate bool, maskRows int, ignoreTypes, ignoreModalities []string, fixvrMode string, perModOverrides map[string]modalityOverride) (skipped bool, ds dicom.Dataset, err error) {
	info, err := src.Stat()
	if err != nil {
		src.Close()
		return false, ds, fmt.Errorf("stat: %w", err)
	}
	br := bufio.NewReaderSize(src, 1<<20)
	ds, err = dicom.Parse(br, info.Size(), nil)
	src.Close()
	if err != nil {
		return false, ds, fmt.Errorf("parse: %w", err)
	}

	if len(ignoreTypes) > 0 {
		if elem, err := ds.FindElementByTag(imageTypeTag); err == nil {
			for _, component := range elemStringComponents(elem) {
				for _, ignore := range ignoreTypes {
					if strings.EqualFold(component, strings.TrimSpace(ignore)) {
						return true, ds, nil
					}
				}
			}
		}
	}

	if len(ignoreModalities) > 0 {
		if elem, err := ds.FindElementByTag(modalityTag); err == nil {
			for _, component := range elemStringComponents(elem) {
				for _, ignore := range ignoreModalities {
					if strings.EqualFold(component, strings.TrimSpace(ignore)) {
						return true, ds, nil
					}
				}
			}
		}
	}

	// Apply per-modality overrides: layer modality-specific settings on top of
	// the base parameters before any modifications are applied.
	if len(perModOverrides) > 0 {
		if modElem, err := ds.FindElementByTag(modalityTag); err == nil {
			for _, mod := range elemStringComponents(modElem) {
				modKey := strings.ToUpper(strings.TrimSpace(mod))
				if ov, ok := perModOverrides[modKey]; ok {
					edits = mergeEdits(edits, ov.edits)
					removals = append(removals, ov.removals...)
					removals = filterKeep(removals, ov.keep)
					if ov.dobMask != "" {
						dobMask = ov.dobMask
					}
					if ov.uidSuffix != "" {
						uidSuffix = ov.uidSuffix
					}
					if ov.fixvrMode != "" {
						fixvrMode = ov.fixvrMode
					}
					if ov.maskRows > 0 {
						maskRows = ov.maskRows
					}
					if ov.removePrivate {
						removePrivate = true
					}
					if ov.keepPrivate {
						removePrivate = false
					}
					if Opts.Verbose {
						fmt.Printf("  [%s override] applied\n", modKey)
					}
					break
				}
			}
		}
	}

	if fixvrMode == "correct" || fixvrMode == "skip" {
		applyFixVR(&ds, fixvrMode)
	}

	if removePrivate {
		filtered := ds.Elements[:0]
		for _, elem := range ds.Elements {
			if elem.Tag.Group%2 != 1 {
				filtered = append(filtered, elem)
			}
		}
		ds.Elements = filtered
	}

	if len(removals) > 0 {
		filtered := ds.Elements[:0]
		for _, elem := range ds.Elements {
			remove := false
			for _, t := range removals {
				if elem.Tag == t {
					remove = true
					break
				}
			}
			if !remove {
				filtered = append(filtered, elem)
			}
		}
		ds.Elements = filtered
	}

	if dobMask != "" {
		if err := applyDOBMask(&ds, dobMask); err != nil {
			return false, ds, err
		}
	}

	if uidSuffix != "" {
		applyUIDSuffix(&ds, uidSuffix)
	}

	if maskRows > 0 {
		applyRowMask(&ds, maskRows)
	}

	for _, e := range edits {
		if err := applyEdit(&ds, e); err != nil {
			return false, ds, err
		}
	}

	return false, ds, nil
}

// applyEdit replaces or inserts an element in ds for the given tagEdit.
func applyEdit(ds *dicom.Dataset, e tagEdit) error {
	newElem, err := buildElement(ds, e)
	if err != nil {
		return err
	}

	for i, elem := range ds.Elements {
		if elem.Tag == e.tag {
			ds.Elements[i] = newElem
			return nil
		}
	}
	// Tag not present — append it.
	ds.Elements = append(ds.Elements, newElem)
	return nil
}

// buildElement creates a replacement *Element whose value encoding matches the
// VR of the existing element (or the standard VR if the tag is not present).
func buildElement(ds *dicom.Dataset, e tagEdit) (*dicom.Element, error) {
	// Determine the VR kind: prefer what's already in the dataset, fall back
	// to the standard tag dictionary.
	vrKind := tag.VRStringList
	if existing, err := ds.FindElementByTag(e.tag); err == nil {
		vrKind = existing.ValueRepresentation
	} else if info, err := tag.Find(e.tag); err == nil {
		vrKind = tag.GetVRKind(e.tag, info.VRs[0])
	}

	switch vrKind {
	case tag.VRUInt16List, tag.VRUInt32List, tag.VRInt16List, tag.VRInt32List:
		n, err := strconv.Atoi(e.value)
		if err != nil {
			return nil, fmt.Errorf("tag %s requires an integer value, got %q", e.tag, e.value)
		}
		return dicom.NewElement(e.tag, []int{n})

	case tag.VRFloat32List, tag.VRFloat64List:
		f, err := strconv.ParseFloat(e.value, 64)
		if err != nil {
			return nil, fmt.Errorf("tag %s requires a float value, got %q", e.tag, e.value)
		}
		return dicom.NewElement(e.tag, []float64{f})

	case tag.VRBytes:
		return dicom.NewElement(e.tag, []byte(e.value))

	default:
		// VRStringList, VRString, VRDate, VRUnknown, etc.
		return dicom.NewElement(e.tag, []string{e.value})
	}
}

// maxUIDLength is the maximum number of characters permitted in a DICOM UID
// (DICOM PS3.5 §9.1).
const maxUIDLength = 64

// applyUIDSuffix iterates every element in ds that carries a UI (UID) value
// and appends ".<suffix>" to it. If the resulting string would exceed
// maxUIDLength, the last dot-delimited component of the original UID is
// replaced with suffix instead.
func applyUIDSuffix(ds *dicom.Dataset, suffix string) {
	for _, elem := range ds.Elements {
		if elem.RawValueRepresentation != "UI" {
			continue
		}
		// Transfer Syntax UIDs must not be modified — they describe the encoding
		// of the file itself and must remain valid, recognised UIDs.
		if elem.Tag == tag.TransferSyntaxUID || elem.Tag == tag.ReferencedTransferSyntaxUIDInFile {
			continue
		}
		vals, ok := elem.Value.GetValue().([]string)
		if !ok {
			continue
		}
		modified := make([]string, len(vals))
		for i, uid := range vals {
			candidate := uid + "." + suffix
			if len(candidate) <= maxUIDLength {
				modified[i] = candidate
			} else {
				// Replace the last component.
				if dot := strings.LastIndex(uid, "."); dot >= 0 {
					modified[i] = uid[:dot+1] + suffix
				} else {
					// No dot at all — just use the suffix directly.
					modified[i] = suffix
				}
			}
		}
		v, err := dicom.NewValue(modified)
		if err != nil {
			continue
		}
		elem.Value = v
	}
}

// applyDOBMask reads the existing PatientBirthDate (0010,0030), applies mask,
// and writes the result back. Each mask character that is a digit (0-9)
// replaces the corresponding position; any other character preserves the
// original digit. Both the original value and mask must be 8 characters.
func applyDOBMask(ds *dicom.Dataset, mask string) error {
	dobTag := tag.Tag{Group: 0x0010, Element: 0x0030}

	original := ""
	if elem, err := ds.FindElementByTag(dobTag); err == nil {
		if v, ok := elem.Value.GetValue().([]string); ok && len(v) > 0 {
			original = v[0]
		}
	}

	// Pad or truncate original to exactly 8 characters.
	src := []byte("00000000")
	copy(src, original)

	result := make([]byte, 8)
	for i := 0; i < 8; i++ {
		if mask[i] >= '0' && mask[i] <= '9' {
			result[i] = mask[i]
		} else {
			result[i] = src[i]
		}
	}

	return applyEdit(ds, tagEdit{tag: dobTag, value: string(result)})
}

// parseTagString converts a "GGGG,EEEE" hex string into a tag.Tag.
func parseTagString(s string) (tag.Tag, error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return tag.Tag{}, fmt.Errorf("expected format GGGG,EEEE")
	}
	group, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 16, 16)
	if err != nil {
		return tag.Tag{}, fmt.Errorf("invalid group %q: %w", parts[0], err)
	}
	elem, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 16, 16)
	if err != nil {
		return tag.Tag{}, fmt.Errorf("invalid element %q: %w", parts[1], err)
	}
	return tag.Tag{Group: uint16(group), Element: uint16(elem)}, nil
}

// applyRowMask zeros the first numRows pixel rows from the top of each frame
// in ds. Compressed (encapsulated) images are skipped with a warning when
// verbose output is enabled.
func applyRowMask(ds *dicom.Dataset, numRows int) {
	pixelDataTag := tag.Tag{Group: 0x7FE0, Element: 0x0010}
	elem, err := ds.FindElementByTag(pixelDataTag)
	if err != nil {
		if Opts.Verbose {
			fmt.Println("  maskrows: no pixel data element found — skipping")
		}
		return
	}

	info, ok := elem.Value.GetValue().(dicom.PixelDataInfo)
	if !ok {
		if Opts.Verbose {
			fmt.Println("  maskrows: unrecognised pixel data value type — skipping")
		}
		return
	}
	if info.IsEncapsulated {
		if Opts.Verbose {
			fmt.Println("  maskrows: pixel data is compressed (encapsulated) — skipping")
		}
		return
	}

	maskedFrames := 0
	for _, f := range info.Frames {
		if f.Encapsulated {
			if Opts.Verbose {
				fmt.Println("  maskrows: frame is encapsulated — skipping frame")
			}
			continue
		}
		native := f.NativeData // frame.INativeFrame
		if native == nil {
			continue
		}
		rows := native.Rows()
		cols := native.Cols()
		spp := native.SamplesPerPixel()
		if rows == 0 || cols == 0 || spp == 0 {
			continue
		}
		count := numRows
		if count > rows {
			count = rows
		}
		// Samples to zero = masked rows × pixels-per-row × samples-per-pixel.
		n := count * cols * spp
		switch raw := native.RawDataSlice().(type) {
		case []uint8:
			if n > len(raw) {
				n = len(raw)
			}
			for i := range raw[:n] {
				raw[i] = 0
			}
			maskedFrames++
		case []uint16:
			if n > len(raw) {
				n = len(raw)
			}
			for i := range raw[:n] {
				raw[i] = 0
			}
			maskedFrames++
		case []uint32:
			if n > len(raw) {
				n = len(raw)
			}
			for i := range raw[:n] {
				raw[i] = 0
			}
			maskedFrames++
		default:
			if Opts.Verbose {
				fmt.Printf("  maskrows: unsupported pixel sample type (%T) — skipping frame\n", native.RawDataSlice())
			}
		}
	}
	if Opts.Verbose && maskedFrames > 0 {
		fmt.Printf("  maskrows: zeroed top %d row(s) across %d frame(s)\n", numRows, maskedFrames)
	}
}

// applyFixVR scans ds for elements whose VR does not match the DICOM standard
// dictionary and either removes them (mode "skip") or attempts to re-encode
// them with the correct VR (mode "correct", falling back to removal on failure).
// Sequence elements are recursed into so that nested elements are also fixed.
// Tags not found in the dictionary (private tags, unknown tags) are left as-is.
func applyFixVR(ds *dicom.Dataset, mode string) {
	ds.Elements = fixVRElements(ds.Elements, mode)
}

// fixVRElements applies VR correction/removal to a flat element list, recursing
// into any sequence elements it encounters.
func fixVRElements(elements []*dicom.Element, mode string) []*dicom.Element {
	filtered := make([]*dicom.Element, 0, len(elements))
	for _, elem := range elements {
		// Recurse into sequences before checking the element's own VR.
		if elem.Value != nil && elem.Value.ValueType() == dicom.Sequences {
			seqItems, ok := elem.Value.GetValue().([]*dicom.SequenceItemValue)
			if ok {
				rebuilt := make([][]*dicom.Element, 0, len(seqItems))
				for _, item := range seqItems {
					itemElems, ok2 := item.GetValue().([]*dicom.Element)
					if ok2 {
						rebuilt = append(rebuilt, fixVRElements(itemElems, mode))
					} else {
						rebuilt = append(rebuilt, nil)
					}
				}
				if newVal, err := dicom.NewValue(rebuilt); err == nil {
					elem.Value = newVal
				}
			}
			filtered = append(filtered, elem)
			continue
		}

		if elem.RawValueRepresentation == "" {
			filtered = append(filtered, elem)
			continue
		}
		tagInfo, err := tag.Find(elem.Tag)
		if err != nil {
			// Unknown or private tag — cannot verify.
			filtered = append(filtered, elem)
			continue
		}
		vrOK := false
		for _, vr := range tagInfo.VRs {
			if vr == elem.RawValueRepresentation {
				vrOK = true
				break
			}
		}
		if vrOK {
			filtered = append(filtered, elem)
			continue
		}
		// VR mismatch.
		if mode == "skip" {
			if Opts.Verbose {
				fmt.Printf("  fixvr: removed %v (VR=%s, expected %v)\n",
					elem.Tag, elem.RawValueRepresentation, tagInfo.VRs)
			}
			continue
		}
		// mode == "correct": attempt re-encoding under the standard VR.
		corrected, cerr := rebuildWithCorrectVR(elem, tagInfo.VRs[0])
		if cerr != nil {
			if Opts.Verbose {
				fmt.Printf("  fixvr: removed %v (VR=%s→%s correction failed: %v)\n",
					elem.Tag, elem.RawValueRepresentation, tagInfo.VRs[0], cerr)
			}
			continue
		}
		if Opts.Verbose {
			fmt.Printf("  fixvr: corrected %v VR %s→%s\n",
				elem.Tag, elem.RawValueRepresentation, tagInfo.VRs[0])
		}
		filtered = append(filtered, corrected)
	}
	return filtered
}

// rebuildWithCorrectVR creates a new element for elem.Tag using correctVR,
// converting the stored value to the appropriate Go type. Returns an error
// when the conversion is not possible.
func rebuildWithCorrectVR(elem *dicom.Element, correctVR string) (*dicom.Element, error) {
	vrKind := tag.GetVRKind(elem.Tag, correctVR)
	raw := elem.Value.GetValue()

	switch vrKind {
	case tag.VRStringList, tag.VRString, tag.VRDate:
		var strs []string
		switch v := raw.(type) {
		case []string:
			strs = v
		case []byte:
			for _, part := range strings.Split(strings.TrimRight(string(v), "\x00 "), `\`) {
				if s := strings.Trim(part, " \x00"); s != "" {
					strs = append(strs, s)
				}
			}
		default:
			strs = []string{strings.Trim(fmt.Sprintf("%v", v), "[]")}
		}
		return dicom.NewElement(elem.Tag, strs)

	case tag.VRUInt16List, tag.VRUInt32List, tag.VRInt16List, tag.VRInt32List:
		switch v := raw.(type) {
		case []int:
			return dicom.NewElement(elem.Tag, v)
		case []byte:
			var ints []int
			switch len(v) % 2 {
			case 0:
				for i := 0; i+1 < len(v); i += 2 {
					ints = append(ints, int(binary.LittleEndian.Uint16(v[i:])))
				}
			default:
				return nil, fmt.Errorf("byte length %d not a multiple of 2", len(v))
			}
			return dicom.NewElement(elem.Tag, ints)
		case []string:
			var ints []int
			for _, s := range v {
				n, err := strconv.Atoi(strings.TrimSpace(s))
				if err != nil {
					return nil, fmt.Errorf("cannot parse %q as integer", s)
				}
				ints = append(ints, n)
			}
			return dicom.NewElement(elem.Tag, ints)
		default:
			return nil, fmt.Errorf("cannot convert %T to integer list", raw)
		}

	case tag.VRFloat32List, tag.VRFloat64List:
		switch v := raw.(type) {
		case []float64:
			return dicom.NewElement(elem.Tag, v)
		case []string:
			var floats []float64
			for _, s := range v {
				f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
				if err != nil {
					return nil, fmt.Errorf("cannot parse %q as float", s)
				}
				floats = append(floats, f)
			}
			return dicom.NewElement(elem.Tag, floats)
		default:
			return nil, fmt.Errorf("cannot convert %T to float list", raw)
		}

	case tag.VRBytes:
		switch v := raw.(type) {
		case []byte:
			return dicom.NewElement(elem.Tag, v)
		case []string:
			return dicom.NewElement(elem.Tag, []byte(strings.Join(v, `\`)))
		default:
			return nil, fmt.Errorf("cannot convert %T to bytes", raw)
		}

	default:
		return nil, fmt.Errorf("unsupported VR kind for %s", correctVR)
	}
}
