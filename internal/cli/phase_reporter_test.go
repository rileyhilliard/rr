package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
)

// A failed phase event's error is the one-line message. The error envelope
// that follows carries the code and suggestion, so the event doesn't repeat
// the pretty-printed form with its symbol, cause and suggestion lines.
func TestStructuredReporter_PhaseFailedIsOneLine(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "structured error",
			err: errors.WrapWithCode(context.Canceled, errors.ErrInterrupted,
				"Stopped waiting for the lock on dev", "Nothing ran."),
			want: "Stopped waiting for the lock on dev",
		},
		{
			name: "plain error",
			err:  stderrors.New("dial tcp: connection refused"),
			want: "dial tcp: connection refused",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStderr(t, func() {
				(&StructuredReporter{}).PhaseFailed("lock", tt.err)
			})
			var event map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &event), out)
			assert.Equal(t, "failed", event["status"])
			assert.Equal(t, tt.want, event["error"])
		})
	}
}
