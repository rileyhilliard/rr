package doctor

import (
	"fmt"
	"sync"
)

// CheckStatus represents the result status of a check.
type CheckStatus int

const (
	StatusPass CheckStatus = iota
	StatusWarn
	StatusFail
)

// String returns a human-readable status string.
func (s CheckStatus) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	default:
		return "unknown"
	}
}

// CheckResult contains the outcome of running a check.
type CheckResult struct {
	Name       string      `json:"name"`
	Status     CheckStatus `json:"status"`
	Message    string      `json:"message"`
	Suggestion string      `json:"suggestion,omitempty"`
	Fixable    bool        `json:"fixable,omitempty"` // Whether --fix can address this
}

// Check defines the interface for diagnostic checks.
type Check interface {
	// Name returns the check's identifier.
	Name() string

	// Category returns the check's category (e.g., "CONFIG", "SSH", "HOSTS").
	Category() string

	// Run executes the check and returns the result.
	Run() CheckResult

	// Fix attempts to automatically fix the issue (if supported).
	// Returns nil if fix was successful or not applicable.
	Fix() error
}

// CheckGroup represents a category of related checks.
type CheckGroup struct {
	Name    string
	Checks  []Check
	Results []CheckResult
}

// RunAll executes all checks and returns the results.
// Checks within a group run sequentially, but groups can be executed in parallel
// if independent is true.
func RunAll(checks []Check) []CheckResult {
	results := make([]CheckResult, len(checks))
	for i, check := range checks {
		results[i] = check.Run()
	}
	return results
}

// RunAllParallel executes all checks in parallel and returns the results.
func RunAllParallel(checks []Check) []CheckResult {
	results := make([]CheckResult, len(checks))
	var wg sync.WaitGroup

	for i, check := range checks {
		wg.Add(1)
		go func(idx int, c Check) {
			defer wg.Done()
			results[idx] = c.Run()
		}(i, check)
	}

	wg.Wait()
	return results
}

// GroupByCategory organizes checks by their category.
func GroupByCategory(checks []Check) map[string][]Check {
	grouped := make(map[string][]Check)
	for _, check := range checks {
		cat := check.Category()
		grouped[cat] = append(grouped[cat], check)
	}
	return grouped
}

// CountByStatus counts results by status.
func CountByStatus(results []CheckResult) map[CheckStatus]int {
	counts := make(map[CheckStatus]int)
	for _, r := range results {
		counts[r.Status]++
	}
	return counts
}

// HasFailures returns true if any result has a fail status.
func HasFailures(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == StatusFail {
			return true
		}
	}
	return false
}

// HasIssues returns true if any result has a fail or warn status.
func HasIssues(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == StatusFail || r.Status == StatusWarn {
			return true
		}
	}
	return false
}

// FixableCount returns the number of issues that can be fixed automatically.
func FixableCount(results []CheckResult) int {
	count := 0
	for _, r := range results {
		if r.Fixable && (r.Status == StatusFail || r.Status == StatusWarn) {
			count++
		}
	}
	return count
}

// Summary returns a summary string of the check results.
func Summary(results []CheckResult) string {
	counts := CountByStatus(results)
	warn := counts[StatusWarn]
	fail := counts[StatusFail]

	if fail == 0 && warn == 0 {
		return "Everything looks good"
	}

	total := warn + fail
	return fmt.Sprintf("%d issue%s found", total, pluralize(total))
}

func pluralize(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
