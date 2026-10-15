package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

// sbxOutput runs `sbx` and returns its stdout only. Since sbx 0.48 results go to stdout and errors,
// warnings and hints to stderr, so stderr must not be parsed along with the result. A variable so
// tests never run a real `sbx` (TestMain makes it fail unless a test stubs it).
var sbxOutput = func(args ...string) ([]byte, error) {
	return exec.Command("sbx", args...).Output()
}

// foldName produces a comparison key ONLY — never pass it to `sbx`. It lowercases and drops
// every non-alphanumeric character so a name compares equal whatever casing and separators
// `sbx` picked: "claude-APP_Schedule", "claude-app-schedule" and "claude-AppSchedule" all
// fold to "claudeappschedule".
func foldName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sbxList returns the lines of the `sbx ls` table, for showing to the user (--status) and as the
// fallback when `sbx ls --json` cannot be read. `sbx list` is tried when `ls` fails, for an sbx
// that predates it. Errors are swallowed: an unavailable or failing `sbx` simply means "no
// sandboxes known", which callers handle.
var sbxList = func() []string {
	out, err := sbxOutput("ls")
	if err != nil && len(out) == 0 {
		if out, err = sbxOutput("list"); err != nil && len(out) == 0 {
			return nil
		}
	}
	return strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
}

// sbxNames returns the names of the local sandboxes. It reads `sbx ls --json`, whose shape is
// documented (sbx 0.48: an array of objects with `name`; earlier `{"sandboxes": [...]}`), rather
// than the table, whose headings and status marks change between releases. Without usable JSON
// every word of the table counts as a candidate, which is what sbxup always did.
var sbxNames = func() []string {
	if out, err := sbxOutput("ls", "--json"); err == nil {
		if names, err := sandboxNamesJSON(out); err == nil {
			return names
		}
	}
	var words []string
	for _, line := range sbxList() {
		words = append(words, strings.Fields(line)...)
	}
	return words
}

