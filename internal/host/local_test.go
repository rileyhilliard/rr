package host

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client tests use a POSIX shell")
	}
}

func TestLocalClient_Exec(t *testing.T) {
	skipOnWindows(t)
	c := NewLocalClient()

	stdout, stderr, code, err := c.Exec("echo out; echo err >&2; exit 3")
	require.NoError(t, err)
	assert.Equal(t, 3, code)
	assert.Equal(t, "out\n", string(stdout))
	assert.Equal(t, "err\n", string(stderr))
}

func TestLocalClient_ExecStreamContext(t *testing.T) {
	skipOnWindows(t)
	c := NewLocalClient()

	var out, errOut bytes.Buffer
	code, err := c.ExecStreamContext(context.Background(), "echo hello; echo oops >&2", &out, &errOut)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "hello\n", out.String())
	assert.Equal(t, "oops\n", errOut.String())
}

func TestLocalClient_ExecStreamContextCancel(t *testing.T) {
	skipOnWindows(t)
	c := NewLocalClient()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out bytes.Buffer
	_, err := c.ExecStreamContext(ctx, "sleep 10", &out, &out)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second, "cancel stops the command")
}

func TestLocalClient_AliveAndIdentity(t *testing.T) {
	c := NewLocalClient()
	ok, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, "local", c.GetHost())
	assert.Equal(t, "local", c.GetAddress())
	assert.NoError(t, c.Close())

	conn := &Connection{Name: "dev", Client: c, Host: config.Host{Local: true}}
	assert.True(t, conn.Alive())
}

func TestConnection_InPlace(t *testing.T) {
	assert.True(t, (&Connection{IsLocal: true}).InPlace(), "local fallback runs in place")
	assert.True(t, (&Connection{Host: config.Host{Local: true}}).InPlace(), "a local host runs in place")
	assert.False(t, (&Connection{Host: config.Host{SSH: []string{"box"}}}).InPlace())
	assert.False(t, (*Connection)(nil).InPlace())
}

func TestSelector_SelectLocalHost(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	hosts := map[string]config.Host{
		"dev": {Local: true, Dir: dir, Tags: []string{"fast"}},
	}

	var events []ConnectionEvent
	s := NewSelector(hosts)
	s.SetEventHandler(func(e ConnectionEvent) { events = append(events, e) })

	conn, err := s.Select("")
	require.NoError(t, err)
	assert.Equal(t, "dev", conn.Name)
	assert.Equal(t, "local", conn.Alias)
	assert.False(t, conn.IsLocal, "a local host is a host, not a fallback")
	assert.True(t, conn.InPlace())
	require.NotNil(t, conn.Client, "the lock and exec paths need a client")

	// The client runs commands on this machine.
	marker := filepath.Join(dir, "ran")
	_, _, code, err := conn.Client.Exec("touch " + marker)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	_, statErr := os.Stat(marker)
	assert.NoError(t, statErr)

	require.NotEmpty(t, events)
	assert.Equal(t, EventConnected, events[len(events)-1].Type)

	// Cached on a second Select.
	again, err := s.Select("dev")
	require.NoError(t, err)
	assert.Same(t, conn, again)
}

func TestSelector_SelectHostAndTagWithLocalHost(t *testing.T) {
	hosts := map[string]config.Host{
		"dev":    {Local: true, Tags: []string{"fast"}},
		"remote": {SSH: []string{"box.invalid"}, Dir: "/tmp/rr", Tags: []string{"slow"}},
	}
	s := NewSelector(hosts)
	s.SetHostOrder([]string{"dev", "remote"})

	assert.Equal(t, []string{"dev", "remote"}, s.GetHostNames())

	conn, err := s.SelectHost("dev")
	require.NoError(t, err)
	assert.Equal(t, "dev", conn.Name)
	assert.True(t, conn.InPlace())

	tagged, err := s.SelectByTag("fast")
	require.NoError(t, err)
	assert.Equal(t, "dev", tagged.Name)

	next, err := s.SelectNextHost(nil)
	require.NoError(t, err)
	assert.Equal(t, "dev", next.Name, "the local host is tried in its place in the order")
}

func TestSelector_HostInfoLocal(t *testing.T) {
	s := NewSelector(map[string]config.Host{"dev": {Local: true, Dir: "/work/proj"}})
	info := s.HostInfo()
	require.Len(t, info, 1)
	assert.Equal(t, []string{"local"}, info[0].SSH)
	assert.Equal(t, "/work/proj", info[0].Dir)
}

// A local host connection made without a dir (the lock and fallback paths
// build one from the global config) gets the project root, so a command
// built for it never runs in the home directory it starts in.
func TestNewLocalHostConnection_DefaultsDirToProjectRoot(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, ".rr.yaml"), []byte("version: 1\n"), 0o644))
	sub := filepath.Join(project, "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	conn := NewLocalHostConnection("dev", config.Host{Local: true})
	got, err := filepath.EvalSymlinks(conn.Host.Dir)
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(project)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	set := NewLocalHostConnection("dev", config.Host{Local: true, Dir: "/elsewhere"})
	assert.Equal(t, "/elsewhere", set.Host.Dir, "a dir that's set is kept")
}
