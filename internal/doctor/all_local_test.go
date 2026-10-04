package doctor

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestAllLocal(t *testing.T) {
	hosts := map[string]config.Host{
		"dev": {Local: true},
		"box": {SSH: []string{"box"}, Dir: "/tmp/rr"},
	}
	tests := []struct {
		name  string
		names []string
		want  bool
	}{
		{"no hosts in scope", nil, false},
		{"only local", []string{"dev"}, true},
		{"only remote", []string{"box"}, false},
		{"mixed", []string{"dev", "box"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, AllLocal(tt.names, hosts))
		})
	}
}
