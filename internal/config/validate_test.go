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

func TestValidateDependencyGraph(t *testing.T) {
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
			name: "valid linear dependency chain",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"lint":      {Run: "golangci-lint run"},
					"typecheck": {Run: "mypy ."},
					"test":      {Run: "pytest"},
					"ci": {
						Depends: []DependencyItem{
							{Task: "lint"},
							{Task: "typecheck"},
							{Task: "test"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid parallel group dependency",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"lint":      {Run: "golangci-lint run"},
					"typecheck": {Run: "mypy ."},
					"test":      {Run: "pytest"},
					"ci": {
						Depends: []DependencyItem{
							{Parallel: []string{"lint", "typecheck"}},
							{Task: "test"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "depends-only task (no run command)",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"lint": {Run: "golangci-lint run"},
					"test": {Run: "pytest"},
					"ci": {
						Depends: []DependencyItem{
							{Task: "lint"},
							{Task: "test"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "depends with run command",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"lint": {Run: "golangci-lint run"},
					"deploy": {
						Depends: []DependencyItem{{Task: "lint"}},
						Run:     "./deploy.sh",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "self-reference dependency",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"recursive": {
						Depends: []DependencyItem{{Task: "recursive"}},
					},
				},
			},
			wantErr:     true,
			errContains: "can't depend on itself",
		},
		{
			name: "non-existent dependency reference",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"test": {Run: "pytest"},
					"ci": {
						Depends: []DependencyItem{
							{Task: "test"},
							{Task: "missing"},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "non-existent task 'missing'",
		},
		{
			name: "circular dependency - direct",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"a": {
						Depends: []DependencyItem{{Task: "b"}},
						Run:     "echo a",
					},
					"b": {
						Depends: []DependencyItem{{Task: "a"}},
						Run:     "echo b",
					},
				},
			},
			wantErr:     true,
			errContains: "circular dependency",
		},
		{
			name: "circular dependency - transitive",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"a": {
						Depends: []DependencyItem{{Task: "b"}},
						Run:     "echo a",
					},
					"b": {
						Depends: []DependencyItem{{Task: "c"}},
						Run:     "echo b",
					},
					"c": {
						Depends: []DependencyItem{{Task: "a"}},
						Run:     "echo c",
					},
				},
			},
			wantErr:     true,
			errContains: "circular dependency",
		},
		{
			name: "parallel group with non-existent task",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"lint": {Run: "golangci-lint run"},
					"ci": {
						Depends: []DependencyItem{
							{Parallel: []string{"lint", "missing"}},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "non-existent task 'missing'",
		},
		{
			name: "diamond dependency (deduplication case)",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"base":  {Run: "echo base"},
					"left":  {Depends: []DependencyItem{{Task: "base"}}, Run: "echo left"},
					"right": {Depends: []DependencyItem{{Task: "base"}}, Run: "echo right"},
					"top": {
						Depends: []DependencyItem{
							{Task: "left"},
							{Task: "right"},
						},
					},
				},
			},
			wantErr: false, // Not a cycle, just shared dependency
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDependencyGraph(tt.config)
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

func TestValidateTask_Depends(t *testing.T) {
	tests := []struct {
		name        string
		task        TaskConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "depends only (no run)",
			task: TaskConfig{
				Depends: []DependencyItem{{Task: "lint"}},
			},
			wantErr: false,
		},
		{
			name: "depends with run",
			task: TaskConfig{
				Depends: []DependencyItem{{Task: "lint"}},
				Run:     "./deploy.sh",
			},
			wantErr: false,
		},
		{
			name: "depends with steps",
			task: TaskConfig{
				Depends: []DependencyItem{{Task: "lint"}},
				Steps: []TaskStep{
					{Name: "step1", Run: "echo step1"},
				},
			},
			wantErr: false,
		},
		{
			name: "depends with both run and steps",
			task: TaskConfig{
				Depends: []DependencyItem{{Task: "lint"}},
				Run:     "echo run",
				Steps:   []TaskStep{{Name: "step1", Run: "echo step1"}},
			},
			wantErr:     true,
			errContains: "both 'run' and 'steps'",
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

func TestValidate_DependencyIntegration(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "full valid config with dependencies",
			config: &Config{
				Version: 1,
				Tasks: map[string]TaskConfig{
					"lint":      {Run: "golangci-lint run"},
					"typecheck": {Run: "mypy ."},
					"test":      {Run: "pytest"},
					"ci": {
						Description: "Run full CI pipeline",
						Depends: []DependencyItem{
							{Parallel: []string{"lint", "typecheck"}},
							{Task: "test"},
						},
					},
					"deploy": {
						Description: "Deploy after CI",
						Depends:     []DependencyItem{{Task: "ci"}},
						Run:         "./deploy.sh",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "dependency with fail_fast",
			config: &Config{
				Version: 1,
				Tasks: map[string]TaskConfig{
					"lint": {Run: "golangci-lint run"},
					"test": {Run: "pytest"},
					"ci": {
						Depends:  []DependencyItem{{Task: "lint"}, {Task: "test"}},
						FailFast: true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "parallel task can't have depends",
			config: &Config{
				Version: 1,
				Tasks: map[string]TaskConfig{
					"lint": {Run: "golangci-lint run"},
					"test": {Run: "pytest"},
					"all": {
						Parallel: []string{"lint", "test"},
						Depends:  []DependencyItem{{Task: "lint"}},
					},
				},
			},
			wantErr:     true,
			errContains: "both 'parallel' and 'depends'",
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
