package host

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
)

// BenchmarkNewSelector measures selector creation overhead.
func BenchmarkNewSelector(b *testing.B) {
	hosts := map[string]config.Host{
		"host-1": {SSH: []string{"h1-lan", "h1-vpn"}, Dir: "~/project", Tags: []string{"fast"}},
		"host-2": {SSH: []string{"h2-lan", "h2-vpn"}, Dir: "~/project", Tags: []string{"gpu"}},
		"host-3": {SSH: []string{"h3-lan"}, Dir: "~/project"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewSelector(hosts)
	}
}

// BenchmarkSelector_resolveHost measures host resolution without connection.
func BenchmarkSelector_resolveHost(b *testing.B) {
	hosts := map[string]config.Host{
		"host-1": {SSH: []string{"h1"}, Dir: "~"},
		"host-2": {SSH: []string{"h2"}, Dir: "~"},
		"host-3": {SSH: []string{"h3"}, Dir: "~"},
		"host-4": {SSH: []string{"h4"}, Dir: "~"},
		"host-5": {SSH: []string{"h5"}, Dir: "~"},
	}
	selector := NewSelector(hosts)

	b.Run("first_host", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, _ = selector.resolveHost("")
		}
	})

	b.Run("specific_host", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, _ = selector.resolveHost("host-3")
		}
	})
}

// BenchmarkSelector_orderedHostNames measures host ordering logic.
func BenchmarkSelector_orderedHostNames(b *testing.B) {
	hosts := make(map[string]config.Host)
	for i := 0; i < 10; i++ {
		hosts[string(rune('a'+i))+"-host"] = config.Host{SSH: []string{"ssh"}, Dir: "~"}
	}

	b.Run("no_order_set", func(b *testing.B) {
		selector := NewSelector(hosts)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = selector.orderedHostNames()
		}
	})

	b.Run("with_order_set", func(b *testing.B) {
		selector := NewSelector(hosts)
		order := make([]string, 0, len(hosts))
		for name := range hosts {
			order = append(order, name)
		}
		selector.SetHostOrder(order)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = selector.orderedHostNames()
		}
	})
}

// BenchmarkSelector_HostInfo measures host info collection.
func BenchmarkSelector_HostInfo(b *testing.B) {
	hosts := make(map[string]config.Host)
	for i := 0; i < 10; i++ {
		hosts[string(rune('a'+i))+"-host"] = config.Host{
			SSH:  []string{"primary", "backup"},
			Dir:  "~/projects/myproject",
			Tags: []string{"fast", "gpu"},
		}
	}
	selector := NewSelector(hosts)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = selector.HostInfo()
	}
}

// BenchmarkHasTag measures tag checking performance.
func BenchmarkHasTag(b *testing.B) {
	tags := []string{"fast", "gpu", "linux", "arm64", "primary"}

	b.Run("found_first", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = hasTag(tags, "fast")
		}
	})

	b.Run("found_last", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = hasTag(tags, "primary")
		}
	})

	b.Run("not_found", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = hasTag(tags, "windows")
		}
	})
}
