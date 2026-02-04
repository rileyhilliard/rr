package cli

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/require"
	"github.com/stretchr/testify/assert"
	requireT "github.com/stretchr/testify/require"
)

func TestProvisionOutput_JSONMarshaling(t *testing.T) {
	output := ProvisionOutput{
		Hosts: []ProvisionHostResult{
			{
				Name:      "m1-mini",
				OS:        "darwin",
				Connected: true,
				Requirements: []ProvisionRequirementItem{
					{Name: "go", Satisfied: true, Path: "/usr/local/go/bin/go", CanInstall: true},
					{Name: "bun", Satisfied: false, CanInstall: true, Installed: true},
				},
			},
			{
				Name:      "linux-box",
				OS:        "linux",
				Connected: false,
				Error:     "connection refused",
			},
		},
		Summary: ProvisionSummary{
			TotalHosts:     2,
			ConnectedHosts: 1,
			TotalMissing:   1,
			CanInstall:     1,
			Installed:      1,
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(output)
	requireT.NoError(t, err)

	// Unmarshal back
	var decoded ProvisionOutput
	err = json.Unmarshal(data, &decoded)
	requireT.NoError(t, err)

	// Verify structure
	assert.Len(t, decoded.Hosts, 2)
	assert.Equal(t, "m1-mini", decoded.Hosts[0].Name)
	assert.Equal(t, "darwin", decoded.Hosts[0].OS)
	assert.True(t, decoded.Hosts[0].Connected)
	assert.Len(t, decoded.Hosts[0].Requirements, 2)

	// Verify second host with error
	assert.Equal(t, "linux-box", decoded.Hosts[1].Name)
	assert.False(t, decoded.Hosts[1].Connected)
	assert.Equal(t, "connection refused", decoded.Hosts[1].Error)

	// Verify summary
	assert.Equal(t, 2, decoded.Summary.TotalHosts)
	assert.Equal(t, 1, decoded.Summary.ConnectedHosts)
	assert.Equal(t, 1, decoded.Summary.TotalMissing)
	assert.Equal(t, 1, decoded.Summary.CanInstall)
	assert.Equal(t, 1, decoded.Summary.Installed)
}

func TestProvisionHostResult_JSONFields(t *testing.T) {
	result := ProvisionHostResult{
		Name:      "test-host",
		OS:        "darwin",
		Connected: true,
		Requirements: []ProvisionRequirementItem{
			{Name: "go", Satisfied: true, Path: "/usr/local/go/bin/go"},
		},
	}

	data, err := json.Marshal(result)
	requireT.NoError(t, err)

	// Verify JSON field names
	assert.Contains(t, string(data), `"name":"test-host"`)
	assert.Contains(t, string(data), `"os":"darwin"`)
	assert.Contains(t, string(data), `"connected":true`)
	assert.Contains(t, string(data), `"requirements":[`)

	// Error should be omitted when empty
	assert.NotContains(t, string(data), `"error"`)
}

func TestProvisionHostResult_WithError(t *testing.T) {
	result := ProvisionHostResult{
		Name:      "failed-host",
		Connected: false,
		Error:     "connection timeout",
	}

	data, err := json.Marshal(result)
	requireT.NoError(t, err)

	assert.Contains(t, string(data), `"error":"connection timeout"`)
	assert.Contains(t, string(data), `"connected":false`)
}

func TestProvisionRequirementItem_JSONFields(t *testing.T) {
	tests := []struct {
		name     string
		item     ProvisionRequirementItem
		contains []string
		excludes []string
	}{
		{
			name: "satisfied requirement",
			item: ProvisionRequirementItem{
				Name:       "go",
				Satisfied:  true,
				Path:       "/usr/local/go/bin/go",
				CanInstall: true,
			},
			contains: []string{
				`"name":"go"`,
				`"satisfied":true`,
				`"path":"/usr/local/go/bin/go"`,
				`"canInstall":true`,
			},
			excludes: []string{`"installed"`},
		},
		{
			name: "missing requirement that was installed",
			item: ProvisionRequirementItem{
				Name:       "bun",
				Satisfied:  false,
				CanInstall: true,
				Installed:  true,
			},
			contains: []string{
				`"name":"bun"`,
				`"satisfied":false`,
				`"canInstall":true`,
				`"installed":true`,
			},
			excludes: []string{`"path"`},
		},
		{
			name: "missing requirement without installer",
			item: ProvisionRequirementItem{
				Name:       "custom-tool",
				Satisfied:  false,
				CanInstall: false,
			},
			contains: []string{
				`"name":"custom-tool"`,
				`"satisfied":false`,
				`"canInstall":false`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.item)
			requireT.NoError(t, err)

			for _, s := range tt.contains {
				assert.Contains(t, string(data), s)
			}
			for _, s := range tt.excludes {
				assert.NotContains(t, string(data), s)
			}
		})
	}
}

func TestProvisionSummary_Defaults(t *testing.T) {
	summary := ProvisionSummary{}

	assert.Equal(t, 0, summary.TotalHosts)
	assert.Equal(t, 0, summary.ConnectedHosts)
	assert.Equal(t, 0, summary.TotalMissing)
	assert.Equal(t, 0, summary.CanInstall)
	assert.Equal(t, 0, summary.Installed)
}

func TestProvisionOutput_EmptyHosts(t *testing.T) {
	output := ProvisionOutput{
		Hosts:   []ProvisionHostResult{},
		Summary: ProvisionSummary{},
	}

	data, err := json.Marshal(output)
	requireT.NoError(t, err)

	assert.Contains(t, string(data), `"hosts":[]`)
}

func TestGetHostsToProvision_NoGlobalConfig(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Global: nil,
	}

	names, hosts, err := getHostsToProvision(resolved, "")
	assert.NoError(t, err)
	assert.Nil(t, names)
	assert.Nil(t, hosts)
}

