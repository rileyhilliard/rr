package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ConnectionAttempt represents a single SSH alias connection attempt.
type ConnectionAttempt struct {
	Alias   string
	Status  ConnectionStatus
	Latency time.Duration
	Error   string
}

// ConnectionStatus represents the outcome of a connection attempt.
type ConnectionStatus int

const (
	// StatusTrying indicates the connection attempt is in progress.
	StatusTrying ConnectionStatus = iota
	// StatusSuccess indicates the connection attempt succeeded.
	StatusSuccess
	// StatusTimeout indicates the connection timed out.
	StatusTimeout
	// StatusRefused indicates the connection was refused.
	StatusRefused
	// StatusUnreachable indicates the host was unreachable.
	StatusUnreachable
	// StatusAuthFailed indicates authentication failed.
	StatusAuthFailed
	// StatusFailed indicates a generic failure.
	StatusFailed
)

// String returns a human-readable description of the status.
func (s ConnectionStatus) String() string {
	switch s {
	case StatusTrying:
		return "trying"
	case StatusSuccess:
		return "connected"
	case StatusTimeout:
		return "timeout"
	case StatusRefused:
		return "refused"
	case StatusUnreachable:
		return "unreachable"
	case StatusAuthFailed:
		return "auth failed"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ConnectionDisplay renders connection progress with sub-phases.
// Shows each alias attempt with its result indented under the main phase.
//
// Example output:
//
//	◐ Connecting...
//	  ○ mini-local                                         timeout (2s)
//	  ● mini (tailscale)                                        0.3s
//	● Connected to mini via mini (tailscale)                    2.3s
type ConnectionDisplay struct {
	mu       sync.Mutex
	w        io.Writer
	attempts []ConnectionAttempt
	spinner  *Spinner
	started  time.Time
	quiet    bool // If true, suppress individual attempt output
}

// NewConnectionDisplay creates a connection display writing to w.
func NewConnectionDisplay(w io.Writer) *ConnectionDisplay {
	return &ConnectionDisplay{
		w:        w,
		attempts: make([]ConnectionAttempt, 0),
	}
}

// SetQuiet enables or disables quiet mode.
// In quiet mode, individual attempts are not shown, only the final result.
func (cd *ConnectionDisplay) SetQuiet(quiet bool) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	cd.quiet = quiet
}

// Start begins the connection phase with an animated spinner.
func (cd *ConnectionDisplay) Start() {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	cd.started = time.Now()
	cd.spinner = NewSpinner("Connecting")
	cd.spinner.SetOutput(func(s string) {
		fmt.Fprint(cd.w, s)
	})
	cd.spinner.Start()
}

// AddAttempt records and displays a connection attempt.
// In quiet mode, attempts are recorded but not displayed until final result.
func (cd *ConnectionDisplay) AddAttempt(alias string, status ConnectionStatus, latency time.Duration, errMsg string) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	attempt := ConnectionAttempt{
		Alias:   alias,
		Status:  status,
		Latency: latency,
		Error:   errMsg,
	}
	cd.attempts = append(cd.attempts, attempt)

	// In quiet mode, don't render individual attempts
	if cd.quiet {
		return
	}

	// Stop spinner temporarily to render the attempt
	if cd.spinner != nil && cd.spinner.State() == SpinnerInProgress {
		cd.spinner.Stop()
	}

	// Render the attempt
	cd.renderAttempt(attempt)

	// Restart spinner if we're still connecting
	if status != StatusSuccess && cd.spinner != nil {
		cd.spinner.Start()
	}
}

// renderAttempt renders a single connection attempt line.
// Format:   ○ mini-local                                         timeout (2s)
func (cd *ConnectionDisplay) renderAttempt(attempt ConnectionAttempt) {
	var symbol string
	var symbolColor lipgloss.Color
	var status string

	switch attempt.Status {
	case StatusSuccess:
		symbol = SymbolComplete
		symbolColor = ColorSuccess
		status = formatDuration(attempt.Latency)
	case StatusTimeout:
		symbol = SymbolPending
		symbolColor = ColorMuted
		status = fmt.Sprintf("timeout (%s)", formatDuration(attempt.Latency))
	case StatusRefused:
		symbol = SymbolPending
		symbolColor = ColorMuted
		status = "refused"
	case StatusUnreachable:
		symbol = SymbolPending
		symbolColor = ColorMuted
		status = "unreachable"
	case StatusAuthFailed:
		symbol = SymbolPending
		symbolColor = ColorMuted
		status = "auth failed"
	case StatusFailed:
		symbol = SymbolPending
		symbolColor = ColorMuted
		if attempt.Error != "" {
			status = attempt.Error
		} else {
			status = "failed"
		}
	default:
		symbol = SymbolPending
		symbolColor = ColorMuted
		status = attempt.Status.String()
	}

	symbolStyle := lipgloss.NewStyle().Foreground(symbolColor)
	statusStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	// Calculate padding to align status on the right
	// Target width for alias + padding is ~50 chars
	aliasLen := len(attempt.Alias)
	padding := 50 - aliasLen
	if padding < 2 {
		padding = 2
	}

	fmt.Fprintf(cd.w, "  %s %s%s%s\n",
		symbolStyle.Render(symbol),
		attempt.Alias,
		strings.Repeat(" ", padding),
		statusStyle.Render(status),
	)
}

