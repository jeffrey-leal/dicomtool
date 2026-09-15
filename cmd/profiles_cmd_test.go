package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempHome points the profile store at a fresh home directory, so profiles
// add never reads or writes the real ~/.dicomtool/profiles.json, and resets the
// shared parse state afterwards.
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if p, err := DefaultProfilePath(); err != nil || !strings.HasPrefix(p, home) {
		t.Fatalf("profile store resolves to %q (err %v), not under the temporary home %q", p, err, home)
	}
	t.Cleanup(func() { parsed, Opts = nil, Options{} })
	Opts = Options{}
	return home
}

func TestProfilesAdd_KeepsRemapUIDsAndFixVR(t *testing.T) {
	home := useTempHome(t)
	parsed = map[string][]string{"remapuids": {"true"}, "fixvr": {"Correct"}, "noprivate": {"true"}}
	if err := profilesAddCmd.RunE(profilesAddCmd, []string{"deident"}); err != nil {
		t.Fatalf("profiles add: %v", err)
	}

	cfg, err := LoadProfileConfig(filepath.Join(home, ".dicomtool", "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg["deident"]
	if !ok {
		t.Fatalf("profile not saved; store holds %v", cfg)
	}
	if !p.RemapUIDs {
		t.Error("remapuids:true was not saved")
	}
	if p.FixVR != "correct" {
		t.Errorf("fixvr saved as %q, want %q", p.FixVR, "correct")
	}

	// Applying the saved profile hands both settings to modify.
	parsed = map[string][]string{}
	mergeProfile(p)
	if !boolParam("remapuids", false) || paramOne("fixvr") != "correct" {
		t.Fatalf("applied profile gave remapuids=%v fixvr=%q, want true and %q",
			boolParam("remapuids", false), paramOne("fixvr"), "correct")
	}
}

func TestProfilesAdd_RejectsInvalidFixVR(t *testing.T) {
	home := useTempHome(t)
	parsed = map[string][]string{"fixvr": {"repair"}}
	err := profilesAddCmd.RunE(profilesAddCmd, []string{"bad"})
	if err == nil || !strings.Contains(err.Error(), "must be correct, skip, or passthrough") {
		t.Fatalf("profiles add error = %v, want the fixvr validation error", err)
	}
	if _, serr := os.Stat(filepath.Join(home, ".dicomtool", "profiles.json")); !os.IsNotExist(serr) {
		t.Fatal("a rejected profile was written to the store")
	}
}

func TestProfilesAdd_RefusesUIDSuffix(t *testing.T) {
	useTempHome(t)
	parsed = map[string][]string{"uid": {"9999"}}
	err := profilesAddCmd.RunE(profilesAddCmd, []string{"legacy"})
	if err == nil || !strings.Contains(err.Error(), "use remapuids:true") {
		t.Fatalf("profiles add error = %v, want the uid: refusal", err)
	}
}
