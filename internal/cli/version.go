package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version information set via ldflags at build time
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the version, commit hash, and build date of rr.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("rr version %s\n", version)
		fmt.Printf("  commit:  %s\n", commit)
		fmt.Printf("  built:   %s\n", date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

// SetVersionInfo sets the version information (called from main).
func SetVersionInfo(v, c, d string) {
	version = v
	commit = c
	date = d
}

// GetVersion returns the current version string.
func GetVersion() string {
	return version
}
