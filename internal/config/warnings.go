package config

import (
	"fmt"
	"sort"
	"strings"
)

// projectWarnings collects every warning for a loaded project config.
func projectWarnings(raw map[string]interface{}, cfg *Config, unused []string, path string) []Warning {
	var warnings []Warning

	// The output: section was removed; give it a specific message rather
	// than the generic unknown-key one. Only a key actually present in the
	// file counts.
	_, hasOutput := raw["output"]
	if hasOutput {
		warnings = append(warnings, Warning{
			File:       path,
			Key:        "output",
			Message:    "The 'output' section has no effect and is no longer supported",
			Suggestion: "Remove the 'output:' block from " + path + ". Use --pretty for human-readable output.",
		})
	}

	for _, w := range unknownKeyWarnings(unused, path) {
		if hasOutput && (w.Key == "output" || strings.HasPrefix(w.Key, "output.")) {
			continue
		}
		warnings = append(warnings, w)
	}

	return append(warnings, taskWarnings(cfg, path)...)
}

// unknownKeyWarnings turns mapstructure's unused keys into warnings.
func unknownKeyWarnings(unused []string, path string) []Warning {
	keys := make([]string, 0, len(unused))
	for _, k := range unused {
		keys = append(keys, normalizeKeyPath(k))
	}
	sort.Strings(keys)

	warnings := make([]Warning, 0, len(keys))
	for _, key := range keys {
		if w, ok := removedKeys[key]; ok {
			w.File = path
			w.Key = key
			warnings = append(warnings, w)
			continue
		}
		warnings = append(warnings, Warning{
			File:       path,
			Key:        key,
			Message:    fmt.Sprintf("Unknown config key '%s' is ignored", key),
			Suggestion: fmt.Sprintf("Check '%s' in %s for a typo, or remove it.", key, path),
		})
	}
	return warnings
}

// removedKeys are keys older rr versions wrote or documented. They get a
// specific message instead of the generic unknown-key one.
var removedKeys = map[string]Warning{
	"defaults.host": {
		Message:    "'defaults.host' is no longer used; rr stopped supporting a default host",
		Suggestion: "Remove it from ~/.rr/config.yaml. To prefer a host, list it first under 'hosts:' in .rr.yaml.",
	},
}

// normalizeKeyPath converts mapstructure's "tasks[test].steps[0].run" form
// to the dotted "tasks.test.steps.0.run" form users see in YAML terms.
func normalizeKeyPath(key string) string {
	key = strings.ReplaceAll(key, "[", ".")
	return strings.ReplaceAll(key, "]", "")
}

// taskWarnings flags task settings that are accepted but have no effect
// where they're placed.
func taskWarnings(cfg *Config, path string) []Warning {
	names := make([]string, 0, len(cfg.Tasks))
	for name := range cfg.Tasks {
		names = append(names, name)
	}
	sort.Strings(names)

	var warnings []Warning
	for _, name := range names {
		task := cfg.Tasks[name]
		parallel := IsParallelTask(&task)

		if parallel && len(task.Pull) > 0 {
			warnings = append(warnings, Warning{
				File:       path,
				Key:        "tasks." + name + ".pull",
				Message:    fmt.Sprintf("'pull' on parallel task '%s' has no effect", name),
				Suggestion: "Move 'pull:' to the subtasks that produce the files.",
			})
		}

		if !parallel && task.Output != "" {
			warnings = append(warnings, Warning{
				File:       path,
				Key:        "tasks." + name + ".output",
				Message:    fmt.Sprintf("'output' on task '%s' has no effect; it only applies to parallel tasks", name),
				Suggestion: "Remove 'output:' from the task. For parallel tasks it sets progress, stream, verbose, or quiet.",
			})
		}
	}
	return warnings
}
