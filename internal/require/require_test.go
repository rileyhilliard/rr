package require

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMerge(t *testing.T) {
	tests := []struct {
		name     string
		sources  [][]string
		expected []string
	}{
		{
			name:     "empty sources",
			sources:  [][]string{},
			expected: nil,
		},
		{
			name:     "single source",
			sources:  [][]string{{"go", "node"}},
			expected: []string{"go", "node"},
		},
		{
			name:     "multiple sources no overlap",
			sources:  [][]string{{"go"}, {"node"}, {"cargo"}},
			expected: []string{"go", "node", "cargo"},
		},
		{
			name:     "duplicates removed",
			sources:  [][]string{{"go", "node"}, {"node", "cargo"}, {"go"}},
			expected: []string{"go", "node", "cargo"},
		},
		{
			name:     "empty strings filtered",
			sources:  [][]string{{"go", ""}, {"", "node"}},
			expected: []string{"go", "node"},
		},
		{
			name:     "preserves order of first occurrence",
			sources:  [][]string{{"cargo", "go"}, {"node", "go"}},
			expected: []string{"cargo", "go", "node"},
		},
		{
			name:     "nil sources handled",
			sources:  [][]string{nil, {"go"}, nil},
			expected: []string{"go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.sources...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCache(t *testing.T) {
	t.Run("get and set", func(t *testing.T) {
		cache := NewCache()

		// Initially empty
		_, ok := cache.Get("host1", "go")
		assert.False(t, ok)

		// Set a value
		cache.Set("host1", "go", CheckResult{Name: "go", Satisfied: true})

		// Now it should exist
		result, ok := cache.Get("host1", "go")
		assert.True(t, ok)
		assert.True(t, result.Satisfied)
	})

	t.Run("different hosts are independent", func(t *testing.T) {
		cache := NewCache()

		cache.Set("host1", "go", CheckResult{Name: "go", Satisfied: true})
		cache.Set("host2", "go", CheckResult{Name: "go", Satisfied: false})

		result1, _ := cache.Get("host1", "go")
		result2, _ := cache.Get("host2", "go")

		assert.True(t, result1.Satisfied)
		assert.False(t, result2.Satisfied)
	})

	t.Run("clear host", func(t *testing.T) {
		cache := NewCache()

		cache.Set("host1", "go", CheckResult{Name: "go", Satisfied: true})
		cache.Set("host2", "node", CheckResult{Name: "node", Satisfied: true})

		cache.Clear("host1")

		_, ok1 := cache.Get("host1", "go")
		_, ok2 := cache.Get("host2", "node")

		assert.False(t, ok1, "host1 should be cleared")
		assert.True(t, ok2, "host2 should still exist")
	})

	t.Run("clear all", func(t *testing.T) {
		cache := NewCache()

		cache.Set("host1", "go", CheckResult{Name: "go", Satisfied: true})
		cache.Set("host2", "node", CheckResult{Name: "node", Satisfied: true})

		cache.ClearAll()

		_, ok1 := cache.Get("host1", "go")
		_, ok2 := cache.Get("host2", "node")

		assert.False(t, ok1)
		assert.False(t, ok2)
	})
}

func TestFilterMissing(t *testing.T) {
	results := []CheckResult{
		{Name: "go", Satisfied: true},
		{Name: "node", Satisfied: false},
		{Name: "cargo", Satisfied: true},
		{Name: "python", Satisfied: false},
	}

	missing := FilterMissing(results)

	assert.Len(t, missing, 2)
	assert.Equal(t, "node", missing[0].Name)
	assert.Equal(t, "python", missing[1].Name)
}

func TestFormatMissing(t *testing.T) {
	tests := []struct {
		name     string
		missing  []CheckResult
		expected string
	}{
		{
			name:     "empty",
			missing:  nil,
			expected: "",
		},
		{
			name: "one without installer",
			missing: []CheckResult{
				{Name: "foo", CanInstall: false},
			},
			expected: "foo",
		},
		{
			name: "one with installer",
			missing: []CheckResult{
				{Name: "go", CanInstall: true},
			},
			expected: "go (can install)",
		},
		{
			name: "multiple mixed",
			missing: []CheckResult{
				{Name: "go", CanInstall: true},
				{Name: "custom", CanInstall: false},
			},
			expected: "go (can install), custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatMissing(tt.missing)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGlobalCache(t *testing.T) {
	// GlobalCache should return the same instance
	cache1 := GlobalCache()
	cache2 := GlobalCache()

	assert.Same(t, cache1, cache2)
}

func TestValidateToolName(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		valid bool
	}{
		// Valid tool names
		{name: "simple lowercase", tool: "go", valid: true},
		{name: "with hyphen", tool: "nvidia-smi", valid: true},
		{name: "with underscore", tool: "my_tool", valid: true},
		{name: "with period", tool: "python3.10", valid: true},
		{name: "with numbers", tool: "python3", valid: true},
		{name: "with plus", tool: "g++", valid: true},
		{name: "uppercase", tool: "Node", valid: true},
		{name: "mixed case", tool: "NodeJS", valid: true},

		// Invalid tool names (potential command injection)
		{name: "empty", tool: "", valid: false},
		{name: "with semicolon", tool: "go;rm -rf", valid: false},
		{name: "with backtick", tool: "go`echo hi`", valid: false},
		{name: "with dollar", tool: "go$PATH", valid: false},
		{name: "with pipe", tool: "go|cat", valid: false},
		{name: "with ampersand", tool: "go&&echo", valid: false},
		{name: "with space", tool: "go version", valid: false},
		{name: "with newline", tool: "go\necho", valid: false},
		{name: "starts with hyphen", tool: "-version", valid: false},
		{name: "starts with period", tool: ".hidden", valid: false},
		{name: "with slash", tool: "/usr/bin/go", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateToolName(tt.tool)
			assert.Equal(t, tt.valid, result, "tool name: %q", tt.tool)
		})
	}
}
