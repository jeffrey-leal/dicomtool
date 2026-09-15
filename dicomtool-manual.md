# dicomtool

**Usage Manual  v2.0.0**

September 15, 2026

A command-line utility for inspecting and modifying DICOM medical imaging files.

---


## 1  Introduction

`dicomtool` is a command-line utility for processing DICOM medical imaging files without requiring specialist imaging software. It operates on entire directory trees, preserving the original folder structure in its output.

Key capabilities:

- Inspect DICOM tag values in one or more files
- Set or replace the value of any tag by its numeric identifier or a user-defined alias
- Remove specific tags by identifier
- Remove all private (odd-group) tags from a file
- Apply a positional mask to the Patient Date of Birth field
- Replace study, series, and instance UIDs with freshly generated, consistent values, renaming files and folders named after them
- Skip Secondary Capture (screenshot) files during batch processing
- Zero out a specified number of pixel rows from the top of each image frame
- Correct or remove tags whose Value Representation does not match the DICOM standard
- Process multiple files simultaneously using a configurable worker pool
- Generate a DICOMDIR index file for the output directory tree
- Define named processing profiles combining any of the above operations
- Map short alias names to DICOM tag identifiers for convenience
Configuration is stored in two JSON files under `~/.dicomtool/`:

- `tags.json` -- tag alias mappings
- `profiles.json` -- named processing profiles
Both files are created automatically with default content on the first invocation.


## 2  Installation

Building from source requires Go 1.21 or later.

```
git clone <repository-url>
cd dicomtool
go build -o dicomtool.exe .
```

The resulting `dicomtool.exe` (Windows) or `dicomtool` (Linux / macOS) is a single self-contained binary with no runtime dependencies. Copy it anywhere on your `PATH`.


## 3  Parameter Syntax

All parameters are passed as `key:value` pairs on the command line. The colon character (`:`) separates the key from its value. Keys are case-insensitive.

```
dicomtool <command> key1:value1 key2:value2 ...
```

Bare arguments that contain no colon are treated as input paths. The following two forms are therefore equivalent:

```
dicomtool inspect C:\scans\study01
dicomtool inspect input:C:\scans\study01
```

Parameters that accept multiple values (e.g. `set:`, `remove:`, `tag:`, `input:`) may be repeated any number of times:

```
dicomtool modify input:C:\in output:C:\out set:0010,0010=ANON set:0010,0020=ID001 remove:0008,0080
```

Boolean parameters accept `true` or `1` (case-insensitive). Any other value, including an empty value, is treated as false.


## 4  Commands Reference


### 4.1  inspect

Parses one or more DICOM files and prints their tag values to the console.


#### Syntax

```
dicomtool inspect input:<file> [input:<file>...] (all:true | tag:<tag> [tag:<tag>...]) [verbose:true]
```


#### Parameters

| Parameter | Description |
|---|---|
| `input:<file>` | Path to a DICOM file to inspect. May be repeated for multiple files. Bare path arguments are also accepted. |
| `tag:<tag>` | A specific tag to display, in `GGGG,EEEE` format or as a defined alias. May be repeated. |
| `all:true` | Display every tag present in the file. Pixel data is reported as a summary line rather than raw bytes. |
| `verbose:true` | Print additional diagnostic information. |


#### Output Format

Each element is printed on one line:

```
  (GGGG,EEEE)  VR  TagName                                  = value
```

Multi-value fields use the DICOM backslash separator. Binary fields longer than 16 bytes are summarised as `<binary, N bytes>`. Pixel data is always shown as a summary:

```
File: C:\scans\image.dcm
  (0002,0001)  OB  FileMetaInformationVersion        = 00 01
  (0008,0020)  DA  StudyDate                          = 20240115
  (0008,0060)  CS  Modality                           = CT
  (0010,0010)  PN  PatientName                        = Smith^John
  (0010,0030)  DA  PatientBirthDate                   = 19800101
  (0020,000D)  UI  StudyInstanceUID                   = 1.2.840.10008...
  (7FE0,0010)  OW  PixelData                          = [pixel data: skipped]
```


#### Sequence Fields

Sequence (SQ) elements are expanded inline. Each sequence item is indented four spaces per nesting level:

```
  (0008,1115)  SQ  ReferencedSeriesSequence          [sequence: 1 item(s)]
    Item 1:
      (0008,1140)  SQ  ReferencedImageSequence            [sequence: 2 item(s)]
        Item 1:
          (0008,1150)  UI  ReferencedSOPClassUID              = 1.2.840.10008.5.1.4.1.1.2
          (0008,1155)  UI  ReferencedSOPInstanceUID           = 1.2.3.4.5.6.7.8
        Item 2:
          (0008,1150)  UI  ReferencedSOPClassUID              = 1.2.840.10008.5.1.4.1.1.2
          (0008,1155)  UI  ReferencedSOPInstanceUID           = 1.2.3.4.5.6.7.9
```


#### Notes

- Either `all:true` or at least one `tag:` parameter is required.
- When using `all:true`, pixel data is skipped during parsing to avoid loading large image buffers; it is shown as `[pixel data: skipped]`.
- When fetching a specific tag (e.g. `tag:7FE0,0010`), the file is fully parsed and pixel data dimensions are shown.
- Tag aliases defined in `tags.json` are resolved before lookup.
- If a requested tag is not present in the file, a `not found` line is printed and processing continues.
- If a file cannot be parsed, an error line is printed and the next file is processed.
- Sequence fields are expanded recursively with no depth limit. Very deeply nested sequences are indented accordingly.

#### Examples

```
dicomtool inspect C:\scans\image.dcm all:true
```

```
dicomtool inspect input:image.dcm tag:0010,0010 tag:0010,0020
```

```
dicomtool inspect input:image.dcm tag:PatientName tag:PatientID
```

```
dicomtool inspect input:a.dcm input:b.dcm input:c.dcm all:true
```


### 4.2  modify

Reads every DICOM file under an input directory tree, applies the specified modifications, and writes the results to an output directory, preserving the original folder structure.


#### Syntax

```
dicomtool modify input:<dir> output:<dir>
    [set:<tag>=<value> ...]
    [remove:<tag> ...]
    [dob:<mask>]
    [shiftdays:<n>]
    [remapuids:true]
    [noprivate:true]
    [ignoretype:<types>]
    [ignoremodality:<modalities>]
    [maskrows:<n>]
    [fixvr:correct|skip|passthrough]
    [workers:<n>]
    [zip:true]
    [dicomdir:true]
    [profile:<name>]
    [verbose:true]
```


#### Parameters

