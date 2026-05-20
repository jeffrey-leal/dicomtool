# Changelog

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
