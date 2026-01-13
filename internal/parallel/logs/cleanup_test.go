package logs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanByRuns_KeepsCorrectNumber(t *testing.T) {
	baseDir := t.TempDir()

	// Create 5 log directories for the same task
	times := []time.Time{
		time.Now().Add(-5 * time.Hour),
		time.Now().Add(-4 * time.Hour),
		time.Now().Add(-3 * time.Hour),
		time.Now().Add(-2 * time.Hour),
		time.Now().Add(-1 * time.Hour),
	}

	for i, tm := range times {
		dirName := "test-task-" + tm.Format("20060102-150405")
		dir := filepath.Join(baseDir, dirName)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "output.log"), []byte("log data"), 0644))
		// Set modification time
		require.NoError(t, os.Chtimes(dir, times[i], times[i]))
	}

	// Keep only 2 runs
	err := CleanByRuns(baseDir, 2)
	require.NoError(t, err)

	// Verify only 2 remain
	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestCleanByRuns_DifferentTasks(t *testing.T) {
	baseDir := t.TempDir()

	// Create directories for different tasks
	tasks := []struct {
		name    string
		modTime time.Time
	}{
		{"task-a-20240101-100000", time.Now().Add(-3 * time.Hour)},
		{"task-a-20240101-110000", time.Now().Add(-2 * time.Hour)},
		{"task-a-20240101-120000", time.Now().Add(-1 * time.Hour)},
		{"task-b-20240101-100000", time.Now().Add(-3 * time.Hour)},
		{"task-b-20240101-110000", time.Now().Add(-2 * time.Hour)},
	}

	for _, task := range tasks {
		dir := filepath.Join(baseDir, task.name)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.Chtimes(dir, task.modTime, task.modTime))
	}

	// Keep 2 runs per task
	err := CleanByRuns(baseDir, 2)
	require.NoError(t, err)

	// Count remaining directories per task
	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)

	taskACount := 0
	taskBCount := 0
	for _, e := range entries {
		if e.Name()[:6] == "task-a" {
			taskACount++
		} else if e.Name()[:6] == "task-b" {
			taskBCount++
		}
	}

	assert.Equal(t, 2, taskACount, "task-a should have 2 remaining")
	assert.Equal(t, 2, taskBCount, "task-b should have 2 remaining")
}

func TestCleanByRuns_ZeroKeep(t *testing.T) {
	baseDir := t.TempDir()

	dir := filepath.Join(baseDir, "test-20240101-100000")
	require.NoError(t, os.MkdirAll(dir, 0755))

	// Zero keep should be a no-op
	err := CleanByRuns(baseDir, 0)
	require.NoError(t, err)

	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestCleanByAge_DeletesOldEntries(t *testing.T) {
	baseDir := t.TempDir()

	// Create directories with different ages
	oldDir := filepath.Join(baseDir, "old-task-20240101-100000")
	newDir := filepath.Join(baseDir, "new-task-20240115-100000")

	require.NoError(t, os.MkdirAll(oldDir, 0755))
	require.NoError(t, os.MkdirAll(newDir, 0755))

	// Set modification times
	oldTime := time.Now().Add(-10 * 24 * time.Hour) // 10 days old
	newTime := time.Now().Add(-1 * time.Hour)       // 1 hour old

	require.NoError(t, os.Chtimes(oldDir, oldTime, oldTime))
	require.NoError(t, os.Chtimes(newDir, newTime, newTime))

	// Clean entries older than 7 days
	err := CleanByAge(baseDir, 7*24*time.Hour)
	require.NoError(t, err)

	// Verify old is deleted, new remains
	_, err = os.Stat(oldDir)
	assert.True(t, os.IsNotExist(err), "old directory should be deleted")

	_, err = os.Stat(newDir)
	assert.NoError(t, err, "new directory should remain")
}

func TestCleanByAge_ZeroMaxAge(t *testing.T) {
	baseDir := t.TempDir()

	dir := filepath.Join(baseDir, "test-20240101-100000")
	require.NoError(t, os.MkdirAll(dir, 0755))

	// Zero maxAge should be a no-op
	err := CleanByAge(baseDir, 0)
	require.NoError(t, err)

	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestCleanBySize_RespectsLimit(t *testing.T) {
	baseDir := t.TempDir()

	// Create directories with known sizes
	dirs := []struct {
		name    string
		size    int
		modTime time.Time
	}{
		{"oldest-20240101-100000", 1000, time.Now().Add(-3 * time.Hour)},
		{"middle-20240101-110000", 1000, time.Now().Add(-2 * time.Hour)},
		{"newest-20240101-120000", 1000, time.Now().Add(-1 * time.Hour)},
	}

	for _, d := range dirs {
		dir := filepath.Join(baseDir, d.name)
		require.NoError(t, os.MkdirAll(dir, 0755))
		// Create a file with the specified size
		require.NoError(t, os.WriteFile(filepath.Join(dir, "data"), make([]byte, d.size), 0644))
		require.NoError(t, os.Chtimes(dir, d.modTime, d.modTime))
	}

	// Set limit to 2500 bytes (should keep 2 directories)
	err := CleanBySize(baseDir, 2500)
	require.NoError(t, err)

	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)

	// Should delete oldest to get under limit
	assert.LessOrEqual(t, len(entries), 2)

	// Newest should still exist
	_, err = os.Stat(filepath.Join(baseDir, "newest-20240101-120000"))
	assert.NoError(t, err)
}

func TestCleanBySize_ZeroLimit(t *testing.T) {
	baseDir := t.TempDir()

	dir := filepath.Join(baseDir, "test-20240101-100000")
	require.NoError(t, os.MkdirAll(dir, 0755))

	// Zero limit should be a no-op
	err := CleanBySize(baseDir, 0)
	require.NoError(t, err)

	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestCleanBySize_UnderLimit(t *testing.T) {
	baseDir := t.TempDir()

	dir := filepath.Join(baseDir, "test-20240101-100000")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data"), []byte("small"), 0644))

	// Limit much higher than content
	err := CleanBySize(baseDir, 1024*1024)
	require.NoError(t, err)

	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestCleanup_PriorityOrder(t *testing.T) {
	baseDir := t.TempDir()

	// Create test directories
	for i := 0; i < 5; i++ {
		tm := time.Now().Add(time.Duration(-i) * time.Hour)
		dir := filepath.Join(baseDir, "task-"+tm.Format("20060102-150405"))
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "data"), make([]byte, 100), 0644))
		require.NoError(t, os.Chtimes(dir, tm, tm))
	}

	// Cleanup with all constraints
	// Priority: size > days > runs
	cfg := config.LogsConfig{
		Dir:       baseDir,
		MaxSizeMB: 1,   // 1MB - shouldn't trigger since total is ~500 bytes
		KeepDays:  365, // 365 days - shouldn't trigger
		KeepRuns:  2,   // 2 runs - should trigger
	}

	err := Cleanup(cfg)
	require.NoError(t, err)

	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(entries), 2)
}

