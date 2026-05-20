package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeffr/dicomtool/cmd/defaults"
)

// Options holds parsed global command-line parameters.
type Options struct {
	Verbose    bool
	Output     string
	Inputs     []string
	ConfigFile string
	TagAliases TagConfig
}

// Opts is the single shared instance populated during argument parsing.
var Opts Options

// parsed holds the raw key -> []value map from the current invocation.
var parsed map[string][]string

// parseArgs splits "key:value" args into a multi-map.
// Args without a colon are treated as bare input paths.
func parseArgs(args []string) error {
	parsed = make(map[string][]string)
	Opts = Options{}

	for _, arg := range args {
		key, value, found := strings.Cut(arg, ":")
		if !found {
			Opts.Inputs = append(Opts.Inputs, arg)
			continue
		}
		key = strings.ToLower(key)
		if key == "" {
			return fmt.Errorf("invalid parameter %q: key must not be empty", arg)
		}
		parsed[key] = append(parsed[key], value)
	}

	// Apply global keys to Opts.
	Opts.Verbose = boolParam("verbose", false)
	Opts.Output = paramOne("output")
	Opts.Inputs = append(Opts.Inputs, param("input")...)
	Opts.ConfigFile = paramOne("config")

	// Ensure default config files exist so users can find and edit them.
	if tagPath, err := DefaultConfigPath(); err == nil {
		ensureDefaultFile(tagPath, defaults.Tags)
	}
	if profPath, err := DefaultProfilePath(); err == nil {
		ensureDefaultFile(profPath, defaults.Profiles)
	}

	// Load tag aliases; non-fatal if config file is absent or unreadable.
	cfgPath := Opts.ConfigFile
	if cfgPath == "" {
		if p, err := DefaultConfigPath(); err == nil {
			cfgPath = p
		}
	}
	if cfgPath != "" {
		if cfg, err := LoadTagConfig(cfgPath); err == nil {
			Opts.TagAliases = cfg
		} else if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: could not load tag aliases from %q: %v\n", cfgPath, err)
		}
	}

	// Resolve profile: merge profile values into parsed, with CLI values
	// taking precedence over profile values.
	if profileName := paramOne("profile"); profileName != "" {
		profPath, err := DefaultProfilePath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: could not determine profile file path: %v\n", err)
		} else {
			cfg, err := LoadProfileConfig(profPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: could not load profiles from %q: %v\n", profPath, err)
			} else {
				if p, err := resolveProfile(profileName, cfg); err == nil {
					mergeProfile(p)
				} else {
					fmt.Fprintf(os.Stderr, "error: profile %q: %v\n", profileName, err)
				}
			}
		}
	}

	return nil
}

// param returns all values for key (case-insensitive).
func param(key string) []string {
	return parsed[strings.ToLower(key)]
}

// paramOne returns the first value for key, or "" if absent.
func paramOne(key string) string {
	if v := param(key); len(v) > 0 {
		return v[0]
	}
	return ""
}

// boolParam returns the bool value for key, or def if absent.
func boolParam(key string, def bool) bool {
	v := paramOne(key)
	if v == "" {
		return def
	}
	return strings.ToLower(v) == "true" || v == "1"
}

// ensureDefaultFile creates path with content if it does not already exist.
// Intermediate directories are created as needed. Errors are silently ignored
// since the absence of a config file is never fatal.
func ensureDefaultFile(path string, content []byte) {
	if _, err := os.Stat(path); err == nil {
		return // already exists
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// O_EXCL ensures we don't overwrite a file that was created between the
	// Stat check and here (e.g. a concurrent invocation).
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(content)
}
