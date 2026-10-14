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

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
)

// hideMount is one cover sbxup made.
type hideMount struct {
	Cover  string `json:"cover"`
	Target string `json:"target"`
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

// applyHide brings the sandbox's covers in line with the config: it removes covers sbxup made that the
// config no longer lists, covers new entries, drops sudo once, and keeps the agent's notice in step. A
// sandbox with nothing configured and nothing recorded costs no sbx call. fresh is set for a sandbox sbxup
// has just created, so a record left by an earlier sandbox of the same name does not count.
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
		have[m.Target] = true
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
			if !wanted[m.Target] {
				fmt.Printf("[dry-run] sbx umount %s %s:%s\n", sandbox, m.Cover, m.Target)
			}
		}
		if len(add) > 0 || needSudo {
			fmt.Printf("[dry-run] sbx exec %s true\n", sandbox)
		}
		for _, h := range add {
			cover, err := coverDir(sandbox, h.Target)
			if err != nil {
				return err
			}
			fmt.Printf("[dry-run] sbx mount %s %s:%s:ro\n", sandbox, cover, h.Target)
		}
		if needSudo {
			fmt.Printf("[dry-run] sbx exec %s sudo -n rm -f /etc/sudoers.d/agent\n", sandbox)
		}
		return nil
	}

	var kept []hideMount
	removed := false
	for _, m := range rec.Mounts {
		if wanted[m.Target] {
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
	covers := map[string]string{}
	for _, h := range hide {
		cover, err := ensureCover(sandbox, h.Target)
		if err != nil {
			return fmt.Errorf("cannot prepare the cover for %s (%v); not starting the sandbox", h.Entry, err)
		}
		covers[h.Target] = cover
	}

	err := func() error {
		if len(add) == 0 && !needSudo {
			return nil
		}
		if err := sbxStart(sandbox); err != nil {
			return fmt.Errorf("cannot start the sandbox to hide directories: %v", err)
		}
		for _, h := range add {
			err := sbxMount(sandbox, covers[h.Target], h.Target)
			if err != nil && strings.Contains(err.Error(), "not running") {
				// It can stop between the start and the mount; seen on a host.
				if sbxStart(sandbox) == nil {
					err = sbxMount(sandbox, covers[h.Target], h.Target)
				}
			}
			if err != nil {
				return fmt.Errorf("cannot hide %s (%v); not starting the sandbox with it visible", h.Entry, err)
			}
			rec.Mounts = append(rec.Mounts, hideMount{Cover: covers[h.Target], Target: h.Target})
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
