package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTask_Success(t *testing.T) {
	cfg := &Config{
		Tasks: map[string]TaskConfig{
			"test": {
				Run:         "make test",
				Description: "Run tests",
			},
			"build": {
				Steps: []TaskStep{
					{Name: "compile", Run: "make build"},
					{Name: "package", Run: "make package"},
				},
			},
		},
	}

	// Get existing task
	task, err := GetTask(cfg, "test")
	require.NoError(t, err)
	assert.Equal(t, "make test", task.Run)
	assert.Equal(t, "Run tests", task.Description)

	// Get another task
	task, err = GetTask(cfg, "build")
	require.NoError(t, err)
	assert.Len(t, task.Steps, 2)
}

func TestGetTask_NotFound(t *testing.T) {
	cfg := &Config{
		Tasks: map[string]TaskConfig{
			"test": {Run: "make test"},
		},
	}

	task, err := GetTask(cfg, "nonexistent")
	require.Error(t, err)
	assert.Nil(t, task)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestGetTask_NilConfig(t *testing.T) {
	task, err := GetTask(nil, "test")
	require.Error(t, err)
	assert.Nil(t, task)
	assert.Contains(t, err.Error(), "nil")
}

func TestGetTask_NoTasks(t *testing.T) {
	cfg := &Config{}

	task, err := GetTask(cfg, "test")
	require.Error(t, err)
	assert.Nil(t, task)
	assert.Contains(t, err.Error(), "No tasks defined")
}

func TestGetTaskWithMergedEnv(t *testing.T) {
	cfg := &Config{
		Hosts: map[string]Host{
			"server": {
				SSH: []string{"server"},
				Dir: "/home/user",
				Env: map[string]string{
					"HOST_VAR":   "host_value",
					"SHARED_VAR": "from_host",
				},
			},
		},
		Tasks: map[string]TaskConfig{
			"test": {
				Run: "make test",
				Env: map[string]string{
					"TASK_VAR":   "task_value",
					"SHARED_VAR": "from_task", // Should override host
				},
			},
		},
	}

	task, env, err := GetTaskWithMergedEnv(cfg, "test", "server")
	require.NoError(t, err)
	assert.NotNil(t, task)

	// Host env should be present
	assert.Equal(t, "host_value", env["HOST_VAR"])

	// Task env should be present
	assert.Equal(t, "task_value", env["TASK_VAR"])

	// Task should override host for shared vars
	assert.Equal(t, "from_task", env["SHARED_VAR"])
}

func TestGetTaskWithMergedEnv_NoHost(t *testing.T) {
	cfg := &Config{
		Tasks: map[string]TaskConfig{
			"test": {
				Run: "make test",
				Env: map[string]string{
					"TASK_VAR": "task_value",
				},
			},
		},
	}

	task, env, err := GetTaskWithMergedEnv(cfg, "test", "")
	require.NoError(t, err)
	assert.NotNil(t, task)
	assert.Equal(t, "task_value", env["TASK_VAR"])
}

func TestTaskNames(t *testing.T) {
	cfg := &Config{
		Tasks: map[string]TaskConfig{
			"test":  {Run: "make test"},
			"build": {Run: "make build"},
			"lint":  {Run: "make lint"},
		},
	}

	names := TaskNames(cfg)
	assert.Len(t, names, 3)
	assert.Contains(t, names, "test")
	assert.Contains(t, names, "build")
	assert.Contains(t, names, "lint")
}

func TestTaskNames_Nil(t *testing.T) {
	names := TaskNames(nil)
	assert.Nil(t, names)

	cfg := &Config{}
	names = TaskNames(cfg)
	assert.Nil(t, names)
}

func TestIsTaskHostAllowed(t *testing.T) {
	tests := []struct {
		name     string
		task     *TaskConfig
		hostName string
		expected bool
	}{
		{
			name:     "nil task",
			task:     nil,
			hostName: "server",
			expected: true,
		},
		{
			name:     "no restrictions",
			task:     &TaskConfig{Run: "test"},
			hostName: "server",
			expected: true,
		},
		{
			name: "allowed host",
			task: &TaskConfig{
				Run:   "test",
				Hosts: []string{"server1", "server2"},
			},
			hostName: "server1",
			expected: true,
		},
		{
			name: "not allowed host",
			task: &TaskConfig{
				Run:   "test",
				Hosts: []string{"server1", "server2"},
			},
			hostName: "server3",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTaskHostAllowed(tt.task, tt.hostName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetStepOnFail(t *testing.T) {
	tests := []struct {
		name     string
		step     TaskStep
		expected string
	}{
		{
			name:     "empty on_fail defaults to stop",
			step:     TaskStep{Run: "test"},
			expected: OnFailStop,
		},
		{
			name:     "explicit stop",
			step:     TaskStep{Run: "test", OnFail: "stop"},
			expected: OnFailStop,
		},
		{
			name:     "explicit continue",
			step:     TaskStep{Run: "test", OnFail: "continue"},
			expected: OnFailContinue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetStepOnFail(tt.step)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatList(t *testing.T) {
	tests := []struct {
		name     string
		items    []string
		expected string
	}{
		{
			name:     "empty",
			items:    []string{},
			expected: "(none)",
		},
		{
			name:     "single",
			items:    []string{"one"},
			expected: "one",
		},
		{
			name:     "multiple",
			items:    []string{"one", "two", "three"},
			expected: "one, two, three",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatList(tt.items)
			assert.Equal(t, tt.expected, result)
		})
	}
}
