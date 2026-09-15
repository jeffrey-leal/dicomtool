package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/tag"
)

// --- test helpers -------------------------------------------------------------

var (
	tagPatientName = tag.Tag{Group: 0x0010, Element: 0x0010}
	tagPatientID   = tag.Tag{Group: 0x0010, Element: 0x0020}
	tagAccession   = tag.Tag{Group: 0x0008, Element: 0x0050}
	tagRefImageSeq = tag.Tag{Group: 0x0008, Element: 0x1140} // ReferencedImageSequence (even group)
	tagPrivate     = tag.Tag{Group: 0x0009, Element: 0x0010} // odd group → private
	tagSOPInstance = tag.Tag{Group: 0x0008, Element: 0x0018} // SOPInstanceUID
	tagSOPClass    = tag.Tag{Group: 0x0008, Element: 0x0016} // SOPClassUID
	tagStudyUID    = tag.Tag{Group: 0x0020, Element: 0x000D} // StudyInstanceUID
	tagRefSOPInst  = tag.Tag{Group: 0x0008, Element: 0x1155} // ReferencedSOPInstanceUID
)

// uidElem builds a single-valued UI (UID) element with an explicit VR.
func uidElem(tg tag.Tag, val string) *dicom.Element {
	return mustElemRaw(tg, "UI", []string{val})
}

// seqElem builds a sequence element whose single item contains the given elements.
func seqElem(t *testing.T, seqTag tag.Tag, item []*dicom.Element) *dicom.Element {
	t.Helper()
	e, err := dicom.NewElement(seqTag, [][]*dicom.Element{item})
	if err != nil {
		t.Fatalf("building sequence element %v: %v", seqTag, err)
	}
	return e
}

// strElem builds a single-valued string element.
func strElem(t *testing.T, tg tag.Tag, val string) *dicom.Element {
	t.Helper()
	e, err := dicom.NewElement(tg, []string{val})
	if err != nil {
		t.Fatalf("building string element %v: %v", tg, err)
	}
	return e
}

// privElem builds a private (odd-group) element with an explicit VR, since
// private tags are not present in the library's tag dictionary.
func privElem(tg tag.Tag, val string) *dicom.Element {
	return mustElemRaw(tg, "LO", []string{val})
}

// nestedItems extracts the per-item element slices from a sequence element.
func nestedItems(t *testing.T, seq *dicom.Element) [][]*dicom.Element {
	t.Helper()
	items, ok := seq.Value.GetValue().([]*dicom.SequenceItemValue)
	if !ok {
		t.Fatalf("element %v is not a sequence", seq.Tag)
	}
	out := make([][]*dicom.Element, 0, len(items))
	for _, it := range items {
		elems, ok := it.GetValue().([]*dicom.Element)
		if !ok {
			t.Fatalf("sequence item is not []*Element")
		}
		out = append(out, elems)
	}
	return out
}

func findTag(elems []*dicom.Element, tg tag.Tag) *dicom.Element {
	for _, e := range elems {
		if e.Tag == tg {
			return e
		}
	}
	return nil
}

func strValue(t *testing.T, e *dicom.Element) string {
	t.Helper()
	if e == nil {
		return ""
	}
	v, ok := e.Value.GetValue().([]string)
	if !ok || len(v) == 0 {
		return ""
	}
	return v[0]
}

func setOf(tags ...tag.Tag) map[tag.Tag]struct{} {
	m := make(map[tag.Tag]struct{}, len(tags))
	for _, tg := range tags {
		m[tg] = struct{}{}
	}
	return m
}

// --- pruneElements: remove recursion -----------------------------------------

