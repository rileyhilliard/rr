package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
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
// `make test` usually wants it used. A missing file just means that stack
// isn't there; a file that exists but can't be read or parsed is an error.
func DetectStack(dir string) (Stack, error) {
	files, err := readMarkers(dir)
	if err != nil {
		return Stack{}, err
	}

	var s Stack
	tasks := map[string]Task{}
	addTask := func(name, run, desc string) {
		tasks[name] = Task{Name: name, Run: run, Description: desc}
	}
	has := func(names ...string) bool {
		for _, name := range names {
			if _, ok := files[name]; ok {
				return true
			}
		}
		return false
	}

	if has("go.mod") {
		s.add("go", "go")
		addTask("test", "go test ./...", "Run the test suite")
		addTask("build", "go build ./...", "Build the project")
	}

	if has("Cargo.toml") {
		s.add("rust", "cargo")
		addTask("test", "cargo test", "Run the test suite")
		addTask("build", "cargo build", "Build the project")
	}

	if data, ok := files["package.json"]; ok {
		pm := nodePackageManager(has)
		s.add("node", "node")
		if pm != "npm" {
			s.Require = append(s.Require, pm)
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return Stack{}, errors.WrapWithCode(err, errors.ErrConfig,
				"Couldn't parse package.json to look for test, build and lint scripts",
				"Fix the JSON syntax in package.json, then run rr setup again.")
		}
		for _, script := range []string{"test", "build", "lint"} {
			if _, ok := pkg.Scripts[script]; ok {
				addTask(script, pm+" run "+script, "Run the "+script+" script")
			}
		}
	}

	if has("pyproject.toml", "requirements.txt") {
		usesUV := has("uv.lock")
		if usesUV {
			s.add("python", "uv")
		} else {
			s.add("python", "python3")
		}
		if mentionsPytest(files) {
			if usesUV {
				addTask("test", "uv run pytest", "Run the test suite")
			} else {
				addTask("test", "python3 -m pytest", "Run the test suite")
			}
		}
	}

	if data, ok := files["Makefile"]; ok {
		s.add("make", "make")
		targets := makeTargets(data)
		for _, target := range []string{"test", "build", "lint"} {
			if targets[target] {
				addTask(target, "make "+target, "Run make "+target)
			}
		}
	}

	for _, t := range tasks {
		s.Tasks = append(s.Tasks, t)
	}
	sort.Slice(s.Tasks, func(i, j int) bool { return s.Tasks[i].Name < s.Tasks[j].Name })
	return s, nil
}

func (s *Stack) add(language, tool string) {
	s.Languages = append(s.Languages, language)
	s.Require = append(s.Require, tool)
}

// markerFiles are the files DetectStack looks at.
var markerFiles = []string{
	"go.mod", "Cargo.toml", "package.json", "bun.lock", "bun.lockb", "pnpm-lock.yaml",
	"yarn.lock", "pyproject.toml", "requirements.txt", "requirements-dev.txt", "uv.lock", "Makefile",
}

// readMarkers reads each marker file in dir that exists, keyed by name. A
// file that doesn't exist is left out; any other failure is returned.
func readMarkers(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, name := range markerFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, errors.WrapWithCode(err, errors.ErrConfig,
				"Couldn't read "+path+" to detect the project's stack",
				"Check the file's permissions, then run rr setup again.")
		}
		files[name] = data
	}
	return files, nil
}

// nodePackageManager picks the package manager from the lockfile.
func nodePackageManager(has func(...string) bool) string {
	switch {
	case has("bun.lock", "bun.lockb"):
		return "bun"
	case has("pnpm-lock.yaml"):
		return "pnpm"
	case has("yarn.lock"):
		return "yarn"
	default:
		return "npm"
	}
}

func mentionsPytest(files map[string][]byte) bool {
	for _, name := range []string{"pyproject.toml", "requirements.txt", "requirements-dev.txt"} {
		if strings.Contains(string(files[name]), "pytest") {
			return true
		}
	}
	return false
}

// makeTargets returns the targets a Makefile's rules define, including every
// name in a multi-target rule like `test lint: prepare`. Recipe lines and
// variable assignments (`X := y`, `X = y`) are skipped.
func makeTargets(data []byte) map[string]bool {
	targets := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || line[0] == '\t' || line[0] == '#' {
			continue
		}
		i := strings.IndexByte(line, ':')
		if i < 0 || strings.Contains(line[:i], "=") || strings.HasPrefix(line[i:], ":=") {
			continue
		}
		for _, name := range strings.Fields(line[:i]) {
			targets[name] = true
		}
	}
	return targets
}
