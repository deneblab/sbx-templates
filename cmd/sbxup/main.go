// Command sbxup launches and manages Claude Code sandboxes from a small YAML config.
//
// sbxup only ever builds an argument list and hands it to the `sbx` CLI, so the sandbox
// semantics live in `sbx`, not here.
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `sbxup — launch and manage Claude Code sandboxes from .sbx/sbxup.config.yaml

Usage:
  sbxup                    Run sandbox using config defaults
  sbxup --init             Create the config — pick a template from the latest release
  sbxup --clone            Run on a private in-container git clone (overrides config)
  sbxup --no-clone         Disable clone mode (overrides config)
  sbxup --exec             Open a shell in the existing sandbox
  sbxup --status           List sandboxes for the current project
  sbxup --stop             Stop the sandbox for the current project
  sbxup --dry-run          Preview the sbx command without running it
  sbxup --version          Print the sbxup version
  sbxup --self-update      Update sbxup to the latest release
  sbxup --help             Show this help message

Templates are always built locally from the templates-v* release, on first use:
  sbxup --update           Check for a newer template version and build it, without asking
  sbxup --rebuild          Rebuild even if the local image already exists
  sbxup --update-claude    Rebuild only the Claude Code layer of the local image
  sbxup --refresh          Re-check for a newer release and re-download its assets

Parameters:
  --config <path>    Path to YAML config (default: .sbx/sbxup.config.yaml)
  --template <name>  Template from the release, e.g. dotnet10 (overrides config)
  --agent <name>     Agent name, e.g. claude (overrides config)

Config file — .sbx/sbxup.config.yaml, the only location read:
  template: dotnet10   # a template from the release, built locally on first use
  version: 0.2.8       # optional: pin the release (default: latest); a fork: owner/repo@0.1.4
  agent: claude        # optional (default: claude)
  clone: false         # optional: true => run on a private in-container git clone
  cache: .sbx-cache    # optional: mount local cache dir into sandbox
  mounts:              # optional: extra host directories; ':ro' for read-only
    - ~/shared-libs:ro
    - ../docs

When a newer version of a template is available, sbxup keeps running the one you have and asks
before building the new one. Without a terminal it does not ask; --update builds it.

Mounts are checked before anything is built: the home directory and credential directories
(~/.ssh, ~/.aws, ...) are refused, and mounts outside the project need your approval once.

Extra arguments are passed through to 'sbx run'.
`

type options struct {
	config       string
	template     string
	agent        string
	clone        bool
	noClone      bool
	exec         bool
	status       bool
	stop         bool
	init         bool
	build        bool // accepted and ignored: a missing template is always built now
	rebuild      bool
	updateClaude bool
	update       bool
	refresh      bool
	dryRun       bool
	version      bool
	selfUpdate   bool
	help         bool
	extra        []string
}

// parseArgs handles the POSIX-style --flags the PowerShell version had to reparse by hand.
// Unrecognised arguments are passed through to `sbx run` untouched.
func parseArgs(argv []string) (*options, error) {
	o := &options{}
	// value pulls the argument following index i, reporting whether one was present.
	value := func(i int) (string, bool) {
		if i+1 >= len(argv) {
			return "", false
		}
		return argv[i+1], true
	}

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		// target receives the value for the flag currently being parsed; when set, the
		// parser consumes the next argument and advances past it.
		var target *string

		switch strings.ToLower(arg) {
		case "--init":
			o.init = true
		case "--build":
			o.build = true
		case "--rebuild":
			o.rebuild = true
		case "--update-claude":
			o.updateClaude = true
		case "--update":
			o.update = true
		case "--refresh":
			o.refresh = true
		case "--clone":
			o.clone = true
		case "--no-clone":
			o.noClone = true
		case "--exec":
			o.exec = true
		case "--status":
			o.status = true
		case "--stop":
			o.stop = true
		case "--dry-run":
			o.dryRun = true
		case "--version":
			o.version = true
		case "--self-update":
			o.selfUpdate = true
		case "--help", "-h":
			o.help = true
		case "--config":
			target = &o.config
		case "--template":
			target = &o.template
		case "--agent":
			target = &o.agent
		default:
			o.extra = append(o.extra, arg)
		}

		if target != nil {
			v, ok := value(i)
			if !ok {
				return nil, fmt.Errorf("%s requires a value", arg)
			}
			*target = v
			i++
		}
	}
	return o, nil
}

