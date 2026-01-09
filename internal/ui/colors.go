package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// DisableColors disables all color output by setting the color profile to Ascii.
// This should be called early in the program if --no-color flag is set.
func DisableColors() {
	lipgloss.SetColorProfile(termenv.Ascii)
}

// Color palette using ANSI color codes for terminal compatibility.
// Maps to proof-of-concept.sh color definitions:
//   RED='\033[0;31m'    -> ANSI 1
//   GREEN='\033[0;32m'  -> ANSI 2
//   YELLOW='\033[0;33m' -> ANSI 3
//   BLUE='\033[0;34m'   -> ANSI 4
//   CYAN='\033[0;36m'   -> ANSI 6
//   GRAY='\033[0;90m'   -> ANSI 8 (bright black)

// Semantic colors for status indication
const (
	ColorSuccess lipgloss.Color = "2" // Green
	ColorError   lipgloss.Color = "1" // Red
	ColorWarning lipgloss.Color = "3" // Yellow
	ColorInfo    lipgloss.Color = "6" // Cyan
)

// Text colors for content hierarchy
const (
	ColorPrimary   lipgloss.Color = "7" // White/default
	ColorSecondary lipgloss.Color = "4" // Blue
	ColorMuted     lipgloss.Color = "8" // Gray (bright black)
)

// Style helpers for common text styling

// SuccessStyle returns a style for success messages.
func SuccessStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorSuccess)
}

// ErrorStyle returns a style for error messages.
func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorError)
}

// WarningStyle returns a style for warning messages.
func WarningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorWarning)
}

// InfoStyle returns a style for informational messages.
func InfoStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorInfo)
}

// MutedStyle returns a style for muted/secondary text.
func MutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorMuted)
}
