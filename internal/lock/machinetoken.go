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
	// machineTokenDir/machineTokenPath are vars so tests can redirect
	// them. The token lives in a per-user 0700 directory: a fixed path in
	// the shared temp dir would let any local user pre-create the file
	// (or a symlink) and control the token.
	machineTokenDir  = filepath.Join(os.TempDir(), fmt.Sprintf("rr-%d", os.Getuid()))
	machineTokenPath = filepath.Join(machineTokenDir, "machine-token")
)

// machineToken returns a random token shared by this user's rr processes on
// this machine, used to identify lock holders more reliably than hostname
// (default hostnames like "MacBook-Pro.local" collide across machines).
// The token lives in the OS temp dir so it survives across processes but
// not across boots; a regenerated or missing token only weakens matching
// back to the hostname+user fallback, which is the conservative direction.
func machineToken() string {
	machineTokenOnce.Do(func() {
		// The directory must be ours alone: refuse a symlink or anything
		// group/other-accessible. An attacker-owned dir at this path just
		// fails the reads/writes below, degrading to the hostname+user
		// fallback.
		if err := os.MkdirAll(machineTokenDir, 0o700); err != nil {
			return
		}
		if fi, err := os.Lstat(machineTokenDir); err != nil || !fi.IsDir() ||
			fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0o077 != 0 {
			return
		}
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
