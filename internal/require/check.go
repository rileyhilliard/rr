package require

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// CheckRequirement verifies a single tool exists on the remote host.
// Uses "command -v <tool>" which is POSIX-compliant and works across shells.
func CheckRequirement(client sshutil.SSHClient, tool string) CheckResult {
	result := CheckResult{
		Name:       tool,
		CanInstall: exec.CanInstallTool(tool),
	}

	// Validate tool name to prevent command injection
	if !ValidateToolName(tool) {
		result.Satisfied = false
		return result
	}

	// Use "command -v" for POSIX-compliant tool detection
	cmd := fmt.Sprintf("command -v %s", tool)
	stdout, _, exitCode, err := client.Exec(cmd)

	if err != nil || exitCode != 0 {
		result.Satisfied = false
		return result
	}

	result.Satisfied = true
	result.Path = strings.TrimSpace(string(stdout))
	return result
}

// CheckAll checks all requirements, using cache and parallel execution.
// Returns results for all requirements, including cached ones.
// Note: Individual check failures are recorded in CheckResult.Satisfied=false,
// not returned as errors. Errors are only returned for systemic failures.
func CheckAll(client sshutil.SSHClient, reqs []string, cache *Cache, hostName string) ([]CheckResult, error) {
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

	// Check uncached requirements in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, idx := range toCheck {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			result := CheckRequirement(client, reqs[i])

			mu.Lock()
			results[i] = result
			cache.Set(hostName, reqs[i], result)
			mu.Unlock()
		}(idx)
	}

	wg.Wait()

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
