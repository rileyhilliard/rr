package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain clears the GIT_* variables a git hook exports (lefthook's
// pre-push runs go test). Inherited, they point the temp repos tests create
// at this repo, so a test's commit lands on the branch being pushed.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if key, _, _ := strings.Cut(kv, "="); strings.HasPrefix(key, "GIT_") {
			_ = os.Unsetenv(key)
		}
	}
	os.Exit(m.Run())
}
