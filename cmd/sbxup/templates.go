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
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// The repository that publishes the canonical templates. A config may name another one, so
	// this is a default rather than the only possibility — but see selfUpdate(), which is pinned
	// to it deliberately.
	defaultOwner = "deneblab"
	defaultRepo  = "sbx-templates"

	// latestTTL is how long a resolved "latest" tag is trusted before GitHub is asked again.
	// Long by design: a floating release should follow upstream at roughly the pace of a work
	// week, not add a network round trip to every single run. `--refresh` bypasses it.
	latestTTL = 180 * time.Hour
)

// now is indirected so tests can age the latest-release cache without sleeping.
var now = time.Now

// releaseRef identifies one templates-v* release: the repository publishing it, and the tag.
// An empty Tag means "the newest templates-v* release", resolved lazily and then cached.
type releaseRef struct {
	Owner string
	Repo  string
	Tag   string
}

// defaultRef names a release in the canonical repository.
func defaultRef(tag string) releaseRef {
	return releaseRef{Owner: defaultOwner, Repo: defaultRepo, Tag: tag}
}

func (r releaseRef) isDefaultSource() bool {
	return r.Owner == defaultOwner && r.Repo == defaultRepo
}

// slug is the cache-directory segment for a source repository. Two repositories can publish the
// same tag, so the tag alone is not a safe cache key.
func (r releaseRef) slug() string {
	return strings.ToLower(r.Owner + "-" + r.Repo)
}

// String renders the ref in the form a config uses, for messages.
func (r releaseRef) String() string {
	tag := r.Tag
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s/%s@%s", r.Owner, r.Repo, tag)
}

// apiURL and webURL locate the release endpoints. The canonical source keeps using repoAPI /
// repoWeb verbatim: those are also sbxup's own release stream, and routing the default through
// them means this change cannot alter where existing installs fetch from.
func (r releaseRef) apiURL() string {
	if r.isDefaultSource() {
		return repoAPI
	}
	return fmt.Sprintf("%s/repos/%s/%s/releases", githubAPI, r.Owner, r.Repo)
}

func (r releaseRef) webURL() string {
	if r.isDefaultSource() {
		return repoWeb
	}
	return fmt.Sprintf("%s/%s/%s/releases", githubWeb, r.Owner, r.Repo)
}

