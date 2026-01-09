package ui

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSyncProgressComponent(t *testing.T) {
	sp := NewSyncProgressComponent("Syncing files")

	assert.Equal(t, "Syncing files", sp.Label)
	assert.False(t, sp.Complete)
	assert.False(t, sp.Failed)
	assert.False(t, sp.StartTime.IsZero())
}

func TestSyncProgressComponentSetProgress(t *testing.T) {
	sp := NewSyncProgressComponent("Syncing")

	cmd := sp.SetProgress(SyncProgress{
		Percent:          0.5,
		BytesTransferred: 1024 * 1024,
		Speed:            "1.5MB/s",
		ETA:              "0:00:30",
	})

	assert.NotNil(t, cmd, "SetProgress should return a command")
	assert.Equal(t, 0.5, sp.Status.Percent)
	assert.Equal(t, int64(1024*1024), sp.Status.BytesTransferred)
	assert.Equal(t, "1.5MB/s", sp.Status.Speed)
}

func TestSyncProgressComponentView(t *testing.T) {
	sp := NewSyncProgressComponent("Syncing files")
	sp.SetProgress(SyncProgress{
		Percent:          0.75,
		BytesTransferred: 50 * 1024 * 1024,
		Speed:            "10.5MB/s",
		ETA:              "0:00:15",
		FilesTransferred: 10,
		TotalFiles:       20,
	})

	view := sp.View()

	assert.Contains(t, view, "Syncing files")
	assert.Contains(t, view, "75%")
	// Stats line should include various info
	assert.Contains(t, view, "10.5MB/s")
}

func TestSyncProgressComponentViewComplete(t *testing.T) {
	sp := NewSyncProgressComponent("Syncing files")
	sp.Success()

	view := sp.View()

	assert.Contains(t, view, SymbolComplete)
	assert.Contains(t, view, "Syncing files")
}

func TestSyncProgressComponentViewFailed(t *testing.T) {
	sp := NewSyncProgressComponent("Syncing files")
	sp.Fail()

	view := sp.View()

	assert.Contains(t, view, SymbolFail)
	assert.Contains(t, view, "Syncing files")
}

func TestSyncProgressComponentElapsed(t *testing.T) {
	sp := NewSyncProgressComponent("Test")
	time.Sleep(10 * time.Millisecond)

	elapsed := sp.Elapsed()
	assert.True(t, elapsed >= 10*time.Millisecond)
}

func TestParseRsyncProgress(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected *SyncProgress
	}{
		{
			name: "basic progress",
			line: "    1,234,567  45%    5.67MB/s    0:01:23",
			expected: &SyncProgress{
				Percent:          0.45,
				BytesTransferred: 1234567,
				Speed:            "5.67MB/s",
				ETA:              "0:01:23",
			},
		},
		{
			name: "with file counts",
			line: "    1,234,567  45%    5.67MB/s    0:01:23 (xfr#123, to-chk=456/789)",
			expected: &SyncProgress{
				Percent:          0.45,
				BytesTransferred: 1234567,
				Speed:            "5.67MB/s",
				ETA:              "0:01:23",
				FilesTransferred: 123,
				TotalFiles:       789,
			},
		},
		{
			name: "100% complete",
			line: "   50,000,000 100%   25.00MB/s    0:00:02",
			expected: &SyncProgress{
				Percent:          1.0,
				BytesTransferred: 50000000,
				Speed:            "25.00MB/s",
				ETA:              "0:00:02",
			},
		},
		{
			name: "ir-chk format",
			line: "      123,456  67%  800.00kB/s    0:00:45 (xfr#5, ir-chk=100/300)",
			expected: &SyncProgress{
				Percent:          0.67,
				BytesTransferred: 123456,
				Speed:            "800.00kB/s",
				ETA:              "0:00:45",
				FilesTransferred: 5,
				TotalFiles:       300,
			},
		},
		{
			name:     "empty line",
			line:     "",
			expected: nil,
		},
		{
			name:     "non-progress line",
			line:     "sending incremental file list",
			expected: nil,
		},
		{
			name:     "file list line",
			line:     "./some/file.txt",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRsyncProgress(tt.line)

			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result, "Expected progress to be parsed")
			assert.InDelta(t, tt.expected.Percent, result.Percent, 0.01)
			assert.Equal(t, tt.expected.BytesTransferred, result.BytesTransferred)
			assert.Equal(t, tt.expected.Speed, result.Speed)
			assert.Equal(t, tt.expected.ETA, result.ETA)
			assert.Equal(t, tt.expected.FilesTransferred, result.FilesTransferred)
			assert.Equal(t, tt.expected.TotalFiles, result.TotalFiles)
		})
	}
}

func TestSyncProgressWriter(t *testing.T) {
	updates := make(chan SyncProgress, 10)
	var passthrough bytes.Buffer

	writer := NewSyncProgressWriter(updates, &passthrough)

	// Write some progress data
	input := "    1,000,000  50%    2.00MB/s    0:00:30\n"
	n, err := writer.Write([]byte(input))

	require.NoError(t, err)
	assert.Equal(t, len(input), n)

	// Check passthrough received the data
	assert.Equal(t, input, passthrough.String())

	// Check update was sent
	select {
	case update := <-updates:
		assert.InDelta(t, 0.5, update.Percent, 0.01)
		assert.Equal(t, int64(1000000), update.BytesTransferred)
		assert.Equal(t, "2.00MB/s", update.Speed)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected update on channel")
	}
}

func TestSyncProgressWriterNonProgressLine(t *testing.T) {
	updates := make(chan SyncProgress, 10)
	writer := NewSyncProgressWriter(updates, nil)

	// Write non-progress data
	input := "sending incremental file list\n./some/file.txt\n"
	n, err := writer.Write([]byte(input))

	require.NoError(t, err)
	assert.Equal(t, len(input), n)

	// No updates should be sent
	select {
	case <-updates:
		t.Fatal("Should not receive update for non-progress line")
	case <-time.After(50 * time.Millisecond):
		// Expected - no update
	}
}

func TestFormatBytesUI(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{500, "500B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1024 * 1024, "1.0MB"},
		{1024 * 1024 * 512, "512.0MB"},
		{1024 * 1024 * 1024, "1.0GB"},
		{1024 * 1024 * 1024 * 2, "2.0GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
