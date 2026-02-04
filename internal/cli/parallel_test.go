package cli

import (
	"os"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/stretchr/testify/assert"
)

func TestFilterHostsByTag(t *testing.T) {
	tests := []struct {
		name      string
		hosts     map[string]config.Host
		hostOrder []string
		tag       string
		wantHosts []string
		wantOrder []string
	}{
		{
			name: "filters to matching tag",
			hosts: map[string]config.Host{
				"gpu-box":    {Tags: []string{"gpu", "linux"}},
				"cpu-server": {Tags: []string{"linux"}},
				"mac-mini":   {Tags: []string{"macos"}},
			},
			hostOrder: []string{"gpu-box", "cpu-server", "mac-mini"},
			tag:       "linux",
			wantHosts: []string{"gpu-box", "cpu-server"},
			wantOrder: []string{"gpu-box", "cpu-server"},
		},
		{
			name: "preserves order",
			hosts: map[string]config.Host{
				"host-a": {Tags: []string{"fast"}},
				"host-b": {Tags: []string{"fast"}},
				"host-c": {Tags: []string{"fast"}},
			},
			hostOrder: []string{"host-c", "host-a", "host-b"},
			tag:       "fast",
			wantHosts: []string{"host-c", "host-a", "host-b"},
			wantOrder: []string{"host-c", "host-a", "host-b"},
		},
		{
			name: "no matches returns empty",
			hosts: map[string]config.Host{
				"host-a": {Tags: []string{"linux"}},
				"host-b": {Tags: []string{"macos"}},
			},
			hostOrder: []string{"host-a", "host-b"},
			tag:       "windows",
			wantHosts: []string{},
			wantOrder: []string{},
		},
		{
			name:      "empty hosts returns empty",
			hosts:     map[string]config.Host{},
			hostOrder: []string{},
			tag:       "any",
			wantHosts: []string{},
			wantOrder: []string{},
		},
		{
			name: "host in order but not in map is skipped",
			hosts: map[string]config.Host{
				"existing": {Tags: []string{"target"}},
			},
			hostOrder: []string{"missing", "existing"},
			tag:       "target",
			wantHosts: []string{"existing"},
			wantOrder: []string{"existing"},
		},
		{
			name: "host with no tags is not matched",
			hosts: map[string]config.Host{
				"tagged":   {Tags: []string{"target"}},
				"untagged": {Tags: nil},
			},
			hostOrder: []string{"tagged", "untagged"},
			tag:       "target",
			wantHosts: []string{"tagged"},
			wantOrder: []string{"tagged"},
		},
		{
			name: "single host with matching tag",
			hosts: map[string]config.Host{
				"only-one": {Tags: []string{"special"}},
			},
			hostOrder: []string{"only-one"},
			tag:       "special",
			wantHosts: []string{"only-one"},
			wantOrder: []string{"only-one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHosts, gotOrder := filterHostsByTag(tt.hosts, tt.hostOrder, tt.tag)

			// Check host count
			assert.Len(t, gotHosts, len(tt.wantHosts))
			assert.Equal(t, tt.wantOrder, gotOrder)

			// Verify all expected hosts are present
			for _, name := range tt.wantHosts {
				_, ok := gotHosts[name]
				assert.True(t, ok, "expected host %s to be in filtered result", name)
			}
		})
	}
}

func TestFilterHostsByTag_PreservesHostData(t *testing.T) {
	// Verify that the filtered hosts retain their original data
	hosts := map[string]config.Host{
		"test-host": {
			SSH:  []string{"test.local", "test.vpn"},
			Dir:  "/home/user/project",
			Tags: []string{"target", "other"},
			Env: map[string]string{
				"KEY": "value",
			},
		},
	}
	hostOrder := []string{"test-host"}

	filtered, order := filterHostsByTag(hosts, hostOrder, "target")

	assert.Len(t, filtered, 1)
	assert.Equal(t, []string{"test-host"}, order)

	host := filtered["test-host"]
	assert.Equal(t, []string{"test.local", "test.vpn"}, host.SSH)
	assert.Equal(t, "/home/user/project", host.Dir)
	assert.Equal(t, []string{"target", "other"}, host.Tags)
	assert.Equal(t, "value", host.Env["KEY"])
}

