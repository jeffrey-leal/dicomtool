package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Manage tag alias mappings",
}

var tagsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tag aliases",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, cfg, err := loadCfg()
		if err != nil {
			return err
		}
		if len(cfg) == 0 {
			fmt.Printf("no aliases defined in %s\n", path)
			return nil
		}
		phrases := make([]string, 0, len(cfg))
		for p := range cfg {
			phrases = append(phrases, p)
		}
		sort.Strings(phrases)
		fmt.Printf("%-30s %s\n", "PHRASE", "TAG")
		fmt.Printf("%-30s %s\n", "------", "---")
		for _, p := range phrases {
			fmt.Printf("%-30s %s\n", p, cfg[p])
		}
		return nil
	},
}

var tagsAddCmd = &cobra.Command{
	Use:   "add <phrase> <tag>",
	Short: "Add or update a tag alias (e.g. tags add PatientID 0010,0020)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		phrase, tagStr := args[0], args[1]

		// Validate the tag string before saving.
		if _, err := parseTagString(tagStr); err != nil {
			return fmt.Errorf("invalid tag %q: %w", tagStr, err)
		}

		path, cfg, err := loadCfg()
		if err != nil {
			return err
		}
		cfg[phrase] = tagStr
		if err := SaveTagConfig(path, cfg); err != nil {
			return err
		}
		if Opts.Verbose {
			fmt.Printf("added: %s -> %s\n", phrase, tagStr)
		}
		return nil
	},
}

var tagsRemoveCmd = &cobra.Command{
	Use:   "remove <phrase>",
	Short: "Remove a tag alias",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		phrase := args[0]
		path, cfg, err := loadCfg()
		if err != nil {
			return err
		}
		if _, ok := cfg[phrase]; !ok {
			return fmt.Errorf("alias %q not found", phrase)
		}
		delete(cfg, phrase)
		if err := SaveTagConfig(path, cfg); err != nil {
			return err
		}
		if Opts.Verbose {
			fmt.Printf("removed: %s\n", phrase)
		}
		return nil
	},
}

func init() {
	tagsCmd.AddCommand(tagsListCmd, tagsAddCmd, tagsRemoveCmd)
	rootCmd.AddCommand(tagsCmd)
}

// loadCfg resolves the config path (honouring config: param if set) and loads it.
func loadCfg() (string, TagConfig, error) {
	path := Opts.ConfigFile
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return "", nil, err
		}
	}
	cfg, err := LoadTagConfig(path)
	if err != nil {
		return "", nil, fmt.Errorf("loading config %q: %w", path, err)
	}
	return path, cfg, nil
}
