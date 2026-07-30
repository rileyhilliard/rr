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
	linuxLockSection  = 10
	darwinLockSection = 8
)

// SnapshotSleepSeconds is the delay between the two samples taken by the
// snapshot command. Delta-based metrics (CPU%, per-core, disk I/O, network
// rates) need two readings, so `rr monitor --once` pays this once.
const SnapshotSleepSeconds = 1

// Number of leading "priming" sections the snapshot command emits before the
// normal metrics battery. Linux primes /proc/stat, /proc/net/dev and
// /proc/diskstats; Darwin only needs netstat (top -l 1 is not delta-based).
const (
	linuxSnapshotPrimeSections  = 3
	darwinSnapshotPrimeSections = 1
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
// 0. /proc/stat - CPU statistics (aggregate + per-core)
// 1. /proc/loadavg - Load averages
// 2. /proc/meminfo - Memory information
// 3. /proc/net/dev - Network interface statistics
// 4. nvidia-smi output - GPU metrics (optional, fails silently if not available)
// 5. ps aux - Process list sorted by CPU (top 16 including header)
// 6. df -P -k / - Root filesystem usage
// 7. /proc/diskstats - Disk I/O counters (for rate deltas)
// 8. hwmon sensors - CPU temperature as "name:millidegrees" lines
// 9. /proc/uptime + uname -r - System info
// 10. Lock info.json - rr lock status (empty if unlocked)
func buildLinuxCommand(lockDir string) string {
	return `cat /proc/stat; echo "---"; cat /proc/loadavg; echo "---"; cat /proc/meminfo; echo "---"; cat /proc/net/dev; echo "---"; nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null || true; echo "---"; ps aux --sort=-%cpu 2>/dev/null | head -16 || ps aux 2>/dev/null | head -16; echo "---"; df -P -k / 2>/dev/null || true; echo "---"; cat /proc/diskstats 2>/dev/null || true; echo "---"; for d in /sys/class/hwmon/hwmon*; do [ -f "$d/name" ] && echo "$(cat $d/name):$(cat $d/temp1_input 2>/dev/null || echo)"; done 2>/dev/null || true; echo "---"; cat /proc/uptime 2>/dev/null; uname -r 2>/dev/null || true; echo "---"; ` + buildLockSection(lockDir)
}

// buildDarwinCommand returns the batched metrics command for macOS hosts.
// Output sections are separated by "---" and include:
// 0. top output - CPU usage and load averages
// 1. vm_stat + sysctl hw.memsize - Memory statistics with total memory
// 2. netstat output - Network interface statistics
// 3. ioreg GPU output - Apple Silicon GPU metrics (optional, fails silently)
// 4. ps aux - Process list sorted by CPU (top 16 including header)
// 5. df -P -k / - Root filesystem usage
// 6. sysctl -n hw.ncpu - CPU core count
// 7. sysctl -n kern.boottime + uname -r - System info
// 8. Lock info.json - rr lock status (empty if unlocked)
func buildDarwinCommand(lockDir string) string {
	return `top -l 1 -n 0 2>/dev/null; echo "---"; vm_stat; sysctl hw.memsize 2>/dev/null; echo "---"; netstat -ib; echo "---"; ioreg -r -c AGXAccelerator 2>/dev/null | grep -E '"(model|gpu-core-count|PerformanceStatistics)"' || true; echo "---"; ps aux -r 2>/dev/null | head -16; echo "---"; df -P -k / 2>/dev/null || true; echo "---"; sysctl -n hw.ncpu 2>/dev/null || true; echo "---"; sysctl -n kern.boottime 2>/dev/null; uname -r 2>/dev/null || true; echo "---"; ` + buildLockSection(lockDir)
}

// BuildSnapshotCommand returns a batched command that collects everything
// BuildMetricsCommand does, preceded by a priming sample of the delta-based
// sources and a one-second sleep. One SSH session yields real CPU%, per-core
// usage, disk I/O rates and network rates without a second round trip.
//
// The output is the priming sections, then exactly the sections
// BuildMetricsCommand produces, so the existing parsers apply unchanged after
// dropping the prime prefix.
func BuildSnapshotCommand(platform Platform, lockDir string) string {
	switch platform {
	case PlatformDarwin:
		return buildDarwinSnapshotPrime() + BuildMetricsCommand(PlatformDarwin, lockDir)
	case PlatformLinux, PlatformUnknown:
		return buildLinuxSnapshotPrime() + BuildMetricsCommand(PlatformLinux, lockDir)
	default:
		return buildLinuxSnapshotPrime() + BuildMetricsCommand(PlatformLinux, lockDir)
	}
}

// SnapshotPrimeSections returns how many leading sections BuildSnapshotCommand
// emits before the regular metrics sections for the given platform.
func SnapshotPrimeSections(platform Platform) int {
	if platform == PlatformDarwin {
		return darwinSnapshotPrimeSections
	}
	return linuxSnapshotPrimeSections
}

// buildLinuxSnapshotPrime emits /proc/stat, /proc/net/dev and /proc/diskstats
// as sections 0-2, then sleeps so the second reading has a real interval.
func buildLinuxSnapshotPrime() string {
	return fmt.Sprintf(
		`cat /proc/stat; echo "---"; cat /proc/net/dev; echo "---"; cat /proc/diskstats 2>/dev/null || true; echo "---"; sleep %d; `,
		SnapshotSleepSeconds)
}

// buildDarwinSnapshotPrime emits netstat -ib as section 0, then sleeps.
// macOS CPU comes from `top -l 1`, which is not delta-based, and there is no
// cheap per-disk byte counter, so only network needs priming.
func buildDarwinSnapshotPrime() string {
	return fmt.Sprintf(
		`netstat -ib; echo "---"; sleep %d; `,
		SnapshotSleepSeconds)
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