func TestPruneElements_RemoveRecursesIntoSequence(t *testing.T) {
	nested := []*dicom.Element{
		strElem(t, tagPatientName, "NESTED"),
		strElem(t, tagAccession, "ACC123"),
	}
	elems := []*dicom.Element{
		strElem(t, tagPatientID, "ID1"),
		seqElem(t, tagRefImageSeq, nested),
	}

	out := pruneElements(elems, setOf(tagPatientName), false)

	// Top-level PatientID and the (non-removed) sequence survive.
	if findTag(out, tagPatientID) == nil {
		t.Fatal("top-level PatientID was unexpectedly removed")
	}
	seq := findTag(out, tagRefImageSeq)
	if seq == nil {
		t.Fatal("sequence element was unexpectedly removed")
	}
	// The nested PatientName is gone; the nested AccessionNumber remains.
	items := nestedItems(t, seq)
	if len(items) != 1 {
		t.Fatalf("expected 1 sequence item, got %d", len(items))
	}
	if findTag(items[0], tagPatientName) != nil {
		t.Fatal("nested PatientName survived removal — recursion failed")
	}
	if findTag(items[0], tagAccession) == nil {
		t.Fatal("nested AccessionNumber was incorrectly removed")
	}
}

// --- pruneElements: noprivate recursion ---------------------------------------

func TestPruneElements_NoPrivateRecursesIntoSequence(t *testing.T) {
	nested := []*dicom.Element{
		strElem(t, tagAccession, "ACC123"),
		privElem(tagPrivate, "PRIV"),
	}
	elems := []*dicom.Element{
		privElem(tagPrivate, "TOPLEVELPRIV"),
		strElem(t, tagPatientID, "ID1"),
		seqElem(t, tagRefImageSeq, nested),
	}

	out := pruneElements(elems, setOf(), true)

	// Top-level private element removed; even-group element kept.
	if findTag(out, tagPrivate) != nil {
		t.Fatal("top-level private tag survived noprivate")
	}
	if findTag(out, tagPatientID) == nil {
		t.Fatal("top-level even-group tag was incorrectly removed")
	}
	seq := findTag(out, tagRefImageSeq)
	if seq == nil {
		t.Fatal("sequence element was unexpectedly removed")
	}
	items := nestedItems(t, seq)
	if findTag(items[0], tagPrivate) != nil {
		t.Fatal("nested private tag survived noprivate — recursion failed")
	}
	if findTag(items[0], tagAccession) == nil {
		t.Fatal("nested even-group tag was incorrectly removed")
	}
}

// --- replaceInElements: set recursion -----------------------------------------

func TestReplaceInElements_RecursesAndReplacesAll(t *testing.T) {
	nested := []*dicom.Element{
		strElem(t, tagPatientName, "NESTED"),
	}
	elems := []*dicom.Element{
		strElem(t, tagPatientName, "ORIGINAL"),
		seqElem(t, tagRefImageSeq, nested),
	}

	newElem := strElem(t, tagPatientName, "ANON")
	if !replaceInElements(elems, newElem) {
		t.Fatal("replaceInElements returned false despite existing occurrences")
	}

	if got := strValue(t, findTag(elems, tagPatientName)); got != "ANON" {
		t.Fatalf("top-level PatientName = %q, want ANON", got)
	}
	items := nestedItems(t, findTag(elems, tagRefImageSeq))
	if got := strValue(t, findTag(items[0], tagPatientName)); got != "ANON" {
		t.Fatalf("nested PatientName = %q, want ANON — recursion failed", got)
	}
}

func TestReplaceInElements_AbsentTagReturnsFalseAndDoesNotInsert(t *testing.T) {
	nested := []*dicom.Element{
		strElem(t, tagPatientName, "NESTED"),
	}
	elems := []*dicom.Element{
		strElem(t, tagPatientID, "ID1"),
		seqElem(t, tagRefImageSeq, nested),
	}

	newElem := strElem(t, tagAccession, "ACC")
	if replaceInElements(elems, newElem) {
		t.Fatal("replaceInElements returned true for an absent tag")
	}
	// The absent tag must not have been injected into the sequence item.
	items := nestedItems(t, findTag(elems, tagRefImageSeq))
	if findTag(items[0], tagAccession) != nil {
		t.Fatal("absent tag was incorrectly inserted into a sequence item")
	}
}

// --- pruneElements: flat dataset parity (regression) --------------------------

func TestPruneElements_FlatDatasetParity(t *testing.T) {
	elems := []*dicom.Element{
		strElem(t, tagPatientName, "NAME"),
		strElem(t, tagPatientID, "ID1"),
		privElem(tagPrivate, "PRIV"),
		strElem(t, tagAccession, "ACC"),
	}

	out := pruneElements(elems, setOf(tagPatientName), true)

	if findTag(out, tagPatientName) != nil {
		t.Fatal("PatientName should have been removed")
	}
	if findTag(out, tagPrivate) != nil {
		t.Fatal("private tag should have been removed")
	}
	if findTag(out, tagPatientID) == nil || findTag(out, tagAccession) == nil {
		t.Fatal("non-targeted tags should have survived")
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 surviving elements, got %d", len(out))
	}
}

