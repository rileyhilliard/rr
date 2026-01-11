package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// SpinnerState represents the current state of a spinner.
type SpinnerState int

const (
	SpinnerPending SpinnerState = iota
	SpinnerInProgress
	SpinnerSuccess
	SpinnerFailed
	SpinnerSkipped
)

// Spinner animation frames - braille scan pattern for Y2K techy feel
var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Spinner displays an animated status indicator with a label.
type Spinner struct {
	mu           sync.Mutex
	label        string
	state        SpinnerState
	frame        int
	startTime    time.Time
	stopChan     chan struct{}
	doneChan     chan struct{}
	output       func(string)
	running      bool
	lastRendered string
}

// NewSpinner creates a new spinner with the given label.
// Output defaults to fmt.Print; use SetOutput to customize.
func NewSpinner(label string) *Spinner {
	return &Spinner{
		label:  label,
		state:  SpinnerPending,
		output: func(s string) { fmt.Print(s) },
	}
}

// SetOutput sets the output function for the spinner.
// Useful for testing or redirecting output.
func (s *Spinner) SetOutput(fn func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output = fn
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.state = SpinnerInProgress
	s.startTime = time.Now()
	s.stopChan = make(chan struct{})
	s.doneChan = make(chan struct{})
	s.mu.Unlock()

	s.render()

	go s.animate()
}

// Stop halts the spinner animation without changing state.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()

	<-s.doneChan
}

// Success stops the spinner and marks it as successful.
func (s *Spinner) Success() {
	s.Stop()
	s.mu.Lock()
	s.state = SpinnerSuccess
	s.mu.Unlock()
	s.renderFinal()
}

// Fail stops the spinner and marks it as failed.
func (s *Spinner) Fail() {
	s.Stop()
	s.mu.Lock()
	s.state = SpinnerFailed
	s.mu.Unlock()
	s.renderFinal()
}

// Skip stops the spinner and marks it as skipped.
func (s *Spinner) Skip() {
	s.Stop()
	s.mu.Lock()
	s.state = SpinnerSkipped
	s.mu.Unlock()
	s.renderFinal()
}

// State returns the current spinner state.
func (s *Spinner) State() SpinnerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Elapsed returns the time since the spinner started.
func (s *Spinner) Elapsed() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startTime.IsZero() {
		return 0
	}
	return time.Since(s.startTime)
}

// Label returns the spinner's label.
func (s *Spinner) Label() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.label
}

// SetLabel updates the spinner's label.
func (s *Spinner) SetLabel(label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.label = label
}

func (s *Spinner) animate() {
	ticker := time.NewTicker(60 * time.Millisecond) // Faster for more energy
	defer ticker.Stop()
	defer close(s.doneChan)

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.mu.Lock()
			s.frame = (s.frame + 1) % len(spinnerFrames)
			s.mu.Unlock()
			s.render()
		}
	}
}

func (s *Spinner) render() {
	s.mu.Lock()
	defer s.mu.Unlock()

	symbol := spinnerFrames[s.frame]
	// Cycle through gradient colors (pink -> purple -> cyan -> green)
	colorIndex := (s.frame / 2) % len(GradientColors)
	style := lipgloss.NewStyle().Foreground(GradientColors[colorIndex])

	line := fmt.Sprintf("\r%s %s...", style.Render(symbol), s.label)

	if s.lastRendered != "" {
		// Clear the previous line
		clearLen := len([]rune(s.lastRendered))
		s.output("\r" + strings.Repeat(" ", clearLen) + "\r")
	}

	s.output(line)
	s.lastRendered = line
}

func (s *Spinner) renderFinal() {
	s.mu.Lock()
	defer s.mu.Unlock()

	var symbol string
	var style lipgloss.Style

	switch s.state {
	case SpinnerSuccess:
		symbol = SymbolComplete
		style = lipgloss.NewStyle().Foreground(ColorSuccess)
	case SpinnerFailed:
		symbol = SymbolFail
		style = lipgloss.NewStyle().Foreground(ColorError)
	case SpinnerSkipped:
		symbol = SymbolSkipped
		style = lipgloss.NewStyle().Foreground(ColorWarning)
	default:
		symbol = SymbolPending
		style = lipgloss.NewStyle().Foreground(ColorMuted)
	}

	elapsed := time.Since(s.startTime)
	timing := formatDuration(elapsed)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	// Clear the current line and render final state
	if s.lastRendered != "" {
		clearLen := len([]rune(s.lastRendered))
		s.output("\r" + strings.Repeat(" ", clearLen) + "\r")
	}

	line := fmt.Sprintf("%s %s %s\n",
		style.Render(symbol),
		s.label,
		timingStyle.Render(timing),
	)
	s.output(line)
}

// formatDuration formats a duration for display (e.g., "0.3s", "1.2s").
func formatDuration(d time.Duration) string {
	secs := d.Seconds()
	if secs < 0.1 {
		return fmt.Sprintf("%.2fs", secs)
	}
	return fmt.Sprintf("%.1fs", secs)
}
