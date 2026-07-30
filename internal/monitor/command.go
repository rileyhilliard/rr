package monitor

import "fmt"

// Platform represents the operating system type of a remote host.
type Platform string

const (
	// PlatformLinux indicates a Linux host.
	PlatformLinux Platform = "linux"
	// PlatformDarwin indicates a macOS host.
	PlatformDarwin Platform = "darwin"
	// PlatformUnknown indicates an unknown platform.
	PlatformUnknown Platform = "unknown"
)

// Separator used to split batched command output.
const OutputSeparator = "---"

// Section indices for the lock payload appended by BuildMetricsCommand.
// The lock section is always last on both platforms.
const (
	linuxLockSection  = 6
	darwinLockSection = 5
)

// BuildMetricsCommand returns a single batched command that collects all metrics
// for the specified platform. This allows collecting all metrics in a single SSH exec.
// lockDir is the rr lock directory on the remote; its info.json is read as the
// final output section (empty when no lock is held).
func BuildMetricsCommand(platform Platform, lockDir string) string {
	switch platform {
	case PlatformLinux:
		return buildLinuxCommand(lockDir)
	case PlatformDarwin:
		return buildDarwinCommand(lockDir)
	default:
		// Default to Linux command, it will fail gracefully
		return buildLinuxCommand(lockDir)
	}
}

// buildLockSection returns the trailing lock-check fragment of the batched command.
// The "|| true" guard is required: with no lock held, cat fails, and a nonzero
// exit would abort the whole batched command.
func buildLockSection(lockDir string) string {
	return fmt.Sprintf(`cat %q 2>/dev/null || true`, lockDir+"/info.json")
}

// buildLinuxCommand returns the batched metrics command for Linux hosts.
// Output sections are separated by "---" and include:
// 0. /proc/stat - CPU statistics
// 1. /proc/loadavg - Load averages
// 2. /proc/meminfo - Memory information
// 3. /proc/net/dev - Network interface statistics
// 4. nvidia-smi output - GPU metrics (optional, fails silently if not available)
// 5. ps aux - Process list sorted by CPU (top 16 including header)
// 6. Lock info.json - rr lock status (empty if unlocked)
func buildLinuxCommand(lockDir string) string {
	return `cat /proc/stat; echo "---"; cat /proc/loadavg; echo "---"; cat /proc/meminfo; echo "---"; cat /proc/net/dev; echo "---"; nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null || true; echo "---"; ps aux --sort=-%cpu 2>/dev/null | head -16 || ps aux 2>/dev/null | head -16; echo "---"; ` + buildLockSection(lockDir)
}

// buildDarwinCommand returns the batched metrics command for macOS hosts.
// Output sections are separated by "---" and include:
// 0. top output - CPU usage and load averages
// 1. vm_stat + sysctl hw.memsize - Memory statistics with total memory
// 2. netstat output - Network interface statistics
// 3. ioreg GPU output - Apple Silicon GPU metrics (optional, fails silently)
// 4. ps aux - Process list sorted by CPU (top 16 including header)
// 5. Lock info.json - rr lock status (empty if unlocked)
func buildDarwinCommand(lockDir string) string {
	return `top -l 1 -n 0 2>/dev/null; echo "---"; vm_stat; sysctl hw.memsize 2>/dev/null; echo "---"; netstat -ib; echo "---"; ioreg -r -c AGXAccelerator 2>/dev/null | grep -E '"(model|gpu-core-count|PerformanceStatistics)"' || true; echo "---"; ps aux -r 2>/dev/null | head -16; echo "---"; ` + buildLockSection(lockDir)
}

// PlatformDetectCommand returns the command to detect the platform type.
func PlatformDetectCommand() string {
	return "uname -s"
}

// ParsePlatform converts uname output to a Platform value.
func ParsePlatform(unameOutput string) Platform {
	switch unameOutput {
	case "Linux":
		return PlatformLinux
	case "Darwin":
		return PlatformDarwin
	default:
		return PlatformUnknown
	}
}
