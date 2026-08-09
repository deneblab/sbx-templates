package main

import (
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

func TestFindConfigSearchOrder(t *testing.T) {
	t.Run("prefers .agents/sbxup.yaml", func(t *testing.T) {
		chdir(t)
		write(t, ".agents/sbxup.yaml", "agent: a\n")
		write(t, ".agents/sbx-runner.yaml", "agent: b\n")
		write(t, "sbxup.yaml", "agent: c\n")
		if got := findConfig(); got != ".agents/sbxup.yaml" {
			t.Fatalf("findConfig() = %q", got)
		}
	})

	t.Run("falls back to the legacy sbx-runner name", func(t *testing.T) {
		chdir(t)
		write(t, ".agents/sbx-runner.yaml", "agent: b\n")
		if got := findConfig(); got != ".agents/sbx-runner.yaml" {
			t.Fatalf("findConfig() = %q", got)
		}
	})

	t.Run("no config", func(t *testing.T) {
		chdir(t)
		if got := findConfig(); got != "" {
			t.Fatalf("findConfig() = %q, want empty", got)
		}
	})
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
