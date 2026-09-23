package cli

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points HOME at an empty temp dir for the whole package, so no test
// can load the developer's real ~/.rr/config.yaml or ~/.ssh/config. Commands
// under test resolve config by walking up from the working directory, which
// inside this repo finds rr's own .rr.yaml; with the real global config
// behind it, a test that runs a sync or task reaches real hosts. Tests that
// need a global config write one with writeGlobalConfig.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "rr-cli-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create test HOME:", err)
		os.Exit(1)
	}
	for _, key := range []string{"HOME", "USERPROFILE"} {
		if err := os.Setenv(key, home); err != nil {
			fmt.Fprintln(os.Stderr, "set test HOME:", err)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
