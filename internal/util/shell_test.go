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

func TestShellQuoteJoin(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{"empty", nil, ""},
		{"single", []string{"foo"}, "'foo'"},
		{"multiple", []string{"tests/foo.py", "-k", "a b"}, "'tests/foo.py' '-k' 'a b'"},
		{"embedded quote", []string{"it's"}, `'it'\''s'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShellQuoteJoin(tt.args); got != tt.expected {
				t.Errorf("ShellQuoteJoin(%v) = %q, want %q", tt.args, got, tt.expected)
			}
		})
	}
}

func TestIsCompoundCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		expected bool
	}{
		{"plain command", "pytest tests/", false},
		{"flags only", "go test -v -run TestFoo ./...", false},
		{"pipe", "pytest | grep -v PASS", true},
		{"newline separated", "make build\nmake test", true},
		{"newline inside single quotes", "echo 'a\nb'", false},
		{"and chain", "cd app && pytest", true},
		{"or chain", "pytest || true", true},
		{"semicolon", "cd app; pytest", true},
		{"redirect out", "pytest > out.log", true},
		{"redirect stderr", "pytest --tb=short 2>&1", true},
		{"redirect in", "wc -l < file", true},
		{"background", "server &", true},
		{"command substitution", "echo $(date)", true},
		{"backticks", "echo `date`", true},
		{"subst inside double quotes", `go build -ldflags "-X main.sha=$(git rev-parse HEAD)"`, true},
		{"backtick inside double quotes", "echo \"`date`\"", true},
		{"pipe inside single quotes", "grep 'a|b' file", false},
		{"semicolon inside single quotes", "echo 'a;b'", false},
		{"redirect inside single quotes", "echo '2>&1'", false},
		{"dollar paren inside single quotes", "echo '$(date)'", false},
		{"escaped pipe", `echo \| foo`, false},
		{"plain dollar var", "echo $HOME", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCompoundCommand(tt.cmd); got != tt.expected {
				t.Errorf("IsCompoundCommand(%q) = %v, want %v", tt.cmd, got, tt.expected)
			}
		})
	}
}
