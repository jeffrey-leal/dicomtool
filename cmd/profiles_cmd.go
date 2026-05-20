package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var profilesCmd = &cobra.Command{
	Use:   "profiles",
	Short: "Manage processing profiles",
}

var profilesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all profile names",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, cfg, err := loadProfileStore()
		if err != nil {
			return err
		}
		if len(cfg) == 0 {
			fmt.Printf("no profiles defined in %s\n", path)
			return nil
		}
		names := make([]string, 0, len(cfg))
		for n := range cfg {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Println(n)
		}
		return nil
	},
}

var profilesShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show the parameters stored in a profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, cfg, err := loadProfileStore()
		if err != nil {
			return err
		}
		p, ok := cfg[args[0]]
		if !ok {
			return fmt.Errorf("profile %q not found", args[0])
		}
		data, _ := json.MarshalIndent(p, "", "  ")
		fmt.Println(string(data))
		return nil
	},
}

var profilesAddCmd = &cobra.Command{
	Use:   "add <name> [param:value ...]",
	Short: "Add or update a profile (e.g. profiles add anonymize set:0010,0010=Anon noprivate:true dob:YYYY0101)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		p := Profile{
			DOB:      paramOne("dob"),
			UIDSuffix: paramOne("uid"),
			Priv:     boolParam("noprivate", false),
			Dicomdir: boolParam("dicomdir", false),
			Verbose:  boolParam("verbose", false),
		}

		if s := paramOne("maskrows"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 {
				return fmt.Errorf("maskrows %q must be a positive integer", s)
			}
			p.MaskRows = n
		}

		for _, s := range param("set") {
			tagStr, _, ok := strings.Cut(s, "=")
			if !ok || tagStr == "" {
				return fmt.Errorf("invalid set value %q: expected <tag>=<value>", s)
			}
			// Resolve alias so the profile always stores raw GGGG,EEEE tags.
			resolved := Opts.TagAliases.Resolve(tagStr)
			if _, err := parseTagString(resolved); err != nil {
				return fmt.Errorf("invalid set tag %q: %w", tagStr, err)
			}
			_, value, _ := strings.Cut(s, "=")
			p.Sets = append(p.Sets, resolved+"="+value)
		}

		for _, r := range param("remove") {
			resolved := Opts.TagAliases.Resolve(r)
			if _, err := parseTagString(resolved); err != nil {
				return fmt.Errorf("invalid remove tag %q: %w", r, err)
			}
			p.Removes = append(p.Removes, resolved)
		}

		if s := paramOne("ignoretype"); s != "" {
			for _, v := range strings.Split(s, ",") {
				if v = strings.TrimSpace(v); v != "" {
					p.IgnoreTypes = append(p.IgnoreTypes, v)
				}
			}
		}

		if s := paramOne("ignoremodality"); s != "" {
			for _, v := range strings.Split(s, ",") {
				if v = strings.TrimSpace(v); v != "" {
					p.IgnoreModalities = append(p.IgnoreModalities, v)
				}
			}
		}

		path, cfg, err := loadProfileStore()
		if err != nil {
			return err
		}

		if baseName := paramOne("base"); baseName != "" {
			if _, ok := cfg[baseName]; !ok {
				return fmt.Errorf("base profile %q not found", baseName)
			}
			p.Base = baseName
		}

		cfg[name] = p
		if err := SaveProfileConfig(path, cfg); err != nil {
			return err
		}
		if Opts.Verbose {
			fmt.Printf("saved profile %q to %s\n", name, path)
		}
		return nil
	},
}

var profilesRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		path, cfg, err := loadProfileStore()
		if err != nil {
			return err
		}
		if _, ok := cfg[name]; !ok {
			return fmt.Errorf("profile %q not found", name)
		}
		delete(cfg, name)
		if err := SaveProfileConfig(path, cfg); err != nil {
			return err
		}
		if Opts.Verbose {
			fmt.Printf("removed profile %q\n", name)
		}
		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesListCmd, profilesShowCmd, profilesAddCmd, profilesRemoveCmd)
	rootCmd.AddCommand(profilesCmd)
}

// loadProfileStore resolves the profile file path and loads it.
func loadProfileStore() (string, ProfileConfig, error) {
	path, err := DefaultProfilePath()
	if err != nil {
		return "", nil, err
	}
	cfg, err := LoadProfileConfig(path)
	if err != nil {
		return "", nil, fmt.Errorf("loading profiles %q: %w", path, err)
	}
	return path, cfg, nil
}
