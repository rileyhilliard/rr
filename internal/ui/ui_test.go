package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestSemanticColorsExist(t *testing.T) {
	// Verify semantic colors are defined and are lipgloss colors
	tests := []struct {
		name  string
		color lipgloss.Color
	}{
		{"ColorSuccess", ColorSuccess},
		{"ColorError", ColorError},
		{"ColorWarning", ColorWarning},
		{"ColorInfo", ColorInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, string(tt.color), "%s should not be empty", tt.name)
		})
	}
}

func TestTextColorsExist(t *testing.T) {
	// Verify text colors are defined
	tests := []struct {
		name  string
		color lipgloss.Color
	}{
		{"ColorPrimary", ColorPrimary},
		{"ColorSecondary", ColorSecondary},
		{"ColorMuted", ColorMuted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, string(tt.color), "%s should not be empty", tt.name)
		})
	}
}

func TestColorValues(t *testing.T) {
	// Verify colors match expected ANSI values from proof-of-concept.sh
	// These map to standard terminal colors
	tests := []struct {
		name     string
		color    lipgloss.Color
		expected string
	}{
		// Semantic colors mapped to ANSI
		{"ColorSuccess is green", ColorSuccess, "2"},  // ANSI green (32 -> 2)
		{"ColorError is red", ColorError, "1"},        // ANSI red (31 -> 1)
		{"ColorWarning is yellow", ColorWarning, "3"}, // ANSI yellow (33 -> 3)
		{"ColorInfo is cyan", ColorInfo, "6"},         // ANSI cyan (36 -> 6)

		// Text colors
		{"ColorPrimary is white", ColorPrimary, "7"},    // ANSI white/default
		{"ColorSecondary is blue", ColorSecondary, "4"}, // ANSI blue (34 -> 4)
		{"ColorMuted is gray", ColorMuted, "8"},         // ANSI bright black/gray (90 -> 8)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.color), "%s should be ANSI color %s", tt.name, tt.expected)
		})
	}
}

func TestSymbolsExist(t *testing.T) {
	// Verify symbols are correct Unicode characters
	tests := []struct {
		name     string
		symbol   string
		expected string
	}{
		{"SymbolSuccess", SymbolSuccess, "✓"},
		{"SymbolFail", SymbolFail, "✗"},
		{"SymbolPending", SymbolPending, "○"},
		{"SymbolProgress", SymbolProgress, "◐"},
		{"SymbolComplete", SymbolComplete, "●"},
		{"SymbolSkipped", SymbolSkipped, "⊘"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.symbol, "%s should be %q", tt.name, tt.expected)
		})
	}
}

func TestColorsAreUnique(t *testing.T) {
	// Semantic and text colors can overlap in purpose,
	// but semantic colors should be distinct from each other
	semanticColors := []lipgloss.Color{
		ColorSuccess,
		ColorError,
		ColorWarning,
		ColorInfo,
	}

	seen := make(map[string]bool)
	for _, c := range semanticColors {
		colorStr := string(c)
		assert.False(t, seen[colorStr], "semantic colors should be unique, found duplicate: %s", colorStr)
		seen[colorStr] = true
	}
}

func TestSymbolsAreUnique(t *testing.T) {
	symbols := []string{
		SymbolSuccess,
		SymbolFail,
		SymbolPending,
		SymbolProgress,
		SymbolComplete,
		SymbolSkipped,
	}

	seen := make(map[string]bool)
	for _, s := range symbols {
		assert.False(t, seen[s], "symbols should be unique, found duplicate: %s", s)
		seen[s] = true
	}
}
