package cli

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/parallel/logs"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/spf13/cobra"
)

// logsCmd implements the `rr logs` command for viewing and managing parallel task logs.
var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View and manage parallel task logs",
	Long: `View and manage log files from parallel task execution.

Log files are stored in ~/.rr/logs/<task>-<timestamp>/ by default.
Each run creates a directory with individual task logs and a summary.json.

Commands:
  rr logs              List recent log directories
  rr logs clean        Run cleanup based on retention policy
  rr logs clean --all  Delete all log files
  rr logs clean --older 7d  Delete logs older than duration`,
	RunE: runLogsList,
}

// logsCleanCmd implements the `rr logs clean` subcommand.
var logsCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up old log directories",
	Long: `Remove old log directories based on retention policy or flags.

Without flags, uses retention settings from ~/.rr/config.yaml:
  - max_size_mb: Delete oldest until under size limit
  - keep_days: Delete logs older than N days
  - keep_runs: Keep only last N runs per task

With flags:
  --all         Delete all log directories
  --older 7d    Delete logs older than specified duration`,
	RunE: runLogsClean,
}

var (
	logsCleanAll   bool
	logsCleanOlder string
)

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.AddCommand(logsCleanCmd)

	logsCleanCmd.Flags().BoolVar(&logsCleanAll, "all", false, "delete all log directories")
	logsCleanCmd.Flags().StringVar(&logsCleanOlder, "older", "", "delete logs older than duration (e.g., 7d, 24h)")
}

// runLogsList displays recent log directories.
func runLogsList(cmd *cobra.Command, args []string) error {
	// Get logs config
	logsCfg := GetGlobalLogsConfig()
	baseDir := logsCfg.Dir
	if baseDir == "" {
		baseDir = "~/.rr/logs"
	}

	// List directories
	dirs, err := logs.ListLogDirs(baseDir)
	if err != nil {
		return err
	}

	if len(dirs) == 0 {
		fmt.Println("No log directories found.")
		fmt.Println()
		fmt.Printf("Logs are stored in: %s\n", ExpandLogsDir(baseDir))
		fmt.Println("Run a parallel task to generate logs.")
		return nil
	}

	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	boldStyle := lipgloss.NewStyle().Bold(true)
	headerStyle := lipgloss.NewStyle().Foreground(ui.ColorSecondary).Bold(true)

	fmt.Println(headerStyle.Render("Recent Parallel Task Logs"))
	fmt.Println()

	for _, d := range dirs {
		// Format: task-name-timestamp  (size)  age
		age := time.Since(d.ModTime)
		ageStr := formatAge(age)
		sizeStr := formatSize(d.Size)

		fmt.Printf("  %s  %s  %s\n",
			boldStyle.Render(d.Name),
			mutedStyle.Render(sizeStr),
			mutedStyle.Render(ageStr),
		)
		fmt.Printf("    %s\n", mutedStyle.Render(d.Path))
	}

	fmt.Println()

	// Show total size
	var totalSize int64
	for _, d := range dirs {
		totalSize += d.Size
	}
	fmt.Printf("Total: %d directories, %s\n", len(dirs), formatSize(totalSize))
	fmt.Println()

	// Show retention settings
	fmt.Println(mutedStyle.Render("Retention settings (from ~/.rr/config.yaml):"))
	if logsCfg.KeepRuns > 0 {
		fmt.Printf("  %s\n", mutedStyle.Render(fmt.Sprintf("keep_runs: %d", logsCfg.KeepRuns)))
	}
	if logsCfg.KeepDays > 0 {
		fmt.Printf("  %s\n", mutedStyle.Render(fmt.Sprintf("keep_days: %d", logsCfg.KeepDays)))
	}
	if logsCfg.MaxSizeMB > 0 {
		fmt.Printf("  %s\n", mutedStyle.Render(fmt.Sprintf("max_size_mb: %d", logsCfg.MaxSizeMB)))
	}

	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  rr logs clean        Run retention cleanup")
	fmt.Println("  rr logs clean --all  Delete all logs")

	return nil
}

// runLogsClean removes old log directories.
func runLogsClean(cmd *cobra.Command, args []string) error {
	// Get logs config
	logsCfg := GetGlobalLogsConfig()
	baseDir := logsCfg.Dir
	if baseDir == "" {
		baseDir = "~/.rr/logs"
	}

	expandedDir := ExpandLogsDir(baseDir)

	// Handle --all flag
	if logsCleanAll {
		fmt.Printf("Deleting all logs in %s...\n", expandedDir)
		if err := logs.CleanAll(baseDir); err != nil {
			return err
		}
		fmt.Println("Done.")
		return nil
	}

	// Handle --older flag
	if logsCleanOlder != "" {
		duration, err := parseDurationWithDays(logsCleanOlder)
		if err != nil {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("Invalid duration '%s'", logsCleanOlder),
				"Use format like '7d' for days, '24h' for hours, or '30m' for minutes.")
		}

		fmt.Printf("Deleting logs older than %s...\n", logsCleanOlder)
		if err := logs.CleanByAge(expandedDir, duration); err != nil {
			return err
		}
		fmt.Println("Done.")
		return nil
	}

	// Default: use retention policy
	fmt.Println("Running cleanup based on retention settings...")

	beforeDirs, _ := logs.ListLogDirs(baseDir)
	beforeCount := len(beforeDirs)

	if err := logs.Cleanup(logsCfg); err != nil {
		return err
	}

	afterDirs, _ := logs.ListLogDirs(baseDir)
	afterCount := len(afterDirs)

	deleted := beforeCount - afterCount
	if deleted > 0 {
		fmt.Printf("Deleted %d log directories.\n", deleted)
	} else {
		fmt.Println("No logs needed cleanup.")
	}

	return nil
}

// parseDurationWithDays parses a duration string that may include 'd' for days.
func parseDurationWithDays(s string) (time.Duration, error) {
	// Handle 'd' suffix for days
	if len(s) > 0 && s[len(s)-1] == 'd' {
		days := s[:len(s)-1]
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return 0, err
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}

	// Fall back to standard duration parsing
	return time.ParseDuration(s)
}

// formatAge formats a duration as a human-readable age string.
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// formatSize formats a byte count as a human-readable string.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
