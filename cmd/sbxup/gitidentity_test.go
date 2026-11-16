package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runGitIdentityScript runs the real script with sh and git against home and returns ~/.gitconfig.
func runGitIdentityScript(t *testing.T, home, stdin string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", gitIdentityScript)
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = errors.New(err.Error() + " " + string(out))
	}
	data, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	return string(data), err
}

func globalGit(t *testing.T, home, key string) string {
	t.Helper()
	cmd := exec.Command("git", "config", "--global", "--get", key)
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"))
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

func TestGitIdentityScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	const name = `Ann "A" O'Neil $HOME`

	t.Run("nothing set", func(t *testing.T) {
		home := t.TempDir()
		got, err := runGitIdentityScript(t, home, name+"\nann@example.invalid\n")
		if err != nil {
			t.Fatal(err)
		}
		if globalGit(t, home, "user.name") != name || globalGit(t, home, "user.email") != "ann@example.invalid" {
			t.Fatalf("not set:\n%s", got)
		}
		again, err := runGitIdentityScript(t, home, "Other\nother@example.invalid\n")
		if err != nil || again != got {
			t.Fatalf("second run changed the file (%v):\n%s", err, again)
		}
	})

	t.Run("name kept, email added", func(t *testing.T) {
		home := t.TempDir()
		os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Own\n"), 0o644)
		if _, err := runGitIdentityScript(t, home, "Host\nhost@example.invalid\n"); err != nil {
			t.Fatal(err)
		}
		if globalGit(t, home, "user.name") != "Own" || globalGit(t, home, "user.email") != "host@example.invalid" {
			t.Fatal("the sandbox's own name was replaced, or the email not added")
		}
	})
}

// fakeGitIdentity sets the host identity and counts the execs with their stdin; fail makes them fail.
func fakeGitIdentity(t *testing.T, name, email *string, fail *bool) (*int, *string) {
	t.Helper()
	cacheHome(t)
	n, stdin := 0, ""
	origExec, origID := sbxExecStdin, gitIdentity
	t.Cleanup(func() { sbxExecStdin, gitIdentity = origExec, origID })
	gitIdentity = func(string) (string, string, error) { return *name, *email, nil }
	sbxExecStdin = func(sandbox, script, in string) error {
		n++
		stdin = in
		if script != gitIdentityScript {
			t.Errorf("unexpected script %q", script)
		}
		if *fail {
			return errors.New("refused")
		}
		return nil
	}
	return &n, &stdin
}

func TestApplyGitIdentity(t *testing.T) {
	name, email, fail := "Ann", "ann@example.invalid", false
	n, stdin := fakeGitIdentity(t, &name, &email, &fail)

	// A new sandbox: set once, then a resume with the same identity costs nothing.
	out := capture(t, func() { applyGitIdentity("x--y--claude", "/p", true, false) })
	capture(t, func() { applyGitIdentity("x--y--claude", "/p", false, false) })
	if *n != 1 || *stdin != "Ann\nann@example.invalid\n" || !strings.Contains(out, "Git author: Ann <ann@example.invalid>") {
		t.Fatalf("new + resume: %d execs, stdin %q\n%s", *n, *stdin, out)
	}

	// The host identity changed: handed over again.
	email = "ann@other.invalid"
	capture(t, func() { applyGitIdentity("x--y--claude", "/p", false, false) })
	if *n != 2 {
		t.Fatalf("changed identity: %d execs", *n)
	}

	// The same name created again (after sbx rm): the old record does not count.
	capture(t, func() { applyGitIdentity("x--y--claude", "/p", true, false) })
	if *n != 3 {
		t.Fatalf("fresh with a stale record: %d execs", *n)
	}

	// A sandbox made by an older sbxup (no record) gets it on resume.
	capture(t, func() { applyGitIdentity("old", "/p", false, false) })
	if *n != 4 {
		t.Fatalf("resume without a record: %d execs", *n)
	}
}

func TestApplyGitIdentityNoHostIdentity(t *testing.T) {
	for _, c := range [][2]string{{"", ""}, {"Ann", ""}, {"", "a@b"}, {"A\nB", "a@b"}} {
		name, email, fail := c[0], c[1], false
		n, _ := fakeGitIdentity(t, &name, &email, &fail)
		out := capture(t, func() { applyGitIdentity("s", "/p", true, false) })
		if *n != 0 || !strings.Contains(out, "WARNING: git has no author on this host") {
			t.Fatalf("%q: %d execs\n%s", c, *n, out)
		}
	}
}

func TestApplyGitIdentityFailureAndDryRun(t *testing.T) {
	name, email, fail := "Ann", "ann@example.invalid", true
	n, _ := fakeGitIdentity(t, &name, &email, &fail)

	out := capture(t, func() { applyGitIdentity("s", "/p", true, false) })
	if *n != 1 || !strings.Contains(out, "WARNING: cannot set the git author") {
		t.Fatalf("failure: %d execs\n%s", *n, out)
	}
	// Nothing was recorded, so the next start tries again.
	fail = false
	capture(t, func() { applyGitIdentity("s", "/p", false, false) })
	if *n != 2 {
		t.Fatalf("retry after a failure: %d execs", *n)
	}

	out = capture(t, func() { applyGitIdentity("d", "/p", true, true) })
	if *n != 2 || !strings.Contains(out, "[dry-run] sbx exec d sh -c") || !strings.Contains(out, "Ann <ann@example.invalid>") {
		t.Fatalf("dry-run: %d execs\n%s", *n, out)
	}
	if path, _ := gitIdentityRecordPath("d"); func() bool { _, err := os.Stat(path); return err == nil }() {
		t.Fatal("dry-run wrote a record")
	}
}

func TestGitIdentityReadsHostConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if name, email, err := hostGitIdentity(repo); err != nil || name != "" || email != "" {
		t.Fatalf("unset: %q %q %v", name, email, err)
	}
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Global\n\temail = g@example.invalid\n"), 0o644)
	exec.Command("git", "-C", repo, "config", "user.name", "Local").Run()
	if name, email, err := hostGitIdentity(repo); err != nil || name != "Local" || email != "g@example.invalid" {
		t.Fatalf("repository over global: %q %q %v", name, email, err)
	}
}
