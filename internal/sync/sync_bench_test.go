package sync

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
)

// BenchmarkBuildArgs measures rsync argument construction.
func BenchmarkBuildArgs(b *testing.B) {
	conn := &host.Connection{
		Name:  "test-host",
		Alias: "test-ssh",
		Host: config.Host{
			Dir: "~/projects/myproject",
		},
	}

	b.Run("minimal_config", func(b *testing.B) {
		cfg := config.SyncConfig{}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = BuildArgs(conn, "/local/path", cfg)
		}
	})

	b.Run("with_excludes", func(b *testing.B) {
		cfg := config.SyncConfig{
			Exclude: []string{".git/", "node_modules/", "*.log", "dist/", "coverage/"},
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = BuildArgs(conn, "/local/path", cfg)
		}
	})

	b.Run("with_preserves", func(b *testing.B) {
		cfg := config.SyncConfig{
			Preserve: []string{".cache/", "vendor/", ".venv/"},
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = BuildArgs(conn, "/local/path", cfg)
		}
	})

	b.Run("full_config", func(b *testing.B) {
		cfg := config.SyncConfig{
			Exclude:  []string{".git/", "node_modules/", "*.log", "dist/", "coverage/", ".DS_Store", "*.swp"},
			Preserve: []string{".cache/", "vendor/", ".venv/", ".go/"},
			Flags:    []string{"--compress-level=9"},
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = BuildArgs(conn, "/local/path", cfg)
		}
	})
}

// BenchmarkScanLinesWithCR measures the custom line scanner performance.
func BenchmarkScanLinesWithCR(b *testing.B) {
	b.Run("short_line", func(b *testing.B) {
		data := []byte("sending incremental file list\n")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, _ = scanLinesWithCR(data, false)
		}
	})

	b.Run("progress_line_with_cr", func(b *testing.B) {
		data := []byte("  1,234,567 100%   10.5MB/s    0:00:00\r")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, _ = scanLinesWithCR(data, false)
		}
	})

	b.Run("long_path", func(b *testing.B) {
		data := []byte("src/very/deep/nested/directory/structure/with/many/levels/file.go\n")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, _ = scanLinesWithCR(data, false)
		}
	})
}
