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
// 1. /proc/stat - CPU statistics
// 2. /proc/loadavg - Load averages
// 3. /proc/meminfo - Memory information
// 4. /proc/net/dev - Network interface statistics
// 5. nvidia-smi output - GPU metrics (optional, fails silently if not available)
func buildLinuxCommand() string {
	return `cat /proc/stat; echo "---"; cat /proc/loadavg; echo "---"; cat /proc/meminfo; echo "---"; cat /proc/net/dev; echo "---"; nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null || true`
}

// buildDarwinCommand returns the batched metrics command for macOS hosts.
// Output sections are separated by "---" and include:
// 1. top output - CPU usage and load averages
// 2. vm_stat output - Memory statistics
// 3. netstat output - Network interface statistics
func buildDarwinCommand() string {
	return `top -l 1 -n 0 2>/dev/null; echo "---"; vm_stat; echo "---"; netstat -ib`
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
