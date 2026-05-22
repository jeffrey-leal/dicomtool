package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dicomtool",
	Short: "A command-line tool for working with DICOM files",
	Long: `dicomtool is a CLI utility for inspecting and modifying DICOM medical imaging files.

Parameters are passed as key:value pairs, for example:
  dicomtool inspect input:scan.dcm verbose:true
  dicomtool inspect input:scan.dcm tag:0010,0020 tag:0008,0060
  dicomtool modify input:scan.dcm output:/tmp/out`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := parseArgs(args); err != nil {
			return err
		}
		return validateParams(cmd)
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

// validateParams enforces cross-parameter constraints.
func validateParams(cmd *cobra.Command) error {
	if requiresOutput(cmd.Name()) && Opts.Output == "" {
		return errors.New("output:<path> is required for this command")
	}
	return nil
}

// requiresOutput returns true for subcommands that produce a file output.
func requiresOutput(name string) bool {
	switch name {
	case "modify":
		return true
	}
	return false
}

// --- version subcommand -------------------------------------------------------

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of dicomtool",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stdout, "dicomtool v1.1.4")
	},
}
