package testing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// CommandResponse defines a canned response for a specific command pattern.
type CommandResponse struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Error    error
}

// MockClient simulates an SSH connection for testing.
// It parses common shell commands and executes them against a virtual filesystem.
type MockClient struct {
	mu       sync.Mutex
	host     string
	address  string
	fs       *MockFS
	closed   bool
	commands map[string]CommandResponse // pattern -> response
}

// NewMockClient creates a new mock SSH client with an empty filesystem.
func NewMockClient(host string) *MockClient {
	return &MockClient{
		host:     host,
		address:  host + ":22",
		fs:       NewMockFS(),
		commands: make(map[string]CommandResponse),
	}
}

// Exec runs a command against the virtual filesystem.
// It parses common shell commands (mkdir, cat, rm, test, which, uname)
// and delegates to the filesystem or returns configured responses.
func (m *MockClient) Exec(cmd string) (stdout, stderr []byte, exitCode int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, nil, -1, errors.New("connection closed")
	}

	// Check for exact command matches first
	if resp, ok := m.commands[cmd]; ok {
		return resp.Stdout, resp.Stderr, resp.ExitCode, resp.Error
	}

	// Check for pattern matches
	for pattern, resp := range m.commands {
		if matched, _ := regexp.MatchString(pattern, cmd); matched {
			return resp.Stdout, resp.Stderr, resp.ExitCode, resp.Error
		}
	}

	// Parse and execute common commands
	return m.parseAndExecute(cmd)
}

// ExecStream runs a command and writes output to the provided writers.
func (m *MockClient) ExecStream(cmd string, stdout, stderr io.Writer) (exitCode int, err error) {
	return m.ExecStreamContext(context.Background(), cmd, stdout, stderr)
}

// ExecStreamContext runs a command with context cancellation support.
func (m *MockClient) ExecStreamContext(ctx context.Context, cmd string, stdout, stderr io.Writer) (exitCode int, err error) {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return 130, ctx.Err()
	default:
	}

	out, errOut, code, execErr := m.Exec(cmd)
	if execErr != nil {
		return -1, execErr
	}

	if stdout != nil && len(out) > 0 {
		stdout.Write(out)
	}
	if stderr != nil && len(errOut) > 0 {
		stderr.Write(errOut)
	}

	return code, nil
}

// Close marks the connection as closed.
func (m *MockClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// GetHost returns the host name.
func (m *MockClient) GetHost() string {
	return m.host
}

// GetAddress returns the host:port address.
func (m *MockClient) GetAddress() string {
	return m.address
}

// SetCommandResponse registers a canned response for a command pattern.
// The pattern can be an exact string or a regex pattern.
func (m *MockClient) SetCommandResponse(pattern string, resp CommandResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands[pattern] = resp
}

// GetFS returns the mock filesystem for direct manipulation in tests.
func (m *MockClient) GetFS() *MockFS {
	return m.fs
}

// mockSession is a minimal session that just closes.
type mockSession struct{}

func (s *mockSession) Close() error { return nil }

// NewSession creates a mock session for liveness checks.
// Returns a Session (io.Closer) that does nothing on Close.
func (m *MockClient) NewSession() (sshutil.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("connection closed")
	}
	return &mockSession{}, nil
}

// SendRequest simulates sending a global request on the SSH connection.
// Used for lightweight connection liveness checks.
func (m *MockClient) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return false, nil, errors.New("connection closed")
	}
	// Mock always accepts requests when connection is open
	return true, nil, nil
}

// parseAndExecute handles common shell commands used by rr.
func (m *MockClient) parseAndExecute(cmd string) (stdout, stderr []byte, exitCode int, err error) {
	// Strip common redirects
	cmd = strings.TrimSuffix(cmd, " 2>/dev/null")
	cmd = strings.TrimSuffix(cmd, " 2>&1")
	cmd = strings.TrimSpace(cmd)

	// Handle mkdir
	if strings.HasPrefix(cmd, "mkdir ") {
		return m.handleMkdir(cmd)
	}

	// Handle cat with heredoc (write)
	if strings.HasPrefix(cmd, "cat >") || strings.HasPrefix(cmd, "cat > ") {
		return m.handleCatWrite(cmd)
	}

	// Handle cat (read)
	if strings.HasPrefix(cmd, "cat ") {
		return m.handleCatRead(cmd)
	}

	// Handle rm -rf
	if strings.HasPrefix(cmd, "rm -rf ") {
		return m.handleRm(cmd)
	}

	// Handle test -d (directory exists)
	if strings.HasPrefix(cmd, "test -d ") || strings.HasPrefix(cmd, "[ -d ") {
		return m.handleTestDir(cmd)
	}

	// Handle test -f (file exists)
	if strings.HasPrefix(cmd, "test -f ") || strings.HasPrefix(cmd, "[ -f ") {
		return m.handleTestFile(cmd)
	}

	// Handle which (check if command exists)
	if strings.HasPrefix(cmd, "which ") {
		return m.handleWhich(cmd)
	}

	// Handle uname
	if strings.HasPrefix(cmd, "uname") {
		return m.handleUname(cmd)
	}

	// Unknown command - return success by default
	return nil, nil, 0, nil
}

