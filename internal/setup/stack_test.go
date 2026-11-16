package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectStack(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		languages []string
		require   []string
		tasks     map[string]string // name -> run
	}{
		{
			name:  "empty dir",
			files: nil,
		},
		{
			name:      "go module",
			files:     map[string]string{"go.mod": "module x\n"},
			languages: []string{"go"},
			require:   []string{"go"},
			tasks:     map[string]string{"test": "go test ./...", "build": "go build ./..."},
		},
		{
			name: "bun project with scripts",
			files: map[string]string{
				"package.json": `{"scripts":{"test":"vitest","lint":"eslint ."}}`,
				"bun.lock":     "",
			},
			languages: []string{"node"},
			require:   []string{"node", "bun"},
			tasks:     map[string]string{"test": "bun run test", "lint": "bun run lint"},
		},
		{
			name:      "npm project without scripts",
			files:     map[string]string{"package.json": `{}`},
			languages: []string{"node"},
			require:   []string{"node"},
			tasks:     map[string]string{},
		},
		{
			name: "uv project with pytest",
			files: map[string]string{
				"pyproject.toml": "[dependency-groups]\ndev = [\"pytest\"]\n",
				"uv.lock":        "",
			},
			languages: []string{"python"},
			require:   []string{"uv"},
			tasks:     map[string]string{"test": "uv run pytest"},
		},
		{
			name:      "plain python without pytest",
			files:     map[string]string{"requirements.txt": "requests\n"},
			languages: []string{"python"},
			require:   []string{"python3"},
			tasks:     map[string]string{},
		},
		{
			name: "makefile targets override language defaults",
			files: map[string]string{
				"go.mod":   "module x\n",
				"Makefile": "test: lint\n\tgo test ./...\nlint:\n\tgolangci-lint run\ninstall:\n",
			},
			languages: []string{"go", "make"},
			require:   []string{"go", "make"},
			tasks:     map[string]string{"test": "make test", "build": "go build ./...", "lint": "make lint"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
			}

			s := DetectStack(dir)
			assert.Equal(t, tt.languages, s.Languages)
			assert.Equal(t, tt.require, s.Require)
			assert.Equal(t, len(tt.files) == 0, s.Empty())

			got := map[string]string{}
			for _, task := range s.Tasks {
				got[task.Name] = task.Run
			}
			if tt.tasks == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.tasks, got)
			}
		})
	}
}