func TestGetHostsToProvision_EmptyHosts(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Global: &config.GlobalConfig{
			Hosts: map[string]config.Host{},
		},
	}

	names, hosts, err := getHostsToProvision(resolved, "")
	assert.NoError(t, err)
	assert.Nil(t, names)
	assert.Nil(t, hosts)
}

func TestGetHostsToProvision_SpecificHost(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Global: &config.GlobalConfig{
			Hosts: map[string]config.Host{
				"mini": {SSH: []string{"mini.local"}},
				"box":  {SSH: []string{"box.local"}},
			},
		},
	}

	names, hosts, err := getHostsToProvision(resolved, "mini")
	requireT.NoError(t, err)

	assert.Len(t, names, 1)
	assert.Equal(t, "mini", names[0])
	assert.Len(t, hosts, 1)
	assert.Contains(t, hosts, "mini")
}

func TestGetHostsToProvision_SpecificHostNotFound(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Global: &config.GlobalConfig{
			Hosts: map[string]config.Host{
				"mini": {SSH: []string{"mini.local"}},
			},
		},
	}

	names, hosts, err := getHostsToProvision(resolved, "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, names)
	assert.Nil(t, hosts)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "mini") // Should list available hosts
}

func TestGetHostsToProvision_AllHosts(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Global: &config.GlobalConfig{
			Hosts: map[string]config.Host{
				"mini":  {SSH: []string{"mini.local"}},
				"box":   {SSH: []string{"box.local"}},
				"linux": {SSH: []string{"linux.local"}},
			},
		},
		Project: &config.Config{},
	}

	names, hosts, err := getHostsToProvision(resolved, "")
	requireT.NoError(t, err)

	// Should return all hosts from global config
	assert.Len(t, names, 3)
	assert.Len(t, hosts, 3)
}

func TestFormatInstallCandidates_SingleHost(t *testing.T) {
	results := []hostCheckResult{
		{name: "mini"},
	}

	candidates := []installCandidate{
		{hostIdx: 0, toolName: "go"},
		{hostIdx: 0, toolName: "bun"},
	}

	formatted := formatInstallCandidates(results, candidates)

	assert.Contains(t, formatted, "mini")
	assert.Contains(t, formatted, "go")
	assert.Contains(t, formatted, "bun")
}

