package util

import "testing"

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\\''quote'"},
		{"", "''"},
		{"path/to/file", "'path/to/file'"},
		{"$variable", "'$variable'"},
		{"$(command)", "'$(command)'"},
		{"`backtick`", "'`backtick`'"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellQuote(tt.input)
			if got != tt.expected {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestShellQuotePreserveTilde(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"~", "~"},
		{"~/path", "~/'path'"},
		{"~/path/to/dir", "~/'path/to/dir'"},
		{"~/path with spaces", "~/'path with spaces'"},
		{"~/path'quote", "~/'path'\\''quote'"},
		{"/absolute/path", "'/absolute/path'"},
		{"relative/path", "'relative/path'"},
		{"~user/path", "'~user/path'"}, // Not current user's home, quote it
		{"", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellQuotePreserveTilde(tt.input)
			if got != tt.expected {
				t.Errorf("ShellQuotePreserveTilde(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
