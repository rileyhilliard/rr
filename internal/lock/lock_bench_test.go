package lock

import (
	"encoding/json"
	"testing"
)

// BenchmarkNewLockInfo measures lock info creation.
func BenchmarkNewLockInfo(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = NewLockInfo("")
	}
}

// BenchmarkLockInfo_JSON measures JSON serialization of lock info.
func BenchmarkLockInfo_JSON(b *testing.B) {
	info, err := NewLockInfo("")
	if err != nil {
		b.Fatalf("setup failed: NewLockInfo returned error: %v", err)
	}

	b.Run("marshal", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(info)
		}
	})

	b.Run("unmarshal", func(b *testing.B) {
		data, _ := json.Marshal(info)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var parsed LockInfo
			_ = json.Unmarshal(data, &parsed)
		}
	})
}

// BenchmarkLockPath measures lock path construction.
func BenchmarkLockPath(b *testing.B) {
	// Simulate lock path construction
	baseDir := "/tmp"

	b.Run("default_dir", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = baseDir + "/rr.lock"
		}
	})
}
