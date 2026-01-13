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

	p.TaskStarted("build", "m1-linux")

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

	p.TaskStarted("test", "dev")
	p.TaskCompleted("test", true)

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

	p.TaskStarted("lint", "server")
	p.TaskCompleted("lint", false)

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
	p.TaskStarted("test-backend", "m1-linux")
	p.TaskStarted("test-frontend", "m1-mini")
	p.TaskStarted("test-opendata", "m4-mini")

	// Give animation a tick
	time.Sleep(100 * time.Millisecond)

	// Complete tasks in different order
	p.TaskCompleted("test-frontend", true)
	p.TaskCompleted("test-opendata", true)
	p.TaskCompleted("test-backend", true)

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

	p.TaskStarted("task1", "host1")
	assert.True(t, p.HasRunningTasks())

	p.TaskCompleted("task1", true)
	assert.False(t, p.HasRunningTasks())
}

func TestParallelProgress_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewParallelProgress(false) // Non-TTY mode
	p.SetWriter(&buf)
	p.Start()

	p.TaskStarted("test", "dev")

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

	p.TaskStarted("build", "host")

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