// --- uidRemapper / applyUIDRemap ----------------------------------------------

func TestUIDRemapper_RepeatableMapping(t *testing.T) {
	r := newUIDRemapper()
	a := r.mapUID("1.2.3.4")
	b := r.mapUID("1.2.3.4")
	if a != b {
		t.Fatalf("same input mapped to different UIDs: %q vs %q", a, b)
	}
	if a == "1.2.3.4" {
		t.Fatal("UID was not remapped")
	}
	if c := r.mapUID("9.9.9.9"); c == a {
		t.Fatal("distinct inputs mapped to the same UID")
	}
}

func TestApplyUIDRemap_ConsistentAndReferentiallyIntact(t *testing.T) {
	const instUID = "1.3.6.1.4.999.1"
	// A referenced-image sequence whose ReferencedSOPInstanceUID points at the
	// same instance as the top-level SOPInstanceUID.
	nested := []*dicom.Element{
		uidElem(tagSOPClass, "1.2.840.10008.5.1.4.1.1.2"), // standard SOP class — must be kept
		uidElem(tagRefSOPInst, instUID),                   // reference to the instance
	}
	elems := []*dicom.Element{
		uidElem(tag.TransferSyntaxUID, "1.2.840.10008.1.2.1"),              // standard — kept
		uidElem(tag.ImplementationClassUID, "1.2.804.114118.3"),           // structural — kept
		uidElem(tagSOPClass, "1.2.840.10008.5.1.4.1.1.2"),                 // standard — kept
		uidElem(tagStudyUID, "1.3.6.1.4.999.7"),                           // site UID — remapped
		uidElem(tagSOPInstance, instUID),                                  // site UID — remapped
		seqElem(t, tagRefImageSeq, nested),
	}

	r := newUIDRemapper()
	applied := make(map[string]string)
	applyUIDRemap(elems, r, applied)

	// Standard / structural UIDs unchanged.
	if got := strValue(t, findTag(elems, tag.TransferSyntaxUID)); got != "1.2.840.10008.1.2.1" {
		t.Fatalf("TransferSyntaxUID was remapped: %q", got)
	}
	if got := strValue(t, findTag(elems, tag.ImplementationClassUID)); got != "1.2.804.114118.3" {
		t.Fatalf("ImplementationClassUID was remapped: %q", got)
	}
	if got := strValue(t, findTag(elems, tagSOPClass)); got != "1.2.840.10008.5.1.4.1.1.2" {
		t.Fatalf("SOPClassUID was remapped: %q", got)
	}

	// Site UIDs remapped.
	newInst := strValue(t, findTag(elems, tagSOPInstance))
	if newInst == instUID || newInst == "" {
		t.Fatalf("SOPInstanceUID not remapped: %q", newInst)
	}
	if newStudy := strValue(t, findTag(elems, tagStudyUID)); newStudy == "1.3.6.1.4.999.7" || newStudy == newInst {
		t.Fatalf("StudyInstanceUID remap invalid: %q", newStudy)
	}

	// Referential integrity: the nested reference now equals the remapped instance.
	items := nestedItems(t, findTag(elems, tagRefImageSeq))
	if got := strValue(t, findTag(items[0], tagRefSOPInst)); got != newInst {
		t.Fatalf("nested ReferencedSOPInstanceUID = %q, want %q (reference broken)", got, newInst)
	}
	// Nested standard SOP class still preserved.
	if got := strValue(t, findTag(items[0], tagSOPClass)); got != "1.2.840.10008.5.1.4.1.1.2" {
		t.Fatalf("nested SOPClassUID was remapped: %q", got)
	}

	// Exactly the replacements made are recorded — they drive output path
	// renaming, so a kept standard or structural UID must not appear.
	newStudy := strValue(t, findTag(elems, tagStudyUID))
	if len(applied) != 2 || applied[instUID] != newInst || applied["1.3.6.1.4.999.7"] != newStudy {
		t.Fatalf("applied = %v, want only the study and instance UIDs mapped to their replacements", applied)
	}
}