func TestFormatInstallCandidates_MultipleHosts(t *testing.T) {
	results := []hostCheckResult{
		{name: "mini"},
		{name: "box"},
	}

	candidates := []installCandidate{
		{hostIdx: 0, toolName: "go"},
		{hostIdx: 1, toolName: "bun"},
		{hostIdx: 1, toolName: "node"},
	}

	formatted := formatInstallCandidates(results, candidates)

	assert.Contains(t, formatted, "mini")
	assert.Contains(t, formatted, "box")
	assert.Contains(t, formatted, "go")
	assert.Contains(t, formatted, "bun")
	assert.Contains(t, formatted, "node")
}

func TestHostCheckResult_Structure(t *testing.T) {
	result := hostCheckResult{
		name:      "test-host",
		os:        "darwin",
		connected: true,
		reqs:      []string{"go", "bun"},
		results: []require.CheckResult{
			{Name: "go", Satisfied: true, Path: "/usr/local/go/bin/go"},
			{Name: "bun", Satisfied: false, CanInstall: true},
		},
		installed:  []string{"bun"},
		installErr: map[string]error{},
	}

	assert.Equal(t, "test-host", result.name)
	assert.Equal(t, "darwin", result.os)
	assert.True(t, result.connected)
	assert.Len(t, result.reqs, 2)
	assert.Len(t, result.results, 2)
	assert.Len(t, result.installed, 1)
	assert.Contains(t, result.installed, "bun")
}

func TestHostCheckResult_WithError(t *testing.T) {
	result := hostCheckResult{
		name:       "failed-host",
		connected:  false,
		connErr:    fmt.Errorf("connection refused"),
		installErr: make(map[string]error),
	}

	assert.False(t, result.connected)
	assert.NotNil(t, result.connErr)
	assert.Equal(t, "connection refused", result.connErr.Error())
}

func TestProvisionOptions_Defaults(t *testing.T) {
	opts := ProvisionOptions{}

	assert.Empty(t, opts.Host)
	assert.False(t, opts.CheckOnly)
	assert.False(t, opts.AutoYes)
	assert.False(t, opts.MachineOut)
}

func TestProvisionOptions_AllFlags(t *testing.T) {
	opts := ProvisionOptions{
		Host:       "mini",
		CheckOnly:  true,
		AutoYes:    true,
		MachineOut: true,
	}

	assert.Equal(t, "mini", opts.Host)
	assert.True(t, opts.CheckOnly)
	assert.True(t, opts.AutoYes)
	assert.True(t, opts.MachineOut)
}

func TestProvisionOutput_FullStructure(t *testing.T) {
	output := ProvisionOutput{
		Hosts: []ProvisionHostResult{
			{
				Name:      "dev",
				OS:        "darwin",
				Connected: true,
				Requirements: []ProvisionRequirementItem{
					{Name: "go", Satisfied: true, Path: "/usr/local/go/bin/go", CanInstall: true},
					{Name: "node", Satisfied: true, Path: "/usr/local/bin/node", CanInstall: true},
				},
			},
			{
				Name:      "staging",
				OS:        "linux",
				Connected: true,
				Requirements: []ProvisionRequirementItem{
					{Name: "go", Satisfied: false, CanInstall: true, Installed: true},
					{Name: "custom", Satisfied: false, CanInstall: false},
				},
			},
			{
				Name:      "prod",
				OS:        "",
				Connected: false,
				Error:     "timeout",
			},
		},
		Summary: ProvisionSummary{
			TotalHosts:     3,
			ConnectedHosts: 2,
			TotalMissing:   2,
			CanInstall:     1,
			Installed:      1,
		},
	}

	data, err := json.Marshal(output)
	requireT.NoError(t, err)

	var decoded ProvisionOutput
	err = json.Unmarshal(data, &decoded)
	requireT.NoError(t, err)

	assert.Len(t, decoded.Hosts, 3)
	assert.Equal(t, 3, decoded.Summary.TotalHosts)
	assert.Equal(t, 2, decoded.Summary.ConnectedHosts)
	assert.Equal(t, 2, decoded.Summary.TotalMissing)
	assert.Equal(t, 1, decoded.Summary.CanInstall)
	assert.Equal(t, 1, decoded.Summary.Installed)
}

