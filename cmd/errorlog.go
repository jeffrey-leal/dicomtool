package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// fileFailure records one file that could not be processed during a modify run.
type fileFailure struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// writeErrorLog writes the collected failures to dir/ERROR.<format> and returns
// the path written. Supported formats are "txt", "csv", and "json". The
// directory is created if it does not already exist (it may be absent when every
// file failed before any output was produced).
func writeErrorLog(dir, format string, processed, failed int, failures []fileFailure) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "ERROR."+format)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	switch format {
	case "txt":
		if _, err := fmt.Fprintf(f, "dicomtool error log\nprocessed: %d\nfailed: %d\n\n", processed, failed); err != nil {
			return "", err
		}
		for _, fl := range failures {
			if _, err := fmt.Fprintf(f, "%s: %s\n", fl.File, fl.Error); err != nil {
				return "", err
			}
		}

	case "csv":
		w := csv.NewWriter(f)
		if err := w.Write([]string{"file", "error"}); err != nil {
			return "", err
		}
		for _, fl := range failures {
			if err := w.Write([]string{fl.File, fl.Error}); err != nil {
				return "", err
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return "", err
		}

	case "json":
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		report := struct {
			Processed int           `json:"processed"`
			Failed    int           `json:"failed"`
			Errors    []fileFailure `json:"errors"`
		}{processed, failed, failures}
		if err := enc.Encode(report); err != nil {
			return "", err
		}

	default:
		return "", fmt.Errorf("unsupported error log format %q", format)
	}

	return path, nil
}
