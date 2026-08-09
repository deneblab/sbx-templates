package main

// Template distribution without a registry: this repository publishes its Dockerfiles inside a
// `templates-v*` GitHub Release tarball. sbxup fetches the manifest, lets the user pick an entry,
// extracts the tarball once and builds locally from it (see tarball.go, build.go). Nothing here
// pulls a prebuilt image, and no template list is hardcoded — the manifest is the catalogue.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TemplateEntry is one template as described by the release manifest.
type TemplateEntry struct {
	Name          string `json:"name"`
	Short         string `json:"short"`
	Description   string `json:"description"`
	Version       string `json:"version"`
	RegistryImage string `json:"registryImage"`
}

// LocalTag is the image tag a locally built copy of this template gets. It carries the
// template's own version so a rebuilt release does not silently reuse the old image, and so
// the tag written into sbxup.yaml pins exactly what was built.
func (t TemplateEntry) LocalTag() string {
	if t.Version == "" {
		return t.Name + ":local"
	}
	return t.Name + ":" + t.Version
}

// Manifest is manifest.json from a templates-v* release.
type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Release       string          `json:"release"`
	Version       string          `json:"version"`
	Tarball       string          `json:"tarball"`
	Templates     []TemplateEntry `json:"templates"`
}

// manifestSchema is the schema version this build understands. A newer manifest is reported
// rather than silently misread, since the failure would otherwise surface as a confusing
// "template not found".
// 1 — every template also shipped as its own <name>.Dockerfile asset, named by a `dockerfile`
// field on each entry. Those releases are still downloadable and still parse: the field is
// simply ignored, and their tarball is what gets used.
// 2 — the release carries only manifest.json and the tarball. A build older than 0.2.6 looks
// for the per-template asset, so it refuses this manifest outright with a self-update hint
// rather than 404ing on something the release no longer publishes.
const manifestSchema = 2

func parseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("cannot parse manifest.json: %w", err)
	}
	if m.SchemaVersion > manifestSchema {
		return nil, fmt.Errorf("manifest schemaVersion %d is newer than this sbxup understands (%d) — run 'sbxup --self-update'",
			m.SchemaVersion, manifestSchema)
	}
	if len(m.Templates) == 0 {
		return nil, fmt.Errorf("manifest.json lists no templates")
	}
	return &m, nil
}

// findTemplate matches on either the full name or the short alias, so both
// `--template sbx-claude-dotnet10` and `--template dotnet10` work.
func findTemplate(m *Manifest, want string) (*TemplateEntry, error) {
	for i := range m.Templates {
		if m.Templates[i].Name == want || m.Templates[i].Short == want {
			return &m.Templates[i], nil
		}
	}
	names := make([]string, 0, len(m.Templates))
	for _, t := range m.Templates {
		names = append(names, t.Short)
	}
	return nil, fmt.Errorf("no template %q in %s (available: %s)", want, m.Release, strings.Join(names, ", "))
}

// releaseBase is the download prefix for a release's assets.
func releaseBase(tag string) string {
	return fmt.Sprintf("%s/download/%s", repoWeb, tag)
}

// fetchVerified downloads a release asset together with its .sha256 sidecar and returns the
// bytes only if the digest matches. A Dockerfile becomes the agent's execution environment,
// so it gets the same treatment selfUpdate() gives the binary: verify before anything lands
// on disk or reaches `docker build`.
func fetchVerified(client *http.Client, tag, asset string) ([]byte, error) {
	base := releaseBase(tag)
	data, err := download(client, base+"/"+asset)
	if err != nil {
		return nil, fmt.Errorf("cannot download %s from %s: %w", asset, tag, err)
	}
	sumFile, err := download(client, base+"/"+asset+".sha256")
	if err != nil {
		return nil, fmt.Errorf("no checksum published for %s in %s: %w", asset, tag, err)
	}
	want, err := parseChecksum(sumFile)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("checksum mismatch for %s — the download is corrupt or has been tampered with. Nothing was written", asset)
	}
	return data, nil
}

