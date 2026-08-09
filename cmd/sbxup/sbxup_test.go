package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdir moves into a fresh temp dir for the duration of a test.
func chdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	// macOS temp dirs are symlinked via /var -> /private/var; resolve so path
	// comparisons against os.Getwd() line up.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseArgs(t *testing.T) {
	t.Run("flags and values", func(t *testing.T) {
		o, err := parseArgs([]string{"--template", "img:1", "--agent", "claude", "--dry-run", "--clone"})
		if err != nil {
			t.Fatal(err)
		}
		if o.template != "img:1" || o.agent != "claude" || !o.dryRun || !o.clone {
			t.Fatalf("unexpected options: %+v", o)
		}
	})

	t.Run("unknown args pass through", func(t *testing.T) {
		o, err := parseArgs([]string{"--dry-run", "--some-sbx-flag", "value"})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"--some-sbx-flag", "value"}
		if strings.Join(o.extra, " ") != strings.Join(want, " ") {
			t.Fatalf("extra = %v, want %v", o.extra, want)
		}
	})

	t.Run("missing value is an error", func(t *testing.T) {
		if _, err := parseArgs([]string{"--template"}); err == nil {
			t.Fatal("expected an error for --template without a value")
		}
	})

	t.Run("value is not swallowed as a flag", func(t *testing.T) {
		o, err := parseArgs([]string{"--agent", "claude", "--stop"})
		if err != nil {
			t.Fatal(err)
		}
		if !o.stop || len(o.extra) != 0 {
			t.Fatalf("unexpected options: %+v", o)
		}
	})
}

func TestFindConfig(t *testing.T) {
	t.Run("reads .sbx/sbxup.config.yaml", func(t *testing.T) {
		chdir(t)
		write(t, ".sbx/sbxup.config.yaml", "agent: new\n")
		if got := findConfig(); got != ".sbx/sbxup.config.yaml" {
			t.Fatalf("findConfig() = %q", got)
		}
	})

	t.Run("no other location is read", func(t *testing.T) {
		chdir(t)
		for _, p := range legacyConfigPaths {
			write(t, p, "agent: legacy\n")
		}
		if got := findConfig(); got != "" {
			t.Fatalf("findConfig() = %q, want empty — only .sbx/sbxup.config.yaml is read", got)
		}
	})

	t.Run("no config at all", func(t *testing.T) {
		chdir(t)
		if got := findConfig(); got != "" {
			t.Fatalf("findConfig() = %q, want empty", got)
		}
	})
}

func TestLegacyConfigIsReportedNotLoaded(t *testing.T) {
	chdir(t)
	if got := legacyConfig(); got != "" {
		t.Fatalf("legacyConfig() = %q with no files present", got)
	}
	write(t, ".agents/sbx-runner.yaml", "agent: b\n")
	if got := legacyConfig(); got != ".agents/sbx-runner.yaml" {
		t.Fatalf("legacyConfig() = %q", got)
	}
	// Present but not loaded: the canonical path is still the only one findConfig returns.
	if got := findConfig(); got != "" {
		t.Fatalf("findConfig() = %q, want empty", got)
	}
}

func TestLoadConfig(t *testing.T) {
	chdir(t)
	write(t, "c.yaml", `template: docker.io/pkudrel/img:latest
agent: claude
clone: true
cache: .sbx-cache
`)
	cfg, err := loadConfig("c.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Template != "docker.io/pkudrel/img:latest" {
		t.Errorf("Template = %q", cfg.Template)
	}
	if cfg.Agent != "claude" {
		t.Errorf("Agent = %q", cfg.Agent)
	}
	if !cfg.Clone {
		t.Error("Clone = false, want true")
	}
	if cfg.Cache != ".sbx-cache" {
		t.Errorf("Cache = %q", cfg.Cache)
	}
}

func TestLoadConfigCommentsAndQuotes(t *testing.T) {
	chdir(t)
	// The grep/sed approach in the PowerShell version mishandles a '#' inside a quoted
	// value; a real parser keeps it.
	write(t, "c.yaml", `template: "docker.io/pkudrel/img:latest"   # trailing comment
agent: claude
`)
	cfg, err := loadConfig("c.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Template != "docker.io/pkudrel/img:latest" {
		t.Errorf("Template = %q", cfg.Template)
	}
}

