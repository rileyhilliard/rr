package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// InlineProgress displays an animated progress bar for CLI use (outside Bubble Tea).
// It uses a goroutine for animation, similar to the Spinner implementation.
type InlineProgress struct {
	mu           sync.Mutex
	label        string
	percent      float64 // Real progress from rsync
	speed        string
	eta          string
	bytes        int64
	startTime    time.Time
	stopChan     chan struct{}
	doneChan     chan struct{}
	output       io.Writer
	running      bool
	lastRendered string
	width        int
	useFake      bool // Whether to use fake progress animation
}

// NewInlineProgress creates a new inline progress display.
func NewInlineProgress(label string, output io.Writer) *InlineProgress {
	return &InlineProgress{
		label:   label,
		output:  output,
		width:   30, // Default progress bar width
		useFake: true,
	}
}

// SetUseFakeProgress enables or disables the fake progress animation.
// When enabled, progress animates with ease-out over 10s to give perceived responsiveness.
func (p *InlineProgress) SetUseFakeProgress(use bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.useFake = use
}

// fakeProgressDuration is how long the fake progress takes to reach 99%.
const fakeProgressDuration = 30 * time.Second

// easeOutQuad applies an ease-out quadratic curve: decelerates toward the end.
// t should be in range [0, 1], returns value in [0, 1].
func easeOutQuad(t float64) float64 {
	return 1 - (1-t)*(1-t)
}

// calculateFakeProgress returns simulated progress based on elapsed time.
// Uses ease-out curve over fakeProgressDuration, capping at 99%.
func (p *InlineProgress) calculateFakeProgress() float64 {
	elapsed := time.Since(p.startTime)
	if elapsed >= fakeProgressDuration {
		return 0.99 // Cap at 99% until real completion
	}
	t := float64(elapsed) / float64(fakeProgressDuration)
	// Ease-out to 99%
	return easeOutQuad(t) * 0.99
}

// SetWidth sets the progress bar width.
func (p *InlineProgress) SetWidth(w int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.width = w
}

// Start begins the progress animation.
func (p *InlineProgress) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.startTime = time.Now()
	p.stopChan = make(chan struct{})
	p.doneChan = make(chan struct{})
	p.mu.Unlock()

	p.render()

	go p.animate()
}

// Update updates the progress with new values.
func (p *InlineProgress) Update(percent float64, speed, eta string, bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.percent = percent
	p.speed = speed
	p.eta = eta
	p.bytes = bytes
}

// Stop halts the progress animation.
func (p *InlineProgress) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	close(p.stopChan)
	p.mu.Unlock()

	<-p.doneChan
}

// Success stops and renders success state.
func (p *InlineProgress) Success() {
	p.Stop()
	p.renderFinal(true)
}

// Fail stops and renders failure state.
func (p *InlineProgress) Fail() {
	p.Stop()
	p.renderFinal(false)
}

func (p *InlineProgress) animate() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer close(p.doneChan)

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.render()
		}
	}
}

// effectiveProgress returns the progress to display.
// Uses the max of real progress and fake progress (if enabled).
// Must be called with lock held.
func (p *InlineProgress) effectiveProgressLocked() float64 {
	if !p.useFake {
		return p.percent
	}
	fake := p.calculateFakeProgress()
	if p.percent > fake {
		return p.percent
	}
	return fake
}

func (p *InlineProgress) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Calculate effective progress (max of real and fake)
	effectivePercent := p.effectiveProgressLocked()

	// Spinner frame
	frame := spinnerFrames[int(time.Since(p.startTime).Milliseconds()/100)%len(spinnerFrames)]
	symbolStyle := lipgloss.NewStyle().Foreground(ColorSecondary)

	// Progress bar
	bar := p.renderBarWithPercent(effectivePercent)

	// Percentage
	pctStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	pctStr := pctStyle.Render(fmt.Sprintf("%3.0f%%", effectivePercent*100))

	// Stats line
	var stats string
	if p.speed != "" || p.bytes > 0 {
		statsStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		var parts []string
		if p.bytes > 0 {
			parts = append(parts, formatBytes(p.bytes))
		}
		if p.speed != "" {
			parts = append(parts, p.speed)
		}
		if p.eta != "" && p.eta != "0:00:00" {
			parts = append(parts, "ETA "+p.eta)
		}
		if len(parts) > 0 {
			stats = " " + statsStyle.Render(strings.Join(parts, " | "))
		}
	}

	line := fmt.Sprintf("\r%s %s %s %s%s",
		symbolStyle.Render(frame),
		p.label,
		bar,
		pctStr,
		stats,
	)

	// Clear previous line if needed
	if p.lastRendered != "" {
		clearLen := len([]rune(stripAnsi(p.lastRendered)))
		fmt.Fprintf(p.output, "\r%s\r", strings.Repeat(" ", clearLen))
	}

	fmt.Fprint(p.output, line)
	p.lastRendered = line
}

func (p *InlineProgress) renderBarWithPercent(percent float64) string {
	filled, empty := CalculateBarCountsNormalized(percent, p.width)

	// Choose color based on progress (higher = better)
	barColor := ProgressColorProgress(percent * 100) // Convert 0-1 to 0-100

	filledStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	filledBar := filledStyle.Render(strings.Repeat(string(BarFilled), filled))
	emptyBar := emptyStyle.Render(strings.Repeat(string(BarEmpty), empty))

	return "[" + filledBar + emptyBar + "]"
}

func (p *InlineProgress) renderFinal(success bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear current line
	if p.lastRendered != "" {
		clearLen := len([]rune(stripAnsi(p.lastRendered)))
		fmt.Fprintf(p.output, "\r%s\r", strings.Repeat(" ", clearLen))
	}

	var symbol string
	var style lipgloss.Style

	if success {
		symbol = SymbolComplete
		style = lipgloss.NewStyle().Foreground(ColorSuccess)
	} else {
		symbol = SymbolFail
		style = lipgloss.NewStyle().Foreground(ColorError)
	}

	elapsed := time.Since(p.startTime)
	timing := formatDuration(elapsed)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	// Include bytes transferred in final output
	var bytesInfo string
	if p.bytes > 0 {
		bytesStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		bytesInfo = " " + bytesStyle.Render("("+formatBytes(p.bytes)+")")
	}

	fmt.Fprintf(p.output, "%s %s%s %s\n",
		style.Render(symbol),
		p.label,
		bytesInfo,
		timingStyle.Render(timing),
	)
}

// stripAnsi removes ANSI escape codes from a string for length calculation.
func stripAnsi(s string) string {
	// Simple approach: remove escape sequences
	result := strings.Builder{}
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// ProgressWriter wraps an io.Writer to parse rsync progress and update an InlineProgress.
type ProgressWriter struct {
	progress    *InlineProgress
	passthrough io.Writer
}

// NewProgressWriter creates a writer that updates progress from rsync output.
func NewProgressWriter(progress *InlineProgress, passthrough io.Writer) *ProgressWriter {
	return &ProgressWriter{
		progress:    progress,
		passthrough: passthrough,
	}
}

// Write implements io.Writer.
func (w *ProgressWriter) Write(p []byte) (n int, err error) {
	n = len(p)

	// Pass through if configured
	if w.passthrough != nil {
		_, _ = w.passthrough.Write(p)
	}

	// Parse lines for progress
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if prog := parseRsyncProgress(line); prog != nil {
			w.progress.Update(prog.Percent, prog.Speed, prog.ETA, prog.BytesTransferred)
		}
	}

	return n, nil
}
