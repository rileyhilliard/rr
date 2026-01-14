package output

import (
	"math"
	"testing"

	"github.com/rileyhilliard/rr/internal/util"
	"github.com/stretchr/testify/assert"
)

func TestGenericFormatterName(t *testing.T) {
	f := NewGenericFormatter()
	assert.Equal(t, "generic", f.Name())
}

func TestGenericFormatterProcessLineNormal(t *testing.T) {
	f := NewGenericFormatter()

	// Normal lines pass through unchanged
	tests := []string{
		"Hello, world!",
		"Running tests...",
		"Test passed: test_login",
		"======================== test session starts =========================",
	}

	for _, line := range tests {
		result := f.ProcessLine(line)
		assert.Equal(t, line, result)
	}
}

func TestGenericFormatterProcessLineError(t *testing.T) {
	f := NewGenericFormatter()

	// Error lines should be identified and processed
	// Note: lipgloss may not output ANSI codes without a TTY,
	// so we just verify the line is processed (contains original text)
	tests := []struct {
		name  string
		input string
	}{
		{"lowercase error:", "error: file not found"},
		{"capitalized Error:", "Error: connection failed"},
		{"uppercase ERROR:", "ERROR: critical failure"},
		{"ERROR in middle", "Something ERROR happened"},
		{"fatal:", "fatal: unrecoverable error"},
		{"Fatal:", "Fatal: crash imminent"},
		{"panic:", "panic: runtime error"},
		{"exception:", "exception: null pointer"},
		{"FAILED prefix", "FAILED tests/test_auth.py::test_login"},
		{"fail:", "fail: assertion error"},
		{"failed:", "failed: expected 1, got 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the line is detected as an error
			assert.True(t, isErrorLine(tt.input), "should be detected as error: %s", tt.input)

			// Verify ProcessLine returns something containing the original text
			result := f.ProcessLine(tt.input)
			assert.Contains(t, result, tt.input)
		})
	}
}

func TestGenericFormatterSummarySuccess(t *testing.T) {
	f := NewGenericFormatter()
	result := f.Summary(0)
	assert.Empty(t, result)
}

func TestGenericFormatterSummaryFailed(t *testing.T) {
	f := NewGenericFormatter()
	result := f.Summary(1)
	assert.Contains(t, result, "exit code 1")
}

func TestGenericFormatterSummaryNegativeCode(t *testing.T) {
	f := NewGenericFormatter()
	result := f.Summary(-1)
	assert.Contains(t, result, "exit code -1")
}

func TestPassthroughFormatterName(t *testing.T) {
	f := NewPassthroughFormatter()
	assert.Equal(t, "passthrough", f.Name())
}

func TestPassthroughFormatterProcessLine(t *testing.T) {
	f := NewPassthroughFormatter()

	tests := []string{
		"normal line",
		"ERROR: this should not be changed",
		"\033[31mRed text\033[0m",
	}

	for _, line := range tests {
		result := f.ProcessLine(line)
		assert.Equal(t, line, result)
	}
}

func TestPassthroughFormatterSummary(t *testing.T) {
	f := NewPassthroughFormatter()
	assert.Empty(t, f.Summary(0))
	assert.Empty(t, f.Summary(1))
}

func TestUtilItoa(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{-1, "-1"},
		{-123, "-123"},
		{math.MaxInt, "9223372036854775807"},
		{math.MinInt, "-9223372036854775808"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, util.Itoa(tt.input))
		})
	}
}

func TestIsErrorLine(t *testing.T) {
	errorLines := []string{
		"error: something went wrong",
		"Error: capitalized error",
		"ERROR: uppercase error",
		"  error: indented error",
		"fatal: git error",
		"Fatal: another fatal",
		"panic: runtime panic",
		"exception: java exception",
		"FAILED tests/test_auth.py::test_login",
		"  FAILED tests/test_users.py::test_create",
		"fail: test failed",
		"failed: assertion failed",
		"Something ERROR in the middle",
		"error is happening",
	}

	for _, line := range errorLines {
		t.Run(line, func(t *testing.T) {
			assert.True(t, isErrorLine(line), "should be detected as error: %s", line)
		})
	}

	nonErrorLines := []string{
		"normal line",
		"Test passed",
		"Errors: 0",           // "Errors:" is not "Error:" prefix
		"error_count = 5",     // has "error" but not as a message
		"No failures",         // has "fail" but in a word
		"Unfailed test",       // has "fail" in word
		"passed successfully", // positive message
	}

	for _, line := range nonErrorLines {
		t.Run(line, func(t *testing.T) {
			assert.False(t, isErrorLine(line), "should not be detected as error: %s", line)
		})
	}
}

func TestFormatterRegistry(t *testing.T) {
	r := NewFormatterRegistry()

	// Should have default formatters
	assert.NotNil(t, r.Get("generic"))
	assert.NotNil(t, r.Get("passthrough"))

	// Unknown formatter should return generic
	f := r.Get("unknown")
	assert.Equal(t, "generic", f.Name())
}

func TestFormatterRegistryRegister(t *testing.T) {
	r := NewFormatterRegistry()

	custom := &mockFormatter{name: "custom"}
	r.Register(custom)

	f := r.Get("custom")
	assert.Equal(t, "custom", f.Name())
}

func TestFormatterRegistryNames(t *testing.T) {
	r := NewFormatterRegistry()

	names := r.Names()
	assert.Contains(t, names, "generic")
	assert.Contains(t, names, "passthrough")
}

type mockFormatter struct {
	name string
}

func (f *mockFormatter) Name() string {
	return f.name
}

func (f *mockFormatter) ProcessLine(line string) string {
	return line
}

func (f *mockFormatter) Summary(_ int) string {
	return ""
}
