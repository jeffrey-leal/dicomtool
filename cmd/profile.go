package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Profile holds a named collection of modify parameters that can be applied at
// runtime via profile:<name>. Fields map directly to the equivalent command-line
// parameters accepted by the modify command.
type Profile struct {
	Base             string   `json:"base,omitempty"`
	Sets             []string `json:"set,omitempty"`
	Removes          []string `json:"remove,omitempty"`
	DOB              string   `json:"dob,omitempty"`
	UIDSuffix        string   `json:"uid,omitempty"`
	Priv             bool     `json:"noprivate,omitempty"`
	Dicomdir         bool     `json:"dicomdir,omitempty"`
	Verbose          bool     `json:"verbose,omitempty"`
	MaskRows         int      `json:"maskrows,omitempty"`
	IgnoreTypes      []string `json:"ignoretype,omitempty"`
	IgnoreModalities []string `json:"ignoremodality,omitempty"`
	FixVR            string   `json:"fixvr,omitempty"`
}

// ProfileConfig maps profile names to their definitions.
type ProfileConfig map[string]Profile

// DefaultProfilePath returns ~/.dicomtool/profiles.json.
func DefaultProfilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dicomtool", "profiles.json"), nil
}

// LoadProfileConfig reads the profile store at path. A missing file returns an
// empty config without error.
func LoadProfileConfig(path string) (ProfileConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ProfileConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg ProfileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// SaveProfileConfig writes cfg to path, creating intermediate directories as needed.
func SaveProfileConfig(path string, cfg ProfileConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// resolveProfile returns the effective Profile for name after fully resolving its
// base chain. Circular references are detected and returned as an error.
func resolveProfile(name string, cfg ProfileConfig) (Profile, error) {
	return resolveProfileChain(name, cfg, make(map[string]bool))
}

func resolveProfileChain(name string, cfg ProfileConfig, visited map[string]bool) (Profile, error) {
	if visited[name] {
		return Profile{}, fmt.Errorf("circular base reference in profile %q", name)
	}
	visited[name] = true

	p, ok := cfg[name]
	if !ok {
		return Profile{}, fmt.Errorf("profile %q not found", name)
	}
	if p.Base == "" {
		return p, nil
	}

	base, err := resolveProfileChain(p.Base, cfg, visited)
	if err != nil {
		return Profile{}, err
	}
	return mergeProfiles(base, p), nil
}

// mergeProfiles returns a new Profile that represents base with override applied
// on top. Override wins for scalars and integers (when non-zero); booleans are
// OR'd; Sets use per-tag precedence (override wins); Removes are a union.
func mergeProfiles(base, override Profile) Profile {
	result := base

	if override.DOB != "" {
		result.DOB = override.DOB
	}
	if override.UIDSuffix != "" {
		result.UIDSuffix = override.UIDSuffix
	}
	if override.MaskRows > 0 {
		result.MaskRows = override.MaskRows
	}
	if override.FixVR != "" {
		result.FixVR = override.FixVR
	}

	result.Priv = base.Priv || override.Priv
	result.Dicomdir = base.Dicomdir || override.Dicomdir
	result.Verbose = base.Verbose || override.Verbose

	// Sets: override wins per tag; base contributes tags not in override.
	overrideTags := make(map[string]bool, len(override.Sets))
	for _, s := range override.Sets {
		if t, _, ok := strings.Cut(s, "="); ok {
			overrideTags[strings.ToLower(strings.TrimSpace(t))] = true
		}
	}
	result.Sets = make([]string, 0, len(base.Sets)+len(override.Sets))
	for _, s := range base.Sets {
		if t, _, ok := strings.Cut(s, "="); ok {
			if !overrideTags[strings.ToLower(strings.TrimSpace(t))] {
				result.Sets = append(result.Sets, s)
			}
		}
	}
	result.Sets = append(result.Sets, override.Sets...)

	// Removes: union, deduplicated.
	seen := make(map[string]bool, len(base.Removes)+len(override.Removes))
	result.Removes = nil
	for _, r := range append(base.Removes, override.Removes...) {
		if !seen[r] {
			seen[r] = true
			result.Removes = append(result.Removes, r)
		}
	}

	// IgnoreTypes: union, deduplicated (case-insensitive).
	seenT := make(map[string]bool, len(base.IgnoreTypes)+len(override.IgnoreTypes))
	result.IgnoreTypes = nil
	for _, v := range append(base.IgnoreTypes, override.IgnoreTypes...) {
		key := strings.ToLower(v)
		if !seenT[key] {
			seenT[key] = true
			result.IgnoreTypes = append(result.IgnoreTypes, v)
		}
	}

	// IgnoreModalities: union, deduplicated (case-insensitive).
	seenM := make(map[string]bool, len(base.IgnoreModalities)+len(override.IgnoreModalities))
	result.IgnoreModalities = nil
	for _, v := range append(base.IgnoreModalities, override.IgnoreModalities...) {
		key := strings.ToLower(v)
		if !seenM[key] {
			seenM[key] = true
			result.IgnoreModalities = append(result.IgnoreModalities, v)
		}
	}

	result.Base = "" // resolved profile carries no further base reference
	return result
}

// mergeProfile applies the profile's values into the shared parsed map, honouring
// CLI-first precedence:
//
//   - Scalar params (dob, uid): profile value is used only when the CLI did not
//     supply one.
//   - Boolean params (priv, dicomdir, verbose): either source can enable the flag.
//   - set: per-tag precedence — for each profile set entry, the tag component
//     (before '=') is compared against tags already present in the CLI set list;
//     profile entries whose tag already appears in the CLI list are discarded so
//     the CLI value wins.
//   - remove: additive union — both sources contribute.
func mergeProfile(p Profile) {
	// Scalar: CLI wins.
	if p.DOB != "" && len(parsed["dob"]) == 0 {
		parsed["dob"] = []string{p.DOB}
	}
	if p.UIDSuffix != "" && len(parsed["uid"]) == 0 {
		parsed["uid"] = []string{p.UIDSuffix}
	}

	// Boolean: either source can enable.
	if p.Priv && len(parsed["noprivate"]) == 0 {
		parsed["noprivate"] = []string{"true"}
	}
	if p.Dicomdir && len(parsed["dicomdir"]) == 0 {
		parsed["dicomdir"] = []string{"true"}
	}
	if p.Verbose && len(parsed["verbose"]) == 0 {
		parsed["verbose"] = []string{"true"}
	}
	// Scalar integer: CLI wins.
	if p.MaskRows > 0 && len(parsed["maskrows"]) == 0 {
		parsed["maskrows"] = []string{strconv.Itoa(p.MaskRows)}
	}
	if p.FixVR != "" && len(parsed["fixvr"]) == 0 {
		parsed["fixvr"] = []string{p.FixVR}
	}

	// set: build the set of tag identifiers already supplied by the CLI,
	// resolving any aliases on both sides so that e.g. CLI "PatientName" and
	// profile "0010,0010" are recognised as the same tag.
	if len(p.Sets) > 0 {
		cliTags := make(map[string]bool, len(parsed["set"]))
		for _, s := range parsed["set"] {
			if t, _, ok := strings.Cut(s, "="); ok {
				resolved := Opts.TagAliases.Resolve(strings.TrimSpace(t))
				cliTags[strings.ToLower(resolved)] = true
			}
		}
		for _, s := range p.Sets {
			t, value, ok := strings.Cut(s, "=")
			if !ok {
				continue
			}
			resolved := Opts.TagAliases.Resolve(strings.TrimSpace(t))
			if !cliTags[strings.ToLower(resolved)] {
				parsed["set"] = append(parsed["set"], resolved+"="+value)
			}
		}
	}

	// remove: resolve aliases, then union.
	for _, r := range p.Removes {
		resolved := Opts.TagAliases.Resolve(strings.TrimSpace(r))
		parsed["remove"] = append(parsed["remove"], resolved)
	}

	// ignoretype / ignoremodality: merge profile values into a single
	// comma-delimited string, deduplicating against any CLI-supplied values.
	if len(p.IgnoreTypes) > 0 {
		existing := make(map[string]bool)
		for _, v := range strings.Split(paramOne("ignoretype"), ",") {
			if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
				existing[v] = true
			}
		}
		combined := paramOne("ignoretype")
		for _, v := range p.IgnoreTypes {
			if !existing[strings.ToLower(strings.TrimSpace(v))] {
				if combined != "" {
					combined += ","
				}
				combined += v
			}
		}
		if combined != "" {
			parsed["ignoretype"] = []string{combined}
		}
	}

	if len(p.IgnoreModalities) > 0 {
		existing := make(map[string]bool)
		for _, v := range strings.Split(paramOne("ignoremodality"), ",") {
			if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
				existing[v] = true
			}
		}
		combined := paramOne("ignoremodality")
		for _, v := range p.IgnoreModalities {
			if !existing[strings.ToLower(strings.TrimSpace(v))] {
				if combined != "" {
					combined += ","
				}
				combined += v
			}
		}
		if combined != "" {
			parsed["ignoremodality"] = []string{combined}
		}
	}
}
