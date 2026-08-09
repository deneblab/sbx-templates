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

const usage = `sbxup — launch and manage Claude Code sandboxes from sbxup.yaml

Usage:
  sbxup                    Run sandbox using config defaults
  sbxup --init             Create sbxup.yaml — pick a template from the latest release
  sbxup --clone            Run on a private in-container git clone (overrides config)
  sbxup --no-clone         Disable clone mode (overrides config)
  sbxup --exec             Open a shell in the existing sandbox
  sbxup --status           List sandboxes for the current project
  sbxup --stop             Stop the sandbox for the current project
  sbxup --dry-run          Preview the sbx command without running it
  sbxup --version          Print the sbxup version
  sbxup --self-update      Update sbxup to the latest release
  sbxup --help             Show this help message

Local templates (no Docker Hub):
  sbxup --build            Build the configured template locally and run it
  sbxup --rebuild          Rebuild even if the local image already exists
  sbxup --update-claude    Rebuild only the Claude Code layer of the local image
  sbxup --refresh          Re-download the template manifest and Dockerfile

Parameters:
  --config <path>    Path to YAML config (default: .sbx/sbxup.config.yaml)
  --template <img>   Docker image to use (overrides config); with --init or --build,
                     a template name from the release, e.g. dotnet10
  --agent <name>     Agent name, e.g. claude (overrides config)

Config file — .sbx/sbxup.config.yaml, the only location read:
  template: docker.io/pkudrel/sbx-claude-dotnet10:latest
  agent: claude
  clone: false        # optional: true => run on a private in-container git clone
  cache: .sbx-cache   # optional: mount local cache dir into sandbox
  build:              # optional: build the template locally instead of pulling it
    name: dotnet10
    release: templates-v0.1.3

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
	build        bool
	rebuild      bool
	updateClaude bool
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

	template := firstNonEmpty(o.template, cfg.Template)
	agent := firstNonEmpty(o.agent, cfg.Agent)

	if agent == "" {
		return fmt.Errorf("agent is required (set in %s or pass --agent). "+
			"Run 'sbxup --init' to create a default config.", configHint(cfgPath))
	}

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

	// Local-build path. The built tag replaces whatever `template` resolved to, so the rest of
	// the run is identical to the registry flow — `sbx run --template <tag>` either way.
	if cfg.Build != nil || o.build || o.rebuild || o.updateClaude {
		tag, err := ensureLocalTemplate(cfg, o)
		if err != nil {
			return err
		}
		template = tag
	}

	if template == "" {
		return fmt.Errorf("template is required (set in %s or pass --template). "+
			"Run 'sbxup --init' to create a default config.", configHint(cfgPath))
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

	cachePath := ""
	if cfg.Cache != "" {
		cachePath = filepath.Join(cwd, cfg.Cache)
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

	args := buildRunArgs(template, agent, cloneEnabled, cachePath, o.extra)

	if o.dryRun {
		fmt.Printf("[dry-run] sbx %s\n", strings.Join(args, " "))
		return nil
	}

	// Resume an existing sandbox rather than failing on "already exists".
	if existing := resolveSandboxName(candidate); existing != "" {
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

	release, err := resolveTemplatesRelease(client, "")
	if err != nil {
		warnf("Cannot reach the template releases (%v) — writing the default registry config.", err)
		return writeOut(defaultConfigBody)
	}
	m, err := loadManifest(client, release, o.refresh)
	if err != nil {
		warnf("Cannot read the template manifest (%v) — writing the default registry config.", err)
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
		warnf("stdin is not a terminal and no --template was given — writing the default registry config.")
		return writeOut(defaultConfigBody)
	}

	return writeOut(buildConfigBody(chosen, release))
}

// ensureLocalTemplate resolves the configured template from the release, builds it if needed,
// and returns the local image tag to run.
func ensureLocalTemplate(cfg *Config, o *options) (string, error) {
	name, pin := "", ""
	if cfg.Build != nil {
		name, pin = cfg.Build.Name, cfg.Build.Release
	}
	// With an explicit build flag, --template names a template rather than an image reference.
	if o.template != "" && (o.build || o.rebuild || o.updateClaude) {
		name = o.template
	}
	if name == "" {
		return "", fmt.Errorf("no template to build: add a 'build:' block to %s or pass --template <name>",
			defaultConfigPath)
	}

	client := &http.Client{Timeout: httpClient}
	release, err := resolveTemplatesRelease(client, pin)
	if err != nil {
		return "", err
	}
	m, err := loadManifest(client, release, o.refresh)
	if err != nil {
		return "", err
	}
	entry, err := findTemplate(m, name)
	if err != nil {
		return "", err
	}
	fmt.Printf("Template: %s %s (%s)\n", entry.Short, entry.Version, release)

	// Already in the sandbox runtime's store: nothing to download and nothing to build, so
	// neither the network nor Docker is touched on the common repeat run.
	tag := entry.LocalTag()
	if !o.rebuild && !o.updateClaude && !o.refresh && !o.dryRun && sbxTemplateListed(tag) {
		fmt.Printf("Reusing template: %s\n", tag)
		return tag, nil
	}

	dockerfile, err := fetchDockerfile(client, release, entry, o.refresh)
	if err != nil {
		return "", err
	}
	return buildTemplate(dockerfile, entry, release, o.rebuild, o.updateClaude, o.dryRun)
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
