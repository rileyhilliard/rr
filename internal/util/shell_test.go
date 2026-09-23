package util

import (
	"os/exec"
	"testing"
)

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

func TestShellDoubleQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "bar", `"bar"`},
		{"empty", "", `""`},
		{"variable left to expand", "$HOME/.local/bin:$PATH", `"$HOME/.local/bin:$PATH"`},
		{"double quote", `say "hi"`, `"say \"hi\""`},
		{"backtick", "`whoami`", "\"\\`whoami\\`\""},
		{"backslash", `C:\dir\`, `"C:\\dir\\"`},
		{"backslash before dollar escapes it", `pa\$\$word`, `"pa\$\$word"`},
		{"trailing backslash", `a\`, `"a\\"`},
		{"single quote needs no escape", "it's", `"it's"`},
		{"spaces and semicolon stay inert", "a b; rm -rf ~", `"a b; rm -rf ~"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShellDoubleQuote(tt.input); got != tt.expected {
				t.Errorf("ShellDoubleQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestShellDoubleQuote_ShellEvaluation runs quoted values through a real sh:
// $ expands, \$ is a literal dollar, and everything else arrives as written.
func TestShellDoubleQuote_ShellEvaluation(t *testing.T) {
	t.Setenv("RR_TEST_HOME", "/home/rr-test")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"variable expands", "$RR_TEST_HOME/bin", "/home/rr-test/bin"},
		{"escaped dollars stay literal", `pa\$\$word`, "pa$$word"},
		{"escaped dollar before a name", `\$RR_TEST_HOME`, "$RR_TEST_HOME"},
		{"trailing backslash", `a\`, `a\`},
		{"backslash not before dollar", `C:\dir\n`, `C:\dir\n`},
		{"newline", "line1\nline2", "line1\nline2"},
		{"double quote and backtick", "say \"hi\" `whoami`", "say \"hi\" `whoami`"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := exec.Command("sh", "-c", "printf '%s' "+ShellDoubleQuote(tt.input)).CombinedOutput()
			if err != nil {
				t.Fatalf("sh failed: %v: %s", err, out)
			}
			if got := string(out); got != tt.want {
				t.Errorf("sh expanded ShellDoubleQuote(%q) to %q, want %q", tt.input, got, tt.want)
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

func TestHasPipe(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		expected bool
	}{
		{"plain command", "pytest tests/", false},
		{"simple pipe", "pytest | tail -3", true},
		{"multiple pipes", "pytest | grep FAIL | wc -l", true},
		{"logical or is not a pipe", "pytest || echo failed", false},
		{"logical or then pipe", "pytest || echo failed | tee log", true},
		{"pipe in single quotes", "grep '|' file.txt", false},
		{"pipe in double quotes", `awk -F"|" '{print $1}' f`, false},
		{"escaped pipe", `echo a \| b`, false},
		{"and-chain without pipe", "cd sub && pytest", false},
		{"redirect is not a pipe", "pytest > out.txt", false},
		{"pipe after quoted section", "grep 'a|b' f | wc -l", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPipe(tt.cmd); got != tt.expected {
				t.Errorf("HasPipe(%q) = %v, want %v", tt.cmd, got, tt.expected)
			}
		})
	}
}
