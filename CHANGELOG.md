# Changelog

## v1.4.3

### Changed

#### Reduced Default `base-deident` Removal List
- The built-in `base-deident` profile no longer removes 30 group `0040` tags: order-entry fields (`OrderEnteredBy`, `OrderCallbackPhoneNumber`, `OrderCallbackTelecomInformation`, `PlacerOrderNumberImagingServiceRequest`, `FillerOrderNumberImagingServiceRequest`, `ImagingServiceRequestComments`), requested-procedure fields (`RequestAttributesSequence`, `RequestedProcedureID`, `ReasonForTheRequestedProcedure`, `PatientTransportArrangements`, `RequestedProcedureLocation`), performer/scheduling fields (`MultipleCopiesFlag`, `HumanPerformerCodeSequence`, `ScheduledProcedureStepModificationDateTime`, `InputAvailabilityFlag`, `InputInformationSequence`), and Structured Report content tags (`VerifyingObserverSequence`, `VerifyingObserverName`, `AuthorObserverSequence`, `PersonName`, `TextValue`, `ContentSequence`, and several retired trial identifier tags).
- The default removal list now covers 103 tags (previously over 130). Note that some of the removed entries (e.g. `VerifyingObserverName`, SR `PersonName`, SR `TextValue`) can carry identifying information — review the updated default against your de-identification requirements before relying on it.
- Existing `~/.dicomtool/profiles.json` files are not modified automatically; run `dicomtool install` to refresh the defaults.

---

## v1.4.2

### Changed

#### Branded DOCX Cover, Header, and Footer
- The generated DOCX user manual now has an IRAT-branded title page: the program logo above the title, the generation date (formatted `Month D, YYYY`) between the subtitle and description, and the Image Response Assessment Team institutional address at the bottom — all centered horizontally and vertically on the page.
- Content pages now carry a running header (`dicomtool` at the left, `Page: N` at the right, with a rule beneath) and footer (the NIH/NCI grant support line, with a rule above). Page numbering starts at 1 on the first content page; the cover page is unnumbered and has no header or footer.
- The Markdown manual is unchanged apart from the added generation date on its cover.

#### Build Script Generates Documentation
- `build-release.sh` now runs the documentation generator (`go run ./gendoc`) before compiling, so the manual is regenerated as part of a release build.

---

## v1.4.1

### Changed

#### Default `base-deident` Profile Now Remaps UIDs
- The built-in `base-deident` de-identification profile now sets `remapuids: true`, so every study, series, and instance UID (and all references to them) is replaced with a freshly generated UID by default, consistently across the run. This closes a residual re-identification vector — the original UIDs are no longer embedded in de-identified output.
- Existing `~/.dicomtool/profiles.json` files are not modified automatically; run `dicomtool install` to refresh the defaults, or add `"remapuids": true` to your profile manually.

---

## v1.4.0

### New Features

#### Resilient Batch Processing (Continue on Error)
- `modify` no longer aborts the entire run when a single file cannot be read, parsed, or written. Failing files are recorded and skipped, and processing continues with the rest of the batch.
- A summary is always printed in the form `N file(s) processed, M file(s) failed` (the failed clause appears only when at least one file failed).
- When any file fails, the command now exits with a non-zero status so scripts and pipelines can detect partial failures. Usage text is suppressed on a post-run failure so the output stays clean.

#### Error Log File (`errorlog`)
- Added `errorlog:txt|csv|json` to the `modify` command. On failure, the detailed per-file error messages are written to a file named `ERROR.<ext>` in the root of the output directory (for ZIP output, alongside the archive) instead of the console.
- Without `errorlog`, the detailed messages print to the console after the summary, as before.
- Formats: `txt` (plain text with a `processed`/`failed` header and one `file: error` line per failure), `csv` (a `file,error` header row plus one properly-quoted row per failure), and `json` (a machine-readable object with `processed`/`failed` counts and an `errors` array). The file is created only when at least one file failed.

---

## v1.3.1

### Performance

No behaviour or output changes — internal efficiency only.

#### Single Read Per File
- The `modify` file walk no longer pre-validates each file with a separate read. File enumeration is now a pure directory walk; the worker that processes a file is the sole reader. This eliminates a redundant open-read-close on every file in the input tree (previously each file was opened twice), most noticeable on large or networked trees.

#### DICOMDIR Built From In-Memory Datasets
- When `dicomdir:true` is set, the directory index is now built from the already-parsed, transformed datasets collected during the modify pass, rather than re-walking and re-parsing the entire output tree afterward.
- On a 441-file study this reduced `dicomdir:true` runtime from ~6.6 s to ~2.1 s (about 3× faster). The generated DICOMDIR is structurally identical to before.

#### Faster DICOMDIR Item-Marker Scan
- `findSequenceItemPositions` now uses the optimized `bytes.Index` scanner instead of a per-byte comparison loop.

---

## v1.3.0

### New Features

