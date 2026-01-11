package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HeaderInfo contains information to display in the header.
type HeaderInfo struct {
	Version string // Version string (e.g., "v0.4.0")
	Tagline string // Optional tagline (e.g., "Remote code runner")
	WorkDir string // Optional working directory to display
}

// HeaderWidth is the default width of the header divider
const HeaderWidth = 50

// RenderHeader renders a clean, branded header in Claude Code style.
// No ASCII art - just clean typography with neon accents.
func RenderHeader(info HeaderInfo) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(ColorNeonPink).
		Bold(true)

	versionStyle := lipgloss.NewStyle().
		Foreground(ColorNeonCyan)

	taglineStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary)

	dividerStyle := lipgloss.NewStyle().
		Foreground(ColorGlassBorder)

	var output strings.Builder

	// Title line: "rr v0.5.0"
	output.WriteString(titleStyle.Render("rr"))
	output.WriteString(" ")
	output.WriteString(versionStyle.Render(info.Version))
	output.WriteString("\n")

	// Tagline (if provided)
	if info.Tagline != "" {
		output.WriteString(taglineStyle.Render(info.Tagline))
		output.WriteString("\n")
	}

	// Working directory (if provided)
	if info.WorkDir != "" {
		workDirStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		output.WriteString(workDirStyle.Render(info.WorkDir))
		output.WriteString("\n")
	}

	// Divider line
	output.WriteString(dividerStyle.Render(strings.Repeat("━", HeaderWidth)))
	output.WriteString("\n")

	return output.String()
}

// PrintHeader prints the styled header to stdout.
func PrintHeader(info HeaderInfo) {
	fmt.Print(RenderHeader(info))
}
