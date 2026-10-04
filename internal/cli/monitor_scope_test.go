package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
)

// writeMonitorConfigs writes global hosts box, spare (remote) and dev
// (local), and a project whose hosts: list is only box.
func writeMonitorConfigs(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	global := `version: 1
hosts:
  box:
    ssh: [box.invalid]
    dir: ~/rr
  spare:
    ssh: [spare.invalid]
    dir: ~/rr
  dev:
    local: true
`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, ".rr.yaml"), []byte("version: 1\nhosts: [box]\n"), 0o644))
	t.Chdir(project)
}

// Without --hosts, monitor shows the project's hosts. Hosts named with
// --hosts are shown whether or not the project's hosts: list has them (the
// local host is usually left out of it), in the order given; a name that
// isn't a configured host is an error, not silently dropped.
func TestResolveMonitorScope_HostsFlag(t *testing.T) {
	tests := []struct {
		name      string
		filter    string
		wantOrder []string
		wantErr   []string // substrings of a HOST_NOT_FOUND error
	}{
		{name: "project hosts by default", wantOrder: []string{"box"}},
		{name: "local host outside the project", filter: "dev,box", wantOrder: []string{"dev", "box"}},
		{name: "remote host outside the project", filter: "spare", wantOrder: []string{"spare"}},
		{name: "unknown name among known", filter: "box,nope", wantErr: []string{"'nope'", "rr host list"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeMonitorConfigs(t)
			scope, err := resolveMonitorScope(tt.filter)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.IsCode(err, errors.ErrHostNotFound), "got %v", err)
				for _, s := range tt.wantErr {
					assert.Contains(t, err.Error(), s)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrder, scope.order)
			assert.Len(t, scope.hosts, len(tt.wantOrder))
		})
	}
}
