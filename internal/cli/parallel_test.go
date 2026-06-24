package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/parallel"
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

// pytestFailureOutput returns realistic pytest output containing a failure.
func pytestFailureOutput(testName, file string, line int, message string) []byte {
	return []byte(fmt.Sprintf(`=================================== FAILURES ===================================
_________________________________ %s _________________________________

    def %s():
>       assert False
E       %s

%s:%d: AssertionError
=========================== short test summary info ============================
FAILED %s::%s - %s
============================== 1 failed, 2 passed ==============================
`, testName, testName, message, file, line, file, testName, message))
}

func TestExtractTaskFailures_PytestFailures(t *testing.T) {
	result := &parallel.Result{
		Passed: 1,
		Failed: 1,
		TaskResults: []parallel.TaskResult{
			{
				TaskName: "test-backend-api",
				Host:     "m1-linux",
				ExitCode: 0,
				Command:  "pytest tests/api",
				Output:   []byte("all passed"),
			},
			{
				TaskName: "test-backend-services",
				Host:     "m1-linux",
				ExitCode: 1,
				Command:  "cd backend && uv run pytest tests/services -qq --tb=short",
				Output:   pytestFailureOutput("test_reject_request", "tests/services/test_requests.py", 42, "AssertionError: expected 403, got 200"),
			},
		},
	}

	failures := extractTaskFailures(result)
	require.Len(t, failures, 1)

	f := failures[0]
	assert.Equal(t, "test-backend-services", f["task"])
	assert.Equal(t, "m1-linux", f["host"])
	assert.Equal(t, 1, f["exit_code"])

	tests, ok := f["tests"].([]map[string]string)
	require.True(t, ok, "failures should contain structured test info")
	require.NotEmpty(t, tests)
	assert.Equal(t, "test_reject_request", tests[0]["name"])
	assert.Contains(t, tests[0]["file"], "tests/services/test_requests.py")
	assert.Contains(t, tests[0]["message"], "AssertionError")
}

func TestExtractTaskFailures_FallbackToOutputTail(t *testing.T) {
	result := &parallel.Result{
		Passed: 0,
		Failed: 1,
		TaskResults: []parallel.TaskResult{
			{
				TaskName: "build-frontend",
				Host:     "m4-mini",
				ExitCode: 1,
				Command:  "bun run build",
				Output:   []byte("Compiling...\nError: Cannot find module 'foo'\n  at /app/src/index.ts:5:1"),
			},
		},
	}

	failures := extractTaskFailures(result)
	require.Len(t, failures, 1)

	f := failures[0]
	assert.Equal(t, "build-frontend", f["task"])
	_, hasTests := f["tests"]
	assert.False(t, hasTests, "non-test output should not produce structured tests")

	tail, ok := f["output_tail"].(string)
	require.True(t, ok, "non-test failures should include output_tail")
	assert.Contains(t, tail, "Cannot find module 'foo'")
}

func TestExtractTaskFailures_EmptyOutput(t *testing.T) {
	result := &parallel.Result{
		Failed: 1,
		TaskResults: []parallel.TaskResult{
			{
				TaskName: "timeout-task",
				Host:     "m1-linux",
				ExitCode: 1,
				Command:  "sleep 999",
				Output:   nil,
				Error:    fmt.Errorf("task timed out after 5m"),
			},
		},
	}

	failures := extractTaskFailures(result)
	require.Len(t, failures, 1)

	f := failures[0]
	assert.Equal(t, "timeout-task", f["task"])
	assert.Equal(t, "task timed out after 5m", f["error"])
	_, hasTests := f["tests"]
	assert.False(t, hasTests)
	_, hasTail := f["output_tail"]
	assert.False(t, hasTail)
}

func TestExtractTaskFailures_SkipsPassingTasks(t *testing.T) {
	result := &parallel.Result{
		Passed: 2,
		Failed: 0,
		TaskResults: []parallel.TaskResult{
			{TaskName: "test-a", ExitCode: 0, Command: "pytest"},
			{TaskName: "test-b", ExitCode: 0, Command: "pytest"},
		},
	}

	failures := extractTaskFailures(result)
	assert.Empty(t, failures)
}

