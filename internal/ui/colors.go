package ui

import "github.com/charmbracelet/lipgloss"

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
