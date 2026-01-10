package exec

import (
	"fmt"
	"io"
	"strings"
)

// ToolInstaller defines how to install a specific tool on different platforms.
type ToolInstaller struct {
	Name string
	// Installers maps OS name (darwin, linux) to install command.
	// Commands should be idempotent or handle already-installed case.
	Installers map[string]string
	// PathAdditions are directories to add to PATH after installation.
	// These use $HOME for portability.
	PathAdditions []string
}

// SSHStreamer is an interface for executing SSH commands with streaming output.
type SSHStreamer interface {
	Exec(cmd string) (stdout, stderr []byte, exitCode int, err error)
	ExecStream(cmd string, stdout, stderr io.Writer) (exitCode int, err error)
}

// toolInstallers contains installation instructions for common dev tools.
var toolInstallers = map[string]ToolInstaller{
	"go": {
		Name: "go",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install go; else echo "Homebrew not found. Install from https://go.dev/dl/" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y golang-go; elif command -v yum &>/dev/null; then sudo yum install -y golang; elif command -v dnf &>/dev/null; then sudo dnf install -y golang; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{"$HOME/go/bin", "/usr/local/go/bin"},
	},
	"node": {
		Name: "node",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install node; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs; elif command -v yum &>/dev/null; then curl -fsSL https://rpm.nodesource.com/setup_lts.x | sudo bash - && sudo yum install -y nodejs; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"npm": {
		Name: "npm",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install node; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs; elif command -v yum &>/dev/null; then curl -fsSL https://rpm.nodesource.com/setup_lts.x | sudo bash - && sudo yum install -y nodejs; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"python": {
		Name: "python",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install python; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y python3 python3-pip; elif command -v yum &>/dev/null; then sudo yum install -y python3 python3-pip; elif command -v dnf &>/dev/null; then sudo dnf install -y python3 python3-pip; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"python3": {
		Name: "python3",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install python; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y python3 python3-pip; elif command -v yum &>/dev/null; then sudo yum install -y python3 python3-pip; elif command -v dnf &>/dev/null; then sudo dnf install -y python3 python3-pip; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"pip": {
		Name: "pip",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install python; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y python3-pip; elif command -v yum &>/dev/null; then sudo yum install -y python3-pip; elif command -v dnf &>/dev/null; then sudo dnf install -y python3-pip; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"cargo": {
		Name: "cargo",
		Installers: map[string]string{
			"darwin": `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`,
			"linux":  `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`,
		},
		PathAdditions: []string{"$HOME/.cargo/bin"},
	},
	"rust": {
		Name: "rust",
		Installers: map[string]string{
			"darwin": `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`,
			"linux":  `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`,
		},
		PathAdditions: []string{"$HOME/.cargo/bin"},
	},
	"rustc": {
		Name: "rustc",
		Installers: map[string]string{
			"darwin": `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`,
			"linux":  `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`,
		},
		PathAdditions: []string{"$HOME/.cargo/bin"},
	},
	"make": {
		Name: "make",
		Installers: map[string]string{
			"darwin": `xcode-select --install 2>/dev/null || true; if ! command -v make &>/dev/null && command -v brew &>/dev/null; then brew install make; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y build-essential; elif command -v yum &>/dev/null; then sudo yum groupinstall -y "Development Tools"; elif command -v dnf &>/dev/null; then sudo dnf groupinstall -y "Development Tools"; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"git": {
		Name: "git",
		Installers: map[string]string{
			"darwin": `xcode-select --install 2>/dev/null || true; if ! command -v git &>/dev/null && command -v brew &>/dev/null; then brew install git; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y git; elif command -v yum &>/dev/null; then sudo yum install -y git; elif command -v dnf &>/dev/null; then sudo dnf install -y git; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
}

// GetToolInstaller returns the installer for a tool, if one exists.
func GetToolInstaller(toolName string) (ToolInstaller, bool) {
	installer, ok := toolInstallers[toolName]
	return installer, ok
}

// CanInstallTool returns true if we have an installer for the given tool.
func CanInstallTool(toolName string) bool {
	_, ok := toolInstallers[toolName]
	return ok
}

// DetectRemoteOS determines the OS of the remote machine via SSH.
// Returns "darwin", "linux", or "unknown".
func DetectRemoteOS(client SSHExecer) (string, error) {
	stdout, _, exitCode, err := client.Exec("uname -s")
	if err != nil {
		return "unknown", fmt.Errorf("failed to detect OS: %w", err)
	}
	if exitCode != 0 {
		return "unknown", fmt.Errorf("uname command failed with exit code %d", exitCode)
	}

	osName := strings.TrimSpace(strings.ToLower(string(stdout)))
	switch osName {
	case "darwin":
		return "darwin", nil
	case "linux":
		return "linux", nil
	default:
		return "unknown", nil
	}
}

// InstallToolResult contains the result of a tool installation attempt.
type InstallToolResult struct {
	Success       bool
	Output        string
	PathAdditions []string
}

// InstallTool attempts to install a tool on the remote machine.
// Returns the result including any PATH additions needed.
func InstallTool(client SSHStreamer, toolName string, stdout, stderr io.Writer) (*InstallToolResult, error) {
	installer, ok := toolInstallers[toolName]
	if !ok {
		return nil, fmt.Errorf("no installer available for '%s'", toolName)
	}

	// Detect remote OS
	osName, err := DetectRemoteOS(client)
	if err != nil {
		return nil, fmt.Errorf("failed to detect remote OS: %w", err)
	}

	installCmd, ok := installer.Installers[osName]
	if !ok {
		return nil, fmt.Errorf("no installer for '%s' on %s", toolName, osName)
	}

	// Run the install command with streaming output
	exitCode, err := client.ExecStream(installCmd, stdout, stderr)
	if err != nil {
		return &InstallToolResult{
			Success: false,
			Output:  fmt.Sprintf("installation command failed: %v", err),
		}, nil
	}

	if exitCode != 0 {
		return &InstallToolResult{
			Success: false,
			Output:  fmt.Sprintf("installation exited with code %d", exitCode),
		}, nil
	}

	return &InstallToolResult{
		Success:       true,
		PathAdditions: installer.PathAdditions,
	}, nil
}

// GetInstallCommandDescription returns a human-readable description of what
// will be run to install a tool.
func GetInstallCommandDescription(toolName, osName string) string {
	installer, ok := toolInstallers[toolName]
	if !ok {
		return ""
	}

	cmd, ok := installer.Installers[osName]
	if !ok {
		return ""
	}

	// Simplify the command for display
	// Just show the main package manager command
	if strings.Contains(cmd, "brew install") {
		return "brew install " + toolName
	}
	if strings.Contains(cmd, "apt-get install") {
		return "apt-get install " + toolName
	}
	if strings.Contains(cmd, "rustup.rs") {
		return "rustup (official installer)"
	}
	if strings.Contains(cmd, "xcode-select") {
		return "xcode-select --install"
	}
	if strings.Contains(cmd, "nodesource") {
		return "nodesource + apt-get install nodejs"
	}

	// Fallback: truncate if too long
	if len(cmd) > 50 {
		return cmd[:47] + "..."
	}
	return cmd
}
