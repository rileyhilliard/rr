package exec

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SSHExecer is an interface for executing SSH commands.
// This allows for easier testing and decoupling from the sshutil package.
type SSHExecer interface {
	Exec(cmd string) (stdout, stderr []byte, exitCode int, err error)
}

// PathProbeResult holds results from probing for a command on the remote.
type PathProbeResult struct {
	Command      string   // Command we searched for
	FoundInLogin bool     // Found via login shell
	FoundInInter bool     // Found via interactive shell
	LoginPath    string   // Full path if found in login shell
	InterPath    string   // Full path if found in interactive shell
	CommonPaths  []string // Paths where command was found in common locations
}

// PathDifference holds the diff between login and interactive shell PATH.
type PathDifference struct {
	LoginOnly []string // Dirs only in login shell
	InterOnly []string // Dirs only in interactive shell (usually the problem)
	Common    []string // Dirs in both
}

// commonBinPaths are typical locations where tools get installed.
// These use $HOME for expansion on the remote.
var commonBinPaths = []string{
	"$HOME/.local/bin",
	"$HOME/.cargo/bin",
	"/opt/homebrew/bin",
	"/opt/homebrew/sbin",
	"$HOME/go/bin",
	"$HOME/.pyenv/shims",
	"/usr/local/bin",
	"/usr/local/go/bin",
	"$HOME/.nvm/current/bin",
	"$HOME/.volta/bin",
	"$HOME/.deno/bin",
}

// ProbeCommandPath searches for a command on the remote in login shell,
// interactive shell, and common binary locations.
func ProbeCommandPath(client SSHExecer, cmd string) (*PathProbeResult, error) {
	if client == nil {
		return nil, fmt.Errorf("no SSH client provided")
	}

	result := &PathProbeResult{Command: cmd}

	// Check in login shell (what rr uses by default)
	// Use command -v as it's more portable than which
	loginCheck := fmt.Sprintf(`$SHELL -l -c "command -v %s 2>/dev/null"`, cmd)
	stdout, _, exitCode, _ := client.Exec(loginCheck)
	if exitCode == 0 && len(stdout) > 0 {
		result.FoundInLogin = true
		result.LoginPath = strings.TrimSpace(string(stdout))
	}

	// Check in interactive shell
	// Redirect stderr to /dev/null to suppress shell startup messages
	interCheck := fmt.Sprintf(`$SHELL -i -c "command -v %s 2>/dev/null" 2>/dev/null`, cmd)
	stdout, _, exitCode, _ = client.Exec(interCheck)
	if exitCode == 0 && len(stdout) > 0 {
		result.FoundInInter = true
		result.InterPath = strings.TrimSpace(string(stdout))
	}

	// Search common locations if not found in either shell
	if !result.FoundInLogin && !result.FoundInInter {
		for _, binPath := range commonBinPaths {
			checkCmd := fmt.Sprintf(`test -x %s/%s && echo %s/%s`, binPath, cmd, binPath, cmd)
			stdout, _, exitCode, _ := client.Exec(checkCmd)
			if exitCode == 0 && len(stdout) > 0 {
				foundPath := strings.TrimSpace(string(stdout))
				result.CommonPaths = append(result.CommonPaths, foundPath)
			}
		}
	}

	return result, nil
}

// GetPATHDifference compares PATH between login and interactive shells.
func GetPATHDifference(client SSHExecer) (*PathDifference, error) {
	if client == nil {
		return nil, fmt.Errorf("no SSH client provided")
	}

	// Get login shell PATH
	loginPATH, err := getShellPATH(client, true)
	if err != nil {
		return nil, fmt.Errorf("get login PATH: %w", err)
	}

	// Get interactive shell PATH
	interPATH, err := getShellPATH(client, false)
	if err != nil {
		return nil, fmt.Errorf("get interactive PATH: %w", err)
	}

	return ComparePATHs(loginPATH, interPATH), nil
}

// getShellPATH retrieves PATH from either login or interactive shell.
func getShellPATH(client SSHExecer, login bool) ([]string, error) {
	var cmd string
	if login {
		cmd = `$SHELL -l -c 'echo $PATH'`
	} else {
		// Redirect stderr to suppress interactive shell startup messages
		cmd = `$SHELL -i -c 'echo $PATH' 2>/dev/null`
	}

	stdout, _, exitCode, err := client.Exec(cmd)
	if err != nil {
		return nil, fmt.Errorf("exec failed: %w", err)
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("shell returned exit code %d", exitCode)
	}

	pathStr := strings.TrimSpace(string(stdout))
	if pathStr == "" {
		return []string{}, nil
	}

	return strings.Split(pathStr, ":"), nil
}

