package main

// Hidden directories (`hide:` in the config): each path is covered inside the sandbox by a read-only
// `sbx mount` of an sbxup-owned directory holding only HIDDEN.txt, and the agent loses sudo, because
// `sudo umount` removes a cover (checked on a Windows host with sbx 0.48). The host's directory stays
// where it is and untouched; git does not notice, since such directories are git-ignored.
//
// Facts from the host this rests on:
//   - `sbx mount` needs a running sandbox ("409 … is not running"); `sbx exec <s> true` starts one, and
//     there is no `sbx start`. A missing target is created, in the sandbox and empty on the host.
//   - mounts are restored after stop/start; a saved mount whose source is gone stops the start.
//   - `sbx umount` revokes the source for every mount of it, so each cover has its own directory.
//   - `sudo rm /etc/sudoers.d/agent` is enough: the passwords of agent and root are locked.
//   - the first dynamic `sbx mount` brings /mnt/host (virtiofs, every allowed host path, the workspace
//     included), where a directory covered on the workspace path was still readable. So every path to a
//     hidden directory is covered and checked on each start (coverEverywhere).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// defaultHide is hidden when the config has no `hide:` key and the directory exists.
const defaultHide = "dev"

// hidePath is one resolved entry of `hide:`.
type hidePath struct {
	Entry  string // as configured, e.g. "dev"
	Host   string // absolute, symlinks resolved
	Target string // the same directory inside the sandbox
}

// hideEntries applies the default: no key hides `dev` when the project has one; `hide: []` hides nothing.
func hideEntries(cfg *[]string, project string) []string {
	if cfg != nil {
		return *cfg
	}
	if fi, err := os.Stat(filepath.Join(project, defaultHide)); err == nil && fi.IsDir() {
		return []string{defaultHide}
	}
	return nil
}

// resolveHide turns entries into host and sandbox paths. Errors: an entry that leaves the project through
// a symlink, one that overlaps the cache, a `mounts:` entry or another entry (two mounts on one path have
// no defined order), and clone mode, where the host repository is readable at /run/sandbox/source and a
// cover there is not verified.
func resolveHide(entries []string, project, cache string, mounts []Mount, clone bool) ([]hidePath, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if clone {
		return nil, errors.New("'hide' does not work with clone mode yet: the host repository stays readable " +
			"at /run/sandbox/source; turn clone off or set 'hide: []'")
	}
	root := resolveExisting(project)
	var out []hidePath
	for _, e := range entries {
		host := resolveExisting(filepath.Join(root, e))
		if samePath(host, root) || !within(host, root) {
			return nil, fmt.Errorf("'hide' entry %q resolves to %s, outside the project", e, host)
		}
		if cache != "" {
			if c := resolveExisting(cache); within(host, c) || within(c, host) {
				return nil, fmt.Errorf("'hide' entry %q overlaps the cache directory %s", e, c)
			}
		}
		for _, m := range mounts {
			if within(host, m.Path) || within(m.Path, host) {
				return nil, fmt.Errorf("'hide' entry %q overlaps the mount %s", e, m.Path)
			}
		}
		for _, o := range out {
			if within(host, o.Host) || within(o.Host, host) {
				return nil, fmt.Errorf("'hide' entries %q and %q overlap; keep the outer one", o.Entry, e)
			}
		}
		target, err := sandboxPath(host, runtime.GOOS)
		if err != nil {
			return nil, fmt.Errorf("'hide' entry %q: %v", e, err)
		}
		out = append(out, hidePath{Entry: e, Host: host, Target: target})
	}
	return out, nil
}

