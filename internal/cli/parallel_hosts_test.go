package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSubtaskHosts(t *testing.T) {
	pinned := parallel.TaskInfo{Name: "test-e2e", AllowedHosts: []string{"m4-mini"}}
	free := parallel.TaskInfo{Name: "test-unit"}

	t.Run("passes when a pinned host is selected", func(t *testing.T) {
		require.NoError(t, checkSubtaskHosts([]parallel.TaskInfo{pinned, free}, []string{"m1-linux", "m4-mini"}))
	})

	t.Run("unrestricted tasks never fail", func(t *testing.T) {
		require.NoError(t, checkSubtaskHosts([]parallel.TaskInfo{free}, []string{"m1-linux"}))
	})

	t.Run("fails when --host excludes every allowed host", func(t *testing.T) {
		err := checkSubtaskHosts([]parallel.TaskInfo{pinned, free}, []string{"m1-linux"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "test-e2e")
		assert.Contains(t, err.Error(), "m4-mini")
	})
}
