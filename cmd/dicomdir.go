package cmd

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/tag"
)

const (
	// explicitLittleEndian is the transfer syntax required by DICOM PS3.10 §8.3
	// for DICOMDIR files. Using implicit VR causes commercial readers to fail.
	explicitLittleEndian = "1.2.840.10008.1.2.1"

	dicomdirSOPClassUID    = "1.2.840.10008.1.3.10"
	implementationClassUID = "2.25.311926237095024698369566570265386635591"
)

// Byte patterns used to locate UL offset fields in the serialised DICOMDIR.
// Each is the 8-byte explicit-VR LE header for the element:
//
//	tag(4) + VR "UL"(2) + length 0x0004(2)
var (
	patFirstRecord = []byte{0x04, 0x00, 0x00, 0x12, 0x55, 0x4C, 0x04, 0x00} // (0004,1200)
	patLastRecord  = []byte{0x04, 0x00, 0x02, 0x12, 0x55, 0x4C, 0x04, 0x00} // (0004,1202)
	patNextRecord  = []byte{0x04, 0x00, 0x00, 0x14, 0x55, 0x4C, 0x04, 0x00} // (0004,1400)
	patLowerLevel  = []byte{0x04, 0x00, 0x20, 0x14, 0x55, 0x4C, 0x04, 0x00} // (0004,1420)
)

// ----------------------------------------------------------------------------
// Hierarchy types
// ----------------------------------------------------------------------------

type dicomdirImageInfo struct {
	fileComponents             []string // one element per path component
	sopClass, sopInstance      string
	transferSyntax             string
	instanceNumber             string
}

type dicomdirSeriesInfo struct {
	uid, modality, number string
	images                []*dicomdirImageInfo
}

type dicomdirStudyInfo struct {
	uid, date, time, accession, id string
	series                         []*dicomdirSeriesInfo
}

type dicomdirPatientInfo struct {
	id, name string
	studies  []*dicomdirStudyInfo
}

// ----------------------------------------------------------------------------
// Flat record type used during construction
// ----------------------------------------------------------------------------

// recEntry holds a single directory record's elements and its position in the
// PATIENT → STUDY → SERIES → IMAGE linked-list tree.
type recEntry struct {
	items []*dicom.Element
	next  int // index of next sibling in flat slice; -1 = none
	child int // index of first child in flat slice; -1 = none
}

// ----------------------------------------------------------------------------
// Public entry point
// ----------------------------------------------------------------------------

// WriteDICOMDIR scans all DICOM files under outputDir and writes a conformant
// DICOMDIR at outputDir/DICOMDIR.
//
// It uses a two-pass approach:
//  1. Write to a bytes.Buffer with all offset fields set to zero.
//  2. Locate every sequence-item start (FE FF 00 E0) in the buffer — each
//     corresponds to one directory record in depth-first order.
//  3. Patch the UL offset fields in-place with the real byte positions.
//  4. Write the patched bytes to the final file.
func WriteDICOMDIR(outputDir string) error {
	sources, err := collectDicomdirSources(outputDir)
	if err != nil {
		return err
	}
	return WriteDICOMDIRFromSources(outputDir, sources)
}

