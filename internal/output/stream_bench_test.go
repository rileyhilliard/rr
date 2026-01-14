package output

import (
	"bytes"
	"strings"
	"testing"
)

// BenchmarkStreamHandler_Write measures write performance.
func BenchmarkStreamHandler_Write(b *testing.B) {
	b.Run("small_writes", func(b *testing.B) {
		var buf bytes.Buffer
		handler := NewStreamHandler(&buf, &buf)
		data := []byte("test output line\n")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			handler.Stdout().Write(data)
		}
	})

	b.Run("large_writes", func(b *testing.B) {
		var buf bytes.Buffer
		handler := NewStreamHandler(&buf, &buf)
		data := bytes.Repeat([]byte("x"), 4096)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			handler.Stdout().Write(data)
		}
	})
}

// BenchmarkLineBuffer_ProcessBytes measures line buffering performance.
func BenchmarkLineBuffer_ProcessBytes(b *testing.B) {
	b.Run("single_line", func(b *testing.B) {
		input := []byte("PASS: TestSomething (0.01s)\n")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := &LineBuffer{}
			_ = buf.ProcessBytes(input)
		}
	})

	b.Run("multiple_lines", func(b *testing.B) {
		input := []byte("=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n--- PASS: TestB (0.00s)\n")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := &LineBuffer{}
			_ = buf.ProcessBytes(input)
		}
	})

	b.Run("large_output", func(b *testing.B) {
		// Simulate 100 test lines
		var sb strings.Builder
		for j := 0; j < 100; j++ {
			sb.WriteString("=== RUN   TestFunction")
			sb.WriteString(string(rune('A' + j%26)))
			sb.WriteString("\n--- PASS: TestFunction")
			sb.WriteString(string(rune('A' + j%26)))
			sb.WriteString(" (0.01s)\n")
		}
		input := []byte(sb.String())

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := &LineBuffer{}
			_ = buf.ProcessBytes(input)
		}
	})
}
