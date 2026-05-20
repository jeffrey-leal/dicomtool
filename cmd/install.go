package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jeffr/dicomtool/cmd/defaults"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Write default tags.json and profiles.json, overwriting any existing files",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tagPath, err := DefaultConfigPath()
		if err != nil {
			return fmt.Errorf("resolving tags path: %w", err)
		}
		profPath, err := DefaultProfilePath()
		if err != nil {
			return fmt.Errorf("resolving profiles path: %w", err)
		}

		if err := writeDefaultFile(tagPath, defaults.Tags); err != nil {
			return fmt.Errorf("writing %s: %w", tagPath, err)
		}
		fmt.Printf("written: %s\n", tagPath)

		if err := writeDefaultFile(profPath, defaults.Profiles); err != nil {
			return fmt.Errorf("writing %s: %w", profPath, err)
		}
		fmt.Printf("written: %s\n", profPath)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}

// writeDefaultFile writes content to path, creating intermediate directories
// as needed and overwriting any existing file.
func writeDefaultFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
