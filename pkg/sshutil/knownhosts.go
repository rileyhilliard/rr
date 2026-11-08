package sshutil

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rileyhilliard/rr/internal/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// hostKeyPolicy is a host's StrictHostKeyChecking setting from ~/.ssh/config.
type hostKeyPolicy int

const (
	// hostKeyStrict is yes and ask (the default). rr can't prompt, so ask
	// behaves like yes, as it does for ssh -o BatchMode=yes.
	hostKeyStrict hostKeyPolicy = iota
	// hostKeyAcceptNew records an unknown host's key and connects; a changed
	// key still fails.
	hostKeyAcceptNew
	// hostKeyIgnore is no/off: the host's key isn't checked at all.
	hostKeyIgnore
)

// defaultGlobalKnownHosts is OpenSSH's system-wide known_hosts file. A
// missing file is skipped, so it's only read where it exists.
const defaultGlobalKnownHosts = "/etc/ssh/ssh_known_hosts"

// defaultUserKnownHosts is where host keys are checked when ~/.ssh/config
// doesn't set UserKnownHostsFile.
func defaultUserKnownHosts() string {
	return filepath.Join(homeDir(), ".ssh", "known_hosts")
}

// parseHostKeyPolicy reads a StrictHostKeyChecking value. An unrecognized
// value checks keys as yes does, with a warning, rather than failing open.
func parseHostKeyPolicy(value, host string) hostKeyPolicy {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "ask":
		return hostKeyStrict
	case "accept-new":
		return hostKeyAcceptNew
	case "no", "off":
		return hostKeyIgnore
	default:
		warnOnce("shkc:"+host+":"+value, fmt.Sprintf(
			"StrictHostKeyChecking %q for '%s' in ~/.ssh/config isn't a value rr knows; checking host keys as with 'yes'",
			value, host))
		return hostKeyStrict
	}
}

// knownHostsPaths reads a UserKnownHostsFile or GlobalKnownHostsFile value:
// whitespace-separated paths with ~ expanded. "none" and /dev/null mean no
// file, so they're dropped. Paths with ssh_config % tokens are skipped with
// a warning, so rr never reads or writes a file with a literal % in its name.
func knownHostsPaths(value, option string) []string {
	paths := []string{}
	for _, p := range strings.Fields(value) {
		p = strings.Trim(p, `"`)
		if p == "" || strings.EqualFold(p, "none") || p == os.DevNull {
			continue
		}
		if strings.Contains(p, "%") {
			warnOnce("token:"+p, fmt.Sprintf(
				"%s %s in ~/.ssh/config uses %% tokens, which rr doesn't expand; rr skips that file",
				option, p))
			continue
		}
		paths = append(paths, expandPath(p))
	}
	return paths
}

// warned holds the warnings already shown, so one that would repeat on every
// dial (monitor refreshes, parallel probes) is shown once per process.
var (
	warned   = map[string]bool{}
	warnedMu sync.Mutex
)

func warnOnce(key, message string) {
	warnedMu.Lock()
	seen := warned[key]
	warned[key] = true
	warnedMu.Unlock()
	if !seen {
		emitWarning(message)
	}
}

// loadKnownHosts returns the known_hosts files to check, in order: each
// user file, then each global file, that exists and parses. knownhosts.New
// fails a whole list on one missing file or one bad line, so each file is
// tried on its own first. A missing file is skipped, except the default
// ~/.ssh/known_hosts, which is created empty as before. A user file that
// can't be read or parsed fails the connection with a structured error; a
// bad global file is skipped with a warning, since the user may not be able
// to fix it.
func loadKnownHosts(user, global []string) ([]string, error) {
	var files []string
	for _, path := range user {
		if path == defaultUserKnownHosts() {
			ensureKnownHostsFile(path)
		}
		_, err := knownhosts.New(path)
		if stderrors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrSSH,
				fmt.Sprintf("Couldn't read known_hosts file %s", path),
				"Fix or remove the line the error names (ssh-keygen -R <host> -f "+path+" removes a host's entries), "+
					"or point UserKnownHostsFile for this host at another file in ~/.ssh/config")
		}
		files = append(files, path)
	}
	for _, path := range global {
		_, err := knownhosts.New(path)
		if stderrors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			warnOnce("global:"+path, fmt.Sprintf("Skipping global known_hosts file %s: %v", path, err))
			continue
		}
		files = append(files, path)
	}
	return files, nil
}

// ensureKnownHostsFile creates an empty known_hosts at path if there's none.
// If that fails the file is just missing, which loadKnownHosts skips, so it's
// a warning rather than an error.
func ensureKnownHostsFile(path string) {
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		warnOnce("create:"+path, fmt.Sprintf("Couldn't create %s: %v", filepath.Dir(path), err))
		return
	}
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		warnOnce("create:"+path, fmt.Sprintf("Couldn't create %s: %v", path, err))
	}
}

// knownHostsMu serializes accept-new appends within this process, so two
// dials to one new host don't both record it.
var knownHostsMu sync.Mutex

// createHostKeyCallback checks host keys against files and turns a changed
// key into a HostKeyMismatchError. Under hostKeyAcceptNew an unknown key is
// accepted and appended to appendTo (the host's first UserKnownHostsFile);
// with no appendTo it's accepted for this connection only.
func createHostKeyCallback(files []string, policy hostKeyPolicy, appendTo string) (ssh.HostKeyCallback, error) {
	callback, err := knownhosts.New(files...)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if err == nil || !stderrors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			return mismatchError(hostname, key, keyErr)
		}
		if policy != hostKeyAcceptNew {
			return err
		}
		return acceptNewKey(files, appendTo, hostname, remote, key)
	}, nil
}

// acceptNewKey records an unknown host's key, as StrictHostKeyChecking
// accept-new does. Under the lock it re-reads the files and checks again: a
// concurrent dial may have just recorded this host, with this key (accepted)
// or a different one (a mismatch, which fails). If the write fails, rr warns
// and connects anyway, as OpenSSH does.
func acceptNewKey(files []string, appendTo, hostname string, remote net.Addr, key ssh.PublicKey) error {
	if appendTo == "" {
		return nil
	}

	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()

	current := files
	if _, err := os.Stat(appendTo); err == nil && !slices.Contains(files, appendTo) {
		current = append(append([]string{}, files...), appendTo)
	}
	recheck, err := knownhosts.New(current...)
	if err != nil {
		return err
	}
	err = recheck(hostname, remote, key)
	var keyErr *knownhosts.KeyError
	if err == nil || !stderrors.As(err, &keyErr) {
		return err
	}
	if len(keyErr.Want) > 0 {
		return mismatchError(hostname, key, keyErr)
	}

	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n"
	if err := appendKnownHost(appendTo, line); err != nil {
		warnOnce("append:"+appendTo, fmt.Sprintf(
			"Couldn't record the host key for %s in %s (%v); connecting without saving it",
			hostname, appendTo, err))
	}
	return nil
}

// appendKnownHost appends line to path in one O_APPEND write, creating the
// file (0600) and its directory (0700) if needed. A file whose last line has
// no newline gets one first, so the new entry doesn't join it.
func appendKnownHost(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		line = "\n" + line
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// mismatchError reports a changed host key, naming the file that holds the
// key rr expected.
func mismatchError(hostname string, key ssh.PublicKey, keyErr *knownhosts.KeyError) *HostKeyMismatchError {
	return &HostKeyMismatchError{
		Hostname:     hostname,
		ReceivedType: key.Type(),
		KnownHosts:   keyErr.Want[0].Filename,
		Want:         keyErr.Want,
	}
}