func TestShiftDateString(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		shiftDays int
		want      string
		wantOK    bool
	}{
		{"positive shift", "20200115", 10, "20200125", true},
		{"negative shift", "20200115", -10, "20200105", true},
		{"month rollover", "20200130", 5, "20200204", true},
		{"year rollover", "20201228", 10, "20210107", true},
		{"leap year Feb 29 plus one", "20200229", 1, "20200301", true},
		{"zero shift is a no-op", "20200115", 0, "20200115", true},
		{"DT value keeps time/fraction/zone suffix", "20200115120000.000000+0000", -1, "20200114120000.000000+0000", true},
		{"too short", "2020011", 1, "2020011", false},
		{"empty", "", 1, "", false},
		{"non-numeric date portion", "2020AB15", 1, "2020AB15", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := shiftDateString(tc.in, tc.shiftDays)
			if ok != tc.wantOK {
				t.Fatalf("shiftDateString(%q, %d) ok = %v, want %v", tc.in, tc.shiftDays, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("shiftDateString(%q, %d) = %q, want %q", tc.in, tc.shiftDays, got, tc.want)
			}
		})
	}
}

func TestApplyDateShift_RecursesAndSkipsPatientBirthDate(t *testing.T) {
	nested := []*dicom.Element{
		strElem(t, tag.StudyDate, "20200115"), // DA, nested — must shift too
	}
	elems := []*dicom.Element{
		strElem(t, tag.StudyDate, "20200115"),           // DA — shift
		strElem(t, tag.AcquisitionDateTime, "20200115120000.000000+0000"), // DT — shift date, keep time
		strElem(t, tag.PatientBirthDate, "19800101"),    // DA — must NOT shift
		strElem(t, tagPatientName, "Doe^Jane"),          // non-date — must NOT change
		seqElem(t, tagRefImageSeq, nested),
	}

	applyDateShift(elems, 10)

	if got := strValue(t, findTag(elems, tag.StudyDate)); got != "20200125" {
		t.Fatalf("StudyDate = %q, want %q", got, "20200125")
	}
	if got := strValue(t, findTag(elems, tag.AcquisitionDateTime)); got != "20200125120000.000000+0000" {
		t.Fatalf("AcquisitionDateTime = %q, want date shifted with time preserved", got)
	}
	if got := strValue(t, findTag(elems, tag.PatientBirthDate)); got != "19800101" {
		t.Fatalf("PatientBirthDate was shifted: %q, want unchanged", got)
	}
	if got := strValue(t, findTag(elems, tagPatientName)); got != "Doe^Jane" {
		t.Fatalf("PatientName was changed: %q", got)
	}

	items := nestedItems(t, findTag(elems, tagRefImageSeq))
	if got := strValue(t, findTag(items[0], tag.StudyDate)); got != "20200125" {
		t.Fatalf("nested StudyDate = %q, want %q (recursion into sequence failed)", got, "20200125")
	}
}

func TestUIDRemapper_ConcurrentMapUID(t *testing.T) {
	r := newUIDRemapper()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.mapUID("1.2.3.4") // same key hammered concurrently
				_ = r.mapUID("5.6.7.8")
			}
		}()
	}
	wg.Wait()
	// All callers must observe the single cached value for a given input.
	if a, b := r.mapUID("1.2.3.4"), r.mapUID("1.2.3.4"); a != b {
		t.Fatalf("inconsistent mapping after concurrent access: %q vs %q", a, b)
	}
}

// --- remapRelPath -------------------------------------------------------------

