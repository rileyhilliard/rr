package lock

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	machineTokenOnce  sync.Once
	machineTokenValue string
	// machineTokenPath is a var so tests can redirect it.
	machineTokenPath = filepath.Join(os.TempDir(), "rr-machine-token")
)

// machineToken returns a random token shared by all rr processes on this
// machine, used to identify lock holders more reliably than hostname
// (default hostnames like "MacBook-Pro.local" collide across machines).
// The token lives in the OS temp dir so it survives across processes but
// not across boots; a regenerated token only weakens matching back to the
// hostname+user fallback, which is the conservative direction.
func machineToken() string {
	machineTokenOnce.Do(func() {
		if data, err := os.ReadFile(machineTokenPath); err == nil {
			tok := strings.TrimSpace(string(data))
			if isHexToken(tok) {
				machineTokenValue = tok
				return
			}
		}
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return // empty token; SameMachine falls back to hostname+user
		}
		tok := hex.EncodeToString(buf)
		// 0644 so rr processes of other local users share the same token.
		if err := os.WriteFile(machineTokenPath, []byte(tok+"\n"), 0o644); err == nil {
			machineTokenValue = tok
		}
	})
	return machineTokenValue
}

func isHexToken(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
