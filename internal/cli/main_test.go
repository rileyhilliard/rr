package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/lock"
)

// runCLIEnv tells the test binary, re-run as a child, to act as the rr
// binary: run the CLI on its arguments and exit with its code. Tests use it
// for concurrent runs, which can't share one process: Run's flags, output
// mode and stdout/stderr capture are process-global. The child keeps the
// HOME its parent gives it and polls a held lock every 50ms.
const runCLIEnv = "RR_TEST_RUN_CLI"

// TestMain points HOME at an empty temp dir for the whole package, so no test
// can load the developer's real ~/.rr/config.yaml or ~/.ssh/config. Commands
// under test resolve config by walking up from the working directory, which
// inside this repo finds rr's own .rr.yaml; with the real global config
// behind it, a test that runs a sync or task reaches real hosts. Tests that
// need a global config write one with writeGlobalConfig.
func TestMain(m *testing.M) {
	if os.Getenv(runCLIEnv) == "1" {
		lock.SetRetryIntervalForTesting(50 * time.Millisecond)
		os.Exit(run())
	}

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
	// A git hook (lefthook's pre-push runs go test) exports GIT_DIR and
	// friends; inherited, they point the temp repos tests create, and rr's own
	// git calls, at this repo, so a test's commit lands on the branch.
	for _, kv := range os.Environ() {
		if key, _, _ := strings.Cut(kv, "="); strings.HasPrefix(key, "GIT_") {
			_ = os.Unsetenv(key)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
