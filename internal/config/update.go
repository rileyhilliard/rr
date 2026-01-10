package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AddSetupCommand adds a setup command to a specific host in the config file.
// It preserves the existing YAML structure and comments.
// If the setup command already exists, it does nothing.
func AddSetupCommand(configPath, hostName, setupCommand string) error {
	// Read the existing file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse as yaml.Node to preserve structure
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Navigate to hosts.<hostName>.setup_commands
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("invalid YAML document structure")
	}

	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping at document root")
	}

	// Find hosts key
	hostsNode := findMapValue(docNode, "hosts")
	if hostsNode == nil {
		return fmt.Errorf("'hosts' key not found in config")
	}

	// Find the specific host
	hostNode := findMapValue(hostsNode, hostName)
	if hostNode == nil {
		return fmt.Errorf("host '%s' not found in config", hostName)
	}

	// Find or create setup_commands
	setupCommandsNode := findMapValue(hostNode, "setup_commands")
	if setupCommandsNode == nil {
		// Need to add setup_commands to this host
		setupCommandsNode = &yaml.Node{
			Kind:    yaml.SequenceNode,
			Tag:     "!!seq",
			Content: []*yaml.Node{},
		}

		// Add the key-value pair to the host mapping
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: "setup_commands",
		}

		hostNode.Content = append(hostNode.Content, keyNode, setupCommandsNode)
	}

	// Check if command already exists
	for _, item := range setupCommandsNode.Content {
		if item.Kind == yaml.ScalarNode && item.Value == setupCommand {
			// Already exists, nothing to do
			return nil
		}
	}

	// Add the new command
	newCmd := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: setupCommand,
	}
	setupCommandsNode.Content = append(setupCommandsNode.Content, newCmd)

	// Write back to file
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	encoder.Close()

	if err := os.WriteFile(configPath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// findMapValue finds a value in a mapping node by key name.
func findMapValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content)-1; i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			return valueNode
		}
	}

	return nil
}

// GeneratePATHExportCommand creates an export command for adding directories to PATH.
func GeneratePATHExportCommand(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	// Convert absolute paths to $HOME-relative
	relativePaths := make([]string, len(paths))
	for i, p := range paths {
		relativePaths[i] = toHomeRelativePath(p)
	}

	return fmt.Sprintf("export PATH=%s:$PATH", strings.Join(relativePaths, ":"))
}

// toHomeRelativePath converts absolute paths starting with common home patterns
// to $HOME-relative paths for portability across users.
func toHomeRelativePath(path string) string {
	// If already using $HOME, return as-is
	if strings.HasPrefix(path, "$HOME") {
		return path
	}

	// Common home directory patterns
	prefixes := []string{
		"/Users/", // macOS
		"/home/",  // Linux
		"/root",   // root user
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
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
