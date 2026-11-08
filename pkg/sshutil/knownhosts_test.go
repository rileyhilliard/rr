package sshutil

import (
	"crypto/ed25519"
	"crypto/rand"
	stderrors "errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/rileyhilliard/rr/internal/errors"
)

// limaConfig is the ssh_config Lima generates for a VM, verbatim from #265.
// ssh connects with it; rr failed with a host key error.
const limaConfig = `Host lima-devbox
  IdentityFile "/Users/<user>/.lima/_config/user"
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  NoHostAuthenticationForLocalhost yes
  PreferredAuthentications publickey
  Compression no
  BatchMode yes
  IdentitiesOnly yes
  GSSAPIAuthentication no
  Ciphers "^aes128-gcm@openssh.com,aes256-gcm@openssh.com"
  User riley
  ControlMaster auto
  ControlPath "/Users/<user>/.lima/devbox/ssh.sock"
  ControlPersist yes
  Hostname 127.0.0.1
  Port 60022
`

// captureWarnings collects warnings from emitWarning, with the once-per-process
// memory cleared so each test sees its own.
func captureWarnings(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	warnedMu.Lock()
	warned = map[string]bool{}
	warnedMu.Unlock()
	WarningHandler = func(m string) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}
	t.Cleanup(func() { WarningHandler = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

func TestResolveSSHSettings_HostKeySettings(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		config      string
		wantPolicy  hostKeyPolicy
		wantUser    []string // relative to HOME unless absolute
		wantGlobal  []string
		wantWarning string
	}{
		{name: "nothing set", host: "target", config: "Host target\n  HostName 10.0.0.5\n",
			wantPolicy: hostKeyStrict, wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "yes", host: "target", config: "Host target\n  StrictHostKeyChecking yes\n",
			wantPolicy: hostKeyStrict, wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "ask acts like yes", host: "target", config: "Host target\n  StrictHostKeyChecking ask\n",
			wantPolicy: hostKeyStrict, wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "accept-new", host: "target", config: "Host target\n  StrictHostKeyChecking accept-new\n",
			wantPolicy: hostKeyAcceptNew, wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "no", host: "target", config: "Host target\n  StrictHostKeyChecking no\n",
			wantPolicy: hostKeyIgnore, wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "off, any case", host: "target", config: "Host target\n  StrictHostKeyChecking Off\n",
			wantPolicy: hostKeyIgnore, wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "unknown value checks keys and warns", host: "target", config: "Host target\n  StrictHostKeyChecking maybe\n",
			wantPolicy: hostKeyStrict, wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{defaultGlobalKnownHosts},
			wantWarning: `StrictHostKeyChecking "maybe"`},
		{name: "tilde expanded", host: "target", config: "Host target\n  UserKnownHostsFile ~/.ssh/known_hosts_rr\n",
			wantUser: []string{".ssh/known_hosts_rr"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "multiple files", host: "target", config: "Host target\n  UserKnownHostsFile ~/one /etc/rr/two\n",
			wantUser: []string{"one", "/etc/rr/two"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "/dev/null means no file", host: "target", config: "Host target\n  UserKnownHostsFile /dev/null\n",
			wantUser: []string{}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "none means no file", host: "target", config: "Host target\n  UserKnownHostsFile none\n  GlobalKnownHostsFile none\n",
			wantUser: []string{}, wantGlobal: []string{}},
		{name: "% token path skipped with a warning", host: "target", config: "Host target\n  UserKnownHostsFile ~/.ssh/kh_%k ~/.ssh/other\n",
			wantUser: []string{".ssh/other"}, wantGlobal: []string{defaultGlobalKnownHosts}, wantWarning: "~/.ssh/kh_%k"},
		{name: "${VAR} expanded", host: "target", config: "Host target\n  UserKnownHostsFile ${RR_KH_DIR}/kh\n",
			wantUser: []string{"/srv/rr/kh"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "unset ${VAR} skipped with a warning", host: "target", config: "Host target\n  UserKnownHostsFile ${RR_KH_NOPE}/kh ~/.ssh/other\n",
			wantUser: []string{".ssh/other"}, wantGlobal: []string{defaultGlobalKnownHosts}, wantWarning: "${RR_KH_NOPE}, which isn't set"},
		{name: "bare $ and $1 used literally", host: "target", config: "Host target\n  UserKnownHostsFile ~/.ssh/kh$1 ~/.ssh/kh$RR_KH_DIR\n",
			wantUser: []string{".ssh/kh$1", ".ssh/kh$RR_KH_DIR"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "${VAR} set to empty expands to empty", host: "target", config: "Host target\n  UserKnownHostsFile ~/.ssh/kh${RR_KH_EMPTY}\n",
			wantUser: []string{".ssh/kh"}, wantGlobal: []string{defaultGlobalKnownHosts}},
		{name: "unterminated ${ skipped with a warning", host: "target", config: "Host target\n  UserKnownHostsFile ~/.ssh/kh${RR_KH_DIR ~/.ssh/other\n",
			wantUser: []string{".ssh/other"}, wantGlobal: []string{defaultGlobalKnownHosts}, wantWarning: "no closing }"},
		{name: "relative path skipped with a warning", host: "target", config: "Host target\n  UserKnownHostsFile kh ~/.ssh/other\n",
			wantUser: []string{".ssh/other"}, wantGlobal: []string{defaultGlobalKnownHosts}, wantWarning: "isn't an absolute path"},
		{name: "global files", host: "target", config: "Host target\n  GlobalKnownHostsFile /etc/rr/global /dev/null\n",
			wantUser: []string{".ssh/known_hosts"}, wantGlobal: []string{"/etc/rr/global"}},
		{name: "Lima config from #265", host: "lima-devbox", config: limaConfig,
			wantPolicy: hostKeyIgnore, wantUser: []string{}, wantGlobal: []string{defaultGlobalKnownHosts}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSSHConfig(t, tt.config)
			t.Setenv("RR_TEST_SSH_USER", "")
			t.Setenv("RR_KH_DIR", "/srv/rr")
			t.Setenv("RR_KH_EMPTY", "")
			warnings := captureWarnings(t)
			home := os.Getenv("HOME")

			settings := resolveSSHSettings(tt.host)

			wantUser := []string{}
			for _, p := range tt.wantUser {
				if !filepath.IsAbs(p) {
					p = filepath.Join(home, p)
				}
				wantUser = append(wantUser, p)
			}
			assert.Equal(t, tt.wantPolicy, settings.hostKeyPolicy)
			assert.Equal(t, wantUser, settings.userKnownHosts)
			assert.Equal(t, tt.wantGlobal, settings.globalKnownHosts)
			if tt.wantWarning == "" {
				assert.Empty(t, warnings())
			} else {
				require.Len(t, warnings(), 1)
				assert.Contains(t, warnings()[0], tt.wantWarning)
			}
		})
	}

	t.Run("Lima config keeps its connection settings", func(t *testing.T) {
		withSSHConfig(t, limaConfig)
		t.Setenv("RR_TEST_SSH_USER", "")
		settings := resolveSSHSettings("lima-devbox")
		assert.Equal(t, "127.0.0.1", settings.hostname)
		assert.Equal(t, "60022", settings.port)
		assert.Equal(t, "riley", settings.user)
	})

	t.Run("a % path warns once", func(t *testing.T) {
		withSSHConfig(t, "Host target\n  UserKnownHostsFile ~/.ssh/kh_%k\n")
		warnings := captureWarnings(t)
		resolveSSHSettings("target")
		resolveSSHSettings("target")
		assert.Len(t, warnings(), 1)
	})
}

// accept-new records keys only where OpenSSH would: the first
// UserKnownHostsFile as written.
func TestResolveSSHSettings_AppendTo(t *testing.T) {
	tests := []struct {
		name            string
		config          string
		wantAppendTo    string // relative to HOME; "" for nowhere
		wantUnsupported string
	}{
		{name: "default", config: "Host target\n  HostName 10.0.0.5\n", wantAppendTo: ".ssh/known_hosts"},
		{name: "first of several", config: "Host target\n  UserKnownHostsFile ~/one ~/two\n", wantAppendTo: "one"},
		{name: "/dev/null first records nothing", config: "Host target\n  UserKnownHostsFile /dev/null ~/.ssh/known_hosts\n"},
		{name: "none first records nothing", config: "Host target\n  UserKnownHostsFile none ~/.ssh/known_hosts\n"},
		{name: "% token first is unsupported", config: "Host target\n  UserKnownHostsFile ~/.ssh/known_hosts.d/%k\n",
			wantUnsupported: "~/.ssh/known_hosts.d/%k"},
		{name: "% token first, a usable file after it", config: "Host target\n  UserKnownHostsFile ~/.ssh/kh_%k ~/.ssh/known_hosts\n",
			wantUnsupported: "~/.ssh/kh_%k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSSHConfig(t, tt.config)
			captureWarnings(t)

			settings := resolveSSHSettings("target")

			want := ""
			if tt.wantAppendTo != "" {
				want = filepath.Join(os.Getenv("HOME"), tt.wantAppendTo)
			}
			assert.Equal(t, want, settings.appendTo)
			assert.Equal(t, tt.wantUnsupported, settings.unsupportedKnownHosts)
		})
	}
}

// hostKeyTarget runs an in-process SSH server that rejects every key, and
// writes ~/.ssh/config with "Host target" pointing at it plus extra. A dial
// that fails with "unable to authenticate" got past host key checking.
func hostKeyTarget(t *testing.T, extra string) (address string, hostKey ssh.PublicKey) {
	t.Helper()
	withSSHConfig(t, "")
	withTestKey(t)
	port, key := startRejectingSSHServer(t, false)
	config := fmt.Sprintf("Host target\n  HostName 127.0.0.1\n  Port %d\n%s", port, extra)
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ssh", "config"), []byte(config), 0o600))
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), key
}

func knownHostsLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func otherHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return key
}

func TestDial_UnknownHostKeyFailsUnderYes(t *testing.T) {
	hostKeyTarget(t, "  StrictHostKeyChecking yes\n")
	knownHosts := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	msg := err.Error()
	assert.NotContains(t, msg, "unable to authenticate")
	assert.Contains(t, msg, "Host key not trusted")
	assert.Contains(t, msg, knownHosts, "names the file it checked")
	assert.Contains(t, msg, "ssh -o StrictHostKeyChecking=accept-new target exit")
	assert.Contains(t, msg, "StrictHostKeyChecking accept-new for 'target'")
}

func TestDial_AcceptNewRecordsTheKey(t *testing.T) {
	addr, key := hostKeyTarget(t, "  StrictHostKeyChecking accept-new\n")
	knownHosts := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate", "host key accepted")
	want := knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)
	assert.Equal(t, []string{want}, knownHostsLines(t, knownHosts))
	info, err := os.Stat(knownHosts)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// A second dial finds the key it recorded, even with checking strict now.
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ssh", "config"),
		[]byte(fmt.Sprintf("Host target\n  HostName 127.0.0.1\n  Port %s\n  StrictHostKeyChecking yes\n", addr[strings.LastIndex(addr, ":")+1:])), 0o600))
	_, err = Dial("target", 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate", "recorded key verified")
	assert.Len(t, knownHostsLines(t, knownHosts), 1)
}

