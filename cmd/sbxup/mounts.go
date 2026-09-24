package main

// Extra host directories declared in the config's `mounts:` list.
//
// The syntax is the one `sbx run` already takes for an extra workspace — `path` or `path:ro` — so
// sbxup passes it through rather than translating it. What sbxup adds is care, because the config
// lives in the repository and the agent runs with permissions off: a project can arrive with
// someone else's config. Two layers guard that (see checkMountAllowed and approveMounts).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Mount is one resolved entry of `mounts:`.
type Mount struct {
	Path     string // absolute, cleaned, symlinks resolved
	ReadOnly bool
}

// Spec is the form `sbx run` takes as a workspace argument.
func (m Mount) Spec() string {
	if m.ReadOnly {
		return m.Path + ":ro"
	}
	return m.Path
}

// Mode names the access for messages.
func (m Mount) Mode() string {
	if m.ReadOnly {
		return "read-only"
	}
	return "read-write"
}

// parseMountSpec splits `path[:ro|:rw]`. `:ro` is what sbx understands; `:rw` is accepted so a
// config can say what it does, and is dropped, since sbx knows no such suffix. Any other suffix is
// an error: a typo such as `:r0` must not quietly mount a directory writable.
//
// The suffix is the text after the last colon when it holds no path separator and the colon is not
// a drive-letter colon, so `D:\data` and `D:\data:ro` both parse.
func parseMountSpec(spec string) (path string, readOnly bool, err error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return "", false, fmt.Errorf("an empty mount")
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		suffix := s[i+1:]
		driveLetter := i == 1 && isASCIILetter(s[0])
		if !driveLetter && !strings.ContainsAny(suffix, `/\`) {
			switch strings.ToLower(suffix) {
			case "ro":
				readOnly = true
			case "rw":
			default:
				return "", false, fmt.Errorf("unknown mode %q after ':' (use ':ro', or ':rw' / nothing for read-write)", suffix)
			}
			s = strings.TrimSpace(s[:i])
		}
	}
	if s == "" {
		return "", false, fmt.Errorf("no path before the mode")
	}
	return s, readOnly, nil
}

func isASCIILetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

// expandMountPath makes p absolute: `~` is the home directory, anything else relative is relative
// to the project directory (like `cache`). No environment variables — $HOME and %USERPROFILE% mean
// different things on different systems.
func expandMountPath(p, home, project string) string {
	switch {
	case p == "~":
		p = home
	case strings.HasPrefix(p, "~/"), strings.HasPrefix(p, `~\`):
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(project, p)
	}
	return filepath.Clean(p)
}

// resolveExisting follows symlinks in the longest prefix of p that exists, so a path that is not
// there yet is still judged by where it would land. Refusing `~/link` while allowing it to point at
// ~/.ssh would be no refusal at all.
func resolveExisting(p string) string {
	rest, cur := "", p
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// within reports whether child is parent itself or lies under it. Case is ignored where the file
// system ignores it.
func within(child, parent string) bool {
	norm := func(p string) string {
		p = filepath.Clean(p)
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			p = strings.ToLower(p)
		}
		return p
	}
	c, pr := norm(child), norm(parent)
	if c == pr {
		return true
	}
	rel, err := filepath.Rel(pr, c)
	if err != nil {
		return false // a different volume
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func samePath(a, b string) bool { return within(a, b) && within(b, a) }

// credentialDirs are home-relative directories that hold secrets. The list catches accidents; it
// is not complete and does not pretend to be.
var credentialDirs = []string{
	".ssh", ".aws", ".gnupg", ".claude", ".azure", ".kube", ".docker", ".password-store",
	filepath.Join(".config", "gcloud"), filepath.Join(".config", "gh"),
}

// checkMountAllowed is layer 1: a hard refusal, with or without `:ro` because a read leaks too.
//
// It is about the home directory itself, not everything under it: `~/shared-libs` is fine. A path
// is refused when it is or contains the home directory (which takes in a filesystem root), or when
// it is, contains or lies inside a credential directory. It judges the path alone — the directory
// need not exist — and both p and home must already have their symlinks resolved.
func checkMountAllowed(p, home string) error {
	if within(home, p) {
		return fmt.Errorf("%s is, or contains, your home directory", p)
	}
	for _, rel := range credentialDirs {
		c := filepath.Join(home, rel)
		for _, cand := range []string{c, resolveExisting(c)} {
			if samePath(p, cand) {
				return fmt.Errorf("%s holds credentials", p)
			}
			if within(p, cand) || within(cand, p) {
				return fmt.Errorf("%s overlaps %s, which holds credentials", p, cand)
			}
		}
	}
	return nil
}

// resolveMounts turns config entries into mounts: parse, expand, resolve, refuse, then drop the
// duplicates and the directories that are not there. A refusal is an error; a missing directory is
// a warning, because the config is shared and the directory may exist on one machine only.
// skip lists paths that are mounted anyway (the project, the cache).
func resolveMounts(specs []string, home, project string, skip ...string) ([]Mount, error) {
	home = resolveExisting(home)
	var out []Mount
	for _, spec := range specs {
		raw, ro, err := parseMountSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("mount %q: %w", spec, err)
		}
		p := resolveExisting(expandMountPath(raw, home, project))
		if err := checkMountAllowed(p, home); err != nil {
			return nil, fmt.Errorf("mount %q refused: %v. sbxup never mounts the home directory or a "+
				"credential directory; mount a folder inside the home directory instead", spec, err)
		}

		dup := false
		for _, s := range skip {
			if s != "" && samePath(p, s) {
				warnf("Mount %q is already mounted (the project or its cache) — skipping.", spec)
				dup = true
			}
		}
		for _, m := range out {
			if samePath(p, m.Path) {
				if m.ReadOnly != ro {
					return nil, fmt.Errorf("mount %q is listed twice with different modes", p)
				}
				dup = true
			}
		}
		if dup {
			continue
		}

		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			warnf("Mount %q: %s is not an existing directory — skipping it.", spec, p)
			continue
		}
		out = append(out, Mount{Path: p, ReadOnly: ro})
	}
	return out, nil
}

// outsideProject picks the mounts that leave the project directory: those are the ones that need
// approval. A mount inside the project exposes nothing that is not exposed already.
func outsideProject(mounts []Mount, project string) []Mount {
	var out []Mount
	for _, m := range mounts {
		if !within(m.Path, resolveExisting(project)) {
			out = append(out, m)
		}
	}
	return out
}

// approvalKey identifies a project together with the exact set of entries (path and mode) that
// left it, so changing `:ro` to writable, or adding a directory, is a new question.
func approvalKey(project string, mounts []Mount) string {
	lines := make([]string, 0, len(mounts))
	for _, m := range mounts {
		lines = append(lines, m.Path+"|"+m.Mode())
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(resolveExisting(project) + "\n" + strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// approvalPath is where an approval is remembered: in the user's cache, never in the repository,
// so a repository cannot approve itself.
func approvalPath(key string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "mounts", key+".json"), nil
}

func mountsApproved(key string) bool {
	path, err := approvalPath(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func recordApproval(key, project string, mounts []Mount) error {
	path, err := approvalPath(key)
	if err != nil {
		return err
	}
	specs := make([]string, 0, len(mounts))
	for _, m := range mounts {
		specs = append(specs, m.Spec())
	}
	// The body is only for a human looking in the cache; the file's name is the approval.
	data, err := json.Marshal(map[string]any{"project": project, "mounts": specs, "approvedAt": now()})
	if err != nil {
		return err
	}
	return writeCache(path, data)
}

// askMounts is a variable so tests can answer without a terminal.
var askMounts = func(question string) (bool, bool) { return askYesNo(os.Stdin, os.Stdout, question) }

const errMountsNotApproved = "the config mounts host directories outside the project (%s) and they have not been " +
	"approved. Run sbxup once from a terminal and answer y; the answer is remembered until the list changes"

// approveMounts is layer 2: a mount outside the project needs the user's one-time approval.
// `:ro` does not waive it — a read leaks data too. Nothing is asked when an existing sandbox is
// resumed (its mounts are already fixed and the run args are dropped) or under --dry-run.
//
// Saying no drops the outside mounts and carries on, so a repository with an unwanted config is
// still usable; having no way to answer is an error, since a script cannot consent.
func approveMounts(mounts []Mount, project string, resuming, dryRun bool) ([]Mount, error) {
	outside := outsideProject(mounts, project)
	if len(outside) == 0 || resuming {
		return mounts, nil
	}
	key := approvalKey(project, outside)
	if mountsApproved(key) {
		return mounts, nil
	}
	if dryRun {
		fmt.Println("[dry-run] would ask you to approve the mounts outside the project before starting.")
		return mounts, nil
	}

	fmt.Println("This project's config mounts host directories into the sandbox:")
	names := make([]string, 0, len(outside))
	for _, m := range outside {
		fmt.Printf("  %s  (%s)\n", m.Path, m.Mode())
		names = append(names, m.Path)
	}
	fmt.Println("The agent runs with permissions off, so a read-write mount can be changed or deleted without asking.")

	if !isInteractive() {
		return nil, fmt.Errorf(errMountsNotApproved, strings.Join(names, ", "))
	}
	yes, answered := askMounts("Mount them? [y/N] ")
	if !answered {
		return nil, fmt.Errorf(errMountsNotApproved, strings.Join(names, ", "))
	}
	if !yes {
		warnf("Not mounting them; continuing with the rest. You will be asked again next time.")
		var kept []Mount
		for _, m := range mounts {
			if within(m.Path, resolveExisting(project)) {
				kept = append(kept, m)
			}
		}
		return kept, nil
	}
	if err := recordApproval(key, project, outside); err != nil {
		warnf("cannot remember the approval (%v); you will be asked again next time.", err)
	}
	return mounts, nil
}

// prepareMounts resolves, checks and approves the configured mounts, and reports what will
// happen. Refusals surface before any template is built.
func prepareMounts(specs []string, project, cache string, resuming, dryRun bool) ([]Mount, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot find your home directory to check the mounts against it: %w", err)
	}
	mounts, err := resolveMounts(specs, home, resolveExisting(project), resolveExisting(project), resolveExisting(cache))
	if err != nil {
		return nil, err
	}
	mounts, err = approveMounts(mounts, project, resuming, dryRun)
	if err != nil {
		return nil, err
	}
	if resuming && !dryRun {
		if len(mounts) > 0 {
			fmt.Println("Note: 'mounts' only apply when a sandbox is created. The existing sandbox keeps the mounts it " +
				"has; to apply changes, remove it with 'sbx rm <name>' (its own state is deleted, not your project files).")
		}
		return mounts, nil
	}
	for _, m := range mounts {
		fmt.Printf("Mount: %s (%s)\n", m.Path, m.Mode())
	}
	return mounts, nil
}
