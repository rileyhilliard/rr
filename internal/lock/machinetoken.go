package lock

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	machineTokenOnce  sync.Once
	machineTokenValue string
	// machineTokenPath is a var so tests can redirect it. The filename is
	// per-user: a fixed world-readable path in the shared temp dir would
	// let any local user pre-create the file and control the token.
	machineTokenPath = filepath.Join(os.TempDir(), fmt.Sprintf("rr-machine-token-%d", os.Getuid()))
)

// machineToken returns a random token shared by this user's rr processes on
// this machine, used to identify lock holders more reliably than hostname
// (default hostnames like "MacBook-Pro.local" collide across machines).
// The token lives in the OS temp dir so it survives across processes but
// not across boots; a regenerated or missing token only weakens matching
// back to the hostname+user fallback, which is the conservative direction.
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
		if err := os.WriteFile(machineTokenPath, []byte(tok+"\n"), 0o600); err == nil {
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
