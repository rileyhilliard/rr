package ui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SpinnerFrames defines the custom animation frames (◐ ◓ ◑ ◒) for use in Bubble Tea programs.
// This provides consistent styling between CLI spinners and TUI components.
var SpinnerFrames = spinner.Spinner{
	Frames: []string{"◐", "◓", "◑", "◒"},
	FPS:    time.Second / 10, // 100ms per frame
}

// SpinnerComponentState represents the state of a spinner in a Bubble Tea model.
type SpinnerComponentState int

const (
	SpinnerComponentPending SpinnerComponentState = iota
	SpinnerComponentInProgress
	SpinnerComponentSuccess
	SpinnerComponentFailed
	SpinnerComponentSkipped
)

// SpinnerComponent is a Bubble Tea model for embedding spinners in TUI programs.
// Unlike the standalone Spinner, this is designed to be composed into larger models.
type SpinnerComponent struct {
	spinner   spinner.Model
	Label     string
	State     SpinnerComponentState
	StartTime time.Time
}

// NewSpinnerComponent creates a new spinner component with the given label.
func NewSpinnerComponent(label string) SpinnerComponent {
	sp := spinner.New()
	sp.Spinner = SpinnerFrames
	sp.Style = lipgloss.NewStyle().Foreground(ColorSecondary)

	return SpinnerComponent{
		spinner: sp,
		Label:   label,
		State:   SpinnerComponentPending,
	}
}

// Init returns the initial command for the spinner (tick).
func (s SpinnerComponent) Init() tea.Cmd {
	return s.spinner.Tick
}

// Update handles spinner animation messages.
func (s SpinnerComponent) Update(msg tea.Msg) (SpinnerComponent, tea.Cmd) {
	if s.State != SpinnerComponentInProgress {
		return s, nil
	}

	if tickMsg, ok := msg.(spinner.TickMsg); ok {
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(tickMsg)
		return s, cmd
	}
	return s, nil
}

// View renders the spinner in its current state.
func (s SpinnerComponent) View() string {
	switch s.State {
	case SpinnerComponentInProgress:
		return s.viewInProgress()
	case SpinnerComponentSuccess:
		return s.viewFinal(SymbolComplete, ColorSuccess)
	case SpinnerComponentFailed:
		return s.viewFinal(SymbolFail, ColorError)
	case SpinnerComponentSkipped:
		return s.viewFinal(SymbolSkipped, ColorWarning)
	default:
		return s.viewFinal(SymbolPending, ColorMuted)
	}
}

func (s SpinnerComponent) viewInProgress() string {
	return s.spinner.View() + " " + s.Label + "..."
}

func (s SpinnerComponent) viewFinal(symbol string, color lipgloss.Color) string {
	symbolStyle := lipgloss.NewStyle().Foreground(color)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	elapsed := time.Since(s.StartTime)
	timing := formatDuration(elapsed)

	return symbolStyle.Render(symbol) + " " + s.Label + " " + timingStyle.Render(timing)
}

// Start transitions the spinner to in-progress state.
func (s *SpinnerComponent) Start() tea.Cmd {
	s.State = SpinnerComponentInProgress
	s.StartTime = time.Now()
	return s.spinner.Tick
}

// Success transitions the spinner to success state.
func (s *SpinnerComponent) Success() {
	s.State = SpinnerComponentSuccess
}

// Fail transitions the spinner to failed state.
func (s *SpinnerComponent) Fail() {
	s.State = SpinnerComponentFailed
}

// Skip transitions the spinner to skipped state.
func (s *SpinnerComponent) Skip() {
	s.State = SpinnerComponentSkipped
}

// Elapsed returns the duration since the spinner started.
func (s SpinnerComponent) Elapsed() time.Duration {
	if s.StartTime.IsZero() {
		return 0
	}
	return time.Since(s.StartTime)
}
