package sync

import (
	"encoding/json"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pruneTestConn builds a connection whose remote dir is /root/rr/myapp@current
// and whose `ls` of the siblings returns the given names.
func pruneTestConn(t *testing.T, siblings string) (*host.Connection, *sshtesting.MockClient) {
	t.Helper()
	mock := sshtesting.NewMockClient("test-host")
	mock.SetCommandResponse(`^cd .* && ls -1d `, sshtesting.CommandResponse{Stdout: []byte(siblings)})
	for _, d := range []string{"myapp", "myapp@current", "myapp@gone", "myapp@live", "myapp@other-machine", "otherrepo@x"} {
		require.NoError(t, mock.GetFS().MkdirAll("/root/rr/"+d))
	}
	conn := &host.Connection{
		Name:   "test-host",
		Alias:  "test-host",
		Client: mock,
		Host:   config.Host{Dir: "/root/rr/myapp@current"},
	}
	return conn, mock
}

func writeMarker(t *testing.T, mock *sshtesting.MockClient, dir, hostname string) {
	t.Helper()
	data, err := json.Marshal(SourceMarker{SourcePath: "/x", Hostname: hostname})
	require.NoError(t, err)
	require.NoError(t, mock.GetFS().WriteFile(dir+"/"+sourceMarkerFile, data))
}

func TestStaleWorktreeDirs(t *testing.T) {
	siblings := "myapp@current\nmyapp@gone\nmyapp@live\nmyapp@other-machine\nmyapp@weird$name\n"

	t.Run("only dirs for missing worktrees, never the current one", func(t *testing.T) {
		conn, mock := pruneTestConn(t, siblings)
		writeMarker(t, mock, "/root/rr/myapp@other-machine", "someone-elses-laptop")
		live := map[string]bool{"live": true, "current": true}

		stale, err := staleWorktreeDirs(conn, "/root/rr/myapp@current", "myapp", live, "my-laptop")
		require.NoError(t, err)
		assert.Equal(t, []string{"/root/rr/myapp@gone"}, stale)
	})

	t.Run("main checkout prunes its worktree siblings too", func(t *testing.T) {
		conn, mock := pruneTestConn(t, siblings)
		conn.Host.Dir = "/root/rr/myapp"
		writeMarker(t, mock, "/root/rr/myapp@other-machine", "someone-elses-laptop")
		live := map[string]bool{"myapp": true, "live": true}

		stale, err := staleWorktreeDirs(conn, "/root/rr/myapp", "myapp", live, "my-laptop")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"/root/rr/myapp@current", "/root/rr/myapp@gone"}, stale)
	})

	t.Run("dirs with no marker count as ours", func(t *testing.T) {
		conn, _ := pruneTestConn(t, "myapp@gone\n")
		stale, err := staleWorktreeDirs(conn, "/root/rr/myapp@current", "myapp", map[string]bool{}, "my-laptop")
		require.NoError(t, err)
		assert.Equal(t, []string{"/root/rr/myapp@gone"}, stale)
	})

	t.Run("a marker from this machine does not protect a dir", func(t *testing.T) {
		conn, mock := pruneTestConn(t, "myapp@gone\n")
		writeMarker(t, mock, "/root/rr/myapp@gone", "my-laptop")
		stale, err := staleWorktreeDirs(conn, "/root/rr/myapp@current", "myapp", map[string]bool{}, "my-laptop")
		require.NoError(t, err)
		assert.Equal(t, []string{"/root/rr/myapp@gone"}, stale)
	})

	t.Run("nothing listed means nothing stale", func(t *testing.T) {
		conn, _ := pruneTestConn(t, "")
		stale, err := staleWorktreeDirs(conn, "/root/rr/myapp@current", "myapp", map[string]bool{}, "my-laptop")
		require.NoError(t, err)
		assert.Empty(t, stale)
	})

	t.Run("refuses to work at the filesystem root", func(t *testing.T) {
		conn, _ := pruneTestConn(t, "myapp@gone\n")
		stale, err := staleWorktreeDirs(conn, "/myapp@current", "myapp", map[string]bool{}, "my-laptop")
		require.NoError(t, err)
		assert.Empty(t, stale)
	})
}