| Parameter | Description |
|---|---|
| `input:<dir>` | Source directory. All DICOM files within the tree are processed. Required. |
| `output:<dir>` | Destination directory. Created if it does not exist. A relative path is resolved against the input directory. Required. |
| `set:<tag>=<value>` | Set the specified tag to the given value. `<tag>` may be a raw `GGGG,EEEE` identifier or a defined alias. Repeatable. |
| `remove:<tag>` | Remove the specified tag entirely from every output file. `<tag>` may be a raw identifier or alias. Repeatable. |
| `dob:<mask>` | Apply an 8-character positional mask to the Patient Date of Birth field (0010,0030). Digit characters in the mask overwrite the corresponding position; any other character preserves the original digit. Format: `YYYYMMDD`. |
| `shiftdays:<n>` | Shift every DA (Date) and DT (DateTime) field in each file by `n` days. `n` may be negative, zero, or positive. For DT fields, only the leading date component moves; the time, fraction, and timezone portion is preserved unchanged. Patient Date of Birth (0010,0030) is never affected by `shiftdays:` — use `dob:` for that field instead. |
| `remapuids:true` | Replace every study, series, and instance UID — and all references to them — with a freshly generated UID. Remapping is consistent across the entire run: the same source UID always maps to the same new UID in every file, so study/series/instance relationships and internal cross-references are preserved while linkage to the source is severed. SOP Class UIDs, Transfer Syntax UIDs, and the Implementation Class UID are left unchanged so files remain valid. Output file and folder names that contain a remapped UID are rewritten with the new UID, so no original UID survives in the output paths, ZIP entry names, or DICOMDIR either. See Section 9.4. |
| `noprivate:true` | Remove all private tags (those with an odd group number) before writing output. |
| `ignoretype:<types>` | Skip files whose Image Type tag (0008,0008) contains any of the supplied comma-delimited values. Comparison is case-insensitive. Example: `ignoretype:SECONDARY,DERIVED`. |
| `ignoremodality:<modalities>` | Skip files whose Modality tag (0008,0060) matches any of the supplied comma-delimited values. Comparison is case-insensitive. Example: `ignoremodality:SC,PR`. |
| `maskrows:<n>` | Zero out the first `n` pixel rows from the top of each image frame. Applies only to uncompressed (native) pixel data; compressed files produce a warning when `verbose:true` is set. `n` must be a positive integer. |
| `fixvr:correct|skip|passthrough` | Handle tags whose Value Representation (VR) does not match the DICOM standard dictionary. `correct` attempts to re-encode the tag value under the correct VR, falling back to removal if the conversion is not possible. `skip` silently removes all mismatched tags. `passthrough` writes the file as-is, suppressing the VR verification error. Private and unknown tags are always kept unchanged. When `verbose:true` is set, each affected tag is reported. |
| `workers:<n>` | Number of files to process simultaneously. Defaults to the number of logical CPU cores. Set to `1` to process files serially. Set to `0` to restore the default. |
| `zip:true` | Package all output files into a single ZIP archive instead of writing them to a directory. The `output:` path is used as the ZIP file name; a `.zip` extension is appended automatically if not already present. Cannot be combined with `dicomdir:true`. |
| `dicomdir:true` | After all files have been written, generate a DICOMDIR index file in the output directory. Cannot be combined with `zip:true`. |
| `profile:<name>` | Apply a named processing profile from `profiles.json`. CLI parameters take precedence over profile values. Only one profile may be given. If the profile cannot be applied — it does not exist, its base profile is missing or circular, or `profiles.json` cannot be read — the command stops with an error before any file is processed, rather than running without it. See Section 6. |
| `errorlog:txt|csv|json` | When one or more files fail, write the detailed per-file error messages to an `ERROR.<ext>` file in the root of the output directory instead of the console, in the chosen format. Without this parameter, the details print to the console. See the Error Handling subsection. |
| `verbose:true` | Print a line for each file written, per-operation diagnostics, and a summary count on completion. |


#### Processing Order

Operations are applied in the following order within each file:

- 1. Parse source file
- 2. Correct or remove VR-mismatched tags (if `fixvr:` supplied)
- 3. Skip file if Image Type (0008,0008) matches any `ignoretype:` value (file is not written to output)
- 4. Skip file if Modality (0008,0060) matches any `ignoremodality:` value (file is not written to output)
- 5. Remove private tags (if `noprivate:true`)
- 6. Apply explicit `remove:` removals
- 7. Shift DA/DT date fields (if `shiftdays:` supplied)
- 8. Apply DOB mask (if `dob:` supplied)
- 9. Remap UIDs (if `remapuids:true`)
- 10. Apply row mask (if `maskrows:` supplied)
- 11. Apply all `set:` edits
- 12. Write output file to directory, or to the ZIP archive if `zip:true` (under a path carrying the remapped UIDs, if `remapuids:true`)

#### Removed: uid:<suffix>

The `uid:<suffix>` parameter was removed in version 2.0.0. It appended a suffix to each UID, which left the original UID readable inside the result; `remapuids:true` replaces it. A run that still supplies `uid:` — on the command line, in the applied profile or a base profile it inherits from, or in a per-modality entry — is refused with an error before any output is written, rather than silently run without it. Delete the `uid` entry from the profile and use `remapuids:true` instead.


#### Non-DICOM Files

Files that do not carry the DICOM magic bytes (`DICM` at byte offset 128) are silently skipped regardless of file name or extension.


#### Error Handling

Processing is resilient: a file that cannot be read, parsed, or written is recorded as a failure and skipped, and the run continues with the remaining files. One bad file never aborts an entire batch.

On completion a summary is always printed in the form `N file(s) processed, M file(s) failed` (the failed clause appears only when at least one file failed). When any file fails, the command exits with a non-zero status so scripts and pipelines can detect partial failures.

The detailed per-file error messages are, by default, printed to the console after the summary. Supplying `errorlog:<format>` instead redirects them into a file named `ERROR.<ext>` written in the root of the output directory (for ZIP output, alongside the archive). The file is created only when at least one file failed. Three formats are supported:

- `errorlog:txt` — plain text: a `processed`/`failed` header followed by one `file: error` line per failure.
- `errorlog:csv` — comma-separated values with a `file,error` header row, suitable for spreadsheets.
- `errorlog:json` — a machine-readable object with `processed` and `failed` counts and an `errors` array of `{file, error}` entries.

#### Examples

```
dicomtool modify input:C:\original output:C:\deidentified
    set:0010,0010=ANONYMOUS set:0010,0020=ID001
    remove:0008,0080 noprivate:true verbose:true
```

```
dicomtool modify input:C:\study output:converted
    profile:anonymize dicomdir:true
```

```
dicomtool modify input:C:\study output:C:\out
    set:PatientName=ANON ignoremodality:SC maskrows:10
```

```
dicomtool modify input:C:\study output:C:\deidentified
    set:PatientName=ANON set:PatientID=ANON
    noprivate:true remapuids:true
```

```
dicomtool modify input:C:\study output:C:\out\study.zip zip:true
    set:PatientName=ANON noprivate:true
```

```
dicomtool modify input:C:\study output:C:\out fixvr:correct
    set:PatientName=ANON noprivate:true
```

