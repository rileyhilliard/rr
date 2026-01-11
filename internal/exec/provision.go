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
	"bun": {
		Name: "bun",
		Installers: map[string]string{
			"darwin": `curl -fsSL https://bun.sh/install | bash`,
			"linux":  `curl -fsSL https://bun.sh/install | bash`,
		},
		PathAdditions: []string{"$HOME/.bun/bin"},
	},
	"uv": {
		Name: "uv",
		Installers: map[string]string{
			"darwin": `curl -LsSf https://astral.sh/uv/install.sh | sh`,
			"linux":  `curl -LsSf https://astral.sh/uv/install.sh | sh`,
		},
		PathAdditions: []string{"$HOME/.local/bin"},
	},
	"uvx": {
		Name: "uvx",
		Installers: map[string]string{
			"darwin": `curl -LsSf https://astral.sh/uv/install.sh | sh`,
			"linux":  `curl -LsSf https://astral.sh/uv/install.sh | sh`,
		},
		PathAdditions: []string{"$HOME/.local/bin"},
	},
	"deno": {
		Name: "deno",
		Installers: map[string]string{
			"darwin": `curl -fsSL https://deno.land/install.sh | sh`,
			"linux":  `curl -fsSL https://deno.land/install.sh | sh`,
		},
		PathAdditions: []string{"$HOME/.deno/bin"},
	},
	"yarn": {
		Name: "yarn",
		Installers: map[string]string{
			"darwin": `if command -v npm &>/dev/null; then npm install -g yarn; elif command -v brew &>/dev/null; then brew install yarn; else echo "npm or Homebrew required" && exit 1; fi`,
			"linux":  `if command -v npm &>/dev/null; then npm install -g yarn; else echo "npm required to install yarn" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"pnpm": {
		Name: "pnpm",
		Installers: map[string]string{
			"darwin": `curl -fsSL https://get.pnpm.io/install.sh | sh -`,
			"linux":  `curl -fsSL https://get.pnpm.io/install.sh | sh -`,
		},
		PathAdditions: []string{"$HOME/.local/share/pnpm"},
	},
	"ruby": {
		Name: "ruby",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install ruby; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y ruby-full; elif command -v yum &>/dev/null; then sudo yum install -y ruby; elif command -v dnf &>/dev/null; then sudo dnf install -y ruby; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"gem": {
		Name: "gem",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install ruby; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y ruby-full; elif command -v yum &>/dev/null; then sudo yum install -y ruby; elif command -v dnf &>/dev/null; then sudo dnf install -y ruby; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"java": {
		Name: "java",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install openjdk; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y default-jdk; elif command -v yum &>/dev/null; then sudo yum install -y java-17-openjdk-devel; elif command -v dnf &>/dev/null; then sudo dnf install -y java-17-openjdk-devel; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"javac": {
		Name: "javac",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install openjdk; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y default-jdk; elif command -v yum &>/dev/null; then sudo yum install -y java-17-openjdk-devel; elif command -v dnf &>/dev/null; then sudo dnf install -y java-17-openjdk-devel; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"docker": {
		Name: "docker",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install --cask docker && echo "Docker Desktop installed. Please open Docker.app to complete setup."; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `curl -fsSL https://get.docker.com | sh && sudo usermod -aG docker $USER && echo "Log out and back in for docker group to take effect"`,
		},
		PathAdditions: []string{},
	},
	"kubectl": {
		Name: "kubectl",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install kubectl; else curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/darwin/arm64/kubectl" && chmod +x kubectl && sudo mv kubectl /usr/local/bin/; fi`,
			"linux":  `curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && chmod +x kubectl && sudo mv kubectl /usr/local/bin/`,
		},
		PathAdditions: []string{},
	},
	"terraform": {
		Name: "terraform",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew tap hashicorp/tap && brew install hashicorp/tap/terraform; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y gnupg software-properties-common && wget -O- https://apt.releases.hashicorp.com/gpg | gpg --dearmor | sudo tee /usr/share/keyrings/hashicorp-archive-keyring.gpg >/dev/null && echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list && sudo apt-get update && sudo apt-get install -y terraform; else echo "apt-get required" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"aws": {
		Name: "aws",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install awscli; else curl "https://awscli.amazonaws.com/AWSCLIV2.pkg" -o "AWSCLIV2.pkg" && sudo installer -pkg AWSCLIV2.pkg -target / && rm AWSCLIV2.pkg; fi`,
			"linux":  `curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && unzip -q awscliv2.zip && sudo ./aws/install && rm -rf aws awscliv2.zip`,
		},
		PathAdditions: []string{},
	},
	"gcloud": {
		Name: "gcloud",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install --cask google-cloud-sdk; else curl https://sdk.cloud.google.com | bash; fi`,
			"linux":  `curl https://sdk.cloud.google.com | bash`,
		},
		PathAdditions: []string{"$HOME/google-cloud-sdk/bin"},
	},
	"jq": {
		Name: "jq",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install jq; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y jq; elif command -v yum &>/dev/null; then sudo yum install -y jq; elif command -v dnf &>/dev/null; then sudo dnf install -y jq; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"curl": {
		Name: "curl",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install curl; else echo "curl should be pre-installed on macOS" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y curl; elif command -v yum &>/dev/null; then sudo yum install -y curl; elif command -v dnf &>/dev/null; then sudo dnf install -y curl; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"wget": {
		Name: "wget",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install wget; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y wget; elif command -v yum &>/dev/null; then sudo yum install -y wget; elif command -v dnf &>/dev/null; then sudo dnf install -y wget; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"htop": {
		Name: "htop",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install htop; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y htop; elif command -v yum &>/dev/null; then sudo yum install -y htop; elif command -v dnf &>/dev/null; then sudo dnf install -y htop; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"tmux": {
		Name: "tmux",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install tmux; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y tmux; elif command -v yum &>/dev/null; then sudo yum install -y tmux; elif command -v dnf &>/dev/null; then sudo dnf install -y tmux; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"vim": {
		Name: "vim",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install vim; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y vim; elif command -v yum &>/dev/null; then sudo yum install -y vim; elif command -v dnf &>/dev/null; then sudo dnf install -y vim; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"nvim": {
		Name: "nvim",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install neovim; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y neovim; elif command -v yum &>/dev/null; then sudo yum install -y neovim; elif command -v dnf &>/dev/null; then sudo dnf install -y neovim; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"neovim": {
		Name: "neovim",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install neovim; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y neovim; elif command -v yum &>/dev/null; then sudo yum install -y neovim; elif command -v dnf &>/dev/null; then sudo dnf install -y neovim; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"ripgrep": {
		Name: "ripgrep",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install ripgrep; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y ripgrep; elif command -v yum &>/dev/null; then sudo yum install -y ripgrep; elif command -v dnf &>/dev/null; then sudo dnf install -y ripgrep; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"rg": {
		Name: "rg",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install ripgrep; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y ripgrep; elif command -v yum &>/dev/null; then sudo yum install -y ripgrep; elif command -v dnf &>/dev/null; then sudo dnf install -y ripgrep; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"fd": {
		Name: "fd",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install fd; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y fd-find; elif command -v yum &>/dev/null; then sudo yum install -y fd-find; elif command -v dnf &>/dev/null; then sudo dnf install -y fd-find; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"fzf": {
		Name: "fzf",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install fzf; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y fzf; elif command -v yum &>/dev/null; then sudo yum install -y fzf; elif command -v dnf &>/dev/null; then sudo dnf install -y fzf; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"tree": {
		Name: "tree",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install tree; else echo "Homebrew not found" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y tree; elif command -v yum &>/dev/null; then sudo yum install -y tree; elif command -v dnf &>/dev/null; then sudo dnf install -y tree; else echo "No supported package manager found" && exit 1; fi`,
		},
		PathAdditions: []string{},
	},
	"rsync": {
		Name: "rsync",
		Installers: map[string]string{
			"darwin": `if command -v brew &>/dev/null; then brew install rsync; else echo "rsync should be pre-installed on macOS" && exit 1; fi`,
			"linux":  `if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y rsync; elif command -v yum &>/dev/null; then sudo yum install -y rsync; elif command -v dnf &>/dev/null; then sudo dnf install -y rsync; else echo "No supported package manager found" && exit 1; fi`,
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
	if strings.Contains(cmd, "bun.sh/install") {
		return "bun.sh (official installer)"
	}
	if strings.Contains(cmd, "astral.sh/uv") {
		return "astral.sh (official installer)"
	}
	if strings.Contains(cmd, "deno.land/install") {
		return "deno.land (official installer)"
	}
	if strings.Contains(cmd, "get.pnpm.io") {
		return "pnpm (official installer)"
	}
	if strings.Contains(cmd, "get.docker.com") {
		return "docker (official installer)"
	}
	if strings.Contains(cmd, "sdk.cloud.google.com") {
		return "gcloud (official installer)"
	}

	// Fallback: truncate if too long
	if len(cmd) > 50 {
		return cmd[:47] + "..."
	}
	return cmd
}