func TestDial_AcceptNewStillFailsOnAChangedKey(t *testing.T) {
	addr, _ := hostKeyTarget(t, "  StrictHostKeyChecking accept-new\n")
	knownHosts := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	old := knownhosts.Line([]string{knownhosts.Normalize(addr)}, otherHostKey(t))
	require.NoError(t, os.WriteFile(knownHosts, []byte(old+"\n"), 0o600))

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	var mismatch *HostKeyMismatchError
	require.True(t, stderrors.As(err, &mismatch), "want a mismatch: %v", err)
	assert.Equal(t, knownHosts, mismatch.KnownHosts)
	assert.Equal(t, []string{old}, knownHostsLines(t, knownHosts), "nothing appended")
}

func TestDial_NoSkipsTheCheckAndWritesNothing(t *testing.T) {
	tests := []struct {
		name  string
		extra string
	}{
		{"StrictHostKeyChecking no", "  StrictHostKeyChecking no\n"},
		{"as Lima writes it", "  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostKeyTarget(t, tt.extra)

			_, err := Dial("target", 5*time.Second)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "unable to authenticate", "host key not checked")
			assert.NoFileExists(t, filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts"))
		})
	}
}

func TestDial_DevNullKnownHostsUnderYes(t *testing.T) {
	hostKeyTarget(t, "  StrictHostKeyChecking yes\n  UserKnownHostsFile /dev/null\n  GlobalKnownHostsFile none\n")

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "Host key not trusted")
	assert.Contains(t, msg, "UserKnownHostsFile none or /dev/null")
	assert.Contains(t, msg, "StrictHostKeyChecking no for 'target'")
	// ssh would record the key in /dev/null, so that advice can't help.
	assert.NotContains(t, msg, "ssh -o StrictHostKeyChecking=accept-new")
}

