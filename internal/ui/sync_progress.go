package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SyncProgress represents the current state of a file sync operation.
type SyncProgress struct {
	Percent          float64 // 0.0 to 1.0
	BytesTransferred int64
	Speed            string
	ETA              string
	FilesTransferred int
	TotalFiles       int
}

// SyncProgressComponent is a Bubble Tea model for displaying rsync progress.
type SyncProgressComponent struct {
	progress  progress.Model
	Label     string
	Status    SyncProgress
	StartTime time.Time
	Width     int
	Complete  bool
	Failed    bool
}

// NewSyncProgressComponent creates a new sync progress component.
func NewSyncProgressComponent(label string) SyncProgressComponent {
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
		progress.WithoutPercentage(), // We'll render our own
	)

	// Use existing color palette for gradient
	p.FullColor = string(ColorSuccess)
	p.EmptyColor = string(ColorMuted)

	return SyncProgressComponent{
		progress:  p,
		Label:     label,
		StartTime: time.Now(),
		Width:     40,
	}
}

// Init returns the initial command (none needed for progress).
func (s SyncProgressComponent) Init() tea.Cmd {
	return nil
}

// Update handles progress model updates.
func (s SyncProgressComponent) Update(msg tea.Msg) (SyncProgressComponent, tea.Cmd) {
	if frameMsg, ok := msg.(progress.FrameMsg); ok {
		progressModel, cmd := s.progress.Update(frameMsg)
		s.progress = progressModel.(progress.Model)
		return s, cmd
	}
	return s, nil
}

// SetProgress updates the progress state.
func (s *SyncProgressComponent) SetProgress(p SyncProgress) tea.Cmd {
	s.Status = p
	return s.progress.SetPercent(p.Percent)
}

// View renders the progress component.
func (s SyncProgressComponent) View() string {
	if s.Failed {
		return s.viewFailed()
	}
	if s.Complete {
		return s.viewComplete()
	}
	return s.viewInProgress()
}

func (s SyncProgressComponent) viewInProgress() string {
	var b strings.Builder

	// First line: label with spinner frame and progress bar
	symbolStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	frame := SpinnerFrames.Frames[int(time.Since(s.StartTime).Milliseconds()/100)%len(SpinnerFrames.Frames)]
	b.WriteString(symbolStyle.Render(frame))
	b.WriteString(" ")
	b.WriteString(s.Label)
	b.WriteString(" ")
	b.WriteString(s.progress.View())

	// Percentage
	pctStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	b.WriteString(" ")
	b.WriteString(pctStyle.Render(fmt.Sprintf("%3.0f%%", s.Status.Percent*100)))

	// Second line: stats
	if s.Status.Speed != "" || s.Status.BytesTransferred > 0 {
		b.WriteString("\n")
		b.WriteString("  ") // Indent to align with label
		statsStyle := lipgloss.NewStyle().Foreground(ColorMuted)

		var stats []string
		if s.Status.BytesTransferred > 0 {
			stats = append(stats, formatBytes(s.Status.BytesTransferred))
		}
		if s.Status.Speed != "" {
			stats = append(stats, s.Status.Speed)
		}
		if s.Status.ETA != "" && s.Status.ETA != "0:00:00" {
			stats = append(stats, "ETA "+s.Status.ETA)
		}
		if s.Status.TotalFiles > 0 {
			stats = append(stats, fmt.Sprintf("%d/%d files", s.Status.FilesTransferred, s.Status.TotalFiles))
		}

		b.WriteString(statsStyle.Render(strings.Join(stats, " | ")))
	}

	return b.String()
}

func (s SyncProgressComponent) viewComplete() string {
	symbolStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	elapsed := time.Since(s.StartTime)
	timing := formatDuration(elapsed)

	return fmt.Sprintf("%s %s %s",
		symbolStyle.Render(SymbolComplete),
		s.Label,
		timingStyle.Render(timing),
	)
}

func (s SyncProgressComponent) viewFailed() string {
	symbolStyle := lipgloss.NewStyle().Foreground(ColorError)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	elapsed := time.Since(s.StartTime)
	timing := formatDuration(elapsed)

	return fmt.Sprintf("%s %s %s",
		symbolStyle.Render(SymbolFail),
		s.Label,
		timingStyle.Render(timing),
	)
}

