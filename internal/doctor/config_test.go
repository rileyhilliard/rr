package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFileCheck(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	t.Run("config not found", func(t *testing.T) {
		check := &ConfigFileCheck{ConfigPath: filepath.Join(tmpDir, "nonexistent.yaml")}
		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail, got %v", result.Status)
		}
	})

	t.Run("config found", func(t *testing.T) {
		// Create a config file (project config - hosts are now in global config)
		cfgPath := filepath.Join(tmpDir, ".rr.yaml")
		content := `version: 1
hosts:
  - test-host
`
		if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		check := &ConfigFileCheck{ConfigPath: cfgPath}
		result := check.Run()

		if result.Status != StatusPass {
			t.Errorf("expected StatusPass, got %v: %s", result.Status, result.Message)
		}
	})

	t.Run("name and category", func(t *testing.T) {
		check := &ConfigFileCheck{}
		if check.Name() != "config_file" {
			t.Errorf("expected name 'config_file', got %s", check.Name())
		}
		if check.Category() != "CONFIG" {
			t.Errorf("expected category 'CONFIG', got %s", check.Category())
		}
	})
}

func TestConfigSchemaCheck(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid schema", func(t *testing.T) {
		cfgPath := filepath.Join(tmpDir, "valid.yaml")
		content := `version: 1
hosts:
  - test-host
`
		if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		check := &ConfigSchemaCheck{ConfigPath: cfgPath}
		result := check.Run()

		if result.Status != StatusPass {
			t.Errorf("expected StatusPass, got %v: %s", result.Status, result.Message)
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		cfgPath := filepath.Join(tmpDir, "invalid.yaml")
		content := `this is not valid yaml: [unclosed`
		if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		check := &ConfigSchemaCheck{ConfigPath: cfgPath}
		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail, got %v", result.Status)
		}
	})

	t.Run("name and category", func(t *testing.T) {
		check := &ConfigSchemaCheck{}
		if check.Name() != "config_schema" {
			t.Errorf("expected name 'config_schema', got %s", check.Name())
		}
		if check.Category() != "CONFIG" {
			t.Errorf("expected category 'CONFIG', got %s", check.Category())
		}
	})
}

func TestConfigHostsCheck(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("hosts configured", func(t *testing.T) {
		// Set up isolated HOME with global config containing hosts
		globalDir := filepath.Join(tmpDir, "withhosts", ".rr")
		if err := os.MkdirAll(globalDir, 0755); err != nil {
			t.Fatal(err)
		}
		globalContent := `version: 1
hosts:
  test:
    ssh: ["test-host"]
    dir: "~/test"
`
		if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", filepath.Join(tmpDir, "withhosts"))

		// Write project config with tasks
		cfgPath := filepath.Join(tmpDir, "withhosts", "hosts.yaml")
		projectContent := `version: 1
tasks:
  build:
    run: "make build"
`
		if err := os.WriteFile(cfgPath, []byte(projectContent), 0644); err != nil {
			t.Fatal(err)
		}

		check := &ConfigHostsCheck{ConfigPath: cfgPath}
		result := check.Run()

		if result.Status != StatusPass {
			t.Errorf("expected StatusPass, got %v: %s", result.Status, result.Message)
		}
		if result.Message == "" {
			t.Error("expected message with host count")
		}
	})

	t.Run("no hosts", func(t *testing.T) {
		// Set up isolated HOME with empty global config (no hosts)
		globalDir := filepath.Join(tmpDir, "nohosts", ".rr")
		if err := os.MkdirAll(globalDir, 0755); err != nil {
			t.Fatal(err)
		}
		globalContent := `version: 1
hosts: {}
`
		if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", filepath.Join(tmpDir, "nohosts"))

		cfgPath := filepath.Join(tmpDir, "nohosts", "nohosts.yaml")
		content := `version: 1
`
		if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		check := &ConfigHostsCheck{ConfigPath: cfgPath}
		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail, got %v", result.Status)
		}
	})
}

func TestConfigReservedNamesCheck(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("no reserved names", func(t *testing.T) {
		cfgPath := filepath.Join(tmpDir, "noreserved.yaml")
		content := `version: 1
hosts:
  - test-host
tasks:
  build:
    run: "make build"
`
		if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		check := &ConfigReservedNamesCheck{ConfigPath: cfgPath}
		result := check.Run()

		if result.Status != StatusPass {
			t.Errorf("expected StatusPass, got %v: %s", result.Status, result.Message)
		}
	})

	t.Run("has reserved name", func(t *testing.T) {
		cfgPath := filepath.Join(tmpDir, "reserved.yaml")
		content := `version: 1
hosts:
  - test-host
tasks:
  run:
    run: "make run"
`
		if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		check := &ConfigReservedNamesCheck{ConfigPath: cfgPath}
		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail, got %v", result.Status)
		}
	})
}

func TestNewConfigChecks(t *testing.T) {
	checks := NewConfigChecks("")

	if len(checks) != 4 {
		t.Errorf("expected 4 config checks, got %d", len(checks))
	}

	// Verify all checks have CONFIG category
	for _, check := range checks {
		if check.Category() != "CONFIG" {
			t.Errorf("expected CONFIG category, got %s", check.Category())
		}
	}
}

// Outside a project, rr still runs against global hosts, so a missing
// .rr.yaml must not fail doctor.
func TestConfigChecks_NoProjectConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Chdir(dir)

	fileResult := (&ConfigFileCheck{}).Run()
	assert.Equal(t, StatusWarn, fileResult.Status, fileResult.Message)
	assert.False(t, fileResult.Fixable, "Fix() is a no-op, so the result must not claim to be fixable")

	schemaResult := (&ConfigSchemaCheck{}).Run()
	assert.Equal(t, StatusPass, schemaResult.Status, schemaResult.Message)
}

func TestConfigHostsCheck_NoHosts(t *testing.T) {
	tests := []struct {
		name    string
		project string
		want    CheckStatus
	}{
		{name: "no local fallback fails", project: "version: 1\n", want: StatusFail},
		{name: "local fallback warns", project: "version: 1\nlocal_fallback: true\n", want: StatusWarn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte("version: 1\nhosts: {}\n"), 0o644))
			t.Setenv("HOME", home)

			cfgPath := filepath.Join(home, ".rr.yaml")
			require.NoError(t, os.WriteFile(cfgPath, []byte(tt.project), 0o644))

			result := (&ConfigHostsCheck{ConfigPath: cfgPath}).Run()
			assert.Equal(t, tt.want, result.Status, result.Message)
		})
	}
}
