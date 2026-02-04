package doctor

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
)

func TestRequirementsCheck_NameAndCategory(t *testing.T) {
	check := &RequirementsCheck{
		HostName: "test-server",
	}

	assert.Equal(t, "requirements_test-server", check.Name())
	assert.Equal(t, "REQUIREMENTS", check.Category())
}

func TestRequirementsCheck_Run_NoConnection(t *testing.T) {
	check := &RequirementsCheck{
		HostName:     "test-server",
		Conn:         nil,
		Requirements: []string{"go", "node"},
	}

	result := check.Run()

	assert.Equal(t, StatusFail, result.Status)
	assert.Contains(t, result.Message, "no connection")
	assert.Contains(t, result.Message, "test-server")
}

func TestRequirementsCheck_Run_NilClient(t *testing.T) {
	check := &RequirementsCheck{
		HostName:     "test-server",
		Conn:         &host.Connection{Client: nil},
		Requirements: []string{"go"},
	}

	result := check.Run()

	assert.Equal(t, StatusFail, result.Status)
	assert.Contains(t, result.Message, "no connection")
}

func TestRequirementsCheck_Run_NoRequirements(t *testing.T) {
	// Note: The code requires Client != nil to proceed past connection check.
	// When Client is nil, it returns "no connection" regardless of requirements.
	// This test verifies that behavior - we'd need a real SSH client to test
	// the "none configured" path.
	check := &RequirementsCheck{
		HostName:     "test-server",
		Conn:         &host.Connection{IsLocal: true, Client: nil},
		Requirements: []string{},
	}

	result := check.Run()

	// With nil Client, we get "no connection" error even for local
	assert.Equal(t, StatusFail, result.Status)
	assert.Contains(t, result.Message, "no connection")
}

func TestNewRequirementsCheck(t *testing.T) {
	tests := []struct {
		name             string
		hostName         string
		hostCfg          config.Host
		projectCfg       *config.Config
		expectedReqs     []string
		expectedHostName string
	}{
		{
			name:             "no requirements",
			hostName:         "server1",
			hostCfg:          config.Host{},
			projectCfg:       nil,
			expectedReqs:     nil,
			expectedHostName: "server1",
		},
		{
			name:     "host requirements only",
			hostName: "server1",
			hostCfg: config.Host{
				Require: []string{"node", "npm"},
			},
			projectCfg:       nil,
			expectedReqs:     []string{"node", "npm"},
			expectedHostName: "server1",
		},
		{
			name:     "project requirements only",
			hostName: "server1",
			hostCfg:  config.Host{},
			projectCfg: &config.Config{
				Require: []string{"go", "cargo"},
			},
			expectedReqs:     []string{"go", "cargo"},
			expectedHostName: "server1",
		},
		{
			name:     "merged requirements - deduplicated",
			hostName: "server1",
			hostCfg: config.Host{
				Require: []string{"go", "node"},
			},
			projectCfg: &config.Config{
				Require: []string{"go", "cargo"},
			},
			expectedReqs:     []string{"go", "cargo", "node"},
			expectedHostName: "server1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := NewRequirementsCheck(tt.hostName, tt.hostCfg, nil, tt.projectCfg)

			assert.Equal(t, tt.expectedHostName, check.HostName)
			assert.ElementsMatch(t, tt.expectedReqs, check.Requirements)
		})
	}
}

func TestNewRequirementsChecks(t *testing.T) {
	tests := []struct {
		name        string
		hosts       map[string]config.Host
		connections map[string]*host.Connection
		projectCfg  *config.Config
		wantCount   int
	}{
		{
			name:        "empty hosts",
			hosts:       map[string]config.Host{},
			connections: map[string]*host.Connection{},
			projectCfg:  nil,
			wantCount:   0,
		},
		{
			name: "host with connection and requirements",
			hosts: map[string]config.Host{
				"server1": {Require: []string{"go"}},
			},
			connections: map[string]*host.Connection{
				"server1": {Name: "server1"},
			},
			projectCfg: nil,
			wantCount:  1,
		},
		{
			name: "host with connection but no requirements",
			hosts: map[string]config.Host{
				"server1": {},
			},
			connections: map[string]*host.Connection{
				"server1": {Name: "server1"},
			},
			projectCfg: nil,
			wantCount:  1, // Still creates check since connection exists
		},
		{
			name: "host without connection and without requirements",
			hosts: map[string]config.Host{
				"server1": {},
			},
			connections: map[string]*host.Connection{},
			projectCfg:  nil,
			wantCount:   0, // No check since no connection and no requirements
		},
		{
			name: "multiple hosts",
			hosts: map[string]config.Host{
				"server1": {Require: []string{"go"}},
				"server2": {Require: []string{"node"}},
			},
			connections: map[string]*host.Connection{
				"server1": {Name: "server1"},
				"server2": {Name: "server2"},
			},
			projectCfg: nil,
			wantCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := NewRequirementsChecks(tt.hosts, tt.connections, tt.projectCfg)
			assert.Len(t, checks, tt.wantCount)
		})
	}
}

func TestRequirementsCheck_Fix_NoConnection(t *testing.T) {
	check := &RequirementsCheck{
		HostName:     "test-server",
		Conn:         nil,
		Requirements: []string{"go"},
	}

	err := check.Fix()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no connection")
}

func TestRequirementsCheck_Fix_NilClient(t *testing.T) {
	check := &RequirementsCheck{
		HostName:     "test-server",
		Conn:         &host.Connection{Client: nil},
		Requirements: []string{"go"},
	}

	err := check.Fix()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no connection")
}