```
dicomtool modify input:C:\study output:C:\out workers:8
    set:PatientName=ANON noprivate:true
```

```
dicomtool modify input:C:\study output:C:\deidentified
    profile:base-deident errorlog:json
```

```
dicomtool modify input:C:\study output:C:\deidentified
    shiftdays:-45 noprivate:true
```


### 4.3  tags

Manages the tag alias mappings stored in `~/.dicomtool/tags.json`. Aliases let you use a short readable name wherever a `GGGG,EEEE` tag identifier is required.


#### tags list

Lists all defined aliases.

```
dicomtool tags list
```


#### tags add

Adds or updates an alias. `<phrase>` is the alias name; `<tag>` must be a valid `GGGG,EEEE` identifier.

```
dicomtool tags add <phrase> <tag>
```

```
dicomtool tags add PatientName 0010,0010
dicomtool tags add AccessionNumber 0008,0050
dicomtool tags add InstitutionName 0008,0080
```


#### tags remove

Removes an existing alias.

```
dicomtool tags remove <phrase>
```

```
dicomtool tags remove PatientName
```


### 4.4  profiles

Manages named processing profiles stored in `~/.dicomtool/profiles.json`. A profile bundles a set of `modify` parameters under a single name that can be applied with `profile:<name>`.


#### profiles list

Lists the names of all defined profiles.

```
dicomtool profiles list
```


#### profiles show

Prints the JSON definition of a named profile.

```
dicomtool profiles show <name>
```

```
dicomtool profiles show anonymize
```


#### profiles add

Creates or completely replaces a profile. Parameters are the same key:value pairs accepted by the `modify` command, excluding `input:`, `output:`, and `profile:`. An optional `base:<name>` parameter may reference an existing profile whose settings are inherited.

```
dicomtool profiles add <name>
    [base:<name>]
    [set:<tag>=<value> ...] [remove:<tag> ...]
    [dob:<mask>] [remapuids:true]
    [noprivate:true] [maskrows:<n>]
    [fixvr:correct|skip|passthrough]
    [dicomdir:true] [verbose:true]
```

```
dicomtool profiles add anonymize
    set:PatientName=ANON set:PatientID=ANON set:AccessionNumber=
    dob:YYYY0101 noprivate:true
```

```
dicomtool profiles add research base:anonymize set:PatientID=RESEARCH001
```

Tag aliases are resolved to raw identifiers at save time, so profiles remain portable even if the alias definitions change later.

When `base:<name>` is supplied the named profile must already exist in the store. The base chain is resolved at runtime so any subsequent changes to the base profile are automatically reflected in derived profiles.


#### profiles remove

Deletes a profile.

```
dicomtool profiles remove <name>
```


### 4.5  version

Prints the application version string.

```
dicomtool version
```


### 4.6  install

Writes the built-in default `tags.json` and `profiles.json` to the standard configuration directory (`~/.dicomtool/`), unconditionally overwriting any existing files.


#### Syntax

```
dicomtool install
```


#### Description

Use `install` to reset your configuration to the factory defaults. This is useful when:

- You have corrupted or accidentally deleted your configuration files.
- You want to start fresh after making unwanted manual edits.
- You are setting up dicomtool on a new machine and want the default aliases and sample profile in place immediately.
Unlike the automatic first-run creation (which only creates files that do not already exist), `install` always overwrites both files even if they already exist.


#### Output

Two lines are printed confirming the written paths:

```
written: C:\Users\username\.dicomtool\tags.json
written: C:\Users\username\.dicomtool\profiles.json
```


#### Notes

- The configuration directory (`~/.dicomtool/`) is created if it does not exist.
- Any existing customisations in `tags.json` or `profiles.json` are permanently replaced. Back up the files first if you want to preserve them.
- `install` takes no parameters.

#### Examples

```
dicomtool install
```


## 5  Tag Aliases


### 5.1  Overview

Tag aliases map a short readable phrase to a full DICOM tag identifier in `GGGG,EEEE` hex format. Once defined, the phrase can be used anywhere a tag identifier is accepted: `set:`, `remove:`, `tag:`, and `profiles add`.

Aliases are stored in `~/.dicomtool/tags.json` and loaded automatically on every invocation. A custom file location can be specified with `config:<path>`.


### 5.2  File Format

The file is a flat JSON object mapping alias names to tag strings:

```
{
  "PatientName":     "0010,0010",
  "PatientID":       "0010,0020",
  "AccessionNumber": "0008,0050",
  "InstitutionName": "0008,0080"
}
```


### 5.3  Alias Resolution

Alias resolution is case-sensitive and exact. If a supplied identifier is not found in the alias table it is used as-is and must therefore be a valid `GGGG,EEEE` string.

Resolution occurs at the following points:

- `inspect tag:<phrase>` -- resolved before display
- `modify set:<phrase>=<value>` -- resolved before applying the edit
- `modify remove:<phrase>` -- resolved before removing the tag
- `profiles add set:<phrase>=<value>` -- resolved and stored as raw tag at save time
- Profile merge at runtime -- CLI and profile set entries are both resolved for per-tag deduplication (see Section 6.3)

### 5.4  Default Aliases

The default `tags.json` shipped with the binary defines the following aliases:

| Alias | Tag |
|---|---|
| TransferSyntaxUID | 0002,0010 |
| ReferencedTransferSyntaxUID | 0004,1512 |
| StudyDate | 0008,0020 |
| StudyTime | 0008,0030 |
| AccessionNumber | 0008,0050 |
| Modality | 0008,0060 |
| Manufacturer | 0008,0070 |
| InstitutionName | 0008,0080 |
| InstitutionAddress | 0008,0081 |
| ReferringPhysicianName | 0008,0090 |
| SeriesDescription | 0008,103E |
| StudyDescription | 0008,1030 |
| PatientName | 0010,0010 |
| PatientID | 0010,0020 |
| PatientDOB | 0010,0030 |
| PatientSex | 0010,0040 |
| PatientAge | 0010,1010 |
| ProtocolName | 0018,1030 |
| StudyInstanceUID | 0020,000D |
| SeriesInstanceUID | 0020,000E |
| StudyID | 0020,0010 |
| SeriesNumber | 0020,0011 |
| InstanceNumber | 0020,0013 |
| ConfidentialityCode | 0040,1008 |

The full content of the default file is reproduced in Appendix A.


## 6  Processing Profiles


### 6.1  Overview

A profile is a named collection of `modify` parameters stored in `~/.dicomtool/profiles.json`. Activating a profile with `profile:<name>` applies all its parameters as if they had been typed on the command line.

Profiles are intended to capture a recurring workflow -- for example a de-identification recipe -- so it can be applied consistently without retyping long parameter lists.


### 6.2  File Format