func TestCleanup_EmptyDir(t *testing.T) {
	cfg := config.LogsConfig{
		Dir:      "",
		KeepRuns: 5,
	}

	// Should not error with empty dir
	err := Cleanup(cfg)
	assert.NoError(t, err)
}

func TestCleanup_NonExistentDir(t *testing.T) {
	cfg := config.LogsConfig{
		Dir:      "/nonexistent/path/to/logs",
		KeepRuns: 5,
	}

	// Should not error with nonexistent dir
	err := Cleanup(cfg)
	assert.NoError(t, err)
}

func TestCleanAll(t *testing.T) {
	baseDir := t.TempDir()

	// Create some directories
	for i := 0; i < 3; i++ {
		dir := filepath.Join(baseDir, "task-"+time.Now().Add(time.Duration(-i)*time.Hour).Format("20060102-150405"))
		require.NoError(t, os.MkdirAll(dir, 0755))
	}

	err := CleanAll(baseDir)
	require.NoError(t, err)

	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestListLogDirs(t *testing.T) {
	baseDir := t.TempDir()

	// Create test directories
	times := []time.Time{
		time.Now().Add(-2 * time.Hour),
		time.Now().Add(-1 * time.Hour),
		time.Now(),
	}

	for _, tm := range times {
		dir := filepath.Join(baseDir, "task-"+tm.Format("20060102-150405"))
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "log"), []byte("data"), 0644))
		require.NoError(t, os.Chtimes(dir, tm, tm))
	}

	dirs, err := ListLogDirs(baseDir)
	require.NoError(t, err)

	assert.Len(t, dirs, 3)
	// Should be sorted newest first
	assert.True(t, dirs[0].ModTime.After(dirs[1].ModTime))
	assert.True(t, dirs[1].ModTime.After(dirs[2].ModTime))
}

func TestExtractTaskName(t *testing.T) {
	tests := []struct {
		dirName string
		want    string
	}{
		{"test-all-20240115-143022", "test-all"},
		{"simple-20240115-143022", "simple"},
		{"multi-part-name-20240115-143022", "multi-part-name"},
		{"no-timestamp", "no-timestamp"},
		{"", ""},
		// Timestamp-only input returns unchanged (no task name prefix to extract)
		{"20240115-143022", "20240115-143022"},
	}

	for _, tt := range tests {
		t.Run(tt.dirName, func(t *testing.T) {
			result := extractTaskName(tt.dirName)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsDigits(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123456", true},
		{"000000", true},
		{"12345a", false},
		{"a12345", false},
		{"", true}, // empty string has no non-digits
		{"hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, isDigits(tt.input))
		})
	}
}

func TestLogDirInfo(t *testing.T) {
	info := LogDirInfo{
		Path:     "/logs/test-20240115-143022",
		Name:     "test-20240115-143022",
		TaskName: "test",
		ModTime:  time.Now(),
		Size:     1024,
	}

	assert.Equal(t, "/logs/test-20240115-143022", info.Path)
	assert.Equal(t, "test-20240115-143022", info.Name)
	assert.Equal(t, "test", info.TaskName)
	assert.Equal(t, int64(1024), info.Size)
}

func TestCleanup_TildeExpansion(t *testing.T) {
	// Save original home
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Set temp home
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	logsDir := filepath.Join(tmpHome, "rr-logs")
	require.NoError(t, os.MkdirAll(logsDir, 0755))

	// Create a test log directory
	logDir := filepath.Join(logsDir, "test-20240115-143022")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	cfg := config.LogsConfig{
		Dir:      "~/rr-logs",
		KeepRuns: 10,
	}

	err := Cleanup(cfg)
	assert.NoError(t, err)
}

func TestCalculateDirSize(t *testing.T) {
	dir := t.TempDir()

	// Create files of known size
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1"), make([]byte, 100), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file2"), make([]byte, 200), 0644))

	subDir := filepath.Join(dir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "file3"), make([]byte, 50), 0644))

	size := calculateDirSize(dir)
	assert.Equal(t, int64(350), size)
}