func TestProvisionSummary_VariousCombinations(t *testing.T) {
	tests := []struct {
		name    string
		summary ProvisionSummary
	}{
		{
			name: "all zeros",
			summary: ProvisionSummary{
				TotalHosts: 0, ConnectedHosts: 0, TotalMissing: 0, CanInstall: 0, Installed: 0,
			},
		},
		{
			name: "all satisfied",
			summary: ProvisionSummary{
				TotalHosts: 5, ConnectedHosts: 5, TotalMissing: 0, CanInstall: 0, Installed: 0,
			},
		},
		{
			name: "some missing with installs",
			summary: ProvisionSummary{
				TotalHosts: 3, ConnectedHosts: 3, TotalMissing: 5, CanInstall: 4, Installed: 4,
			},
		},
		{
			name: "connection failures",
			summary: ProvisionSummary{
				TotalHosts: 5, ConnectedHosts: 2, TotalMissing: 0, CanInstall: 0, Installed: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.summary)
			requireT.NoError(t, err)

			var decoded ProvisionSummary
			err = json.Unmarshal(data, &decoded)
			requireT.NoError(t, err)

			assert.Equal(t, tt.summary.TotalHosts, decoded.TotalHosts)
			assert.Equal(t, tt.summary.ConnectedHosts, decoded.ConnectedHosts)
			assert.Equal(t, tt.summary.TotalMissing, decoded.TotalMissing)
			assert.Equal(t, tt.summary.CanInstall, decoded.CanInstall)
			assert.Equal(t, tt.summary.Installed, decoded.Installed)
		})
	}
}

func TestOutputProvisionJSON_Format(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "test-host",
			os:        "darwin",
			connected: true,
			results: []require.CheckResult{
				{Name: "go", Satisfied: true, Path: "/usr/local/go/bin/go", CanInstall: true},
			},
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionJSON(results)
	})

	// Should be valid JSON
	var decoded ProvisionOutput
	err := json.Unmarshal([]byte(output), &decoded)
	requireT.NoError(t, err)

	// Verify structure
	assert.Len(t, decoded.Hosts, 1)
	assert.Equal(t, "test-host", decoded.Hosts[0].Name)
	assert.Equal(t, "darwin", decoded.Hosts[0].OS)
	assert.True(t, decoded.Hosts[0].Connected)
}

func TestOutputProvisionJSON_WithMissing(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "test-host",
			os:        "darwin",
			connected: true,
			results: []require.CheckResult{
				{Name: "go", Satisfied: true},
				{Name: "bun", Satisfied: false, CanInstall: true},
				{Name: "custom", Satisfied: false, CanInstall: false},
			},
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionJSON(results)
	})

	var decoded ProvisionOutput
	err := json.Unmarshal([]byte(output), &decoded)
	requireT.NoError(t, err)

	assert.Equal(t, 1, decoded.Summary.TotalHosts)
	assert.Equal(t, 1, decoded.Summary.ConnectedHosts)
	assert.Equal(t, 2, decoded.Summary.TotalMissing)
	assert.Equal(t, 1, decoded.Summary.CanInstall)
}

func TestOutputProvisionJSON_ConnectionError(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "failed-host",
			connected: false,
			connErr:   fmt.Errorf("connection refused"),
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionJSON(results)
	})

	var decoded ProvisionOutput
	err := json.Unmarshal([]byte(output), &decoded)
	requireT.NoError(t, err)

	assert.Len(t, decoded.Hosts, 1)
	assert.False(t, decoded.Hosts[0].Connected)
	assert.Equal(t, "connection refused", decoded.Hosts[0].Error)
	assert.Equal(t, 1, decoded.Summary.TotalHosts)
	assert.Equal(t, 0, decoded.Summary.ConnectedHosts)
}