// sandboxPath is where sbx shows a host path: the same path, and on Windows the drive letter as a root
// directory (W:\tmp\12 → /w/tmp/12, as `sbx create` prints it).
func sandboxPath(host, goos string) (string, error) {
	if goos != "windows" {
		return host, nil
	}
	if strings.HasPrefix(host, `\\`) || len(host) < 3 || host[1] != ':' || !isASCIILetter(host[0]) {
		return "", fmt.Errorf("%s is not on a drive letter; sbx cannot mount it", host)
	}
	return "/" + strings.ToLower(host[:1]) + strings.ReplaceAll(host[2:], `\`, "/"), nil
}

// hiddenText is the only file the agent sees in a hidden directory.
const hiddenText = "This directory is hidden from the agent on purpose by sbxup ('hide:' in .sbx/sbxup.config.yaml).\n" +
	"Its real content stays on the host. Do not try to reach it; if a task needs it, ask the user.\n"

// hideBase is the user's sbxup/hide directory: a cover per sandbox and target, and the records. In the
// config directory, not the cache: clearing a cache must not break a sandbox's saved mounts.
func hideBase() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a config directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "hide"), nil
}

// coverDir is the cover of one target. Hashed, since folding '/' would make "a/b" and "a-b" collide.
func coverDir(sandbox, target string) (string, error) {
	base, err := hideBase()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(base, strings.ToLower(sandbox), hex.EncodeToString(sum[:])[:12]), nil
}

// ensureCover creates the cover with HIDDEN.txt and returns its resolved path, which is what is mounted.
func ensureCover(sandbox, target string) (string, error) {
	dir, err := coverDir(sandbox, target)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	file := filepath.Join(dir, "HIDDEN.txt")
	if b, err := os.ReadFile(file); err != nil || string(b) != hiddenText {
		if err := os.WriteFile(file, []byte(hiddenText), 0o644); err != nil {
			return "", err
		}
	}
	return filepath.EvalSymlinks(dir)
}

func sbxCmd(args ...string) error {
	out, err := exec.Command("sbx", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sbx %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

var (
	// sbxStart starts a stopped sandbox: there is no `sbx start`, and `sbx exec` starts one.
	sbxStart = func(sandbox string) error { return sbxCmd("exec", sandbox, "true") }
	sbxMount = func(sandbox, src, target string) error {
		return sbxCmd("mount", sandbox, src+":"+target+":ro")
	}
	// sbxUmount passes the target back, as `sbx umount --help` requires; it works on a stopped sandbox.
	sbxUmount = func(sandbox, src, target string) error {
		return sbxCmd("umount", sandbox, src+":"+target)
	}
	// dropSudo removes the agent's sudo and checks it is gone. Repeating it is fine: when sudo already
	// fails there is nothing to remove.
	dropSudo = func(sandbox string) error {
		if sbxCmd("exec", sandbox, "sudo", "-n", "true") != nil {
			return nil
		}
		if err := sbxCmd("exec", sandbox, "sudo", "-n", "rm", "-f", "/etc/sudoers.d/agent"); err != nil {
			return err
		}
		if sbxCmd("exec", sandbox, "sudo", "-n", "true") == nil {
			return errors.New("sudo still works after removing /etc/sudoers.d/agent")
		}
		return nil
	}
	// sbxFindmnt lists the sandbox's mounts.
	sbxFindmnt = func(sandbox string) ([]sbxMountInfo, error) {
		out, err := sbxOutput("exec", sandbox, "findmnt", "-J", "-o", "TARGET,SOURCE,FSTYPE")
		if err != nil {
			return nil, err
		}
		return parseFindmnt(out)
	}
	// sbxListDir lists a directory in the sandbox, hidden entries included.
	sbxListDir = func(sandbox, dir string) ([]string, error) {
		out, err := sbxOutput("exec", sandbox, "ls", "-A", dir)
		if err != nil {
			return nil, err
		}
		return strings.Fields(string(out)), nil
	}
)

// sbxMountInfo is one line of `findmnt` inside the sandbox.
type sbxMountInfo struct {
	Target string `json:"target"`
	Source string `json:"source"`
	FSType string `json:"fstype"`
}

// parseFindmnt flattens `findmnt -J`: {"filesystems": [{..., "children": [...]}]}.
func parseFindmnt(data []byte) ([]sbxMountInfo, error) {
	type node struct {
		sbxMountInfo
		Children []node `json:"children"`
	}
	var doc struct {
		Filesystems *[]node `json:"filesystems"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Filesystems == nil {
		return nil, errors.New("not findmnt -J output")
	}
	var out []sbxMountInfo
	var walk func([]node)
	walk = func(ns []node) {
		for _, n := range ns {
			out = append(out, n.sbxMountInfo)
			walk(n.Children)
		}
	}
	walk(*doc.Filesystems)
	return out, nil
}

// hostSource is the host directory behind a virtiofs mount, in sbx's form: `host` is the whole host
// (/mnt/host), `host[/w/app]` one directory. Anything else is not from the host.
func hostSource(m sbxMountInfo) (string, bool) {
	if m.FSType != "virtiofs" {
		return "", false
	}
	switch {
	case m.Source == "host":
		return "/", true
	case strings.HasPrefix(m.Source, "host[") && strings.HasSuffix(m.Source, "]"):
		return m.Source[len("host[") : len(m.Source)-1], true
	}
	return "", false
}

// withinSlash is `within` for sandbox paths, which use '/' on every host; fold ignores case, as the
// host's file system does on Windows and macOS.
func withinSlash(child, parent string, fold bool) (string, bool) {
	c, p := path.Clean(child), path.Clean(parent)
	if fold {
		c, p = strings.ToLower(c), strings.ToLower(p)
	}
	if p == "/" {
		return strings.TrimPrefix(path.Clean(child), "/"), true
	}
	if c == p {
		return "", true
	}
	if strings.HasPrefix(c, p+"/") {
		return path.Clean(child)[len(p)+1:], true
	}
	return "", false
}

// aliasesOf is every path in the sandbox where the host directory target (in sbx's form) can be read:
// under each mount from the host whose source contains it. The workspace is one; since sbx 0.48 a
// dynamic `sbx mount` also brings /mnt/host with every allowed host path, workspace included.
func aliasesOf(target string, mounts []sbxMountInfo, fold bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mounts {
		src, ok := hostSource(m)
		if !ok || isCover(m) {
			continue
		}
		if rel, ok := withinSlash(target, src, fold); ok {
			a := path.Join(m.Target, rel)
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	return out
}

// isCover tells sbxup's covers apart from mounts that show host files.
func isCover(m sbxMountInfo) bool { return strings.Contains(m.Source, "/sbxup/hide/") }

func coveredAt(alias string, mounts []sbxMountInfo) bool {
	for _, m := range mounts {
		if m.Target == alias && isCover(m) {
			return true
		}
	}
	return false
}

// hideMount is one cover sbxup made. Of is the configured target an extra cover belongs to (another
// path to the same host directory); empty for the configured target itself.
type hideMount struct {
	Cover  string `json:"cover"`
	Target string `json:"target"`
	Of     string `json:"of,omitempty"`
}

func (m hideMount) owner() string {
	if m.Of != "" {
		return m.Of
	}
	return m.Target
}

// hideRecord is what sbxup did to one sandbox.
type hideRecord struct {
	Sandbox     string      `json:"sandbox"`
	Mounts      []hideMount `json:"mounts"`
	SudoDropped bool        `json:"sudoDropped"`
	Notice      string      `json:"notice,omitempty"`
}

func hideRecordPath(sandbox string) (string, error) {
	base, err := hideBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, strings.ToLower(sandbox)+".json"), nil
}

func loadHideRecord(sandbox string) hideRecord {
	rec := hideRecord{Sandbox: sandbox}
	if path, err := hideRecordPath(sandbox); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &rec)
		}
	}
	return rec
}