// handleMkdir processes: mkdir [-p] "path" or mkdir [-p] path
func (m *MockClient) handleMkdir(cmd string) ([]byte, []byte, int, error) {
	args := strings.TrimPrefix(cmd, "mkdir ")
	args = strings.TrimSpace(args)

	// Check for -p flag
	createParents := false
	if strings.HasPrefix(args, "-p ") {
		createParents = true
		args = strings.TrimPrefix(args, "-p ")
		args = strings.TrimSpace(args)
	}

	path := extractPath(args)
	if path == "" {
		return nil, []byte("mkdir: missing operand"), 1, nil
	}

	if createParents {
		// mkdir -p: create all parent directories, don't fail if exists
		err := m.fs.MkdirAll(path)
		if err != nil {
			return nil, []byte("mkdir: cannot create directory: " + err.Error()), 1, nil
		}
		return nil, nil, 0, nil
	}

	// Regular mkdir: check parent exists first (simulates real mkdir behavior)
	parent := filepath.Dir(path)
	if parent != "" && parent != "/" && parent != "." {
		if !m.fs.IsDir(parent) {
			return nil, []byte(fmt.Sprintf("mkdir: cannot create directory '%s': No such file or directory", path)), 1, nil
		}
	}

	err := m.fs.Mkdir(path)
	if err != nil {
		return nil, []byte("mkdir: cannot create directory: " + err.Error()), 1, nil
	}
	return nil, nil, 0, nil
}

// handleCatWrite processes: cat > "path" << 'MARKER'\ncontent\nMARKER
func (m *MockClient) handleCatWrite(cmd string) ([]byte, []byte, int, error) {
	// Extract the path
	// Pattern: cat > "path" << 'MARKER' or cat > path << 'MARKER'
	pathStart := strings.Index(cmd, ">")
	if pathStart == -1 {
		return nil, []byte("cat: missing output file"), 1, nil
	}

	rest := strings.TrimSpace(cmd[pathStart+1:])

	// Find heredoc marker
	heredocIdx := strings.Index(rest, "<<")
	if heredocIdx == -1 {
		// Simple redirect without heredoc - just create empty file
		path := extractPath(rest)
		if path == "" {
			return nil, []byte("cat: missing output file"), 1, nil
		}
		_ = m.fs.WriteFile(path, nil)
		return nil, nil, 0, nil
	}

	path := extractPath(strings.TrimSpace(rest[:heredocIdx]))
	if path == "" {
		return nil, []byte("cat: missing output file"), 1, nil
	}

	// Extract content between heredoc markers
	heredocPart := strings.TrimSpace(rest[heredocIdx+2:])

	// Find the marker (e.g., 'EOF' or 'LOCKINFO')
	marker := ""
	if strings.HasPrefix(heredocPart, "'") {
		endQuote := strings.Index(heredocPart[1:], "'")
		if endQuote != -1 {
			marker = heredocPart[1 : endQuote+1]
			heredocPart = strings.TrimSpace(heredocPart[endQuote+2:])
		}
	} else {
		// Unquoted marker
		parts := strings.Fields(heredocPart)
		if len(parts) > 0 {
			marker = parts[0]
			heredocPart = strings.TrimPrefix(heredocPart, marker)
			heredocPart = strings.TrimSpace(heredocPart)
		}
	}

	// Find content between first newline and marker
	content := heredocPart
	if marker != "" {
		// Remove the trailing marker
		markerIdx := strings.LastIndex(content, marker)
		if markerIdx != -1 {
			content = content[:markerIdx]
		}
	}

	// Trim leading newline but preserve content
	content = strings.TrimPrefix(content, "\n")
	content = strings.TrimSuffix(content, "\n")

	_ = m.fs.WriteFile(path, []byte(content))
	return nil, nil, 0, nil
}