func TestDial_AcceptNewWithDevNullAcceptsForThisConnection(t *testing.T) {
	hostKeyTarget(t, "  StrictHostKeyChecking accept-new\n  UserKnownHostsFile /dev/null\n")

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate")
	assert.NoFileExists(t, filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts"))
}

func TestDial_AcceptNewWithDevNullFirstWritesNothing(t *testing.T) {
	hostKeyTarget(t, "  StrictHostKeyChecking accept-new\n  UserKnownHostsFile /dev/null ~/.ssh/known_hosts\n")
	knownHosts := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate", "accepted for this connection")
	// rr creates the default file empty when it's listed; nothing goes in it.
	data, err := os.ReadFile(knownHosts)
	if err == nil {
		assert.Empty(t, data)
	} else {
		assert.ErrorIs(t, err, os.ErrNotExist)
	}
}

// A first UserKnownHostsFile rr can't use is where ssh would record new keys.
// Accepting keys and recording none would trust any key on every
// connection, so accept-new checks strictly instead. Only accept-new's
// advice says so: under yes, rr checks strictly anyway.
func TestDial_AcceptNewWithAnUnusableFirstFileChecksStrictly(t *testing.T) {
	tests := []struct {
		name         string
		policy       string
		files        string
		strictReason bool
	}{
		{"only a % token path", "accept-new", "~/.ssh/known_hosts.d/%k", true},
		{"% token path, then a usable file", "accept-new", "~/.ssh/known_hosts.d/%k ~/.ssh/other_known_hosts", true},
		{"yes with a % token path first", "yes", "~/.ssh/known_hosts.d/%k ~/.ssh/other_known_hosts", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostKeyTarget(t, "  StrictHostKeyChecking "+tt.policy+"\n  UserKnownHostsFile "+tt.files+"\n")
			captureWarnings(t)

			_, err := Dial("target", 5*time.Second)

			require.Error(t, err)
			msg := err.Error()
			assert.Contains(t, msg, "Host key not trusted")
			assert.Contains(t, msg, "~/.ssh/known_hosts.d/%k")
			assert.Contains(t, msg, "rr can't use")
			assert.Contains(t, msg, "ssh -o StrictHostKeyChecking=accept-new target exit")
			if tt.strictReason {
				assert.Contains(t, msg, "rather than accept ones it can't record")
			} else {
				assert.NotContains(t, msg, "rather than accept")
			}
			assert.NotContains(t, msg, "none or /dev/null")
			assert.NoFileExists(t, filepath.Join(os.Getenv("HOME"), ".ssh", "other_known_hosts"))
			assert.NoDirExists(t, filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts.d"))
		})
	}
}

// A file that fails to parse on the re-check under the lock is named on its
// own, whichever of the files it is, so the suggestion's ssh-keygen -f
// points at the file to fix.
func TestAcceptNewKey_UnparseableOnRecheckIsAStructuredError(t *testing.T) {
	const badLine = "example.com ssh-ed25519 not-base64!!\n"
	tests := []struct {
		name    string
		badFile string // "appendTo" or "read"
	}{
		// appendTo wasn't among the files read at dial time, and someone
		// else created it with a bad line since.
		{name: "appendTo created after the dial", badFile: "appendTo"},
		// A file read at dial time was rewritten with a bad line since.
		{name: "a file read at dial time rewritten", badFile: "read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			read := filepath.Join(dir, "known_hosts")
			appendTo := filepath.Join(dir, "rr_known_hosts")
			require.NoError(t, os.WriteFile(read, nil, 0o600))
			require.NoError(t, os.WriteFile(appendTo, nil, 0o600))
			bad, good := appendTo, read
			if tt.badFile == "read" {
				bad, good = read, appendTo
			}
			require.NoError(t, os.WriteFile(bad, []byte(badLine), 0o600))

			err := acceptNewKey([]string{read}, appendTo, "example.com:22", proxyAddr{addr: "example.com:22"}, otherHostKey(t))

			require.Error(t, err)
			var rrErr *errors.Error
			require.True(t, stderrors.As(err, &rrErr))
			assert.Equal(t, errors.ErrSSH, rrErr.Code)
			assert.Contains(t, rrErr.Message, bad)
			assert.NotContains(t, rrErr.Message, good)
			assert.Contains(t, rrErr.Suggestion, "ssh-keygen -R <host> -f "+bad+" ")
		})
	}
}

