package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// Host command flags
var (
	hostListJSON bool
	// Non-interactive host add flags
	hostAddName  string
	hostAddSSH   string
	hostAddDir   string
	hostAddLocal bool
	hostAddTags  []string
	hostAddEnv   []string // KEY=VALUE pairs
)

// HostListOutput represents the JSON output for host list command.
type HostListOutput struct {
	Hosts       []HostConfigInfo `json:"hosts"`
	DefaultHost string           `json:"default_host,omitempty"`
	Config      string           `json:"config,omitempty"`
}

// HostConfigInfo represents a single host in JSON output.
type HostConfigInfo struct {
	Name       string            `json:"name"`
	SSHAliases []string          `json:"ssh_aliases"`
	Local      bool              `json:"local,omitempty"`
	Dir        string            `json:"dir"`
	Tags       []string          `json:"tags,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	IsDefault  bool              `json:"is_default"`
}

// HostAddOptions holds options for the host add command.
type HostAddOptions struct {
	Host      string // Pre-specified SSH host/alias
	Name      string // Friendly name for the host
	Dir       string // Pre-specified remote directory
	SkipProbe bool   // Skip connection testing
}

// hostAdd adds a new host to the global configuration.
func hostAdd(opts HostAddOptions) error {
	cfg, _, err := loadGlobalConfig()
	if err != nil {
		return err
	}

	if hostAddLocal {
		return hostAddLocalFromFlags(cfg)
	}

	// Check if non-interactive mode is requested via flags
	if hostAddName != "" && hostAddSSH != "" {
		return hostAddNonInteractive(cfg, opts.SkipProbe)
	}

	// Offer this machine first, unless a local host already exists
	if findLocalHost(cfg) == "" {
		local, err := promptThisMachine()
		if err != nil {
			return err
		}
		if local {
			_, err := addLocalHostInteractive(cfg)
			return err
		}
	}

	// Get list of existing SSH hosts to exclude from picker
	var existingSSHHosts []string
	for name := range cfg.Hosts {
		existingSSHHosts = append(existingSSHHosts, cfg.Hosts[name].SSH...)
	}

	// Collect machine config interactively (don't skip probe)
	machine, cancelled, err := collectMachineConfig(existingSSHHosts, false, newHostNameValidator(cfg))
	if err != nil {
		return err
	}
	if cancelled {
		fmt.Println("Cancelled.")
		return nil
	}

	// Check for name conflict
	if _, exists := cfg.Hosts[machine.name]; exists {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' already exists", machine.name),
			"Choose a different name, or use 'rr host remove' first.")
	}

	// Determine remote directory
	remoteDir := opts.Dir
	if remoteDir == "" {
		// Use same dir as existing hosts, or default
		for name := range cfg.Hosts {
			if cfg.Hosts[name].Dir != "" {
				remoteDir = cfg.Hosts[name].Dir
				break
			}
		}
		if remoteDir == "" {
			remoteDir = "~/rr/${PROJECT}"
		}

		// Prompt to confirm or change
		if err := promptRemoteDir(&remoteDir); err != nil {
			return err
		}
	}

	// Test connection (unless --skip-probe)
	if !opts.SkipProbe && len(machine.sshHosts) > 0 {
		if err := testConnectionForAdd(machine.sshHosts[0]); err != nil {
			return err
		}
	}

	// Add to config
	cfg.Hosts[machine.name] = config.Host{
		SSH: machine.sshHosts,
		Dir: remoteDir,
	}

	// Save config
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("%s Added host '%s'\n", ui.SymbolSuccess, machine.name)
	return nil
}

// hostAddNonInteractive adds a host using command-line flags (for CI/LLM usage).
// Uses package-level flag variables (hostAddName, hostAddSSH, etc.) for input.
func hostAddNonInteractive(cfg *config.GlobalConfig, skipProbe bool) error {
	// Parse SSH aliases from comma-separated string
	sshAliases := strings.Split(hostAddSSH, ",")
	for i := range sshAliases {
		sshAliases[i] = strings.TrimSpace(sshAliases[i])
	}

	// Validate inputs
	if hostAddName == "" {
		return errors.New(errors.ErrConfig,
			"Host name is required",
			"Use --name to specify a friendly name for the host")
	}
	if len(sshAliases) == 0 || sshAliases[0] == "" {
		return errors.New(errors.ErrConfig,
			"SSH connection is required",
			"Use --ssh to specify SSH hostname or alias (comma-separated for multiple)")
	}

	// Check for name conflict
	if _, exists := cfg.Hosts[hostAddName]; exists {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' already exists", hostAddName),
			"Choose a different name, or use 'rr host remove' first.")
	}
	if hostAddName == "local" {
		return errors.New(errors.ErrConfig,
			"A host can't be named 'local' - that name is reserved for rr's local fallback",
			"Pick another --name, for example the machine's hostname.")
	}

	// Determine remote directory
	remoteDir := hostAddDir
	if remoteDir == "" {
		// Use same dir as existing hosts, or default
		for name := range cfg.Hosts {
			if cfg.Hosts[name].Dir != "" {
				remoteDir = cfg.Hosts[name].Dir
				break
			}
		}
		if remoteDir == "" {
			remoteDir = "~/rr/${PROJECT}"
		}
	}

	// Test connection (unless --skip-probe)
	if !skipProbe {
		_, err := host.Probe(sshAliases[0], 10*time.Second)
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrSSH,
				fmt.Sprintf("Can't reach %s", sshAliases[0]),
				"Make sure the host is up and SSH is working: ssh "+sshAliases[0])
		}
	}

	envMap := parseHostEnv(hostAddEnv)

	// Build host config
	hostConfig := config.Host{
		SSH:  sshAliases,
		Dir:  remoteDir,
		Tags: hostAddTags,
		Env:  envMap,
	}

	// Add to config
	cfg.Hosts[hostAddName] = hostConfig

	// Save config
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}

	// Output result
	if MachineMode() {
		return WriteJSONSuccess(os.Stdout, map[string]interface{}{
			"name":        hostAddName,
			"ssh_aliases": sshAliases,
			"dir":         remoteDir,
			"tags":        hostAddTags,
			"env":         envMap,
		})
	}

	fmt.Printf("%s Added host '%s'\n", ui.SymbolSuccess, hostAddName)
	return nil
}

// parseHostEnv turns KEY=VALUE pairs into a map. Pairs without '=' are skipped.
// Returns nil when there are no pairs.
func parseHostEnv(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	envMap := make(map[string]string)
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	return envMap
}

// findLocalHost returns the name of the host with local: true, or "".
func findLocalHost(cfg *config.GlobalConfig) string {
	names := make([]string, 0, len(cfg.Hosts))
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if cfg.Hosts[name].Local {
			return name
		}
	}
	return ""
}

// addLocalHost adds this machine to the global config as a local host and
// saves it. It refuses a name that is taken or a second local host, and
// validates the result before writing. Shared by `rr host add --local` and
// `rr init`.
func addLocalHost(cfg *config.GlobalConfig, name string, tags []string, env map[string]string) error {
	if name == "" {
		return errors.New(errors.ErrConfig,
			"Host name is required",
			"Use --name to specify a friendly name for the host")
	}
	if _, exists := cfg.Hosts[name]; exists {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' already exists", name),
			"Choose a different name, or use 'rr host remove' first.")
	}
	if existing := findLocalHost(cfg); existing != "" {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("only one host can be local, but '%s' already sets 'local: true'", existing),
			fmt.Sprintf("Use '%s' as this machine, or run 'rr host remove %s' first.", existing, existing))
	}

	if cfg.Hosts == nil {
		cfg.Hosts = make(map[string]config.Host)
	}
	cfg.Hosts[name] = config.Host{Local: true, Tags: tags, Env: env}

	if err := config.ValidateGlobal(cfg); err != nil {
		delete(cfg.Hosts, name)
		return err
	}
	if err := saveGlobalConfig(cfg); err != nil {
		delete(cfg.Hosts, name)
		return err
	}
	return nil
}

// printLocalHostNote explains how a new local host joins project rotations.
func printLocalHostNote() {
	fmt.Println(ui.MutedStyle().Render("  Projects without a 'hosts:' list in .rr.yaml use every global host, so this"))
	fmt.Println(ui.MutedStyle().Render("  machine joins their rotation. List hosts in 'hosts:' to control the order."))
}

// hostAddLocalFromFlags adds a local host from --name, --tag and --env.
func hostAddLocalFromFlags(cfg *config.GlobalConfig) error {
	if hostAddSSH != "" || hostAddDir != "" {
		return errors.New(errors.ErrConfig,
			"--local can't be combined with --ssh or --dir",
			"A local host runs in place on this machine. Drop --local to add a remote host, or drop --ssh and --dir.")
	}
	if hostAddName == "" {
		return errors.New(errors.ErrConfig,
			"Host name is required",
			"Use --name to specify a friendly name for the host")
	}

	envMap := parseHostEnv(hostAddEnv)
	if err := addLocalHost(cfg, hostAddName, hostAddTags, envMap); err != nil {
		return err
	}

	if MachineMode() {
		data := map[string]interface{}{"name": hostAddName, "local": true}
		if len(hostAddTags) > 0 {
			data["tags"] = hostAddTags
		}
		if len(envMap) > 0 {
			data["env"] = envMap
		}
		return WriteJSONSuccess(os.Stdout, data)
	}

	fmt.Printf("%s Added local host '%s'\n", ui.SymbolSuccess, hostAddName)
	printLocalHostNote()
	return nil
}

// addLocalHostInteractive asks for a name, then adds this machine as a local host.
func addLocalHostInteractive(cfg *config.GlobalConfig) (string, error) {
	name, err := promptLocalHostName(cfg)
	if err != nil {
		return "", err
	}
	if err := addLocalHost(cfg, name, nil, nil); err != nil {
		return "", err
	}
	fmt.Printf("%s Added local host '%s'\n", ui.SymbolSuccess, name)
	printLocalHostNote()
	return name, nil
}

// defaultLocalHostName suggests a name for this machine: the short hostname,
// or "local-dev" when that is unusable. It never returns "local".
func defaultLocalHostName(cfg *config.GlobalConfig) string {
	const fallback = "local-dev"
	h, err := os.Hostname()
	if err != nil {
		return fallback
	}
	h = strings.ToLower(strings.TrimSpace(strings.SplitN(h, ".", 2)[0]))
	if h == "" || h == "local" || h == "localhost" {
		return fallback
	}
	if _, taken := cfg.Hosts[h]; taken {
		return fallback
	}
	return h
}

// promptThisMachine asks whether to add this machine (local) or an SSH host.
func promptThisMachine() (bool, error) {
	var local bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[bool]().
				Title("Which machine is this host?").
				Options(
					huh.NewOption("This machine (run in place, no SSH)", true),
					huh.NewOption("A remote machine over SSH", false),
				).
				Value(&local),
		),
	)
	if err := form.Run(); err != nil {
		return false, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your selection",
			"Try non-interactive mode: rr host add --local --name <name>")
	}
	return local, nil
}

// promptLocalHostName asks for the name of the local host.
func promptLocalHostName(cfg *config.GlobalConfig) (string, error) {
	name := defaultLocalHostName(cfg)
	if err := promptMachineName(&name, newHostNameValidator(cfg)); err != nil {
		return "", err
	}
	return strings.TrimSpace(name), nil
}

// newHostNameValidator checks a new host's name at the prompt, rejecting
// names the saved config can't have (taken, or the reserved "local") so the
// user can retype instead of losing every answer given so far.
func newHostNameValidator(cfg *config.GlobalConfig) func(string) error {
	return func(s string) error {
		if err := validateMachineName(s); err != nil {
			return err
		}
		name := strings.TrimSpace(s)
		if name == "local" {
			return fmt.Errorf("a host can't be named 'local' - that name is reserved for rr's local fallback")
		}
		if _, exists := cfg.Hosts[name]; exists {
			return fmt.Errorf("host '%s' already exists, choose a different name", name)
		}
		return nil
	}
}

// hostRemove removes a host from the global configuration.
func hostRemove(name string) error {
	cfg, _, err := loadGlobalConfig()
	if err != nil {
		return err
	}

	// If no name provided, show picker
	if name == "" {
		if len(cfg.Hosts) == 0 {
			return errors.New(errors.ErrConfig,
				"No hosts configured",
				"Nothing to remove.")
		}

		// Build sorted list of host names
		var hostNames []string
		for k := range cfg.Hosts {
			hostNames = append(hostNames, k)
		}
		sort.Strings(hostNames)

		// Build options with SSH info
		options := make([]huh.Option[string], len(hostNames))
		for i, h := range hostNames {
			options[i] = huh.NewOption(hostPickerLabel(h, cfg.Hosts[h]), h)
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select host to remove").
					Options(options...).
					Value(&name),
			),
		)
		if err := form.Run(); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				"Couldn't get your selection",
				"Try again or use: rr host remove <name>")
		}
	}

	// Check if host exists
	hostConfig, exists := cfg.Hosts[name]
	if !exists {
		// List available hosts in error message
		var available []string
		for k := range cfg.Hosts {
			available = append(available, k)
		}
		sort.Strings(available)
		return errors.New(errors.ErrHostNotFound,
			fmt.Sprintf("Host '%s' not found", name),
			fmt.Sprintf("Available hosts: %s", strings.Join(available, ", ")))
	}

	// Confirm removal
	var confirm bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Remove host '%s'?", name)).
				Description("This cannot be undone").
				Value(&confirm),
		),
	)
	if err := form.Run(); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Try again or edit ~/.rr/config.yaml manually.")
	}
	if !confirm {
		fmt.Println("Cancelled.")
		return nil
	}

	// Try to clean up remote artifacts before removing from config
	cleanupRemoteArtifacts(name, hostConfig)

	// Remove the host
	delete(cfg.Hosts, name)

	// Save config
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("%s Removed host '%s'\n", ui.SymbolSuccess, name)
	return nil
}

// hostList lists all configured hosts from global config.
func hostList() error {
	cfg, globalPath, err := loadGlobalConfig()
	if err != nil {
		if hostListJSON || MachineMode() {
			return WriteJSONFromError(os.Stdout, err)
		}
		return err
	}

	// Try to load project config to get host order for determining default
	var hostOrder []string
	if projectPath, findErr := config.Find(""); findErr == nil && projectPath != "" {
		if projectCfg, loadErr := config.Load(projectPath); loadErr == nil {
			// Use project's host order: Hosts (plural) takes precedence over Host (singular)
			if len(projectCfg.Hosts) > 0 {
				hostOrder = projectCfg.Hosts
			} else if projectCfg.Host != "" {
				hostOrder = []string{projectCfg.Host}
			}
		}
	}

	// JSON/machine mode output
	if hostListJSON || MachineMode() {
		return outputHostListJSON(cfg, hostOrder, globalPath)
	}

	// Human-readable output
	return outputHostListText(cfg, globalPath)
}

// sshAliasesOrEmpty returns aliases, or an empty list for a host with none
// (a local host), so the JSON has [] rather than null.
func sshAliasesOrEmpty(aliases []string) []string {
	if aliases == nil {
		return []string{}
	}
	return aliases
}

// outputHostListJSON outputs hosts in JSON format with envelope.
// hostOrder specifies the priority order from project config (if available).
// The default host is the first valid host from hostOrder, falling back to alphabetical.
func outputHostListJSON(cfg *config.GlobalConfig, hostOrder []string, globalPath string) error {
	output := HostListOutput{
		Hosts:  make([]HostConfigInfo, 0, len(cfg.Hosts)),
		Config: globalPath,
	}

	// Sort host names for consistent output
	var names []string
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	// Determine default host: first valid host from hostOrder, else first alphabetically
	if len(hostOrder) > 0 {
		// Find first host from order that exists in global config
		for _, h := range hostOrder {
			if _, exists := cfg.Hosts[h]; exists {
				output.DefaultHost = h
				break
			}
		}
	}
	// Fall back to alphabetical if no host order or no match found
	if output.DefaultHost == "" && len(names) > 0 {
		output.DefaultHost = names[0]
	}

	for _, name := range names {
		h := cfg.Hosts[name]
		info := HostConfigInfo{
			Name:       name,
			SSHAliases: sshAliasesOrEmpty(h.SSH),
			Local:      h.Local,
			Dir:        h.Dir,
			Tags:       h.Tags,
			Env:        h.Env,
			IsDefault:  name == output.DefaultHost,
		}
		output.Hosts = append(output.Hosts, info)
	}

	// Use envelope wrapper in machine mode, plain JSON for --json
	if MachineMode() {
		return WriteJSONSuccess(os.Stdout, output)
	}

	// Legacy --json behavior (no envelope)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputHostListText outputs hosts in human-readable format.
func outputHostListText(cfg *config.GlobalConfig, globalPath string) error {
	if len(cfg.Hosts) == 0 {
		fmt.Println("No hosts configured.")
		fmt.Println("\nAdd one with: rr host add")
		return nil
	}

	// Sort host names for consistent output
	var names []string
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	// Styles
	nameStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Show config location
	fmt.Printf("%s\n\n", dimStyle.Render("Config: "+globalPath))

	for _, name := range names {
		h := cfg.Hosts[name]

		// Name
		fmt.Println(nameStyle.Render(name))

		if h.Local {
			fmt.Printf("%s%s\n", dimStyle.Render("  └─ "), "local (this machine, runs in the project dir)")
		}

		// SSH connections
		for i, ssh := range h.SSH {
			prefix := "  └─ "
			if i < len(h.SSH)-1 {
				prefix = "  ├─ "
			}
			fmt.Printf("%s%s\n", dimStyle.Render(prefix), ssh)
		}

		// Directory
		if h.Dir != "" {
			fmt.Printf("  %s\n", dimStyle.Render("dir: "+h.Dir))
		}
		fmt.Println()
	}

	return nil
}

// loadGlobalConfig loads the global config from ~/.rr/config.yaml.
// Returns the config, the path to the config file, and any error.
func loadGlobalConfig() (*config.GlobalConfig, string, error) {
	globalPath, err := config.GlobalConfigPath()
	if err != nil {
		return nil, "", err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return nil, "", err
	}

	return cfg, globalPath, nil
}

// saveGlobalConfig saves the global config to ~/.rr/config.yaml.
func saveGlobalConfig(cfg *config.GlobalConfig) error {
	return config.SaveGlobal(cfg)
}

// testConnectionForAdd tests the SSH connection when adding a host.
func testConnectionForAdd(sshHost string) error {
	fmt.Println()
	spinner := ui.NewSpinner("Testing connection to " + sshHost)
	spinner.Start()

	_, err := host.Probe(sshHost, 10*time.Second)
	if err == nil {
		spinner.Success()
		fmt.Println()
		return nil
	}

	spinner.Fail()

	// Offer to save anyway
	fmt.Printf("\n%s Connection to '%s' failed: %v\n\n", ui.SymbolFail, sshHost, err)
	var saveAnyway bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Add host anyway? (You can fix the connection later)").
				Value(&saveAnyway),
		),
	)

	if formErr := form.Run(); formErr != nil || !saveAnyway {
		return errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Can't reach %s", sshHost),
			"Make sure the host is up and SSH is working: ssh "+sshHost)
	}
	return nil
}

// cleanupRemoteArtifacts attempts to remove synced files from the remote host.
// If the host is unreachable, it logs a warning but doesn't fail.
func cleanupRemoteArtifacts(hostName string, hostConfig config.Host) {
	if len(hostConfig.SSH) == 0 || hostConfig.Dir == "" {
		return
	}

	// Try each SSH alias until one works
	var client *sshutil.Client
	var connErr error
	for _, sshAlias := range hostConfig.SSH {
		client, _, connErr = host.ProbeAndConnect(sshAlias, 10*time.Second)
		if connErr == nil {
			break
		}
	}

	if connErr != nil {
		remoteDir := config.ExpandRemote(hostConfig.Dir)
		fmt.Printf("  %s Host '%s' is unreachable, skipping remote cleanup\n", ui.SymbolWarning, hostName)
		fmt.Printf("    Synced files remain at: %s\n", remoteDir)
		fmt.Printf("    To remove manually: ssh %s 'rm -rf %s'\n", hostConfig.SSH[0], remoteDir)
		return
	}
	defer client.Close()

	// Expand the remote directory path
	remoteDir := config.ExpandRemote(hostConfig.Dir)

	// Build rm command with proper quoting for tilde expansion
	rmCmd := fmt.Sprintf("rm -rf %s", util.ShellQuotePreserveTilde(remoteDir))

	spinner := ui.NewSpinner("Cleaning up remote files")
	spinner.Start()

	_, stderr, exitCode, err := client.Exec(rmCmd)
	if err != nil || exitCode != 0 {
		spinner.Fail()
		errMsg := strings.TrimSpace(string(stderr))
		if err != nil {
			errMsg = err.Error()
		}
		fmt.Printf("  %s Remote cleanup failed: %s\n", ui.SymbolWarning, errMsg)
		fmt.Printf("    Synced files remain at: %s\n", remoteDir)
		fmt.Printf("    To remove manually: ssh %s 'rm -rf %s'\n", hostConfig.SSH[0], remoteDir)
		return
	}

	spinner.Success()
}
