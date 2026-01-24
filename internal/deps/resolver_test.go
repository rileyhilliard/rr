package deps

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolver_Resolve(t *testing.T) {
	tests := []struct {
		name           string
		tasks          map[string]config.TaskConfig
		targetTask     string
		opts           ResolveOptions
		wantErr        bool
		errContains    string
		wantTaskCount  int
		wantStageCount int
		validate       func(t *testing.T, plan *ExecutionPlan)
	}{
		{
			name: "simple linear chain",
			tasks: map[string]config.TaskConfig{
				"lint": {Run: "golangci-lint run"},
				"test": {Run: "go test ./..."},
				"ci": {
					Depends: []config.DependencyItem{
						{Task: "lint"},
						{Task: "test"},
					},
				},
			},
			targetTask:     "ci",
			wantTaskCount:  2, // lint, test (ci has no run command)
			wantStageCount: 2,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				assert.Equal(t, "lint", plan.Stages[0].Tasks[0])
				assert.Equal(t, "test", plan.Stages[1].Tasks[0])
				assert.False(t, plan.Stages[0].Parallel)
				assert.False(t, plan.Stages[1].Parallel)
			},
		},
		{
			name: "parallel group dependency",
			tasks: map[string]config.TaskConfig{
				"lint":      {Run: "golangci-lint run"},
				"typecheck": {Run: "mypy ."},
				"test":      {Run: "pytest"},
				"ci": {
					Depends: []config.DependencyItem{
						{Parallel: []string{"lint", "typecheck"}},
						{Task: "test"},
					},
				},
			},
			targetTask:     "ci",
			wantTaskCount:  3,
			wantStageCount: 2,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				// First stage is parallel group
				assert.True(t, plan.Stages[0].Parallel)
				assert.ElementsMatch(t, []string{"lint", "typecheck"}, plan.Stages[0].Tasks)
				// Second stage is test
				assert.False(t, plan.Stages[1].Parallel)
				assert.Equal(t, []string{"test"}, plan.Stages[1].Tasks)
			},
		},
		{
			name: "dependency with own command",
			tasks: map[string]config.TaskConfig{
				"lint": {Run: "golangci-lint run"},
				"deploy": {
					Depends: []config.DependencyItem{{Task: "lint"}},
					Run:     "./deploy.sh",
				},
			},
			targetTask:     "deploy",
			wantTaskCount:  2, // lint, deploy
			wantStageCount: 2,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				assert.Equal(t, "lint", plan.Stages[0].Tasks[0])
				assert.Equal(t, "deploy", plan.Stages[1].Tasks[0])
			},
		},
		{
			name: "diamond dependency (deduplication)",
			tasks: map[string]config.TaskConfig{
				"base":  {Run: "echo base"},
				"left":  {Depends: []config.DependencyItem{{Task: "base"}}, Run: "echo left"},
				"right": {Depends: []config.DependencyItem{{Task: "base"}}, Run: "echo right"},
				"top": {
					Depends: []config.DependencyItem{
						{Task: "left"},
						{Task: "right"},
					},
				},
			},
			targetTask:     "top",
			wantTaskCount:  3, // base, left, right (top has no run, base deduplicated)
			wantStageCount: 3,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				// base should only appear once due to deduplication
				baseCount := 0
				for _, stage := range plan.Stages {
					for _, task := range stage.Tasks {
						if task == "base" {
							baseCount++
						}
					}
				}
				assert.Equal(t, 1, baseCount, "base should appear exactly once")
			},
		},
		{
			name: "skip deps",
			tasks: map[string]config.TaskConfig{
				"lint": {Run: "golangci-lint run"},
				"deploy": {
					Depends: []config.DependencyItem{{Task: "lint"}},
					Run:     "./deploy.sh",
				},
			},
			targetTask:     "deploy",
			opts:           ResolveOptions{SkipDeps: true},
			wantTaskCount:  1, // only deploy
			wantStageCount: 1,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				assert.Equal(t, "deploy", plan.Stages[0].Tasks[0])
			},
		},
		{
			name: "skip deps on orchestrator (no run command)",
			tasks: map[string]config.TaskConfig{
				"lint": {Run: "golangci-lint run"},
				"test": {Run: "go test ./..."},
				"ci": {
					Depends: []config.DependencyItem{
						{Task: "lint"},
						{Task: "test"},
					},
				},
			},
			targetTask:     "ci",
			opts:           ResolveOptions{SkipDeps: true},
			wantTaskCount:  0, // ci has no run command
			wantStageCount: 0,
		},
		{
			name: "from task",
			tasks: map[string]config.TaskConfig{
				"lint": {Run: "golangci-lint run"},
				"test": {Run: "go test ./..."},
				"build": {
					Depends: []config.DependencyItem{
						{Task: "lint"},
						{Task: "test"},
					},
					Run: "go build",
				},
			},
			targetTask:     "build",
			opts:           ResolveOptions{From: "test"},
			wantTaskCount:  2, // test, build
			wantStageCount: 2,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				assert.Equal(t, "test", plan.Stages[0].Tasks[0])
				assert.Equal(t, "build", plan.Stages[1].Tasks[0])
			},
		},
		{
			name: "from task not in chain",
			tasks: map[string]config.TaskConfig{
				"lint": {Run: "golangci-lint run"},
				"ci": {
					Depends: []config.DependencyItem{{Task: "lint"}},
				},
			},
			targetTask:  "ci",
			opts:        ResolveOptions{From: "missing"},
			wantErr:     true,
			errContains: "not found in dependency chain",
		},
		{
			name: "task not found",
			tasks: map[string]config.TaskConfig{
				"lint": {Run: "golangci-lint run"},
			},
			targetTask:  "missing",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "nested dependencies",
			tasks: map[string]config.TaskConfig{
				"setup": {Run: "make setup"},
				"lint": {
					Depends: []config.DependencyItem{{Task: "setup"}},
					Run:     "golangci-lint run",
				},
				"test": {
					Depends: []config.DependencyItem{{Task: "lint"}},
					Run:     "go test ./...",
				},
				"ci": {
					Depends: []config.DependencyItem{{Task: "test"}},
				},
			},
			targetTask:     "ci",
			wantTaskCount:  3, // setup, lint, test (ci has no run)
			wantStageCount: 3,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				assert.Equal(t, "setup", plan.Stages[0].Tasks[0])
				assert.Equal(t, "lint", plan.Stages[1].Tasks[0])
				assert.Equal(t, "test", plan.Stages[2].Tasks[0])
			},
		},
		{
			name: "task with no dependencies",
			tasks: map[string]config.TaskConfig{
				"test": {Run: "go test ./..."},
			},
			targetTask:     "test",
			wantTaskCount:  1,
			wantStageCount: 1,
			validate: func(t *testing.T, plan *ExecutionPlan) {
				assert.Equal(t, "test", plan.Stages[0].Tasks[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewResolver(tt.tasks)
			plan, err := resolver.Resolve(tt.targetTask, tt.opts)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, plan)

			assert.Equal(t, tt.targetTask, plan.TargetTask)
			assert.Equal(t, tt.wantTaskCount, plan.TaskCount(), "task count mismatch")
			assert.Equal(t, tt.wantStageCount, len(plan.Stages), "stage count mismatch")

			if tt.validate != nil {
				tt.validate(t, plan)
			}
		})
	}
}

