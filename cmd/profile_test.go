package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jeffr/dicomtool/cmd/defaults"
)

// loadDefaultProfiles parses the profiles.json embedded in the binary.
func loadDefaultProfiles(t *testing.T) ProfileConfig {
	t.Helper()
	var cfg ProfileConfig
	if err := json.Unmarshal(defaults.Profiles, &cfg); err != nil {
		t.Fatalf("default profiles.json does not parse: %v", err)
	}
	return cfg
}

func TestDefaultProfiles_FourDigitTags(t *testing.T) {
	canonical := regexp.MustCompile(`^[0-9A-F]{4},[0-9A-F]{4}$`)
	check := func(where string, p Profile) {
		for _, list := range []struct {
			field string
			refs  []string
		}{{"remove", p.Removes}, {"keep", p.Keep}} {
			for _, ref := range list.refs {
				if !canonical.MatchString(ref) {
					t.Errorf("%s %s entry %q is not in four-digit GGGG,EEEE form", where, list.field, ref)
				}
			}
		}
		for _, s := range p.Sets {
			// A set entry names its tag by alias or by GGGG,EEEE; only the
			// latter has a comma, and it must be canonical too.
			if ref, _, _ := strings.Cut(s, "="); strings.Contains(ref, ",") && !canonical.MatchString(ref) {
				t.Errorf("%s set entry %q is not in four-digit GGGG,EEEE form", where, s)
			}
		}
	}
	for name, p := range loadDefaultProfiles(t) {
		check("profile "+name, p)
		for mod, mp := range p.PerModality {
			check("profile "+name+" per-modality "+mod, mp)
		}
	}
}

// TestDefaultProfiles_Valid guards hand edits to the shipped profiles.json:
// every profile must resolve (base chain intact) and every tag it names — raw,
// or a set entry's alias from the shipped tags.json — must parse. The manual
// generator also requires base-deident, which Appendix A.2 describes.
func TestDefaultProfiles_Valid(t *testing.T) {
	cfg := loadDefaultProfiles(t)
	if _, ok := cfg["base-deident"]; !ok {
		t.Fatal("default profiles.json has no base-deident profile")
	}

	t.Cleanup(func() { Opts = Options{} })
	Opts = Options{}
	if err := json.Unmarshal(defaults.Tags, &Opts.TagAliases); err != nil {
		t.Fatalf("default tags.json does not parse: %v", err)
	}
	checkRefs := func(where string, p Profile) {
		refs := append(append([]string{}, p.Removes...), p.Keep...)
		for _, s := range p.Sets {
			ref, _, _ := strings.Cut(s, "=")
			refs = append(refs, ref)
		}
		for _, ref := range refs {
			if _, err := parseTagString(Opts.TagAliases.Resolve(ref)); err != nil {
				t.Errorf("%s: tag %q does not parse and is not a default alias: %v", where, ref, err)
			}
		}
	}
	for name := range cfg {
		p, err := resolveProfile(name, cfg)
		if err != nil {
			t.Errorf("profile %s does not resolve: %v", name, err)
			continue
		}
		checkRefs("profile "+name, p)
		for mod, mp := range p.PerModality {
			checkRefs("profile "+name+" per-modality "+mod, mp)
		}
	}
}

func TestParseArgs_UnusableProfileStopsTheCommand(t *testing.T) {
	home := useTempHome(t)
	store := filepath.Join(home, ".dicomtool", "profiles.json")
	tests := []struct {
		name     string
		profiles string // profiles.json content; "" lets parseArgs seed the defaults
		args     []string
		want     string
	}{
		{"unknown profile", "", []string{"profile:base-deident-keep-order"}, `profile "base-deident-keep-order" not found in`},
		{"empty name", "", []string{"profile:"}, "profile: requires a profile name"},
		{"more than one", "", []string{"profile:base-deident", "profile:other"}, "only one profile: may be given"},
		{"unknown base", `{"child": {"base": "gone"}}`, []string{"profile:child"}, `profile "gone" not found`},
		{"circular base", `{"a": {"base": "b"}, "b": {"base": "a"}}`, []string{"profile:a"}, "circular base reference"},
		{"invalid profiles.json", `{"a": `, []string{"profile:a"}, "could not load"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Remove(store)
			if tt.profiles != "" {
				if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(store, []byte(tt.profiles), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := parseArgs(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseArgs(%q) error = %v, want one containing %q", tt.args, err, tt.want)
			}
		})
	}

	t.Run("known profile still applies", func(t *testing.T) {
		_ = os.Remove(store)
		if err := parseArgs([]string{"profile:base-deident"}); err != nil {
			t.Fatalf("parseArgs: %v", err)
		}
		if !boolParam("remapuids", false) {
			t.Fatal("base-deident was not merged into the parameters")
		}
	})
}

func TestModify_UnknownProfileWritesNothing(t *testing.T) {
	useTempHome(t)
	in := t.TempDir()
	writeTestDICOM(t, filepath.Join(in, "image.dcm"), "1.2.3", "1.2.3.4", "1.2.3.4.5")
	out := filepath.Join(t.TempDir(), "out")

	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	rootCmd.SetArgs([]string{"modify", "input:" + in, "output:" + out, "profile:base-deident-keep-order", "set:PatientName=X"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("modify with an unknown profile returned %v, want a profile-not-found error", err)
	}
	if _, serr := os.Stat(out); !os.IsNotExist(serr) {
		t.Fatalf("modify with an unknown profile created output %q", out)
	}
}

func TestMergeProfiles_KeepMatchesByTagIdentity(t *testing.T) {
	t.Cleanup(func() { Opts = Options{} })
	Opts = Options{TagAliases: TagConfig{"InstitutionName": "0008,0080"}}
	base := Profile{Removes: []string{"0040,0275", "0008,0080", "0010,1000"}}
	child := Profile{Keep: []string{"40,275", " InstitutionName "}}

	got := mergeProfiles(base, child).Removes
	if len(got) != 1 || got[0] != "0010,1000" {
		t.Fatalf("merged removals = %v, want only 0010,1000: a short-form keep and an alias keep must each cancel the base removal of the same tag", got)
	}
}
