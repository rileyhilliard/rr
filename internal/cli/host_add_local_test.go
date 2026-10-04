package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setHostAddFlags sets the host add flag vars and restores them after the test.
func setHostAddFlags(t *testing.T, name, ssh, dir string, local bool, tags, env []string) {
	t.Helper()
	oName, oSSH, oDir, oLocal, oTags, oEnv := hostAddName, hostAddSSH, hostAddDir, hostAddLocal, hostAddTags, hostAddEnv
	t.Cleanup(func() {
		hostAddName, hostAddSSH, hostAddDir, hostAddLocal, hostAddTags, hostAddEnv = oName, oSSH, oDir, oLocal, oTags, oEnv
	})
	hostAddName, hostAddSSH, hostAddDir, hostAddLocal, hostAddTags, hostAddEnv = name, ssh, dir, local, tags, env
}

// seedGlobalConfig loads the (default) global config under the test's HOME,
// sets its hosts and saves it, so tests start from a config rr itself wrote.
func seedGlobalConfig(t *testing.T, hosts map[string]config.Host) *config.GlobalConfig {
	t.Helper()
	cfg, err := config.LoadGlobal()
	require.NoError(t, err)
	cfg.Hosts = hosts
	require.NoError(t, config.SaveGlobal(cfg))
	return cfg
}

func requireConfigError(t *testing.T, err error) *errors.Error {
	t.Helper()
	require.Error(t, err)
	var rrErr *errors.Error
	require.ErrorAs(t, err, &rrErr)
	assert.Equal(t, errors.ErrConfig, rrErr.Code)
	assert.NotEmpty(t, rrErr.Suggestion)
	return rrErr
}

func TestHostAddLocal_WritesLocalHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setHostAddFlags(t, "dev", "", "", true, nil, nil)

	require.NoError(t, hostAdd(HostAddOptions{}))

	cfg, err := config.LoadGlobal()
	require.NoError(t, err)
	h, ok := cfg.Hosts["dev"]
	require.True(t, ok)
	assert.True(t, h.Local)
	assert.Empty(t, h.SSH)
	assert.Empty(t, h.Dir)
}

func TestHostAddLocal_HonorsTagAndEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setHostAddFlags(t, "dev", "", "", true, []string{"fast", "arm"}, []string{"FOO=bar", "A=b=c", "bad"})

	require.NoError(t, hostAdd(HostAddOptions{}))

	cfg, err := config.LoadGlobal()
	require.NoError(t, err)
	h := cfg.Hosts["dev"]
	assert.True(t, h.Local)
	assert.Equal(t, []string{"fast", "arm"}, h.Tags)
	assert.Equal(t, map[string]string{"FOO": "bar", "A": "b=c"}, h.Env)
}

func TestHostAddLocal_Errors(t *testing.T) {
	tests := []struct {
		name         string
		flagName     string
		ssh, dir     string
		existing     map[string]config.Host
		wantMsg      string
		wantSuggests string
	}{
		{
			name:         "with --ssh",
			flagName:     "dev",
			ssh:          "box.local",
			wantMsg:      "--local can't be combined with --ssh or --dir",
			wantSuggests: "Drop --local",
		},
		{
			name:         "with --dir",
			flagName:     "dev",
			dir:          "~/x",
			wantMsg:      "--local can't be combined with --ssh or --dir",
			wantSuggests: "Drop --local",
		},
		{
			name:         "without --name",
			wantMsg:      "Host name is required",
			wantSuggests: "--name",
		},
		{
			name:     "another local host exists",
			flagName: "dev",
			existing: map[string]config.Host{"mine": {Local: true}},
			wantMsg:  "only one host can be local, but 'mine' already sets 'local: true'",
			// The suggestion names the existing local host.
			wantSuggests: "'mine'",
		},
		{
			name:         "name taken",
			flagName:     "dev",
			existing:     map[string]config.Host{"dev": {SSH: []string{"dev.local"}, Dir: "~/rr"}},
			wantMsg:      "Host 'dev' already exists",
			wantSuggests: "rr host remove",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			seedGlobalConfig(t, tt.existing)
			setHostAddFlags(t, tt.flagName, tt.ssh, tt.dir, true, nil, nil)

			rrErr := requireConfigError(t, hostAdd(HostAddOptions{}))
			assert.Contains(t, rrErr.Message, tt.wantMsg)
			assert.Contains(t, rrErr.Suggestion, tt.wantSuggests)

			// A refused add leaves the config as it was.
			cfg, err := config.LoadGlobal()
			require.NoError(t, err)
			assert.Len(t, cfg.Hosts, len(tt.existing))
		})
	}
}

func TestAddLocalHost(t *testing.T) {
	t.Run("adds, validates and saves", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cfg := seedGlobalConfig(t, map[string]config.Host{"box": {SSH: []string{"box.local"}, Dir: "~/rr"}})

		require.NoError(t, addLocalHost(cfg, "dev", []string{"fast"}, map[string]string{"K": "v"}))

		assert.Equal(t, config.Host{Local: true, Tags: []string{"fast"}, Env: map[string]string{"K": "v"}}, cfg.Hosts["dev"])
		saved, err := config.LoadGlobal()
		require.NoError(t, err)
		assert.True(t, saved.Hosts["dev"].Local)
		assert.Contains(t, saved.Hosts, "box")
	})

	t.Run("nil hosts map", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cfg := seedGlobalConfig(t, nil)
		require.NoError(t, addLocalHost(cfg, "dev", nil, nil))
		assert.True(t, cfg.Hosts["dev"].Local)
	})

	t.Run("refuses a second local host and leaves cfg untouched", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cfg := seedGlobalConfig(t, map[string]config.Host{"mine": {Local: true}})
		rrErr := requireConfigError(t, addLocalHost(cfg, "dev", nil, nil))
		assert.Contains(t, rrErr.Message, "only one host can be local")
		assert.Contains(t, rrErr.Suggestion, "mine")
		assert.NotContains(t, cfg.Hosts, "dev")
	})

	t.Run("empty name", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cfg := seedGlobalConfig(t, map[string]config.Host{})
		requireConfigError(t, addLocalHost(cfg, "", nil, nil))
		assert.Empty(t, cfg.Hosts)
	})
}

func TestFindLocalHost(t *testing.T) {
	cfg := &config.GlobalConfig{Hosts: map[string]config.Host{"a": {SSH: []string{"a"}}}}
	assert.Equal(t, "", findLocalHost(cfg))
	cfg.Hosts["z"] = config.Host{Local: true}
	assert.Equal(t, "z", findLocalHost(cfg))
}

func TestDefaultLocalHostName(t *testing.T) {
	name := defaultLocalHostName(&config.GlobalConfig{})
	assert.NotEmpty(t, name)
	assert.NotEqual(t, "local", name)
	assert.NotContains(t, name, ".")

	// A taken hostname falls back rather than colliding.
	taken := &config.GlobalConfig{Hosts: map[string]config.Host{name: {SSH: []string{"x"}, Dir: "~/rr"}}}
	assert.NotEqual(t, name, defaultLocalHostName(taken))
}