func warnf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", a...)
}

func errorf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		errorf("%v", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	o, err := parseArgs(argv)
	if err != nil {
		return err
	}

	switch {
	case o.help:
		fmt.Print(usage)
		return nil
	case o.version:
		fmt.Println(version)
		return nil
	case o.selfUpdate:
		return selfUpdate()
	case o.init:
		return initFlow(o)
	}

	// --- Resolve config -------------------------------------------------------
	cfgPath := o.config
	if cfgPath == "" {
		cfgPath = findConfig()
	} else if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("config file not found: %s", cfgPath)
	}

	cfg := &Config{}
	if cfgPath != "" {
		fmt.Printf("Config: %s\n", cfgPath)
		cfg, err = loadConfig(cfgPath)
		if err != nil {
			return err
		}
	} else if legacy := legacyConfig(); legacy != "" {
		// sbxup reads one path only. Naming the stale file turns a puzzling "agent is
		// required" into an obvious one-line fix.
		warnf("Found %s, which sbxup no longer reads. Rename it to %s.", legacy, defaultConfigPath)
	}

	agent := firstNonEmpty(o.agent, cfg.Agent, defaultAgent)

	// --- Sandbox name ---------------------------------------------------------
	// The candidate is what `sbx` most likely called the sandbox: agent + folder, verbatim.
	// Every command that takes a name resolves it through `sbx list` first and falls back to
	// the candidate only when no sandbox exists yet.
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	folder := filepath.Base(cwd)
	candidate := agent + "-" + folder

	switch {
	case o.status:
		return statusCmd(folder)
	case o.stop:
		return stopCmd(candidate, o.dryRun)
	case o.exec:
		return execCmd(candidate, o.dryRun)
	}

	// The template is a name from the release, built locally on first use. The built tag is
	// what `sbx run --template` receives.
	name := firstNonEmpty(o.template, cfg.Template)
	if name == "" {
		return fmt.Errorf("template is required (set 'template: <name>' in %s or pass --template <name>). "+
			"Run 'sbxup --init' to create a config.", configHint(cfgPath))
	}
	if o.template != "" {
		if err := checkTemplateName(o.template, "--template"); err != nil {
			return err
		}
	}
	// Looked up before the template so an update offer can say the sandbox will keep its image;
	// the same answer decides below whether to resume it.
	existing := resolveSandboxName(candidate)
	cachePath := ""
	if cfg.Cache != "" {
		cachePath = filepath.Join(cwd, cfg.Cache)
	}
	// Mounts are checked and approved before a template is built, so a refusal costs no minutes.
	mounts, err := prepareMounts(cfg.Mounts, cwd, cachePath, existing != "", o.dryRun)
	if err != nil {
		return err
	}
	template, err := ensureLocalTemplate(name, cfg.Version, existing, o)
	if err != nil {
		return err
	}

	// Precedence: --no-clone > --clone > config 'clone' key (default: off).
	cloneEnabled := cfg.Clone
	if o.clone {
		cloneEnabled = true
	}
	if o.noClone {
		cloneEnabled = false
	}
	if cloneEnabled {
		fmt.Println("Clone mode: on (--clone)")
	}

	if cachePath != "" {
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			if !o.dryRun {
				if err := os.MkdirAll(cachePath, 0o755); err != nil {
					return fmt.Errorf("cannot create cache directory %s: %w", cachePath, err)
				}
				fmt.Printf("Created cache directory: %s\n", cachePath)
			}
		}
		fmt.Printf("Cache: %s\n", cachePath)
	}

	var workspaces []string
	if cachePath != "" {
		workspaces = append(workspaces, cachePath)
	}
	for _, m := range mounts {
		workspaces = append(workspaces, m.Spec())
	}
	args := buildRunArgs(template, agent, cloneEnabled, workspaces, o.extra)

	if o.dryRun {
		fmt.Printf("[dry-run] sbx %s\n", strings.Join(args, " "))
		return nil
	}

	// Resume an existing sandbox rather than failing on "already exists".
	if existing != "" {
		fmt.Printf("Resuming existing sandbox: %s\n", existing)
		return runSbx("run", "--name", existing)
	}
	return runSbx(args...)
}

