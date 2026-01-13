// Package util provides common utility functions used across the codebase.
package util

import "strings"

// ShellQuote wraps a string in single quotes, escaping any existing single quotes.
// This is safe for use in shell commands where the string should be treated literally.
func ShellQuote(s string) string {
	// Replace ' with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}

// ShellQuotePreserveTilde quotes a path for shell execution while preserving tilde expansion.
// For paths starting with ~/, the tilde is kept unquoted and the rest is single-quoted.
// For other paths, the entire path is single-quoted.
//
// This is useful for remote command construction where you want the remote shell
// to expand ~ to the user's home directory, but still handle paths with spaces safely.
func ShellQuotePreserveTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		// Keep ~ unquoted, quote the rest
		return "~/" + ShellQuote(path[2:])
	}
	if path == "~" {
		return "~"
	}
	return ShellQuote(path)
}