```
{
  "anonymize": {
    "set":            ["0010,0010=ANON", "0010,0020=ANON", "0008,0050="],
    "remove":         ["0008,0080", "0008,0081"],
    "keep":           [],
    "dob":            "YYYY0101",
    "remapuids":      true,
    "noprivate":      true,
    "keepprivate":    false,
    "maskrows":       0,
    "ignoretype":     ["SECONDARY"],
    "ignoremodality": ["SC", "PR"],
    "fixvr":          "correct",
    "dicomdir":       false,
    "verbose":        false,
    "per-modality":   {}
  }
}
```

All fields are optional. Omitted fields take their command-line defaults.

The `keep` field lists tags that are excluded from the removal list. The `keepprivate` field suppresses `noprivate` for files processed by this profile. The `per-modality` field maps DICOM Modality values to override settings applied only to files of that modality (see Section 6.7).

The full content of the default `profiles.json` is reproduced in Appendix A.


### 6.3  CLI Override Precedence

When both a profile and command-line parameters are supplied, the following merge rules apply:

| Parameter type | Merge rule |
|---|---|
| `dob` | CLI value wins. The profile value is ignored if the parameter was supplied on the command line. |
| `maskrows` | CLI value wins. The profile value is ignored if `maskrows:` was supplied on the command line. |
| `noprivate`, `dicomdir`, `verbose` | Either source can enable the flag. If either the CLI or the profile sets it to true, the flag is active. |
| `set` | Per-tag precedence. For each tag in the profile's set list, if the same tag (after alias resolution) appears in the CLI set list, the CLI value is used and the profile value is discarded. Tags only in the profile are added. |
| `remove` | Additive union. Tags from both the CLI and the profile are removed. |
| `ignoretype`, `ignoremodality` | Additive union. Values from both the CLI and the profile are combined, with duplicates removed. |
| `fixvr` | CLI value wins. The profile value is used only if `fixvr:` was not supplied on the command line. |

This means a profile establishes a baseline that can be partially overridden at runtime. For example, a profile that sets PatientName to ANON can be overridden for a specific run with `set:PatientName=Smith` without affecting any other profile parameters.


### 6.4  Base Profile Inheritance

A profile can inherit the settings of another profile by specifying `base:<name>` when it is created. When the derived profile is applied, the base chain is resolved at runtime: the base profile's settings are applied first, then the derived profile's settings are merged on top using the same precedence rules described in Section 6.3.

Example: create a base anonymization profile, then derive a research variant that keeps the same de-identification rules but overrides the Patient ID:

```
dicomtool profiles add anonymize
    set:PatientName=ANON set:PatientID=ANON
    set:AccessionNumber= dob:YYYY0101 noprivate:true

dicomtool profiles add research base:anonymize set:PatientID=RESEARCH001
```

Applying the `research` profile is equivalent to applying `anonymize` with `set:PatientID=RESEARCH001` added. All other `anonymize` settings -- PatientName, AccessionNumber, dob, noprivate -- are inherited unchanged.

Merge rules for base inheritance:

| Parameter type | Merge rule |
|---|---|
| `dob` | Derived value wins if non-empty; otherwise base value is used. |
| `maskrows` | Derived value wins if greater than zero; otherwise base value is used. |
| `noprivate`, `dicomdir`, `verbose`, `keepprivate` | OR'd: either base or derived being true activates the flag. |
| `set` | Per-tag precedence. Derived profile wins for any tag it defines; base contributes remaining tags. |
| `remove` | Union of both lists, deduplicated. |
| `keep` | Tags listed in the derived profile's `keep` list are removed from the merged `remove` list. A derived profile can restore tags that a parent removes. |
| `ignoretype`, `ignoremodality` | Union of both lists, deduplicated (case-insensitive). |
| `fixvr` | Derived value wins if non-empty; otherwise base value is used. |
| `per-modality` | Maps are merged per modality key. Derived entry wins for any modality it defines; base contributes remaining modalities. Entries for the same modality are themselves merged using these same rules. |

Base chains may be arbitrarily deep (profile A bases on B, which bases on C, and so on). Circular references are detected and reported as an error.


### 6.5  Modifying an Existing Profile

`profiles add` always performs a complete replace. To update a profile, re-run `profiles add` with the full desired parameter set. Alternatively, edit `~/.dicomtool/profiles.json` directly in a text editor -- the file is plain JSON.

To view the current definition before editing:

```
dicomtool profiles show anonymize
```


### 6.6  Setting a Tag to an Empty String in a Profile

To store an empty value for a tag (e.g. to blank the Accession Number), use `set:<tag>=` with nothing after the `=` sign:

```
dicomtool profiles add anonymize set:AccessionNumber=
```

This stores `"0008,0050="` in the profile's set list, which causes the tag to be written as an empty string when the profile is applied.


### 6.7  Per-Modality Overrides

A profile can define different processing rules for individual DICOM modalities using the `per-modality` field. When a file is processed, its Modality tag (0008,0060) is checked against the map; if a matching entry exists, its settings are layered on top of the base profile settings for that file only.

Per-modality overrides are additive: they do not replace the base profile, they extend it. The supported fields within a per-modality entry are: `set`, `remove`, `keep`, `keepprivate`, `dob`, `fixvr`, `maskrows`, and `noprivate`. The `per-modality` and `base` fields are not supported inside a per-modality entry.

Modality keys are matched case-insensitively. Tag aliases are resolved the same way as in top-level profile fields.

Merge rules for per-modality overrides (applied at runtime, per file):

| Field | Behaviour when modality matches |
|---|---|
| `set` | Override entries win per tag; base profile tags not overridden are kept. |
| `remove` | Union with base removals. Override adds to the list. |
| `keep` | Tags listed are subtracted from the combined removal list. Use to restore a tag that the base profile removes for all other modalities. |
| `keepprivate: true` | Suppresses `noprivate` for matched files, even when the base profile enables it. Lets private tags be preserved for a specific modality. |
| `noprivate: true` | Forces private tag removal for matched files, even if the base profile does not enable it. |
| `dob`, `fixvr` | Replaces the base value when non-empty. |
| `maskrows` | Replaces the base value when greater than zero. |

Example — remove Image Type for all modalities except MR; strip private tags everywhere except PT:

```
{
  "study-deident": {
    "set":       ["PatientName=ANON", "PatientID=ANON"],
    "remove":    ["0008,0008"],
    "noprivate": true,
    "fixvr":     "correct",
    "per-modality": {
      "MR": {
        "keep": ["0008,0008"]
      },
      "PT": {
        "keepprivate": true
      },
      "CT": {
        "remove": ["0018,1400", "0018,1401"],
        "set":    ["Manufacturer="]
      }
    }
  }
}
```

When `verbose:true` is active, a line is printed for each file that triggers a per-modality override, identifying the modality key that was matched. This provides an audit trail for de-identification workflows.


## 7  Configuration Files


### 7.1  Default Locations

