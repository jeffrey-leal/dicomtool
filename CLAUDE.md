# dicomtool

A cross-platform CLI tool for inspecting, modifying, and working with DICOM files.

## Build

Pure Go — no CGO required:

```
go build -o dicomtool.exe .
```

Cross-compile for other platforms:
```
GOOS=linux   GOARCH=amd64 go build -o dicomtool-linux .
GOOS=darwin  GOARCH=amd64 go build -o dicomtool-mac-x64 .
GOOS=darwin  GOARCH=arm64 go build -o dicomtool-mac-arm64 .
```

## Testing

```
go test ./...
```

Tests live alongside their commands in `cmd/` (e.g. `inspect_test.go`, `root_test.go`).

## Project structure

| Path | Purpose |
|---|---|
| `main.go` | Entry point — delegates to `cmd.Execute()` |
| `cmd/root.go` | Root Cobra command and global flags |
| `cmd/dicom.go` | `isDICOMFile` magic-byte check, `openDICOMFile` helper |
| `cmd/inspect.go` | `inspect` command — dumps DICOM tags |
| `cmd/modify.go` | `modify` command — edits tag values |
| `cmd/tags.go` | `tags` command — lists known tags |
| `cmd/dicomdir.go` | `dicomdir` command |
| `cmd/profile.go` | Profile support |
| `cmd/options.go` | Shared option parsing |

## Key dependencies

- `github.com/spf13/cobra v1.10.2` — CLI framework
- `github.com/suyashkumar/dicom v1.1.0` — DICOM parsing

## Notes

- DICOM validation: checks for `DICM` magic bytes at offset 128 (`cmd/dicom.go`)
- `isDICOMFile` is the canonical check used before any parse attempt