var (
	// One slash, and only characters GitHub itself allows — this value becomes both a URL path
	// and a cache directory segment, so it is a traversal boundary, not decoration.
	ownerRepoRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	versionRe   = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)*$`)
)

const releaseFormsHint = "expected 'latest', a version like '0.1.4', a tag like 'templates-v0.1.4', " +
	"or '<owner>/<repo>@<version|tag|latest>'"

// parseReleaseRef turns a config `release:` value into a releaseRef.
//
// Accepted: "" and "latest" (newest release of the default repo), "0.1.4" / "v0.1.4" /
// "templates-v0.1.4" (that release of the default repo), and any of those after
// "<owner>/<repo>@". A bare "<owner>/<repo>" means that repo's newest release.
//
// Anything else is an error rather than a fallback: this key exists to make a build
// reproducible, so a value sbxup cannot read must never quietly resolve to "latest".
func parseReleaseRef(value string) (releaseRef, error) {
	v := strings.TrimSpace(value)
	ref := defaultRef("")
	if v == "" {
		return ref, nil
	}

	if strings.Contains(v, "/") {
		source, rest := v, ""
		if at := strings.Index(v, "@"); at >= 0 {
			source, rest = v[:at], strings.TrimSpace(v[at+1:])
			if rest == "" {
				return releaseRef{}, fmt.Errorf("release %q names no version after '@' — %s", value, releaseFormsHint)
			}
		}
		owner, repo, err := parseSource(source, value)
		if err != nil {
			return releaseRef{}, err
		}
		ref.Owner, ref.Repo = owner, repo
		v = rest
	}

	tag, err := normalizeReleaseTag(v)
	if err != nil {
		return releaseRef{}, fmt.Errorf("cannot read release %q: %w", value, err)
	}
	ref.Tag = tag
	return ref, nil
}

func parseSource(source, whole string) (string, string, error) {
	if !ownerRepoRe.MatchString(source) || strings.Contains(source, "..") {
		return "", "", fmt.Errorf("release %q does not name a repository as '<owner>/<repo>' — %s", whole, releaseFormsHint)
	}
	owner, repo, _ := strings.Cut(source, "/")
	if owner == "." || repo == "." {
		return "", "", fmt.Errorf("release %q does not name a repository as '<owner>/<repo>' — %s", whole, releaseFormsHint)
	}
	return owner, repo, nil
}

// normalizeReleaseTag maps a version or tag onto a templates-v* tag; "" means latest.
func normalizeReleaseTag(v string) (string, error) {
	switch {
	case v == "" || strings.EqualFold(v, "latest"):
		return "", nil
	case strings.HasPrefix(v, templatesTagPrefix):
		if !versionRe.MatchString(strings.TrimPrefix(v, templatesTagPrefix)) {
			return "", fmt.Errorf("%q is not a templates release tag — %s", v, releaseFormsHint)
		}
		return v, nil
	case versionRe.MatchString(v):
		return templatesTagPrefix + strings.TrimPrefix(v, "v"), nil
	}
	return "", fmt.Errorf("%q is neither a version nor a tag — %s", v, releaseFormsHint)
}

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
//
// A template from a non-default repository is namespaced by owner, because the sandbox runtime
// has one image store: a fork publishing the same template name and version would otherwise
// overwrite the canonical image, and neither build could tell that it had happened. The default
// source keeps its bare name, so no existing tag changes.
func (t TemplateEntry) LocalTag(ref releaseRef) string {
	name := t.Name
	if !ref.isDefaultSource() {
		name = strings.ToLower(ref.Owner) + "-" + name
	}
	if t.Version == "" {
		return name + ":local"
	}
	return name + ":" + t.Version
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
func releaseBase(ref releaseRef) string {
	return fmt.Sprintf("%s/download/%s", ref.webURL(), ref.Tag)
}

// fetchVerified downloads a release asset together with its .sha256 sidecar and returns the
// bytes only if the digest matches. A Dockerfile becomes the agent's execution environment,
// so it gets the same treatment selfUpdate() gives the binary: verify before anything lands
// on disk or reaches `docker build`.
func fetchVerified(client *http.Client, ref releaseRef, asset string) ([]byte, error) {
	base := releaseBase(ref)
	data, err := download(client, base+"/"+asset)
	if err != nil {
		return nil, fmt.Errorf("cannot download %s from %s: %w", asset, ref, err)
	}
	sumFile, err := download(client, base+"/"+asset+".sha256")
	if err != nil {
		return nil, fmt.Errorf("no checksum published for %s in %s: %w", asset, ref, err)
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
//
// The source repository is part of the path: two repositories can publish the same
// templates-v0.1.4 tag, and a flat tag-keyed cache would serve one fork's tarball for another's
// release.
func templatesCacheDir(ref releaseRef) (string, error) {
	base, err := sourceCacheDir(ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ref.Tag), nil
}

// sourceCacheDir is the per-repository cache root, holding one directory per release plus the
// resolved-latest record.
func sourceCacheDir(ref releaseRef) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "templates", ref.slug()), nil
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

// latestRecord is the cached answer to "which release is newest?" for one repository.
type latestRecord struct {
	Tag        string    `json:"tag"`
	ResolvedAt time.Time `json:"resolvedAt"`
}

func latestRecordPath(ref releaseRef) (string, error) {
	dir, err := sourceCacheDir(ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "latest.json"), nil
}

func readLatestRecord(ref releaseRef) (latestRecord, bool) {
	path, err := latestRecordPath(ref)
	if err != nil {
		return latestRecord{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return latestRecord{}, false
	}
	var rec latestRecord
	if err := json.Unmarshal(data, &rec); err != nil || rec.Tag == "" {
		return latestRecord{}, false
	}
	return rec, true
}

func writeLatestRecord(ref releaseRef, tag string) error {
	path, err := latestRecordPath(ref)
	if err != nil {
		return err
	}
	data, err := json.Marshal(latestRecord{Tag: tag, ResolvedAt: now()})
	if err != nil {
		return err
	}
	return writeCache(path, data)
}

// resolveTemplatesRelease returns the release to use: an explicit pin, or the newest
// templates-v* release of the named repository.
//
// A resolved "latest" is remembered for latestTTL, so the common repeat run costs no network
// call at all and still works with GitHub unreachable — without that, dropping the pin from a
// config would trade a version number for a hard network dependency. Order: pin, then a fresh
// cached answer, then the network, then a stale cached answer with a warning.
func resolveTemplatesRelease(client *http.Client, pin string, refresh bool) (releaseRef, error) {
	ref, err := parseReleaseRef(pin)
	if err != nil {
		return releaseRef{}, err
	}
	if ref.Tag != "" {
		return ref, nil
	}

	cached, hasCached := readLatestRecord(ref)
	if hasCached && !refresh && now().Sub(cached.ResolvedAt) < latestTTL {
		ref.Tag = cached.Tag
		return ref, nil
	}

	tag, err := latestRelease(client, ref, templatesTagPrefix)
	if err != nil {
		if hasCached {
			warnf("cannot check %s/%s for a newer release (%v) — using %s. Run 'sbxup --refresh' when back online.",
				ref.Owner, ref.Repo, err, cached.Tag)
			ref.Tag = cached.Tag
			return ref, nil
		}
		return releaseRef{}, err
	}
	ref.Tag = tag
	if err := writeLatestRecord(ref, tag); err != nil {
		warnf("cannot cache the resolved release: %v", err)
	}
	return ref, nil
}

// loadManifest returns the manifest for a release, using the cached copy unless refresh is set.
// Reading the cache first is what lets an offline run learn a template's version — and therefore
// its local tag — without touching the network.
func loadManifest(client *http.Client, ref releaseRef, refresh bool) (*Manifest, error) {
	dir, err := templatesCacheDir(ref)
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

	data, err := fetchVerified(client, ref, "manifest.json")
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
func fetchDockerfile(client *http.Client, ref releaseRef, m *Manifest, t *TemplateEntry, refresh bool) (string, error) {
	if m.Tarball == "" {
		return "", fmt.Errorf("%s publishes no tarball — the manifest is incomplete", ref)
	}
	tree, err := ensureTemplateTree(client, ref, m, refresh)
	if err != nil {
		return "", err
	}
	path := filepath.Join(tree, t.Name, "Dockerfile")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s has no %s/Dockerfile inside %s", ref, t.Name, m.Tarball)
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
