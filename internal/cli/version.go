package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version information set via ldflags at build time
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionShort controls whether to show short or full version output
var versionShort bool

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the version, commit hash, and build date of rr.`,
	Run: func(cmd *cobra.Command, args []string) {
		if versionShort {
			fmt.Println(version)
			return
		}

		fmt.Printf("rr %s\n", formatVersion(version))
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built: %s\n", date)
		fmt.Printf("go: %s\n", runtime.Version())
		fmt.Printf("os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)

		// Check for updates (non-blocking, uses cached result)
		checkAndDisplayUpdate()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVar(&versionShort, "short", false, "Print only the version number")
}

// formatVersion ensures version has a 'v' prefix for display
func formatVersion(v string) string {
	if v == "" || v == "dev" {
		return v
	}
	if v[0] != 'v' {
		return "v" + v
	}
	return v
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
