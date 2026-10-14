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

func TestParseHide(t *testing.T) {
	ok := map[string][]string{
		"hide: dev\n":                    {"dev"},
		"hide: [dev, src/big, dev]\n":    {"dev", filepath.FromSlash("src/big")},
		"hide: [./dev/]\n":               {"dev"},
		"hide: []\n":                     {},
		"hide:\n":                        {},
		"hide:\n  - dev\n  - .secrets\n": {"dev", ".secrets"},
	}
	path := filepath.Join(t.TempDir(), "c.yaml")
	for body, want := range ok {
		write(t, path, "template: dotnet10\n"+body)
		cfg, err := loadConfig(path)
		if err != nil || cfg.Hide == nil {
			t.Errorf("%q: %v %v", body, cfg, err)
			continue
		}
		if strings.Join(*cfg.Hide, "|") != strings.Join(want, "|") {
			t.Errorf("%q: got %q, want %q", body, *cfg.Hide, want)
		}
	}
	bad := map[string]string{
		"hide: /etc\n":           "relative to the project",
		`hide: 'C:\data'` + "\n": "relative to the project",
		"hide: ../other\n":       "leaves the project",
		"hide: [dev/../..]\n":    "leaves the project",
		"hide: ['**/dev']\n":     "patterns are not supported",
		"hide: .\n":              "project itself",
		"hide: [1]\n":            "must be a path",
		"hide: {dev: true}\n":    "must be a list",
	}
	for body, want := range bad {
		write(t, path, "template: dotnet10\n"+body)
		if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", body, want, err)
		}
	}
	write(t, path, "template: dotnet10\n")
	if cfg, err := loadConfig(path); err != nil || cfg.Hide != nil {
		t.Fatalf("no key must leave Hide nil: %v %v", cfg.Hide, err)
	}
}

func TestHideEntriesDefault(t *testing.T) {
	project := t.TempDir()
	if got := hideEntries(nil, project); len(got) != 0 {
		t.Fatalf("no dev directory, no key: %v", got)
	}
	os.Mkdir(filepath.Join(project, "dev"), 0o755)
	if got := hideEntries(nil, project); len(got) != 1 || got[0] != "dev" {
		t.Fatalf("dev exists, no key: %v", got)
	}
	off := []string{}
	if got := hideEntries(&off, project); len(got) != 0 {
		t.Fatalf("hide: [] must turn it off: %v", got)
	}
	mine := []string{"data"}
	if got := hideEntries(&mine, project); len(got) != 1 || got[0] != "data" {
		t.Fatalf("configured list: %v", got)
	}
}

func TestResolveHide(t *testing.T) {
	project := resolveExisting(t.TempDir())
	os.Mkdir(filepath.Join(project, "dev"), 0o755)

	got, err := resolveHide([]string{"dev", "missing"}, project, "", nil, false)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].Host != filepath.Join(project, "dev") || got[1].Host != filepath.Join(project, "missing") {
		t.Fatalf("hosts: %+v", got)
	}
	if runtime.GOOS != "windows" && got[0].Target != got[0].Host {
		t.Fatalf("target %q, want the host path", got[0].Target)
	}

	cache := filepath.Join(project, "dev", "cache")
	for name, c := range map[string]struct {
		entries []string
		cache   string
		mounts  []Mount
		clone   bool
		want    string
	}{
		"cache inside": {[]string{"dev"}, cache, nil, false, "overlaps the cache"},
		"mount inside": {[]string{"dev"}, "", []Mount{{Path: filepath.Join(project, "dev", "x")}}, false, "overlaps the mount"},
		"mount around": {[]string{"dev"}, "", []Mount{{Path: project}}, false, "overlaps the mount"},
		"nested":       {[]string{"dev", filepath.Join("dev", "a")}, "", nil, false, "overlap; keep the outer one"},
		"clone":        {[]string{"dev"}, "", nil, true, "clone mode"},
	} {
		if _, err := resolveHide(c.entries, project, c.cache, c.mounts, c.clone); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", name, c.want, err)
		}
	}
	if got, err := resolveHide(nil, project, "", nil, true); err != nil || got != nil {
		t.Fatalf("nothing to hide must not care about clone mode: %v %v", got, err)
	}

	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		os.Symlink(outside, filepath.Join(project, "link"))
		if _, err := resolveHide([]string{"link"}, project, "", nil, false); err == nil || !strings.Contains(err.Error(), "outside the project") {
			t.Fatalf("a symlink leading out: %v", err)
		}
	}
}

func TestSandboxPath(t *testing.T) {
	for host, want := range map[string]string{
		`W:\tmp\12\dev`:       "/w/tmp/12/dev",
		`c:\Users\Me\app\dev`: "/c/Users/Me/app/dev",
	} {
		if got, err := sandboxPath(host, "windows"); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", host, got, err, want)
		}
	}
	for _, host := range []string{`\\server\share\dev`, `dev`, `1:\x`} {
		if _, err := sandboxPath(host, "windows"); err == nil {
			t.Errorf("%s: want an error", host)
		}
	}
	if got, _ := sandboxPath("/home/me/app/dev", "linux"); got != "/home/me/app/dev" {
		t.Fatalf("linux: %q", got)
	}
}