| File | Default path |
|---|---|
| Tag aliases | `%USERPROFILE%\.dicomtool\tags.json` |
| Profiles | `%USERPROFILE%\.dicomtool\profiles.json` |

On Linux and macOS the directory is `~/.dicomtool/`.


### 7.2  Auto-creation

Both files are created automatically on the first invocation of any `dicomtool` command if they do not already exist. The files are seeded with default content -- the built-in alias table and a sample anonymization profile -- so they are immediately usable and serve as a starting point for customisation.

If the directory `~/.dicomtool/` does not exist it is created at the same time.


### 7.3  Custom Config Path

The tag alias file location can be overridden per-invocation with `config:<path>`. This does not affect the profile file location.

```
dicomtool modify input:C:\in output:C:\out config:D:\configs\custom-tags.json set:PatientName=ANON
```


### 7.4  Error Handling

Configuration errors always print to stderr regardless of the `verbose:` setting. If `tags.json` exists but contains invalid JSON, an error is printed, the file is treated as empty, and processing continues with no aliases loaded:

```
error: could not load tag aliases from "...": invalid character ...
```

A profile requested with `profile:` is different: if it cannot be applied, the command stops with an error and a non-zero exit status before any file is processed. Running on without the profile would silently skip every step it asks for — for a de-identification profile, writing output that is not de-identified. This covers a profile that does not exist, a missing or circular base profile, a `profiles.json` that cannot be read or parsed, an empty `profile:` value, and more than one `profile:` parameter:

```
Error: profile "name" not found in C:\Users\username\.dicomtool\profiles.json — run "dicomtool profiles list" to see the profiles defined there
Error: profile "name": profile "base-name" not found
Error: profile "name": circular base reference in profile "name"
Error: profile "name": could not load C:\Users\username\.dicomtool\profiles.json: invalid character ...
Error: profile: requires a profile name
Error: only one profile: may be given, got 2 (first, second)
```


## 8  Examples


### 8.1  Inspecting All Tags in a File

Print every tag and value in a DICOM file:

```
dicomtool inspect C:\scans\image.dcm all:true
```

Inspect selected tags using aliases:

```
dicomtool inspect input:image.dcm tag:PatientName tag:PatientID tag:StudyDate
```

Inspect multiple files at once:

```
dicomtool inspect input:a.dcm input:b.dcm all:true
```


### 8.2  Replacing a Tag Value

Set the Referring Physician Name (0008,0090) to an empty string:

```
dicomtool modify input:C:\study output:C:\out set:0008,0090=
```

Set the Patient Name using a defined alias:

```
dicomtool modify input:C:\study output:C:\out set:PatientName=SMITH^JOHN
```


### 8.3  Setting Multiple Tags in One Pass

```
dicomtool modify input:C:\study output:C:\out
    set:PatientName=ANON
    set:PatientID=ID001
    set:AccessionNumber=
    set:0008,0090=
```


### 8.4  Removing a Specific Tag

Remove the Institution Name tag entirely:

```
dicomtool modify input:C:\study output:C:\out remove:0008,0080
```

Remove multiple tags:

```
dicomtool modify input:C:\study output:C:\out remove:0008,0080 remove:0008,0081 remove:0032,1032
```


### 8.5  Removing All Private Tags

Private tags have an odd group number. The `noprivate:true` flag removes all of them:

```
dicomtool modify input:C:\study output:C:\out noprivate:true
```


### 8.6  Masking the Date of Birth

The `dob:<mask>` parameter applies a positional replacement to the Patient Date of Birth field (0010,0030). The mask must be exactly 8 characters in `YYYYMMDD` format. A digit character in the mask replaces the corresponding position; any non-digit character leaves the original digit unchanged.

Retain the year, replace month and day with January 1st:

```
dicomtool modify input:C:\study output:C:\out dob:YYYY0101
```

Replace the entire date with a fixed value:

```
dicomtool modify input:C:\study output:C:\out dob:19000101
```

Retain year and month, replace only the day:

```
dicomtool modify input:C:\study output:C:\out dob:YYYYMM01
```


### 8.7  Shifting Date Fields

The `shiftdays:<n>` parameter shifts every DA (Date) and DT (DateTime) field by `n` days, preserving the interval between dates within a study (e.g. days between a baseline and follow-up exam) without exposing the real calendar dates. `n` may be negative, zero, or positive. Patient Date of Birth (0010,0030) is never touched by `shiftdays:` -- combine it with `dob:` if the birth date also needs to be masked.

Shift every date back by 45 days:

```
dicomtool modify input:C:\study output:C:\out shiftdays:-45
```

Combine with other de-identification steps:

```
dicomtool modify input:C:\study output:C:\out
    set:PatientName=ANON dob:YYYY0101 noprivate:true shiftdays:-45
```

For a DT field such as AcquisitionDateTime (0008,002A), only the leading `YYYYMMDD` date component is shifted; the time, fraction, and timezone portion is left exactly as it was.


### 8.8  Remapping UIDs

Replace every study, series, and instance UID with a freshly generated one, consistently across the whole run:

```
dicomtool modify input:C:\study output:C:\out remapuids:true
```

Where the input tree is named after its UIDs, the output tree is named after the new ones, so the original UIDs do not survive in file or folder names:

```
C:\study\1.2.840.113619.2.55.3.1\1.2.840.113619.2.55.3.1.7\1.2.840.113619.2.55.3.1.7.12.dcm
    becomes
C:\out\2.25.1406...\2.25.2977...\2.25.8813....dcm
```

Transfer Syntax UIDs, SOP Class UIDs, and other standard UIDs are never remapped. See Section 9.4 for the full rules.


### 8.9  Skipping Files by Image Type or Modality

Use `ignoretype:` to skip files whose Image Type tag (0008,0008) contains any of the supplied values, and `ignoremodality:` to skip files whose Modality tag (0008,0060) matches any of the supplied values. Both parameters accept comma-delimited lists and comparisons are case-insensitive.

Skip Secondary Capture files by modality and image type:

```
dicomtool modify input:C:\study output:C:\out
    set:PatientName=ANON ignoremodality:SC ignoretype:SECONDARY
```

Skip presentation state and registration objects:

```
dicomtool modify input:C:\study output:C:\out ignoremodality:PR,REG
```

Skipped files are omitted from the output. With `verbose:true` a line is printed for each skipped file:

```
skipped (secondary capture): C:\study\series1\screen001.dcm
```


### 8.10  Masking Pixel Rows

The `maskrows:<n>` parameter zeros out the first `n` pixel rows from the top of every image frame. This is useful for removing patient demographics or institution names that are burned into the image pixel data rather than stored as separate DICOM tags.

Zero the top 20 rows of each frame:

```
dicomtool modify input:C:\study output:C:\out maskrows:20
```

Combine with other de-identification steps:

```
dicomtool modify input:C:\study output:C:\out
    set:PatientName=ANON dob:YYYY0101 noprivate:true
    ignoremodality:SC ignoretype:SECONDARY maskrows:20 verbose:true
```