// Success marks the progress as complete.
func (s *SyncProgressComponent) Success() {
	s.Complete = true
}

// Fail marks the progress as failed.
func (s *SyncProgressComponent) Fail() {
	s.Failed = true
}

// Elapsed returns the duration since progress started.
func (s SyncProgressComponent) Elapsed() time.Duration {
	return time.Since(s.StartTime)
}

// formatBytes formats bytes in a compact form.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// SyncProgressWriter is an io.Writer that parses rsync progress output
// and sends updates to a channel.
type SyncProgressWriter struct {
	updates chan<- SyncProgress
	output  io.Writer // Optional pass-through output
}

// NewSyncProgressWriter creates a writer that parses rsync progress.
func NewSyncProgressWriter(updates chan<- SyncProgress, passthrough io.Writer) *SyncProgressWriter {
	return &SyncProgressWriter{
		updates: updates,
		output:  passthrough,
	}
}

// Write implements io.Writer, parsing rsync progress lines.
func (w *SyncProgressWriter) Write(p []byte) (n int, err error) {
	n = len(p)

	// Pass through to output if configured
	if w.output != nil {
		_, _ = w.output.Write(p)
	}

	// Parse each line for progress info
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if prog := parseRsyncProgress(line); prog != nil {
			select {
			case w.updates <- *prog:
			default:
				// Don't block if channel is full
			}
		}
	}

	return n, nil
}

// Close closes the updates channel.
func (w *SyncProgressWriter) Close() {
	close(w.updates)
}

// parseRsyncProgress parses a line of rsync --info=progress2 output.
// Returns nil if the line doesn't contain progress info.
func parseRsyncProgress(line string) *SyncProgress {
	// Import the sync package's parser would create a cycle,
	// so we duplicate the minimal parsing logic here.
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// Look for the percentage marker
	if !strings.Contains(line, "%") {
		return nil
	}

	// Split by whitespace
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return nil
	}

	// Find the percentage field
	var pctIdx int
	for i, f := range fields {
		if strings.HasSuffix(f, "%") {
			pctIdx = i
			break
		}
	}

	if pctIdx == 0 || pctIdx >= len(fields)-2 {
		return nil
	}

	// Parse percentage
	pctStr := strings.TrimSuffix(fields[pctIdx], "%")
	var pct int
	if _, err := fmt.Sscanf(pctStr, "%d", &pct); err != nil {
		return nil
	}

	// Parse bytes (field before percentage, remove commas)
	bytesStr := strings.ReplaceAll(fields[pctIdx-1], ",", "")
	var bytes int64
	if _, err := fmt.Sscanf(bytesStr, "%d", &bytes); err != nil {
		bytes = 0
	}

	// Speed and ETA follow percentage
	speed := ""
	eta := ""
	if pctIdx+1 < len(fields) {
		speed = fields[pctIdx+1]
	}
	if pctIdx+2 < len(fields) {
		eta = fields[pctIdx+2]
		// Strip any trailing parenthetical info
		if idx := strings.Index(eta, "("); idx > 0 {
			eta = eta[:idx]
		}
	}

	// Parse file counts from (xfr#N, to-chk=M/T) format
	var filesTransferred, totalFiles int
	for _, f := range fields {
		if strings.HasPrefix(f, "(xfr#") {
			_, _ = fmt.Sscanf(f, "(xfr#%d,", &filesTransferred)
		}
		if strings.Contains(f, "-chk=") {
			var remaining int
			if strings.Contains(f, "to-chk=") {
				_, _ = fmt.Sscanf(f, "to-chk=%d/%d)", &remaining, &totalFiles)
			} else if strings.Contains(f, "ir-chk=") {
				_, _ = fmt.Sscanf(f, "ir-chk=%d/%d)", &remaining, &totalFiles)
			}
		}
	}

	return &SyncProgress{
		Percent:          float64(pct) / 100.0,
		BytesTransferred: bytes,
		Speed:            speed,
		ETA:              eta,
		FilesTransferred: filesTransferred,
		TotalFiles:       totalFiles,
	}
}
