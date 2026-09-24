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

// defaultAgent applies when neither the config nor --agent names one: every template is a
// sbx-claude-* image, so the key would only ever say the same thing.
const defaultAgent = "claude"

// defaultConfigBody is what --init writes when it cannot list the release (no network, no
// terminal). It needs no network to be valid: a template is just a name, resolved on first run.
const defaultConfigBody = `template: dotnet10
`

// BuildConfig is the deprecated `build:` block, still read so existing configs keep working.
// It is folded into Config.Template and Config.Version: every template is built locally now, so
// the block no longer marks anything. New configs write `template:` and `version:` instead.
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
	// Template names a template from the templates-v* release, e.g. "dotnet10". It is always
	// built locally; sbxup never pulls one of our images from a registry.
	Template string
	// Version pins the release the template comes from, in any form parseReleaseRef accepts:
	// "0.2.8", "templates-v0.2.8", "latest", or "owner/repo@0.2.8" for a fork. Empty floats at
	// the newest release. It pins the release, not the image version: the two counters are
	// independent, which is why the start-up line prints both.
	Version string
	Agent   string
	Clone   bool
	Cache   string
	// Mounts are extra host directories, each `path` or `path:ro`. Kept raw: they are resolved and
	// checked at run time, when the project directory and the home directory are known.
	Mounts []string
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
	var (
		build    *BuildConfig
		template string
	)
	for key, val := range raw {
		switch strings.ToLower(key) {
		case "template":
			template = scalarString(val)
		case "version":
			cfg.Version = scalarString(val)
		case "agent":
			cfg.Agent = scalarString(val)
		case "cache":
			cfg.Cache = scalarString(val)
		case "clone":
			cfg.Clone = isTruthy(val)
		case "mounts":
			m, err := parseMounts(val, path)
			if err != nil {
				return nil, err
			}
			cfg.Mounts = m
		case "build":
			b, err := parseBuild(val, path)
			if err != nil {
				return nil, err
			}
			build = b
		case "branch":
			warnf("Key 'branch' in %s is no longer supported ('sbx run' dropped --branch). "+
				"Rename it to 'clone: true|false'.", path)
		default:
			warnf("Unknown key '%s' in %s (expected: template, version, agent, clone, cache, mounts)", key, path)
		}
	}

	if build != nil {
		// A config written before templates were always built locally. The block wins over a
		// `template:` beside it, exactly as before: that value named the image tag, not the
		// template, so it is not validated either.
		warnf("'build:' in %s is deprecated. Write 'template: %s' and, to pin a release, "+
			"'version: <release>' instead.", path, build.Name)
		cfg.Template = build.Name
		if build.Release != "" {
			if cfg.Version != "" && cfg.Version != build.Release {
				return nil, fmt.Errorf("%s sets both 'version: %s' and 'build.release: %s' — keep only 'version'",
					path, cfg.Version, build.Release)
			}
			cfg.Version = build.Release
		}
		return cfg, nil
	}
	if template != "" {
		if err := checkTemplateName(template, "'template' in "+path); err != nil {
			return nil, err
		}
		cfg.Template = template
	}
	return cfg, nil
}

// parseMounts reads the `mounts:` list. A single string is accepted as a list of one.
func parseMounts(val any, path string) ([]string, error) {
	switch t := val.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, nil
		}
		return []string{strings.TrimSpace(t)}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("each entry of 'mounts' in %s must be a path such as '~/shared-libs:ro', got %v", path, item)
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("'mounts' in %s must be a list of paths", path)
	}
}

// checkTemplateName rejects an image reference where a template name belongs. Before templates
// were always built locally, `template:` held a registry image; a config that still does would
// otherwise fail later with "no template" and no hint that the meaning changed. The error names
// the replacement.
func checkTemplateName(value, where string) error {
	if !strings.ContainsAny(value, "/:@") {
		return nil
	}
	hint := ""
	if name := suggestTemplateName(value); name != "" {
		hint = fmt.Sprintf(" — e.g. 'template: %s'", name)
	}
	return fmt.Errorf("%s is %q, an image reference, but sbxup now builds templates locally and never pulls one. "+
		"Name a template from the release instead%s ('sbxup --init' lists them)", where, value, hint)
}

// suggestTemplateName reduces an image reference to the bare name it would have been built
// from: docker.io/pkudrel/sbx-claude-dotnet10:latest -> sbx-claude-dotnet10.
func suggestTemplateName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	if i := strings.IndexAny(ref, ":@"); i >= 0 {
		ref = ref[:i]
	}
	return ref
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

// buildConfigBody renders a config for a template chosen from a release.
//
// No pin is written: `template` is the bare name and the release floats at latest. Writing a
// resolved pin here was the reason a user had to know a release tag before they could edit their
// own config — pinning stays available, as a deliberate edit, and a commented example shows how.
// A fork is the exception: without an explicit `version` the config would silently build the
// canonical repository's template instead of the one that was picked.
func buildConfigBody(t *TemplateEntry, ref releaseRef) string {
	name := t.Short
	if name == "" {
		name = t.Name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "template: %s   # built locally from src/%s/Dockerfile in the release tarball\n", name, t.Name)
	if !ref.isDefaultSource() {
		fmt.Fprintf(&b, "version: %s   # this template comes from a fork\n",
			releaseRef{Owner: ref.Owner, Repo: ref.Repo})
	}
	if pin := strings.TrimPrefix(ref.Tag, templatesTagPrefix); pin != "" {
		fmt.Fprintf(&b, "# version: %s   # pin the release to freeze the environment; without it sbxup follows the latest\n", pin)
	}
	return b.String()
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
