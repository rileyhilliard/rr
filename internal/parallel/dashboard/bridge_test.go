package dashboard

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/parallel"
)

// Compile-time check that Bridge implements DashboardBridge interface.
// This catches interface drift at compile time rather than runtime.
func TestBridge_ImplementsDashboardBridge(t *testing.T) {
	var _ parallel.DashboardBridge = (*Bridge)(nil)
}
