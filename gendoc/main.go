// gendoc generates dicomtool-manual.docx from embedded content.
// Run with: go run ./gendoc
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// ── XML helpers ───────────────────────────────────────────────────────────────

func esc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// ── Document builder ──────────────────────────────────────────────────────────

type Doc struct{ b strings.Builder }

func (d *Doc) w(s string) { d.b.WriteString(s) }

// runs converts text containing `backtick` spans into Word XML runs.
func (d *Doc) runs(text string) {
	parts := strings.Split(text, "`")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i%2 == 0 {
			d.w(`<w:r><w:t xml:space="preserve">` + esc(p) + `</w:t></w:r>`)
		} else {
			d.w(`<w:r><w:rPr>` +
				`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>` +
				`<w:sz w:val="18"/><w:szCs w:val="18"/>` +
				`<w:shd w:val="clear" w:color="auto" w:fill="EBEBEB"/>` +
				`</w:rPr><w:t xml:space="preserve">` + esc(p) + `</w:t></w:r>`)
		}
	}
}

func (d *Doc) TitleP(text string) {
	d.w(`<w:p><w:pPr><w:pStyle w:val="DocTitle"/></w:pPr><w:r><w:t>` + esc(text) + `</w:t></w:r></w:p>`)
}
func (d *Doc) SubtitleP(text string) {
	d.w(`<w:p><w:pPr><w:pStyle w:val="DocSubtitle"/></w:pPr><w:r><w:t>` + esc(text) + `</w:t></w:r></w:p>`)
}
func (d *Doc) H1(text string) {
	d.w(`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>` + esc(text) + `</w:t></w:r></w:p>`)
}
func (d *Doc) H2(text string) {
	d.w(`<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>` + esc(text) + `</w:t></w:r></w:p>`)
}
func (d *Doc) H3(text string) {
	d.w(`<w:p><w:pPr><w:pStyle w:val="Heading3"/></w:pPr><w:r><w:t>` + esc(text) + `</w:t></w:r></w:p>`)
}
func (d *Doc) P(text string) {
	d.w(`<w:p>`)
	d.runs(text)
	d.w(`</w:p>`)
}
func (d *Doc) Bullet(text string) {
	d.w(`<w:p><w:pPr><w:pStyle w:val="ListBullet"/></w:pPr>`)
	d.runs(text)
	d.w(`</w:p>`)
}
func (d *Doc) Code(text string) {
	for _, line := range strings.Split(text, "\n") {
		d.w(`<w:p><w:pPr><w:pStyle w:val="Code"/></w:pPr>`)
		if line != "" {
			d.w(`<w:r><w:t xml:space="preserve">` + esc(line) + `</w:t></w:r>`)
		}
		d.w(`</w:p>`)
	}
}
func (d *Doc) Space() {
	d.w(`<w:p><w:pPr><w:spacing w:after="0"/></w:pPr></w:p>`)
}
func (d *Doc) PageBreak() {
	d.w(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// Table renders a 2-column table; row 0 is the header row.
type Row struct{ A, B string }

func (d *Doc) Table(rows []Row) {
	border := `w:val="single" w:sz="4" w:space="0" w:color="BBBBBB"`
	d.w(`<w:tbl><w:tblPr>`)
	d.w(`<w:tblW w:w="0" w:type="auto"/>`)
	d.w(`<w:tblBorders>`)
	d.w(`<w:top ` + border + `/><w:left ` + border + `/><w:bottom ` + border + `/>` +
		`<w:right ` + border + `/><w:insideH ` + border + `/><w:insideV ` + border + `/>`)
	d.w(`</w:tblBorders>`)
	d.w(`<w:tblCellMar>` +
		`<w:top w:w="80" w:type="dxa"/><w:left w:w="140" w:type="dxa"/>` +
		`<w:bottom w:w="80" w:type="dxa"/><w:right w:w="140" w:type="dxa"/>` +
		`</w:tblCellMar>`)
	d.w(`</w:tblPr>`)
	d.w(`<w:tblGrid><w:gridCol w:w="2700"/><w:gridCol w:w="6300"/></w:tblGrid>`)

	for i, row := range rows {
		isHdr := i == 0
		d.w(`<w:tr>`)
		cells := []struct{ text, w string }{{row.A, "2700"}, {row.B, "6300"}}
		for _, cell := range cells {
			d.w(`<w:tc><w:tcPr><w:tcW w:w="` + cell.w + `" w:type="dxa"/>`)
			if isHdr {
				d.w(`<w:shd w:val="clear" w:color="auto" w:fill="2E74B5"/>`)
			} else if i%2 == 0 {
				d.w(`<w:shd w:val="clear" w:color="auto" w:fill="F5F5F5"/>`)
			}
			d.w(`</w:tcPr><w:p>`)
			if isHdr {
				d.w(`<w:r><w:rPr><w:b/><w:color w:val="FFFFFF"/>` +
					`<w:sz w:val="20"/><w:szCs w:val="20"/>` +
					`</w:rPr><w:t xml:space="preserve">` + esc(cell.text) + `</w:t></w:r>`)
			} else {
				parts := strings.Split(cell.text, "`")
				for j, p := range parts {
					if p == "" {
						continue
					}
					if j%2 == 0 {
						d.w(`<w:r><w:t xml:space="preserve">` + esc(p) + `</w:t></w:r>`)
					} else {
						d.w(`<w:r><w:rPr>` +
							`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>` +
							`<w:sz w:val="18"/><w:szCs w:val="18"/>` +
							`<w:shd w:val="clear" w:color="auto" w:fill="EBEBEB"/>` +
							`</w:rPr><w:t xml:space="preserve">` + esc(p) + `</w:t></w:r>`)
					}
				}
			}
			d.w(`</w:p></w:tc>`)
		}
		d.w(`</w:tr>`)
	}
	d.w(`</w:tbl><w:p/>`)
}

// ── Static XML parts ──────────────────────────────────────────────────────────

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
  <Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`

const wordRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>
</Relationships>`

const settings = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:defaultTabStop w:val="720"/>
  <w:compat>
    <w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/>
  </w:compat>
</w:settings>`

const numbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:multiLevelType w:val="hybridMultilevel"/>
    <w:lvl w:ilvl="0">
      <w:start w:val="1"/>
      <w:numFmt w:val="bullet"/>
      <w:lvlText w:val="&#x2022;"/>
      <w:lvlJc w:val="left"/>
      <w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr>
      <w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="22"/></w:rPr>
    </w:lvl>
  </w:abstractNum>
  <w:num w:numId="1">
    <w:abstractNumId w:val="0"/>
  </w:num>
</w:numbering>`

const coreProps = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties
  xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
  xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title>dicomtool Usage Manual</dc:title>
  <dc:creator>dicomtool</dc:creator>
</cp:coreProperties>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:docDefaults>
    <w:rPrDefault>
      <w:rPr>
        <w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/>
        <w:sz w:val="22"/><w:szCs w:val="22"/>
        <w:lang w:val="en-US"/>
      </w:rPr>
    </w:rPrDefault>
    <w:pPrDefault>
      <w:pPr>
        <w:spacing w:after="160" w:line="259" w:lineRule="auto"/>
      </w:pPr>
    </w:pPrDefault>
  </w:docDefaults>

  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
  </w:style>

  <w:style w:type="paragraph" w:styleId="DocTitle">
    <w:name w:val="DocTitle"/>
    <w:basedOn w:val="Normal"/>
    <w:pPr>
      <w:jc w:val="center"/>
      <w:spacing w:before="1440" w:after="240" w:line="259" w:lineRule="auto"/>
    </w:pPr>
    <w:rPr>
      <w:b/>
      <w:color w:val="2E74B5"/>
      <w:sz w:val="72"/><w:szCs w:val="72"/>
    </w:rPr>
  </w:style>

  <w:style w:type="paragraph" w:styleId="DocSubtitle">
    <w:name w:val="DocSubtitle"/>
    <w:basedOn w:val="Normal"/>
    <w:pPr>
      <w:jc w:val="center"/>
      <w:spacing w:before="0" w:after="240" w:line="259" w:lineRule="auto"/>
    </w:pPr>
    <w:rPr>
      <w:color w:val="595959"/>
      <w:sz w:val="32"/><w:szCs w:val="32"/>
    </w:rPr>
  </w:style>

  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr>
      <w:keepNext/>
      <w:spacing w:before="480" w:after="120"/>
      <w:outlineLvl w:val="0"/>
    </w:pPr>
    <w:rPr>
      <w:b/>
      <w:color w:val="2E74B5"/>
      <w:sz w:val="40"/><w:szCs w:val="40"/>
    </w:rPr>
  </w:style>

  <w:style w:type="paragraph" w:styleId="Heading2">
    <w:name w:val="heading 2"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr>
      <w:keepNext/>
      <w:spacing w:before="320" w:after="80"/>
      <w:outlineLvl w:val="1"/>
    </w:pPr>
    <w:rPr>
      <w:b/>
      <w:color w:val="2E74B5"/>
      <w:sz w:val="28"/><w:szCs w:val="28"/>
    </w:rPr>
  </w:style>

  <w:style w:type="paragraph" w:styleId="Heading3">
    <w:name w:val="heading 3"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr>
      <w:keepNext/>
      <w:spacing w:before="200" w:after="40"/>
      <w:outlineLvl w:val="2"/>
    </w:pPr>
    <w:rPr>
      <w:b/>
      <w:color w:val="595959"/>
      <w:sz w:val="24"/><w:szCs w:val="24"/>
    </w:rPr>
  </w:style>

  <w:style w:type="paragraph" w:styleId="Code">
    <w:name w:val="Code"/>
    <w:basedOn w:val="Normal"/>
    <w:pPr>
      <w:spacing w:before="40" w:after="40" w:line="240" w:lineRule="auto"/>
      <w:ind w:left="400" w:right="400"/>
      <w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>
      <w:pBdr>
        <w:left w:val="single" w:sz="12" w:space="4" w:color="AAAAAA"/>
      </w:pBdr>
    </w:pPr>
    <w:rPr>
      <w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:cs="Courier New"/>
      <w:sz w:val="18"/><w:szCs w:val="18"/>
    </w:rPr>
  </w:style>

  <w:style w:type="paragraph" w:styleId="ListBullet">
    <w:name w:val="List Bullet"/>
    <w:basedOn w:val="Normal"/>
    <w:pPr>
      <w:numPr>
        <w:ilvl w:val="0"/>
        <w:numId w:val="1"/>
      </w:numPr>
      <w:spacing w:before="0" w:after="80"/>
    </w:pPr>
  </w:style>
</w:styles>`



// Document content

func buildContent(d *Doc) {

	// Title page
	d.Space()
	d.Space()
	d.Space()
	d.TitleP("dicomtool")
	d.SubtitleP("Usage Manual  v1.1.3")
	d.Space()
	d.P("A command-line utility for inspecting and modifying DICOM medical imaging files.")
	d.PageBreak()

	// 1. Introduction
	d.H1("1  Introduction")
	d.P("`dicomtool` is a command-line utility for processing DICOM medical imaging files without requiring specialist imaging software. It operates on entire directory trees, preserving the original folder structure in its output.")
	d.P("Key capabilities:")
	d.Bullet("Inspect DICOM tag values in one or more files")
	d.Bullet("Set or replace the value of any tag by its numeric identifier or a user-defined alias")
	d.Bullet("Remove specific tags by identifier")
	d.Bullet("Remove all private (odd-group) tags from a file")
	d.Bullet("Apply a positional mask to the Patient Date of Birth field")
	d.Bullet("Append a numeric suffix to all UID fields, with automatic length management")
	d.Bullet("Skip Secondary Capture (screenshot) files during batch processing")
	d.Bullet("Zero out a specified number of pixel rows from the top of each image frame")
	d.Bullet("Correct or remove tags whose Value Representation does not match the DICOM standard")
	d.Bullet("Process multiple files simultaneously using a configurable worker pool")
	d.Bullet("Generate a DICOMDIR index file for the output directory tree")
	d.Bullet("Define named processing profiles combining any of the above operations")
	d.Bullet("Map short alias names to DICOM tag identifiers for convenience")
	d.Space()
	d.P("Configuration is stored in two JSON files under `~/.dicomtool/`:")
	d.Bullet("`tags.json` -- tag alias mappings")
	d.Bullet("`profiles.json` -- named processing profiles")
	d.P("Both files are created automatically with default content on the first invocation.")

	// 2. Installation
	d.H1("2  Installation")
	d.P("Building from source requires Go 1.21 or later.")
	d.Code("git clone <repository-url>\ncd dicomtool\ngo build -o dicomtool.exe .")
	d.P("The resulting `dicomtool.exe` (Windows) or `dicomtool` (Linux / macOS) is a single self-contained binary with no runtime dependencies. Copy it anywhere on your `PATH`.")

	// 3. Parameter Syntax
	d.H1("3  Parameter Syntax")
	d.P("All parameters are passed as `key:value` pairs on the command line. The colon character (`:`) separates the key from its value. Keys are case-insensitive.")
	d.Code("dicomtool <command> key1:value1 key2:value2 ...")
	d.P("Bare arguments that contain no colon are treated as input paths. The following two forms are therefore equivalent:")
	d.Code("dicomtool inspect C:\\scans\\study01\ndicomtool inspect input:C:\\scans\\study01")
	d.P("Parameters that accept multiple values (e.g. `set:`, `remove:`, `tag:`, `input:`) may be repeated any number of times:")
	d.Code("dicomtool modify input:C:\\in output:C:\\out set:0010,0010=ANON set:0010,0020=ID001 remove:0008,0080")
	d.P("Boolean parameters accept `true` or `1` (case-insensitive). Any other value, including an empty value, is treated as false.")

	// 4. Commands Reference
	d.H1("4  Commands Reference")

	// 4.1 inspect
	d.H2("4.1  inspect")
	d.P("Parses one or more DICOM files and prints their tag values to the console.")
	d.H3("Syntax")
	d.Code("dicomtool inspect input:<file> [input:<file>...] (all:true | tag:<tag> [tag:<tag>...]) [verbose:true]")
	d.H3("Parameters")
	d.Table([]Row{
		{"Parameter", "Description"},
		{"`input:<file>`", "Path to a DICOM file to inspect. May be repeated for multiple files. Bare path arguments are also accepted."},
		{"`tag:<tag>`", "A specific tag to display, in `GGGG,EEEE` format or as a defined alias. May be repeated."},
		{"`all:true`", "Display every tag present in the file. Pixel data is reported as a summary line rather than raw bytes."},
		{"`verbose:true`", "Print additional diagnostic information."},
	})
	d.H3("Output Format")
	d.P("Each element is printed on one line:")
	d.Code("  (GGGG,EEEE)  VR  TagName                                  = value")
	d.P("Multi-value fields use the DICOM backslash separator. Binary fields longer than 16 bytes are summarised as `<binary, N bytes>`. Pixel data is always shown as a summary:")
	d.Code("File: C:\\scans\\image.dcm\n  (0002,0001)  OB  FileMetaInformationVersion        = 00 01\n  (0008,0020)  DA  StudyDate                          = 20240115\n  (0008,0060)  CS  Modality                           = CT\n  (0010,0010)  PN  PatientName                        = Smith^John\n  (0010,0030)  DA  PatientBirthDate                   = 19800101\n  (0020,000D)  UI  StudyInstanceUID                   = 1.2.840.10008...\n  (7FE0,0010)  OW  PixelData                          = [pixel data: skipped]")
	d.H3("Sequence Fields")
	d.P("Sequence (SQ) elements are expanded inline. Each sequence item is indented four spaces per nesting level:")
	d.Code("  (0008,1115)  SQ  ReferencedSeriesSequence          [sequence: 1 item(s)]\n    Item 1:\n      (0008,1140)  SQ  ReferencedImageSequence            [sequence: 2 item(s)]\n        Item 1:\n          (0008,1150)  UI  ReferencedSOPClassUID              = 1.2.840.10008.5.1.4.1.1.2\n          (0008,1155)  UI  ReferencedSOPInstanceUID           = 1.2.3.4.5.6.7.8\n        Item 2:\n          (0008,1150)  UI  ReferencedSOPClassUID              = 1.2.840.10008.5.1.4.1.1.2\n          (0008,1155)  UI  ReferencedSOPInstanceUID           = 1.2.3.4.5.6.7.9")
	d.H3("Notes")
	d.Bullet("Either `all:true` or at least one `tag:` parameter is required.")
	d.Bullet("When using `all:true`, pixel data is skipped during parsing to avoid loading large image buffers; it is shown as `[pixel data: skipped]`.")
	d.Bullet("When fetching a specific tag (e.g. `tag:7FE0,0010`), the file is fully parsed and pixel data dimensions are shown.")
	d.Bullet("Tag aliases defined in `tags.json` are resolved before lookup.")
	d.Bullet("If a requested tag is not present in the file, a `not found` line is printed and processing continues.")
	d.Bullet("If a file cannot be parsed, an error line is printed and the next file is processed.")
	d.Bullet("Sequence fields are expanded recursively with no depth limit. Very deeply nested sequences are indented accordingly.")
	d.H3("Examples")
	d.Code("dicomtool inspect C:\\scans\\image.dcm all:true")
	d.Code("dicomtool inspect input:image.dcm tag:0010,0010 tag:0010,0020")
	d.Code("dicomtool inspect input:image.dcm tag:PatientName tag:PatientID")
	d.Code("dicomtool inspect input:a.dcm input:b.dcm input:c.dcm all:true")

	// 4.2 modify
	d.H2("4.2  modify")
	d.P("Reads every DICOM file under an input directory tree, applies the specified modifications, and writes the results to an output directory, preserving the original folder structure.")
	d.H3("Syntax")
	d.Code("dicomtool modify input:<dir> output:<dir>\n    [set:<tag>=<value> ...]\n    [remove:<tag> ...]\n    [dob:<mask>]\n    [uid:<suffix>]\n    [noprivate:true]\n    [ignoretype:<types>]\n    [ignoremodality:<modalities>]\n    [maskrows:<n>]\n    [fixvr:correct|skip|passthrough]\n    [workers:<n>]\n    [zip:true]\n    [dicomdir:true]\n    [profile:<name>]\n    [verbose:true]")
	d.H3("Parameters")
	d.Table([]Row{
		{"Parameter", "Description"},
		{"`input:<dir>`", "Source directory. All DICOM files within the tree are processed. Required."},
		{"`output:<dir>`", "Destination directory. Created if it does not exist. A relative path is resolved against the input directory. Required."},
		{"`set:<tag>=<value>`", "Set the specified tag to the given value. `<tag>` may be a raw `GGGG,EEEE` identifier or a defined alias. Repeatable."},
		{"`remove:<tag>`", "Remove the specified tag entirely from every output file. `<tag>` may be a raw identifier or alias. Repeatable."},
		{"`dob:<mask>`", "Apply an 8-character positional mask to the Patient Date of Birth field (0010,0030). Digit characters in the mask overwrite the corresponding position; any other character preserves the original digit. Format: `YYYYMMDD`."},
		{"`uid:<suffix>`", "Append `.<suffix>` to every UID field in each file. If the result would exceed 64 characters the last dot-delimited component is replaced instead of appended. `<suffix>` must contain digits only. Transfer Syntax UIDs are excluded."},
		{"`noprivate:true`", "Remove all private tags (those with an odd group number) before writing output."},
		{"`ignoretype:<types>`", "Skip files whose Image Type tag (0008,0008) contains any of the supplied comma-delimited values. Comparison is case-insensitive. Example: `ignoretype:SECONDARY,DERIVED`."},
		{"`ignoremodality:<modalities>`", "Skip files whose Modality tag (0008,0060) matches any of the supplied comma-delimited values. Comparison is case-insensitive. Example: `ignoremodality:SC,PR`."},
		{"`maskrows:<n>`", "Zero out the first `n` pixel rows from the top of each image frame. Applies only to uncompressed (native) pixel data; compressed files produce a warning when `verbose:true` is set. `n` must be a positive integer."},
		{"`fixvr:correct|skip|passthrough`", "Handle tags whose Value Representation (VR) does not match the DICOM standard dictionary. `correct` attempts to re-encode the tag value under the correct VR, falling back to removal if the conversion is not possible. `skip` silently removes all mismatched tags. `passthrough` writes the file as-is, suppressing the VR verification error. Private and unknown tags are always kept unchanged. When `verbose:true` is set, each affected tag is reported."},
		{"`workers:<n>`", "Number of files to process simultaneously. Defaults to the number of logical CPU cores. Set to `1` to process files serially. Set to `0` to restore the default."},
		{"`zip:true`", "Package all output files into a single ZIP archive instead of writing them to a directory. The `output:` path is used as the ZIP file name; a `.zip` extension is appended automatically if not already present. Cannot be combined with `dicomdir:true`."},
		{"`dicomdir:true`", "After all files have been written, generate a DICOMDIR index file in the output directory. Cannot be combined with `zip:true`."},
		{"`profile:<name>`", "Apply a named processing profile from `profiles.json`. CLI parameters take precedence over profile values. See Section 6."},
		{"`verbose:true`", "Print a line for each file written, per-operation diagnostics, and a summary count on completion."},
	})
	d.H3("Processing Order")
	d.P("Operations are applied in the following order within each file:")
	d.Bullet("1. Parse source file")
	d.Bullet("2. Correct or remove VR-mismatched tags (if `fixvr:` supplied)")
	d.Bullet("3. Skip file if Image Type (0008,0008) matches any `ignoretype:` value (file is not written to output)")
	d.Bullet("4. Skip file if Modality (0008,0060) matches any `ignoremodality:` value (file is not written to output)")
	d.Bullet("5. Remove private tags (if `noprivate:true`)")
	d.Bullet("6. Apply explicit `remove:` removals")
	d.Bullet("7. Apply DOB mask (if `dob:` supplied)")
	d.Bullet("8. Apply UID suffix (if `uid:` supplied)")
	d.Bullet("9. Apply row mask (if `maskrows:` supplied)")
	d.Bullet("10. Apply all `set:` edits")
	d.Bullet("11. Write output file to directory, or to the ZIP archive if `zip:true`")
	d.H3("Non-DICOM Files")
	d.P("Files that do not carry the DICOM magic bytes (`DICM` at byte offset 128) are silently skipped regardless of file name or extension.")
	d.H3("Examples")
	d.Code("dicomtool modify input:C:\\original output:C:\\deidentified\n    set:0010,0010=ANONYMOUS set:0010,0020=ID001\n    remove:0008,0080 noprivate:true verbose:true")
	d.Code("dicomtool modify input:C:\\study output:converted\n    profile:anonymize dicomdir:true")
	d.Code("dicomtool modify input:C:\\study output:C:\\out\n    set:PatientName=ANON ignoremodality:SC maskrows:10")
	d.Code("dicomtool modify input:C:\\study output:C:\\out\\study.zip zip:true\n    set:PatientName=ANON noprivate:true")
	d.Code("dicomtool modify input:C:\\study output:C:\\out fixvr:correct\n    set:PatientName=ANON noprivate:true")
	d.Code("dicomtool modify input:C:\\study output:C:\\out workers:8\n    set:PatientName=ANON noprivate:true")

	// 4.3 tags
	d.H2("4.3  tags")
	d.P("Manages the tag alias mappings stored in `~/.dicomtool/tags.json`. Aliases let you use a short readable name wherever a `GGGG,EEEE` tag identifier is required.")

	d.H3("tags list")
	d.P("Lists all defined aliases.")
	d.Code("dicomtool tags list")

	d.H3("tags add")
	d.P("Adds or updates an alias. `<phrase>` is the alias name; `<tag>` must be a valid `GGGG,EEEE` identifier.")
	d.Code("dicomtool tags add <phrase> <tag>")
	d.Code("dicomtool tags add PatientName 0010,0010\ndicomtool tags add AccessionNumber 0008,0050\ndicomtool tags add InstitutionName 0008,0080")

	d.H3("tags remove")
	d.P("Removes an existing alias.")
	d.Code("dicomtool tags remove <phrase>")
	d.Code("dicomtool tags remove PatientName")

	// 4.4 profiles
	d.H2("4.4  profiles")
	d.P("Manages named processing profiles stored in `~/.dicomtool/profiles.json`. A profile bundles a set of `modify` parameters under a single name that can be applied with `profile:<name>`.")

	d.H3("profiles list")
	d.P("Lists the names of all defined profiles.")
	d.Code("dicomtool profiles list")

	d.H3("profiles show")
	d.P("Prints the JSON definition of a named profile.")
	d.Code("dicomtool profiles show <name>")
	d.Code("dicomtool profiles show anonymize")

	d.H3("profiles add")
	d.P("Creates or completely replaces a profile. Parameters are the same key:value pairs accepted by the `modify` command, excluding `input:`, `output:`, and `profile:`. An optional `base:<name>` parameter may reference an existing profile whose settings are inherited.")
	d.Code("dicomtool profiles add <name>\n    [base:<name>]\n    [set:<tag>=<value> ...] [remove:<tag> ...]\n    [dob:<mask>] [uid:<suffix>]\n    [noprivate:true] [maskrows:<n>]\n    [dicomdir:true] [verbose:true]")
	d.Code("dicomtool profiles add anonymize\n    set:PatientName=ANON set:PatientID=ANON set:AccessionNumber=\n    dob:YYYY0101 noprivate:true")
	d.Code("dicomtool profiles add research base:anonymize set:PatientID=RESEARCH001")
	d.P("Tag aliases are resolved to raw identifiers at save time, so profiles remain portable even if the alias definitions change later.")
	d.P("When `base:<name>` is supplied the named profile must already exist in the store. The base chain is resolved at runtime so any subsequent changes to the base profile are automatically reflected in derived profiles.")

	d.H3("profiles remove")
	d.P("Deletes a profile.")
	d.Code("dicomtool profiles remove <name>")

	// 4.5 version
	d.H2("4.5  version")
	d.P("Prints the application version string.")
	d.Code("dicomtool version")

	// 4.6 install
	d.H2("4.6  install")
	d.P("Writes the built-in default `tags.json` and `profiles.json` to the standard configuration directory (`~/.dicomtool/`), unconditionally overwriting any existing files.")
	d.H3("Syntax")
	d.Code("dicomtool install")
	d.H3("Description")
	d.P("Use `install` to reset your configuration to the factory defaults. This is useful when:")
	d.Bullet("You have corrupted or accidentally deleted your configuration files.")
	d.Bullet("You want to start fresh after making unwanted manual edits.")
	d.Bullet("You are setting up dicomtool on a new machine and want the default aliases and sample profile in place immediately.")
	d.P("Unlike the automatic first-run creation (which only creates files that do not already exist), `install` always overwrites both files even if they already exist.")
	d.H3("Output")
	d.P("Two lines are printed confirming the written paths:")
	d.Code("written: C:\\Users\\username\\.dicomtool\\tags.json\nwritten: C:\\Users\\username\\.dicomtool\\profiles.json")
	d.H3("Notes")
	d.Bullet("The configuration directory (`~/.dicomtool/`) is created if it does not exist.")
	d.Bullet("Any existing customisations in `tags.json` or `profiles.json` are permanently replaced. Back up the files first if you want to preserve them.")
	d.Bullet("`install` takes no parameters.")
	d.H3("Examples")
	d.Code("dicomtool install")

	// 5. Tag Aliases
	d.H1("5  Tag Aliases")
	d.H2("5.1  Overview")
	d.P("Tag aliases map a short readable phrase to a full DICOM tag identifier in `GGGG,EEEE` hex format. Once defined, the phrase can be used anywhere a tag identifier is accepted: `set:`, `remove:`, `tag:`, and `profiles add`.")
	d.P("Aliases are stored in `~/.dicomtool/tags.json` and loaded automatically on every invocation. A custom file location can be specified with `config:<path>`.")

	d.H2("5.2  File Format")
	d.P("The file is a flat JSON object mapping alias names to tag strings:")
	d.Code("{\n  \"PatientName\":     \"0010,0010\",\n  \"PatientID\":       \"0010,0020\",\n  \"AccessionNumber\": \"0008,0050\",\n  \"InstitutionName\": \"0008,0080\"\n}")

	d.H2("5.3  Alias Resolution")
	d.P("Alias resolution is case-sensitive and exact. If a supplied identifier is not found in the alias table it is used as-is and must therefore be a valid `GGGG,EEEE` string.")
	d.P("Resolution occurs at the following points:")
	d.Bullet("`inspect tag:<phrase>` -- resolved before display")
	d.Bullet("`modify set:<phrase>=<value>` -- resolved before applying the edit")
	d.Bullet("`modify remove:<phrase>` -- resolved before removing the tag")
	d.Bullet("`profiles add set:<phrase>=<value>` -- resolved and stored as raw tag at save time")
	d.Bullet("Profile merge at runtime -- CLI and profile set entries are both resolved for per-tag deduplication (see Section 6.3)")

	d.H2("5.4  Default Aliases")
	d.P("The default `tags.json` shipped with the binary defines the following aliases:")
	d.Table([]Row{
		{"Alias", "Tag"},
		{"TransferSyntaxUID", "0002,0010"},
		{"ReferencedTransferSyntaxUID", "0004,1512"},
		{"StudyDate", "0008,0020"},
		{"StudyTime", "0008,0030"},
		{"AccessionNumber", "0008,0050"},
		{"Modality", "0008,0060"},
		{"Manufacturer", "0008,0070"},
		{"InstitutionName", "0008,0080"},
		{"InstitutionAddress", "0008,0081"},
		{"ReferringPhysicianName", "0008,0090"},
		{"SeriesDescription", "0008,103E"},
		{"StudyDescription", "0008,1030"},
		{"PatientName", "0010,0010"},
		{"PatientID", "0010,0020"},
		{"PatientDOB", "0010,0030"},
		{"PatientSex", "0010,0040"},
		{"PatientAge", "0010,1010"},
		{"ProtocolName", "0018,1030"},
		{"StudyInstanceUID", "0020,000D"},
		{"SeriesInstanceUID", "0020,000E"},
		{"StudyID", "0020,0010"},
		{"SeriesNumber", "0020,0011"},
		{"InstanceNumber", "0020,0013"},
		{"ConfidentialityCode", "0040,1008"},
	})
	d.P("The full content of the default file is reproduced in Appendix A.")

	// 6. Processing Profiles
	d.H1("6  Processing Profiles")
	d.H2("6.1  Overview")
	d.P("A profile is a named collection of `modify` parameters stored in `~/.dicomtool/profiles.json`. Activating a profile with `profile:<name>` applies all its parameters as if they had been typed on the command line.")
	d.P("Profiles are intended to capture a recurring workflow -- for example a de-identification recipe -- so it can be applied consistently without retyping long parameter lists.")

	d.H2("6.2  File Format")
	d.Code("{\n  \"anonymize\": {\n    \"set\":            [\"0010,0010=ANON\", \"0010,0020=ANON\", \"0008,0050=\"],\n    \"remove\":         [\"0008,0080\", \"0008,0081\"],\n    \"dob\":            \"YYYY0101\",\n    \"uid\":            \"9999\",\n    \"noprivate\":      true,\n    \"maskrows\":       0,\n    \"ignoretype\":     [\"SECONDARY\"],\n    \"ignoremodality\": [\"SC\", \"PR\"],\n    \"fixvr\":          \"correct\",\n    \"dicomdir\":       false,\n    \"verbose\":        false\n  }\n}")
	d.P("All fields are optional. Omitted fields take their command-line defaults.")
	d.P("The full content of the default `profiles.json` is reproduced in Appendix A.")

	d.H2("6.3  CLI Override Precedence")
	d.P("When both a profile and command-line parameters are supplied, the following merge rules apply:")
	d.Table([]Row{
		{"Parameter type", "Merge rule"},
		{"`dob`, `uid`", "CLI value wins. The profile value is ignored if the parameter was supplied on the command line."},
		{"`maskrows`", "CLI value wins. The profile value is ignored if `maskrows:` was supplied on the command line."},
		{"`noprivate`, `dicomdir`, `verbose`", "Either source can enable the flag. If either the CLI or the profile sets it to true, the flag is active."},
		{"`set`", "Per-tag precedence. For each tag in the profile's set list, if the same tag (after alias resolution) appears in the CLI set list, the CLI value is used and the profile value is discarded. Tags only in the profile are added."},
		{"`remove`", "Additive union. Tags from both the CLI and the profile are removed."},
		{"`ignoretype`, `ignoremodality`", "Additive union. Values from both the CLI and the profile are combined, with duplicates removed."},
		{"`fixvr`", "CLI value wins. The profile value is used only if `fixvr:` was not supplied on the command line."},
	})
	d.P("This means a profile establishes a baseline that can be partially overridden at runtime. For example, a profile that sets PatientName to ANON can be overridden for a specific run with `set:PatientName=Smith` without affecting any other profile parameters.")

	d.H2("6.4  Base Profile Inheritance")
	d.P("A profile can inherit the settings of another profile by specifying `base:<name>` when it is created. When the derived profile is applied, the base chain is resolved at runtime: the base profile's settings are applied first, then the derived profile's settings are merged on top using the same precedence rules described in Section 6.3.")
	d.P("Example: create a base anonymization profile, then derive a research variant that keeps the same de-identification rules but overrides the Patient ID:")
	d.Code("dicomtool profiles add anonymize\n    set:PatientName=ANON set:PatientID=ANON\n    set:AccessionNumber= dob:YYYY0101 noprivate:true\n\ndicomtool profiles add research base:anonymize set:PatientID=RESEARCH001")
	d.P("Applying the `research` profile is equivalent to applying `anonymize` with `set:PatientID=RESEARCH001` added. All other `anonymize` settings -- PatientName, AccessionNumber, dob, noprivate -- are inherited unchanged.")
	d.P("Merge rules for base inheritance:")
	d.Table([]Row{
		{"Parameter type", "Merge rule"},
		{"`dob`, `uid`", "Derived value wins if non-empty; otherwise base value is used."},
		{"`maskrows`", "Derived value wins if greater than zero; otherwise base value is used."},
		{"`noprivate`, `dicomdir`, `verbose`", "OR'd: either base or derived being true activates the flag."},
		{"`set`", "Per-tag precedence. Derived profile wins for any tag it defines; base contributes remaining tags."},
		{"`remove`", "Union of both lists, deduplicated."},
		{"`ignoretype`, `ignoremodality`", "Union of both lists, deduplicated (case-insensitive)."},
		{"`fixvr`", "Derived value wins if non-empty; otherwise base value is used."},
	})
	d.P("Base chains may be arbitrarily deep (profile A bases on B, which bases on C, and so on). Circular references are detected and reported as an error.")

	d.H2("6.5  Modifying an Existing Profile")
	d.P("`profiles add` always performs a complete replace. To update a profile, re-run `profiles add` with the full desired parameter set. Alternatively, edit `~/.dicomtool/profiles.json` directly in a text editor -- the file is plain JSON.")
	d.P("To view the current definition before editing:")
	d.Code("dicomtool profiles show anonymize")

	d.H2("6.6  Setting a Tag to an Empty String in a Profile")
	d.P("To store an empty value for a tag (e.g. to blank the Accession Number), use `set:<tag>=` with nothing after the `=` sign:")
	d.Code("dicomtool profiles add anonymize set:AccessionNumber=")
	d.P("This stores `\"0008,0050=\"` in the profile's set list, which causes the tag to be written as an empty string when the profile is applied.")

	// 7. Configuration Files
	d.H1("7  Configuration Files")
	d.H2("7.1  Default Locations")
	d.Table([]Row{
		{"File", "Default path"},
		{"Tag aliases", "`%USERPROFILE%\\.dicomtool\\tags.json`"},
		{"Profiles", "`%USERPROFILE%\\.dicomtool\\profiles.json`"},
	})
	d.P("On Linux and macOS the directory is `~/.dicomtool/`.")

	d.H2("7.2  Auto-creation")
	d.P("Both files are created automatically on the first invocation of any `dicomtool` command if they do not already exist. The files are seeded with default content -- the built-in alias table and a sample anonymization profile -- so they are immediately usable and serve as a starting point for customisation.")
	d.P("If the directory `~/.dicomtool/` does not exist it is created at the same time.")

	d.H2("7.3  Custom Config Path")
	d.P("The tag alias file location can be overridden per-invocation with `config:<path>`. This does not affect the profile file location.")
	d.Code("dicomtool modify input:C:\\in output:C:\\out config:D:\\configs\\custom-tags.json set:PatientName=ANON")

	d.H2("7.4  Error Handling")
	d.P("Configuration errors always print to stderr regardless of the `verbose:` setting. If a configuration file exists but contains invalid JSON, an error is printed and the file is treated as empty. Processing continues with no aliases or profiles loaded.")
	d.Code("error: could not load tag aliases from \"...\": invalid character ...\nerror: could not load profiles from \"...\": invalid character ...\nerror: profile \"name\": profile \"name\" not found")

	// 8. Examples
	d.H1("8  Examples")

	d.H2("8.1  Inspecting All Tags in a File")
	d.P("Print every tag and value in a DICOM file:")
	d.Code("dicomtool inspect C:\\scans\\image.dcm all:true")
	d.P("Inspect selected tags using aliases:")
	d.Code("dicomtool inspect input:image.dcm tag:PatientName tag:PatientID tag:StudyDate")
	d.P("Inspect multiple files at once:")
	d.Code("dicomtool inspect input:a.dcm input:b.dcm all:true")

	d.H2("8.2  Replacing a Tag Value")
	d.P("Set the Referring Physician Name (0008,0090) to an empty string:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out set:0008,0090=")
	d.P("Set the Patient Name using a defined alias:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out set:PatientName=SMITH^JOHN")

	d.H2("8.3  Setting Multiple Tags in One Pass")
	d.Code("dicomtool modify input:C:\\study output:C:\\out\n    set:PatientName=ANON\n    set:PatientID=ID001\n    set:AccessionNumber=\n    set:0008,0090=")

	d.H2("8.4  Removing a Specific Tag")
	d.P("Remove the Institution Name tag entirely:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out remove:0008,0080")
	d.P("Remove multiple tags:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out remove:0008,0080 remove:0008,0081 remove:0032,1032")

	d.H2("8.5  Removing All Private Tags")
	d.P("Private tags have an odd group number. The `noprivate:true` flag removes all of them:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out noprivate:true")

	d.H2("8.6  Masking the Date of Birth")
	d.P("The `dob:<mask>` parameter applies a positional replacement to the Patient Date of Birth field (0010,0030). The mask must be exactly 8 characters in `YYYYMMDD` format. A digit character in the mask replaces the corresponding position; any non-digit character leaves the original digit unchanged.")
	d.P("Retain the year, replace month and day with January 1st:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out dob:YYYY0101")
	d.P("Replace the entire date with a fixed value:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out dob:19000101")
	d.P("Retain year and month, replace only the day:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out dob:YYYYMM01")

	d.H2("8.7  Appending a UID Suffix")
	d.P("Append `.9999` to all UID fields. If a UID would exceed 64 characters, the last dot-delimited component is replaced instead of appended:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out uid:9999")
	d.P("Transfer Syntax UIDs (0002,0010) and Referenced Transfer Syntax UIDs (0004,1512) are excluded from modification as they describe the file encoding.")

	d.H2("8.8  Skipping Files by Image Type or Modality")
	d.P("Use `ignoretype:` to skip files whose Image Type tag (0008,0008) contains any of the supplied values, and `ignoremodality:` to skip files whose Modality tag (0008,0060) matches any of the supplied values. Both parameters accept comma-delimited lists and comparisons are case-insensitive.")
	d.P("Skip Secondary Capture files by modality and image type:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out\n    set:PatientName=ANON ignoremodality:SC ignoretype:SECONDARY")
	d.P("Skip presentation state and registration objects:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out ignoremodality:PR,REG")
	d.P("Skipped files are omitted from the output. With `verbose:true` a line is printed for each skipped file:")
	d.Code("skipped (secondary capture): C:\\study\\series1\\screen001.dcm")

	d.H2("8.9  Masking Pixel Rows")
	d.P("The `maskrows:<n>` parameter zeros out the first `n` pixel rows from the top of every image frame. This is useful for removing patient demographics or institution names that are burned into the image pixel data rather than stored as separate DICOM tags.")
	d.P("Zero the top 20 rows of each frame:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out maskrows:20")
	d.P("Combine with other de-identification steps:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out\n    set:PatientName=ANON dob:YYYY0101 noprivate:true\n    ignoremodality:SC ignoretype:SECONDARY maskrows:20 verbose:true")
	d.P("With `verbose:true` a confirmation is printed for each file where rows are successfully zeroed:")
	d.Code("  maskrows: zeroed top 20 row(s) across 1 frame(s)")
	d.P("Note: `maskrows` operates only on uncompressed (native) pixel data. Files with JPEG or JPEG-LS compression are skipped with a warning when `verbose:true` is set:")
	d.Code("  maskrows: pixel data is compressed (encapsulated) -- skipping")

	d.H2("8.10  Full De-identification Without a Profile")
	d.Code("dicomtool modify input:C:\\original output:C:\\deidentified\n    set:PatientName=ANON\n    set:PatientID=ANON001\n    set:AccessionNumber=\n    remove:0008,0080\n    remove:0008,0081\n    remove:0008,0090\n    dob:YYYY0101\n    uid:9999\n    noprivate:true\n    ignoremodality:SC\n    ignoretype:SECONDARY\n    maskrows:20\n    verbose:true")

	d.H2("8.11  Applying a Profile")
	d.Code("dicomtool modify input:C:\\study output:C:\\out profile:anonymize")

	d.H2("8.12  Overriding a Profile Parameter")
	d.P("The `PatientID` value from the profile is replaced by `STUDY42`; all other profile parameters apply unchanged:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out profile:anonymize set:PatientID=STUDY42")

	d.H2("8.13  Generating a DICOMDIR")
	d.P("Generate a DICOMDIR index alongside the modified files:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out profile:anonymize dicomdir:true")
	d.P("A `DICOMDIR` file is written to the root of the output directory after all files have been processed. It is formatted as Explicit VR Little Endian and conforms to PS3.3 of the DICOM standard.")

	d.H2("8.14  Relative Output Path")
	d.P("A relative `output:` path is resolved relative to the `input:` directory. The following two invocations are equivalent when the input is `C:\\study`:")
	d.Code("dicomtool modify input:C:\\study output:deidentified\ndicomtool modify input:C:\\study output:C:\\study\\deidentified")

	d.H2("8.15  Managing Tag Aliases")
	d.Code("# Add aliases\ndicomtool tags add PatientName 0010,0010\ndicomtool tags add InstitutionName 0008,0080\n\n# List all aliases\ndicomtool tags list\n\n# Remove an alias\ndicomtool tags remove InstitutionName")

	d.H2("8.16  Creating and Using a Profile")
	d.Code("# Create a profile\ndicomtool profiles add anonymize\n    set:PatientName=ANON\n    set:PatientID=ANON\n    set:AccessionNumber=\n    dob:YYYY0101\n    noprivate:true\n    maskrows:20\n\n# List all profiles\ndicomtool profiles list\n\n# Inspect the profile definition\ndicomtool profiles show anonymize\n\n# Apply it\ndicomtool modify input:C:\\study output:C:\\out profile:anonymize\n\n# Remove the profile\ndicomtool profiles remove anonymize")

	d.H2("8.17  Using Profile Inheritance")
	d.P("Create a base de-identification profile, then derive a study-specific variant that inherits all base settings but overrides the Patient ID:")
	d.Code("# Base profile\ndicomtool profiles add anonymize\n    set:PatientName=ANON\n    set:PatientID=ANON\n    set:AccessionNumber=\n    dob:YYYY0101\n    noprivate:true\n\n# Derived profile -- inherits everything from anonymize,\n# but assigns a specific Patient ID for this study\ndicomtool profiles add study42 base:anonymize set:PatientID=STUDY0042\n\n# Applying study42 is equivalent to applying anonymize\n# with set:PatientID=STUDY0042 overriding the ANON value\ndicomtool modify input:C:\\study output:C:\\out profile:study42")

	d.H2("8.18  Packaging Output as a ZIP Archive")
	d.P("Use `zip:true` to write all processed DICOM files into a single ZIP archive instead of an output directory. The archive preserves the original folder structure of the input tree.")
	d.Code("dicomtool modify input:C:\\study output:C:\\out\\study.zip zip:true\n    set:PatientName=ANON noprivate:true")
	d.P("If the `output:` path does not end in `.zip`, the extension is appended automatically:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out\\study zip:true set:PatientName=ANON\n# ZIP is written to C:\\out\\study.zip")
	d.P("On completion, the full path to the ZIP file is always printed:")
	d.Code("42 file(s) written to: C:\\out\\study.zip")
	d.P("With `verbose:true`, each entry is listed as it is added:")
	d.Code("  set 0010,0010 = \"ANON\"\nzipped: series1\\CT.1.2.3.dcm\n...")
	d.H3("Notes")
	d.Bullet("`zip:true` and `dicomdir:true` cannot be combined. Use one or the other.")
	d.Bullet("Each ZIP entry carries the creation timestamp of the run, so extracted files have normal filesystem date attributes.")
	d.Bullet("The internal file paths within the ZIP use forward slashes and are relative to the input directory root.")

	d.H2("8.19  Handling Tags with Incorrect Value Representations")
	d.P("Some DICOM files contain tags whose stored Value Representation (VR) does not match the DICOM standard. This can occur when equipment vendors write non-conformant files, or when files have been processed by third-party tools that do not validate VRs. By default, dicomtool will return an error when it tries to write such a file. The `fixvr:` parameter controls how these tags are handled.")
	d.P("Attempt to re-encode each mismatched tag under its correct standard VR. If the re-encoding fails (for example, because the stored bytes cannot be interpreted as the expected type), the tag is removed and reported when `verbose:true` is set:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out fixvr:correct set:PatientName=ANON")
	d.P("Silently remove all tags with an incorrect VR without attempting correction:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out fixvr:skip set:PatientName=ANON")
	d.P("Preserve the file exactly as-is, writing the incorrect VR to the output without raising an error:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out fixvr:passthrough set:PatientName=ANON")
	d.H3("Modes")
	d.Table([]Row{
		{"Mode", "Behaviour"},
		{"`correct`", "Re-encodes the tag value under the standard VR. Supports string, integer, and float conversions. Tags that cannot be converted are removed. Private and unknown tags are kept unchanged."},
		{"`skip`", "Removes any tag whose VR does not match the standard dictionary. Private and unknown tags are kept unchanged."},
		{"`passthrough`", "Disables VR verification on write. Tags are written to the output file exactly as parsed, preserving the incorrect VR."},
	})
	d.H3("Notes")
	d.Bullet("Only tags present in the DICOM standard dictionary are checked. Private tags (odd group numbers) and tags not found in the dictionary are always kept.")
	d.Bullet("`fixvr` applies recursively to all elements, including those nested within sequence (SQ) items at any depth.")
	d.Bullet("With `verbose:true`, each affected tag is reported with its original and target VR.")
	d.Bullet("`fixvr:correct` is the safest option for most non-conformant files. Use `fixvr:passthrough` only when you need to preserve the original encoding exactly.")
	d.Bullet("`fixvr` can be set in a processing profile via the `fixvr` key (see Section 6).")

	d.H2("8.20  Parallel Processing")
	d.P("By default, dicomtool processes files using all available CPU cores simultaneously. For large studies this can significantly reduce total run time compared to serial processing.")
	d.P("Process files using 8 worker goroutines:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out workers:8 set:PatientName=ANON")
	d.P("Use all available CPU cores (default behaviour):")
	d.Code("dicomtool modify input:C:\\study output:C:\\out set:PatientName=ANON")
	d.P("Process files one at a time (serial, equivalent to pre-1.1.1 behaviour):")
	d.Code("dicomtool modify input:C:\\study output:C:\\out workers:1 set:PatientName=ANON")
	d.H3("Notes")
	d.Bullet("Setting `workers:0` is equivalent to omitting the parameter -- the default (CPU core count) is used.")
	d.Bullet("When writing to a ZIP archive (`zip:true`), file processing is parallel but archive writes are serialised internally, so the output ZIP is always valid.")
	d.Bullet("With `verbose:true`, output lines from different workers may be interleaved. The final summary count and any errors are always accurate regardless of worker count.")
	d.Bullet("Setting `workers:` higher than the number of files in the input tree has no effect -- the pool is capped at the job count automatically.")

	d.H2("8.22  Resetting Configuration to Defaults")
	d.P("To restore both `tags.json` and `profiles.json` to their factory defaults, overwriting any existing customisations:")
	d.Code("dicomtool install")
	d.P("Sample output:")
	d.Code("written: C:\\Users\\username\\.dicomtool\\tags.json\nwritten: C:\\Users\\username\\.dicomtool\\profiles.json")

	d.H2("8.23  Verbose Mode")
	d.P("With `verbose:true`, each written file path, per-operation diagnostics, and a summary count are printed to stdout:")
	d.Code("dicomtool modify input:C:\\study output:C:\\out set:PatientName=ANON maskrows:10 verbose:true")
	d.P("Sample output:")
	d.Code("  set 0010,0010 = \"ANON\"\n  maskrows: zeroed top 10 row(s) across 1 frame(s)\nwritten: C:\\out\\series1\\image001.dcm\n3 file(s) processed")
	d.P("Without `verbose:true` only errors and warnings are shown.")

	// 9. Tag Format Reference
	d.H1("9  Tag Format Reference")
	d.H2("9.1  GGGG,EEEE Format")
	d.P("DICOM tags are identified by a group number and an element number, both 16-bit hex values written as `GGGG,EEEE`. Leading zeros may be omitted but both components are required.")
	d.Code("0010,0010   Patient Name\n0010,0020   Patient ID\n0008,0050   Accession Number\n0008,0060   Modality\n0020,000D   Study Instance UID\n0020,000E   Series Instance UID\n0008,0018   SOP Instance UID")

	d.H2("9.2  Commonly Used Tags")
	d.Table([]Row{
		{"Tag", "Name"},
		{"0008,0020", "Study Date"},
		{"0008,0030", "Study Time"},
		{"0008,0050", "Accession Number"},
		{"0008,0060", "Modality"},
		{"0008,0070", "Manufacturer"},
		{"0008,0080", "Institution Name"},
		{"0008,0081", "Institution Address"},
		{"0008,0090", "Referring Physician Name"},
		{"0008,103E", "Series Description"},
		{"0008,1030", "Study Description"},
		{"0010,0010", "Patient Name"},
		{"0010,0020", "Patient ID"},
		{"0010,0030", "Patient Birth Date"},
		{"0010,0040", "Patient Sex"},
		{"0010,1010", "Patient Age"},
		{"0018,1030", "Protocol Name"},
		{"0020,000D", "Study Instance UID"},
		{"0020,000E", "Series Instance UID"},
		{"0020,0010", "Study ID"},
		{"0020,0011", "Series Number"},
		{"0020,0013", "Instance Number"},
	})

	d.H2("9.3  Private Tags")
	d.P("Tags whose group number is odd (e.g. `0009,xxxx`, `0019,xxxx`) are private tags used by specific vendors or applications. They are not standardised. The `noprivate:true` flag removes all such tags from every output file.")

	d.H2("9.4  Transfer Syntax and UID Exclusions")
	d.P("The following UID tags are excluded from the `uid:<suffix>` operation because they describe the encoding of the file itself and must remain valid, recognised values:")
	d.Table([]Row{
		{"Tag", "Name"},
		{"0002,0010", "Transfer Syntax UID"},
		{"0004,1512", "Referenced Transfer Syntax UID in File"},
	})

	d.H2("9.5  Filtering by Modality and Image Type")
	d.P("Files can be excluded from processing based on their Modality (0008,0060) or Image Type (0008,0008) tags using the `ignoremodality:` and `ignoretype:` parameters. Both accept comma-delimited lists and perform case-insensitive comparisons.")
	d.P("Common use cases:")
	d.Table([]Row{
		{"Example", "Effect"},
		{"`ignoremodality:SC`", "Skip Secondary Capture files (screen captures, annotation overlays)"},
		{"`ignoremodality:PR`", "Skip Presentation State objects"},
		{"`ignoremodality:SC,PR,REG`", "Skip multiple modality types in one pass"},
		{"`ignoretype:SECONDARY`", "Skip files with SECONDARY in their Image Type field"},
		{"`ignoretype:DERIVED,SECONDARY`", "Skip derived or secondary image types"},
	})

	// 10. Output and Error Behaviour
	d.H1("10  Output and Error Behaviour")
	d.Table([]Row{
		{"Condition", "Behaviour"},
		{"Non-DICOM file encountered in input tree", "Silently skipped"},
		{"File matches `ignoremodality:` or `ignoretype:` value", "Skipped and not written to output (logged with `verbose:true`)"},
		{"Output directory does not exist", "Created automatically (including intermediate directories)"},
		{"Tag not present in source file", "For `set:` operations the tag is inserted; for `remove:` it is a no-op"},
		{"Relative `output:` path", "Resolved relative to the `input:` directory"},
		{"`tags.json` has invalid JSON", "Error printed to stderr; aliases treated as empty"},
		{"`profiles.json` has invalid JSON", "Error printed to stderr; profiles treated as empty"},
		{"Named profile not found", "Error printed to stderr; profile is not applied"},
		{"Base profile not found during inheritance resolution", "Error printed to stderr; profile is not applied"},
		{"Circular base reference in profile chain", "Error printed to stderr; profile is not applied"},
		{"`maskrows:` on compressed pixel data", "Frame skipped; warning printed when `verbose:true`"},
		{"`inspect` file parse error", "Error printed per file; remaining files continue to be processed"},
		{"`inspect` tag not found in file", "`not found` printed for that tag; remaining tags continue"},
		{"`install` config directory does not exist", "Directory created automatically before writing files"},
		{"`zip:true` with `dicomdir:true`", "Error returned; the two options cannot be combined"},
		{"`zip:true` output path has no `.zip` extension", "`.zip` is appended automatically"},
	})

	// Appendix A — Default Configuration Files
	d.PageBreak()
	d.H1("Appendix A  Default Configuration Files")
	d.P("The following files are written to `~/.dicomtool/` on the first run of any `dicomtool` command, and can be restored at any time with `dicomtool install`.")

	d.H2("A.1  tags.json")
	d.P("Maps short alias names to DICOM tag identifiers. Any alias defined here can be used in place of a raw `GGGG,EEEE` identifier on the command line or in a profile.")
	d.Code("{\n  \"TransferSyntaxUID\":          \"0002,0010\",\n  \"ReferencedTransferSyntaxUID\": \"0004,1512\",\n  \"StudyDate\":                  \"0008,0020\",\n  \"StudyTime\":                  \"0008,0030\",\n  \"AccessionNumber\":            \"0008,0050\",\n  \"Modality\":                   \"0008,0060\",\n  \"Manufacturer\":               \"0008,0070\",\n  \"InstitutionName\":            \"0008,0080\",\n  \"InstitutionAddress\":         \"0008,0081\",\n  \"ReferringPhysicianName\":     \"0008,0090\",\n  \"SeriesDescription\":          \"0008,103E\",\n  \"StudyDescription\":           \"0008,1030\",\n  \"PatientName\":                \"0010,0010\",\n  \"PatientID\":                  \"0010,0020\",\n  \"PatientDOB\":                 \"0010,0030\",\n  \"PatientSex\":                 \"0010,0040\",\n  \"PatientAge\":                 \"0010,1010\",\n  \"ProtocolName\":               \"0018,1030\",\n  \"StudyInstanceUID\":           \"0020,000D\",\n  \"SeriesInstanceUID\":          \"0020,000E\",\n  \"StudyID\":                    \"0020,0010\",\n  \"SeriesNumber\":               \"0020,0011\",\n  \"InstanceNumber\":             \"0020,0013\",\n  \"ConfidentialityCode\":        \"0040,1008\"\n}")

	d.H2("A.2  profiles.json")
	d.P("Defines two built-in profiles. `base-anon` is a comprehensive de-identification baseline: it sets patient identity fields to anonymous values, masks the date of birth to year only, removes private tags, and removes tags commonly associated with identifying information. `anon-img` derives from `base-anon` and additionally skips Secondary Capture, Overlay, Presentation State, and Structured Report objects.")
	d.Code("{\n  \"base-anon\": {\n    \"set\": [\n      \"PatientName=ANON\",\n      \"PatientID=ANON\",\n      \"AccessionNumber=\",\n      \"ConfidentialityCode=Y\"\n    ],\n    \"remove\": [\n      \"8,80\",   \"8,81\",   \"8,90\",   \"8,92\",   \"8,94\",\n      \"8,1010\", \"8,1032\", \"8,1040\", \"8,1048\", \"8,1050\",\n      \"8,1060\", \"8,1070\", \"8,1080\", \"8,1100\", \"8,1110\",\n      \"10,21\",  \"10,22\",  \"10,24\",\n      \"10,1000\",\"10,1001\",\"10,1002\",\"10,1005\",\"10,1010\",\n      \"10,1040\",\"10,1050\",\"10,1060\",\"10,1080\",\"10,1090\",\n      \"10,2000\",\"10,2110\",\"10,2150\",\"10,2152\",\"10,2154\",\n      \"10,2155\",\"10,2297\",\"10,2298\",\"10,2299\",\n      \"10,3020\",\"10,4000\",\"10,21B0\",\"10,21F0\",\n      \"18,1400\",\"18,1401\",\n      \"20,4000\",\n      \"28,300\",\n      \"32,1031\",\"32,1032\",\"32,1033\",\n      \"38,10\",  \"38,300\", \"38,400\", \"38,500\", \"38,4000\",\n      \"40,275\", \"40,1001\",\"40,1002\",\"40,1004\",\"40,1005\",\n      \"40,2008\",\"40,2009\",\"40,2010\",\"40,2011\",\"40,2016\",\n      \"40,2017\",\"40,2400\",\"40,4006\",\"40,4009\",\"40,4010\",\n      \"40,4020\",\"40,4021\",\"40,A057\",\"40,A060\",\"40,A066\",\n      \"40,A067\",\"40,A068\",\"40,A070\",\"40,A073\",\"40,A075\",\n      \"40,A078\",\"40,A123\",\"40,A160\",\"40,A730\",\n      \"50,10\",\n      \"70,1\",   \"70,2\",   \"70,3\",   \"70,4\",   \"70,5\",\n      \"70,6\",   \"70,8\",   \"70,9\",   \"70,10\",  \"70,11\",\n      \"70,12\",  \"70,13\",  \"70,14\",  \"70,80\",  \"70,81\",\n      \"70,82\",  \"70,83\",  \"70,84\",  \"70,207\", \"70,208\",\n      \"70,209\", \"70,303\",\n      \"72,2\",   \"72,4\",   \"72,6\",   \"72,8\",   \"72,A\",\n      \"72,C\",   \"72,E\",   \"72,10\",\n      \"400,100\",\"400,105\",\"400,110\",\"400,115\",\"400,120\",\n      \"400,402\",\"400,403\",\"400,404\",\"400,561\",\"400,562\",\n      \"400,563\",\"400,564\",\"400,565\",\n      \"2030,20\",\n      \"2110,10\",\"2110,20\",\"2110,30\",\n      \"2200,1\", \"2200,2\"\n    ],\n    \"dob\":       \"YYYY0101\",\n    \"noprivate\": true,\n    \"fixvr\":     \"correct\"\n  },\n  \"anon-img\": {\n    \"base\":           \"base-anon\",\n    \"ignoretype\":     [\"SECONDARY\"],\n    \"ignoremodality\": [\"SC\", \"OT\", \"PR\", \"SR\"]\n  }\n}")
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	d := &Doc{}
	buildContent(d)

	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` +
		d.b.String() +
		`<w:sectPr>` +
		`<w:pgSz w:w="12240" w:h="15840"/>` +
		`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"` +
		` w:header="720" w:footer="720" w:gutter="0"/>` +
		`</w:sectPr>` +
		`</w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string) {
		f, err := zw.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := fmt.Fprint(f, content); err != nil {
			panic(err)
		}
	}
	add("[Content_Types].xml", contentTypes)
	add("_rels/.rels", rootRels)
	add("word/_rels/document.xml.rels", wordRels)
	add("word/document.xml", docXML)
	add("word/styles.xml", stylesXML)
	add("word/numbering.xml", numbering)
	add("word/settings.xml", settings)
	add("docProps/core.xml", coreProps)
	if err := zw.Close(); err != nil {
		panic(err)
	}
	out := "dicomtool-manual.docx"
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("written:", out)
}