func TestDetermineOutputMode(t *testing.T) {
	tests := []struct {
		name     string
		opts     ParallelTaskOptions
		task     *config.TaskConfig
		expected parallel.OutputMode
	}{
		{
			name:     "default is progress",
			opts:     ParallelTaskOptions{},
			task:     &config.TaskConfig{},
			expected: parallel.OutputProgress,
		},
		{
			name:     "stream flag takes precedence",
			opts:     ParallelTaskOptions{Stream: true},
			task:     &config.TaskConfig{Output: "quiet"},
			expected: parallel.OutputStream,
		},
		{
			name:     "verbose flag takes precedence",
			opts:     ParallelTaskOptions{Verbose: true},
			task:     &config.TaskConfig{Output: "quiet"},
			expected: parallel.OutputVerbose,
		},
		{
			name:     "quiet flag takes precedence",
			opts:     ParallelTaskOptions{Quiet: true},
			task:     &config.TaskConfig{Output: "stream"},
			expected: parallel.OutputQuiet,
		},
		{
			name:     "task output stream",
			opts:     ParallelTaskOptions{},
			task:     &config.TaskConfig{Output: "stream"},
			expected: parallel.OutputStream,
		},
		{
			name:     "task output verbose",
			opts:     ParallelTaskOptions{},
			task:     &config.TaskConfig{Output: "verbose"},
			expected: parallel.OutputVerbose,
		},
		{
			name:     "task output quiet",
			opts:     ParallelTaskOptions{},
			task:     &config.TaskConfig{Output: "quiet"},
			expected: parallel.OutputQuiet,
		},
		{
			name:     "task output progress",
			opts:     ParallelTaskOptions{},
			task:     &config.TaskConfig{Output: "progress"},
			expected: parallel.OutputProgress,
		},
		{
			name:     "unknown task output falls back to progress",
			opts:     ParallelTaskOptions{},
			task:     &config.TaskConfig{Output: "invalid"},
			expected: parallel.OutputProgress,
		},
		{
			name:     "stream flag precedence over verbose",
			opts:     ParallelTaskOptions{Stream: true, Verbose: true},
			task:     &config.TaskConfig{},
			expected: parallel.OutputStream,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineOutputMode(tt.opts, tt.task)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildStepsCommand(t *testing.T) {
	tests := []struct {
		name     string
		steps    []config.TaskStep
		expected string
	}{
		{
			name:     "empty steps",
			steps:    []config.TaskStep{},
			expected: "",
		},
		{
			name:     "single step",
			steps:    []config.TaskStep{{Run: "echo hello"}},
			expected: "echo hello",
		},
		{
			name: "two steps",
			steps: []config.TaskStep{
				{Run: "echo first"},
				{Run: "echo second"},
			},
			expected: "(echo first) && (echo second)",
		},
		{
			name: "three steps",
			steps: []config.TaskStep{
				{Run: "npm install"},
				{Run: "npm run build"},
				{Run: "npm test"},
			},
			expected: "(npm install) && (npm run build) && (npm test)",
		},
		{
			name: "steps with shell metacharacters",
			steps: []config.TaskStep{
				{Run: "cat file | grep pattern"},
				{Run: "echo $VAR && true"},
			},
			expected: "(cat file | grep pattern) && (echo $VAR && true)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildStepsCommand(tt.steps)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetParallelFlagValues(t *testing.T) {
	tests := []struct {
		name        string
		stream      bool
		verbose     bool
		quiet       bool
		failFast    bool
		maxParallel int
		noLogs      bool
		dryRun      bool
	}{
		{
			name:        "all defaults",
			stream:      false,
			verbose:     false,
			quiet:       false,
			failFast:    false,
			maxParallel: 0,
			noLogs:      false,
			dryRun:      false,
		},
		{
			name:        "stream enabled",
			stream:      true,
			verbose:     false,
			quiet:       false,
			failFast:    false,
			maxParallel: 0,
			noLogs:      false,
			dryRun:      false,
		},
		{
			name:        "all flags enabled",
			stream:      true,
			verbose:     true,
			quiet:       true,
			failFast:    true,
			maxParallel: 4,
			noLogs:      true,
			dryRun:      true,
		},
		{
			name:        "max parallel set",
			stream:      false,
			verbose:     false,
			quiet:       false,
			failFast:    false,
			maxParallel: 8,
			noLogs:      false,
			dryRun:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetParallelFlagValues(tt.stream, tt.verbose, tt.quiet, tt.failFast, tt.maxParallel, tt.noLogs, tt.dryRun)

			assert.Equal(t, tt.stream, result.Stream)
			assert.Equal(t, tt.verbose, result.Verbose)
			assert.Equal(t, tt.quiet, result.Quiet)
			assert.Equal(t, tt.failFast, result.FailFast)
			assert.Equal(t, tt.maxParallel, result.MaxParallel)
			assert.Equal(t, tt.noLogs, result.NoLogs)
			assert.Equal(t, tt.dryRun, result.DryRun)
		})
	}
}

func TestExpandLogsDir(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tilde prefix expands",
			input:    "~/.rr/logs",
			expected: home + "/.rr/logs",
		},
		{
			name:     "tilde alone expands",
			input:    "~",
			expected: home,
		},
		{
			name:     "absolute path unchanged",
			input:    "/var/log/rr",
			expected: "/var/log/rr",
		},
		{
			name:     "relative path unchanged",
			input:    "logs/output",
			expected: "logs/output",
		},
		{
			name:     "empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "tilde in middle not expanded",
			input:    "/home/~user/logs",
			expected: "/home/~user/logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandLogsDir(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
