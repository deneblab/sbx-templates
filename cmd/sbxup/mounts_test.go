package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMountSpec(t *testing.T) {
	cases := []struct {
		in       string
		wantPath string
		wantRO   bool
		wantErr  bool
	}{
		{"~/libs", "~/libs", false, false},
		{"~/libs:ro", "~/libs", true, false},
		{"~/libs:RO", "~/libs", true, false},
		{"../docs:rw", "../docs", false, false}, // ':rw' is accepted and dropped
		{"../docs:Rw", "../docs", false, false},
		{` D:\data`, `D:\data`, false, false}, // a drive letter is not a mode
		{`D:\data:ro`, `D:\data`, true, false},
		{`D:\data:rw`, `D:\data`, false, false},
		{"/tmp/a:b/c", "/tmp/a:b/c", false, false}, // the text after ':' holds a separator: no suffix
		{"~/libs:r0", "", false, true},             // a typo must not quietly mean writable
		{"~/libs:readonly", "", false, true},
		{"~/libs:", "", false, true},
		{":ro", "", false, true},
		{"", "", false, true},
		{"   ", "", false, true},
	}
	for _, c := range cases {
		path, ro, err := parseMountSpec(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseMountSpec(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && (path != c.wantPath || ro != c.wantRO) {
			t.Errorf("parseMountSpec(%q) = (%q, %v), want (%q, %v)", c.in, path, ro, c.wantPath, c.wantRO)
		}
	}
}

func TestMountSpecAndMode(t *testing.T) {
	if got := (Mount{Path: "/a"}).Spec(); got != "/a" {
		t.Errorf("writable Spec = %q", got)
	}
	if got := (Mount{Path: "/a", ReadOnly: true}).Spec(); got != "/a:ro" {
		t.Errorf("read-only Spec = %q", got)
	}
	if (Mount{}).Mode() != "read-write" || (Mount{ReadOnly: true}).Mode() != "read-only" {
		t.Error("Mode names are wrong")
	}
}

func TestExpandMountPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "h", "u")
	proj := filepath.Join(string(filepath.Separator), "w", "proj")
	abs := filepath.Join(string(filepath.Separator), "data", "x")
	for in, want := range map[string]string{
		"~":          home,
		"~/libs":     filepath.Join(home, "libs"),
		`~\libs`:     filepath.Join(home, "libs"),
		"docs":       filepath.Join(proj, "docs"),
		"../docs":    filepath.Join(filepath.Dir(proj), "docs"),
		"./a/../b":   filepath.Join(proj, "b"),
		abs:          abs,
		"~other/dir": filepath.Join(proj, "~other", "dir"), // only a bare ~ is the home directory
	} {
		if got := expandMountPath(in, home, proj); got != want {
			t.Errorf("expandMountPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	p := func(parts ...string) string { return sep + filepath.Join(parts...) }
	for _, c := range []struct {
		child, parent string
		want          bool
	}{
		{p("a", "b"), p("a"), true},
		{p("a"), p("a"), true},
		{p("a"), p("a", "b"), false},
		{p("ab"), p("a"), false}, // a shared prefix is not containment
		{p("a", "b", "c"), p("a"), true},
		{p("x"), p("a"), false},
		{p("a", "..", "b"), p("b"), true},
	} {
		if got := within(c.child, c.parent); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

func TestCheckMountAllowed(t *testing.T) {
	home := resolveExisting(t.TempDir())
	under := func(rel ...string) string { return filepath.Join(append([]string{home}, rel...)...) }

	refused := map[string]string{
		"the home directory":               home,
		"the parent of the home directory": filepath.Dir(home),
		"a filesystem root":                filepath.VolumeName(home) + string(filepath.Separator),
		"~/.ssh":                           under(".ssh"),
		"inside ~/.ssh":                    under(".ssh", "keys"),
		"~/.aws":                           under(".aws"),
		"~/.claude":                        under(".claude"),
		"a parent of ~/.config/gcloud":     under(".config"),
		"~/.config/gcloud":                 under(".config", "gcloud"),
		"inside ~/.config/gcloud":          under(".config", "gcloud", "x"),
	}
	for what, p := range refused {
		if err := checkMountAllowed(p, home); err == nil {
			t.Errorf("%s (%s) was allowed", what, p)
		}
	}

	// Inside the home directory only the credential directories are off limits.
	allowed := map[string]string{
		"a plain folder in home":      under("shared-libs"),
		"a nested folder in home":     under("projects", "x"),
		"a name that starts like one": under(".sshfoo"),
		".config/other":               under(".config", "other"),
		"a directory elsewhere":       filepath.Join(t.TempDir(), "elsewhere"),
	}
	for what, p := range allowed {
		if err := checkMountAllowed(p, home); err != nil {
			t.Errorf("%s (%s) was refused: %v", what, p, err)
		}
	}
}

// mountEnv builds a home directory and a project directory next to each other.
func mountEnv(t *testing.T) (home, project string) {
	t.Helper()
	root := resolveExisting(t.TempDir())
	home = filepath.Join(root, "home")
	project = filepath.Join(root, "proj")
	for _, d := range []string{home, project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, project
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveMounts(t *testing.T) {
	home, project := mountEnv(t)
	libs := mkdir(t, filepath.Join(home, "libs"))
	docs := mkdir(t, filepath.Join(project, "..", "docs"))
	docs = resolveExisting(docs)

	t.Run("paths are resolved and modes kept", func(t *testing.T) {
		got, err := resolveMounts([]string{"~/libs:ro", "../docs"}, home, project)
		if err != nil {
			t.Fatal(err)
		}
		want := []Mount{{Path: libs, ReadOnly: true}, {Path: docs}}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("mounts = %+v, want %+v", got, want)
		}
	})

	t.Run("a missing directory is skipped, not an error", func(t *testing.T) {
		got, err := resolveMounts([]string{"~/nope", "~/libs"}, home, project)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Path != libs {
			t.Fatalf("mounts = %+v, want only the existing one", got)
		}
	})

	t.Run("a file is not a directory", func(t *testing.T) {
		f := filepath.Join(home, "file.txt")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := resolveMounts([]string{"~/file.txt"}, home, project)
		if err != nil || len(got) != 0 {
			t.Fatalf("got (%+v, %v), want nothing and no error", got, err)
		}
	})

	t.Run("the project and the cache are not mounted twice", func(t *testing.T) {
		cache := mkdir(t, filepath.Join(project, ".sbx-cache"))
		got, err := resolveMounts([]string{".", ".sbx-cache", "~/libs"}, home, project, project, cache)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Path != libs {
			t.Fatalf("mounts = %+v, want only libs", got)
		}
	})

	t.Run("the same directory twice is harmless, with two modes it is an error", func(t *testing.T) {
		got, err := resolveMounts([]string{"~/libs:ro", "~/libs:ro"}, home, project)
		if err != nil || len(got) != 1 {
			t.Fatalf("got (%+v, %v)", got, err)
		}
		if _, err := resolveMounts([]string{"~/libs:ro", "~/libs"}, home, project); err == nil {
			t.Fatal("expected an error for one directory in two modes")
		}
	})

	t.Run("a refusal names the entry and fails the whole list", func(t *testing.T) {
		mkdir(t, filepath.Join(home, ".ssh"))
		_, err := resolveMounts([]string{"~/libs", "~/.ssh:ro"}, home, project)
		if err == nil || !strings.Contains(err.Error(), "~/.ssh:ro") {
			t.Fatalf("error = %v, want one naming the entry", err)
		}
	})

	t.Run("a refused path is refused even if it does not exist", func(t *testing.T) {
		if _, err := resolveMounts([]string{"~/.aws"}, home, project); err == nil {
			t.Fatal("a missing credential directory was allowed")
		}
	})

	t.Run("a bad mode names the entry", func(t *testing.T) {
		_, err := resolveMounts([]string{"~/libs:r0"}, home, project)
		if err == nil || !strings.Contains(err.Error(), "~/libs:r0") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("a symlink into a credential directory does not get around the refusal", func(t *testing.T) {
		ssh := mkdir(t, filepath.Join(home, ".ssh"))
		link := filepath.Join(home, "harmless")
		if err := os.Symlink(ssh, link); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
		if _, err := resolveMounts([]string{"~/harmless"}, home, project); err == nil {
			t.Fatal("a symlink to ~/.ssh was mounted")
		}
	})
}

func TestApprovalKey(t *testing.T) {
	a, b := Mount{Path: "/x/a"}, Mount{Path: "/x/b", ReadOnly: true}
	base := approvalKey("/p", []Mount{a, b})
	if approvalKey("/p", []Mount{b, a}) != base {
		t.Error("the order of the entries changed the key")
	}
	if approvalKey("/other", []Mount{a, b}) == base {
		t.Error("a different project shares the approval")
	}
	if approvalKey("/p", []Mount{a, {Path: "/x/b"}}) == base {
		t.Error("changing :ro to writable kept the approval")
	}
	if approvalKey("/p", []Mount{a}) == base {
		t.Error("dropping an entry kept the approval")
	}
}

// stubApproval makes the approval prompt scriptable and the cache private.
func stubApproval(t *testing.T, interactiveTerm bool, answer, answered bool) *int {
	t.Helper()
	cacheHome(t)
	asked := new(int)
	origInt, origAsk := isInteractive, askMounts
	t.Cleanup(func() { isInteractive, askMounts = origInt, origAsk })
	isInteractive = func() bool { return interactiveTerm }
	askMounts = func(string) (bool, bool) { *asked++; return answer, answered }
	return asked
}

func TestApproveMounts(t *testing.T) {
	home, project := mountEnv(t)
	inside := Mount{Path: mkdir(t, filepath.Join(project, "vendor"))}
	libs := Mount{Path: mkdir(t, filepath.Join(home, "libs")), ReadOnly: true}

	t.Run("a mount inside the project needs no approval", func(t *testing.T) {
		asked := stubApproval(t, true, true, true)
		got, err := approveMounts([]Mount{inside}, project, false, false)
		if err != nil || len(got) != 1 || *asked != 0 {
			t.Fatalf("got (%+v, %v), asked %d", got, err, *asked)
		}
	})

	t.Run("a resumed sandbox is not asked: its mounts are fixed", func(t *testing.T) {
		asked := stubApproval(t, true, true, true)
		if _, err := approveMounts([]Mount{libs}, project, true, false); err != nil || *asked != 0 {
			t.Fatalf("err = %v, asked %d", err, *asked)
		}
	})

	t.Run("yes is remembered, and :ro does not waive it", func(t *testing.T) {
		asked := stubApproval(t, true, true, true)
		got, err := approveMounts([]Mount{inside, libs}, project, false, false)
		if err != nil || len(got) != 2 || *asked != 1 {
			t.Fatalf("got (%+v, %v), asked %d, want a yes after one question", got, err, *asked)
		}
		if _, err := approveMounts([]Mount{inside, libs}, project, false, false); err != nil || *asked != 1 {
			t.Fatalf("second run: err = %v, asked %d, want no new question", err, *asked)
		}
	})

	t.Run("changing :ro to writable asks again", func(t *testing.T) {
		asked := stubApproval(t, true, true, true)
		approveMounts([]Mount{libs}, project, false, false)
		writable := Mount{Path: libs.Path}
		approveMounts([]Mount{writable}, project, false, false)
		if *asked != 2 {
			t.Errorf("asked %d times, want 2", *asked)
		}
	})

	t.Run("no drops the outside mounts and keeps the inside ones", func(t *testing.T) {
		asked := stubApproval(t, true, false, true)
		got, err := approveMounts([]Mount{inside, libs}, project, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != inside {
			t.Fatalf("mounts = %+v, want only the one inside the project", got)
		}
		// Not remembered: the next start asks again.
		approveMounts([]Mount{inside, libs}, project, false, false)
		if *asked != 2 {
			t.Errorf("asked %d times, want 2", *asked)
		}
	})

	t.Run("no terminal refuses and names the path", func(t *testing.T) {
		asked := stubApproval(t, false, true, true)
		_, err := approveMounts([]Mount{libs}, project, false, false)
		if err == nil || !strings.Contains(err.Error(), libs.Path) {
			t.Fatalf("error = %v, want a refusal naming %s", err, libs.Path)
		}
		if *asked != 0 {
			t.Error("prompted without a terminal")
		}
	})

	t.Run("end of input is no answer and refuses", func(t *testing.T) {
		stubApproval(t, true, false, false)
		if _, err := approveMounts([]Mount{libs}, project, false, false); err == nil {
			t.Fatal("an unanswered question was treated as an answer")
		}
	})

	t.Run("dry-run neither asks nor records", func(t *testing.T) {
		asked := stubApproval(t, true, true, true)
		if _, err := approveMounts([]Mount{libs}, project, false, true); err != nil || *asked != 0 {
			t.Fatalf("err = %v, asked %d", err, *asked)
		}
		if mountsApproved(approvalKey(project, []Mount{libs})) {
			t.Error("--dry-run recorded an approval")
		}
	})
}

func TestPrepareMounts(t *testing.T) {
	home, project := mountEnv(t)
	libs := resolveExisting(mkdir(t, filepath.Join(home, "libs")))
	// stubApproval points HOME at a private cache directory, so the fake home is set after it.
	useHome := func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
	}

	t.Run("nothing configured, nothing done", func(t *testing.T) {
		got, err := prepareMounts(nil, project, "", false, false)
		if err != nil || got != nil {
			t.Fatalf("got (%+v, %v)", got, err)
		}
	})

	t.Run("resolves and approves", func(t *testing.T) {
		stubApproval(t, true, true, true)
		useHome(t)
		got, err := prepareMounts([]string{"~/libs:ro"}, project, "", false, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != (Mount{Path: libs, ReadOnly: true}) {
			t.Fatalf("mounts = %+v", got)
		}
	})

	t.Run("a refusal comes before anything is approved", func(t *testing.T) {
		asked := stubApproval(t, true, true, true)
		useHome(t)
		if _, err := prepareMounts([]string{"~"}, project, "", false, false); err == nil {
			t.Fatal("the home directory was accepted")
		}
		if *asked != 0 {
			t.Error("asked about a mount that is refused outright")
		}
	})
}

func TestLoadConfigMounts(t *testing.T) {
	chdir(t)

	write(t, "c.yaml", "template: dotnet10\nmounts:\n  - ~/libs:ro\n  - ../docs\n")
	cfg, err := loadConfig("c.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Mounts, "|") != "~/libs:ro|../docs" {
		t.Errorf("Mounts = %v", cfg.Mounts)
	}

	write(t, "c.yaml", "mounts: ~/libs:ro\n")
	if cfg, err = loadConfig("c.yaml"); err != nil || len(cfg.Mounts) != 1 {
		t.Errorf("a single string should be a list of one: %v, %v", cfg, err)
	}

	write(t, "c.yaml", "mounts:\n")
	if cfg, err = loadConfig("c.yaml"); err != nil || len(cfg.Mounts) != 0 {
		t.Errorf("an empty key should mean none: %v, %v", cfg, err)
	}

	for _, bad := range []string{
		"mounts:\n  - path: ~/x\n    mode: ro\n", // the object form does not exist
		"mounts:\n  - 5\n",
		"mounts:\n  - ''\n",
		"mounts:\n  a: b\n",
	} {
		write(t, "c.yaml", bad)
		if _, err := loadConfig("c.yaml"); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
