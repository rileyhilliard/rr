package cli

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/setup"
	"github.com/rileyhilliard/rr/internal/ui"
)

// setupGuide is the how-to for each setup step. It ships in the binary so an
// agent gets it only when it runs `rr setup`, not in every session's context.
//
//go:embed setup_guide.md
var setupGuide string

// ProjectSetupHost is a configured host as `rr setup` reports it.
type ProjectSetupHost struct {
	Name  string `json:"name"`
	Local bool   `json:"local,omitempty"`
}

// ProjectSetupOutput is the `rr setup` result: where setup stands, what it
// wrote, what's left, and the guide for doing it.
type ProjectSetupOutput struct {
	GlobalConfig  string             `json:"global_config"`
	Hosts         []ProjectSetupHost `json:"hosts"`
	ProjectConfig string             `json:"project_config,omitempty"`
	Created       []string           `json:"created"`
	Stack         setup.Stack        `json:"stack"`
	NextSteps     []string           `json:"next_steps"`
	Guide         string             `json:"guide"`
}

// ProjectSetup sets up rr for the project in the current directory. It writes
// a .rr.yaml from the detected stack when the project has none, then reports
// what's left to do along with the setup guide. It never touches the global
// config: hosts need details only the user has.
func ProjectSetup() error {
	out, err := projectSetup()
	if err != nil {
		return err
	}
	if MachineMode() {
		return WriteJSONSuccess(os.Stdout, out)
	}
	printProjectSetup(out)
	return nil
}

func projectSetup() (*ProjectSetupOutput, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Can't figure out what directory you're in",
			"This is unusual - check your directory permissions.")
	}

	globalPath, err := config.GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return nil, err
	}

	stack, err := setup.DetectStack(cwd)
	if err != nil {
		return nil, err
	}

	out := &ProjectSetupOutput{
		GlobalConfig: globalPath,
		Hosts:        []ProjectSetupHost{},
		Created:      []string{},
		Stack:        stack,
		Guide:        setupGuide,
	}
	for name := range globalCfg.Hosts {
		out.Hosts = append(out.Hosts, ProjectSetupHost{Name: name, Local: globalCfg.Hosts[name].Local})
	}
	sort.Slice(out.Hosts, func(i, j int) bool { return out.Hosts[i].Name < out.Hosts[j].Name })

	// Find reports a .rr.yaml above a nested worktree, or a --config path
	// that doesn't exist, as an error that says how to fix it; writing a new
	// one here would hide that.
	existing, err := config.Find(cfgFile)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		out.ProjectConfig = existing
	} else {
		path := filepath.Join(cwd, config.ConfigFileName)
		vals := &projectConfigValues{require: out.Stack.Require, tasks: out.Stack.Tasks}
		if err := saveProjectConfig(path, vals); err != nil {
			return nil, err
		}
		out.ProjectConfig = path
		out.Created = append(out.Created, path)
	}

	out.NextSteps = projectSetupNextSteps(out)
	return out, nil
}

// projectSetupNextSteps lists what's left, in order, as commands or short
// instructions an agent can act on.
func projectSetupNextSteps(out *ProjectSetupOutput) []string {
	var steps []string

	if len(out.Hosts) == 0 {
		steps = append(steps,
			"Ask the user which machine(s) to run on: an SSH alias or user@host per machine and the remote directory (see guide section 1)",
			`Add each one: rr host add --name <name> --ssh "<alias>" --dir '~/rr/${PROJECT}'`,
			"Or, to run on this machine: rr host add --local --name <name>",
			"Run rr setup again to continue",
		)
		return steps
	}

	if len(out.Created) > 0 {
		steps = append(steps, fmt.Sprintf("Review %s: check the generated tasks and require list against how the project really builds and tests (guide section 2)", out.ProjectConfig))
	} else if !out.Stack.Empty() {
		steps = append(steps, fmt.Sprintf("Compare %s with stack.require and stack.tasks and add anything missing (guide section 2)", out.ProjectConfig))
	}

	steps = append(steps,
		"rr doctor",
		`rr exec "echo rr-test-ok"`,
		`rr run "ls -la"`,
		"rr doctor --requirements",
	)

	final := "Run the project's test command, for example rr run \"<test command>\""
	for _, t := range out.Stack.Tasks {
		if t.Name == "test" {
			final = "rr test"
		}
	}
	return append(steps, final)
}

func printProjectSetup(out *ProjectSetupOutput) {
	fmt.Println("rr setup")
	fmt.Println()

	if len(out.Hosts) == 0 {
		fmt.Printf("%s No hosts in %s\n", ui.SymbolPending, out.GlobalConfig)
	} else {
		fmt.Printf("%s %d host(s) in %s\n", ui.SymbolSuccess, len(out.Hosts), out.GlobalConfig)
	}
	if len(out.Created) > 0 {
		fmt.Printf("%s Created %s\n", ui.SymbolSuccess, out.ProjectConfig)
	} else {
		fmt.Printf("%s Using %s\n", ui.SymbolSuccess, out.ProjectConfig)
	}
	if !out.Stack.Empty() {
		fmt.Printf("%s Detected %v, require %v\n", ui.SymbolSuccess, out.Stack.Languages, out.Stack.Require)
	}

	fmt.Println()
	fmt.Println("Next steps:")
	for i, s := range out.NextSteps {
		fmt.Printf("  %d. %s\n", i+1, s)
	}
	fmt.Println()
	fmt.Print(out.Guide)
}
