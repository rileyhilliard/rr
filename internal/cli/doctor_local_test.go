package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
)

// A local host has no SSH, so the SSH key and agent checks only add a false
// warning when every host in scope is local. Any remote host brings them back.
func TestCollectChecks_SSHChecksSkippedForLocalOnlyHosts(t *testing.T) {
	remote := config.Host{SSH: []string{"box"}, Dir: "/tmp/rr"}
	local := config.Host{Local: true}

	tests := []struct {
		name    string
		hosts   map[string]config.Host
		project *config.Config
		wantSSH bool
	}{
		{"only local host", map[string]config.Host{"dev": local}, nil, false},
		{"local and remote", map[string]config.Host{"dev": local, "box": remote}, nil, true},
		{"only remote host", map[string]config.Host{"box": remote}, nil, true},
		{"project scopes to the local host", map[string]config.Host{"dev": local, "box": remote}, &config.Config{Hosts: []string{"dev"}}, false},
		{"project scopes to a remote host", map[string]config.Host{"dev": local, "box": remote}, &config.Config{Hosts: []string{"box"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := collectChecks("", tt.project, &config.GlobalConfig{Hosts: tt.hosts})

			names := make(map[string]bool)
			for _, c := range checks {
				names[c.Name()] = true
			}
			assert.Equal(t, tt.wantSSH, names["ssh_key"], "ssh_key check")
			assert.Equal(t, tt.wantSSH, names["ssh_agent"], "ssh_agent check")
		})
	}
}