// handleCatRead processes: cat "path" or cat path
func (m *MockClient) handleCatRead(cmd string) ([]byte, []byte, int, error) {
	path := extractPath(strings.TrimPrefix(cmd, "cat "))
	if path == "" {
		return nil, []byte("cat: missing file operand"), 1, nil
	}

	content, err := m.fs.ReadFile(path)
	if err != nil {
		return nil, []byte("cat: " + path + ": No such file or directory"), 1, nil
	}
	return content, nil, 0, nil
}

// handleRm processes: rm -rf "path"
func (m *MockClient) handleRm(cmd string) ([]byte, []byte, int, error) {
	path := extractPath(strings.TrimPrefix(cmd, "rm -rf "))
	if path == "" {
		return nil, []byte("rm: missing operand"), 1, nil
	}

	_ = m.fs.Remove(path)
	return nil, nil, 0, nil
}

// handleTestDir processes: test -d "path" or [ -d "path" ]
func (m *MockClient) handleTestDir(cmd string) ([]byte, []byte, int, error) {
	path := ""
	if strings.HasPrefix(cmd, "test -d ") {
		path = extractPath(strings.TrimPrefix(cmd, "test -d "))
	} else {
		// [ -d "path" ]
		path = extractPath(strings.TrimPrefix(strings.TrimSuffix(cmd, " ]"), "[ -d "))
	}

	if m.fs.IsDir(path) {
		return nil, nil, 0, nil
	}
	return nil, nil, 1, nil
}

// handleTestFile processes: test -f "path" or [ -f "path" ]
func (m *MockClient) handleTestFile(cmd string) ([]byte, []byte, int, error) {
	path := ""
	if strings.HasPrefix(cmd, "test -f ") {
		path = extractPath(strings.TrimPrefix(cmd, "test -f "))
	} else {
		// [ -f "path" ]
		path = extractPath(strings.TrimPrefix(strings.TrimSuffix(cmd, " ]"), "[ -f "))
	}

	if m.fs.IsFile(path) {
		return nil, nil, 0, nil
	}
	return nil, nil, 1, nil
}

// handleWhich processes: which <command>
func (m *MockClient) handleWhich(cmd string) ([]byte, []byte, int, error) {
	cmdName := strings.TrimSpace(strings.TrimPrefix(cmd, "which "))

	// Simulate common commands existing
	knownCommands := map[string]string{
		"rsync": "/usr/bin/rsync",
		"bash":  "/bin/bash",
		"sh":    "/bin/sh",
		"cat":   "/bin/cat",
		"mkdir": "/bin/mkdir",
		"rm":    "/bin/rm",
	}

	if path, ok := knownCommands[cmdName]; ok {
		return []byte(path + "\n"), nil, 0, nil
	}

	return nil, nil, 1, nil
}

// handleUname processes: uname [-s|-r|-a]
func (m *MockClient) handleUname(cmd string) ([]byte, []byte, int, error) {
	if strings.Contains(cmd, "-s") || cmd == "uname" {
		return []byte("Linux\n"), nil, 0, nil
	}
	if strings.Contains(cmd, "-r") {
		return []byte("5.15.0-generic\n"), nil, 0, nil
	}
	if strings.Contains(cmd, "-a") {
		return []byte("Linux mockhost 5.15.0-generic #1 SMP x86_64 GNU/Linux\n"), nil, 0, nil
	}
	return []byte("Linux\n"), nil, 0, nil
}

// extractPath extracts a path from a command argument.
// Handles both quoted and unquoted paths.
func extractPath(arg string) string {
	arg = strings.TrimSpace(arg)

	// Handle quoted paths
	if strings.HasPrefix(arg, "\"") {
		endQuote := strings.Index(arg[1:], "\"")
		if endQuote != -1 {
			return arg[1 : endQuote+1]
		}
	}
	if strings.HasPrefix(arg, "'") {
		endQuote := strings.Index(arg[1:], "'")
		if endQuote != -1 {
			return arg[1 : endQuote+1]
		}
	}

	// Unquoted path - take first space-separated token
	parts := strings.Fields(arg)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
