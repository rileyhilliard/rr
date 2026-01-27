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
			name: "nested parallel task is allowed",
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
			wantErr: false,
		},
		{
			name: "deeply nested parallel tasks allowed",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"test1": {Run: "test1"},
					"test2": {Run: "test2"},
					"test3": {Run: "test3"},
					"level1": {
						Parallel: []string{"test1", "test2"},
					},
					"level2": {
						Parallel: []string{"level1", "test3"},
					},
					"level3": {
						Parallel: []string{"level2"},
					},
				},
			},
			wantErr: false,
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
			wantErr:     true,
			errContains: "can't reference itself",
		},
		{
			name: "circular reference in parallel tasks",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"test": {Run: "go test ./..."},
					"a": {
						Parallel: []string{"b", "test"},
					},
					"b": {
						Parallel: []string{"a"},
					},
				},
			},
			wantErr:     true,
			errContains: "circular reference",
		},
		{
			name: "indirect circular reference",
			config: &Config{
				Tasks: map[string]TaskConfig{
					"test": {Run: "go test ./..."},
					"a": {
						Parallel: []string{"b"},
					},
					"b": {
						Parallel: []string{"c"},
					},
					"c": {
						Parallel: []string{"a", "test"},
					},
				},
			},
			wantErr:     true,
			errContains: "circular reference",
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

func TestFlattenParallelTasks(t *testing.T) {
	tests := []struct {
		name        string
		taskName    string
		tasks       map[string]TaskConfig
		want        []string
		wantErr     bool
		errContains string
	}{
		{
			name:     "simple parallel task - no nesting",
			taskName: "all",
			tasks: map[string]TaskConfig{
				"test": {Run: "go test ./..."},
				"lint": {Run: "golangci-lint run"},
				"all":  {Parallel: []string{"test", "lint"}},
			},
			want: []string{"test", "lint"},
		},
		{
			name:     "nested parallel task - single level",
			taskName: "outer",
			tasks: map[string]TaskConfig{
				"test":  {Run: "go test ./..."},
				"lint":  {Run: "golangci-lint run"},
				"inner": {Parallel: []string{"test", "lint"}},
				"outer": {Parallel: []string{"inner"}},
			},
			want: []string{"test", "lint"},
		},
		{
			name:     "nested parallel task - mixed references",
			taskName: "all",
			tasks: map[string]TaskConfig{
				"test-1":       {Run: "test1"},
				"test-2":       {Run: "test2"},
				"test-3":       {Run: "test3"},
				"test-backend": {Run: "backend"},
				"test-group":   {Parallel: []string{"test-1", "test-2", "test-3"}},
				"all":          {Parallel: []string{"test-group", "test-backend"}},
			},
			want: []string{"test-1", "test-2", "test-3", "test-backend"},
		},
		{
			name:     "deeply nested parallel tasks",
			taskName: "level3",
			tasks: map[string]TaskConfig{
				"a":      {Run: "a"},
				"b":      {Run: "b"},
				"c":      {Run: "c"},
				"d":      {Run: "d"},
				"level1": {Parallel: []string{"a", "b"}},
				"level2": {Parallel: []string{"level1", "c"}},
				"level3": {Parallel: []string{"level2", "d"}},
			},
			want: []string{"a", "b", "c", "d"},
		},
		{
			name:     "parallel task with multiple nested parallel refs",
			taskName: "test",
			tasks: map[string]TaskConfig{
				"opendata-1":    {Run: "test opendata 1"},
				"opendata-2":    {Run: "test opendata 2"},
				"opendata-3":    {Run: "test opendata 3"},
				"backend-1":     {Run: "test backend 1"},
				"backend-2":     {Run: "test backend 2"},
				"backend-3":     {Run: "test backend 3"},
				"frontend":      {Run: "test frontend"},
				"test-opendata": {Parallel: []string{"opendata-1", "opendata-2", "opendata-3"}},
				"test-backend":  {Parallel: []string{"backend-1", "backend-2", "backend-3"}},
				"test":          {Parallel: []string{"test-opendata", "test-backend", "frontend"}},
			},
			want: []string{
				"opendata-1", "opendata-2", "opendata-3",
				"backend-1", "backend-2", "backend-3",
				"frontend",
			},
		},
		{
			name:     "non-parallel task returns error",
			taskName: "simple",
			tasks: map[string]TaskConfig{
				"simple": {Run: "echo hello"},
			},
			wantErr:     true,
			errContains: "not a parallel task",
		},
		{
			name:     "missing task returns error",
			taskName: "missing",
			tasks: map[string]TaskConfig{
				"test": {Run: "go test ./..."},
			},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:     "reference to missing task returns error",
			taskName: "all",
			tasks: map[string]TaskConfig{
				"test": {Run: "go test ./..."},
				"all":  {Parallel: []string{"test", "missing"}},
			},
			wantErr:     true,
			errContains: "'missing' not found",
		},
		{
			name:     "diamond dependency expands all occurrences",
			taskName: "test",
			tasks: map[string]TaskConfig{
				"shared":   {Run: "echo shared"},
				"unique-a": {Run: "echo a"},
				"unique-b": {Run: "echo b"},
				"group-a":  {Parallel: []string{"shared", "unique-a"}},
				"group-b":  {Parallel: []string{"shared", "unique-b"}},
				"test":     {Parallel: []string{"group-a", "group-b"}},
			},
			// shared appears in both group-a and group-b, and appears twice in result
			want: []string{"shared", "unique-a", "shared", "unique-b"},
		},
		{
			name:     "direct duplicates preserved for flake detection",
			taskName: "flake",
			tasks: map[string]TaskConfig{
				"my-test": {Run: "go test ./..."},
				"flake":   {Parallel: []string{"my-test", "my-test", "my-test"}},
			},
			// same task listed 3x runs 3x (flake detection use case)
			want: []string{"my-test", "my-test", "my-test"},
		},
		{
			name:     "nested parallel with duplicates expands fully",
			taskName: "outer",
			tasks: map[string]TaskConfig{
				"a":     {Run: "a"},
				"b":     {Run: "b"},
				"inner": {Parallel: []string{"a", "b"}},
				"outer": {Parallel: []string{"inner", "inner"}},
			},
			// inner listed twice expands to [a, b, a, b]
			want: []string{"a", "b", "a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FlattenParallelTasks(tt.taskName, tt.tasks)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