func TestRemoveRemoteDir(t *testing.T) {
	t.Run("removes the named dir and nothing else", func(t *testing.T) {
		conn, mock := pruneTestConn(t, "")
		require.NoError(t, removeRemoteDir(conn, "/root/rr/myapp@gone"))
		assert.False(t, mock.GetFS().Exists("/root/rr/myapp@gone"))
		assert.True(t, mock.GetFS().Exists("/root/rr/myapp@live"))
	})

	t.Run("refuses paths that are not worktree dirs", func(t *testing.T) {
		conn, mock := pruneTestConn(t, "")
		err := removeRemoteDir(conn, "/root/rr/myapp")
		require.Error(t, err)
		assert.True(t, mock.GetFS().Exists("/root/rr/myapp"))

		err = removeRemoteDir(conn, "/x@y")
		require.Error(t, err)
	})
}

func TestPruneDirs(t *testing.T) {
	stale := []string{"/root/rr/myapp@gone", "/root/rr/myapp@live"}

	t.Run("removes and reports each dir", func(t *testing.T) {
		conn, mock := pruneTestConn(t, "")
		var reported []string
		done, err := pruneDirs(conn, stale, PruneOptions{
			Pruned: func(dir string) { reported = append(reported, dir) },
		})
		require.NoError(t, err)
		assert.Equal(t, stale, done)
		assert.Equal(t, stale, reported)
		assert.False(t, mock.GetFS().Exists("/root/rr/myapp@gone"))
	})

	t.Run("a dir that could not be removed is not reported", func(t *testing.T) {
		conn, mock := pruneTestConn(t, "")
		mock.SetCommandResponse(`^rm -rf `, sshtesting.CommandResponse{
			Stderr:   []byte("Permission denied"),
			ExitCode: 1,
		})
		var reported []string
		done, err := pruneDirs(conn, stale, PruneOptions{
			Pruned: func(dir string) { reported = append(reported, dir) },
		})
		require.Error(t, err)
		assert.Empty(t, done)
		assert.Empty(t, reported)
		assert.True(t, mock.GetFS().Exists("/root/rr/myapp@gone"))
	})

	t.Run("dry run reports without removing", func(t *testing.T) {
		conn, mock := pruneTestConn(t, "")
		var reported []string
		done, err := pruneDirs(conn, stale, PruneOptions{
			DryRun: true,
			Pruned: func(dir string) { reported = append(reported, dir) },
		})
		require.NoError(t, err)
		assert.Equal(t, stale, done)
		assert.Equal(t, stale, reported)
		assert.True(t, mock.GetFS().Exists("/root/rr/myapp@gone"))
	})
}

func TestPruneStaleWorktrees_SkipsLocalAndNil(t *testing.T) {
	got, err := PruneStaleWorktrees(nil, t.TempDir(), PruneOptions{})
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = PruneStaleWorktrees(&host.Connection{IsLocal: true}, t.TempDir(), PruneOptions{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPruneStaleWorktrees_SkipsWhenDirIsNotProject(t *testing.T) {
	// Host dir ends in a fixed component, not ${PROJECT}: the sibling layout
	// assumption does not hold, so nothing is listed or removed.
	mock := sshtesting.NewMockClient("test-host")
	mock.SetCommandResponse(`^cd `, sshtesting.CommandResponse{Stdout: []byte("myapp@gone\n")})
	conn := &host.Connection{Name: "h", Alias: "h", Client: mock, Host: config.Host{Dir: "/root/rr/fixed-name/src"}}

	got, err := PruneStaleWorktrees(conn, t.TempDir(), PruneOptions{})
	require.NoError(t, err)
	assert.Nil(t, got)
}