With `verbose:true` a confirmation is printed for each file where rows are successfully zeroed:

```
  maskrows: zeroed top 20 row(s) across 1 frame(s)
```

Note: `maskrows` operates only on uncompressed (native) pixel data. Files with JPEG or JPEG-LS compression are skipped with a warning when `verbose:true` is set:

```
  maskrows: pixel data is compressed (encapsulated) -- skipping
```


### 8.11  Full De-identification Without a Profile

```
dicomtool modify input:C:\original output:C:\deidentified
    set:PatientName=ANON
    set:PatientID=ANON001
    set:AccessionNumber=
    remove:0008,0080
    remove:0008,0081
    remove:0008,0090
    dob:YYYY0101
    remapuids:true
    noprivate:true
    ignoremodality:SC
    ignoretype:SECONDARY
    maskrows:20
    verbose:true
```


### 8.12  Applying a Profile

```
dicomtool modify input:C:\study output:C:\out profile:anonymize
```


### 8.13  Overriding a Profile Parameter

The `PatientID` value from the profile is replaced by `STUDY42`; all other profile parameters apply unchanged:

```
dicomtool modify input:C:\study output:C:\out profile:anonymize set:PatientID=STUDY42
```


### 8.14  Generating a DICOMDIR

Generate a DICOMDIR index alongside the modified files:

```
dicomtool modify input:C:\study output:C:\out profile:anonymize dicomdir:true
```

A `DICOMDIR` file is written to the root of the output directory after all files have been processed. It is formatted as Explicit VR Little Endian and conforms to PS3.3 of the DICOM standard.


### 8.15  Relative Output Path

A relative `output:` path is resolved relative to the `input:` directory. The following two invocations are equivalent when the input is `C:\study`:

```
dicomtool modify input:C:\study output:deidentified
dicomtool modify input:C:\study output:C:\study\deidentified
```


### 8.16  Managing Tag Aliases

```
# Add aliases
dicomtool tags add PatientName 0010,0010
dicomtool tags add InstitutionName 0008,0080

# List all aliases
dicomtool tags list

# Remove an alias
dicomtool tags remove InstitutionName
```


### 8.17  Creating and Using a Profile

```
# Create a profile
dicomtool profiles add anonymize
    set:PatientName=ANON
    set:PatientID=ANON
    set:AccessionNumber=
    dob:YYYY0101
    noprivate:true
    maskrows:20

# List all profiles
dicomtool profiles list

# Inspect the profile definition
dicomtool profiles show anonymize

# Apply it
dicomtool modify input:C:\study output:C:\out profile:anonymize

# Remove the profile
dicomtool profiles remove anonymize
```


### 8.18  Using Profile Inheritance

Create a base de-identification profile, then derive a study-specific variant that inherits all base settings but overrides the Patient ID:

```
# Base profile
dicomtool profiles add anonymize
    set:PatientName=ANON
    set:PatientID=ANON
    set:AccessionNumber=
    dob:YYYY0101
    noprivate:true

# Derived profile -- inherits everything from anonymize,
# but assigns a specific Patient ID for this study
dicomtool profiles add study42 base:anonymize set:PatientID=STUDY0042

# Applying study42 is equivalent to applying anonymize
# with set:PatientID=STUDY0042 overriding the ANON value
dicomtool modify input:C:\study output:C:\out profile:study42
```


### 8.19  Packaging Output as a ZIP Archive

Use `zip:true` to write all processed DICOM files into a single ZIP archive instead of an output directory. The archive preserves the original folder structure of the input tree.

```
dicomtool modify input:C:\study output:C:\out\study.zip zip:true
    set:PatientName=ANON noprivate:true
```

If the `output:` path does not end in `.zip`, the extension is appended automatically:

```
dicomtool modify input:C:\study output:C:\out\study zip:true set:PatientName=ANON
# ZIP is written to C:\out\study.zip
```

On completion, the full path to the ZIP file is always printed:

```
42 file(s) written to: C:\out\study.zip
```

With `verbose:true`, each entry is listed as it is added:

```
  set 0010,0010 = "ANON"
zipped: series1\CT.1.2.3.dcm
...
```


#### Notes

- `zip:true` and `dicomdir:true` cannot be combined. Use one or the other.
- Each ZIP entry carries the creation timestamp of the run, so extracted files have normal filesystem date attributes.
- The internal file paths within the ZIP use forward slashes and are relative to the input directory root. With `remapuids:true`, UIDs in those paths are replaced with their new values (see Section 9.4).

### 8.20  Handling Tags with Incorrect Value Representations

Some DICOM files contain tags whose stored Value Representation (VR) does not match the DICOM standard. This can occur when equipment vendors write non-conformant files, or when files have been processed by third-party tools that do not validate VRs. By default, dicomtool will return an error when it tries to write such a file. The `fixvr:` parameter controls how these tags are handled.

Attempt to re-encode each mismatched tag under its correct standard VR. If the re-encoding fails (for example, because the stored bytes cannot be interpreted as the expected type), the tag is removed and reported when `verbose:true` is set:

```
dicomtool modify input:C:\study output:C:\out fixvr:correct set:PatientName=ANON
```

Silently remove all tags with an incorrect VR without attempting correction:

```
dicomtool modify input:C:\study output:C:\out fixvr:skip set:PatientName=ANON
```

Preserve the file exactly as-is, writing the incorrect VR to the output without raising an error:

```
dicomtool modify input:C:\study output:C:\out fixvr:passthrough set:PatientName=ANON
```


#### Modes

| Mode | Behaviour |
|---|---|
| `correct` | Re-encodes the tag value under the standard VR. Supports string, integer, and float conversions. Tags that cannot be converted are removed. Private and unknown tags are kept unchanged. |
| `skip` | Removes any tag whose VR does not match the standard dictionary. Private and unknown tags are kept unchanged. |
| `passthrough` | Disables VR verification on write. Tags are written to the output file exactly as parsed, preserving the incorrect VR. |


#### Notes

- Only tags present in the DICOM standard dictionary are checked. Private tags (odd group numbers) and tags not found in the dictionary are always kept.
- `fixvr` applies recursively to all elements, including those nested within sequence (SQ) items at any depth.
- With `verbose:true`, each affected tag is reported with its original and target VR.
- `fixvr:correct` is the safest option for most non-conformant files. Use `fixvr:passthrough` only when you need to preserve the original encoding exactly.
- `fixvr` can be set in a processing profile via the `fixvr` key (see Section 6).

### 8.21  Parallel Processing

By default, dicomtool processes files using all available CPU cores simultaneously. For large studies this can significantly reduce total run time compared to serial processing.

Process files using 8 worker goroutines:

```
dicomtool modify input:C:\study output:C:\out workers:8 set:PatientName=ANON
```

Use all available CPU cores (default behaviour):

