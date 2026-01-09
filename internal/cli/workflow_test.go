package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWorkflowOptions_Defaults(t *testing.T) {
	opts := WorkflowOptions{}

	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.SkipSync)
	assert.False(t, opts.SkipLock)
	assert.Empty(t, opts.WorkingDir)
	assert.False(t, opts.Quiet)
}

func TestWorkflowOptions_WithValues(t *testing.T) {
	opts := WorkflowOptions{
		Host:         "dev-server",
		Tag:          "fast",
		ProbeTimeout: 10 * time.Second,
		SkipSync:     true,
		SkipLock:     true,
		WorkingDir:   "/project",
		Quiet:        true,
	}

	assert.Equal(t, "dev-server", opts.Host)
	assert.Equal(t, "fast", opts.Tag)
	assert.Equal(t, 10*time.Second, opts.ProbeTimeout)
	assert.True(t, opts.SkipSync)
	assert.True(t, opts.SkipLock)
	assert.Equal(t, "/project", opts.WorkingDir)
	assert.True(t, opts.Quiet)
}

func TestWorkflowContext_Close_NilLock(t *testing.T) {
	ctx := &WorkflowContext{
		Lock:     nil,
		selector: nil,
	}

	// Should not panic
	ctx.Close()
}

func TestWorkflowContext_Close_NilSelector(t *testing.T) {
	ctx := &WorkflowContext{
		Lock:     nil,
		selector: nil,
	}

	// Should not panic
	ctx.Close()
}

func TestWorkflowContext_ZeroValues(t *testing.T) {
	ctx := &WorkflowContext{}

	// Zero values should be safe
	assert.Nil(t, ctx.Config)
	assert.Nil(t, ctx.Conn)
	assert.Nil(t, ctx.Lock)
	assert.Empty(t, ctx.WorkDir)
	assert.Nil(t, ctx.PhaseDisplay)
	assert.True(t, ctx.StartTime.IsZero())
}

func TestWorkflowContext_WithValues(t *testing.T) {
	now := time.Now()
	ctx := &WorkflowContext{
		WorkDir:   "/test/dir",
		StartTime: now,
	}

	assert.Equal(t, "/test/dir", ctx.WorkDir)
	assert.Equal(t, now, ctx.StartTime)
}
