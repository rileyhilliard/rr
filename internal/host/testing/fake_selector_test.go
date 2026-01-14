package testing

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeSelector_Select_Success(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("gpu-box", config.Host{
		SSH: []string{"gpu-local", "gpu-vpn"},
		Dir: "/home/user/project",
	})

	conn, err := selector.Select("")
	require.NoError(t, err)
	assert.Equal(t, "gpu-box", conn.Name)
	assert.Equal(t, "gpu-local", conn.Alias)
	assert.NotNil(t, conn.Client)
	assert.False(t, conn.IsLocal)
}

func TestFakeSelector_Select_PreferredHost(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("host-a", config.Host{SSH: []string{"a"}})
	selector.AddHost("host-b", config.Host{SSH: []string{"b"}})

	conn, err := selector.Select("host-b")
	require.NoError(t, err)
	assert.Equal(t, "host-b", conn.Name)
}

func TestFakeSelector_Select_CachesConnection(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("test-host", config.Host{SSH: []string{"test"}})

	conn1, err := selector.Select("")
	require.NoError(t, err)

	conn2, err := selector.Select("")
	require.NoError(t, err)

	// Same connection should be returned
	assert.Equal(t, conn1, conn2)
	assert.Equal(t, 2, selector.ConnectionAttempts)
}

func TestFakeSelector_Select_FailingHost(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddFailingHost("bad-host", config.Host{SSH: []string{"bad"}}, nil)

	conn, err := selector.Select("")
	assert.Error(t, err)
	assert.Nil(t, conn)
}

func TestFakeSelector_Select_LocalFallback(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddFailingHost("bad-host", config.Host{SSH: []string{"bad"}}, nil)
	selector.SetLocalFallback(true)

	conn, err := selector.Select("")
	require.NoError(t, err)
	assert.True(t, conn.IsLocal)
	assert.Equal(t, "local", conn.Name)
}

func TestFakeSelector_SelectByTag(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("gpu-1", config.Host{
		SSH:  []string{"gpu1"},
		Tags: []string{"gpu", "fast"},
	})
	selector.AddHost("cpu-1", config.Host{
		SSH:  []string{"cpu1"},
		Tags: []string{"cpu"},
	})

	conn, err := selector.SelectByTag("gpu")
	require.NoError(t, err)
	assert.Equal(t, "gpu-1", conn.Name)

	_, err = selector.SelectByTag("nonexistent")
	assert.Error(t, err)
}

func TestFakeSelector_SelectHost(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("host-a", config.Host{SSH: []string{"a"}})
	selector.AddHost("host-b", config.Host{SSH: []string{"b"}})

	// SelectHost doesn't cache, so each call creates new connection
	conn1, err := selector.SelectHost("host-a")
	require.NoError(t, err)
	assert.Equal(t, "host-a", conn1.Name)

	conn2, err := selector.SelectHost("host-b")
	require.NoError(t, err)
	assert.Equal(t, "host-b", conn2.Name)
}

func TestFakeSelector_SelectNextHost(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("host-a", config.Host{SSH: []string{"a"}})
	selector.AddHost("host-b", config.Host{SSH: []string{"b"}})
	selector.AddHost("host-c", config.Host{SSH: []string{"c"}})

	// Skip first host
	conn, err := selector.SelectNextHost([]string{"host-a"})
	require.NoError(t, err)
	assert.NotEqual(t, "host-a", conn.Name)

	// Skip all hosts
	_, err = selector.SelectNextHost([]string{"host-a", "host-b", "host-c"})
	assert.Error(t, err)
}

func TestFakeSelector_EventHandler(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("test-host", config.Host{SSH: []string{"test"}})

	var events []host.ConnectionEvent
	selector.SetEventHandler(func(e host.ConnectionEvent) {
		events = append(events, e)
	})

	_, err := selector.Select("")
	require.NoError(t, err)

	// Should have received connected event
	require.Len(t, events, 1)
	assert.Equal(t, host.EventConnected, events[0].Type)
}

func TestFakeSelector_HostInfo(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("host-a", config.Host{
		SSH:  []string{"a"},
		Dir:  "/home/a",
		Tags: []string{"fast"},
	})

	info := selector.HostInfo()
	require.Len(t, info, 1)
	assert.Equal(t, "host-a", info[0].Name)
	assert.Equal(t, []string{"a"}, info[0].SSH)
	assert.Equal(t, "/home/a", info[0].Dir)
	assert.Equal(t, []string{"fast"}, info[0].Tags)
}

func TestFakeSelector_Reset(t *testing.T) {
	selector := NewFakeSelector()
	selector.AddHost("test-host", config.Host{SSH: []string{"test"}})

	_, err := selector.Select("")
	require.NoError(t, err)
	assert.Equal(t, 1, selector.ConnectionAttempts)

	selector.Reset()
	assert.Equal(t, 0, selector.ConnectionAttempts)
	assert.Nil(t, selector.GetCached())
}
