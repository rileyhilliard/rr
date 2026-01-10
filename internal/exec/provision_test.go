package exec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetToolInstaller(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		wantOk   bool
	}{
		{
			name:     "go is supported",
			toolName: "go",
			wantOk:   true,
		},
		{
			name:     "node is supported",
			toolName: "node",
			wantOk:   true,
		},
		{
			name:     "npm is supported",
			toolName: "npm",
			wantOk:   true,
		},
		{
			name:     "python is supported",
			toolName: "python",
			wantOk:   true,
		},
		{
			name:     "cargo is supported",
			toolName: "cargo",
			wantOk:   true,
		},
		{
			name:     "make is supported",
			toolName: "make",
			wantOk:   true,
		},
		{
			name:     "unknown tool not supported",
			toolName: "unknowntool123",
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installer, ok := GetToolInstaller(tt.toolName)
			assert.Equal(t, tt.wantOk, ok)
			if ok {
				assert.Equal(t, tt.toolName, installer.Name)
			}
		})
	}
}

func TestCanInstallTool(t *testing.T) {
	assert.True(t, CanInstallTool("go"))
	assert.True(t, CanInstallTool("node"))
	assert.True(t, CanInstallTool("python"))
	assert.False(t, CanInstallTool("unknowntool"))
}

func TestToolInstaller_HasDarwinAndLinux(t *testing.T) {
	// Verify all registered tools have both darwin and linux installers
	for name, installer := range toolInstallers {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, installer.Installers["darwin"], "missing darwin installer for %s", name)
			assert.NotEmpty(t, installer.Installers["linux"], "missing linux installer for %s", name)
		})
	}
}

func TestGetInstallCommandDescription(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		osName   string
		wantSub  string // expect description to contain this
	}{
		{
			name:     "go on darwin",
			toolName: "go",
			osName:   "darwin",
			wantSub:  "brew install",
		},
		{
			name:     "cargo on linux",
			toolName: "cargo",
			osName:   "linux",
			wantSub:  "rustup",
		},
		{
			name:     "make on darwin",
			toolName: "make",
			osName:   "darwin",
			wantSub:  "make", // Returns simplified description
		},
		{
			name:     "unknown tool",
			toolName: "unknowntool",
			osName:   "darwin",
			wantSub:  "", // should return empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := GetInstallCommandDescription(tt.toolName, tt.osName)
			if tt.wantSub == "" {
				assert.Empty(t, desc)
			} else {
				assert.Contains(t, desc, tt.wantSub)
			}
		})
	}
}