func TestEnsureCover(t *testing.T) {
	configHome(t)
	a, err := ensureCover("App.Claude", "/w/app/dev")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(a, "HIDDEN.txt")); string(b) != hiddenText {
		t.Fatalf("HIDDEN.txt: %q", b)
	}
	entries, _ := os.ReadDir(a)
	if len(entries) != 1 {
		t.Fatalf("the cover must hold only HIDDEN.txt: %v", entries)
	}
	again, _ := ensureCover("app.claude", "/w/app/dev")
	other, _ := ensureCover("app.claude", "/w/app/data")
	if again != a || other == a || filepath.Dir(other) != filepath.Dir(a) {
		t.Fatalf("covers: %s %s %s", a, again, other)
	}
	if !strings.Contains(filepath.ToSlash(a), "sbxup/hide/app.claude/") {
		t.Fatalf("cover location: %s", a)
	}
}

// fakeHide stubs every sbx call applyHide makes and records them.
type fakeHide struct {
	mountFail, sudoFail, notRunningOnce bool
	calls, notices                      []string
}

func (f *fakeHide) install(t *testing.T) {
	t.Helper()
	configHome(t)
	origStart, origMount, origUmount, origSudo, origExec := sbxStart, sbxMount, sbxUmount, dropSudo, sbxExecStdin
	t.Cleanup(func() {
		sbxStart, sbxMount, sbxUmount, dropSudo, sbxExecStdin = origStart, origMount, origUmount, origSudo, origExec
	})
	sbxStart = func(s string) error { f.calls = append(f.calls, "start "+s); return nil }
	sbxMount = func(s, src, target string) error {
		f.calls = append(f.calls, "mount "+s+" "+target)
		if f.notRunningOnce {
			f.notRunningOnce = false
			return errors.New(`409 Conflict: sandbox "x" is not running`)
		}
		if f.mountFail {
			return errors.New("refused")
		}
		if _, err := os.Stat(filepath.Join(src, "HIDDEN.txt")); err != nil {
			t.Errorf("mounted %s without its cover", src)
		}
		return nil
	}
	sbxUmount = func(s, src, target string) error { f.calls = append(f.calls, "umount "+s+" "+target); return nil }
	dropSudo = func(s string) error {
		f.calls = append(f.calls, "sudo "+s)
		if f.sudoFail {
			return errors.New("sudo still works")
		}
		return nil
	}
	sbxExecStdin = func(s, script, stdin string) error {
		f.calls = append(f.calls, "exec "+s)
		f.notices = append(f.notices, stdin)
		return nil
	}
}

func hp(entry string) hidePath {
	p := "/w/app/" + entry
	return hidePath{Entry: entry, Host: p, Target: p}
}

