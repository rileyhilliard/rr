package logs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
)

// logDir represents a log directory with metadata for cleanup decisions.
type logDir struct {
	path    string
	modTime time.Time
	size    int64
}

// Cleanup removes old log directories based on retention policy.
// Priority: MaxSizeMB > KeepDays > KeepRuns
// Returns nil if no cleanup is needed or cfg has no retention settings.
func Cleanup(cfg config.LogsConfig) error {
	baseDir := cfg.Dir
	if baseDir == "" {
		return nil
	}

	// Expand ~ in baseDir
	if len(baseDir) > 0 && baseDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				"Can't determine home directory",
				"Check your environment configuration.")
		}
		baseDir = filepath.Join(home, baseDir[1:])
	}

	// Check if directory exists
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return nil // Nothing to clean up
	}

	// Priority 1: MaxSizeMB
	if cfg.MaxSizeMB > 0 {
		if err := CleanBySize(baseDir, int64(cfg.MaxSizeMB)*1024*1024); err != nil {
			return err
		}
	}

	// Priority 2: KeepDays
	if cfg.KeepDays > 0 {
		maxAge := time.Duration(cfg.KeepDays) * 24 * time.Hour
		if err := CleanByAge(baseDir, maxAge); err != nil {
			return err
		}
	}

	// Priority 3: KeepRuns
	if cfg.KeepRuns > 0 {
		if err := CleanByRuns(baseDir, cfg.KeepRuns); err != nil {
			return err
		}
	}

	return nil
}

// CleanByRuns keeps only the last N runs per parallel task.
// Groups log directories by task name prefix and keeps the most recent N.
func CleanByRuns(baseDir string, keep int) error {
	if keep <= 0 {
		return nil
	}

	dirs, err := listLogDirs(baseDir)
	if err != nil {
		return err
	}

	// Group by task name (everything before the last timestamp)
	taskGroups := make(map[string][]logDir)
	for _, d := range dirs {
		taskName := extractTaskName(filepath.Base(d.path))
		taskGroups[taskName] = append(taskGroups[taskName], d)
	}

	// For each task, keep only the last N runs
	for _, group := range taskGroups {
		// Sort by modification time (newest first)
		sort.Slice(group, func(i, j int) bool {
			return group[i].modTime.After(group[j].modTime)
		})

		// Delete older runs
		if len(group) > keep {
			for _, d := range group[keep:] {
				if err := os.RemoveAll(d.path); err != nil {
					return errors.WrapWithCode(err, errors.ErrExec,
						"Can't delete log directory "+d.path,
						"Check your permissions.")
				}
			}
		}
	}

	return nil
}

// CleanByAge deletes logs older than maxAge.
func CleanByAge(baseDir string, maxAge time.Duration) error {
	if maxAge <= 0 {
		return nil
	}

	dirs, err := listLogDirs(baseDir)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)

	for _, d := range dirs {
		if d.modTime.Before(cutoff) {
			if err := os.RemoveAll(d.path); err != nil {
				return errors.WrapWithCode(err, errors.ErrExec,
					"Can't delete log directory "+d.path,
					"Check your permissions.")
			}
		}
	}

	return nil
}

// CleanBySize deletes oldest logs until total size is under limit.
func CleanBySize(baseDir string, maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}

	dirs, err := listLogDirs(baseDir)
	if err != nil {
		return err
	}

	// Calculate total size
	var totalSize int64
	for _, d := range dirs {
		totalSize += d.size
	}

	// If under limit, nothing to do
	if totalSize <= maxBytes {
		return nil
	}

	// Sort by modification time (oldest first for deletion)
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.Before(dirs[j].modTime)
	})

	// Delete oldest until under limit
	for _, d := range dirs {
		if totalSize <= maxBytes {
			break
		}

		if err := os.RemoveAll(d.path); err != nil {
			return errors.WrapWithCode(err, errors.ErrExec,
				"Can't delete log directory "+d.path,
				"Check your permissions.")
		}
		totalSize -= d.size
	}

	return nil
}

// CleanAll removes all log directories.
func CleanAll(baseDir string) error {
	// Expand ~ in baseDir
	if len(baseDir) > 0 && baseDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				"Can't determine home directory",
				"Check your environment configuration.")
		}
		baseDir = filepath.Join(home, baseDir[1:])
	}

	dirs, err := listLogDirs(baseDir)
	if err != nil {
		return err
	}

	for _, d := range dirs {
		if err := os.RemoveAll(d.path); err != nil {
			return errors.WrapWithCode(err, errors.ErrExec,
				"Can't delete log directory "+d.path,
				"Check your permissions.")
		}
	}

	return nil
}

// listLogDirs returns all log directories in baseDir with metadata.
func listLogDirs(baseDir string) ([]logDir, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.WrapWithCode(err, errors.ErrExec,
			"Can't read log directory "+baseDir,
			"Check your permissions.")
	}

	var dirs []logDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(baseDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't stat
		}

		// Calculate directory size
		size := calculateDirSize(path)

		dirs = append(dirs, logDir{
			path:    path,
			modTime: info.ModTime(),
			size:    size,
		})
	}

	return dirs, nil
}

// calculateDirSize returns the total size of all files in a directory.
func calculateDirSize(path string) int64 {
	var size int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}

// extractTaskName extracts the task name from a log directory name.
// Format: <taskname>-<timestamp> (e.g., "test-all-20240115-143022")
// Returns the part before the timestamp.
func extractTaskName(dirName string) string {
	// Timestamp format: YYYYMMDD-HHMMSS (15 chars including dash)
	// Find the second-to-last dash to split
	parts := strings.Split(dirName, "-")
	if len(parts) < 3 {
		return dirName
	}

	// Check if last two parts look like timestamp (8 digits - 6 digits)
	if len(parts) >= 2 {
		lastPart := parts[len(parts)-1]
		secondLast := parts[len(parts)-2]

		// If last part is 6 digits and second-to-last is 8 digits, it's a timestamp
		if len(lastPart) == 6 && len(secondLast) == 8 && isDigits(lastPart) && isDigits(secondLast) {
			return strings.Join(parts[:len(parts)-2], "-")
		}
	}

	return dirName
}

// isDigits returns true if all characters are digits.
func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ListLogDirs returns log directories for display purposes.
// Returns directories sorted by modification time (newest first).
func ListLogDirs(baseDir string) ([]LogDirInfo, error) {
	// Expand ~ in baseDir
	if len(baseDir) > 0 && baseDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrConfig,
				"Can't determine home directory",
				"Check your environment configuration.")
		}
		baseDir = filepath.Join(home, baseDir[1:])
	}

	dirs, err := listLogDirs(baseDir)
	if err != nil {
		return nil, err
	}

	// Sort newest first
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.After(dirs[j].modTime)
	})

	result := make([]LogDirInfo, len(dirs))
	for i, d := range dirs {
		result[i] = LogDirInfo{
			Path:     d.path,
			Name:     filepath.Base(d.path),
			TaskName: extractTaskName(filepath.Base(d.path)),
			ModTime:  d.modTime,
			Size:     d.size,
		}
	}

	return result, nil
}

// LogDirInfo contains information about a log directory for display.
type LogDirInfo struct {
	Path     string
	Name     string
	TaskName string
	ModTime  time.Time
	Size     int64
}
