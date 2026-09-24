package util

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

var (
	machineIDOnce  sync.Once
	machineIDValue string
)

var ioPlatformUUIDRe = regexp.MustCompile(`"IOPlatformUUID" = "([^"]+)"`)

// MachineID returns a stable identifier for the machine running rr, or ""
// when the platform has none. Unlike the hostname, it doesn't change when
// macOS renames the machine for a new network (MacBookAir-5625.lan, then
// macbookair.lan). It's a short hash of the platform ID (the hardware UUID on
// macOS, /etc/machine-id on Linux), so the raw ID never leaves the machine.
func MachineID() string {
	machineIDOnce.Do(func() {
		raw := platformMachineID()
		if raw == "" {
			return
		}
		sum := sha256.Sum256([]byte("rr-machine:" + raw))
		machineIDValue = hex.EncodeToString(sum[:])[:16]
	})
	return machineIDValue
}

func platformMachineID() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err != nil {
			return ""
		}
		if m := ioPlatformUUIDRe.FindSubmatch(out); m != nil {
			return string(m[1])
		}
	case "linux":
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil {
				if id := strings.TrimSpace(string(b)); id != "" {
					return id
				}
			}
		}
	}
	return ""
}
