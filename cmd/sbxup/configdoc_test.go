package main

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// kebab turns a Go field name into its config key: BitwardenItem -> bitwarden-item,
// ClientIDField -> client-id-field, GitHub -> github.
func kebab(name string) string {
	if name == "GitHub" {
		return "github"
	}
	r := []rune(name)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 {
			prevLower := unicode.IsLower(r[i-1])
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if prevLower || (nextLower && unicode.IsUpper(r[i-1])) {
				b.WriteByte('-')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

func TestKebab(t *testing.T) {
	for in, want := range map[string]string{
		"Template": "template", "GitHub": "github", "BitwardenItem": "bitwarden-item",
		"ClientIDField": "client-id-field", "AppIDField": "app-id-field",
	} {
		if got := kebab(in); got != want {
			t.Errorf("kebab(%q) = %q, want %q", in, got, want)
		}
	}
}

func docFor(key string) *configDoc {
	for i := range configDocs {
		if configDocs[i].Key == key {
			return &configDocs[i]
		}
	}
	return nil
}

// TestConfigDocsCoverEveryKey fails when a field is added to Config or GitHubConfig without an entry
// in the --init template or in the help text.
func TestConfigDocsCoverEveryKey(t *testing.T) {
	ct := reflect.TypeOf(Config{})
	for i := 0; i < ct.NumField(); i++ {
		key := kebab(ct.Field(i).Name)
		if key == "template" || key == "version" {
			continue // written by renderConfig itself
		}
		if docFor(key) == nil {
			t.Errorf("Config.%s has no entry in configDocs (key %q)", ct.Field(i).Name, key)
		}
		if !strings.Contains(usage, "  "+key+":") {
			t.Errorf("usage does not show %q", key)
		}
	}

	// The github key has one entry per mode; together they must show every field.
	var lines []string
	for _, d := range configDocs {
		if d.Key == "github" {
			lines = append(lines, d.Lines...)
		}
	}
	if len(lines) == 0 {
		t.Fatal("no github doc")
	}
	body := strings.Join(lines, "\n")
	gt := reflect.TypeOf(GitHubConfig{})
	for i := 0; i < gt.NumField(); i++ {
		key := kebab(gt.Field(i).Name)
		if !strings.Contains(body, "  "+key+":") {
			t.Errorf("GitHubConfig.%s is missing from the github doc (key %q)", gt.Field(i).Name, key)
		}
		if !strings.Contains(usage, key+":") {
			t.Errorf("usage does not show github.%s", key)
		}
	}
}

// TestConfigDocsUncommentToAValidConfig takes each commented block, removes "# " and loads it.
func TestConfigDocsUncommentToAValidConfig(t *testing.T) {
	chdir(t)
	blocks := append([]configDoc{{Key: "version", Lines: []string{"version: latest"}}}, configDocs...)
	for _, d := range blocks {
		t.Run(d.Key, func(t *testing.T) {
			write(t, "c.yaml", "template: dotnet10\n"+strings.Join(d.Lines, "\n")+"\n")
			var cfg *Config
			var err error
			out := capture(t, func() { cfg, err = loadConfig("c.yaml") })
			if err != nil {
				t.Fatalf("does not load: %v\n%s", err, strings.Join(d.Lines, "\n"))
			}
			if strings.Contains(out, "WARNING") {
				t.Fatalf("warns: %s", out)
			}
			// Each block must actually set something, or it documents a key that is not read.
			if reflect.DeepEqual(*cfg, Config{Template: "dotnet10"}) && d.Key != "version" {
				t.Fatalf("uncommenting %s changed nothing", d.Key)
			}
		})
	}
}

// TestRenderedConfigStatesNoDefaults: everything but the template is a comment.
func TestRenderedConfigStatesNoDefaults(t *testing.T) {
	chdir(t)
	for name, body := range map[string]string{
		"offline":  defaultConfigBody,
		"releases": buildConfigBody(&TemplateEntry{Name: "sbx-claude-dotnet10", Short: "dotnet10"}, defaultRef("templates-v0.2.9")),
	} {
		t.Run(name, func(t *testing.T) {
			write(t, "c.yaml", body)
			var cfg *Config
			var err error
			out := capture(t, func() { cfg, err = loadConfig("c.yaml") })
			if err != nil || strings.Contains(out, "WARNING") {
				t.Fatalf("err=%v out=%s", err, out)
			}
			if !reflect.DeepEqual(*cfg, Config{Template: "dotnet10"}) {
				t.Fatalf("the generated config states more than the template: %+v", *cfg)
			}
			for _, d := range configDocs {
				if !strings.Contains(body, "# "+d.Lines[0]) {
					t.Errorf("%s is not offered:\n%s", d.Key, body)
				}
			}
		})
	}
}

func TestInitWarnsWithTheDryRunHint(t *testing.T) {
	chdir(t)
	write(t, defaultConfigPath, "template: mine\n")
	out := capture(t, func() {
		if err := initConfig(""); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "--init --dry-run") {
		t.Fatalf("no hint: %s", out)
	}
}
