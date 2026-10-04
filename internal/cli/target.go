package cli

import (
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
)

// execTarget is where a run executes. It's decided once, from flags and
// config, before any host is contacted, and every run path (run, exec,
// tasks, --repeat, parallel tasks) consumes the same decision.
//
// A local target never builds a host selector or dials anything. Falling
// back to local at runtime (hosts unreachable or all locked) is a different
// thing: the target is remote, and the fallback is reported as a warning.
//
// When the global config has a local host, a local target runs on it
// (hostName and host are set): with its setup, env, shell, require checks
// and lock, as if --host had named it. Without one, it runs bare in the
// caller's terminal.
type execTarget struct {
	local bool
	// reason is host.LocalReasonFlag or host.LocalReasonMode when local.
	reason string
	// hostName and host are the global config's local host a local target
	// runs on; hostName is empty when there's none.
	hostName string
	host     config.Host
}

// resolveExecTarget decides the execution target:
//   - --local: local, reason local_flag.
//   - local mode (see configLocalMode) with no --host or --tag: local,
//     reason local_mode.
//   - otherwise remote.
func resolveExecTarget(resolved *config.ResolvedConfig, localFlag bool, hostFlag, tagFlag string) execTarget {
	var target execTarget
	switch {
	case localFlag:
		target = execTarget{local: true, reason: host.LocalReasonFlag}
	case hostFlag == "" && tagFlag == "" && configLocalMode(resolved):
		target = execTarget{local: true, reason: host.LocalReasonMode}
	default:
		return execTarget{}
	}
	target.hostName, target.host = config.LocalHost(resolved)
	return target
}

// onHosts reports whether the run goes to configured hosts: a remote target,
// or a local target on its local host, which runs there as --host would.
// Only bare local execution ignores tasks' host restrictions.
func (t execTarget) onHosts() bool {
	return !t.local || t.hostName != ""
}

// connection returns the connection a local target runs on (see
// host.LocalRunConnection).
func (t execTarget) connection() *host.Connection {
	return host.LocalRunConnection(t.hostName, t.host, t.reason)
}

// configLocalMode reports whether the config runs locally by design: the
// project enables local_fallback without listing hosts, or local_fallback is
// enabled and no hosts are configured at all, so there's nothing to fall
// back from.
func configLocalMode(resolved *config.ResolvedConfig) bool {
	if resolved == nil {
		return false
	}
	if config.ProjectLocalMode(resolved) {
		return true
	}
	return resolved.Global != nil && len(resolved.Global.Hosts) == 0 && config.ResolveLocalFallback(resolved)
}

// validationOptions returns the ValidateResolved options for this target.
func (t execTarget) validationOptions() []config.ValidationOption {
	if t.local {
		return []config.ValidationOption{config.AllowLocalTarget()}
	}
	return nil
}

// loadRunConfig loads and validates config for a run and decides its
// execution target. --local with --tag is rejected before config loads.
func loadRunConfig(localFlag bool, hostFlag, tagFlag string) (*config.ResolvedConfig, execTarget, error) {
	if err := ValidateLocalAndTag(localFlag, tagFlag); err != nil {
		return nil, execTarget{}, err
	}
	resolved, err := config.LoadResolved(Config())
	if err != nil {
		return nil, execTarget{}, err
	}
	target := resolveExecTarget(resolved, localFlag, hostFlag, tagFlag)
	if err := config.ValidateResolved(resolved, target.validationOptions()...); err != nil {
		return nil, execTarget{}, err
	}
	return resolved, target, nil
}

// resolveTargetHosts returns the hosts a multi-run (parallel task or
// --repeat) may use. A local target uses its local host as the one worker,
// as --host would; with no local host it uses none, which the orchestrator
// runs locally. A remote target resolves hosts from config and --host.
func resolveTargetHosts(resolved *config.ResolvedConfig, target execTarget, hostFlag string) ([]string, map[string]config.Host, error) {
	if target.local {
		if target.hostName != "" {
			return []string{target.hostName}, map[string]config.Host{target.hostName: target.host}, nil
		}
		return nil, make(map[string]config.Host), nil
	}
	return config.ResolveHosts(resolved, hostFlag)
}

// emitLocalConnect writes the structured connect phase for a local target:
// started, then complete on its local host (or "local" without one) with
// details.reason. Nothing is dialed and nothing went wrong, so it's a normal
// completion, not a fallback warning.
func emitLocalConnect(target execTarget) {
	WritePhaseEvent(PhaseEvent{Type: "phase", Phase: "connect", Status: "started"})
	WritePhaseEvent(PhaseEvent{
		Type:    "phase",
		Phase:   "connect",
		Status:  "complete",
		Host:    target.connection().Name,
		Details: map[string]interface{}{"reason": target.reason},
	})
}