// initFlow writes a config, preferring one wired to a template from the latest templates-v*
// release. It degrades rather than fails: no network, no release, or a non-interactive stdin
// with no --template all fall back to the static registry default, so scripted and offline
// installs keep working exactly as before.
func initFlow(o *options) error {
	client := &http.Client{Timeout: httpClient}

	// writeOut honours --dry-run on every exit path, including the fallbacks: a preview that
	// creates the file it is previewing is worse than no preview at all.
	writeOut := func(body string) error {
		if o.dryRun {
			path := o.config
			if path == "" {
				path = defaultConfigPath
			}
			fmt.Printf("[dry-run] would write %s:\n\n%s\n", path, body)
			return nil
		}
		return initConfigBody(o.config, body)
	}

	ref, err := resolveTemplatesRelease(client, "", o.refresh)
	if err != nil {
		warnf("Cannot reach the template releases (%v) — writing the default config.", err)
		return writeOut(defaultConfigBody)
	}
	m, err := loadManifest(client, ref, o.refresh)
	if err != nil {
		warnf("Cannot read the template manifest (%v) — writing the default config.", err)
		return writeOut(defaultConfigBody)
	}

	var chosen *TemplateEntry
	switch {
	case o.template != "":
		// An explicit name that does not exist is a mistake worth reporting, not something to
		// paper over with a default the user did not ask for.
		if chosen, err = findTemplate(m, o.template); err != nil {
			return err
		}
	case interactive():
		if chosen, err = pickTemplate(m, os.Stdin, os.Stdout); err != nil {
			return err
		}
	default:
		warnf("stdin is not a terminal and no --template was given — writing the default config.")
		return writeOut(defaultConfigBody)
	}

	return writeOut(buildConfigBody(chosen, ref))
}

// ensureLocalTemplate resolves the named template from the release (pin, or the newest when
// empty), builds it if needed, and returns the local image tag to run.
//
// A template that is not built yet is built without asking only when there is no older version
// of it to run instead (a first start), when the release is pinned, or when the user asked
// (--update, --rebuild, --update-claude). Otherwise offerUpdate keeps the installed version and
// asks first. sandbox is the existing sandbox for this project, or "".
func ensureLocalTemplate(name, pin, sandbox string, o *options) (string, error) {
	client := &http.Client{Timeout: httpClient}
	ref, err := resolveTemplatesRelease(client, pin, o.refresh || o.update)
	if err != nil {
		return "", err
	}
	m, err := loadManifest(client, ref, o.refresh)
	if err != nil {
		return "", err
	}
	entry, err := findTemplate(m, name)
	if err != nil {
		return "", err
	}
	if ref.isDefaultSource() {
		fmt.Printf("Template: %s %s (%s)\n", entry.Short, entry.Version, ref.Tag)
	} else {
		fmt.Printf("Template: %s %s (%s)\n", entry.Short, entry.Version, ref)
	}

	// Already in the sandbox runtime's store: nothing to download and nothing to build, so
	// neither the network nor Docker is touched on the common repeat run.
	tag := entry.LocalTag(ref)
	if !o.rebuild && !o.updateClaude && !o.refresh && !o.dryRun && sbxTemplateListed(tag) {
		fmt.Printf("Reusing template: %s\n", tag)
		return tag, nil
	}

	consent := o.rebuild || o.updateClaude || o.update
	if pin == "" && !consent {
		if use, done := offerUpdate(entry, ref, sandbox, o); done {
			return use, nil
		}
	}

	dockerfile, err := fetchDockerfile(client, ref, m, entry, o.refresh)
	if err != nil {
		return "", err
	}
	built, err := buildTemplate(dockerfile, entry, ref, o.rebuild, o.updateClaude, o.dryRun)
	if err != nil {
		return "", err
	}
	if sandbox != "" && !o.dryRun {
		// sbx keeps a sandbox on the image it was created from, and sbxup resumes it by name.
		fmt.Printf("Sandbox %s keeps the image it was created with. To use %s, remove it with "+
			"'sbx rm %s' (this deletes the sandbox's state, not your project files) and run sbxup again.\n",
			sandbox, built, sandbox)
	}
	return built, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func configHint(cfgPath string) string {
	if cfgPath != "" {
		return cfgPath
	}
	return defaultConfigPath + " (not found)"
}
