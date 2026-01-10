package sshutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSSHConfigFile(t *testing.T) {
	// Create a temp SSH config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	configContent := `
Host myserver
    HostName 192.168.1.100
    User admin
    Port 22
    IdentityFile ~/.ssh/id_myserver

Host gpu-box
    HostName gpu.example.com
    User ubuntu

Host *
    ServerAliveInterval 60

Host work-*
    User workuser
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)

	// Should have exactly 2 hosts (myserver and gpu-box)
	// Wildcards (*) and patterns (work-*) should be excluded
	assert.Len(t, hosts, 2)

	// Check that hosts are sorted alphabetically
	assert.Equal(t, "gpu-box", hosts[0].Alias)
	assert.Equal(t, "myserver", hosts[1].Alias)

	// Check myserver details
	myserver := hosts[1]
	assert.Equal(t, "192.168.1.100", myserver.Hostname)
	assert.Equal(t, "admin", myserver.User)
	assert.Equal(t, "22", myserver.Port)
	assert.Contains(t, myserver.IdentityFile, "id_myserver")

	// Check gpu-box details
	gpubox := hosts[0]
	assert.Equal(t, "gpu.example.com", gpubox.Hostname)
	assert.Equal(t, "ubuntu", gpubox.User)
	assert.Equal(t, "", gpubox.Port) // Not specified
}

func TestParseSSHConfigFileNotExists(t *testing.T) {
	hosts, err := ParseSSHConfigFile("/nonexistent/config")

	// Should return nil hosts and nil error for missing config
	assert.NoError(t, err)
	assert.Nil(t, hosts)
}

func TestSSHHostEntryDescription(t *testing.T) {
	tests := []struct {
		name     string
		entry    SSHHostEntry
		expected string
	}{
		{
			name: "full entry",
			entry: SSHHostEntry{
				Alias:    "myserver",
				Hostname: "192.168.1.100",
				User:     "admin",
				Port:     "2222",
			},
			expected: "192.168.1.100, user: admin, port: 2222",
		},
		{
			name: "default port",
			entry: SSHHostEntry{
				Alias:    "myserver",
				Hostname: "192.168.1.100",
				User:     "admin",
				Port:     "22",
			},
			expected: "192.168.1.100, user: admin",
		},
		{
			name: "hostname same as alias",
			entry: SSHHostEntry{
				Alias:    "myserver",
				Hostname: "myserver",
				User:     "admin",
			},
			expected: "user: admin",
		},
		{
			name: "minimal entry",
			entry: SSHHostEntry{
				Alias: "myserver",
			},
			expected: "myserver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entry.Description()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterHostsWithKeys(t *testing.T) {
	// Create a temp directory with a fake key
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "id_test")
	err := os.WriteFile(keyPath, []byte("fake key"), 0600)
	require.NoError(t, err)

	hosts := []SSHHostEntry{
		{Alias: "with-key", IdentityFile: keyPath},
		{Alias: "without-key", IdentityFile: "/nonexistent/key"},
		{Alias: "no-identity"},
	}

	// This test depends on whether default keys exist
	// Just verify the filter runs without error
	filtered := FilterHostsWithKeys(hosts)

	// The host with the valid key should be included
	hasWithKey := false
	for _, h := range filtered {
		if h.Alias == "with-key" {
			hasWithKey = true
			break
		}
	}
	assert.True(t, hasWithKey, "Host with valid identity file should be included")
}

func TestParseSSHConfigWithMatch(t *testing.T) {
	// Create a temp SSH config with Match directive
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	configContent := `
Host before-match
    HostName before.example.com

Match host *.example.com
    User matchuser

Host after-match
    HostName after.example.com
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)

	// Should only see the host before the Match directive
	assert.Len(t, hosts, 1)
	assert.Equal(t, "before-match", hosts[0].Alias)
}

func TestSSHHostEntry_HasIdentityFile_CustomPath(t *testing.T) {
	// Create a temp identity file
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "custom_key")
	err := os.WriteFile(keyPath, []byte("fake key content"), 0600)
	require.NoError(t, err)

	entry := SSHHostEntry{
		Alias:        "test-host",
		IdentityFile: keyPath,
	}

	assert.True(t, entry.HasIdentityFile())
}

func TestSSHHostEntry_HasIdentityFile_NonexistentPath(t *testing.T) {
	entry := SSHHostEntry{
		Alias:        "test-host",
		IdentityFile: "/nonexistent/key/file",
	}

	// Will return true only if default keys exist in ~/.ssh/
	// We can't control this in the test, but we can verify it doesn't panic
	_ = entry.HasIdentityFile()
}

func TestSSHHostEntry_HasIdentityFile_EmptyPath(t *testing.T) {
	entry := SSHHostEntry{
		Alias:        "test-host",
		IdentityFile: "",
	}

	// Will check for default keys
	// Just verify it doesn't panic
	_ = entry.HasIdentityFile()
}

func TestParseSSHConfigFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	// Create an empty config file
	err := os.WriteFile(configPath, []byte(""), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)
	assert.Empty(t, hosts)
}

func TestParseSSHConfigFile_CommentsOnly(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	configContent := `
# This is a comment
# Another comment

# Yet another comment
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)
	assert.Empty(t, hosts)
}