func TestDial_AcceptNewWarnsAndConnectsWhenItCantWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}
	readOnly := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(readOnly, 0o500))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) }) // #nosec G302 -- let TempDir cleanup remove it
	knownHosts := filepath.Join(readOnly, "known_hosts")
	hostKeyTarget(t, "  StrictHostKeyChecking accept-new\n  UserKnownHostsFile "+knownHosts+"\n")
	warnings := captureWarnings(t)

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate", "connected anyway")
	require.Len(t, warnings(), 1)
	assert.Contains(t, warnings()[0], "Couldn't record the host key")
	assert.Contains(t, warnings()[0], knownHosts)
	assert.NoFileExists(t, knownHosts)
}

func TestDial_ConcurrentAcceptNewRecordsOneLine(t *testing.T) {
	hostKeyTarget(t, "  StrictHostKeyChecking accept-new\n")
	knownHosts := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")

	const dials = 8
	errs := make([]error, dials)
	var wg sync.WaitGroup
	for i := 0; i < dials; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = Dial("target", 5*time.Second)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to authenticate")
	}
	assert.Len(t, knownHostsLines(t, knownHosts), 1)
}

func TestDial_MalformedGlobalKnownHostsIsSkippedWithAWarning(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "ssh_known_hosts")
	require.NoError(t, os.WriteFile(bad, []byte("example.com ssh-ed25519 not-base64!!\n"), 0o600))
	addr, key := hostKeyTarget(t, "  GlobalKnownHostsFile "+bad+"\n")
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts"), []byte(line+"\n"), 0o600))
	warnings := captureWarnings(t)

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate", "user file still checked")
	require.Len(t, warnings(), 1)
	assert.Contains(t, warnings()[0], bad)
}

