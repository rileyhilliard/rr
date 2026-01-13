package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateParallelTasks(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "no tasks",
			config:  &Config{},
			wantErr: false,
		},
		{
			name: "valid parallel task",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"test": {Run: "go test ./..."},
					"lint": {Run: "golangci-lint run"},
					"all": {
						Parallel: []string{"test", "lint"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "parallel references non-existent task",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"test": {Run: "go test ./..."},
					"all": {
						Parallel: []string{"test", "missing"},
					},
				},
			},
			wantErr:     true,
			errContains: "non-existent task 'missing'",
		},
		{
			name: "parallel task references another parallel task",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"test": {Run: "go test ./..."},
					"lint": {Run: "golangci-lint run"},
					"inner": {
						Parallel: []string{"test", "lint"},
					},
					"outer": {
						Parallel: []string{"inner"},
					},
				},
			},
			wantErr:     true,
			errContains: "can't reference another parallel task",
		},
		{
			name: "parallel task references itself",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"recursive": {
						Parallel: []string{"recursive"},
					},
				},
			},
			wantErr: true,
			// Self-reference is caught as nested parallel error (task references itself which is parallel)
			errContains: "can't reference another parallel task",
		},
		{
			name: "parallel task can reference step-based task",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"multi-step": {
						Steps: []TaskStep{
							{Name: "step1", Run: "echo step1"},
							{Name: "step2", Run: "echo step2"},
						},
					},
					"test": {Run: "go test ./..."},
					"all": {
						Parallel: []string{"multi-step", "test"},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParallelTasks(tt.config)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTask_Parallel(t *testing.T) {
	tests := []struct {
		name        string
		task        TaskConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid parallel only",
			task: TaskConfig{
				Parallel: []string{"test", "lint"},
			},
			wantErr: false,
		},
		{
			name: "parallel with run",
			task: TaskConfig{
				Run:      "make all",
				Parallel: []string{"test"},
			},
			wantErr:     true,
			errContains: "both 'parallel' and 'run'",
		},
		{
			name: "parallel with steps",
			task: TaskConfig{
				Steps:    []TaskStep{{Run: "echo hi"}},
				Parallel: []string{"test"},
			},
			wantErr:     true,
			errContains: "both 'parallel' and 'steps'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTask("test-task", tt.task)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_ParallelIntegration(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "full valid config with parallel",
			config: &Config{
				Version: 1,
				Tasks: map[string]TaskConfig{
					"test":  {Run: "go test ./..."},
					"lint":  {Run: "golangci-lint run"},
					"build": {Run: "go build"},
					"verify": {
						Description: "Run all checks",
						Parallel:    []string{"test", "lint", "build"},
						FailFast:    true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "parallel task with timeout",
			config: &Config{
				Version: 1,
				Tasks: map[string]TaskConfig{
					"test": {Run: "go test ./..."},
					"lint": {Run: "golangci-lint run"},
					"verify": {
						Parallel: []string{"test", "lint"},
						Timeout:  "10m",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "parallel with max_parallel",
			config: &Config{
				Version: 1,
				Tasks: map[string]TaskConfig{
					"t1": {Run: "echo 1"},
					"t2": {Run: "echo 2"},
					"t3": {Run: "echo 3"},
					"all": {
						Parallel:    []string{"t1", "t2", "t3"},
						MaxParallel: 2,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.config)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsParallelTask(t *testing.T) {
	tests := []struct {
		name   string
		task   *TaskConfig
		expect bool
	}{
		// Note: nil task would panic; the function expects a valid pointer
		{
			name:   "run task",
			task:   &TaskConfig{Run: "make test"},
			expect: false,
		},
		{
			name:   "steps task",
			task:   &TaskConfig{Steps: []TaskStep{{Run: "echo hi"}}},
			expect: false,
		},
		{
			name:   "parallel task",
			task:   &TaskConfig{Parallel: []string{"test", "lint"}},
			expect: true,
		},
		{
			name:   "empty parallel",
			task:   &TaskConfig{Parallel: []string{}},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsParallelTask(tt.task)
			assert.Equal(t, tt.expect, result)
		})
	}
}
