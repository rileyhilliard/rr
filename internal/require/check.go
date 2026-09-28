package require

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// notFoundMarker is what the lookup prints when the tool isn't there, so a
// missing tool (exit 0, marker) is told apart from a failed setup command
// (non-zero exit before the lookup runs).
const notFoundMarker = "rr-require: not found"

// CheckRequirement verifies a single tool exists on the remote host.
// Uses "command -v <tool>" which is POSIX-compliant and works across shells.
//
// With a host config, the lookup runs the way rr runs commands there (rc
// files, the host's setup_commands, its shell), so a tool those put on PATH
// counts as present. It skips the cd into the project dir, which doesn't exist
// before the first sync. A nil host runs the bare lookup.
//
// The error is non-nil only when the host's setup_commands fail, which would
// otherwise read as every tool missing.
func CheckRequirement(client sshutil.SSHClient, host *config.Host, tool string) (CheckResult, error) {
	result := CheckResult{
		Name:       tool,
		CanInstall: exec.CanInstallTool(tool),
	}

	// Validate tool name to prevent command injection
	if !ValidateToolName(tool) {
		result.Satisfied = false
		return result, nil
	}

	// Use "command -v" for POSIX-compliant tool detection
	cmd := fmt.Sprintf("command -v %s || echo %s", tool, util.ShellQuote(notFoundMarker))
	if host != nil {
		env := *host
		env.Dir = ""
		cmd = exec.BuildRemoteCommand(cmd, &env)
	}
	stdout, stderr, exitCode, err := client.Exec(cmd)

	if err != nil {
		result.Satisfied = false
		return result, nil
	}
	if exitCode != 0 {
		if host == nil || len(host.SetupCommands) == 0 {
			result.Satisfied = false
			return result, nil
		}
		detail := strings.TrimSpace(string(stderr))
		if detail == "" {
			detail = fmt.Sprintf("exit code %d", exitCode)
		}
		return result, errors.New(errors.ErrExec,
			fmt.Sprintf("The host's setup_commands failed while checking requirements: %s", detail),
			"Fix the host's setup_commands in ~/.rr/config.yaml, or run them on the host to see what fails.")
	}

	// rc files and setup_commands can print before the lookup runs (e.g. "nvm
	// use" announcing a version); command -v's answer is the last line.
	out := strings.TrimSpace(string(stdout))
	answer := out[strings.LastIndex(out, "\n")+1:]
	// An alias from an rc file isn't expanded when rr runs a command
	// (non-interactive shell), so it doesn't count.
	if answer == notFoundMarker || strings.HasPrefix(answer, "alias ") {
		result.Satisfied = false
		return result, nil
	}

	result.Satisfied = true
	result.Path = answer
	return result, nil
}

// CheckAll checks all requirements, using cache and parallel execution.
// host is the host's config (see CheckRequirement); nil for a bare lookup.
// Returns results for all requirements, including cached ones.
// Note: Individual check failures are recorded in CheckResult.Satisfied=false,
// not returned as errors. Errors are only returned for systemic failures (the
// host's setup_commands failing).
func CheckAll(client sshutil.SSHClient, host *config.Host, reqs []string, cache *Cache, hostName string) ([]CheckResult, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	results := make([]CheckResult, len(reqs))
	var toCheck []int // Indices of requirements not in cache

	// Check cache first
	for i, req := range reqs {
		if cached, ok := cache.Get(hostName, req); ok {
			results[i] = cached
		} else {
			toCheck = append(toCheck, i)
		}
	}

	// If everything was cached, return early
	if len(toCheck) == 0 {
		return results, nil
	}

	// Check uncached requirements in parallel with bounded concurrency.
	// Limit to 5 concurrent SSH calls to avoid overwhelming the connection.
	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var setupErr error

	for _, idx := range toCheck {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			result, err := CheckRequirement(client, host, reqs[i])

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if setupErr == nil {
					setupErr = err
				}
				return
			}
			results[i] = result
			cache.Set(hostName, reqs[i], result)
		}(idx)
	}

	wg.Wait()

	if setupErr != nil {
		return nil, setupErr
	}
	return results, nil
}

// FilterMissing returns only the unsatisfied requirements.
func FilterMissing(results []CheckResult) []CheckResult {
	var missing []CheckResult
	for _, r := range results {
		if !r.Satisfied {
			missing = append(missing, r)
		}
	}
	return missing
}

// FormatMissing creates a human-readable list of missing requirements.
func FormatMissing(missing []CheckResult) string {
	if len(missing) == 0 {
		return ""
	}

	var parts []string
	for _, m := range missing {
		if m.CanInstall {
			parts = append(parts, fmt.Sprintf("%s (can install)", m.Name))
		} else {
			parts = append(parts, m.Name)
		}
	}
	return strings.Join(parts, ", ")
}
