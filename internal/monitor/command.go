package monitor

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

// BuildMetricsCommand returns a single batched command that collects all metrics
// for the specified platform. This allows collecting all metrics in a single SSH exec.
func BuildMetricsCommand(platform Platform) string {
	switch platform {
	case PlatformLinux:
		return buildLinuxCommand()
	case PlatformDarwin:
		return buildDarwinCommand()
	default:
		// Default to Linux command, it will fail gracefully
		return buildLinuxCommand()
	}
}

// buildLinuxCommand returns the batched metrics command for Linux hosts.
// Output sections are separated by "---" and include:
// 0. /proc/stat - CPU statistics
// 1. /proc/loadavg - Load averages
// 2. /proc/meminfo - Memory information
// 3. /proc/net/dev - Network interface statistics
// 4. nvidia-smi output - GPU metrics (optional, fails silently if not available)
// 5. ps aux - Process list sorted by CPU (top 16 including header)
func buildLinuxCommand() string {
	return `cat /proc/stat; echo "---"; cat /proc/loadavg; echo "---"; cat /proc/meminfo; echo "---"; cat /proc/net/dev; echo "---"; nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null || true; echo "---"; ps aux --sort=-%cpu 2>/dev/null | head -16 || ps aux 2>/dev/null | head -16`
}

// buildDarwinCommand returns the batched metrics command for macOS hosts.
// Output sections are separated by "---" and include:
// 0. top output - CPU usage and load averages
// 1. vm_stat + sysctl hw.memsize - Memory statistics with total memory
// 2. netstat output - Network interface statistics
// 3. ioreg GPU output - Apple Silicon GPU metrics (optional, fails silently)
// 4. ps aux - Process list sorted by CPU (top 16 including header)
func buildDarwinCommand() string {
	return `top -l 1 -n 0 2>/dev/null; echo "---"; vm_stat; sysctl hw.memsize 2>/dev/null; echo "---"; netstat -ib; echo "---"; ioreg -r -c AGXAccelerator 2>/dev/null | grep -E '"(model|gpu-core-count|PerformanceStatistics)"' || true; echo "---"; ps aux -r 2>/dev/null | head -16`
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