func saveHideRecord(rec hideRecord) error {
	path, err := hideRecordPath(rec.Sandbox)
	if err != nil {
		return err
	}
	if len(rec.Mounts) == 0 && !rec.SudoDropped && rec.Notice == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return writeCache(path, data)
}

const (
	hideNoticeStart = "<!-- sbxup:hide -->"
	hideNoticeEnd   = "<!-- /sbxup:hide -->"
)

var hideNoticeScript = blockScript(hideNoticeStart, hideNoticeEnd)

// hideNotice tells the agent why a directory shows only HIDDEN.txt and sudo asks for a password.
func hideNotice(hide []hidePath, sudoOff bool) string {
	if len(hide) == 0 && !sudoOff {
		return ""
	}
	var b strings.Builder
	b.WriteString(hideNoticeStart + "\n")
	b.WriteString("## Hidden directories (set by sbxup from .sbx/sbxup.config.yaml)\n\n")
	if len(hide) > 0 {
		names := make([]string, len(hide))
		for i, h := range hide {
			names[i] = "`" + filepath.ToSlash(h.Entry) + "`"
		}
		b.WriteString("- Hidden on purpose: " + strings.Join(names, ", ") + ". Each shows only HIDDEN.txt; the real " +
			"content stays on the host. Do not try to read, recreate or unmount them; if a task needs them, tell the user.\n")
	}
	if sudoOff {
		b.WriteString("- sudo is not available in this sandbox (removed so the hidden directories stay hidden). " +
			"Do not look for another way to become root; if a task needs a system package, tell the user.\n")
	}
	b.WriteString(hideNoticeEnd + "\n")
	return b.String()
}