func TestRemapRelPath(t *testing.T) {
	remapped := map[string]string{
		"1.2.3":   "2.25.100", // study
		"1.2.3.4": "2.25.200", // instance, prefixed by the study UID
		"9.8.7":   "2.25.300", // series
	}
	tests := []struct{ name, rel, want string }{
		{"file named after its UID", "1.2.3.4.dcm", "2.25.200.dcm"},
		{"extensionless UID file", "1.2.3.4", "2.25.200"},
		{"UID embedded in a longer name", "CT.1.2.3.4.dcm", "CT.2.25.200.dcm"},
		{"UID followed by an instance number", "9.8.7.12.dcm", "2.25.300.12.dcm"},
		{"folders named after UIDs", filepath.Join("1.2.3", "9.8.7", "IM0001"), filepath.Join("2.25.100", "2.25.300", "IM0001")},
		{"partial component is not a match", "1.2.34.dcm", "1.2.34.dcm"},
		{"name without a UID", filepath.Join("study", "IM0001.dcm"), filepath.Join("study", "IM0001.dcm")},
		{"UID the file did not carry", "5.5.5.dcm", "5.5.5.dcm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remapRelPath(tt.rel, remapped); got != tt.want {
				t.Fatalf("remapRelPath(%q) = %q, want %q", tt.rel, got, tt.want)
			}
		})
	}
	if got := remapRelPath("1.2.3.4.dcm", nil); got != "1.2.3.4.dcm" {
		t.Fatalf("remapRelPath with no remap = %q, want the path unchanged", got)
	}
}

// --- runModify end to end -----------------------------------------------------

// writeTestDICOM writes a minimal Explicit VR Little Endian CT file at path.
func writeTestDICOM(t *testing.T, path, studyUID, seriesUID, sopUID string) {
	t.Helper()
	const ctImageStorage = "1.2.840.10008.5.1.4.1.1.2"
	ds := dicom.Dataset{Elements: []*dicom.Element{
		uidElem(tag.MediaStorageSOPClassUID, ctImageStorage),
		uidElem(tag.MediaStorageSOPInstanceUID, sopUID),
		uidElem(tag.TransferSyntaxUID, "1.2.840.10008.1.2.1"),
		uidElem(tagSOPClass, ctImageStorage),
		uidElem(tagSOPInstance, sopUID),
		strElem(t, tag.Modality, "CT"),
		strElem(t, tagPatientName, "TEST^PATIENT"),
		strElem(t, tagPatientID, "PID1"),
		uidElem(tagStudyUID, studyUID),
		uidElem(tag.SeriesInstanceUID, seriesUID),
	}}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := dicom.Write(f, ds); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// runModifyWith runs the modify command over in → out with params as the
// parsed command line, restoring the shared parse state afterwards.
func runModifyWith(t *testing.T, in, out string, params map[string][]string) error {
	t.Helper()
	t.Cleanup(func() { parsed, Opts = nil, Options{} })
	parsed = params
	Opts = Options{Inputs: []string{in}, Output: out}
	return runModify()
}

func TestRunModify_RemapUIDsRenamesPaths(t *testing.T) {
	// Every source UID starts with root; a generated 2.25.<digits> UID cannot
	// contain it, so its absence from a path proves no original UID survived.
	const root = "1.3.6.1.4.1.99999"
	study, series := root+".1", root+".1.2" // the study UID prefixes the series UID
	in := t.TempDir()
	sourceNames := []string{
		series + ".3.1.dcm",
		series + ".3.2",
		"CT." + series + ".3.3.dcm",
		"IM0004",
	}
	for i, name := range sourceNames {
		writeTestDICOM(t, filepath.Join(in, study, series, name), study, series, fmt.Sprintf("%s.3.%d", series, i+1))
	}

	t.Run("directory with DICOMDIR", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		if err := runModifyWith(t, in, out, map[string][]string{"remapuids": {"true"}, "dicomdir": {"true"}}); err != nil {
			t.Fatalf("runModify: %v", err)
		}
		kinds := map[string]bool{}
		err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() == "DICOMDIR" {
				return err
			}
			rel, _ := filepath.Rel(out, path)
			if strings.Contains(rel, root) {
				t.Errorf("output path %q still carries an original UID", rel)
			}
			ds, perr := dicom.ParseFile(path, nil)
			if perr != nil {
				t.Fatalf("parsing %s: %v", rel, perr)
			}
			get := func(tg tag.Tag) string { e, _ := ds.FindElementByTag(tg); return strValue(t, e) }
			sop := get(tagSOPInstance)
			// The folders carry this file's own remapped study and series UIDs.
			if want := filepath.Join(get(tagStudyUID), get(tag.SeriesInstanceUID)); filepath.Dir(rel) != want {
				t.Errorf("%q is in folder %q, want %q", rel, filepath.Dir(rel), want)
			}
			switch filepath.Base(rel) {
			case sop + ".dcm":
				kinds["uid.dcm"] = true
			case sop:
				kinds["uid"] = true
			case "CT." + sop + ".dcm":
				kinds["CT.uid.dcm"] = true
			case "IM0004":
				kinds["IM0004"] = true
			default:
				t.Errorf("unexpected output file %q (its SOP Instance UID is %s)", rel, sop)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(kinds) != len(sourceNames) {
			t.Fatalf("output names matched %v, want one file of each source naming", kinds)
		}

		dd, err := dicom.ParseFile(filepath.Join(out, "DICOMDIR"), nil)
		if err != nil {
			t.Fatalf("parsing DICOMDIR: %v", err)
		}
		records, err := dd.FindElementByTag(tag.DirectoryRecordSequence)
		if err != nil {
			t.Fatalf("DICOMDIR has no directory records: %v", err)
		}
		images := 0
		for _, item := range nestedItems(t, records) {
			e := findTag(item, tag.ReferencedFileID)
			if e == nil {
				continue
			}
			images++
			comps, _ := e.Value.GetValue().([]string)
			ref := filepath.Join(comps...)
			if strings.Contains(ref, root) {
				t.Errorf("DICOMDIR references %q, which still carries an original UID", ref)
			}
			if _, serr := os.Stat(filepath.Join(out, ref)); serr != nil {
				t.Errorf("DICOMDIR references %q, which does not exist: %v", ref, serr)
			}
		}
		if images != len(sourceNames) {
			t.Fatalf("DICOMDIR references %d files, want %d", images, len(sourceNames))
		}
	})

	t.Run("zip", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out.zip")
		if err := runModifyWith(t, in, out, map[string][]string{"remapuids": {"true"}, "zip": {"true"}}); err != nil {
			t.Fatalf("runModify: %v", err)
		}
		zr, err := zip.OpenReader(out)
		if err != nil {
			t.Fatal(err)
		}
		defer zr.Close()
		if len(zr.File) != len(sourceNames) {
			t.Fatalf("zip has %d entries, want %d", len(zr.File), len(sourceNames))
		}
		dirs := map[string]bool{}
		for _, f := range zr.File {
			if strings.Contains(f.Name, root) {
				t.Errorf("zip entry %q still carries an original UID", f.Name)
			}
			dirs[path.Dir(f.Name)] = true
		}
		if len(dirs) != 1 {
			t.Fatalf("zip entries span folders %v, want the one renamed series folder", dirs)
		}
	})
}

