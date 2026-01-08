package doctor

import (
	"os"
	"path/filepath"
	"testing"
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
		// Create a config file
		cfgPath := filepath.Join(tmpDir, ".rr.yaml")
		content := `version: 1
hosts:
  test:
    ssh: ["test-host"]
    dir: "~/test"
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
  test:
    ssh: ["test-host"]
    dir: "~/test"
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
		cfgPath := filepath.Join(tmpDir, "hosts.yaml")
		content := `version: 1
hosts:
  test:
    ssh: ["test-host"]
    dir: "~/test"
tasks:
  build:
    run: "make build"
`
		if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
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
		cfgPath := filepath.Join(tmpDir, "nohosts.yaml")
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
  test:
    ssh: ["test-host"]
    dir: "~/test"
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
  test:
    ssh: ["test-host"]
    dir: "~/test"
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
