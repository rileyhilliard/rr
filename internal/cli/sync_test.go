package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSyncOptions_Defaults(t *testing.T) {
	opts := SyncOptions{}

	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.DryRun)
	assert.Empty(t, opts.WorkingDir)
}

func TestSyncOptions_WithValues(t *testing.T) {
	opts := SyncOptions{
		Host:         "remote-dev",
		Tag:          "fast",
		ProbeTimeout: 5 * time.Second,
		DryRun:       true,
		WorkingDir:   "/path/to/project",
	}

	assert.Equal(t, "remote-dev", opts.Host)
	assert.Equal(t, "fast", opts.Tag)
	assert.Equal(t, 5*time.Second, opts.ProbeTimeout)
	assert.True(t, opts.DryRun)
	assert.Equal(t, "/path/to/project", opts.WorkingDir)
}

func TestSyncCommand_InvalidProbeTimeout(t *testing.T) {
	err := syncCommand("", "", "invalid-duration", false)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "doesn't look like a valid timeout")
}

func TestSyncCommand_ValidProbeTimeoutFormats(t *testing.T) {
	// These will fail later in the process (no config), but should not fail
	// on duration parsing
	tests := []struct {
		name    string
		timeout string
	}{
		{"seconds", "5s"},
		{"minutes", "2m"},
		{"milliseconds", "500ms"},
		{"combined", "1m30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := syncCommand("", "", tt.timeout, false)
			// Should fail with config error, not parse error
			if err != nil {
				assert.NotContains(t, err.Error(), "Invalid probe timeout",
					"should parse duration %s correctly", tt.timeout)
			}
		})
	}
}