func TestApplyHideLifecycle(t *testing.T) {
	f := &fakeHide{}
	f.install(t)
	call := func(hide []hidePath, fresh bool) {
		t.Helper()
		f.calls, f.notices = nil, nil
		capture(t, func() {
			if err := applyHide("app.claude", hide, fresh, false); err != nil {
				t.Fatal(err)
			}
		})
	}

	// A new sandbox: start, cover, sudo gone, the agent told.
	call([]hidePath{hp("dev")}, true)
	if strings.Join(f.calls, "; ") != "start app.claude; mount app.claude /w/app/dev; sudo app.claude; exec app.claude" ||
		!strings.Contains(f.notices[0], "`dev`") || !strings.Contains(f.notices[0], "sudo is not available") {
		t.Fatalf("new sandbox: %v %q", f.calls, f.notices)
	}

	// A resume with the same config: not a single sbx call.
	call([]hidePath{hp("dev")}, false)
	if len(f.calls) != 0 {
		t.Fatalf("resume: %v", f.calls)
	}

	// A new entry: start and mount it; sudo is already gone.
	call([]hidePath{hp("dev"), hp("secrets")}, false)
	if strings.Join(f.calls, "; ") != "start app.claude; mount app.claude /w/app/secrets; exec app.claude" {
		t.Fatalf("adding secrets: %v", f.calls)
	}

	// Dropping dev: unmounted (no start needed), its cover deleted, the other one kept.
	devCover, _ := coverDir("app.claude", "/w/app/dev")
	secretsCover, _ := coverDir("app.claude", "/w/app/secrets")
	call([]hidePath{hp("secrets")}, false)
	if strings.Join(f.calls, "; ") != "umount app.claude /w/app/dev; exec app.claude" {
		t.Fatalf("dropping dev: %v", f.calls)
	}
	if _, err := os.Stat(devCover); !os.IsNotExist(err) {
		t.Fatalf("dev's cover is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secretsCover, "HIDDEN.txt")); err != nil {
		t.Fatalf("secrets' cover went too: %v", err)
	}

	// Dropping everything: sudo stays off, and the notice says only that.
	var out string
	f.calls, f.notices = nil, nil
	out = capture(t, func() { applyHide("app.claude", nil, false, false) })
	if strings.Join(f.calls, "; ") != "umount app.claude /w/app/secrets; exec app.claude" ||
		strings.Contains(f.notices[0], "Hidden on purpose") || !strings.Contains(f.notices[0], "sudo is not available") ||
		!strings.Contains(out, "sudo stays off") {
		t.Fatalf("dropping all: %v %q\n%s", f.calls, f.notices, out)
	}

	// From then on nothing changes and nothing is called.
	call(nil, false)
	if len(f.calls) != 0 {
		t.Fatalf("after dropping all: %v", f.calls)
	}
}

func TestApplyHideNothingConfigured(t *testing.T) {
	f := &fakeHide{}
	f.install(t)
	if err := applyHide("app.claude", nil, true, false); err != nil || len(f.calls) != 0 {
		t.Fatalf("%v %v", f.calls, err)
	}
}

func TestApplyHideFailures(t *testing.T) {
	f := &fakeHide{mountFail: true}
	f.install(t)
	err := applyHide("app.claude", []hidePath{hp("dev")}, true, false)
	if err == nil || !strings.Contains(err.Error(), "not starting the sandbox with it visible") || f.count("sudo ") != 0 {
		t.Fatalf("a failed mount must stop the start: %v %v", err, f.calls)
	}

	f2 := &fakeHide{sudoFail: true}
	f2.install(t)
	err = applyHide("app.claude", []hidePath{hp("dev")}, true, false)
	if err == nil || !strings.Contains(err.Error(), "cannot remove sudo") {
		t.Fatalf("sudo that stays must stop the start: %v", err)
	}
	// The cover is recorded, so the next start does not mount it again but retries sudo.
	f2.sudoFail, f2.calls = false, nil
	capture(t, func() { err = applyHide("app.claude", []hidePath{hp("dev")}, false, false) })
	if err != nil || strings.Join(f2.calls, "; ") != "start app.claude; sudo app.claude; exec app.claude" {
		t.Fatalf("retry: %v %v", f2.calls, err)
	}

	// The sandbox stopped between the start and the mount: started again, mounted again.
	f3 := &fakeHide{notRunningOnce: true}
	f3.install(t)
	capture(t, func() { err = applyHide("app.claude", []hidePath{hp("dev")}, true, false) })
	if err != nil || f3.count("start ") != 2 || f3.count("mount ") != 2 {
		t.Fatalf("not running: %v %v", f3.calls, err)
	}
}

func (f *fakeHide) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestApplyHideDryRun(t *testing.T) {
	f := &fakeHide{}
	f.install(t)
	out := capture(t, func() { applyHide("app.claude", []hidePath{hp("dev")}, true, true) })
	if len(f.calls) != 0 ||
		!strings.Contains(out, "[dry-run] sbx exec app.claude true") ||
		!strings.Contains(out, ":/w/app/dev:ro") ||
		!strings.Contains(out, "sudo -n rm -f /etc/sudoers.d/agent") {
		t.Fatalf("dry-run: %v\n%s", f.calls, out)
	}
	if cover, _ := coverDir("app.claude", "/w/app/dev"); fileExists(cover) {
		t.Fatal("dry-run created the cover")
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// TestApplyHideFreshIgnoresAStaleRecord: a sandbox removed with `sbx rm` and created again under the same
// name has no covers and still has sudo.
func TestApplyHideFreshIgnoresAStaleRecord(t *testing.T) {
	f := &fakeHide{}
	f.install(t)
	capture(t, func() { applyHide("app.claude", []hidePath{hp("dev")}, true, false) })
	f.calls = nil
	capture(t, func() { applyHide("app.claude", []hidePath{hp("dev")}, true, false) })
	if f.count("mount ") != 1 || f.count("sudo ") != 1 || f.count("exec ") != 1 {
		t.Fatalf("fresh sandbox: %v", f.calls)
	}
}

// TestNoticeBlocksAreIndependent runs both real scripts: each replaces its own block only.
func TestNoticeBlocksAreIndependent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh and awk")
	}
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("no awk")
	}
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "CLAUDE.md")
	run := func(script, stdin string) string {
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(stdin)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		data, _ := os.ReadFile(path)
		return string(data)
	}
	run(noticeScript, restrictNotice(&RestrictConfig{NoPush: true}))
	run(hideNoticeScript, hideNotice([]hidePath{hp("dev")}, true))
	got := run(hideNoticeScript, hideNotice([]hidePath{hp("dev")}, true))
	if strings.Count(got, hideNoticeStart) != 1 || strings.Count(got, noticeStart) != 1 {
		t.Fatalf("after two writes:\n%s", got)
	}
	got = run(noticeScript, "")
	if strings.Contains(got, noticeStart) || !strings.Contains(got, "`dev`") {
		t.Fatalf("removing the restrict block touched the hide block:\n%s", got)
	}
}
