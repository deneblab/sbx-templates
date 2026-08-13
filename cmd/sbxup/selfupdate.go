package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// The repository publishes two independent release streams, told apart by tag prefix.
	tagPrefix          = "sbxup-v"
	templatesTagPrefix = "templates-v"
	httpClient         = 60 * time.Second
)

// Endpoints are variables rather than constants so tests can point them at a local server;
// nothing outside the tests reassigns them.
//
// repoAPI / repoWeb are the canonical repository — both sbxup's own release stream and the
// default template source. githubAPI / githubWeb are the hosts a *different* template repository
// is addressed through (see releaseRef.apiURL).
var (
	repoAPI = "https://api.github.com/repos/deneblab/sbx-templates/releases"
	repoWeb = "https://github.com/deneblab/sbx-templates/releases"

	githubAPI = "https://api.github.com"
	githubWeb = "https://github.com"
)

// assetName is the release asset for the running platform, matching the names the release
// workflow publishes (sbxup-<goos>-<goarch>, .exe on Windows).
func assetName(goos, goarch string) string {
	name := fmt.Sprintf("sbxup-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

type release struct {
	TagName string `json:"tag_name"`
}

// latestRelease finds the newest release of ref's repository whose tag carries the given prefix.
// A repository publishes both sbxup binaries (sbxup-v*) and template Dockerfiles (templates-v*),
// so /releases/latest is not necessarily the stream the caller wants.
func latestRelease(client *http.Client, ref releaseRef, prefix string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, ref.apiURL()+"?per_page=50", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// sbxup sends no credentials, so a private repository is indistinguishable from a
		// missing one — say both rather than reporting a missing release.
		return "", fmt.Errorf("repository %s/%s not found (it must exist and be public)", ref.Owner, ref.Repo)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s listing releases", resp.Status)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("cannot parse release list: %w", err)
	}
	for _, r := range releases {
		if strings.HasPrefix(r.TagName, prefix) {
			return r.TagName, nil
		}
	}
	return "", fmt.Errorf("no %s* release found at %s", prefix, ref.webURL())
}

func download(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// parseChecksum pulls the digest out of a `sha256sum`-format line: "<hex>  <filename>".
func parseChecksum(data []byte) (string, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", fmt.Errorf("malformed checksum file")
	}
	return strings.ToLower(fields[0]), nil
}

// selfUpdate downloads the latest release asset for this platform, verifies its checksum and
// replaces the running executable. Nothing is written until verification passes.
func selfUpdate() error {
	client := &http.Client{Timeout: httpClient}

	// Deliberately pinned to the canonical repository: a config may point template builds at a
	// fork, but where sbxup replaces its own executable must never follow project configuration.
	tag, err := latestRelease(client, defaultRef(""), tagPrefix)
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(tag, tagPrefix)
	if latest == version {
		fmt.Printf("sbxup %s is already the latest release.\n", version)
		return nil
	}

	asset := assetName(runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("%s/download/%s", repoWeb, tag)
	fmt.Printf("Updating sbxup %s -> %s (%s)\n", version, latest, asset)

	binary, err := download(client, base+"/"+asset)
	if err != nil {
		return fmt.Errorf("download failed: %w\nIf your platform is unpublished, see %s", err, repoWeb)
	}
	sumFile, err := download(client, base+"/"+asset+".sha256")
	if err != nil {
		return fmt.Errorf("no checksum published for %s: %w", asset, err)
	}
	want, err := parseChecksum(sumFile)
	if err != nil {
		return err
	}
	got := sha256.Sum256(binary)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch — the download is corrupt or has been tampered with. Nothing was changed")
	}

	if err := replaceSelf(binary); err != nil {
		return err
	}
	fmt.Printf("Updated to sbxup %s\n", latest)
	return nil
}

// backupName is where the running executable is moved aside on Windows. The pid keeps it
// unique, so an undeletable leftover from an earlier update cannot block this one.
func backupName(exe string, pid int) string {
	return fmt.Sprintf("%s.old-%d", exe, pid)
}

// sweepBackups deletes leftovers from earlier updates. Best-effort: one still held open by a
// running process simply stays, which is now harmless because names no longer collide.
func sweepBackups(exe string) {
	matches, err := filepath.Glob(exe + ".old*")
	if err != nil {
		return
	}
	for _, m := range matches {
		os.Remove(m)
	}
}

// replaceSelf swaps the running executable for newBinary. The new file is staged alongside
// the target so the final step is a same-filesystem rename.
func replaceSelf(newBinary []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".sbxup-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has succeeded

	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	// Windows refuses to replace a running image, so move it aside first.
	if runtime.GOOS == "windows" {
		// A fixed ".old" name is a trap: Go's rename maps to MoveFileEx with
		// MOVEFILE_REPLACE_EXISTING, and replacing a *locked* leftover from an earlier update
		// fails with "Access is denied" — reported as a failure to move the running exe, which
		// points at the wrong file. A pid-unique name cannot collide with that leftover.
		old := backupName(exe, os.Getpid())
		sweepBackups(exe)
		if err := os.Rename(exe, old); err != nil {
			return fmt.Errorf("cannot move the running executable aside: %w\n"+
				"Close any other running sbxup processes and retry, or reinstall with:\n"+
				"  irm https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.ps1 | iex", err)
		}
		if err := os.Rename(tmpName, exe); err != nil {
			os.Rename(old, exe) // put it back rather than leaving nothing installed
			return err
		}
		return nil
	}

	return os.Rename(tmpName, exe)
}
