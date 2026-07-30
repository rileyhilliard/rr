package monitor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildMetricsCommand_Linux(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformLinux, "/tmp/rr.lock")

	// Should contain Linux-specific commands
	assert.Contains(t, cmd, "/proc/stat")
	assert.Contains(t, cmd, "/proc/loadavg")
	assert.Contains(t, cmd, "/proc/meminfo")
	assert.Contains(t, cmd, "/proc/net/dev")
	assert.Contains(t, cmd, "nvidia-smi")
	assert.Contains(t, cmd, "ps aux")
	assert.Contains(t, cmd, "df -P -k /")
	assert.Contains(t, cmd, "/proc/diskstats")
	assert.Contains(t, cmd, "/sys/class/hwmon")
	assert.Contains(t, cmd, "/proc/uptime")
	assert.Contains(t, cmd, "uname -r")

	// Should use the output separator
	assert.Contains(t, cmd, OutputSeparator)
}

func TestBuildMetricsCommand_Darwin(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformDarwin, "/tmp/rr.lock")

	// Should contain macOS-specific commands
	assert.Contains(t, cmd, "top -l 1")
	assert.Contains(t, cmd, "vm_stat")
	assert.Contains(t, cmd, "sysctl hw.memsize")
	assert.Contains(t, cmd, "netstat -ib")
	assert.Contains(t, cmd, "ps aux")
	assert.Contains(t, cmd, "df -P -k /")
	assert.Contains(t, cmd, "sysctl -n hw.ncpu")
	assert.Contains(t, cmd, "sysctl -n kern.boottime")
	assert.Contains(t, cmd, "uname -r")

	// Should use the output separator
	assert.Contains(t, cmd, OutputSeparator)
}

func TestBuildMetricsCommand_Unknown(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformUnknown, "/tmp/rr.lock")

	// Should default to Linux command
	assert.Contains(t, cmd, "/proc/stat")
}

func TestBuildMetricsCommand_LockSection(t *testing.T) {
	tests := []struct {
		name     string
		platform Platform
		lockDir  string
	}{
		{name: "linux default lock dir", platform: PlatformLinux, lockDir: "/tmp/rr.lock"},
		{name: "linux configured lock dir", platform: PlatformLinux, lockDir: "/var/lock/rr.lock"},
		{name: "darwin default lock dir", platform: PlatformDarwin, lockDir: "/tmp/rr.lock"},
		{name: "darwin configured lock dir", platform: PlatformDarwin, lockDir: "/var/lock/rr.lock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := BuildMetricsCommand(tt.platform, tt.lockDir)

			// The lock read must be the last section
			lockSection := `cat "` + tt.lockDir + `/info.json" 2>/dev/null || true`
			assert.True(t, strings.HasSuffix(cmd, lockSection),
				"lock section should be the final command, got: %s", cmd)

			// The lock read must be guarded so a missing lock (cat exits nonzero)
			// cannot abort the whole batched command
			assert.Contains(t, cmd, lockSection)
		})
	}
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
	cmd := BuildMetricsCommand(PlatformLinux, "/tmp/rr.lock")

	// Count the number of sections by counting separators
	// Linux command should have 10 separators (11 sections, lock info last)
	separatorCount := strings.Count(cmd, `echo "---"`)
	assert.Equal(t, 10, separatorCount, "Linux command should have 10 separators for 11 sections")
	assert.Equal(t, linuxLockSection, separatorCount, "lock section index should match separator count")
}

func TestBuildDarwinCommand_SectionCount(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformDarwin, "/tmp/rr.lock")

	// Darwin command should have 8 separators (9 sections: top, vm_stat, netstat,
	// ioreg GPU, ps, df, hw.ncpu, boottime+kernel, lock info)
	separatorCount := strings.Count(cmd, `echo "---"`)
	assert.Equal(t, 8, separatorCount, "Darwin command should have 8 separators for 9 sections")
	assert.Equal(t, darwinLockSection, separatorCount, "lock section index should match separator count")
}

func TestBuildMetricsCommand_GracefulGPUFailure(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformLinux, "/tmp/rr.lock")

	// nvidia-smi should fail gracefully with "|| true"
	assert.Contains(t, cmd, "nvidia-smi")
	assert.Contains(t, cmd, "2>/dev/null || true")
}

func TestBuildMetricsCommand_ProcessLimit(t *testing.T) {
	cmd := BuildMetricsCommand(PlatformLinux, "/tmp/rr.lock")

	// Should limit process output to top 16
	assert.Contains(t, cmd, "head -16")
}