#### Consistent UID Remapping (`remapuids`)
- Added `remapuids:true` to the `modify` command. It replaces every study, series, and instance UID — and all references to them — with a freshly generated UID.
- Remapping is consistent across the entire run: the same source UID always maps to the same new UID in every file, so the study/series/instance hierarchy and all internal cross-references (including `ReferencedSOPInstanceUID` values nested in sequences) are preserved, while linkage back to the source system is severed.
- SOP Class UIDs, Transfer Syntax UIDs (and the Implementation Class UID) are never remapped, so output files remain valid. New UIDs use the ISO `2.25` UUID-derived root.
- Unlike `uid:<suffix>` (which appends a reversible suffix), `remapuids` produces irreversible, per-run-random UIDs — the stronger choice for de-identification. The two parameters are mutually exclusive.
- Available as a profile field (`"remapuids": true`) as well as on the command line.

### Bug Fixes

#### De-identification Now Reaches Nested Sequences
- `remove`, `noprivate`, and `set` previously operated only on top-level dataset elements. Identifiers nested inside sequences (e.g. `OtherPatientIDsSequence`, Request/Original Attributes Sequences) survived a removal or overwrite the user expected to apply everywhere — a genuine PHI leak.
- All three operations now recurse into sequences at every nesting depth, matching the behaviour of `fixvr`. `remove` and `noprivate` strip matching/private tags wherever they appear; `set` overwrites every nested occurrence (and still appends once at the top level when the tag is absent, without injecting it into sequence items).
- This is on by default; no new parameter is required.

### Performance

#### Map-Based Tag Removal
- The `remove` operation previously scanned every element against the full removal list (O(elements × removals)). It now uses a single map lookup (O(elements)), a clear win for large removal lists such as the `base-deident` profile's ~130 tags.

---

## v1.2.0

### New Features

#### Per-Modality Profile Overrides (`per-modality`)
- Profiles now support a `per-modality` key containing a map of DICOM Modality values (e.g. `"CT"`, `"MR"`, `"PT"`) to override settings applied only when a file of that modality is processed.
- Override settings layer additively on top of the base profile: `set` entries win per tag, `remove` is unioned, scalars (`dob`, `uid`, `fixvr`, `maskrows`) replace the base value when specified, and `noprivate` is OR'd.
- Modality keys are matched case-insensitively.

#### Keep List (`keep`)
- Profiles and per-modality entries now support a `keep` field — a list of tags that are **excluded** from the removal list for that scope.
- At the profile inheritance level, a derived profile can use `keep` to restore tags that a parent profile removes, without needing to re-specify the entire removal list.
- At the per-modality level, `keep` allows a specific modality to retain tags that the base profile removes globally (e.g. keep Image Type for MR while removing it for all other modalities).

#### Keep Private Tags for Specific Modalities (`keepprivate`)
- Per-modality entries (and derived profiles) support `keepprivate: true`, which suppresses `noprivate` for files of that modality.
- Enables workflows where private tags are stripped globally but preserved for specific modalities that carry diagnostically relevant vendor data in private sequences (e.g. PET reconstruction parameters).

### Bug Fixes

#### Stale `"convert"` References in Test Suite
- `root_test.go` referenced command name `"convert"` which was renamed to `"modify"` in an earlier release. Both failing tests now use the correct command name.

---

## v1.1.4

### Added

#### MSYS2 MinGW64 Build Scripts
- Added `build-release.sh` — cross-compiles for all four target platforms (Windows amd64, Linux amd64, macOS x64, macOS arm64) with `-ldflags="-s -w"` to strip debug symbols. Requires MSYS2 MinGW64 shell.
- Added `build-debug.sh` — Windows-only debug build (no strip flags), for use during development.

---

## v1.1.3

### Improvements

#### Extended Command-Line Help (`inspect`, `modify`)
- Added detailed `Long` help descriptions to the `inspect` and `modify` commands, displayed when `--help` or `-h` is passed.
- `inspect --help` now documents all parameters, the output line format, sequence expansion behaviour, and usage examples.
- `modify --help` now documents all parameters with inline descriptions, the processing order, and usage examples.

---

## v1.1.2

### Bug Fixes

#### `fixvr` Now Applies Recursively to Sequence Elements
- `fixvr:correct` and `fixvr:skip` previously processed only top-level dataset elements. Tags nested inside sequence (SQ) items at any depth were not visited, so `dicomio.verifyElement` errors could still occur during write even when `fixvr:` was specified.
- The pre-processing step now recurses fully into sequence items and applies the same correction or removal logic to every nested element, regardless of nesting depth.
- As an additional safety net, the writer now also skips VR verification for `correct` and `skip` modes, covering any edge cases (such as elements inside private sequences) that the pre-processing walk cannot reach.

#### `fixvr` in Processing Profiles Was Silently Ignored
- The `Profile` struct was missing a `FixVR` field, causing the `"fixvr"` key in `profiles.json` to be silently discarded when the file was unmarshalled.
- Fixed by adding the `FixVR string` field (JSON key `"fixvr"`) to the struct, wiring it through `mergeProfile` (CLI takes precedence over profile) and `mergeProfiles` (derived profile wins over base profile when non-empty).

### Configuration

