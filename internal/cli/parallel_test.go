package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestParallelTaskOptions_ArgsField verifies the Args field is present
// and carries through as expected (struct-level coverage for forward_args plumbing).
func TestParallelTaskOptions_ArgsField(t *testing.T) {
	opts := ParallelTaskOptions{
		TaskName: "test-backend",
		Args:     []string{"-k", "bond", "-v"},
	}
	assert.Equal(t, []string{"-k", "bond", "-v"}, opts.Args)
}

// TestForwardArgs_ConfigFieldPresent verifies the ForwardArgs field is on TaskConfig.
func TestForwardArgs_ConfigFieldPresent(t *testing.T) {
	task := config.TaskConfig{
		Parallel:    []string{"sub-a", "sub-b"},
		ForwardArgs: true,
	}
	assert.True(t, task.ForwardArgs)

	taskDefault := config.TaskConfig{
		Parallel: []string{"sub-a"},
	}
	assert.False(t, taskDefault.ForwardArgs, "ForwardArgs should default to false")
}

// TestParallelTask_RejectsArgsViaCobra verifies that a parallel task without
// forward_args rejects extra positional arguments with a descriptive error.
func TestParallelTask_RejectsArgsViaCobra(t *testing.T) {
	task := config.TaskConfig{
		Parallel: []string{"sub"},
	}
	cmd := createParallelTaskCommand("my-task", task)
	cmd.SetArgs([]string{"extra-arg"})
	execErr := cmd.Execute()
	require.Error(t, execErr)
	assert.Contains(t, execErr.Error(), "doesn't accept extra arguments")
	assert.Contains(t, execErr.Error(), "extra-arg")
}

// TestBuildSubtaskInfos_ForwardArgsAppended verifies that when forward_args is true
// extra args are appended to each subtask's run command.
func TestBuildSubtaskInfos_ForwardArgsAppended(t *testing.T) {
	proj := &config.Config{
		Tasks: map[string]config.TaskConfig{
			"sub-a": {Run: "pytest tests/a"},
			"sub-b": {Run: "pytest tests/b"},
		},
	}
	parentTask := &config.TaskConfig{
		Parallel:    []string{"sub-a", "sub-b"},
		ForwardArgs: true,
	}
	args := []string{"-k", "bond", "-v"}

	infos, err := buildSubtaskInfos(proj, parentTask, []string{"sub-a", "sub-b"}, args)
	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.Equal(t, "pytest tests/a '-k' 'bond' '-v'", infos[0].Command)
	assert.Equal(t, "pytest tests/b '-k' 'bond' '-v'", infos[1].Command)
}

// TestBuildSubtaskInfos_ForwardArgsRejectsStepsSubtask verifies that forward_args
// produces a clear error when a subtask uses steps rather than a single run command.
func TestBuildSubtaskInfos_ForwardArgsRejectsStepsSubtask(t *testing.T) {
	proj := &config.Config{
		Tasks: map[string]config.TaskConfig{
			"multi-step": {Steps: []config.TaskStep{{Run: "step1"}, {Run: "step2"}}},
		},
	}
	parentTask := &config.TaskConfig{
		Parallel:    []string{"multi-step"},
		ForwardArgs: true,
	}

	_, err := buildSubtaskInfos(proj, parentTask, []string{"multi-step"}, []string{"-k", "bond"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uses steps and cannot accept forwarded args")
}

// TestBuildSubtaskInfos_NoForwardArgsIgnoresArgs verifies that when forward_args is
// false, extra args are not appended (they should have been rejected earlier by cobra).
func TestBuildSubtaskInfos_NoForwardArgsIgnoresArgs(t *testing.T) {
	proj := &config.Config{
		Tasks: map[string]config.TaskConfig{
			"sub-a": {Run: "pytest tests/a"},
		},
	}
	parentTask := &config.TaskConfig{
		Parallel:    []string{"sub-a"},
		ForwardArgs: false,
	}

	infos, err := buildSubtaskInfos(proj, parentTask, []string{"sub-a"}, []string{"-k", "bond"})
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "pytest tests/a", infos[0].Command, "args should not be appended when forward_args is false")
}
