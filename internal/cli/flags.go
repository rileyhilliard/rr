package cli

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/spf13/cobra"
)

// CommonFlags holds the standard flags used across run/exec/sync/task commands.
type CommonFlags struct {
	Host         string
	Tag          string
	ProbeTimeout string
	Local        bool
}

// AddCommonFlags registers --host, --tag, --probe-timeout, and --local flags on a command.
func AddCommonFlags(cmd *cobra.Command, flags *CommonFlags) {
	cmd.Flags().StringVar(&flags.Host, "host", "", "target host name")
	cmd.Flags().StringVar(&flags.Tag, "tag", "", "select host by tag")
	cmd.Flags().StringVar(&flags.ProbeTimeout, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")
	cmd.Flags().BoolVar(&flags.Local, "local", false, "force local execution (skip remote hosts)")
}

// ParseProbeTimeout parses a probe timeout string into a duration.
// Returns zero duration if the flag is empty.
func ParseProbeTimeout(flag string) (time.Duration, error) {
	if flag == "" {
		return 0, nil
	}

	duration, err := time.ParseDuration(flag)
	if err != nil {
		return 0, errors.WrapWithCode(err, errors.ErrConfig,
			fmt.Sprintf("'%s' doesn't look like a valid timeout", flag),
			"Try something like 5s, 2m, or 500ms.")
	}
	return duration, nil
}

// hashProject creates a short hash of the project path for lock identification.
func hashProject(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8]) // First 16 hex chars
}
