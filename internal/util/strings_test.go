package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJoinOrNone(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{
			name:  "nil slice returns (none)",
			items: nil,
			want:  "(none)",
		},
		{
			name:  "empty slice returns (none)",
			items: []string{},
			want:  "(none)",
		},
		{
			name:  "single item returns item",
			items: []string{"foo"},
			want:  "foo",
		},
		{
			name:  "multiple items joined with comma",
			items: []string{"foo", "bar", "baz"},
			want:  "foo, bar, baz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinOrNone(tt.items)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJoinOrDefault(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		def   string
		want  string
	}{
		{
			name:  "empty slice returns default",
			items: []string{},
			def:   "N/A",
			want:  "N/A",
		},
		{
			name:  "empty slice with empty default",
			items: []string{},
			def:   "",
			want:  "",
		},
		{
			name:  "items returned regardless of default",
			items: []string{"a", "b"},
			def:   "default",
			want:  "a, b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinOrDefault(tt.items, tt.def)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		singular string
		plural   string
		want     string
	}{
		{
			name:     "zero returns plural",
			count:    0,
			singular: "item",
			plural:   "items",
			want:     "items",
		},
		{
			name:     "one returns singular",
			count:    1,
			singular: "item",
			plural:   "items",
			want:     "item",
		},
		{
			name:     "two returns plural",
			count:    2,
			singular: "item",
			plural:   "items",
			want:     "items",
		},
		{
			name:     "negative returns plural",
			count:    -1,
			singular: "item",
			plural:   "items",
			want:     "items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Pluralize(tt.count, tt.singular, tt.plural)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"test", "tset", 2},      // transposition (2 edits)
		{"test", "tests", 1},     // insertion
		{"tests", "test", 1},     // deletion
		{"test", "Test", 1},      // case difference
		{"kitten", "sitting", 3}, // classic example
		{"flaw", "lawn", 2},      // substitution + deletion
	}

	for _, tt := range tests {
		t.Run(tt.a+"->"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.expected, LevenshteinDistance(tt.a, tt.b))
		})
	}
}

func TestSuggestSimilar(t *testing.T) {
	candidates := []string{"test", "tests", "build", "deploy", "lint", "verify"}

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "typo suggests correct",
			input:    "tset",
			expected: []string{"test"},
		},
		{
			name:     "extra char suggests both",
			input:    "testt",
			expected: []string{"test", "tests"},
		},
		{
			name:     "missing char",
			input:    "tes",
			expected: []string{"test", "tests"},
		},
		{
			name:     "no close match returns nil",
			input:    "xyz",
			expected: nil,
		},
		{
			name:     "empty input returns nil",
			input:    "",
			expected: nil,
		},
		{
			name:     "case insensitive",
			input:    "TEST",
			expected: []string{"test", "tests"},
		},
		{
			name:     "exact match returns it",
			input:    "build",
			expected: []string{"build"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SuggestSimilar(tt.input, candidates, 3)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSuggestSimilar_EmptyCandidates(t *testing.T) {
	result := SuggestSimilar("test", nil, 3)
	assert.Nil(t, result)

	result = SuggestSimilar("test", []string{}, 3)
	assert.Nil(t, result)
}
