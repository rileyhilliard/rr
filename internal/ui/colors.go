package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// DisableColors disables all color output by setting the color profile to Ascii.
// This should be called early in the program if --no-color flag is set.
func DisableColors() {
	lipgloss.SetColorProfile(termenv.Ascii)
}

// Electric Synthwave color palette - Gen Z dopamine-inducing neons
// Primary accent colors
const (
	ColorNeonPink   lipgloss.Color = "#FF2E97" // Primary accent, selected states
	ColorNeonCyan   lipgloss.Color = "#00FFFF" // Secondary accent, info, values
	ColorNeonPurple lipgloss.Color = "#BF40FF" // Tertiary, gradient midpoint
	ColorNeonGreen  lipgloss.Color = "#39FF14" // Success states
	ColorNeonOrange lipgloss.Color = "#FF6B35" // Warnings (alt)
	ColorNeonAmber  lipgloss.Color = "#FFAA00" // Warnings
)

// Background colors (glassmorphism-inspired)
const (
	ColorDeepVoid    lipgloss.Color = "#0A0A0F" // Main background
	ColorDarkSurface lipgloss.Color = "#12121A" // Card backgrounds
	ColorGlassBorder lipgloss.Color = "#2A2A4A" // Borders (purple tint)
)

// Semantic colors for status indication
const (
	ColorSuccess lipgloss.Color = "#39FF14" // Neon Green
	ColorError   lipgloss.Color = "#FF0055" // Hot Red-Pink
	ColorWarning lipgloss.Color = "#FFAA00" // Electric Amber
	ColorInfo    lipgloss.Color = "#00FFFF" // Neon Cyan
)

// Text colors for content hierarchy
const (
	ColorPrimary   lipgloss.Color = "#FFFFFF" // Pure white
	ColorSecondary lipgloss.Color = "#B4B4D0" // Lavender gray
	ColorMuted     lipgloss.Color = "#6B6B8D" // Purple-gray
)

// Gradient colors for animations and progress bars
var GradientColors = []lipgloss.Color{
	"#FF2E97", // Neon Pink
	"#BF40FF", // Neon Purple
	"#00FFFF", // Neon Cyan
	"#39FF14", // Neon Green
}

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

// SymbolWarning is the warning symbol (⚠)
const SymbolWarning = "⚠"

// PrintWarning prints a styled warning message to stderr.
func PrintWarning(message string) {
	style := lipgloss.NewStyle().Foreground(ColorWarning)
	fmt.Fprintf(os.Stderr, "%s %s\n", style.Render(SymbolWarning), style.Render(message))
}