```
dicomtool modify input:C:\study output:C:\out set:PatientName=ANON
```

Process files one at a time (serial, equivalent to pre-1.1.1 behaviour):

```
dicomtool modify input:C:\study output:C:\out workers:1 set:PatientName=ANON
```


#### Notes

- Setting `workers:0` is equivalent to omitting the parameter -- the default (CPU core count) is used.
- When writing to a ZIP archive (`zip:true`), file processing is parallel but archive writes are serialised internally, so the output ZIP is always valid.
- With `verbose:true`, output lines from different workers may be interleaved. The final summary count and any errors are always accurate regardless of worker count.
- Setting `workers:` higher than the number of files in the input tree has no effect -- the pool is capped at the job count automatically.

### 8.23  Resetting Configuration to Defaults

To restore both `tags.json` and `profiles.json` to their factory defaults, overwriting any existing customisations:

```
dicomtool install
```

Sample output:

```
written: C:\Users\username\.dicomtool\tags.json
written: C:\Users\username\.dicomtool\profiles.json
```


### 8.24  Verbose Mode

With `verbose:true`, each written file path, per-operation diagnostics, and a summary count are printed to stdout:

```
dicomtool modify input:C:\study output:C:\out set:PatientName=ANON maskrows:10 verbose:true
```

Sample output:

```
  set 0010,0010 = "ANON"
  maskrows: zeroed top 10 row(s) across 1 frame(s)
written: C:\out\series1\image001.dcm
3 file(s) processed
```

Without `verbose:true` only errors and warnings are shown.


## 9  Tag Format Reference


### 9.1  GGGG,EEEE Format

DICOM tags are identified by a group number and an element number, both 16-bit hex values written as `GGGG,EEEE`: four hex digits each, with leading zeros, such as `0008,0080`. This is the form used throughout this manual and the built-in configuration files, and the recommended form for command lines and profiles. Both components are required; for compatibility with older profiles, dicomtool also accepts a component written with its leading zeros omitted.

```
0010,0010   Patient Name
0010,0020   Patient ID
0008,0050   Accession Number
0008,0060   Modality
0020,000D   Study Instance UID
0020,000E   Series Instance UID
0008,0018   SOP Instance UID
```


### 9.2  Commonly Used Tags

| Tag | Name |
|---|---|
| 0008,0020 | Study Date |
| 0008,0030 | Study Time |
| 0008,0050 | Accession Number |
| 0008,0060 | Modality |
| 0008,0070 | Manufacturer |
| 0008,0080 | Institution Name |
| 0008,0081 | Institution Address |
| 0008,0090 | Referring Physician Name |
| 0008,103E | Series Description |
| 0008,1030 | Study Description |
| 0010,0010 | Patient Name |
| 0010,0020 | Patient ID |
| 0010,0030 | Patient Birth Date |
| 0010,0040 | Patient Sex |
| 0010,1010 | Patient Age |
| 0018,1030 | Protocol Name |
| 0020,000D | Study Instance UID |
| 0020,000E | Series Instance UID |
| 0020,0010 | Study ID |
| 0020,0011 | Series Number |
| 0020,0013 | Instance Number |


### 9.3  Private Tags

Tags whose group number is odd (e.g. `0009,xxxx`, `0019,xxxx`) are private tags used by specific vendors or applications. They are not standardised. The `noprivate:true` flag removes all such tags from every output file.


### 9.4  UID Remapping

`remapuids:true` replaces each UID with a freshly generated value. Remapping uses a single shared table for the entire run, so the same source UID always maps to the same new UID in every file. This preserves the study/series/instance hierarchy and every internal reference (for example `ReferencedSOPInstanceUID` values nested inside sequences continue to point at the correct, remapped instances) while severing any link back to the originating system. New UIDs use the ISO `2.25` UUID-derived root and are unique per run; re-running the command produces a different set of UIDs.

UIDs beginning with the DICOM standard root `1.2.840.10008.` (SOP Class UIDs, Transfer Syntax UIDs, and other well-known values), together with the Implementation Class UID (0002,0012), are never remapped because they identify the object type, encoding, and creating software rather than the patient or study. The following UID tags are excluded by tag as well, whatever their value:

| Tag | Name |
|---|---|
| 0002,0010 | Transfer Syntax UID |
| 0002,0012 | Implementation Class UID |
| 0004,1512 | Referenced Transfer Syntax UID in File |


#### Output File and Folder Names

Source trees are often named after their UIDs — a folder per Study Instance UID, a subfolder per Series Instance UID, and a file per SOP Instance UID. Remapping the values inside each file would leave those names behind, so the output path of every file is rewritten as well: each UID in a file or folder name that the file itself carried, and that was remapped, is replaced with its new value. The rewritten path is used for the output directory, for ZIP entry names, and for the DICOMDIR's referenced file IDs.

- A UID is matched only as whole dot-separated components, wherever it appears in a name: `1.2.3.dcm`, an extensionless `1.2.3`, `CT.1.2.3.dcm`, and `1.2.3.12.dcm` all have `1.2.3` replaced, while `1.2.34.dcm` is left alone. Where one UID prefixes another, the longer match wins.
- Names that contain no remapped UID, such as `IM0001` or `Series 2`, are kept unchanged.
- Only the UIDs a file itself holds are used for its path. Every file in a study folder carries that study's UID, so they all land in the same renamed folder. A file that lacks the UID its folder is named after — for example, a damaged file with no Study Instance UID — keeps that folder's original name.
The `uid:<suffix>` parameter, which appended a suffix to every UID and left the original readable inside the result, was removed in version 2.0.0. A run or profile that still uses it is refused; see the modify command's "Removed: uid:<suffix>" note in Section 4.2.


### 9.5  Filtering by Modality and Image Type

Files can be excluded from processing based on their Modality (0008,0060) or Image Type (0008,0008) tags using the `ignoremodality:` and `ignoretype:` parameters. Both accept comma-delimited lists and perform case-insensitive comparisons.

Common use cases:

| Example | Effect |
|---|---|
| `ignoremodality:SC` | Skip Secondary Capture files (screen captures, annotation overlays) |
| `ignoremodality:PR` | Skip Presentation State objects |
| `ignoremodality:SC,PR,REG` | Skip multiple modality types in one pass |
| `ignoretype:SECONDARY` | Skip files with SECONDARY in their Image Type field |
| `ignoretype:DERIVED,SECONDARY` | Skip derived or secondary image types |


## 10  Output and Error Behaviour

