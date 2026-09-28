package require

import (
	"bytes"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shellClient runs each command in a local shell, standing in for the SSH
// session so the check's real shell semantics are exercised. Only Exec is
// implemented; the embedded interface covers the rest.
type shellClient struct{ sshutil.SSHClient }

func (shellClient) Exec(cmd string) ([]byte, []byte, int, error) {
	var stdout, stderr bytes.Buffer
	c := osexec.Command("sh", "-c", cmd)
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	if exitErr, ok := err.(*osexec.ExitError); ok {
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return nil, nil, -1, err
	}
	return stdout.Bytes(), stderr.Bytes(), 0, nil
}

// A tool that only the host's setup_commands put on PATH (a bun or node
// install under $HOME) is there for every command rr runs, so the requirement
// check must see it too. The project dir isn't needed: requirements are
// checked before the first sync creates it.
func TestCheckRequirement_UsesHostSetupCommands(t *testing.T) {
	if _, err := osexec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("HOME", t.TempDir()) // no rc files to source

	binDir := t.TempDir()
	tool := filepath.Join(binDir, "rrprobetool")
	require.NoError(t, os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755))

	host := &config.Host{
		Shell:         "bash -c",
		Dir:           filepath.Join(t.TempDir(), "not-synced-yet"),
		SetupCommands: []string{`export PATH="` + binDir + `:$PATH"`},
	}

	got, err := CheckRequirement(shellClient{}, host, "rrprobetool")
	require.NoError(t, err)
	assert.True(t, got.Satisfied, "setup_commands put the tool on PATH")
	assert.Equal(t, tool, got.Path)

	// Setup commands that print (nvm announcing a version) don't end up in the path.
	host.SetupCommands = append([]string{"echo Now using node v20"}, host.SetupCommands...)
	got, err = CheckRequirement(shellClient{}, host, "rrprobetool")
	require.NoError(t, err)
	assert.True(t, got.Satisfied)
	assert.Equal(t, tool, got.Path)

	got, err = CheckRequirement(shellClient{}, &config.Host{Shell: "bash -c"}, "rrprobetool")
	require.NoError(t, err, "a missing tool isn't an error")
	assert.False(t, got.Satisfied, "without the setup command the tool isn't on PATH")

	got, err = CheckRequirement(shellClient{}, nil, "sh")
	require.NoError(t, err)
	assert.True(t, got.Satisfied, "no host config: plain lookup")
}

// A failing setup command stops the lookup before it runs. That's reported as
// the setup failing, not as the tool missing (which would point at
// `rr provision` for a tool that may well be installed).
func TestCheckRequirement_SetupCommandFails(t *testing.T) {
	if _, err := osexec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("HOME", t.TempDir())

	host := &config.Host{
		Shell:         "bash -c",
		SetupCommands: []string{"echo 'no such venv' >&2; false"},
	}
	_, err := CheckRequirement(shellClient{}, host, "sh")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such venv")

	results, err := CheckAll(shellClient{}, host, []string{"sh", "bash"}, NewCache(), "box")
	require.Error(t, err)
	assert.Nil(t, results)
}

// An alias from an rc file isn't expanded in the non-interactive shell a run
// uses, so it doesn't satisfy a requirement.
func TestCheckRequirement_AliasDoesNotCount(t *testing.T) {
	if _, err := osexec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	rc := "alias rrprobealias=true\n"
	require.NoError(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte(rc), 0o644))

	got, err := CheckRequirement(shellClient{}, &config.Host{Shell: "bash -c"}, "rrprobealias")
	require.NoError(t, err)
	assert.False(t, got.Satisfied, "path: %q", got.Path)
}
