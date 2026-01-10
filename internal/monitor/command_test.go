package monitor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildMetricsCommand_Linux(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformLinux)

	// Should contain Linux-specific commands
	assert.Contains(t, cmd, "/proc/stat")
	assert.Contains(t, cmd, "/proc/loadavg")
	assert.Contains(t, cmd, "/proc/meminfo")
	assert.Contains(t, cmd, "/proc/net/dev")
	assert.Contains(t, cmd, "nvidia-smi")
	assert.Contains(t, cmd, "ps aux")

	// Should use the output separator
	assert.Contains(t, cmd, OutputSeparator)
}

func TestBuildMetricsCommand_Darwin(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformDarwin)

	// Should contain macOS-specific commands
	assert.Contains(t, cmd, "top -l 1")
	assert.Contains(t, cmd, "vm_stat")
	assert.Contains(t, cmd, "sysctl hw.memsize")
	assert.Contains(t, cmd, "netstat -ib")
	assert.Contains(t, cmd, "ps aux")

	// Should use the output separator
	assert.Contains(t, cmd, OutputSeparator)
}

func TestBuildMetricsCommand_Unknown(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformUnknown)

	// Should default to Linux command
	assert.Contains(t, cmd, "/proc/stat")
}

func TestPlatformDetectCommand(t *testing.T) {
	cmd := PlatformDetectCommand()

	// Should use uname -s for platform detection
	assert.Equal(t, "uname -s", cmd)
}

func TestParsePlatform(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect Platform
	}{
		{
			name:   "Linux",
			input:  "Linux",
			expect: PlatformLinux,
		},
		{
			name:   "Darwin",
			input:  "Darwin",
			expect: PlatformDarwin,
		},
		{
			name:   "FreeBSD",
			input:  "FreeBSD",
			expect: PlatformUnknown,
		},
		{
			name:   "Windows",
			input:  "MINGW64_NT-10.0",
			expect: PlatformUnknown,
		},
		{
			name:   "empty",
			input:  "",
			expect: PlatformUnknown,
		},
		{
			name:   "lowercase linux",
			input:  "linux",
			expect: PlatformUnknown, // Case-sensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePlatform(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestPlatform_Constants(t *testing.T) {
	// Verify platform constants are defined correctly
	assert.Equal(t, Platform("linux"), PlatformLinux)
	assert.Equal(t, Platform("darwin"), PlatformDarwin)
	assert.Equal(t, Platform("unknown"), PlatformUnknown)
}

func TestOutputSeparator(t *testing.T) {
	// Verify the separator is what we expect
	assert.Equal(t, "---", OutputSeparator)
}

func TestBuildLinuxCommand_SectionCount(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformLinux)

	// Count the number of sections by counting separators
	// Linux command should have 5 separators (6 sections)
	separatorCount := strings.Count(cmd, `echo "---"`)
	assert.Equal(t, 5, separatorCount, "Linux command should have 5 separators for 6 sections")
}

func TestBuildDarwinCommand_SectionCount(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformDarwin)

	// Darwin command should have 3 separators (4 sections)
	separatorCount := strings.Count(cmd, `echo "---"`)
	assert.Equal(t, 3, separatorCount, "Darwin command should have 3 separators for 4 sections")
}

func TestBuildMetricsCommand_GracefulGPUFailure(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformLinux)

	// nvidia-smi should fail gracefully with "|| true"
	assert.Contains(t, cmd, "nvidia-smi")
	assert.Contains(t, cmd, "2>/dev/null || true")
}

func TestBuildMetricsCommand_ProcessLimit(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformLinux)

	// Should limit process output to top 16
	assert.Contains(t, cmd, "head -16")
}