func TestOutputProvisionText_AllSatisfied(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "mini",
			os:        "darwin",
			connected: true,
			reqs:      []string{"go", "bun"},
			results: []require.CheckResult{
				{Name: "go", Satisfied: true},
				{Name: "bun", Satisfied: true},
			},
			installErr: make(map[string]error),
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionText(results, ProvisionOptions{})
	})

	assert.Contains(t, output, "mini")
	assert.Contains(t, output, "darwin")
	assert.Contains(t, output, "All requirements satisfied")
}

func TestOutputProvisionText_MissingTools(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "mini",
			os:        "darwin",
			connected: true,
			reqs:      []string{"go", "bun"},
			results: []require.CheckResult{
				{Name: "go", Satisfied: true},
				{Name: "bun", Satisfied: false, CanInstall: true},
			},
			installErr: make(map[string]error),
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionText(results, ProvisionOptions{CheckOnly: true})
	})

	assert.Contains(t, output, "mini")
	assert.Contains(t, output, "go")
	assert.Contains(t, output, "bun")
	assert.Contains(t, output, "can install")
	assert.Contains(t, output, "rr provision")
}

func TestOutputProvisionText_NoRequirements(t *testing.T) {
	results := []hostCheckResult{
		{
			name:       "mini",
			os:         "darwin",
			connected:  true,
			reqs:       []string{},
			results:    []require.CheckResult{},
			installErr: make(map[string]error),
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionText(results, ProvisionOptions{})
	})

	assert.Contains(t, output, "mini")
	assert.Contains(t, output, "No requirements configured")
}

func TestOutputProvisionText_ConnectionFailed(t *testing.T) {
	results := []hostCheckResult{
		{
			name:       "mini",
			connected:  false,
			connErr:    fmt.Errorf("connection refused"),
			installErr: make(map[string]error),
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionText(results, ProvisionOptions{})
	})

	assert.Contains(t, output, "mini")
	assert.Contains(t, output, "Could not connect")
	assert.Contains(t, output, "connection refused")
}

func TestOutputProvisionText_ManualInstallRequired(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "mini",
			os:        "darwin",
			connected: true,
			reqs:      []string{"custom-tool"},
			results: []require.CheckResult{
				{Name: "custom-tool", Satisfied: false, CanInstall: false},
			},
			installErr: make(map[string]error),
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionText(results, ProvisionOptions{})
	})

	assert.Contains(t, output, "custom-tool")
	assert.Contains(t, output, "manual install required")
}

func TestOutputProvisionText_InstalledTool(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "mini",
			os:        "darwin",
			connected: true,
			reqs:      []string{"bun"},
			results: []require.CheckResult{
				{Name: "bun", Satisfied: false, CanInstall: true},
			},
			installed:  []string{"bun"},
			installErr: make(map[string]error),
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionText(results, ProvisionOptions{})
	})

	assert.Contains(t, output, "bun")
	assert.Contains(t, output, "installed")
	assert.Contains(t, output, "1 tool(s) installed")
}

func TestOutputProvisionText_InstallFailed(t *testing.T) {
	results := []hostCheckResult{
		{
			name:      "mini",
			os:        "darwin",
			connected: true,
			reqs:      []string{"bun"},
			results: []require.CheckResult{
				{Name: "bun", Satisfied: false, CanInstall: true},
			},
			installed: []string{},
			installErr: map[string]error{
				"bun": fmt.Errorf("installation failed"),
			},
		},
	}

	output := captureOutput(func() {
		_ = outputProvisionText(results, ProvisionOptions{})
	})

	assert.Contains(t, output, "bun")
	assert.Contains(t, output, "install failed")
}
