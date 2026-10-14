package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestMain keeps every test away from a real `sbx`: a test that needs its output stubs sbxOutput
// (or a seam above it), and anything else gets "not available".
func TestMain(m *testing.M) {
	sbxOutput = func(args ...string) ([]byte, error) {
		return nil, errors.New("sbx is not available in tests")
	}
	os.Exit(m.Run())
}

// stubSbx answers sbxOutput from a table of "args" -> stdout; anything else fails like an sbx
// that does not know the command or flag.
func stubSbx(t *testing.T, answers map[string]string) *[]string {
	t.Helper()
	var calls []string
	orig := sbxOutput
	t.Cleanup(func() { sbxOutput = orig; templateJSONWarned = false })
	sbxOutput = func(args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		calls = append(calls, key)
		if out, ok := answers[key]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("unknown flag")
	}
	return &calls
}

func TestSandboxNamesJSON(t *testing.T) {
	for in, want := range map[string]string{
		`[{"name":"claude-AbcVersion","status":"running","detached":false},{"name":"claude-x"}]`: "claude-AbcVersion,claude-x", // 0.48
		`{"sandboxes":[{"name":"claude-AbcVersion"}]}`:                                           "claude-AbcVersion",          // before 0.48
		`[]`:               "",
		`{"sandboxes":[]}`: "",
	} {
		got, err := sandboxNamesJSON([]byte(in))
		if err != nil || strings.Join(got, ",") != want {
			t.Errorf("%s: got %v, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{`not json`, `{"other":[]}`, `[{"id":"1"}]`} {
		if _, err := sandboxNamesJSON([]byte(in)); err == nil {
			t.Errorf("%s: accepted", in)
		}
	}
}

func TestResolveSandboxNameReadsJSON(t *testing.T) {
	stubSbx(t, map[string]string{
		// 0.48 table: status marks and a CREATED column; the JSON is what is read.
		"ls --json": `[{"name":"claude-AbcVersion","status":"running"}]`,
		"ls":        "SANDBOX            AGENT    STATUS      CREATED\nclaude-AbcVersion  claude   ● running   2m\n",
	})
	if got := resolveSandboxName("claude-abcversion"); got != "claude-AbcVersion" {
		t.Fatalf("resolveSandboxName = %q", got)
	}
	// A word of the table that is not a sandbox name must not match through the JSON path.
	if got := resolveSandboxName("running"); got != "" {
		t.Fatalf("matched a status word: %q", got)
	}
}

func TestSbxNamesFallsBackToTheTable(t *testing.T) {
	calls := stubSbx(t, map[string]string{
		"list": "NAME                STATUS\nclaude-AbcVersion   running\n",
	})
	if got := resolveSandboxName("claude-abcversion"); got != "claude-AbcVersion" {
		t.Fatalf("resolveSandboxName = %q", got)
	}
	if strings.Join(*calls, "|") != "ls --json|ls|list" {
		t.Fatalf("calls %v, want JSON, then ls, then the old list", *calls)
	}
}

func TestTemplateRefsJSON(t *testing.T) {
	cases := map[string]string{
		// 0.48: an array; field names as Docker uses them.
		`[{"repository":"docker.io/library/sbx-claude-dotnet10","tag":"0.2.8","id":"c0ffee"}]`: "docker.io/library/sbx-claude-dotnet10:0.2.8",
		// before 0.48: wrapped, any casing.
		`{"images":[{"Repository":"sbx-claude-dotnet10","Tag":"0.1.3"}]}`: "docker.io/library/sbx-claude-dotnet10:0.1.3",
		// a single reference
		`[{"name":"pkudrel/sbx-claude-python-uv:latest"}]`: "docker.io/pkudrel/sbx-claude-python-uv:latest",
		// a list of references
		`[{"id":"x","tags":["sbx-claude-dotnet10:0.2.8","sbx-claude-dotnet10:0.2.9"]}]`: "docker.io/library/sbx-claude-dotnet10:0.2.8,docker.io/library/sbx-claude-dotnet10:0.2.9",
		`[]`: "",
	}
	for in, want := range cases {
		got, err := templateRefsJSON([]byte(in))
		if err != nil || strings.Join(got, ",") != want {
			t.Errorf("%s: got %v, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{`table text`, `{"other":1}`, `[{"id":"c0ffee","size":1}]`} {
		if _, err := templateRefsJSON([]byte(in)); err == nil {
			t.Errorf("%s: accepted", in)
		}
	}
}

func TestSbxTemplateListedReadsJSON(t *testing.T) {
	stubSbx(t, map[string]string{
		"template ls --json": `[{"repository":"sbx-claude-dotnet10","tag":"0.1.3"},{"repository":"sbx-claude-dotnet10","tag":"0.2.8"},` +
			`{"repository":"docker.io/pkudrel/sbx-claude-dotnet10","tag":"latest"}]`,
	})
	if !sbxTemplateListed("sbx-claude-dotnet10:0.2.8") || sbxTemplateListed("sbx-claude-dotnet10:0.2.9") {
		t.Fatal("listed answers wrong")
	}
	if got := sbxTemplateTags("sbx-claude-dotnet10"); strings.Join(got, ",") != "0.1.3,0.2.8" {
		t.Fatalf("tags = %v", got)
	}
}

// TestSbxTemplateUnknownJSONWarnsAndUsesTheTable: a JSON shape sbxup cannot read must not look like
// "nothing is built" (which would rebuild on every start); it warns once and reads the table.
func TestSbxTemplateUnknownJSONWarnsAndUsesTheTable(t *testing.T) {
	stubSbx(t, map[string]string{
		"template ls --json": `[{"id":"c0ffee","size":123}]`,
		"template ls":        sbxTemplateLsOutput,
	})
	var listed bool
	out := capture(t, func() {
		listed = sbxTemplateListed("sbx-claude-dotnet10:0.1.3")
		sbxTemplateListed("sbx-claude-dotnet10:0.1.3")
	})
	if !listed {
		t.Fatal("the table fallback did not find the template")
	}
	if strings.Count(out, "WARNING") != 1 || !strings.Contains(out, "sbx template ls --json") {
		t.Fatalf("want one warning, got: %s", out)
	}
}

func TestSbxTemplateOldSbxWithoutJSON(t *testing.T) {
	stubSbx(t, map[string]string{"template ls": sbxTemplateLsOutput})
	out := capture(t, func() {
		if !sbxTemplateListed("sbx-claude-dotnet10:0.1.3") {
			t.Error("not listed")
		}
	})
	if strings.Contains(out, "WARNING") {
		t.Fatalf("an sbx without --json warned: %s", out)
	}
	if _, ok := sbxTemplateRefs(); !ok {
		t.Fatal("table answer counted as no answer")
	}
}