// WriteDICOMDIRFromSources builds and writes a conformant DICOMDIR at
// outputDir/DICOMDIR from pre-collected per-file metadata, avoiding a second
// parse of the output tree. Sources are sorted by relative path so the record
// order is deterministic regardless of how they were gathered.
func WriteDICOMDIRFromSources(outputDir string, sources []dicomdirSource) error {
	sort.Slice(sources, func(i, j int) bool { return sources[i].rel < sources[j].rel })
	patients := buildPatientsFromSources(sources)
	if len(patients) == 0 {
		return nil
	}

	sopInstanceUID := generateUID()
	recs, lastPatIdx := buildDicomdirRecords(patients)

	ds := buildDicomdirDataset(sopInstanceUID, recs)

	// Pass 1: serialise with all offsets = 0.
	var buf bytes.Buffer
	if err := dicom.Write(&buf, ds); err != nil {
		return fmt.Errorf("DICOMDIR first-pass write: %w", err)
	}
	data := buf.Bytes()

	// Locate every sequence-item start byte.
	positions := findSequenceItemPositions(data)
	if len(positions) != len(recs) {
		return fmt.Errorf("DICOMDIR: record count mismatch — expected %d records, found %d item markers in output",
			len(recs), len(positions))
	}

	// Patch root-level first / last record pointers.
	patchUL32(data, 0, len(data), patFirstRecord, uint32(positions[0]))
	patchUL32(data, 0, len(data), patLastRecord, uint32(positions[lastPatIdx]))

	// Patch the next-sibling and first-child pointers inside each item.
	for i, rec := range recs {
		start := positions[i]
		end := len(data)
		if i+1 < len(positions) {
			end = positions[i+1]
		}

		nextOff := uint32(0)
		if rec.next >= 0 {
			nextOff = uint32(positions[rec.next])
		}
		childOff := uint32(0)
		if rec.child >= 0 {
			childOff = uint32(positions[rec.child])
		}

		patchUL32(data, start, end, patNextRecord, nextOff)
		patchUL32(data, start, end, patLowerLevel, childOff)
	}

	dest := filepath.Join(outputDir, "DICOMDIR")
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create DICOMDIR: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write DICOMDIR: %w", err)
	}
	if Opts.Verbose {
		fmt.Printf("written: %s\n", dest)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Tree collection
// ----------------------------------------------------------------------------

// dicomdirSource is the minimal per-file metadata needed to build a DICOMDIR,
// extracted once from a parsed dataset so the output tree need not be re-parsed.
type dicomdirSource struct {
	rel                                                   string
	patientID, patientName                                string
	studyUID, studyDate, studyTime, accession, studyID    string
	seriesUID, modality, seriesNumber                     string
	sopClass, sopInstance, transferSyntax, instanceNumber string
}

// extractDicomdirSource pulls the DICOMDIR-relevant fields from a parsed dataset.
// rel is the file's path relative to the output root.
func extractDicomdirSource(ds *dicom.Dataset, rel string) dicomdirSource {
	ts := stringVal(ds, tag.TransferSyntaxUID)
	if ts == "" {
		ts = explicitLittleEndian
	}
	return dicomdirSource{
		rel:            rel,
		patientID:      stringVal(ds, tag.PatientID),
		patientName:    stringVal(ds, tag.PatientName),
		studyUID:       stringVal(ds, tag.StudyInstanceUID),
		studyDate:      stringVal(ds, tag.StudyDate),
		studyTime:      stringVal(ds, tag.Tag{Group: 0x0008, Element: 0x0030}), // StudyTime TM
		accession:      stringVal(ds, tag.Tag{Group: 0x0008, Element: 0x0050}), // AccessionNumber SH
		studyID:        stringVal(ds, tag.StudyID),
		seriesUID:      stringVal(ds, tag.SeriesInstanceUID),
		modality:       stringVal(ds, tag.Modality),
		seriesNumber:   stringVal(ds, tag.SeriesNumber),
		sopClass:       stringVal(ds, tag.SOPClassUID),
		sopInstance:    stringVal(ds, tag.SOPInstanceUID),
		transferSyntax: ts,
		instanceNumber: stringVal(ds, tag.InstanceNumber),
	}
}

// collectDicomdirSources walks outputDir, parsing each file once to extract its
// DICOMDIR metadata. Used by the standalone WriteDICOMDIR path; the modify
// pipeline collects sources from in-memory datasets instead.
func collectDicomdirSources(outputDir string) ([]dicomdirSource, error) {
	var sources []dicomdirSource
	err := filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) == "DICOMDIR" {
			return nil
		}
		srcFile, ferr := openDICOMFile(path)
		if ferr != nil || srcFile == nil {
			return nil
		}
		info, serr := srcFile.Stat()
		if serr != nil {
			srcFile.Close()
			return nil
		}
		ds, perr := dicom.Parse(bufio.NewReaderSize(srcFile, 1<<20), info.Size(), nil)
		srcFile.Close()
		if perr != nil {
			return nil // skip unparseable files silently
		}
		rel, rerr := filepath.Rel(outputDir, path)
		if rerr != nil {
			return rerr
		}
		sources = append(sources, extractDicomdirSource(&ds, rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sources, nil
}

// buildPatientsFromSources assembles the patient / study / series / image
// hierarchy from per-file sources. Iteration order follows the (pre-sorted)
// sources slice, so patient/study/series ordering is deterministic.
func buildPatientsFromSources(sources []dicomdirSource) []*dicomdirPatientInfo {
	patMap := map[string]*dicomdirPatientInfo{}
	var patOrder []string

	for _, src := range sources {
		// DICOM ReferencedFileID is multi-valued CS, one component per path segment.
		components := strings.Split(filepath.ToSlash(src.rel), "/")

		pat, exists := patMap[src.patientID]
		if !exists {
			pat = &dicomdirPatientInfo{id: src.patientID, name: src.patientName}
			patMap[src.patientID] = pat
			patOrder = append(patOrder, src.patientID)
		}

		var study *dicomdirStudyInfo
		for _, s := range pat.studies {
			if s.uid == src.studyUID {
				study = s
				break
			}
		}
		if study == nil {
			study = &dicomdirStudyInfo{
				uid:       src.studyUID,
				date:      src.studyDate,
				time:      src.studyTime,
				accession: src.accession,
				id:        src.studyID,
			}
			pat.studies = append(pat.studies, study)
		}

		var series *dicomdirSeriesInfo
		for _, s := range study.series {
			if s.uid == src.seriesUID {
				series = s
				break
			}
		}
		if series == nil {
			series = &dicomdirSeriesInfo{
				uid:      src.seriesUID,
				modality: src.modality,
				number:   src.seriesNumber,
			}
			study.series = append(study.series, series)
		}

		series.images = append(series.images, &dicomdirImageInfo{
			fileComponents: components,
			sopClass:       src.sopClass,
			sopInstance:    src.sopInstance,
			transferSyntax: src.transferSyntax,
			instanceNumber: src.instanceNumber,
		})
	}

	out := make([]*dicomdirPatientInfo, 0, len(patOrder))
	for _, id := range patOrder {
		out = append(out, patMap[id])
	}
	return out
}

// ----------------------------------------------------------------------------
// Flat record construction
// ----------------------------------------------------------------------------

// buildDicomdirRecords flattens the patient tree into a depth-first ordered
// []recEntry and wires the sibling/child index links.
// Returns the slice and the flat-slice index of the last PATIENT record.
func buildDicomdirRecords(patients []*dicomdirPatientInfo) ([]recEntry, int) {
	var recs []recEntry
	var patIndices []int

	for _, pat := range patients {
		pi := len(recs)
		patIndices = append(patIndices, pi)
		recs = append(recs, recEntry{items: buildPatientRecord(pat), next: -1, child: -1})

		var studyIndices []int
		for _, study := range pat.studies {
			si := len(recs)
			studyIndices = append(studyIndices, si)
			recs = append(recs, recEntry{items: buildStudyRecord(study), next: -1, child: -1})

			var seriesIndices []int
			for _, series := range study.series {
				sri := len(recs)
				seriesIndices = append(seriesIndices, sri)
				recs = append(recs, recEntry{items: buildSeriesRecord(series), next: -1, child: -1})

				var imgIndices []int
				for _, img := range series.images {
					ii := len(recs)
					imgIndices = append(imgIndices, ii)
					recs = append(recs, recEntry{items: buildImageRecord(img), next: -1, child: -1})
				}
				linkSiblings(recs, imgIndices)
				if len(imgIndices) > 0 {
					recs[sri].child = imgIndices[0]
				}
			}
			linkSiblings(recs, seriesIndices)
			if len(seriesIndices) > 0 {
				recs[si].child = seriesIndices[0]
			}
		}
		linkSiblings(recs, studyIndices)
		if len(studyIndices) > 0 {
			recs[pi].child = studyIndices[0]
		}
	}
	linkSiblings(recs, patIndices)

	lastPat := -1
	if len(patIndices) > 0 {
		lastPat = patIndices[len(patIndices)-1]
	}
	return recs, lastPat
}

// linkSiblings sets the .next field of each entry in indices to the following one.
func linkSiblings(recs []recEntry, indices []int) {
	for i := 0; i+1 < len(indices); i++ {
		recs[indices[i]].next = indices[i+1]
	}
}

// ----------------------------------------------------------------------------
// Dataset builder
// ----------------------------------------------------------------------------

func buildDicomdirDataset(sopInstanceUID string, recs []recEntry) dicom.Dataset {
	seqItems := make([][]*dicom.Element, len(recs))
	for i, r := range recs {
		seqItems[i] = r.items
	}

	var ds dicom.Dataset
	ds.Elements = []*dicom.Element{
		// File Meta (group 0002) — written by writeFileHeader in Explicit VR LE.
		mustElem(tag.FileMetaInformationVersion, []byte{0x00, 0x01}),
		mustElem(tag.MediaStorageSOPClassUID, []string{dicomdirSOPClassUID}),
		mustElem(tag.MediaStorageSOPInstanceUID, []string{sopInstanceUID}),
		mustElem(tag.TransferSyntaxUID, []string{explicitLittleEndian}),
		mustElem(tag.ImplementationClassUID, []string{implementationClassUID}),
		mustElem(tag.ImplementationVersionName, []string{"DICOMTOOL_V1"}),
		// Group 0004 — ascending tag order required by standard.
		mustElem(tag.FileSetID, []string{""}),
		mustElem(tag.OffsetOfTheFirstDirectoryRecordOfTheRootDirectoryEntity, []int{0}),
		mustElem(tag.OffsetOfTheLastDirectoryRecordOfTheRootDirectoryEntity, []int{0}),
		mustElem(tag.FileSetConsistencyFlag, []int{0}),
		mustElem(tag.DirectoryRecordSequence, seqItems),
	}
	return ds
}

// ----------------------------------------------------------------------------
// Per-level record element builders (elements in ascending tag order)
// ----------------------------------------------------------------------------

func buildPatientRecord(p *dicomdirPatientInfo) []*dicom.Element {
	return []*dicom.Element{
		mustElem(tag.OffsetOfTheNextDirectoryRecord, []int{0}),              // (0004,1400)
		mustElem(tag.RecordInUseFlag, []int{0xFFFF}),                        // (0004,1410)
		mustElem(tag.OffsetOfReferencedLowerLevelDirectoryEntity, []int{0}), // (0004,1420)
		mustElem(tag.DirectoryRecordType, []string{"PATIENT"}),              // (0004,1430)
		mustElem(tag.PatientName, []string{p.name}),                         // (0010,0010)
		mustElem(tag.PatientID, []string{p.id}),                             // (0010,0020)
	}
}

func buildStudyRecord(s *dicomdirStudyInfo) []*dicom.Element {
	return []*dicom.Element{
		mustElem(tag.OffsetOfTheNextDirectoryRecord, []int{0}),
		mustElem(tag.RecordInUseFlag, []int{0xFFFF}),
		mustElem(tag.OffsetOfReferencedLowerLevelDirectoryEntity, []int{0}),
		mustElem(tag.DirectoryRecordType, []string{"STUDY"}),
		mustElem(tag.StudyDate, []string{s.date}),                                     // (0008,0020)
		mustElemRaw(tag.Tag{Group: 0x0008, Element: 0x0030}, "TM", []string{s.time}),  // StudyTime
		mustElemRaw(tag.Tag{Group: 0x0008, Element: 0x0050}, "SH", []string{s.accession}), // AccessionNumber
		mustElem(tag.StudyInstanceUID, []string{s.uid}),                               // (0020,000D)
		mustElem(tag.StudyID, []string{s.id}),                                         // (0020,0010)
	}
}

func buildSeriesRecord(s *dicomdirSeriesInfo) []*dicom.Element {
	return []*dicom.Element{
		mustElem(tag.OffsetOfTheNextDirectoryRecord, []int{0}),
		mustElem(tag.RecordInUseFlag, []int{0xFFFF}),
		mustElem(tag.OffsetOfReferencedLowerLevelDirectoryEntity, []int{0}),
		mustElem(tag.DirectoryRecordType, []string{"SERIES"}),
		mustElem(tag.Modality, []string{s.modality}),           // (0008,0060)
		mustElem(tag.SeriesInstanceUID, []string{s.uid}),        // (0020,000E)
		mustElem(tag.SeriesNumber, []string{s.number}),          // (0020,0011)
	}
}

func buildImageRecord(img *dicomdirImageInfo) []*dicom.Element {
	return []*dicom.Element{
		mustElem(tag.OffsetOfTheNextDirectoryRecord, []int{0}),
		mustElem(tag.RecordInUseFlag, []int{0xFFFF}),
		mustElem(tag.OffsetOfReferencedLowerLevelDirectoryEntity, []int{0}),
		mustElem(tag.DirectoryRecordType, []string{"IMAGE"}),
		mustElem(tag.ReferencedFileID, img.fileComponents),                            // (0004,1500) CS multi-value
		mustElem(tag.ReferencedSOPClassUIDInFile, []string{img.sopClass}),             // (0004,1510)
		mustElem(tag.ReferencedSOPInstanceUIDInFile, []string{img.sopInstance}),       // (0004,1511)
		mustElem(tag.ReferencedTransferSyntaxUIDInFile, []string{img.transferSyntax}), // (0004,1512)
		mustElem(tag.InstanceNumber, []string{img.instanceNumber}),                    // (0020,0013)
	}
}

// ----------------------------------------------------------------------------
// Two-pass helpers
// ----------------------------------------------------------------------------

// findSequenceItemPositions returns the byte offset of every DICOM sequence-item
// start tag (FFFE,E000 = FE FF 00 E0 in little-endian) found in data.
// Our DICOMDIR content (UIDs, names, dates) never contains this byte sequence,
// so all matches are genuine item boundaries.
func findSequenceItemPositions(data []byte) []int {
	marker := []byte{0xFE, 0xFF, 0x00, 0xE0}
	var positions []int
	// Advance by one past each hit: the 4-byte marker cannot self-overlap, so
	// this finds exactly the same set of starts as a per-byte scan, but lets the
	// optimized bytes.Index scanner do the work.
	for off := 0; off <= len(data)-len(marker); {
		i := bytes.Index(data[off:], marker)
		if i < 0 {
			break
		}
		positions = append(positions, off+i)
		off += i + 1
	}
	return positions
}

// patchUL32 locates the first occurrence of pattern within data[start:end] and
// overwrites the four bytes immediately following it with value (little-endian).
func patchUL32(data []byte, start, end int, pattern []byte, value uint32) {
	if end > len(data) {
		end = len(data)
	}
	idx := bytes.Index(data[start:end], pattern)
	if idx < 0 {
		return
	}
	off := start + idx + len(pattern)
	binary.LittleEndian.PutUint32(data[off:off+4], value)
}

// ----------------------------------------------------------------------------
// Utility
// ----------------------------------------------------------------------------

// generateUID returns a globally unique DICOM UID using the ISO 2.25 UUID root.
func generateUID() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(fmt.Sprintf("generateUID: %v", err))
	}
	var n big.Int
	n.SetBytes(b)
	return "2.25." + n.String()
}

// stringVal returns the first string value of the element with tag t, or "".
func stringVal(ds *dicom.Dataset, t tag.Tag) string {
	elem, err := ds.FindElementByTag(t)
	if err != nil {
		return ""
	}
	if v, ok := elem.Value.GetValue().([]string); ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

// mustElem creates an Element via the library's tag dictionary. Panics on error.
func mustElem(t tag.Tag, data any) *dicom.Element {
	e, err := dicom.NewElement(t, data)
	if err != nil {
		panic(fmt.Sprintf("mustElem %v: %v", t, err))
	}
	return e
}

// mustElemRaw creates an Element with an explicit VR string, bypassing tag.Find.
// Used for tags that may not be present in the library's dictionary.
func mustElemRaw(t tag.Tag, vr string, data any) *dicom.Element {
	v, err := dicom.NewValue(data)
	if err != nil {
		panic(fmt.Sprintf("mustElemRaw value %v: %v", t, err))
	}
	return &dicom.Element{
		Tag:                    t,
		ValueRepresentation:    tag.GetVRKind(t, vr),
		RawValueRepresentation: vr,
		Value:                  v,
	}
}