// sandboxNamesJSON reads either shape of `sbx ls --json`.
func sandboxNamesJSON(data []byte) ([]string, error) {
	type row struct {
		Name string `json:"name"`
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		var wrapped struct {
			Sandboxes *[]row `json:"sandboxes"`
		}
		if json.Unmarshal(data, &wrapped) != nil || wrapped.Sandboxes == nil {
			return nil, errors.New("not a list of sandboxes")
		}
		rows = *wrapped.Sandboxes
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	if len(names) == 0 && len(rows) > 0 {
		return nil, errors.New("sandboxes without names")
	}
	return names, nil
}

// resolveSandboxName asks `sbx ls` for the real name of a sandbox.
//
// sbxup names every new sandbox itself (sandboxName), but names are compared folded, so the one
// `sbx ls` reports is returned exactly as spelled there, which is what `sbx run --name` and
// `sbx stop` need. Returns "" when nothing matches.
func resolveSandboxName(sandbox string) string {
	key := foldName(sandbox)
	for _, name := range sbxNames() {
		if name != "" && foldName(name) == key {
			return name
		}
	}
	return ""
}

// sandboxNameFor turns one part of a name (a folder, an agent) into letters, digits and '-': every other
// character becomes '-', so '_' stays free to separate the parts. resolveSandboxName folds names, so a
// later start finds the sandbox under either spelling.
func sandboxNameFor(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// joinSandboxName joins the parts with '_' into a name both `sbx create --name` and the scope of an
// approval rule (`sbx policy approval respond`: letters, digits, '-' and '_') accept: at least two
// characters, not starting with '-' or '_', not "default". ('_' in `sbx create --name` is not yet
// checked on a host.)
func joinSandboxName(parts ...string) string {
	clean := make([]string, len(parts))
	for i, p := range parts {
		clean[i] = sandboxNameFor(p)
	}
	name := strings.TrimLeft(strings.Join(clean, "_"), "-_")
	if len(name) < 2 || strings.EqualFold(name, "default") {
		name = "sbx-" + name
	}
	return name
}

// sandboxName is the name sbxup gives the project's sandbox: the config's `sandbox:` verbatim, or
// `<parent>_<folder>_<agent>` (deneblab_sbx-templates_claude), which tells apart two projects that
// share a folder name. A project at a filesystem or drive root has no parent: `<folder>_<agent>`.
// Not '.': sbx refuses it in the scope of an approval rule, so no approval could be applied.
func sandboxName(cwd, agent, override string) string {
	if override != "" {
		return override
	}
	if dir := filepath.Dir(cwd); filepath.Dir(dir) != dir {
		return joinSandboxName(filepath.Base(dir), filepath.Base(cwd), agent)
	}
	return joinSandboxName(filepath.Base(cwd), agent)
}

// validSandboxName reports whether a name from the config is used unchanged: the same rules
// joinSandboxName enforces, checked instead of applied, because the user typed it.
func validSandboxName(name string) bool {
	if len(name) < 2 || strings.EqualFold(name, "default") || strings.IndexAny(name[:1], "-_") == 0 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// runOnlyFlags are the `sbx run` flags `sbx create` does not take (sbx 0.48).
var runOnlyFlags = map[string]bool{"--detach": true, "-d": true, "--new": true, "--rm": true}

// splitRunArgs divides the extra arguments between `sbx create` and the `sbx run` that follows it:
// run-only flags and everything after "--" (the agent's own arguments) go to run, the rest to create.
func splitRunArgs(extra []string) (create, run []string) {
	for i := 0; i < len(extra); i++ {
		a := extra[i]
		switch {
		case a == "--":
			return create, append(run, extra[i:]...)
		case runOnlyFlags[a] || strings.HasPrefix(a, "--detach-keys="):
			run = append(run, a)
		case a == "--detach-keys":
			run = append(run, a)
			if i+1 < len(extra) {
				i++
				run = append(run, extra[i])
			}
		default:
			create = append(create, a)
		}
	}
	return create, run
}

// buildCreateArgs assembles the `sbx create` argument list, with the sandbox name sbxup chose, so policy
// rules and the GitHub token can be set before `sbx run --name` starts the agent. Kept free of side
// effects so tests can assert the exact command line, which is the cross-platform parity criterion.
//
// workspaces are the extra directories to mount (the cache, then the config's mounts), already in
// the form sbx takes: `path` or `path:ro`. The project itself always comes first, as ".": unlike
// `sbx run`, `sbx create` without a path makes a sandbox with no workspace at all.
func buildCreateArgs(template, agent, name string, clone bool, workspaces, extra []string) []string {
	args := []string{"create", "--name", name, "--template", template, agent}
	if clone {
		args = append(args, "--clone")
	}
	args = append(args, ".")
	args = append(args, workspaces...)
	return append(args, extra...)
}

// runSbx executes `sbx` with the current stdio attached, so interactive sessions work.
//
// While it runs, sbxup catches Ctrl+C instead of dying: the child gets the interrupt from the
// terminal itself and decides, and sbxup must outlive it to stop the token refresh and revoke the
// token. Notify rather than Ignore, because an ignored signal would be inherited by the child.
var runSbx = func(args ...string) error {
	cmd := exec.Command("sbx", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	return cmd.Run()
}

func statusCmd(sandbox string) error {
	fmt.Printf("Sandboxes matching '%s':\n", sandbox)
	key := foldName(sandbox)
	found := false
	for _, line := range sbxList() {
		if line == "" {
			continue
		}
		if strings.Contains(foldName(line), key) {
			fmt.Printf("  %s\n", line)
			found = true
		}
	}
	if !found {
		fmt.Println("  (none found)")
	}
	return nil
}

func stopCmd(sandbox string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] sbx stop %s\n", sandbox)
		return nil
	}
	target := resolveSandboxName(sandbox)
	if target == "" {
		warnf("No sandbox found matching '%s'.", sandbox)
		return nil
	}
	fmt.Printf("Stopping sandbox: %s\n", target)
	return runSbx("stop", target)
}

func execCmd(sandbox string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] sbx exec -it %s bash\n", sandbox)
		return nil
	}
	target := resolveSandboxName(sandbox)
	if target == "" {
		warnf("No sandbox found matching '%s'.", sandbox)
		return nil
	}
	fmt.Printf("Exec into: %s\n", target)
	return runSbx("exec", "-it", target, "bash")
}