func TestRunModify_RefusesUIDSuffix(t *testing.T) {
	in := t.TempDir()
	tests := []struct {
		name  string
		setup func(t *testing.T)
		want  string
	}{
		{"command line", func(t *testing.T) {
			parsed["uid"] = []string{"9999"}
		}, "use remapuids:true"},
		{"profile, inherited from its base", func(t *testing.T) {
			cfg := ProfileConfig{
				"legacy": {UIDSuffix: "9999"},
				"child":  {Base: "legacy", Priv: true},
			}
			p, err := resolveProfile("child", cfg)
			if err != nil {
				t.Fatal(err)
			}
			parsed["profile"] = []string{"child"}
			mergeProfile(p)
		}, `profile "child" carries a "uid" entry`},
		{"per-modality entry", func(t *testing.T) {
			parsed["profile"] = []string{"mixed"}
			mergeProfile(Profile{PerModality: map[string]Profile{"ct": {UIDSuffix: "5"}}})
		}, "per-modality entry CT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			t.Cleanup(func() { parsed, Opts = nil, Options{} })
			parsed = map[string][]string{"remapuids": {"true"}}
			Opts = Options{Inputs: []string{in}, Output: out}
			tt.setup(t)
			err := runModify()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("runModify error = %v, want one containing %q", err, tt.want)
			}
			if _, serr := os.Stat(out); !os.IsNotExist(serr) {
				t.Fatalf("a refused run created output %q", out)
			}
		})
	}
}

// --- findSequenceItemPositions ------------------------------------------------