func TestIsTruthy(t *testing.T) {
	truthy := []any{true, "true", "True", "1", "yes", "on", "ON"}
	for _, v := range truthy {
		if !isTruthy(v) {
			t.Errorf("isTruthy(%v) = false, want true", v)
		}
	}
	falsy := []any{false, "false", "0", "no", "off", "", nil, "maybe"}
	for _, v := range falsy {
		if isTruthy(v) {
			t.Errorf("isTruthy(%v) = true, want false", v)
		}
	}
}

func TestInitConfig(t *testing.T) {
	t.Run("creates default and parent dir", func(t *testing.T) {
		chdir(t)
		if err := initConfig(""); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(defaultConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Agent != "claude" || cfg.Clone {
			t.Fatalf("unexpected default config: %+v", cfg)
		}
	})

	t.Run("never clobbers an existing file", func(t *testing.T) {
		chdir(t)
		write(t, defaultConfigPath, "agent: mine\n")
		if err := initConfig(""); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(defaultConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "agent: mine\n" {
			t.Fatalf("existing config was overwritten: %q", data)
		}
	})
}

func TestFoldName(t *testing.T) {
	want := "claudeappschedule"
	for _, in := range []string{"claude-APP_Schedule", "claude-app-schedule", "claude-AppSchedule"} {
		if got := foldName(in); got != want {
			t.Errorf("foldName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildRunArgs(t *testing.T) {
	tests := []struct {
		name      string
		clone     bool
		cachePath string
		extra     []string
		want      string
	}{
		{
			name: "minimal",
			want: "run --template img:1 claude",
		},
		{
			name:  "clone",
			clone: true,
			want:  "run --template img:1 claude --clone",
		},
		{
			name:      "cache mounts the workspace pair",
			cachePath: "/w/proj/.sbx-cache",
			want:      "run --template img:1 claude . /w/proj/.sbx-cache",
		},
		{
			name:      "clone and cache and passthrough",
			clone:     true,
			cachePath: "/w/proj/.sbx-cache",
			extra:     []string{"--foo", "bar"},
			want:      "run --template img:1 claude --clone . /w/proj/.sbx-cache --foo bar",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(buildRunArgs("img:1", "claude", tc.clone, tc.cachePath, tc.extra), " ")
			if got != tc.want {
				t.Errorf("buildRunArgs = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveSandboxName(t *testing.T) {
	orig := sbxList
	t.Cleanup(func() { sbxList = orig })

	sbxList = func() []string {
		return []string{
			"NAME                STATUS",
			"claude-AbcVersion   running",
			"claude-other        stopped",
		}
	}

	t.Run("returns the name exactly as sbx reports it", func(t *testing.T) {
		if got := resolveSandboxName("claude-abcversion"); got != "claude-AbcVersion" {
			t.Fatalf("resolveSandboxName = %q, want claude-AbcVersion", got)
		}
	})

	t.Run("no match", func(t *testing.T) {
		if got := resolveSandboxName("claude-missing"); got != "" {
			t.Fatalf("resolveSandboxName = %q, want empty", got)
		}
	})
}

func TestAssetName(t *testing.T) {
	tests := map[string]string{
		"linux/amd64":   "sbxup-linux-amd64",
		"darwin/arm64":  "sbxup-darwin-arm64",
		"windows/amd64": "sbxup-windows-amd64.exe",
	}
	for platform, want := range tests {
		parts := strings.SplitN(platform, "/", 2)
		if got := assetName(parts[0], parts[1]); got != want {
			t.Errorf("assetName(%s) = %q, want %q", platform, got, want)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	digest := strings.Repeat("a", 64)
	got, err := parseChecksum([]byte(digest + "  sbxup-linux-amd64\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != digest {
		t.Errorf("parseChecksum = %q", got)
	}
	if _, err := parseChecksum([]byte("nonsense\n")); err == nil {
		t.Error("expected an error for a malformed checksum file")
	}
}

// ─── Local templates (templates-v* release stream) ──────────────────────────

const testManifest = `{
  "schemaVersion": 1,
  "release": "templates-v0.1.3",
  "version": "0.1.3",
  "tarball": "templates-0.1.3.tar.gz",
  "templates": [
    {
      "name": "sbx-claude-dotnet10",
      "short": "dotnet10",
      "description": ".NET SDK 10.0",
      "dockerfile": "sbx-claude-dotnet10.Dockerfile",
      "version": "0.1.2",
      "registryImage": "docker.io/pkudrel/sbx-claude-dotnet10:latest"
    },
    {
      "name": "sbx-claude-python-uv",
      "short": "python-uv",
      "description": "Latest Python via uv",
      "dockerfile": "sbx-claude-python-uv.Dockerfile",
      "version": "0.1.1",
      "registryImage": "docker.io/pkudrel/sbx-claude-python-uv:latest"
    }
  ]
}`

func TestParseManifest(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m, err := parseManifest([]byte(testManifest))
		if err != nil {
			t.Fatal(err)
		}
		if m.Release != "templates-v0.1.3" || len(m.Templates) != 2 {
			t.Fatalf("unexpected manifest: %+v", m)
		}
		if m.Templates[0].Description != ".NET SDK 10.0" {
			t.Errorf("Description = %q", m.Templates[0].Description)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		if _, err := parseManifest([]byte("{not json")); err == nil {
			t.Fatal("expected an error for malformed json")
		}
	})

	t.Run("no templates", func(t *testing.T) {
		if _, err := parseManifest([]byte(`{"schemaVersion":1,"templates":[]}`)); err == nil {
			t.Fatal("expected an error for an empty template list")
		}
	})

	t.Run("newer schema is reported, not misread", func(t *testing.T) {
		body := `{"schemaVersion":99,"templates":[{"name":"x"}]}`
		_, err := parseManifest([]byte(body))
		if err == nil || !strings.Contains(err.Error(), "self-update") {
			t.Fatalf("err = %v, want a self-update hint", err)
		}
	})

	// Schema 2 drops the per-template `dockerfile` field: the Dockerfiles ship only inside the
	// tarball. Schema 1 manifests still parse — testManifest above carries the old field, and
	// it is simply ignored — so a config pinned to an older release keeps working.
	t.Run("schema 2 has no dockerfile field", func(t *testing.T) {
		body := `{"schemaVersion":2,"release":"templates-v0.2.6","tarball":"templates-0.2.6.tar.gz",
			"templates":[{"name":"sbx-claude-dotnet10","short":"dotnet10","version":"0.2.6"}]}`
		m, err := parseManifest([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if m.Tarball != "templates-0.2.6.tar.gz" || m.Templates[0].Short != "dotnet10" {
			t.Fatalf("unexpected manifest: %+v", m)
		}
	})
}

// A manifest with no tarball is a broken release, not something to work around.
func TestFetchDockerfileRejectsAManifestWithNoTarball(t *testing.T) {
	_, err := fetchDockerfile(nil, "templates-v0.2.6", &Manifest{}, &TemplateEntry{Name: "x"}, false)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("err = %v, want an incomplete-manifest error", err)
	}
}

func TestFindTemplate(t *testing.T) {
	m, err := parseManifest([]byte(testManifest))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dotnet10", "sbx-claude-dotnet10"} {
		got, err := findTemplate(m, want)
		if err != nil {
			t.Fatalf("findTemplate(%q): %v", want, err)
		}
		if got.Name != "sbx-claude-dotnet10" {
			t.Errorf("findTemplate(%q) = %q", want, got.Name)
		}
	}
	_, err = findTemplate(m, "nope")
	if err == nil || !strings.Contains(err.Error(), "dotnet10") {
		t.Fatalf("err = %v, want the available names listed", err)
	}
}

func TestLocalTag(t *testing.T) {
	versioned := TemplateEntry{Name: "sbx-claude-dotnet10", Version: "0.1.2"}
	if got := versioned.LocalTag(); got != "sbx-claude-dotnet10:0.1.2" {
		t.Errorf("LocalTag = %q", got)
	}
	unversioned := TemplateEntry{Name: "sbx-claude-dotnet10"}
	if got := unversioned.LocalTag(); got != "sbx-claude-dotnet10:local" {
		t.Errorf("LocalTag = %q", got)
	}
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name         string
		updateClaude bool
		want         string
	}{
		{
			name: "full build",
			want: "buildx build --load -f /c/x.Dockerfile -t img:1 " +
				"--build-arg VERSION=1 --build-arg SHORT_SHA=templates-v9 --build-arg BUILD_DATE=D /c",
		},
		{
			name:         "claude stage only",
			updateClaude: true,
			want: "buildx build --load --no-cache-filter claude -f /c/x.Dockerfile -t img:1 " +
				"--build-arg VERSION=1 --build-arg SHORT_SHA=templates-v9 --build-arg BUILD_DATE=D /c",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(buildArgs("/c/x.Dockerfile", "/c", "img:1", "1", "templates-v9", "D", tc.updateClaude), " ")
			if got != tc.want {
				t.Errorf("buildArgs = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSaveLoadArgs(t *testing.T) {
	if got := strings.Join(saveArgs("img:1", "/tmp/t.tar"), " "); got != "image save img:1 -o /tmp/t.tar" {
		t.Errorf("saveArgs = %q", got)
	}
	if got := strings.Join(loadArgs("/tmp/t.tar"), " "); got != "template load /tmp/t.tar" {
		t.Errorf("loadArgs = %q", got)
	}
}

func TestPickTemplate(t *testing.T) {
	m, err := parseManifest([]byte(testManifest))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in   string
		want string
	}{
		{"1\n", "sbx-claude-dotnet10"},
		{"2\n", "sbx-claude-python-uv"},
		{"\n", "sbx-claude-dotnet10"},           // empty input takes the first entry
		{"python-uv\n", "sbx-claude-python-uv"}, // a pasted alias works too
	}
	for _, tc := range tests {
		got, err := pickTemplate(m, strings.NewReader(tc.in), io.Discard)
		if err != nil {
			t.Fatalf("pickTemplate(%q): %v", tc.in, err)
		}
		if got.Name != tc.want {
			t.Errorf("pickTemplate(%q) = %q, want %q", tc.in, got.Name, tc.want)
		}
	}

	for _, bad := range []string{"0\n", "9\n", "nope\n"} {
		if _, err := pickTemplate(m, strings.NewReader(bad), io.Discard); err == nil {
			t.Errorf("pickTemplate(%q) = nil error, want a rejection", bad)
		}
	}
}

func TestResolveTemplatesRelease(t *testing.T) {
	// A pin is honoured verbatim, and a bare version is normalised to a tag.
	for in, want := range map[string]string{
		"templates-v0.1.3": "templates-v0.1.3",
		"0.1.3":            "templates-v0.1.3",
		"v0.1.3":           "templates-v0.1.3",
	} {
		got, err := resolveTemplatesRelease(nil, in)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("resolveTemplatesRelease(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLatestReleaseByPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Newest first, mixing both release streams — the same shape the GitHub API returns.
		fmt.Fprint(w, `[{"tag_name":"templates-v0.1.3"},{"tag_name":"sbxup-v0.2.1"},{"tag_name":"templates-v0.1.2"}]`)
	}))
	defer srv.Close()

	orig := repoAPI
	repoAPI = srv.URL
	t.Cleanup(func() { repoAPI = orig })

	for prefix, want := range map[string]string{
		templatesTagPrefix: "templates-v0.1.3",
		tagPrefix:          "sbxup-v0.2.1",
	} {
		got, err := latestRelease(srv.Client(), prefix)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("latestRelease(%q) = %q, want %q", prefix, got, want)
		}
	}

	if _, err := latestRelease(srv.Client(), "nothing-v"); err == nil {
		t.Error("expected an error when no release carries the prefix")
	}
}

func TestFetchVerified(t *testing.T) {
	const body = "FROM scratch\n"
	digest := sha256.Sum256([]byte(body))

	newServer := func(sum string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".sha256") {
				fmt.Fprintf(w, "%s  x.Dockerfile\n", sum)
				return
			}
			fmt.Fprint(w, body)
		}))
	}

	t.Run("matching checksum returns the bytes", func(t *testing.T) {
		srv := newServer(hex.EncodeToString(digest[:]))
		defer srv.Close()
		orig := repoWeb
		repoWeb = srv.URL
		t.Cleanup(func() { repoWeb = orig })

		got, err := fetchVerified(srv.Client(), "templates-v0.1.3", "x.Dockerfile")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Errorf("fetchVerified = %q", got)
		}
	})

	t.Run("tampered content is refused", func(t *testing.T) {
		srv := newServer(strings.Repeat("b", 64))
		defer srv.Close()
		orig := repoWeb
		repoWeb = srv.URL
		t.Cleanup(func() { repoWeb = orig })

		_, err := fetchVerified(srv.Client(), "templates-v0.1.3", "x.Dockerfile")
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("err = %v, want a checksum mismatch", err)
		}
	})
}

func TestLoadConfigBuildBlock(t *testing.T) {
	chdir(t)

	t.Run("mapping form", func(t *testing.T) {
		write(t, "c.yaml", `template: sbx-claude-dotnet10:0.1.2
agent: claude
build:
  name: dotnet10
  release: templates-v0.1.3
`)
		cfg, err := loadConfig("c.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Build == nil || cfg.Build.Name != "dotnet10" || cfg.Build.Release != "templates-v0.1.3" {
			t.Fatalf("Build = %+v", cfg.Build)
		}
	})

	t.Run("string shorthand", func(t *testing.T) {
		write(t, "c.yaml", "agent: claude\nbuild: dotnet10\n")
		cfg, err := loadConfig("c.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Build == nil || cfg.Build.Name != "dotnet10" {
			t.Fatalf("Build = %+v", cfg.Build)
		}
	})

	t.Run("mapping without a name is an error", func(t *testing.T) {
		write(t, "c.yaml", "agent: claude\nbuild:\n  release: templates-v0.1.3\n")
		if _, err := loadConfig("c.yaml"); err == nil {
			t.Fatal("expected an error for a build block with no name")
		}
	})

	t.Run("absent build block leaves the registry flow untouched", func(t *testing.T) {
		write(t, "c.yaml", "template: docker.io/pkudrel/img:latest\nagent: claude\n")
		cfg, err := loadConfig("c.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Build != nil {
			t.Fatalf("Build = %+v, want nil", cfg.Build)
		}
	})
}

func TestBuildConfigBodyRoundTrip(t *testing.T) {
	chdir(t)
	entry := TemplateEntry{
		Name:    "sbx-claude-dotnet10",
		Short:   "dotnet10",
		Version: "0.1.2",
	}
	write(t, "c.yaml", buildConfigBody(&entry, "templates-v0.1.3"))

	cfg, err := loadConfig("c.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Template != "sbx-claude-dotnet10:0.1.2" {
		t.Errorf("Template = %q", cfg.Template)
	}
	if cfg.Agent != "claude" {
		t.Errorf("Agent = %q", cfg.Agent)
	}
	if cfg.Build == nil || cfg.Build.Name != "dotnet10" || cfg.Build.Release != "templates-v0.1.3" {
		t.Fatalf("Build = %+v", cfg.Build)
	}
}

func TestInitConfigBodyNeverClobbers(t *testing.T) {
	chdir(t)
	write(t, defaultConfigPath, "agent: mine\n")
	if err := initConfigBody("", "agent: theirs\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(defaultConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "agent: mine\n" {
		t.Fatalf("existing config was overwritten: %q", data)
	}
}

func TestTemplatesCacheDir(t *testing.T) {
	dir, err := templatesCacheDir("templates-v0.1.3")
	if err != nil {
		t.Skipf("no user cache dir available: %v", err)
	}
	if filepath.Base(dir) != "templates-v0.1.3" || !strings.Contains(dir, "sbxup") {
		t.Errorf("templatesCacheDir = %q", dir)
	}
}

func TestWriteCacheIsAtomicAndReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "manifest.json")
	if err := writeCache(path, []byte(testManifest)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != testManifest {
		t.Error("cached content does not round-trip")
	}
	// No temp files may be left behind next to the target.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("stray files left in the cache dir: %d entries", len(entries))
	}
}

func TestParseArgsLocalTemplateFlags(t *testing.T) {
	o, err := parseArgs([]string{"--build", "--rebuild", "--update-claude", "--refresh", "--template", "dotnet10"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.build || !o.rebuild || !o.updateClaude || !o.refresh || o.template != "dotnet10" {
		t.Fatalf("unexpected options: %+v", o)
	}
	if len(o.extra) != 0 {
		t.Errorf("extra = %v, want none", o.extra)
	}
}

func TestInitFlowDryRunWritesNothing(t *testing.T) {
	dir := chdir(t)

	// No release carries the templates-v prefix, so initFlow takes its fallback path — the
	// one that previously wrote the config even under --dry-run.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"sbxup-v0.2.1"}]`)
	}))
	defer srv.Close()
	orig := repoAPI
	repoAPI = srv.URL
	t.Cleanup(func() { repoAPI = orig })

	if err := initFlow(&options{dryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, defaultConfigPath)); !os.IsNotExist(err) {
		t.Fatalf("--dry-run created %s", defaultConfigPath)
	}

	// Without --dry-run the same path does write it.
	if err := initFlow(&options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, defaultConfigPath)); err != nil {
		t.Fatalf("config was not written: %v", err)
	}
}

func TestBuildTemplateDoesNotNeedDockerForARegisteredTemplate(t *testing.T) {
	entry := &TemplateEntry{Name: "sbx-claude-dotnet10", Short: "dotnet10", Version: "0.1.3"}

	origListed, origAvail, origExists := sbxTemplateListed, dockerAvailable, dockerImageExists
	t.Cleanup(func() {
		sbxTemplateListed, dockerAvailable, dockerImageExists = origListed, origAvail, origExists
	})

	// `sbx` already has it; Docker must not be consulted at all — that is the whole point,
	// since the sandbox runtime runs it with Docker Desktop closed.
	sbxTemplateListed = func(string) bool { return true }
	dockerAvailable = func() bool { t.Fatal("dockerAvailable called for an already-registered template"); return false }
	dockerImageExists = func(string) bool {
		t.Fatal("dockerImageExists called for an already-registered template")
		return false
	}

	tag, err := buildTemplate("/cache/x.Dockerfile", entry, "templates-v0.1.4", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "sbx-claude-dotnet10:0.1.3" {
		t.Errorf("tag = %q", tag)
	}
}

// Verbatim `sbx template ls` output. REPOSITORY and TAG are separate columns and repositories
// come back fully qualified, so a substring search for "name:tag" never matches — which made
// sbxup rebuild a template it had just imported, and fail when Docker Desktop was closed.
const sbxTemplateLsOutput = `REPOSITORY                                      TAG                  IMAGE ID       FLAVOR               CREATED
docker.io/docker/sandbox-templates              claude-code-docker   a8531b2e5fc6   claude-code-docker   About a month ago
docker.io/library/sbx-claude-dotnet10           0.1.3                b8140be92b47   claude-code          21 minutes ago
docker.io/pkudrel/sbx-claude-dotnet10           latest               b5dd4d442761   claude-code          4 months ago
docker.io/pkudrel/sbx-claude-python-uv          latest               606de2d7ff46   claude-code          About a month ago
`

func TestTemplateListedIn(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"sbx-claude-dotnet10:0.1.3", true},                     // bare name => docker.io/library/
		{"docker.io/library/sbx-claude-dotnet10:0.1.3", true},   // already canonical
		{"pkudrel/sbx-claude-dotnet10:latest", true},            // Hub user, no registry host
		{"docker.io/pkudrel/sbx-claude-python-uv:latest", true}, //
		{"docker.io/pkudrel/sbx-claude-python-uv", true},        // implicit :latest
		{"sbx-claude-dotnet10:0.1.4", false},                    // right name, wrong version
		{"pkudrel/sbx-claude-dotnet10:0.1.3", false},            // right version, wrong namespace
		{"sbx-claude-golang124-node24:0.1.3", false},            // absent entirely
		{"REPOSITORY:TAG", false},                               // the header is not a template
	}
	for _, c := range cases {
		if got := templateListedIn(sbxTemplateLsOutput, c.tag); got != c.want {
			t.Errorf("templateListedIn(%q) = %v, want %v", c.tag, got, c.want)
		}
	}
	if templateListedIn("", "sbx-claude-dotnet10:0.1.3") {
		t.Error("empty output must not report a template as listed")
	}
}

