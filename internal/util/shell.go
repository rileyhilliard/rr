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

// ShellDoubleQuote wraps s in double quotes for a POSIX shell so the shell
// still expands $VAR, ${VAR}, and $(...) in it. Use it for values the user
// expects expanded, like PATH=$HOME/.local/bin:$PATH; use ShellQuote for
// anything that must stay literal.
//
// The escaping rule, outside command substitutions: " and ` are escaped, so
// they arrive literally. A backslash directly before $ is kept as the shell's
// escape, so \$ yields a literal dollar ("pa\$\$word" -> pa$$word). Every
// other backslash, trailing ones included, is escaped and arrives literally.
//
// A $(...) is a new command context: the surrounding double quotes don't
// apply inside it, so its text is copied through unchanged and runs as
// written, quotes, backslashes, and backticks included
// ($(echo "a:b" | tr ':' '\n') splits on the colon). An unclosed $( gets
// no such treatment and is escaped like the rest of the value.
func ShellDoubleQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteByte('\\')
			if i+1 < len(s) && s[i+1] == '$' {
				b.WriteByte('$') // \$ stays an escaped dollar, never an opener
				i++
			} else {
				b.WriteByte('\\')
			}
		case '"', '`':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '$':
			if end, _ := MatchExpansion(s, i); end > 0 && s[i+1] == '(' {
				b.WriteString(s[i:end]) // a command substitution runs as written
				i = end - 1
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// MatchExpansion scans the ${...} or $(...) that opens at s[start] and
// returns the index just past its closing brace or paren. When it, or
// something nested in it, is never closed, end is -1 and unclosed is the
// innermost opener left open ("${" or "$("). For any other s[start], end is
// -1 and unclosed is "".
//
// It reads s the way the shell reads a value ShellDoubleQuote produced.
// Inside ${ }, only \$ is an escape and quotes are plain characters. Inside
// $( ), the text is shell code: a backslash escapes the next character,
// parentheses nest, single-quoted text is skipped, and a double-quoted
// string is its own context in which parentheses don't count but ${ and $(
// still open.
func MatchExpansion(s string, start int) (end int, unclosed string) {
	if start+1 >= len(s) || s[start] != '$' || (s[start+1] != '{' && s[start+1] != '(') {
		return -1, ""
	}
	// closers holds what closes each open context: '}' for ${, ')' for $(
	// and each paren nested in it, '"' for a double-quoted string in $( ).
	var closers []byte
	for i := start; i < len(s); i++ {
		c := s[i]
		var top byte
		if len(closers) > 0 {
			top = closers[len(closers)-1]
		}
		inCode := top == ')' || top == '"'
		switch {
		case c == '\\' && (inCode || (i+1 < len(s) && s[i+1] == '$')):
			i++
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			closers = append(closers, '}')
			i++
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			closers = append(closers, ')')
			i++
		case top == ')' && c == '(':
			closers = append(closers, ')')
		case top == ')' && c == '"':
			closers = append(closers, '"')
		case top == ')' && c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				i = len(s) // an unclosed quote swallows everything after it
				break
			}
			i += j + 1
		case c == top:
			closers = closers[:len(closers)-1]
			if len(closers) == 0 {
				return i + 1, ""
			}
		}
	}
	// A '"' only ever sits inside a $( ), so it reports as "$(".
	if closers[len(closers)-1] == '}' {
		return -1, "${"
	}
	return -1, "$("
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
