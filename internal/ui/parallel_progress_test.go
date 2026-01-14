package ui

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParallelProgress_TaskStarted(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(true)
	p.SetWriter(&buf)
	p.Start()

	p.TaskStarted("build", 0, "m1-linux")

	// Give animation a tick to render
	time.Sleep(100 * time.Millisecond)

	p.Stop()

	output := buf.String()
	assert.Contains(t, output, "build")
	assert.Contains(t, output, "[m1-linux]")
}

func TestParallelProgress_TaskCompleted(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(true)
	p.SetWriter(&buf)
	p.Start()

	p.TaskStarted("test", 0, "dev")
	p.TaskCompleted("test", 0, true)

	// Give animation a tick to render
	time.Sleep(100 * time.Millisecond)

	p.Stop()

	output := buf.String()
	assert.Contains(t, output, "test")
	assert.Contains(t, output, SymbolSuccess) // Should show success symbol
}

func TestParallelProgress_TaskFailed(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(true)
	p.SetWriter(&buf)
	p.Start()

	p.TaskStarted("lint", 0, "server")
	p.TaskCompleted("lint", 0, false)

	// Give animation a tick
	time.Sleep(100 * time.Millisecond)

	p.Stop()

	output := buf.String()
	assert.Contains(t, output, "lint")
	assert.Contains(t, output, SymbolFail) // Should show fail symbol
}

func TestParallelProgress_MultipleTasks(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(true)
	p.SetWriter(&buf)
	p.Start()

	// Start multiple tasks
	p.TaskStarted("test-backend", 0, "m1-linux")
	p.TaskStarted("test-frontend", 1, "m1-mini")
	p.TaskStarted("test-opendata", 2, "m4-mini")

	// Give animation a tick
	time.Sleep(100 * time.Millisecond)

	// Complete tasks in different order
	p.TaskCompleted("test-frontend", 1, true)
	p.TaskCompleted("test-opendata", 2, true)
	p.TaskCompleted("test-backend", 0, true)

	time.Sleep(100 * time.Millisecond)

	p.Stop()

	output := buf.String()

	// All tasks should be in output
	assert.Contains(t, output, "test-backend")
	assert.Contains(t, output, "test-frontend")
	assert.Contains(t, output, "test-opendata")

	// All hosts should be in output
	assert.Contains(t, output, "[m1-linux]")
	assert.Contains(t, output, "[m1-mini]")
	assert.Contains(t, output, "[m4-mini]")
}

func TestParallelProgress_HasRunningTasks(t *testing.T) {
	p := NewParallelProgress(false) // non-TTY mode for simpler test

	assert.False(t, p.HasRunningTasks())

	p.TaskStarted("task1", 0, "host1")
	assert.True(t, p.HasRunningTasks())

	p.TaskCompleted("task1", 0, true)
	assert.False(t, p.HasRunningTasks())
}

func TestParallelProgress_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(false) // Non-TTY mode
	p.SetWriter(&buf)
	p.Start()

	p.TaskStarted("test", 0, "dev")

	time.Sleep(100 * time.Millisecond)

	p.Stop()

	// In non-TTY mode, should not render (no terminal control)
	output := buf.String()
	assert.Empty(t, output)
}

func TestParallelProgress_AnimationFrames(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(true)
	p.SetWriter(&buf)
	p.Start()

	p.TaskStarted("build", 0, "host")

	// Let multiple animation frames render
	time.Sleep(300 * time.Millisecond)

	p.Stop()

	output := buf.String()

	// Animation should produce ANSI escape sequences for cursor movement
	// This indicates in-place updates are happening (the animation feature)
	assert.Contains(t, output, "\x1b[", "output should contain ANSI escape sequences from animation")

	// Should also contain task info
	assert.Contains(t, output, "build")
	assert.Contains(t, output, "[host]")
}

// TestParallelProgress_DuplicateTaskNames verifies that tasks with the same name
// but different indices are tracked and updated separately.
func TestParallelProgress_DuplicateTaskNames(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(true)
	p.SetWriter(&buf)
	p.Start()

	// Initialize three tasks with the same name
	p.InitTasks([]TaskInit{
		{Name: "test-opendata", Index: 0},
		{Name: "test-opendata", Index: 1},
		{Name: "test-opendata", Index: 2},
	})

	// Give animation a tick
	time.Sleep(100 * time.Millisecond)

	// Start syncing each (they go to different hosts)
	p.TaskSyncing("test-opendata", 0, "host1")
	p.TaskSyncing("test-opendata", 1, "host2")
	p.TaskSyncing("test-opendata", 2, "host3")

	time.Sleep(100 * time.Millisecond)

	// Transition to executing
	p.TaskExecuting("test-opendata", 0)
	p.TaskExecuting("test-opendata", 1)
	p.TaskExecuting("test-opendata", 2)

	time.Sleep(100 * time.Millisecond)

	// Complete in different order with different results
	p.TaskCompleted("test-opendata", 1, true)  // index 1 passes
	p.TaskCompleted("test-opendata", 2, false) // index 2 fails
	p.TaskCompleted("test-opendata", 0, true)  // index 0 passes

	time.Sleep(100 * time.Millisecond)

	p.Stop()

	output := buf.String()

	// All three hosts should appear in output
	assert.Contains(t, output, "[host1]")
	assert.Contains(t, output, "[host2]")
	assert.Contains(t, output, "[host3]")

	// Should have both success and failure symbols
	assert.Contains(t, output, SymbolSuccess)
	assert.Contains(t, output, SymbolFail)
}