func TestBuildTemplateReportsAStoppedDaemon(t *testing.T) {
	entry := &TemplateEntry{Name: "sbx-claude-dotnet10", Version: "0.1.3"}

	origListed, origAvail := sbxTemplateListed, dockerAvailable
	t.Cleanup(func() { sbxTemplateListed, dockerAvailable = origListed, origAvail })

	sbxTemplateListed = func(string) bool { return false } // not built yet
	dockerAvailable = func() bool { return false }         // Docker Desktop is closed

	_, err := buildTemplate("/cache/x.Dockerfile", entry, "templates-v0.1.4", false, false, false)
	if err == nil {
		t.Fatal("expected an error when a build is required and Docker is unreachable")
	}
	if !strings.Contains(err.Error(), "Docker Desktop") {
		t.Errorf("err = %v, want an actionable Docker Desktop message", err)
	}
}

func TestBackupNameIsUniquePerProcess(t *testing.T) {
	exe := `C:\Users\piotr\AppData\Local\Programs\sbxup\sbxup.exe`
	a, b := backupName(exe, 1234), backupName(exe, 5678)
	if a == b {
		t.Fatal("backup names collide across processes")
	}
	// Must stay a sibling of the executable so the final step is a same-directory rename,
	// and must match the glob sweepBackups uses.
	if filepath.Dir(a) != filepath.Dir(exe) && !strings.HasPrefix(a, exe+".old") {
		t.Errorf("backupName = %q, want a %s.old* sibling", a, exe)
	}
}