| Condition | Behaviour |
|---|---|
| Non-DICOM file encountered in input tree | Silently skipped |
| File matches `ignoremodality:` or `ignoretype:` value | Skipped and not written to output (logged with `verbose:true`) |
| Output directory does not exist | Created automatically (including intermediate directories) |
| Tag not present in source file | For `set:` operations the tag is inserted; for `remove:` it is a no-op |
| Relative `output:` path | Resolved relative to the `input:` directory |
| `tags.json` has invalid JSON | Error printed to stderr; aliases treated as empty |
| `profiles.json` has invalid JSON and `profile:` is given | Error returned; the command stops before any file is processed |
| Named profile not found | Error returned; the command stops before any file is processed |
| Base profile not found during inheritance resolution | Error returned; the command stops before any file is processed |
| Circular base reference in profile chain | Error returned; the command stops before any file is processed |
| Empty `profile:` value, or more than one `profile:` | Error returned; the command stops before any file is processed |
| `maskrows:` on compressed pixel data | Frame skipped; warning printed when `verbose:true` |
| `inspect` file parse error | Error printed per file; remaining files continue to be processed |
| `inspect` tag not found in file | `not found` printed for that tag; remaining tags continue |
| `install` config directory does not exist | Directory created automatically before writing files |
| `zip:true` with `dicomdir:true` | Error returned; the two options cannot be combined |
| `zip:true` output path has no `.zip` extension | `.zip` is appended automatically |
| `remapuids:true` and a file or folder name contains a remapped UID | The name is rewritten with the new UID in the output directory, ZIP entry, and DICOMDIR |
| `uid:` supplied, or the profile (or a base or per-modality entry) carries `uid` | Error returned before any output is written; use `remapuids:true` instead |


---


## Appendix A  Default Configuration Files

The following files are written to `~/.dicomtool/` on the first run of any `dicomtool` command, and can be restored at any time with `dicomtool install`.


### A.1  tags.json

Maps short alias names to DICOM tag identifiers. Any alias defined here can be used in place of a raw `GGGG,EEEE` identifier on the command line or in a profile.

```
{
  "TransferSyntaxUID":          "0002,0010",
  "ReferencedTransferSyntaxUID": "0004,1512",
  "StudyDate":                  "0008,0020",
  "StudyTime":                  "0008,0030",
  "AccessionNumber":            "0008,0050",
  "Modality":                   "0008,0060",
  "Manufacturer":               "0008,0070",
  "InstitutionName":            "0008,0080",
  "InstitutionAddress":         "0008,0081",
  "ReferringPhysicianName":     "0008,0090",
  "SeriesDescription":          "0008,103E",
  "StudyDescription":           "0008,1030",
  "PatientName":                "0010,0010",
  "PatientID":                  "0010,0020",
  "PatientDOB":                 "0010,0030",
  "PatientSex":                 "0010,0040",
  "PatientAge":                 "0010,1010",
  "ProtocolName":               "0018,1030",
  "StudyInstanceUID":           "0020,000D",
  "SeriesInstanceUID":          "0020,000E",
  "StudyID":                    "0020,0010",
  "SeriesNumber":               "0020,0011",
  "InstanceNumber":             "0020,0013",
  "ConfidentialityCode":        "0040,1008"
}
```


### A.2  profiles.json

Defines the built-in profiles, reproduced below from the file shipped with this version, with tag lists shown several to a line. `base-deident` is a comprehensive de-identification baseline: it sets patient identity fields to anonymous values, masks the date of birth to year only, removes all private tags, corrects non-standard Value Representations, remaps every study/series/instance UID consistently, and removes 132 tags commonly associated with identifying information.

```
{
  "base-deident": {
    "set": [
      "PatientName=ANON",
      "PatientID=ANON",
      "AccessionNumber=",
      "ConfidentialityCode=Y"
    ],
    "remove": [
      "0008,0080", "0008,0081", "0008,0090", "0008,0092", "0008,0094",
      "0008,1010", "0008,1032", "0008,1040", "0008,1048", "0008,1050",
      "0008,1060", "0008,1070", "0008,1080", "0008,1100", "0008,1110",
      "0010,0021", "0010,0022", "0010,0024", "0010,1000", "0010,1001",
      "0010,1002", "0010,1005", "0010,1010", "0010,1040", "0010,1050",
      "0010,1060", "0010,1080", "0010,1090", "0010,2000", "0010,2110",
      "0010,2150", "0010,2152", "0010,2154", "0010,2155", "0010,2297",
      "0010,2298", "0010,2299", "0010,3020", "0010,4000", "0010,21B0",
      "0010,21F0",
      "0018,1400", "0018,1401",
      "0020,4000",
      "0028,0300",
      "0032,1031", "0032,1032", "0032,1033",
      "0038,0010", "0038,0300", "0038,0400", "0038,0500", "0038,4000",
      "0040,0275", "0040,1001", "0040,1002", "0040,1004", "0040,1005",
      "0040,2008", "0040,2009", "0040,2010", "0040,2011", "0040,2016",
      "0040,2017", "0040,2400", "0040,4006", "0040,4009", "0040,4010",
      "0040,4020", "0040,4021", "0040,A057", "0040,A060", "0040,A066",
      "0040,A067", "0040,A068", "0040,A070", "0040,A073", "0040,A075",
      "0040,A078", "0040,A123", "0040,A160", "0040,A730",
      "0050,0010",
      "0070,0001", "0070,0002", "0070,0003", "0070,0004", "0070,0005",
      "0070,0006", "0070,0008", "0070,0009", "0070,0010", "0070,0011",
      "0070,0012", "0070,0013", "0070,0014", "0070,0080", "0070,0081",
      "0070,0082", "0070,0083", "0070,0084", "0070,0207", "0070,0208",
      "0070,0209", "0070,0303",
      "0072,0002", "0072,0004", "0072,0006", "0072,0008", "0072,000A",
      "0072,000C", "0072,000E", "0072,0010",
      "0400,0100", "0400,0105", "0400,0110", "0400,0115", "0400,0120",
      "0400,0402", "0400,0403", "0400,0404", "0400,0561", "0400,0562",
      "0400,0563", "0400,0564", "0400,0565",
      "2030,0020",
      "2110,0010", "2110,0020", "2110,0030",
      "2200,0001", "2200,0002"
    ],
    "dob": "YYYY0101",
    "noprivate": true,
    "fixvr": "correct",
    "remapuids": true
  }
}
```


---


## Credits

dicomtool is a human–AI collaboration. Credit is given by role, reflecting how the work was actually divided.


### Architecture & Design

Jeffrey Leal <jeffrey.leal@gmail.com>

https://github.com/jeffrey-leal


### Implementation

Claude by Anthropic (https://anthropic.com)

Application code and documentation


### DICOM Standard Reference

Protocol implementation and data dictionary usage follow the DICOM Standard published by NEMA:

DICOM PS3 (2024b) — https://dicom.nema.org/medical/dicom/current


### Open-Source Libraries

| Library | Purpose |
|---|---|
| `github.com/spf13/cobra` v1.10.2 | CLI framework — command routing, flag parsing, help generation |
| `github.com/suyashkumar/dicom` v1.1.0 | DICOM file parsing, data dictionary, and element model |

A full list of all transitive dependencies and their versions is recorded in `go.sum`.