// mountCover mounts one cover, retrying once when the sandbox stopped between the start and the mount
// (seen on a host).
func mountCover(sandbox, cover, target string) error {
	err := sbxMount(sandbox, cover, target)
	if err != nil && strings.Contains(err.Error(), "not running") {
		if sbxStart(sandbox) == nil {
			err = sbxMount(sandbox, cover, target)
		}
	}
	return err
}

// coverEverywhere covers every other path to the hidden directories and then checks each path shows only
// HIDDEN.txt. On a host (sbx 0.48) the cover on the workspace path was not enough: the first dynamic
// `sbx mount` brings /mnt/host, where the same directory was readable. A new cover can bring new paths, so
// it repeats until nothing is left to cover. Anything it cannot check stops the start.
func coverEverywhere(sandbox string, hide []hidePath, rec *hideRecord) error {
	fold := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	for round := 0; round < 4; round++ {
		mounts, err := sbxFindmnt(sandbox)
		if err != nil {
			return fmt.Errorf("cannot list the sandbox's mounts to check the hidden directories (%v); not starting the sandbox", err)
		}
		type extra struct{ alias, of string }
		var missing []extra
		for _, h := range hide {
			for _, a := range aliasesOf(h.Target, mounts, fold) {
				if !coveredAt(a, mounts) {
					missing = append(missing, extra{a, h.Target})
				}
			}
		}
		if len(missing) == 0 {
			for _, h := range hide {
				paths := []string{h.Target}
				for _, a := range aliasesOf(h.Target, mounts, fold) {
					if a != h.Target {
						paths = append(paths, a)
					}
				}
				for _, a := range paths {
					entries, err := sbxListDir(sandbox, a)
					if err != nil || len(entries) != 1 || entries[0] != "HIDDEN.txt" {
						return fmt.Errorf("%s is still readable in the sandbox at %s (%v %v); not starting the sandbox",
							h.Entry, a, entries, err)
					}
				}
			}
			return nil
		}
		for _, x := range missing {
			cover, err := ensureCover(sandbox, x.alias)
			if err != nil {
				return fmt.Errorf("cannot prepare a cover for %s (%v); not starting the sandbox", x.alias, err)
			}
			if err := mountCover(sandbox, cover, x.alias); err != nil {
				return fmt.Errorf("cannot cover %s, another path to a hidden directory (%v); not starting the sandbox", x.alias, err)
			}
			rec.Mounts = append(rec.Mounts, hideMount{Cover: cover, Target: x.alias, Of: x.of})
		}
	}
	return errors.New("the hidden directories keep appearing at new paths in the sandbox; not starting it")
}