func TestSweepBackupsRemovesLeftovers(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sbxup")
	write(t, exe, "binary")
	for _, leftover := range []string{exe + ".old", exe + ".old-111", exe + ".old-222"} {
		write(t, leftover, "stale")
	}

	sweepBackups(exe)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "sbxup" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("after sweep: %v, want only the executable", names)
	}
}

// --- release tarball -------------------------------------------------------

// tarGz builds a gzipped tar in memory. A nil-bodied entry with a trailing slash is a
// directory; setting link makes it a symlink, which extraction must refuse.
func tarGz(t *testing.T, entries []tar.Header, bodies []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for i, h := range entries {
		hdr := h
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(bodies[i]))
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(bodies[i])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func reg(name string) tar.Header  { return tar.Header{Name: name, Typeflag: tar.TypeReg} }
func dir_(name string) tar.Header { return tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755} }

func TestExtractTarGzWritesTheSrcTree(t *testing.T) {
	data := tarGz(t,
		[]tar.Header{dir_("src/"), dir_("src/sbx-claude-dotnet10/"), reg("src/sbx-claude-dotnet10/Dockerfile"), reg("src/sbx-claude-dotnet10/template.yaml")},
		[]string{"", "", "FROM scratch\n", "name: sbx-claude-dotnet10\n"})

	dest := t.TempDir()
	n, err := extractTarGz(data, dest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("wrote %d files, want 2", n)
	}
	got, err := os.ReadFile(filepath.Join(dest, "src", "sbx-claude-dotnet10", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "FROM scratch\n" {
		t.Errorf("Dockerfile = %q", got)
	}
}

func TestExtractTarGzRefusesEscapesAndOddEntries(t *testing.T) {
	cases := []struct {
		name    string
		hdrs    []tar.Header
		bodies  []string
		wantErr string // empty => must succeed but write nothing outside src/
	}{
		{
			name:    "parent traversal",
			hdrs:    []tar.Header{reg("src/../../escaped.txt")},
			bodies:  []string{"pwned"},
			wantErr: "", // Clean() moves it out of src/, so it is ignored, not written
		},
		{
			name:    "absolute path",
			hdrs:    []tar.Header{reg("/etc/passwd")},
			bodies:  []string{"pwned"},
			wantErr: "",
		},
		{
			name:    "symlink inside src",
			hdrs:    []tar.Header{{Name: "src/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
			bodies:  []string{""},
			wantErr: "unsupported type",
		},
		{
			name:    "hard link inside src",
			hdrs:    []tar.Header{{Name: "src/hard", Typeflag: tar.TypeLink, Linkname: "src/x"}},
			bodies:  []string{""},
			wantErr: "unsupported type",
		},
		{
			name:    "outside src is ignored",
			hdrs:    []tar.Header{reg("README.md"), dir_("src/"), reg("src/t/Dockerfile")},
			bodies:  []string{"hi", "", "FROM scratch\n"},
			wantErr: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dest := t.TempDir()
			_, err := extractTarGz(tarGz(t, c.hdrs, c.bodies), dest)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want success", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
			// Whatever happened, nothing may exist outside the src/ subtree.
			entries, err := os.ReadDir(dest)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != "src" {
					t.Errorf("extraction wrote %q outside src/", e.Name())
				}
			}
			if _, err := os.Stat(filepath.Join(dest, "..", "escaped.txt")); err == nil {
				t.Error("extraction escaped the destination directory")
			}
		})
	}
}

func TestExtractTarGzRejectsTooManyEntries(t *testing.T) {
	hdrs := make([]tar.Header, maxTarEntries+1)
	bodies := make([]string, maxTarEntries+1)
	for i := range hdrs {
		hdrs[i] = reg(fmt.Sprintf("src/t/f%d", i))
		bodies[i] = "x"
	}
	if _, err := extractTarGz(tarGz(t, hdrs, bodies), t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "entries") {
		t.Fatalf("err = %v, want an entry-count refusal", err)
	}
}

func TestExtractTarGzRejectsNonGzip(t *testing.T) {
	if _, err := extractTarGz([]byte("not a gzip stream"), t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "gzip") {
		t.Fatalf("err = %v, want a gzip error", err)
	}
}

// cacheHome points os.UserCacheDir at a temp dir on every platform.
func cacheHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir) // linux
	t.Setenv("LocalAppData", dir)   // windows
	t.Setenv("HOME", dir)           // darwin: $HOME/Library/Caches
	return dir
}