func TestExtractTaskFailures_TruncatesLongMessages(t *testing.T) {
	longMessage := strings.Repeat("x", maxFailureMessageLen+100)

	result := &parallel.Result{
		Failed: 1,
		TaskResults: []parallel.TaskResult{
			{
				TaskName: "test-long",
				Host:     "m1-linux",
				ExitCode: 1,
				Command:  "cd backend && uv run pytest tests -qq --tb=short",
				Output:   pytestFailureOutput("test_long_output", "tests/test_long.py", 10, longMessage),
			},
		},
	}

	failures := extractTaskFailures(result)
	require.Len(t, failures, 1)

	tests, ok := failures[0]["tests"].([]map[string]string)
	require.True(t, ok)
	require.NotEmpty(t, tests)
	assert.LessOrEqual(t, len(tests[0]["message"]), maxFailureMessageLen+3) // +3 for "..."
	assert.True(t, strings.HasSuffix(tests[0]["message"], "..."))
}

func TestExtractTaskFailures_OutputTailCapped(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d: some build output", i)
	}

	result := &parallel.Result{
		Failed: 1,
		TaskResults: []parallel.TaskResult{
			{
				TaskName: "build-fail",
				Host:     "m4-mini",
				ExitCode: 1,
				Command:  "make build",
				Output:   []byte(strings.Join(lines, "\n")),
			},
		},
	}

	failures := extractTaskFailures(result)
	require.Len(t, failures, 1)

	tail, ok := failures[0]["output_tail"].(string)
	require.True(t, ok)
	tailLines := strings.Split(tail, "\n")
	assert.LessOrEqual(t, len(tailLines), maxOutputTailLines)
	assert.Contains(t, tail, "line 99")
	assert.NotContains(t, tail, "line 0:")
}

func TestRenderParallelResult_MachineMode_IncludesFailures(t *testing.T) {
	oldPretty := prettyMode
	defer func() { prettyMode = oldPretty }()
	prettyMode = false

	result := &parallel.Result{
		Passed: 1,
		Failed: 1,
		TaskResults: []parallel.TaskResult{
			{TaskName: "test-pass", ExitCode: 0, Command: "pytest"},
			{
				TaskName: "test-fail",
				Host:     "m1-linux",
				ExitCode: 1,
				Command:  "cd backend && uv run pytest tests -qq --tb=short",
				Output:   pytestFailureOutput("test_broken", "tests/test_broken.py", 7, "assert 1 == 2"),
			},
		},
	}

	output := captureStderr(t, func() {
		renderParallelResult(result, nil, "test")
	})

	var event PhaseEvent
	err := json.Unmarshal([]byte(output), &event)
	require.NoError(t, err, "machine-mode output should be valid JSON: %s", output)

	assert.Equal(t, "result", event.Type)
	assert.Equal(t, "failed", event.Status)

	failuresRaw, ok := event.Details["failures"]
	require.True(t, ok, "failed result must include 'failures' key in details")

	failuresJSON, err := json.Marshal(failuresRaw)
	require.NoError(t, err)

	var failures []map[string]interface{}
	err = json.Unmarshal(failuresJSON, &failures)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	assert.Equal(t, "test-fail", failures[0]["task"])
	assert.Equal(t, "m1-linux", failures[0]["host"])
}

func TestRenderParallelResult_MachineMode_SuccessHasNoFailures(t *testing.T) {
	oldPretty := prettyMode
	defer func() { prettyMode = oldPretty }()
	prettyMode = false

	result := &parallel.Result{
		Passed: 2,
		Failed: 0,
		TaskResults: []parallel.TaskResult{
			{TaskName: "test-a", ExitCode: 0, Command: "pytest"},
			{TaskName: "test-b", ExitCode: 0, Command: "pytest"},
		},
	}

	output := captureStderr(t, func() {
		renderParallelResult(result, nil, "test")
	})

	var event PhaseEvent
	err := json.Unmarshal([]byte(output), &event)
	require.NoError(t, err)

	assert.Equal(t, "success", event.Status)
	_, hasFailures := event.Details["failures"]
	assert.False(t, hasFailures, "successful result should not include failures key")
}
