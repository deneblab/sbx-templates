package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// defaultConfigPath is the one location sbxup reads. There is deliberately no search order:
// a single path means the config a project has is the config sbxup uses, with no ambiguity
// about which of several files won.
const defaultConfigPath = ".sbx/sbxup.config.yaml"

// legacyConfigPaths are no longer loaded. They are probed only so that a project still holding
// one gets "rename this file" instead of a bare "config not found".
var legacyConfigPaths = []string{
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
	Name string // template name or short alias, e.g. "dotnet10"
	// Release names the source repository and the release, in any form parseReleaseRef
	// accepts: "deneblab/sbx-templates@0.1.4", "@latest", a bare "0.1.4" or tag for the
	// default repository, or empty for its newest release.
	Release string
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

// findConfig returns the config path if it exists, or "" when it does not.
func findConfig() string {
	if _, err := os.Stat(defaultConfigPath); err == nil {
		return defaultConfigPath
	}
	return ""
}

// legacyConfig returns the first no-longer-supported config file present, or "". Used purely
// to make the missing-config error actionable.
func legacyConfig() string {
	for _, p := range legacyConfigPaths {
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

// buildConfigBody renders a config wired to a locally built template.
//
// No version appears anywhere: `template` is the bare template name and `release` floats at
// @latest. Writing a resolved pin here was the reason a user had to know a release tag before
// they could edit their own config — pinning stays available, as a deliberate edit.
func buildConfigBody(t *TemplateEntry, ref releaseRef) string {
	source := releaseRef{Owner: ref.Owner, Repo: ref.Repo} // no Tag => @latest
	return fmt.Sprintf(`template: %s
agent: claude
clone: false
build:
  name: %s   # built locally from src/%s/Dockerfile in the release tarball
  release: %s   # replace 'latest' with a version, e.g. @%s, to freeze the environment
`, t.Name, t.Name, t.Name, source, versionOrLatest(t))
}

// versionOrLatest supplies the example pin in the generated comment: the version actually
// resolved, so the user can see the shape of the value they would be typing.
func versionOrLatest(t *TemplateEntry) string {
	if t.Version == "" {
		return "latest"
	}
	return t.Version
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