// tarballServer serves manifest.json, a tarball and their .sha256 sidecars, counting hits.
func tarballServer(t *testing.T, tarball []byte, hits *int) *httptest.Server {
	t.Helper()
	sum := func(b []byte) string { d := sha256.Sum256(b); return hex.EncodeToString(d[:]) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		switch {
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			w.Write(tarball)
		case strings.HasSuffix(r.URL.Path, ".tar.gz.sha256"):
			fmt.Fprintf(w, "%s  templates-0.2.5.tar.gz\n", sum(tarball))
		default:
			http.NotFound(w, r)
		}
	}))
	orig := repoWeb
	repoWeb = srv.URL
	t.Cleanup(func() { repoWeb = orig; srv.Close() })
	return srv
}

func TestFetchDockerfileUsesTheTarball(t *testing.T) {
	cacheHome(t)
	tarball := tarGz(t,
		[]tar.Header{dir_("src/"), reg("src/sbx-claude-dotnet10/Dockerfile"), reg("src/sbx-claude-python-uv/Dockerfile")},
		[]string{"", "FROM dotnet\n", "FROM python\n"})

	hits := 0
	srv := tarballServer(t, tarball, &hits)
	m := &Manifest{Tarball: "templates-0.2.5.tar.gz"}
	dotnet := &TemplateEntry{Name: "sbx-claude-dotnet10"}
	python := &TemplateEntry{Name: "sbx-claude-python-uv"}

	path, err := fetchDockerfile(srv.Client(), "templates-v0.2.5", m, dotnet, false)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(path); string(body) != "FROM dotnet\n" {
		t.Errorf("Dockerfile = %q", body)
	}
	// The context directory handed to `docker build` is the template's own directory.
	if filepath.Base(filepath.Dir(path)) != "sbx-claude-dotnet10" {
		t.Errorf("context dir = %q", filepath.Dir(path))
	}

	after := hits
	// A second template from the same release is already on disk: no further downloads.
	if _, err := fetchDockerfile(srv.Client(), "templates-v0.2.5", m, python, false); err != nil {
		t.Fatal(err)
	}
	if hits != after {
		t.Errorf("second template caused %d extra requests, want 0", hits-after)
	}
}

