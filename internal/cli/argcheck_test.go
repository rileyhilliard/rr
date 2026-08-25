package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withDiscoveryState swaps the package-level discovery state for a test.
func withDiscoveryState(t *testing.T, state *configDiscoveryState) {
	t.Helper()
	prev := discoveryState
	discoveryState = state
	t.Cleanup(func() { discoveryState = prev })
}

// withGlobalHosts points HOME at a temp dir whose ~/.rr/config.yaml defines
// the given hosts (or no config at all when hosts is empty).
func withGlobalHosts(t *testing.T, hosts ...string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	if len(hosts) == 0 {
		return
	}
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	content := "version: 1\nhosts:\n"
	for _, h := range hosts {
		content += "  " + h + ":\n    ssh: [" + h + "]\n    dir: ~/rr/proj\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(content), 0o644))
}

func TestValidateLeadingArg(t *testing.T) {
	t.Run("task name first is rejected", func(t *testing.T) {
		withGlobalHosts(t)
		withDiscoveryState(t, &configDiscoveryState{TasksAvailable: []string{"test-backend"}})

		err := validateLeadingArg([]string{"test-backend", "-k", "bond"}, "run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "'test-backend' is a task")
	})

	t.Run("host name first is rejected", func(t *testing.T) {
		withGlobalHosts(t, "m4-mini")
		withDiscoveryState(t, nil)

		err := validateLeadingArg([]string{"m4-mini", "make", "test"}, "run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "'m4-mini' is a host")
		assert.Contains(t, err.Error(), "--host m4-mini")
	})

	t.Run("legit multi-word command passes", func(t *testing.T) {
		withGlobalHosts(t, "m4-mini")
		withDiscoveryState(t, &configDiscoveryState{TasksAvailable: []string{"test-backend"}})

		assert.NoError(t, validateLeadingArg([]string{"make", "test"}, "run"))
	})

	t.Run("single arg passes even when it matches", func(t *testing.T) {
		withGlobalHosts(t)
		withDiscoveryState(t, &configDiscoveryState{TasksAvailable: []string{"test-backend"}})

		assert.NoError(t, validateLeadingArg([]string{"test-backend"}, "run"))
	})

	t.Run("non-bare leading word passes", func(t *testing.T) {
		withGlobalHosts(t)
		withDiscoveryState(t, nil)

		assert.NoError(t, validateLeadingArg([]string{"./script.sh", "arg"}, "run"))
		assert.NoError(t, validateLeadingArg([]string{"VAR=1", "make"}, "run"))
	})

	t.Run("nil discovery state and no config is safe", func(t *testing.T) {
		withGlobalHosts(t)
		withDiscoveryState(t, nil)

		assert.NoError(t, validateLeadingArg([]string{"anything", "goes"}, "run"))
	})
}

func TestIsBareWord(t *testing.T) {
	assert.True(t, isBareWord("test-backend"))
	assert.True(t, isBareWord("m4_mini.local"))
	assert.False(t, isBareWord(""))
	assert.False(t, isBareWord("./script.sh"))
	assert.False(t, isBareWord("a b"))
	assert.False(t, isBareWord("FOO=1"))
	assert.False(t, isBareWord("$(cmd)"))
}
