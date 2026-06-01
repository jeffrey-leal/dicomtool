package cmd

import (
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
	applyUIDRemap(elems, r)

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
