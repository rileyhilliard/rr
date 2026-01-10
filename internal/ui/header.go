package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Road runner ASCII art - Looney Tunes style with tall crest and running pose
var roadRunnerArt = []string{
	"       //",
	"      //",
	"     //",
	"    (o>",
	" __/   \\___",
	"<__   /|   >",
	"     /_\\",
}

// HeaderInfo contains information to display in the header.
type HeaderInfo struct {
	Version string // Version string (e.g., "v0.4.0")
	Tagline string // Optional tagline (e.g., "Remote code runner")
	WorkDir string // Optional working directory to display
}

// RenderHeader renders a styled header with road runner art and version info.
// Similar to Claude Code's header style but with road runner branding.
func RenderHeader(info HeaderInfo) string {
	// Define colors
	artColor := lipgloss.Color("208")  // Orange (similar to Claude Code's coral)
	titleColor := lipgloss.Color("34") // Green for "rr"
	mutedColor := ColorMuted

	artStyle := lipgloss.NewStyle().Foreground(artColor)
	titleStyle := lipgloss.NewStyle().Foreground(titleColor).Bold(true)
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")) // White
	mutedStyle := lipgloss.NewStyle().Foreground(mutedColor)

	// Build the right side content
	var rightLines []string

	// Title line: "rr v0.4.0"
	titleLine := fmt.Sprintf("%s %s", titleStyle.Render("rr"), versionStyle.Render(info.Version))
	rightLines = append(rightLines, titleLine)

	// Tagline (if provided)
	if info.Tagline != "" {
		rightLines = append(rightLines, mutedStyle.Render(info.Tagline))
	}

	// Working directory (if provided)
	if info.WorkDir != "" {
		rightLines = append(rightLines, mutedStyle.Render(info.WorkDir))
	}

	// Calculate dimensions
	artWidth := maxLineWidth(roadRunnerArt)
	gap := 3 // Space between art and text

	// Build output by combining art and right content
	var output strings.Builder

	// Calculate vertical centering for right content
	artHeight := len(roadRunnerArt)
	rightHeight := len(rightLines)
	rightStartLine := (artHeight - rightHeight) / 2
	if rightStartLine < 0 {
		rightStartLine = 0
	}

	for i := 0; i < artHeight; i++ {
		// Render art line
		artLine := ""
		if i < len(roadRunnerArt) {
			artLine = roadRunnerArt[i]
		}
		paddedArt := padRight(artLine, artWidth)
		output.WriteString(artStyle.Render(paddedArt))

		// Add gap
		output.WriteString(strings.Repeat(" ", gap))

		// Render right content if we're in the right range
		rightIndex := i - rightStartLine
		if rightIndex >= 0 && rightIndex < len(rightLines) {
			output.WriteString(rightLines[rightIndex])
		}

		output.WriteString("\n")
	}

	return output.String()
}

// PrintHeader prints the styled header to stdout.
func PrintHeader(info HeaderInfo) {
	fmt.Print(RenderHeader(info))
}

// maxLineWidth returns the maximum width among the given lines.
func maxLineWidth(lines []string) int {
	maxWidth := 0
	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}
	return maxWidth
}
