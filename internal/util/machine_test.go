package util

import (
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMachineID(t *testing.T) {
	id := MachineID()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("no machine ID source on %s", runtime.GOOS)
	}
	if id == "" {
		// Containers often have no /etc/machine-id; callers fall back to
		// the hostname.
		t.Skip("no machine ID available on this system")
	}
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{16}$`), id, "a short hash, never the raw hardware ID")
	assert.Equal(t, id, MachineID(), "stable across calls")
}
