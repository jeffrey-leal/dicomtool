package cmd

import (
	"io"
	"os"
)

// openDICOMFile opens path, verifies the DICOM magic bytes, seeks back to the
// start of the file, and returns the open *os.File. The caller must close it.
// Returns (nil, nil) when the file does not carry the DICOM magic signature.
func openDICOMFile(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, dicomMagicOffset+len(dicomMagic))
	n, err := io.ReadFull(f, buf)
	if err != nil || n < len(buf) {
		f.Close()
		return nil, nil
	}
	for i, b := range dicomMagic {
		if buf[dicomMagicOffset+i] != b {
			f.Close()
			return nil, nil
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// dicomMagicOffset is the byte offset of the DICOM magic bytes.
const dicomMagicOffset = 128

// dicomMagic is the four-byte signature present in every valid DICOM file.
var dicomMagic = []byte{'D', 'I', 'C', 'M'}

// isDICOMFile reports whether path is a valid DICOM file by checking for the
// "DICM" signature at byte offset 128, regardless of filename extension.
func isDICOMFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, dicomMagicOffset+len(dicomMagic))
	n, err := io.ReadFull(f, buf)
	if err != nil || n < len(buf) {
		// File is too short to contain the magic bytes — not a DICOM file.
		return false, nil
	}

	for i, b := range dicomMagic {
		if buf[dicomMagicOffset+i] != b {
			return false, nil
		}
	}
	return true, nil
}
