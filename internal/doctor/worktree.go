package doctor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
)

// WorktreeMappingCheck reports which remote directory this local tree syncs
// to on each host, so "where will this sync?" has a one-command answer.
type WorktreeMappingCheck struct {
	Hosts map[string]config.Host
}

func (c *WorktreeMappingCheck) Name() string {
	return "Worktree mapping"
}

func (c *WorktreeMappingCheck) Category() string {
	return "CONFIG"
}

func (c *WorktreeMappingCheck) Run() CheckResult {
	wt := config.DetectWorktree()

	names := make([]string, 0, len(c.Hosts))
	for name := range c.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	mappings := make([]string, 0, len(names))
	var inPlace []string
	for _, name := range names {
		if c.Hosts[name].Local {
			inPlace = append(inPlace, name)
			continue
		}
		mappings = append(mappings, fmt.Sprintf("%s:%s", name, config.ExpandRemote(c.Hosts[name].Dir)))
	}

	treeDesc := "main checkout"
	if wt.IsLinked {
		treeDesc = fmt.Sprintf("linked worktree '%s'", wt.Name)
	}

	var clauses []string
	if len(mappings) > 0 {
		clauses = append(clauses, "syncs to "+strings.Join(mappings, ", "))
	}
	if len(inPlace) > 0 {
		clauses = append(clauses, "runs in place on "+strings.Join(inPlace, ", "))
	}
	result := CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("%s %s", treeDesc, strings.Join(clauses, " and ")),
	}

	// A linked worktree sharing the main checkout's remote dir means
	// cross-clobber between trees - warn when any host's dir lacks the
	// worktree suffix, regardless of host ordering.
	if wt.IsLinked {
		for _, m := range mappings {
			if !strings.Contains(m, "@") {
				result.Status = StatusWarn
				result.Suggestion = "worktree isolation is disabled (sync.worktree_isolation: false): this worktree shares the main checkout's remote directory, so syncs from different trees will overwrite each other"
				break
			}
		}
	}

	return result
}

func (c *WorktreeMappingCheck) Fix() error {
	return nil
}