#### Updated Default `profiles.json`
- The built-in `base-anon` profile has been updated with a revised tag removal list reflecting current de-identification requirements.
- The default `fixvr` mode for `base-anon` is now `"correct"` (was `"passthrough"`), so non-conformant VRs are automatically re-encoded rather than passed through unchanged.

---

## v1.1.1

### New Features

#### VR Correction and Skipping (`fixvr:`)
- Added `fixvr:` parameter to the `modify` command for handling DICOM files that contain tags whose stored Value Representation does not match the standard dictionary. Accepts three modes:
  - `correct` — attempts to re-encode the tag value under the correct standard VR. Supports string, integer, and float conversions. Tags that cannot be converted fall back to removal.
  - `skip` — silently removes all tags with a non-conformant VR from the output.
  - `passthrough` — disables VR verification on write, preserving the incorrect VR in the output file without raising an error.
- Private tags and tags not found in the standard dictionary are always kept unchanged under all modes.
- With `verbose:true`, each affected tag is reported with its original and corrected VR.

#### Parallel File Processing (`workers:`)
- The `modify` command now processes files concurrently using a worker pool, significantly reducing total run time on large directory trees.
- The number of workers defaults to the number of logical CPU cores (`runtime.NumCPU()`).
- The new `workers:<n>` parameter explicitly sets the pool size. `workers:1` restores serial behaviour. `workers:0` restores the default.
- Processing is split into two phases: a fast serial directory walk to collect file paths, followed by parallel parse-edit-write for each file.
- When writing to a ZIP archive (`zip:true`), file processing is parallel but archive writes are serialised internally, ensuring a valid output ZIP.

---

## v1.1.0

### New Features

#### ZIP Archive Output (`zip:true`)
- Added `zip:true` parameter to the `modify` command. When set, all processed DICOM files are packaged into a single ZIP archive instead of being written to an output directory.
- The `output:` path is used as the ZIP file name; a `.zip` extension is appended automatically if not already present.
- Each ZIP entry carries the timestamp of the run, ensuring extracted files have valid filesystem date attributes.
- `zip:true` and `dicomdir:true` cannot be combined.

#### File Filtering by Image Type (`ignoretype:`)
- Added `ignoretype:<values>` parameter to the `modify` command. Accepts a comma-delimited list of Image Type components (tag 0008,0008). Files whose Image Type contains any of the specified values are silently skipped and not written to output.
- Comparison is case-insensitive. Each backslash-delimited component of the Image Type field is checked individually.
- Example: `ignoretype:SECONDARY,DERIVED`

#### File Filtering by Modality (`ignoremodality:`)
- Added `ignoremodality:<values>` parameter to the `modify` command. Accepts a comma-delimited list of modality codes (tag 0008,0060). Files whose Modality matches any of the specified values are silently skipped.
- Comparison is case-insensitive.
- Example: `ignoremodality:SC,PR,SR`

#### Profile Base Inheritance (`base:<name>`)
- Profiles can now inherit the settings of an existing profile using the `base:<name>` parameter when calling `profiles add`.
- The base chain is resolved at runtime: the base profile's settings are applied first, then the derived profile's settings are merged on top.
- Chains may be arbitrarily deep. Circular references are detected and reported as an error.
- Merge rules: scalars (`dob`, `uid`) — derived wins if non-empty; integers (`maskrows`) — derived wins if greater than zero; booleans (`noprivate`, `dicomdir`, `verbose`) — OR'd; `set` — per-tag precedence (derived wins); `remove`, `ignoretype`, `ignoremodality` — deduplicated union.

#### `ignoretype` and `ignoremodality` in Profiles
- Profiles stored in `profiles.json` now support `ignoretype` and `ignoremodality` fields as JSON arrays.
- Values from both the CLI and the active profile are combined at runtime (additive union, deduplicated).

### Changes

#### `priv:true` Renamed to `noprivate:true`
- The parameter for removing private tags has been renamed from `priv:true` to `noprivate:true` for clarity.
- The corresponding profile JSON field has changed from `"priv"` to `"noprivate"`.

#### `noscreenshots:true` Removed
- The `noscreenshots:true` parameter has been removed. Its functionality is fully covered by the new `ignoretype:SECONDARY` and `ignoremodality:SC` parameters, which provide finer-grained control.

### Improvements

#### Configuration Error Reporting
- Errors loading `tags.json` or `profiles.json` (invalid JSON, missing named profile, circular base reference) are now always printed to stderr, regardless of the `verbose:` setting.
- Messages use the `error:` prefix rather than `warning:` to better reflect their impact.

#### Default `tags.json`
- The built-in default alias table has been expanded from 3 aliases to 24, covering the most commonly used patient, study, series, and instance tags.

#### Default `profiles.json`
- The built-in default profile has been replaced with `base-anon`, a comprehensive de-identification baseline that sets patient identity fields to anonymous values, masks the date of birth to year only, removes private tags, and removes over 130 tags commonly associated with identifying information. It also filters out Secondary Capture, Overlay, Presentation State, and Structured Report objects by default.

---

## v1.0.0

Initial release.
