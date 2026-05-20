package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// TagConfig maps user-defined shortcut phrases to DICOM tag strings ("GGGG,EEEE").
type TagConfig map[string]string

// DefaultConfigPath returns the default config file location: ~/.dicomtool/tags.json
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dicomtool", "tags.json"), nil
}

// LoadTagConfig reads the config file at path. If the file does not exist an
// empty config is returned without error.
func LoadTagConfig(path string) (TagConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return TagConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg TagConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// SaveTagConfig writes cfg to path, creating intermediate directories as needed.
func SaveTagConfig(path string, cfg TagConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Resolve returns the tag string for phrase if it exists in the config,
// otherwise returns phrase unchanged.
func (c TagConfig) Resolve(phrase string) string {
	if c == nil {
		return phrase
	}
	if t, ok := c[phrase]; ok {
		return t
	}
	return phrase
}
