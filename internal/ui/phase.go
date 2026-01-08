package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// DividerWidth is the default width for divider lines.
const DividerWidth = 64

// Phase represents a distinct execution phase.
type Phase struct {
	Name      string
	StartTime time.Time
	EndTime   time.Time
	Success   bool
	Skipped   bool
	Error     error
}

// Duration returns the phase duration.
func (p Phase) Duration() time.Duration {
	if p.EndTime.IsZero() {
		return time.Since(p.StartTime)
	}
	return p.EndTime.Sub(p.StartTime)
}

// PhaseDisplay renders phase status to an output writer.
type PhaseDisplay struct {
	w      io.Writer
	phases []Phase
}

// NewPhaseDisplay creates a new phase display writing to w.
func NewPhaseDisplay(w io.Writer) *PhaseDisplay {
	return &PhaseDisplay{
		w:      w,
		phases: make([]Phase, 0),
	}
}

// RenderProgress renders a phase in progress.
// Shows: ◐ Connecting... (animated symbol in blue)
func (pd *PhaseDisplay) RenderProgress(name string) {
	style := lipgloss.NewStyle().Foreground(ColorSecondary)
	fmt.Fprintf(pd.w, "\r%s %s...", style.Render(SymbolProgress), name)
}

// RenderSuccess renders a completed phase.
// Shows: ● Connected (0.3s)
func (pd *PhaseDisplay) RenderSuccess(name string, duration time.Duration) {
	pd.clearLine()

	symbolStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	fmt.Fprintf(pd.w, "%s %s %s\n",
		symbolStyle.Render(SymbolComplete),
		name,
		timingStyle.Render(formatDuration(duration)),
	)
}

// RenderFailed renders a failed phase.
// Shows: ✗ Connection failed (2.3s)
func (pd *PhaseDisplay) RenderFailed(name string, duration time.Duration, err error) {
	pd.clearLine()

	symbolStyle := lipgloss.NewStyle().Foreground(ColorError)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	fmt.Fprintf(pd.w, "%s %s %s\n",
		symbolStyle.Render(SymbolFail),
		name,
		timingStyle.Render(formatDuration(duration)),
	)
}

// RenderSkipped renders a skipped phase.
// Shows: ⊘ Syncing (skipped)
func (pd *PhaseDisplay) RenderSkipped(name string, reason string) {
	pd.clearLine()

	symbolStyle := lipgloss.NewStyle().Foreground(ColorWarning)
	reasonStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	if reason != "" {
		fmt.Fprintf(pd.w, "%s %s %s\n",
			symbolStyle.Render(SymbolSkipped),
			name,
			reasonStyle.Render("("+reason+")"),
		)
	} else {
		fmt.Fprintf(pd.w, "%s %s\n",
			symbolStyle.Render(SymbolSkipped),
			name,
		)
	}
}

// RenderSubStatus renders an indented sub-status line.
// Used for showing connection attempts, file counts, etc.
// Shows:   ○ mini-local                                         timeout (2s)
func (pd *PhaseDisplay) RenderSubStatus(symbol string, name string, status string) {
	style := lipgloss.NewStyle().Foreground(ColorMuted)
	fmt.Fprintf(pd.w, "  %s %s %s\n",
		style.Render(symbol),
		name,
		style.Render(status),
	)
}

// Divider renders a horizontal line to separate phases from command output.
// Uses thick box-drawing characters: ━━━━━━━━━━━━━━━━━
func (pd *PhaseDisplay) Divider() {
	style := lipgloss.NewStyle().Foreground(ColorMuted)
	fmt.Fprintf(pd.w, "\n%s\n\n", style.Render(strings.Repeat("━", DividerWidth)))
}

// ThinDivider renders a thin horizontal line.
// Uses thin box-drawing characters: ────────────────
func (pd *PhaseDisplay) ThinDivider() {
	style := lipgloss.NewStyle().Foreground(ColorMuted)
	fmt.Fprintf(pd.w, "\n%s\n\n", style.Render(strings.Repeat("─", DividerWidth)))
}

// CommandPrompt renders the command about to be executed.
// Shows: $ pytest -n auto
func (pd *PhaseDisplay) CommandPrompt(cmd string) {
	style := lipgloss.NewStyle().Foreground(ColorMuted)
	fmt.Fprintf(pd.w, "%s %s\n", style.Render("$"), cmd)
}

// Newline writes an empty line.
func (pd *PhaseDisplay) Newline() {
	fmt.Fprintln(pd.w)
}

// clearLine clears the current line (for overwriting spinner output).
func (pd *PhaseDisplay) clearLine() {
	// Write carriage return and spaces to clear any spinner output
	// This is a simple approach; more sophisticated clearing could be added
	fmt.Fprint(pd.w, "\r"+strings.Repeat(" ", 80)+"\r")
}

// FormatPhase returns a formatted phase line as a string.
// Useful for building custom output.
func FormatPhase(symbol string, symbolColor lipgloss.Color, name string, timing string) string {
	symbolStyle := lipgloss.NewStyle().Foreground(symbolColor)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	if timing == "" {
		return fmt.Sprintf("%s %s", symbolStyle.Render(symbol), name)
	}
	return fmt.Sprintf("%s %s %s", symbolStyle.Render(symbol), name, timingStyle.Render(timing))
}

// FormatDivider returns a divider line as a string.
func FormatDivider(width int) string {
	style := lipgloss.NewStyle().Foreground(ColorMuted)
	return style.Render(strings.Repeat("━", width))
}
