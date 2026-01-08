package sync

import (
	"regexp"
	"strconv"
	"strings"
)

// Progress represents parsed rsync progress information.
type Progress struct {
	BytesTransferred int64   // Total bytes transferred so far
	Percentage       int     // Transfer percentage (0-100)
	Speed            string  // Transfer speed (e.g., "1.23MB/s")
	TimeRemaining    string  // Estimated time remaining (e.g., "0:01:23")
	FileCount        int     // Number of files transferred (when available)
	TotalFiles       int     // Total files to transfer (when available)
}

// progressRegex matches rsync --info=progress2 output lines.
// Example output: "         32,768 100%    1.23MB/s    0:00:01 (xfr#1, to-chk=99/100)"
// Or simpler:     "      1,234,567  42%  500.00kB/s    0:01:23"
var progressRegex = regexp.MustCompile(
	`^\s*([\d,]+)\s+(\d+)%\s+([\d.]+[kMG]?B/s)\s+([\d:]+)(?:\s+\(xfr#(\d+),\s*(?:ir-chk|to-chk)=(\d+)/(\d+)\))?`,
)

// ParseProgress parses a line of rsync --info=progress2 output.
// Returns nil if the line is not a progress line.
func ParseProgress(line string) *Progress {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	matches := progressRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	p := &Progress{}

	// Parse bytes transferred (remove commas)
	bytesStr := strings.ReplaceAll(matches[1], ",", "")
	if bytes, err := strconv.ParseInt(bytesStr, 10, 64); err == nil {
		p.BytesTransferred = bytes
	}

	// Parse percentage
	if pct, err := strconv.Atoi(matches[2]); err == nil {
		p.Percentage = pct
	}

	// Speed and time remaining are kept as strings
	p.Speed = matches[3]
	p.TimeRemaining = matches[4]

	// Parse file counts if present (xfr# info)
	if len(matches) > 5 && matches[5] != "" {
		if xfr, err := strconv.Atoi(matches[5]); err == nil {
			p.FileCount = xfr
		}
	}

	// Parse total files from to-chk info
	// Format: to-chk=remaining/total, so total files = total
	if len(matches) > 7 && matches[7] != "" {
		if total, err := strconv.Atoi(matches[7]); err == nil {
			p.TotalFiles = total
		}
	}

	return p
}

// IsComplete returns true if the transfer is at 100%.
func (p *Progress) IsComplete() bool {
	return p != nil && p.Percentage == 100
}

// FormatBytes returns a human-readable byte count.
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return strconv.FormatFloat(float64(bytes)/float64(GB), 'f', 2, 64) + " GB"
	case bytes >= MB:
		return strconv.FormatFloat(float64(bytes)/float64(MB), 'f', 2, 64) + " MB"
	case bytes >= KB:
		return strconv.FormatFloat(float64(bytes)/float64(KB), 'f', 2, 64) + " KB"
	default:
		return strconv.FormatInt(bytes, 10) + " B"
	}
}