// bruteForceItemPositions is the reference per-byte scan the optimized
// findSequenceItemPositions must match exactly.
func bruteForceItemPositions(data []byte) []int {
	marker := []byte{0xFE, 0xFF, 0x00, 0xE0}
	var positions []int
	for i := 0; i <= len(data)-4; i++ {
		if bytes.Equal(data[i:i+4], marker) {
			positions = append(positions, i)
		}
	}
	return positions
}

func TestFindSequenceItemPositions(t *testing.T) {
	m := []byte{0xFE, 0xFF, 0x00, 0xE0}
	cases := map[string][]byte{
		"empty":       {},
		"too short":   {0xFE, 0xFF, 0x00},
		"none":        {0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		"single":      append([]byte{0x00, 0x11}, m...),
		"at start":    append(append([]byte{}, m...), 0x01, 0x02),
		"at end":      append([]byte{0x09, 0x08}, m...),
		"back to back": append(append([]byte{}, m...), m...),
		"three apart": bytes.Join([][]byte{m, {0xAA, 0xBB}, m, {0xCC}, m}, nil),
		"near miss":   {0xFE, 0xFF, 0x00, 0xE1, 0xFE, 0xFF, 0x00, 0xE0},
	}
	for name, data := range cases {
		got := findSequenceItemPositions(data)
		want := bruteForceItemPositions(data)
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", name, got, want)
			}
		}
	}
}

// --- writeErrorLog ------------------------------------------------------------

// sampleFailures includes a comma and quote so CSV/JSON escaping is exercised.
func sampleFailures() []fileFailure {
	return []fileFailure{
		{File: `C:\in\a.dcm`, Error: "process: parse: unexpected EOF"},
		{File: `C:\in\b.dcm`, Error: `open: permission denied, "x"`},
	}
}

func TestWriteErrorLog_JSON(t *testing.T) {
	dir := t.TempDir()
	failures := sampleFailures()
	path, err := writeErrorLog(dir, "json", 5, len(failures), failures)
	if err != nil {
		t.Fatalf("writeErrorLog: %v", err)
	}
	if filepath.Base(path) != "ERROR.json" {
		t.Fatalf("unexpected file name: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var report struct {
		Processed int           `json:"processed"`
		Failed    int           `json:"failed"`
		Errors    []fileFailure `json:"errors"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("json round-trip: %v", err)
	}
	if report.Processed != 5 || report.Failed != 2 || len(report.Errors) != 2 {
		t.Fatalf("envelope mismatch: %+v", report)
	}
	if report.Errors[1].File != `C:\in\b.dcm` || report.Errors[1].Error != `open: permission denied, "x"` {
		t.Fatalf("entry mismatch: %+v", report.Errors[1])
	}
}

func TestWriteErrorLog_CSV(t *testing.T) {
	dir := t.TempDir()
	failures := sampleFailures()
	path, err := writeErrorLog(dir, "csv", 5, len(failures), failures)
	if err != nil {
		t.Fatalf("writeErrorLog: %v", err)
	}
	if filepath.Base(path) != "ERROR.csv" {
		t.Fatalf("unexpected file name: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	if len(rows) != 3 { // header + 2
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0][0] != "file" || rows[0][1] != "error" {
		t.Fatalf("bad header: %v", rows[0])
	}
	if rows[2][0] != `C:\in\b.dcm` || rows[2][1] != `open: permission denied, "x"` {
		t.Fatalf("bad row (escaping?): %v", rows[2])
	}
}

func TestWriteErrorLog_TXT(t *testing.T) {
	dir := t.TempDir()
	failures := sampleFailures()
	path, err := writeErrorLog(dir, "txt", 5, len(failures), failures)
	if err != nil {
		t.Fatalf("writeErrorLog: %v", err)
	}
	if filepath.Base(path) != "ERROR.txt" {
		t.Fatalf("unexpected file name: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	for _, want := range []string{"processed: 5", "failed: 2", `C:\in\a.dcm: process: parse: unexpected EOF`} {
		if !strings.Contains(text, want) {
			t.Fatalf("txt missing %q in:\n%s", want, text)
		}
	}
}

func TestWriteErrorLog_CreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist")
	if _, err := writeErrorLog(dir, "json", 0, 1, sampleFailures()[:1]); err != nil {
		t.Fatalf("expected dir to be created, got error: %v", err)
	}
}