// applyHide brings the sandbox's covers in line with the config: it removes covers sbxup made that the
// config no longer lists, covers new entries and every other path to them, drops sudo once, checks that
// nothing hidden is readable, and keeps the agent's notice in step. The check runs on every start, so a
// sandbox with something hidden is always started first. A sandbox with nothing configured and nothing
// recorded costs no sbx call. fresh is set for a sandbox sbxup has just created, so a record left by an
// earlier sandbox of the same name does not count.
func applyHide(sandbox string, hide []hidePath, fresh, dryRun bool) error {
	rec := loadHideRecord(sandbox)
	if fresh {
		rec = hideRecord{Sandbox: sandbox}
	}
	if len(hide) == 0 && len(rec.Mounts) == 0 && !rec.SudoDropped && rec.Notice == "" {
		return nil
	}
	wanted := map[string]bool{}
	for _, h := range hide {
		wanted[h.Target] = true
	}
	have := map[string]bool{}
	for _, m := range rec.Mounts {
		if m.Of == "" {
			have[m.Target] = true
		}
	}
	var add []hidePath
	for _, h := range hide {
		if !have[h.Target] {
			add = append(add, h)
		}
	}
	needSudo := len(hide) > 0 && !rec.SudoDropped

	if dryRun {
		for _, m := range rec.Mounts {
			if !wanted[m.owner()] {
				fmt.Printf("[dry-run] sbx umount %s %s:%s\n", sandbox, m.Cover, m.Target)
			}
		}
		if len(hide) > 0 {
			fmt.Printf("[dry-run] sbx exec %s true\n", sandbox)
		}
		for _, h := range add {
			cover, err := coverDir(sandbox, h.Target)
			if err != nil {
				return err
			}
			fmt.Printf("[dry-run] sbx mount %s %s:%s:ro\n", sandbox, cover, h.Target)
		}
		if len(hide) > 0 {
			fmt.Printf("[dry-run] sbx exec %s findmnt -J …  # cover every other path to them, check each shows only HIDDEN.txt\n", sandbox)
		}
		if needSudo {
			fmt.Printf("[dry-run] sbx exec %s sudo -n rm -f /etc/sudoers.d/agent\n", sandbox)
		}
		return nil
	}

	var kept []hideMount
	removed := false
	for _, m := range rec.Mounts {
		if wanted[m.owner()] {
			kept = append(kept, m)
			continue
		}
		// Left in place, a cover only hides more, so a failure here is a warning.
		if err := sbxUmount(sandbox, m.Cover, m.Target); err != nil {
			warnf("cannot stop hiding %s: %v", m.Target, err)
			kept = append(kept, m)
			continue
		}
		os.RemoveAll(m.Cover)
		removed = true
	}
	rec.Mounts = kept

	// Every cover must exist before the sandbox starts: sbx refuses to restore a mount without its source.
	for _, m := range rec.Mounts {
		if _, err := ensureCover(sandbox, m.Target); err != nil {
			return fmt.Errorf("cannot prepare the cover for %s (%v); not starting the sandbox", m.Target, err)
		}
	}
	covers := map[string]string{}
	for _, h := range hide {
		cover, err := ensureCover(sandbox, h.Target)
		if err != nil {
			return fmt.Errorf("cannot prepare the cover for %s (%v); not starting the sandbox", h.Entry, err)
		}
		covers[h.Target] = cover
	}

	err := func() error {
		if len(hide) == 0 {
			return nil
		}
		if err := sbxStart(sandbox); err != nil {
			return fmt.Errorf("cannot start the sandbox to hide directories: %v", err)
		}
		for _, h := range add {
			if err := mountCover(sandbox, covers[h.Target], h.Target); err != nil {
				return fmt.Errorf("cannot hide %s (%v); not starting the sandbox with it visible", h.Entry, err)
			}
			rec.Mounts = append(rec.Mounts, hideMount{Cover: covers[h.Target], Target: h.Target})
		}
		if err := coverEverywhere(sandbox, hide, &rec); err != nil {
			return err
		}
		if needSudo {
			if err := dropSudo(sandbox); err != nil {
				return fmt.Errorf("cannot remove sudo (%v); not starting the sandbox, since sudo can unmount the cover", err)
			}
			rec.SudoDropped = true
		}
		return nil
	}()
	if err != nil {
		saveHideRecord(rec)
		return err
	}

	if notice := hideNotice(hide, rec.SudoDropped); notice != rec.Notice {
		if err := sbxExecStdin(sandbox, hideNoticeScript, notice); err != nil {
			warnf("cannot tell the agent about the hidden directories: %v", err)
		} else {
			rec.Notice = notice
		}
	}
	if err := saveHideRecord(rec); err != nil {
		warnf("cannot record the hidden directories (%v); sbxup will not remove their covers if the config drops them.", err)
	}
	if len(hide) > 0 {
		names := make([]string, len(hide))
		for i, h := range hide {
			names[i] = filepath.ToSlash(h.Entry)
		}
		fmt.Printf("Hidden: %s (sudo off in %s)\n", strings.Join(names, ", "), sandbox)
	} else if removed && rec.SudoDropped {
		fmt.Printf("Nothing hidden, but sudo stays off in %s; 'sbx rm %s' and a new start bring it back.\n", sandbox, sandbox)
	}
	return nil
}
