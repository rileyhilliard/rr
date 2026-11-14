package cli

import (
	"fmt"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
)

// remoteTargetHosts narrows the hosts rr sync or rr pull may use to the
// remote ones. A local host runs in the project dir, so there's nothing to
// sync to or pull from it: it's never the default, and naming it with
// --host is a config error. verb is "sync to" or "pull from".
func remoteTargetHosts(order []string, hosts map[string]config.Host, named, verb string) ([]string, map[string]config.Host, error) {
	if h, ok := hosts[named]; ok && h.Local {
		return nil, nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("'%s' is a local host; there's nothing to %s it", named, verb),
			"It runs in your project directory already. Name a remote host with --host, or leave --host off.")
	}
	remote := make(map[string]config.Host, len(hosts))
	for name := range hosts {
		if !hosts[name].Local {
			remote[name] = hosts[name]
		}
	}
	remoteOrder := make([]string, 0, len(order))
	for _, name := range order {
		if _, ok := remote[name]; ok {
			remoteOrder = append(remoteOrder, name)
		}
	}
	if len(remote) == 0 {
		return nil, nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("No remote host to %s", verb),
			"Every host this project uses is local, so files are already where commands run.")
	}
	return remoteOrder, remote, nil
}

// preferredRemoteHost returns the host to try first: the named one, or the
// configured default unless that's a host rr sync and rr pull don't use.
func preferredRemoteHost(resolved *config.ResolvedConfig, named string, hosts map[string]config.Host) string {
	if named != "" {
		return named
	}
	preferred, _, _ := config.ResolveHost(resolved, "")
	if _, ok := hosts[preferred]; !ok {
		return ""
	}
	return preferred
}
