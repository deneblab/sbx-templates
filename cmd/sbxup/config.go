package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultConfigPath = ".agents/sbxup.yaml"

// configSearchPaths is the ordered lookup for an implicit config. The sbx-runner names are
// kept so repositories set up for the previous PowerShell tool keep working untouched.
var configSearchPaths = []string{
	".agents/sbxup.yaml",
	".agents/sbx-runner.yaml",
	"sbxup.yaml",
	"sbx-runner.yaml",
}

const defaultConfigBody = `template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false
`

// Config is the resolved sbxup.yaml. Clone is decoded leniently because the PowerShell
// version accepted true/1/yes/on as strings as well as a real YAML boolean.
type Config struct {
	Template string
	Agent    string
	Clone    bool
	Cache    string
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
		case "branch":
			warnf("Key 'branch' in %s is no longer supported ('sbx run' dropped --branch). "+
				"Rename it to 'clone: true|false'.", path)
		default:
			warnf("Unknown key '%s' in %s (expected: template, agent, clone, cache)", key, path)
		}
	}
	return cfg, nil
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

// initConfig writes a starter config, refusing to clobber an existing one.
func initConfig(path string) error {
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
	if err := os.WriteFile(path, []byte(defaultConfigBody), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	fmt.Printf("Created config: %s\n", path)
	return nil
}
