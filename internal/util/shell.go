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

// ShellQuoteJoin quotes each argument and joins them with single spaces.
func ShellQuoteJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = ShellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// IsCompoundCommand reports whether cmd contains shell control operators or
// substitutions (pipes, ;, &, redirections, $(), backticks) outside single
// quotes. Appending arguments to a compound command is ambiguous: they would
// bind to the last command in the pipeline, not the intended one.
func IsCompoundCommand(cmd string) bool {
	inSingle := false
	inDouble := false
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '\\':
			i++ // skip escaped character
		case inDouble:
			switch c {
			case '"':
				inDouble = false
			case '`':
				return true // command substitution runs inside double quotes
			case '$':
				if i+1 < len(cmd) && cmd[i+1] == '(' {
					return true
				}
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '|' || c == ';' || c == '&' || c == '<' || c == '>' || c == '`' || c == '\n':
			return true
		case c == '$':
			if i+1 < len(cmd) && cmd[i+1] == '(' {
				return true
			}
		}
	}
	return false
}

// HasPipe reports whether cmd pipes output between commands outside quotes.
// This matters for exit codes: without `set -o pipefail` the shell reports only
// the last stage's status, so a runner that failed upstream still exits 0.
// `||` is not a pipe, so consecutive bars are skipped.
func HasPipe(cmd string) bool {
	inSingle := false
	inDouble := false
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '\\':
			i++ // skip escaped character
		case inDouble:
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '|':
			if i+1 < len(cmd) && cmd[i+1] == '|' {
				i++ // `||` is logical OR, not a pipe
				continue
			}
			return true
		}
	}
	return false
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
