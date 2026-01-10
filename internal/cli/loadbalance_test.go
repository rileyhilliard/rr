package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReorderWithDefault(t *testing.T) {
	tests := []struct {
		name        string
		hostNames   []string
		defaultHost string
		want        []string
	}{
		{
			name:        "default first when in middle",
			hostNames:   []string{"a-host", "b-host", "c-host"},
			defaultHost: "b-host",
			want:        []string{"b-host", "a-host", "c-host"},
		},
		{
			name:        "default first when at end",
			hostNames:   []string{"a-host", "b-host", "c-host"},
			defaultHost: "c-host",
			want:        []string{"c-host", "a-host", "b-host"},
		},
		{
			name:        "already first stays first",
			hostNames:   []string{"a-host", "b-host", "c-host"},
			defaultHost: "a-host",
			want:        []string{"a-host", "b-host", "c-host"},
		},
		{
			name:        "empty default returns unchanged",
			hostNames:   []string{"a-host", "b-host"},
			defaultHost: "",
			want:        []string{"a-host", "b-host"},
		},
		{
			name:        "missing default returns unchanged",
			hostNames:   []string{"a-host", "b-host"},
			defaultHost: "not-found",
			want:        []string{"a-host", "b-host"},
		},
		{
			name:        "single host stays unchanged",
			hostNames:   []string{"only-host"},
			defaultHost: "only-host",
			want:        []string{"only-host"},
		},
		{
			name:        "real scenario: m4-mini before m1-mini",
			hostNames:   []string{"m1-mini", "m4-mini"},
			defaultHost: "m4-mini",
			want:        []string{"m4-mini", "m1-mini"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderWithDefault(tt.hostNames, tt.defaultHost)
			assert.Equal(t, tt.want, got)
		})
	}
}
