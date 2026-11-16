package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Task is a named command suggested for a project's .rr.yaml.
type Task struct {
	Name        string `json:"name"`
	Run         string `json:"run"`
	Description string `json:"description"`
}

// Stack is what DetectStack found in a project directory.
type Stack struct {
	Languages []string `json:"languages"` // e.g. go, node, python, rust, make
	Require   []string `json:"require"`   // tools the hosts need, for require:
	Tasks     []Task   `json:"tasks"`     // suggested tasks, sorted by name
}

// Empty reports whether nothing was detected.
func (s Stack) Empty() bool {
	return len(s.Languages) == 0
}

// DetectStack looks at marker files in dir (go.mod, package.json,
// pyproject.toml, Cargo.toml, Makefile) and suggests requirements and tasks.
// Makefile targets win over language defaults, since a project that has a
// `make test` usually wants it used.
func DetectStack(dir string) Stack {
	var s Stack
	tasks := map[string]Task{}
	addTask := func(name, run, desc string) {
		tasks[name] = Task{Name: name, Run: run, Description: desc}
	}

	if exists(dir, "go.mod") {
		s.add("go", "go")
		addTask("test", "go test ./...", "Run the test suite")
		addTask("build", "go build ./...", "Build the project")
	}

	if exists(dir, "Cargo.toml") {
		s.add("rust", "cargo")
		addTask("test", "cargo test", "Run the test suite")
		addTask("build", "cargo build", "Build the project")
	}

	if exists(dir, "package.json") {
		pm := nodePackageManager(dir)
		s.add("node", "node")
		if pm != "npm" {
			s.Require = append(s.Require, pm)
		}
		for _, script := range []string{"test", "build", "lint"} {
			if hasPackageScript(dir, script) {
				addTask(script, pm+" run "+script, "Run the "+script+" script")
			}
		}
	}

	if exists(dir, "pyproject.toml") || exists(dir, "requirements.txt") {
		usesUV := exists(dir, "uv.lock")
		if usesUV {
			s.add("python", "uv")
		} else {
			s.add("python", "python3")
		}
		if mentionsPytest(dir) {
			if usesUV {
				addTask("test", "uv run pytest", "Run the test suite")
			} else {
				addTask("test", "python3 -m pytest", "Run the test suite")
			}
		}
	}

	if exists(dir, "Makefile") {
		s.add("make", "make")
		for _, target := range makeTargets(dir, "test", "build", "lint") {
			addTask(target, "make "+target, "Run make "+target)
		}
	}

	for _, t := range tasks {
		s.Tasks = append(s.Tasks, t)
	}
	sort.Slice(s.Tasks, func(i, j int) bool { return s.Tasks[i].Name < s.Tasks[j].Name })
	return s
}

func (s *Stack) add(language, tool string) {
	s.Languages = append(s.Languages, language)
	s.Require = append(s.Require, tool)
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// nodePackageManager picks the package manager from the lockfile.
func nodePackageManager(dir string) string {
	switch {
	case exists(dir, "bun.lock"), exists(dir, "bun.lockb"):
		return "bun"
	case exists(dir, "pnpm-lock.yaml"):
		return "pnpm"
	case exists(dir, "yarn.lock"):
		return "yarn"
	default:
		return "npm"
	}
}

func hasPackageScript(dir, name string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	_, ok := pkg.Scripts[name]
	return ok
}

func mentionsPytest(dir string) bool {
	for _, name := range []string{"pyproject.toml", "requirements.txt", "requirements-dev.txt"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil && strings.Contains(string(data), "pytest") {
			return true
		}
	}
	return false
}

// makeTargets returns which of the wanted targets the Makefile defines.
func makeTargets(dir string, wanted ...string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return nil
	}
	var found []string
	for _, target := range wanted {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `\s*:`)
		if re.Match(data) {
			found = append(found, target)
		}
	}
	return found
}