func TestEnsureTemplateTreeRefreshReplacesStaleFiles(t *testing.T) {
	cacheHome(t)
	hits := 0
	old := tarGz(t, []tar.Header{dir_("src/"), reg("src/t/Dockerfile"), reg("src/t/gone.txt")},
		[]string{"", "FROM old\n", "stale"})
	srv := tarballServer(t, old, &hits)
	m := &Manifest{Tarball: "templates-0.2.5.tar.gz"}

	tree, err := ensureTemplateTree(srv.Client(), "templates-v0.2.5", m, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tree, "t", "gone.txt")); err != nil {
		t.Fatal("first extraction is incomplete")
	}

	// Re-publish the release without gone.txt; --refresh must not leave it behind.
	srv.Close()
	hits = 0
	fresh := tarGz(t, []tar.Header{dir_("src/"), reg("src/t/Dockerfile")}, []string{"", "FROM new\n"})
	tarballServer(t, fresh, &hits)

	tree, err = ensureTemplateTree(srv.Client(), "templates-v0.2.5", m, true)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(tree, "t", "Dockerfile")); string(body) != "FROM new\n" {
		t.Errorf("Dockerfile = %q, want the refreshed content", body)
	}
	if _, err := os.Stat(filepath.Join(tree, "t", "gone.txt")); err == nil {
		t.Error("refresh kept a file the new release no longer ships")
	}
	// The swap must not leave extraction temp dirs behind.
	entries, _ := os.ReadDir(filepath.Dir(tree))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".sbxup-extract-") {
			t.Errorf("leftover temp dir %q", e.Name())
		}
	}
}
