package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

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

// sbxList returns the raw lines of `sbx list`. Errors are swallowed: an unavailable or
// failing `sbx` simply means "no sandboxes known", which callers handle.
var sbxList = func() []string {
	out, err := exec.Command("sbx", "list").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil
	}
	return strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
}

// resolveSandboxName asks `sbx list` for the real name of a sandbox.
//
// `sbx` derives the sandbox name from the folder itself and its exact rule is undocumented —
// it does preserve case (folder "AbcVersion" -> sandbox "claude-AbcVersion"), so mirroring it
// here is guesswork that produces names `sbx run --name` / `sbx stop` cannot resolve. Ask
// `sbx list` instead and return the name exactly as it reports it. Returns "" when nothing matches.
func resolveSandboxName(candidate string) string {
	key := foldName(candidate)
	for _, line := range sbxList() {
		for _, token := range strings.Fields(line) {
			if token != "" && foldName(token) == key {
				return token
			}
		}
	}
	return ""
}

// buildRunArgs assembles the `sbx run` argument list. Kept free of side effects so tests can
// assert the exact command line, which is the cross-platform parity criterion.
func buildRunArgs(template, agent string, clone bool, cachePath string, extra []string) []string {
	args := []string{"run", "--template", template, agent}
	if clone {
		args = append(args, "--clone")
	}
	if cachePath != "" {
		args = append(args, ".", cachePath)
	}
	args = append(args, extra...)
	return args
}

// runSbx executes `sbx` with the current stdio attached, so interactive sessions work.
func runSbx(args ...string) error {
	cmd := exec.Command("sbx", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func statusCmd(folder string) error {
	fmt.Printf("Sandboxes matching '%s':\n", folder)
	key := foldName(folder)
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

func stopCmd(candidate string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] sbx stop %s\n", candidate)
		return nil
	}
	target := resolveSandboxName(candidate)
	if target == "" {
		warnf("No sandbox found matching '%s'.", candidate)
		return nil
	}
	fmt.Printf("Stopping sandbox: %s\n", target)
	return runSbx("stop", target)
}

func execCmd(candidate string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] sbx exec -it %s bash\n", candidate)
		return nil
	}
	target := resolveSandboxName(candidate)
	if target == "" {
		warnf("No sandbox found matching '%s'.", candidate)
		return nil
	}
	fmt.Printf("Exec into: %s\n", target)
	return runSbx("exec", "-it", target, "bash")
}