func TestExecutionPlan_String(t *testing.T) {
	t.Run("empty plan", func(t *testing.T) {
		plan := &ExecutionPlan{}
		assert.Equal(t, "empty plan", plan.String())
	})

	t.Run("single task", func(t *testing.T) {
		plan := &ExecutionPlan{
			Stages: []Stage{{Tasks: []string{"test"}, Parallel: false}},
		}
		assert.Equal(t, "1. test", plan.String())
	})

	t.Run("parallel and sequential", func(t *testing.T) {
		plan := &ExecutionPlan{
			Stages: []Stage{
				{Tasks: []string{"lint", "typecheck"}, Parallel: true},
				{Tasks: []string{"test"}, Parallel: false},
			},
		}
		result := plan.String()
		assert.Contains(t, result, "[lint, typecheck] (parallel)")
		assert.Contains(t, result, "test")
	})
}

func TestExecutionPlan_TaskCount(t *testing.T) {
	plan := &ExecutionPlan{
		Stages: []Stage{
			{Tasks: []string{"lint", "typecheck"}, Parallel: true},
			{Tasks: []string{"test"}, Parallel: false},
			{Tasks: []string{"build", "deploy"}, Parallel: true},
		},
	}
	assert.Equal(t, 5, plan.TaskCount())
}
