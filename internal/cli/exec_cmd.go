package cli

import (
	"os"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
)

// execCommand executes a command without syncing files first.
// This shares the core logic with run but skips the sync phase.
func execCommand(args []string, hostFlag string) error {
	if len(args) == 0 {
		return errors.New(errors.ErrExec,
			"No command specified",
			"Usage: rr exec <command>")
	}

	// Join all args as the command
	cmd := strings.Join(args, " ")

	exitCode, err := Run(RunOptions{
		Command:  cmd,
		Host:     hostFlag,
		SkipSync: true, // Key difference from run
	})

	if err != nil {
		return err
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