func TestParseSSHConfigFile_DuplicateHosts(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	configContent := `
Host duplicate
    HostName first.example.com

Host duplicate
    HostName second.example.com
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)

	// Should only have one entry (seen filter)
	assert.Len(t, hosts, 1)
	assert.Equal(t, "duplicate", hosts[0].Alias)
}

func TestParseSSHConfigFile_MultiplePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	configContent := `
Host server1 server2 server3
    User shareduser
    Port 2222
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)

	// Should have all three hosts
	assert.Len(t, hosts, 3)

	// All should have the same user and port
	for _, h := range hosts {
		assert.Equal(t, "shareduser", h.User)
		assert.Equal(t, "2222", h.Port)
	}
}

func TestSSHHostEntry_Description_OnlyUser(t *testing.T) {
	entry := SSHHostEntry{
		Alias: "myserver",
		User:  "admin",
	}

	desc := entry.Description()
	assert.Equal(t, "user: admin", desc)
}

func TestSSHHostEntry_Description_OnlyPort(t *testing.T) {
	entry := SSHHostEntry{
		Alias: "myserver",
		Port:  "2222",
	}

	desc := entry.Description()
	assert.Equal(t, "port: 2222", desc)
}

func TestSSHHostEntry_Description_OnlyHostname(t *testing.T) {
	entry := SSHHostEntry{
		Alias:    "myserver",
		Hostname: "192.168.1.100",
	}

	desc := entry.Description()
	assert.Equal(t, "192.168.1.100", desc)
}

func TestFilterHostsWithKeys_EmptyInput(t *testing.T) {
	filtered := FilterHostsWithKeys([]SSHHostEntry{})
	assert.Empty(t, filtered)
}

func TestFilterHostsWithKeys_NilInput(t *testing.T) {
	filtered := FilterHostsWithKeys(nil)
	assert.Empty(t, filtered)
}

func TestParseSSHConfigFile_SpecialCharactersInAlias(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	configContent := `
Host my-server_01
    HostName server01.example.com
    User admin
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)

	assert.Len(t, hosts, 1)
	assert.Equal(t, "my-server_01", hosts[0].Alias)
}

func TestParseSSHConfigFile_WithIdentityFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	configContent := `
Host secure-server
    HostName secure.example.com
    IdentityFile ~/.ssh/special_key
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	hosts, err := ParseSSHConfigFile(configPath)
	require.NoError(t, err)

	assert.Len(t, hosts, 1)
	assert.Contains(t, hosts[0].IdentityFile, "special_key")
}
