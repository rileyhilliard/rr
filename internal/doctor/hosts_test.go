package doctor

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestScopeHosts(t *testing.T) {
	global := &config.GlobalConfig{Hosts: map[string]config.Host{
		"alpha":   {SSH: []string{"alpha"}},
		"beta":    {SSH: []string{"beta"}},
		"offline": {SSH: []string{"offline"}},
	}}
	localMode := config.LocalFallbackAlways

	tests := []struct {
		name      string
		project   *config.Config
		global    *config.GlobalConfig
		wantNames []string
		wantErr   bool
	}{
		{name: "outside a project checks every global host", global: global, wantNames: []string{"alpha", "beta", "offline"}},
		{name: "project hosts list scopes the check", project: &config.Config{Hosts: []string{"beta", "alpha"}}, global: global, wantNames: []string{"beta", "alpha"}},
		{name: "project single host", project: &config.Config{Host: "alpha"}, global: global, wantNames: []string{"alpha"}},
		{name: "project without hosts uses all global hosts", project: &config.Config{}, global: global, wantNames: []string{"alpha", "beta", "offline"}},
		{name: "local mode checks no hosts", project: &config.Config{LocalFallback: &localMode}, global: global, wantNames: []string{}},
		{name: "unknown project host errors", project: &config.Config{Hosts: []string{"nope"}}, global: global, wantErr: true},
		{name: "no global hosts", project: &config.Config{}, global: &config.GlobalConfig{}, wantNames: nil},
		{name: "no global config", wantNames: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names, hosts, err := ScopeHosts(tt.project, tt.global)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNames, names)
			for _, n := range names {
				assert.Contains(t, hosts, n)
			}
		})
	}
}

func TestHostResolutionCheck(t *testing.T) {
	err := errors.New(errors.ErrConfig, "Host 'nope' not found in global config", "Available hosts: alpha.")
	result := (&HostResolutionCheck{Err: err}).Run()

	assert.Equal(t, StatusFail, result.Status)
	assert.Equal(t, "Host 'nope' not found in global config", result.Message)
	assert.Equal(t, "Available hosts: alpha.", result.Suggestion)
	assert.False(t, result.Fixable)
}

func TestHostConnectivityCheck_InjectedProbe(t *testing.T) {
	check := &HostConnectivityCheck{
		HostName:   "alpha",
		HostConfig: config.Host{SSH: []string{"alpha-lan", "alpha-vpn"}},
		Probe: func(aliases []string, _ time.Duration) []host.ProbeResult {
			return []host.ProbeResult{
				{SSHAlias: aliases[0], Success: false},
				{SSHAlias: aliases[1], Success: true},
			}
		},
	}

	result := check.Run()
	assert.Equal(t, StatusWarn, result.Status)
	assert.Len(t, check.Results, 2)
}

// hostCheckWith builds a host check whose probe reports every alias as
// reachable (up) or not.
func hostCheckWith(name string, up bool) *HostConnectivityCheck {
	return &HostConnectivityCheck{
		HostName:   name,
		HostConfig: config.Host{SSH: []string{name + "-lan"}},
		Probe: func(aliases []string, _ time.Duration) []host.ProbeResult {
			return []host.ProbeResult{{SSHAlias: aliases[0], Success: up}}
		},
	}
}

func TestGradeHostResults(t *testing.T) {
	tests := []struct {
		name        string
		up          map[string]bool
		fallback    config.LocalFallbackMode
		want        map[string]CheckStatus
		wantSuggest map[string]string
	}{
		{
			name:        "one of two down is a warning: runs use the other host",
			up:          map[string]bool{"a": true, "b": false},
			fallback:    config.LocalFallbackNever,
			want:        map[string]CheckStatus{"a": StatusPass, "b": StatusWarn},
			wantSuggest: map[string]string{"b": "runs use the other reachable hosts"},
		},
		{
			name:     "all down without local fallback fails",
			up:       map[string]bool{"a": false, "b": false},
			fallback: config.LocalFallbackNever,
			want:     map[string]CheckStatus{"a": StatusFail, "b": StatusFail},
		},
		{
			name:     "all down, unset fallback fails",
			up:       map[string]bool{"a": false},
			fallback: "",
			want:     map[string]CheckStatus{"a": StatusFail},
		},
		{
			name:        "all down with on-unreachable warns about local fallback",
			up:          map[string]bool{"a": false, "b": false},
			fallback:    config.LocalFallbackOnUnreachable,
			want:        map[string]CheckStatus{"a": StatusWarn, "b": StatusWarn},
			wantSuggest: map[string]string{"a": "runs will fall back to local"},
		},
		{
			name:     "all down with always warns",
			up:       map[string]bool{"a": false},
			fallback: config.LocalFallbackAlways,
			want:     map[string]CheckStatus{"a": StatusWarn},
		},
		{
			name:     "single host down without fallback fails",
			up:       map[string]bool{"a": false},
			fallback: config.LocalFallbackNever,
			want:     map[string]CheckStatus{"a": StatusFail},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var checks []Check
			// A non-host check must be left alone.
			checks = append(checks, &HostResolutionCheck{Err: stderrors.New("boom")})
			for _, name := range []string{"a", "b"} {
				if up, ok := tt.up[name]; ok {
					checks = append(checks, hostCheckWith(name, up))
				}
			}

			results := RunAll(checks)
			GradeHostResults(checks, results, tt.fallback)

			assert.Equal(t, StatusFail, results[0].Status, "non-host checks are not regraded")
			for i, c := range checks[1:] {
				hc := c.(*HostConnectivityCheck)
				r := results[i+1]
				assert.Equal(t, tt.want[hc.HostName], r.Status, "host %s", hc.HostName)
				if s, ok := tt.wantSuggest[hc.HostName]; ok {
					assert.Contains(t, r.Suggestion, s)
				}
			}
		})
	}
}

// With --path/--requirements, a host whose remote checks were skipped is
// reported once, on its HOSTS result.
func TestHostConnectivityCheck_RemoteErr(t *testing.T) {
	dialErr := stderrors.New("dial failed")

	t.Run("unreachable host notes the skipped checks", func(t *testing.T) {
		check := hostCheckWith("a", false)
		check.RemoteErr = dialErr
		result := check.Run()
		assert.Equal(t, StatusFail, result.Status)
		assert.Contains(t, result.Suggestion, "--path/--requirements checks skipped")
	})

	t.Run("probe ok but remote dial failed warns", func(t *testing.T) {
		check := hostCheckWith("a", true)
		check.RemoteErr = dialErr
		result := check.Run()
		assert.Equal(t, StatusWarn, result.Status)
		assert.Contains(t, result.Message, "dial failed")
		assert.Contains(t, result.Suggestion, "--path/--requirements checks skipped")
	})
}