// Success completes the connection display with a success state.
// Renders the final "Connected to X via Y" line with total duration.
func (cd *ConnectionDisplay) Success(hostName, alias string) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if cd.spinner != nil {
		cd.spinner.Stop()
	}

	totalDuration := time.Since(cd.started)

	// Clear the spinner line if there are no attempts shown (quiet mode)
	if cd.quiet && len(cd.attempts) > 0 {
		// Clear line (carriage return + spaces + carriage return)
		fmt.Fprint(cd.w, "\r"+strings.Repeat(" ", 80)+"\r")
	}

	// Render final success line
	symbolStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	msg := fmt.Sprintf("Connected to %s via %s", hostName, alias)
	fmt.Fprintf(cd.w, "%s %s %s\n",
		symbolStyle.Render(SymbolComplete),
		msg,
		timingStyle.Render(formatDuration(totalDuration)),
	)
}

// SuccessLocal completes the connection display for local fallback.
func (cd *ConnectionDisplay) SuccessLocal() {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if cd.spinner != nil {
		cd.spinner.Stop()
	}

	totalDuration := time.Since(cd.started)

	// Clear spinner line
	fmt.Fprint(cd.w, "\r"+strings.Repeat(" ", 80)+"\r")

	// Render local fallback message
	symbolStyle := lipgloss.NewStyle().Foreground(ColorWarning)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	fmt.Fprintf(cd.w, "%s Running locally (all remote hosts unreachable) %s\n",
		symbolStyle.Render(SymbolComplete),
		timingStyle.Render(formatDuration(totalDuration)),
	)
}

// Fail completes the connection display with a failure state.
func (cd *ConnectionDisplay) Fail(errMsg string) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if cd.spinner != nil {
		cd.spinner.Stop()
	}

	totalDuration := time.Since(cd.started)

	// Clear spinner line
	fmt.Fprint(cd.w, "\r"+strings.Repeat(" ", 80)+"\r")

	// Render failure line
	symbolStyle := lipgloss.NewStyle().Foreground(ColorError)
	timingStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	msg := "Connection failed"
	if errMsg != "" {
		msg = fmt.Sprintf("Connection failed: %s", errMsg)
	}

	fmt.Fprintf(cd.w, "%s %s %s\n",
		symbolStyle.Render(SymbolFail),
		msg,
		timingStyle.Render(formatDuration(totalDuration)),
	)
}

// Attempts returns a copy of all recorded connection attempts.
func (cd *ConnectionDisplay) Attempts() []ConnectionAttempt {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	result := make([]ConnectionAttempt, len(cd.attempts))
	copy(result, cd.attempts)
	return result
}

// HasFailedAttempts returns true if any attempts failed before success.
func (cd *ConnectionDisplay) HasFailedAttempts() bool {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	for _, a := range cd.attempts {
		if a.Status != StatusSuccess && a.Status != StatusTrying {
			return true
		}
	}
	return false
}

// SuccessfulAlias returns the alias that successfully connected, or empty string.
func (cd *ConnectionDisplay) SuccessfulAlias() string {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	for _, a := range cd.attempts {
		if a.Status == StatusSuccess {
			return a.Alias
		}
	}
	return ""
}

// RenderAttemptLine returns a formatted attempt line as a string (for testing).
func RenderAttemptLine(alias string, status ConnectionStatus, latency time.Duration, errMsg string) string {
	var symbol string
	var statusStr string

	switch status {
	case StatusSuccess:
		symbol = SymbolComplete
		statusStr = formatDuration(latency)
	case StatusTimeout:
		symbol = SymbolPending
		statusStr = fmt.Sprintf("timeout (%s)", formatDuration(latency))
	case StatusRefused:
		symbol = SymbolPending
		statusStr = "refused"
	case StatusUnreachable:
		symbol = SymbolPending
		statusStr = "unreachable"
	case StatusAuthFailed:
		symbol = SymbolPending
		statusStr = "auth failed"
	case StatusFailed:
		symbol = SymbolPending
		if errMsg != "" {
			statusStr = errMsg
		} else {
			statusStr = "failed"
		}
	default:
		symbol = SymbolPending
		statusStr = status.String()
	}

	// Calculate padding
	aliasLen := len(alias)
	padding := 50 - aliasLen
	if padding < 2 {
		padding = 2
	}

	return fmt.Sprintf("  %s %s%s%s",
		symbol,
		alias,
		strings.Repeat(" ", padding),
		statusStr,
	)
}