func TestDial_MalformedUserKnownHostsIsAStructuredError(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(bad, []byte("example.com ssh-ed25519 not-base64!!\n"), 0o600))
	hostKeyTarget(t, "  UserKnownHostsFile "+bad+"\n")

	_, err := Dial("target", 5*time.Second)

	require.Error(t, err)
	var rrErr *errors.Error
	require.True(t, stderrors.As(err, &rrErr))
	assert.Equal(t, errors.ErrSSH, rrErr.Code)
	assert.Contains(t, rrErr.Message, bad)
	assert.NotContains(t, rrErr.Suggestion, "ssh-add")
	assert.Contains(t, rrErr.Suggestion, "UserKnownHostsFile")
}

func TestHostKeyMismatchError_SuggestionNamesFileAndPort(t *testing.T) {
	key := otherHostKey(t)
	tests := []struct {
		name     string
		hostname string
		contains []string
	}{
		{"port 22", "example.com:22", []string{
			"ssh-keygen -R example.com -f /home/user/.ssh/rr_known_hosts",
			"ssh-keyscan -t rsa,ecdsa,ed25519 example.com >> /home/user/.ssh/rr_known_hosts",
		}},
		{"other port", "example.com:2222", []string{
			"ssh-keygen -R '[example.com]:2222' -f /home/user/.ssh/rr_known_hosts",
			"ssh-keyscan -p 2222 -t rsa,ecdsa,ed25519 example.com >> /home/user/.ssh/rr_known_hosts",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &HostKeyMismatchError{
				Hostname:     tt.hostname,
				ReceivedType: "ssh-ed25519",
				KnownHosts:   "/home/user/.ssh/known_hosts",
				Want:         []knownhosts.KnownKey{{Key: key, Filename: "/home/user/.ssh/rr_known_hosts", Line: 3}},
			}
			s := e.Suggestion()
			for _, want := range tt.contains {
				assert.Contains(t, s, want)
			}
		})
	}
}

// The --no-strict-host-key-checking flag reaches the jump host's ssh too.
func TestProxyJumpArgs_NoStrictHostKeyChecking(t *testing.T) {
	assert.NotContains(t, proxyJumpArgs("bastion", "10.0.0.5", "22"), "StrictHostKeyChecking=no")

	StrictHostKeyChecking = false
	t.Cleanup(func() { StrictHostKeyChecking = true })

	assert.Equal(t,
		[]string{"-o", "BatchMode=yes", "-o", "LogLevel=ERROR",
			"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"-W", "[10.0.0.5]:22", "--", "bastion"},
		proxyJumpArgs("bastion", "10.0.0.5", "22"))
}
