package parallel

import (
	"context"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskInfo_AllowsHost(t *testing.T) {
	assert.True(t, TaskInfo{}.AllowsHost("any"))
	pinned := TaskInfo{AllowedHosts: []string{"m4-mini"}}
	assert.True(t, pinned.AllowsHost("m4-mini"))
	assert.False(t, pinned.AllowsHost("m1-linux"))
}

func TestOrchestrator_PickWorkerHosts(t *testing.T) {
	hosts := map[string]config.Host{
		"m1-linux": {SSH: []string{"a"}, Dir: "~"},
		"m4-mini":  {SSH: []string{"b"}, Dir: "~"},
	}
	order := []string{"m1-linux", "m4-mini"}

	t.Run("unrestricted tasks take the first n hosts", func(t *testing.T) {
		orch := NewOrchestrator([]TaskInfo{{Name: "a"}}, hosts, order, nil, Config{})
		assert.Equal(t, []string{"m1-linux"}, orch.pickWorkerHosts(1))
		assert.Equal(t, order, orch.pickWorkerHosts(2))
	})

	t.Run("a pinned task adds its host beyond n", func(t *testing.T) {
		tasks := []TaskInfo{{Name: "e2e", AllowedHosts: []string{"m4-mini"}}}
		orch := NewOrchestrator(tasks, hosts, order, nil, Config{})
		assert.Equal(t, []string{"m1-linux", "m4-mini"}, orch.pickWorkerHosts(1))
	})

	t.Run("an already covered pin adds nothing", func(t *testing.T) {
		tasks := []TaskInfo{{Name: "e2e", AllowedHosts: []string{"m1-linux"}}}
		orch := NewOrchestrator(tasks, hosts, order, nil, Config{})
		assert.Equal(t, []string{"m1-linux"}, orch.pickWorkerHosts(1))
	})

	t.Run("a pin with no matching host adds nothing", func(t *testing.T) {
		tasks := []TaskInfo{{Name: "e2e", AllowedHosts: []string{"elsewhere"}}}
		orch := NewOrchestrator(tasks, hosts, order, nil, Config{})
		assert.Equal(t, []string{"m1-linux"}, orch.pickWorkerHosts(1))
	})
}

func TestOrchestrator_HasAvailableHostFor(t *testing.T) {
	hosts := map[string]config.Host{
		"m1-linux": {SSH: []string{"a"}, Dir: "~"},
		"m4-mini":  {SSH: []string{"b"}, Dir: "~"},
	}
	order := []string{"m1-linux", "m4-mini"}
	pinned := TaskInfo{Name: "e2e", AllowedHosts: []string{"m4-mini"}}
	orch := NewOrchestrator([]TaskInfo{pinned}, hosts, order, nil, Config{})
	orch.workerHosts = order

	assert.True(t, orch.hasAvailableHostFor(pinned))
	assert.True(t, orch.hasAvailableHostFor(TaskInfo{Name: "any"}))

	orch.markHostUnavailable("m4-mini")
	assert.False(t, orch.hasAvailableHostFor(pinned), "pinned task's only host is down")
	assert.True(t, orch.hasAvailableHostFor(TaskInfo{Name: "any"}))

	orch.markHostUnavailable("m1-linux")
	assert.False(t, orch.hasAvailableHostFor(TaskInfo{Name: "any"}))
}

func TestOrchestrator_RestrictedTask_NeverRunsOnDisallowedHost(t *testing.T) {
	// Both hosts are unreachable. The pinned task must not be attempted on
	// host-a: it is bounced to host-b, whose connect failure marks it
	// unavailable, and the task then fails with the restriction named in
	// the error rather than a generic host failure.
	tasks := []TaskInfo{
		{Name: "unit", Index: 0, Command: "echo unit"},
		{Name: "e2e", Index: 1, Command: "echo e2e", AllowedHosts: []string{"host-b"}},
	}
	hosts := map[string]config.Host{
		"host-a": {SSH: []string{"nonexistent-host-a-xxxx"}, Dir: "~"},
		"host-b": {SSH: []string{"nonexistent-host-b-xxxx"}, Dir: "~"},
	}
	orch := NewOrchestrator(tasks, hosts, []string{"host-a", "host-b"}, nil, Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := orch.Run(ctx)
	require.NoError(t, err)
	require.NoError(t, ctx.Err(), "run did not finish: possible bounce livelock")
	require.Len(t, result.TaskResults, 2)

	for _, tr := range result.TaskResults {
		assert.False(t, tr.Success())
		if tr.TaskName == "e2e" {
			assert.NotEqual(t, "host-a", tr.Host, "pinned task landed on a disallowed host")
			if tr.Host == "none" {
				require.Error(t, tr.Error)
				assert.Contains(t, tr.Error.Error(), "host-b")
			}
		}
	}
}
