package logger

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvLogger_Debug(t *testing.T) {
	tests := []struct {
		name      string
		envValue  string
		expectLog bool
	}{
		{
			name:      "logs when RR_DEBUG is set",
			envValue:  "1",
			expectLog: true,
		},
		{
			name:      "logs when RR_DEBUG is any value",
			envValue:  "true",
			expectLog: true,
		},
		{
			name:      "does not log when RR_DEBUG is empty",
			envValue:  "",
			expectLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture log output
			var buf bytes.Buffer
			log.SetOutput(&buf)
			defer log.SetOutput(os.Stderr)

			// Set environment
			if tt.envValue != "" {
				t.Setenv("RR_DEBUG", tt.envValue)
			} else {
				os.Unsetenv("RR_DEBUG")
			}

			l := NewEnvLogger("[test]")
			l.Debug("test message %s", "arg")

			if tt.expectLog {
				assert.Contains(t, buf.String(), "[test] test message arg")
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestEnvLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	l := NewEnvLogger("[info-test]")
	l.Info("info message %d", 42)

	assert.Contains(t, buf.String(), "[info-test] info message 42")
}

func TestEnvLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	l := NewEnvLogger("[warn-test]")
	l.Warn("warning message")

	assert.Contains(t, buf.String(), "[warn-test] WARN: warning message")
}

func TestEnvLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	l := NewEnvLogger("[error-test]")
	l.Error("error message")

	assert.Contains(t, buf.String(), "[error-test] ERROR: error message")
}

func TestNoopLogger(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	l := Noop()
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")

	assert.Empty(t, buf.String(), "noop logger should not produce any output")
}

func TestBufferLogger(t *testing.T) {
	l := NewBufferLogger()

	l.Debug("debug %s", "msg")
	l.Info("info %s", "msg")
	l.Warn("warn %s", "msg")
	l.Error("error %s", "msg")

	require.Len(t, l.Messages, 4)

	assert.Equal(t, "debug", l.Messages[0].Level)
	assert.Equal(t, "debug msg", l.Messages[0].Message)

	assert.Equal(t, "info", l.Messages[1].Level)
	assert.Equal(t, "info msg", l.Messages[1].Message)

	assert.Equal(t, "warn", l.Messages[2].Level)
	assert.Equal(t, "warn msg", l.Messages[2].Message)

	assert.Equal(t, "error", l.Messages[3].Level)
	assert.Equal(t, "error msg", l.Messages[3].Message)
}

func TestBufferLogger_HasLevel(t *testing.T) {
	l := NewBufferLogger()

	assert.False(t, l.HasLevel("debug"))
	assert.False(t, l.HasLevel("error"))

	l.Debug("test")
	assert.True(t, l.HasLevel("debug"))
	assert.False(t, l.HasLevel("error"))

	l.Error("test")
	assert.True(t, l.HasLevel("error"))
}

func TestBufferLogger_Clear(t *testing.T) {
	l := NewBufferLogger()

	l.Debug("test1")
	l.Info("test2")
	require.Len(t, l.Messages, 2)

	l.Clear()
	assert.Empty(t, l.Messages)
}

func TestDefault(t *testing.T) {
	original := defaultLogger
	defer func() { defaultLogger = original }()

	// Default should return a logger
	d := Default()
	assert.NotNil(t, d)

	// SetDefault should change the default
	buf := NewBufferLogger()
	SetDefault(buf)

	assert.Equal(t, buf, Default())
}

func TestLoggerInterface(t *testing.T) {
	// Verify all implementations satisfy the interface
	_ = NewEnvLogger("")
	_ = Noop()
	_ = NewBufferLogger()
}

func TestEnvLogger_FormatStrings(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	l := NewEnvLogger("[fmt]")

	// Test various format specifiers
	l.Info("int: %d, string: %s, float: %.2f", 42, "hello", 3.14159)

	output := buf.String()
	assert.True(t, strings.Contains(output, "int: 42"))
	assert.True(t, strings.Contains(output, "string: hello"))
	assert.True(t, strings.Contains(output, "float: 3.14"))
}
