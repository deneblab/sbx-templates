package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultConfigPath = ".sbx/sbxup.config.yaml"

// configSearchPaths is the ordered lookup for an implicit config. `.sbx/` is the current home;
// the `.agents/` and sbx-runner names are kept so repositories set up for earlier versions —
// including the previous PowerShell tool — keep working untouched. `.sbx/sbxup.yaml` is
// accepted as a near-miss of the canonical name rather than failing with "config not found".
var configSearchPaths = []string{
	".sbx/sbxup.config.yaml",
	".sbx/sbxup.yaml",
	".agents/sbxup.yaml",
	".agents/sbx-runner.yaml",
	"sbxup.yaml",
	"sbx-runner.yaml",
}

const defaultConfigBody = `template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false
`

// BuildConfig is the optional `build:` block. Its presence is what marks a template as
// locally built rather than pulled: with it set, sbxup builds the named template from the
// templates-v* release and runs the resulting local image.
type BuildConfig struct {
	Name    string // template name or short alias, e.g. "dotnet10"
	Release string // optional pin, e.g. "templates-v0.1.3"; empty means latest
}

// Config is the resolved sbxup.yaml. Clone is decoded leniently because the PowerShell
// version accepted true/1/yes/on as strings as well as a real YAML boolean.
type Config struct {
	Template string
	Agent    string
	Clone    bool
	Cache    string
	Build    *BuildConfig
}

// findConfig returns the first existing config in search order, or "" when none exists.
func findConfig() string {
	for _, p := range configSearchPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// loadConfig reads and validates a config file, warning about unknown and removed keys
// exactly as the PowerShell implementation did.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config %s: %w", path, err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", path, err)
	}

	cfg := &Config{}
	for key, val := range raw {
		switch strings.ToLower(key) {
		case "template":
			cfg.Template = scalarString(val)
		case "agent":
			cfg.Agent = scalarString(val)
		case "cache":
			cfg.Cache = scalarString(val)
		case "clone":
			cfg.Clone = isTruthy(val)
		case "build":
			b, err := parseBuild(val, path)
			if err != nil {
				return nil, err
			}
			cfg.Build = b
		case "branch":
			warnf("Key 'branch' in %s is no longer supported ('sbx run' dropped --branch). "+
				"Rename it to 'clone: true|false'.", path)
		default:
			warnf("Unknown key '%s' in %s (expected: template, agent, clone, cache, build)", key, path)
		}
	}
	return cfg, nil
}

// parseBuild decodes the `build:` block. A bare string is accepted as shorthand for the
// template name, so `build: dotnet10` means the same as `build: {name: dotnet10}`.
func parseBuild(val any, path string) (*BuildConfig, error) {
	switch t := val.(type) {
	case nil:
		return nil, nil
	case string:
		return &BuildConfig{Name: strings.TrimSpace(t)}, nil
	case map[string]any:
		b := &BuildConfig{}
		for k, v := range t {
			switch strings.ToLower(k) {
			case "name", "template":
				b.Name = scalarString(v)
			case "release", "version":
				b.Release = scalarString(v)
			default:
				warnf("Unknown key 'build.%s' in %s (expected: name, release)", k, path)
			}
		}
		if b.Name == "" {
			return nil, fmt.Errorf("'build' in %s needs a 'name' (the template to build)", path)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("'build' in %s must be a template name or a mapping", path)
	}
}

// scalarString renders a YAML scalar as a trimmed string without quoting artefacts.
func scalarString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

// isTruthy mirrors the PowerShell _IsTruthy helper: real booleans plus the string forms.
func isTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case nil:
		return false
	}
	switch strings.ToLower(scalarString(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// buildConfigBody renders a config wired to a locally built template: `template` is the tag
// the build produces, and the `build` block records how to reproduce it. The release is
// pinned so a later `sbxup` rebuilds the same thing rather than silently following upstream.
func buildConfigBody(t *TemplateEntry, release string) string {
	return fmt.Sprintf(`template: %s
agent: claude
clone: false
build:
  name: %s          # built locally from %s — no Docker Hub pull
  release: %s
`, t.LocalTag(), t.Short, t.Dockerfile, release)
}

// initConfig writes the default starter config, refusing to clobber an existing one.
func initConfig(path string) error {
	return initConfigBody(path, defaultConfigBody)
}

// initConfigBody writes body as the config file.
func initConfigBody(path, body string) error {
	if path == "" {
		path = defaultConfigPath
	}
	if _, err := os.Stat(path); err == nil {
		warnf("Config file already exists: %s", path)
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	fmt.Printf("Created config: %s\n", path)
	return nil
}
