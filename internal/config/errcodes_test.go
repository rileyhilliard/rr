package config

import (
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorCodes_ClassifiedAtSource checks that config and host lookups
// classify their errors with the specific internal code, so the CLI maps
// them to CONFIG_NOT_FOUND / HOST_NOT_FOUND without reading message text.
func TestErrorCodes_ClassifiedAtSource(t *testing.T) {
	globalWithDev := &GlobalConfig{
		Version: 1,
		Hosts:   map[string]Host{"dev": {SSH: []string{"dev"}, Dir: "/home/dev"}},
	}
	missingPath := filepath.Join(t.TempDir(), "nope.yaml")

	tests := []struct {
		name     string
		run      func() error
		wantCode string
	}{
		{
			name: "Find with a missing explicit path",
			run: func() error {
				_, err := Find(missingPath)
				return err
			},
			wantCode: errors.ErrConfigNotFound,
		},
		{
			name: "Load of a missing file",
			run: func() error {
				_, err := Load(missingPath)
				return err
			},
			wantCode: errors.ErrConfigNotFound,
		},
		{
			name: "ResolveHosts with an unknown --host",
			run: func() error {
				_, _, err := ResolveHosts(&ResolvedConfig{Global: globalWithDev, Project: &Config{}}, "typo")
				return err
			},
			wantCode: errors.ErrHostNotFound,
		},
		{
			name: "ValidateResolved with an unknown project host",
			run: func() error {
				return ValidateResolved(&ResolvedConfig{Global: globalWithDev, Project: &Config{Version: 1, Host: "prod"}})
			},
			wantCode: errors.ErrHostNotFound,
		},
		{
			name: "ValidateResolved with an unknown entry in project hosts",
			run: func() error {
				return ValidateResolved(&ResolvedConfig{Global: globalWithDev, Project: &Config{Version: 1, Hosts: []string{"dev", "prod"}}})
			},
			wantCode: errors.ErrHostNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			require.Error(t, err)
			assert.True(t, errors.IsCode(err, tt.wantCode), "want code %s, got: %v", tt.wantCode, err)
		})
	}
}
