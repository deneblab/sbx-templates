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

// fakeHide stubs every sbx call applyHide makes, records them, and keeps a sandbox's mount table the way
// sbx 0.48 behaved on a host: the workspace at /w/app, and /mnt/host (the whole allowlist) once the first
// dynamic mount exists. A directory lists "secret.txt" unless a cover sits exactly on it.
type fakeHide struct {
	mountFail, sudoFail, notRunningOnce bool
	noMntHost, leak, findmntFail        bool
	calls, notices                      []string
	mounts                              []sbxMountInfo
}

func (f *fakeHide) reset() {
	f.mounts = []sbxMountInfo{
		{Target: "/", Source: "overlay", FSType: "overlay"},
		{Target: "/w/app", Source: "host[/w/app]", FSType: "virtiofs"},
		{Target: "/etc/resolv.conf", Source: "bind-95f0[/resolv.conf]", FSType: "virtiofs"},
	}
}

func (f *fakeHide) install(t *testing.T) {
	t.Helper()
	configHome(t)
	f.reset()
	origStart, origMount, origUmount, origSudo, origExec := sbxStart, sbxMount, sbxUmount, dropSudo, sbxExecStdin
	origFind, origLs := sbxFindmnt, sbxListDir
	t.Cleanup(func() {
		sbxStart, sbxMount, sbxUmount, dropSudo, sbxExecStdin = origStart, origMount, origUmount, origSudo, origExec
		sbxFindmnt, sbxListDir = origFind, origLs
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
		if !f.noMntHost && !f.has("/mnt/host") {
			f.mounts = append(f.mounts, sbxMountInfo{Target: "/mnt/host", Source: "host", FSType: "virtiofs"})
		}
		f.mounts = append(f.mounts, sbxMountInfo{Target: target, Source: "host[/c/u/sbxup/hide/" + filepath.Base(src) + "]", FSType: "virtiofs"})
		return nil
	}
	sbxUmount = func(s, src, target string) error {
		f.calls = append(f.calls, "umount "+s+" "+target)
		for i, m := range f.mounts {
			if m.Target == target {
				f.mounts = append(f.mounts[:i], f.mounts[i+1:]...)
				break
			}
		}
		return nil
	}
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
	sbxFindmnt = func(s string) ([]sbxMountInfo, error) {
		f.calls = append(f.calls, "findmnt "+s)
		if f.findmntFail {
			return nil, errors.New("no findmnt")
		}
		return append([]sbxMountInfo(nil), f.mounts...), nil
	}
	sbxListDir = func(s, dir string) ([]string, error) {
		f.calls = append(f.calls, "ls "+dir)
		if !f.leak && coveredAt(dir, f.mounts) {
			return []string{"HIDDEN.txt"}, nil
		}
		return []string{"secret.txt"}, nil
	}
}

func (f *fakeHide) has(target string) bool {
	for _, m := range f.mounts {
		if m.Target == target {
			return true
		}
	}
	return false
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

func hp(entry string) hidePath {
	p := "/w/app/" + entry
	return hidePath{Entry: entry, Host: p, Target: p}
}

func TestParseFindmntAndAliases(t *testing.T) {
	// `findmnt -J -o TARGET,SOURCE,FSTYPE` from the host test (antipiracy.RobeNova.claude), trimmed.
	const out = `{"filesystems": [{"target":"/","source":"overlay","fstype":"overlay","children":[
	  {"target":"/mnt/host","source":"host","fstype":"virtiofs"},
	  {"target":"/w/antipiracy/RobeNova","source":"host[/w/antipiracy/RobeNova]","fstype":"virtiofs","children":[
	    {"target":"/w/antipiracy/RobeNova/dev","source":"host[/c/Users/pk/AppData/Roaming/sbxup/hide/antipiracy.robenova.claude/aebcc21bf842]","fstype":"virtiofs"}]},
	  {"target":"/run","source":"tmpfs","fstype":"tmpfs","children":[
	    {"target":"/run/sbx/skills-shared","source":"host[/c/Users/pk/AppData/Local/DockerSandboxes/sandboxes/state/agent-skills]","fstype":"virtiofs"}]},
	  {"target":"/etc/resolv.conf","source":"bind-95f03ffa45a7d998[/resolv.conf]","fstype":"virtiofs"}]}]}`
	mounts, err := parseFindmnt([]byte(out))
	if err != nil || len(mounts) != 7 {
		t.Fatalf("%d mounts, %v", len(mounts), err)
	}
	got := aliasesOf("/w/antipiracy/RobeNova/dev", mounts, true)
	if strings.Join(got, " ") != "/mnt/host/w/antipiracy/RobeNova/dev /w/antipiracy/RobeNova/dev" {
		t.Fatalf("aliases %v", got)
	}
	if !coveredAt("/w/antipiracy/RobeNova/dev", mounts) || coveredAt("/mnt/host/w/antipiracy/RobeNova/dev", mounts) {
		t.Fatal("coveredAt")
	}
	// Case differs between the config and sbx's view on Windows; folded there, not on Linux.
	if len(aliasesOf("/w/antipiracy/robenova/dev", mounts, true)) != 2 || len(aliasesOf("/w/antipiracy/robenova/dev", mounts, false)) != 1 {
		t.Fatal("case folding")
	}
	if _, err := parseFindmnt([]byte(`[]`)); err == nil {
		t.Fatal("accepted a non-findmnt document")
	}
}

func TestApplyHideLifecycle(t *testing.T) {
	f := &fakeHide{}
	f.install(t)
	call := func(hide []hidePath, fresh bool) string {
		t.Helper()
		f.calls, f.notices = nil, nil
		capture(t, func() {
			if err := applyHide("app.claude", hide, fresh, false); err != nil {
				t.Fatal(err)
			}
		})
		return strings.Join(f.calls, "; ")
	}

	// A new sandbox: start, cover, cover /mnt/host which the first cover brought, check both, sudo, notice.
	if got := call([]hidePath{hp("dev")}, true); got != "start app.claude; mount app.claude /w/app/dev; "+
		"findmnt app.claude; mount app.claude /mnt/host/w/app/dev; findmnt app.claude; "+
		"ls /w/app/dev; ls /mnt/host/w/app/dev; sudo app.claude; exec app.claude" {
		t.Fatalf("new sandbox: %s", got)
	}
	if !strings.Contains(f.notices[0], "`dev`") || !strings.Contains(f.notices[0], "sudo is not available") {
		t.Fatalf("notice %q", f.notices)
	}

	// A resume with the same config: nothing mounted, but checked again.
	if got := call([]hidePath{hp("dev")}, false); got != "start app.claude; findmnt app.claude; ls /w/app/dev; ls /mnt/host/w/app/dev" {
		t.Fatalf("resume: %s", got)
	}

	// A new entry: covered on both paths; sudo is already gone.
	if got := call([]hidePath{hp("dev"), hp("secrets")}, false); got != "start app.claude; mount app.claude /w/app/secrets; "+
		"findmnt app.claude; mount app.claude /mnt/host/w/app/secrets; findmnt app.claude; "+
		"ls /w/app/dev; ls /mnt/host/w/app/dev; ls /w/app/secrets; ls /mnt/host/w/app/secrets; exec app.claude" {
		t.Fatalf("adding secrets: %s", got)
	}

	// Dropping dev: both of its covers go and their directories are deleted; secrets stays and is checked.
	devCover, _ := coverDir("app.claude", "/w/app/dev")
	devExtra, _ := coverDir("app.claude", "/mnt/host/w/app/dev")
	secretsCover, _ := coverDir("app.claude", "/w/app/secrets")
	if got := call([]hidePath{hp("secrets")}, false); got != "umount app.claude /w/app/dev; umount app.claude /mnt/host/w/app/dev; "+
		"start app.claude; findmnt app.claude; ls /w/app/secrets; ls /mnt/host/w/app/secrets; exec app.claude" {
		t.Fatalf("dropping dev: %s", got)
	}
	if fileExists(devCover) || fileExists(devExtra) || !fileExists(filepath.Join(secretsCover, "HIDDEN.txt")) {
		t.Fatal("covers after dropping dev")
	}

	// Dropping everything: no start, sudo stays off, and the notice says only that.
	var out string
	f.calls, f.notices = nil, nil
	out = capture(t, func() { applyHide("app.claude", nil, false, false) })
	if strings.Join(f.calls, "; ") != "umount app.claude /w/app/secrets; umount app.claude /mnt/host/w/app/secrets; exec app.claude" ||
		strings.Contains(f.notices[0], "Hidden on purpose") || !strings.Contains(f.notices[0], "sudo is not available") ||
		!strings.Contains(out, "sudo stays off") {
		t.Fatalf("dropping all: %v %q\n%s", f.calls, f.notices, out)
	}

	// From then on nothing changes and nothing is called.
	if got := call(nil, false); got != "" {
		t.Fatalf("after dropping all: %s", got)
	}
}

func TestApplyHideWithoutMntHost(t *testing.T) {
	// An sbx that brings no /mnt/host: one cover, one check.
	f := &fakeHide{noMntHost: true}
	f.install(t)
	capture(t, func() {
		if err := applyHide("app.claude", []hidePath{hp("dev")}, true, false); err != nil {
			t.Fatal(err)
		}
	})
	if f.count("mount ") != 1 || f.count("ls ") != 1 {
		t.Fatalf("%v", f.calls)
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

	// A directory still readable somewhere: no sudo removal, no start.
	fl := &fakeHide{leak: true}
	fl.install(t)
	err = applyHide("app.claude", []hidePath{hp("dev")}, true, false)
	if err == nil || !strings.Contains(err.Error(), "still readable in the sandbox at /w/app/dev") || fl.count("sudo ") != 0 {
		t.Fatalf("a readable directory must stop the start: %v %v", err, fl.calls)
	}

	ff := &fakeHide{findmntFail: true}
	ff.install(t)
	err = applyHide("app.claude", []hidePath{hp("dev")}, true, false)
	if err == nil || !strings.Contains(err.Error(), "cannot list the sandbox's mounts") {
		t.Fatalf("no way to check must stop the start: %v", err)
	}

	f2 := &fakeHide{sudoFail: true}
	f2.install(t)
	err = applyHide("app.claude", []hidePath{hp("dev")}, true, false)
	if err == nil || !strings.Contains(err.Error(), "cannot remove sudo") {
		t.Fatalf("sudo that stays must stop the start: %v", err)
	}
	// Both covers are recorded, so the next start mounts nothing but retries sudo.
	f2.sudoFail, f2.calls = false, nil
	capture(t, func() { err = applyHide("app.claude", []hidePath{hp("dev")}, false, false) })
	if err != nil || f2.count("mount ") != 0 || f2.count("sudo ") != 1 {
		t.Fatalf("retry: %v %v", f2.calls, err)
	}

	// The sandbox stopped between the start and the mount: started again, mounted again.
	f3 := &fakeHide{notRunningOnce: true}
	f3.install(t)
	capture(t, func() { err = applyHide("app.claude", []hidePath{hp("dev")}, true, false) })
	if err != nil || f3.count("start ") != 2 || f3.count("mount app.claude /w/app/dev") != 2 {
		t.Fatalf("not running: %v %v", f3.calls, err)
	}
}

func TestApplyHideDryRun(t *testing.T) {
	f := &fakeHide{}
	f.install(t)
	out := capture(t, func() { applyHide("app.claude", []hidePath{hp("dev")}, true, true) })
	if len(f.calls) != 0 ||
		!strings.Contains(out, "[dry-run] sbx exec app.claude true") ||
		!strings.Contains(out, ":/w/app/dev:ro") ||
		!strings.Contains(out, "findmnt") ||
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
	f.reset()
	capture(t, func() { applyHide("app.claude", []hidePath{hp("dev")}, true, false) })
	if f.count("mount ") != 2 || f.count("sudo ") != 1 || f.count("exec ") != 1 {
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