// ComparePATHs returns the difference between two PATH arrays.
func ComparePATHs(login, inter []string) *PathDifference {
	loginSet := make(map[string]bool)
	for _, p := range login {
		if p != "" {
			loginSet[p] = true
		}
	}

	interSet := make(map[string]bool)
	for _, p := range inter {
		if p != "" {
			interSet[p] = true
		}
	}

	diff := &PathDifference{}

	// Find dirs only in interactive (the common problem case)
	for _, p := range inter {
		if p == "" {
			continue
		}
		if !loginSet[p] {
			diff.InterOnly = append(diff.InterOnly, p)
		} else if !contains(diff.Common, p) {
			// Only add to common once
			diff.Common = append(diff.Common, p)
		}
	}

	// Find dirs only in login
	for _, p := range login {
		if p == "" {
			continue
		}
		if !interSet[p] {
			diff.LoginOnly = append(diff.LoginOnly, p)
		}
	}

	return diff
}

// GenerateSetupSuggestion creates a setup_commands suggestion based on probe results.
func GenerateSetupSuggestion(result *PathProbeResult, hostName string) string {
	if result == nil {
		return ""
	}

	var sb strings.Builder

	// Case 1: Found in interactive but not login shell
	if result.FoundInInter && !result.FoundInLogin && result.InterPath != "" {
		dir := filepath.Dir(result.InterPath)
		// Convert absolute paths to $HOME-relative for portability
		homeRelative := toHomeRelative(dir)

		sb.WriteString(fmt.Sprintf("'%s' is available in interactive shells but not when rr runs commands.\n\n", result.Command))
		sb.WriteString("This usually means your shell profile (~/.zshrc, ~/.bashrc) adds to PATH,\n")
		sb.WriteString("but your login profile (~/.zprofile, ~/.bash_profile) doesn't.\n\n")
		sb.WriteString("Fix by adding to your .rr.yaml:\n\n")
		sb.WriteString("  hosts:\n")
		sb.WriteString(fmt.Sprintf("    %s:\n", hostName))
		sb.WriteString("      setup_commands:\n")
		sb.WriteString(fmt.Sprintf("        - export PATH=%s:$PATH\n", homeRelative))
		return sb.String()
	}

	// Case 2: Found in common location but not in either shell
	if len(result.CommonPaths) > 0 {
		dir := filepath.Dir(result.CommonPaths[0])
		homeRelative := toHomeRelative(dir)

		sb.WriteString(fmt.Sprintf("Found '%s' at %s but it's not in PATH.\n\n", result.Command, result.CommonPaths[0]))
		sb.WriteString("Add to your .rr.yaml:\n\n")
		sb.WriteString("  hosts:\n")
		sb.WriteString(fmt.Sprintf("    %s:\n", hostName))
		sb.WriteString("      setup_commands:\n")
		sb.WriteString(fmt.Sprintf("        - export PATH=%s:$PATH\n", homeRelative))
		return sb.String()
	}

	// Case 3: Not found anywhere - generic message
	sb.WriteString(fmt.Sprintf("'%s' wasn't found on the remote machine.\n\n", result.Command))
	sb.WriteString("This can happen if:\n")
	sb.WriteString("  - The tool isn't installed on the remote\n")
	sb.WriteString("  - The tool is installed in an unusual location\n\n")
	sb.WriteString("Fixes:\n\n")
	sb.WriteString(fmt.Sprintf("  1. Install '%s' on the remote machine\n\n", result.Command))
	sb.WriteString("  2. If installed, find where and add to setup_commands:\n")
	sb.WriteString(fmt.Sprintf("     ssh %s \"find ~/ -name %s -type f 2>/dev/null\"\n\n", hostName, result.Command))
	sb.WriteString("  3. Add explicit PATH via setup_commands:\n")
	sb.WriteString("     hosts:\n")
	sb.WriteString(fmt.Sprintf("       %s:\n", hostName))
	sb.WriteString("         setup_commands:\n")
	sb.WriteString("           - export PATH=$HOME/.local/bin:$PATH\n")

	return sb.String()
}

// toHomeRelative converts absolute paths starting with common home patterns
// to $HOME-relative paths for portability across users.
func toHomeRelative(path string) string {
	// Common home directory patterns
	prefixes := []string{
		"/Users/", // macOS
		"/home/",  // Linux
		"/root",   // root user
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			// Find the end of the username part
			rest := path[len(prefix):]
			if prefix == "/root" {
				return "$HOME" + rest
			}
			// Skip past username to get the rest of the path
			if idx := strings.Index(rest, "/"); idx != -1 {
				return "$HOME" + rest[idx:]
			}
			return "$HOME"
		}
	}

	return path
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
