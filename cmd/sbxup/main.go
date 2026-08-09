// Command sbxup launches and manages Claude Code sandboxes from a small YAML config.
//
// It is a port of shells/sbx-runner.ps1 to a single cross-platform binary. Behaviour is
// deliberately identical: sbxup only ever builds an argument list and hands it to the `sbx`
// CLI, so the sandbox semantics live in `sbx`, not here.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `sbxup — launch and manage Claude Code sandboxes from sbxup.yaml

Usage:
  sbxup                    Run sandbox using config defaults
  sbxup --init             Create a default sbxup.yaml config
  sbxup --clone            Run on a private in-container git clone (overrides config)
  sbxup --no-clone         Disable clone mode (overrides config)
  sbxup --exec             Open a shell in the existing sandbox
  sbxup --status           List sandboxes for the current project
  sbxup --stop             Stop the sandbox for the current project
  sbxup --dry-run          Preview the sbx command without running it
  sbxup --version          Print the sbxup version
  sbxup --self-update      Update sbxup to the latest release
  sbxup --help             Show this help message

Parameters:
  --config <path>    Path to YAML config (default: .agents/sbxup.yaml)
  --template <img>   Docker image to use (overrides config)
  --agent <name>     Agent name, e.g. claude (overrides config)

Config file (.agents/sbxup.yaml):
  template: docker.io/pkudrel/sbx-claude-dotnet10:latest
  agent: claude
  clone: false        # optional: true => run on a private in-container git clone
  cache: .sbx-cache   # optional: mount local cache dir into sandbox

Extra arguments are passed through to 'sbx run'.
`

type options struct {
	config     string
	template   string
	agent      string
	clone      bool
	noClone    bool
	exec       bool
	status     bool
	stop       bool
	init       bool
	dryRun     bool
	version    bool
	selfUpdate bool
	help       bool
	extra      []string
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
		return initConfig(o.config)
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