// templatesCacheDir is where a release's assets are kept between runs.
// os.UserCacheDir resolves to ~/.cache on Linux, ~/Library/Caches on macOS and
// %LocalAppData% on Windows, so no per-platform branching is needed.
func templatesCacheDir(tag string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "templates", tag), nil
}

// writeCache stores data at path via a temp file in the same directory, so an interrupted
// write cannot leave a truncated Dockerfile that a later run would happily build.
func writeCache(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sbxup-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// resolveTemplatesRelease returns the release tag to use: an explicit pin, or the newest
// templates-v* release.
func resolveTemplatesRelease(client *http.Client, pin string) (string, error) {
	if pin != "" {
		if !strings.HasPrefix(pin, templatesTagPrefix) {
			// Accept a bare version ("0.1.3") as well as a full tag.
			pin = templatesTagPrefix + strings.TrimPrefix(pin, "v")
		}
		return pin, nil
	}
	return latestRelease(client, templatesTagPrefix)
}

// loadManifest returns the manifest for a release, using the cached copy unless refresh is set.
func loadManifest(client *http.Client, tag string, refresh bool) (*Manifest, error) {
	dir, err := templatesCacheDir(tag)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "manifest.json")

	if !refresh {
		if data, err := os.ReadFile(path); err == nil {
			if m, err := parseManifest(data); err == nil {
				return m, nil
			}
			// A corrupt cache should not be fatal — fall through and re-fetch.
		}
	}

	data, err := fetchVerified(client, tag, "manifest.json")
	if err != nil {
		return nil, err
	}
	m, err := parseManifest(data)
	if err != nil {
		return nil, err
	}
	if err := writeCache(path, data); err != nil {
		warnf("cannot cache manifest: %v", err)
	}
	return m, nil
}

// fetchDockerfile returns the on-disk path of a template's Dockerfile, extracted from the
// release tarball. One verified download makes every template in the release available, so
// switching templates later costs nothing and works offline.
func fetchDockerfile(client *http.Client, tag string, m *Manifest, t *TemplateEntry, refresh bool) (string, error) {
	if m.Tarball == "" {
		return "", fmt.Errorf("%s publishes no tarball — the manifest is incomplete", tag)
	}
	tree, err := ensureTemplateTree(client, tag, m, refresh)
	if err != nil {
		return "", err
	}
	path := filepath.Join(tree, t.Name, "Dockerfile")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s has no %s/Dockerfile inside %s", tag, t.Name, m.Tarball)
	}
	return path, nil
}

// interactive reports whether stdin is a terminal. The picker is only shown when it is:
// a piped or redirected stdin means a script is driving sbxup, and a prompt there would
// hang CI instead of failing usefully.
func interactive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// pickTemplate renders the manifest as a numbered menu and reads a choice. Empty input takes
// the first entry, which keeps the common "just give me the default" path to a single Enter.
func pickTemplate(m *Manifest, in io.Reader, out io.Writer) (*TemplateEntry, error) {
	fmt.Fprintf(out, "\nTemplates in %s:\n\n", m.Release)
	for i, t := range m.Templates {
		fmt.Fprintf(out, "  %d) %-22s %s\n", i+1, t.Short, t.Description)
	}
	fmt.Fprintf(out, "\nChoose a template [1-%d] (default 1): ", len(m.Templates))

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return nil, fmt.Errorf("cannot read a choice: %w", err)
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return &m.Templates[0], nil
	}
	// A name is as acceptable as a number — people paste the short alias they can see.
	if n, convErr := strconv.Atoi(choice); convErr == nil {
		if n < 1 || n > len(m.Templates) {
			return nil, fmt.Errorf("choice %d is out of range [1-%d]", n, len(m.Templates))
		}
		return &m.Templates[n-1], nil
	}
	return findTemplate(m, choice)
}
