package doctor

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
)

func TestHostConnectivityCheck(t *testing.T) {
	t.Run("name and category", func(t *testing.T) {
		check := &HostConnectivityCheck{HostName: "test-host"}

		if check.Name() != "host_test-host" {
			t.Errorf("expected name 'host_test-host', got %s", check.Name())
		}
		if check.Category() != "HOSTS" {
			t.Errorf("expected category 'HOSTS', got %s", check.Category())
		}
	})

	t.Run("no SSH aliases", func(t *testing.T) {
		check := &HostConnectivityCheck{
			HostName:   "empty-host",
			HostConfig: config.Host{SSH: []string{}},
		}

		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail for empty SSH aliases, got %v", result.Status)
		}
	})

	t.Run("fix returns nil", func(t *testing.T) {
		check := &HostConnectivityCheck{}
		if err := check.Fix(); err != nil {
			t.Errorf("expected Fix() to return nil, got %v", err)
		}
	})
}

func TestNewHostsChecks(t *testing.T) {
	hosts := map[string]config.Host{
		"host1": {
			SSH: []string{"host1-local", "host1"},
			Dir: "~/projects",
		},
		"host2": {
			SSH: []string{"host2"},
			Dir: "~/work",
		},
	}

	checks := NewHostsChecks(hosts)

	if len(checks) != 2 {
		t.Errorf("expected 2 host checks, got %d", len(checks))
	}

	// Verify all checks have HOSTS category
	for _, check := range checks {
		if check.Category() != "HOSTS" {
			t.Errorf("expected HOSTS category, got %s", check.Category())
		}
	}
}

func TestGetHostCheckDetails(t *testing.T) {
	checks := []Check{
		&HostConnectivityCheck{
			HostName: "test-host",
			HostConfig: config.Host{
				SSH: []string{"alias1"},
				Dir: "~/test",
			},
		},
		&mockCheck{name: "other", category: "OTHER"}, // Non-host check
	}

	details := GetHostCheckDetails(checks)

	if len(details) != 1 {
		t.Errorf("expected 1 host detail, got %d", len(details))
	}

	if details[0].HostName != "test-host" {
		t.Errorf("expected host name 'test-host', got %s", details[0].HostName)
	}
}
