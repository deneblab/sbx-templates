package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// policyLsJSON is `sbx policy ls sbxt003 --json` from a host (sbx 0.48), trimmed: a global rule, a kit
// rule, two sandbox HTTP deny rules, and one rule of another sandbox.
const policyLsJSON = `{"rules": [
 {"id":"default-allow-all","scope":"global","resource_type":"network","decision":"allow","resources":["**"]},
 {"id":"8b00","scope":"sandbox:sbxt003","resource_type":"network","decision":"allow","resources":["api.anthropic.com:443"],"provenance":{"created_via":"provisioned"}},
 {"id":"6591","scope":"sandbox:sbxt003","resource_type":"http","decision":"deny","resources":["github.com"],
  "methods":["POST"],"http_targets":[{"host":"github.com","path":"/*/*/git-receive-pack"}]},
 {"id":"cd6b","scope":"sandbox:sbxt003","resource_type":"http","decision":"deny","resources":["api.github.com"],
  "methods":["DELETE"],"http_targets":[{"host":"api.github.com","path":"/repos/**"}]},
 {"id":"zzzz","scope":"sandbox:other","resource_type":"http","decision":"deny","resources":["github.com"],
  "methods":["GET"],"http_targets":[{"host":"github.com","path":"/**"}]}
]}`

func TestRestrictRules(t *testing.T) {
	if len(restrictRules(nil)) != 0 || len(restrictRules(&RestrictConfig{})) != 0 {
		t.Fatal("rules without restrictions")
	}
	push := restrictRules(&RestrictConfig{NoPush: true})
	if len(push) != 6 || push[0].String() != "POST github.com/*/*/git-receive-pack" {
		t.Fatalf("no-push rules: %v", push)
	}
	for _, r := range push {
		// A rule naming a repository is bypassed by changing the case of the URL (host check B).
		if strings.Contains(r.Path, "deneblab") || !strings.Contains(r.Path, "*") {
			t.Errorf("rule without wildcards: %s", r)
		}
	}
	del := restrictRules(&RestrictConfig{NoDelete: true})
	if len(del) != 1 || del[0].String() != "DELETE api.github.com/repos/**" {
		t.Fatalf("no-delete rules: %v", del)
	}
	if restrictSummary(&RestrictConfig{NoPush: true, NoDelete: true}) != "no push, no delete" {
		t.Fatal("summary")
	}
}

func TestLoadConfigRestrictBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	write(t, path, "template: dotnet10\nrestrict:\n  no-push: true\n  no-delete: false\n")
	cfg, err := loadConfig(path)
	if err != nil || cfg.Restrict == nil || !cfg.Restrict.NoPush || cfg.Restrict.NoDelete {
		t.Fatalf("parsed %+v, %v", cfg.Restrict, err)
	}
	for body, want := range map[string]string{
		"restrict:\n  no-psuh: true\n":  "unknown key 'restrict.no-psuh'",
		"restrict:\n  no-push: maybe\n": "use true or false",
		"restrict: yes\n":               "must be a block",
	} {
		write(t, path, "template: dotnet10\n"+body)
		if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", body, want, err)
		}
	}
}

func TestSandboxDenyRules(t *testing.T) {
	rules, err := sandboxDenyRules([]byte(policyLsJSON), "sbxt003")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rules {
		got = append(got, r.String())
	}
	if strings.Join(got, "; ") != "POST github.com/*/*/git-receive-pack; DELETE api.github.com/repos/**" {
		t.Fatalf("rules %v", got)
	}
	if _, err := sandboxDenyRules([]byte(`[]`), "x"); err == nil {
		t.Fatal("accepted a non-policy list")
	}
}

// fakePolicy stubs every sbx call applyRestrictions makes and records them.
type fakePolicy struct {
	listed   string // `sbx policy ls --json` stdout; "" fails
	denyFail bool
	calls    []string
	notices  []string
}

func (f *fakePolicy) install(t *testing.T) {
	t.Helper()
	cacheHome(t)
	origOut, origDeny, origRm, origExec := sbxOutput, sbxPolicyDeny, sbxPolicyRm, sbxExecStdin
	t.Cleanup(func() { sbxOutput, sbxPolicyDeny, sbxPolicyRm, sbxExecStdin = origOut, origDeny, origRm, origExec })
	sbxOutput = func(args ...string) ([]byte, error) {
		f.calls = append(f.calls, strings.Join(args, " "))
		if f.listed == "" {
			return nil, errors.New("no")
		}
		return []byte(f.listed), nil
	}
	sbxPolicyDeny = func(sandbox string, r policyRule) error {
		f.calls = append(f.calls, "deny "+sandbox+" "+r.String())
		if f.denyFail {
			return errors.New("refused")
		}
		return nil
	}
	sbxPolicyRm = func(sandbox string, r policyRule) error {
		f.calls = append(f.calls, "rm "+sandbox+" "+r.String())
		return nil
	}
	sbxExecStdin = func(sandbox, script, stdin string) error {
		f.calls = append(f.calls, "exec "+sandbox)
		f.notices = append(f.notices, stdin)
		return nil
	}
}

func (f *fakePolicy) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestApplyRestrictionsLifecycle(t *testing.T) {
	f := &fakePolicy{listed: `{"rules":[]}`}
	f.install(t)
	both := &RestrictConfig{NoPush: true, NoDelete: true}

	// A new sandbox: every rule added, the agent told once.
	capture(t, func() {
		if err := applyRestrictions("claude-x", both, true, false); err != nil {
			t.Fatal(err)
		}
	})
	if f.count("deny ") != 7 || f.count("exec ") != 1 || !strings.Contains(f.notices[0], "No push") {
		t.Fatalf("first start: %v", f.calls)
	}

	// A resume with everything in place: nothing added, no exec.
	f.calls, f.notices = nil, nil
	f.listed = policyLsJSONFor("claude-x", restrictRules(both))
	capture(t, func() { applyRestrictions("claude-x", both, false, false) })
	if f.count("deny ") != 0 || f.count("exec ") != 0 {
		t.Fatalf("resume added again: %v", f.calls)
	}

	// The config drops no-delete: only that rule goes, and the notice changes.
	f.calls, f.notices = nil, nil
	capture(t, func() { applyRestrictions("claude-x", &RestrictConfig{NoPush: true}, false, false) })
	if f.count("rm ") != 1 || f.count("rm claude-x DELETE") != 1 || f.count("exec ") != 1 ||
		strings.Contains(f.notices[0], "No delete") {
		t.Fatalf("dropping no-delete: %v %q", f.calls, f.notices)
	}

	// The config drops restrict: all owned rules go and the notice is emptied.
	f.calls, f.notices = nil, nil
	capture(t, func() { applyRestrictions("claude-x", nil, false, false) })
	if f.count("rm ") != 6 || f.count("exec ") != 1 || f.notices[0] != "" {
		t.Fatalf("dropping restrict: %v %q", f.calls, f.notices)
	}

	// Nothing configured and nothing recorded: not a single sbx call.
	f.calls = nil
	applyRestrictions("claude-x", nil, false, false)
	if len(f.calls) != 0 {
		t.Fatalf("calls without restrictions: %v", f.calls)
	}
}

func TestApplyRestrictionsKeepsTheUsersRules(t *testing.T) {
	f := &fakePolicy{listed: policyLsJSON}
	f.install(t)
	// sbxt003 already has two deny rules the user added by hand; sbxup recorded none.
	capture(t, func() { applyRestrictions("sbxt003", &RestrictConfig{NoDelete: true}, false, false) })
	capture(t, func() { applyRestrictions("sbxt003", nil, false, false) })
	// Both rules were there before sbxup wanted one of them: neither is sbxup's to remove.
	if f.count("rm ") != 0 || f.count("deny ") != 0 {
		t.Fatalf("touched the user's rules: %v", f.calls)
	}
}

func TestApplyRestrictionsFailuresAndDryRun(t *testing.T) {
	f := &fakePolicy{denyFail: true}
	f.install(t)
	err := applyRestrictions("claude-x", &RestrictConfig{NoPush: true}, true, false)
	if err == nil || !strings.Contains(err.Error(), "not starting the sandbox") {
		t.Fatalf("a refused rule must stop the start: %v", err)
	}

	f2 := &fakePolicy{}
	f2.install(t)
	out := capture(t, func() { applyRestrictions("claude-x", &RestrictConfig{NoPush: true}, true, true) })
	if len(f2.calls) != 0 || strings.Count(out, "[dry-run] sbx policy deny network --sandbox claude-x") != 6 {
		t.Fatalf("dry-run: calls %v\n%s", f2.calls, out)
	}
}

// TestApplyRestrictionsFreshIgnoresAStaleRecord: a sandbox removed with `sbx rm` and created again under
// the same name has none of the old rules, so the old record must not make sbxup skip the notice.
func TestApplyRestrictionsFreshIgnoresAStaleRecord(t *testing.T) {
	f := &fakePolicy{listed: `{"rules":[]}`}
	f.install(t)
	c := &RestrictConfig{NoPush: true}
	capture(t, func() { applyRestrictions("claude-x", c, true, false) })
	f.calls = nil
	capture(t, func() { applyRestrictions("claude-x", c, true, false) })
	if f.count("deny ") != 6 || f.count("exec ") != 1 {
		t.Fatalf("fresh sandbox: %v", f.calls)
	}
}

func policyLsJSONFor(sandbox string, rules []policyRule) string {
	var parts []string
	for _, r := range rules {
		parts = append(parts, `{"scope":"sandbox:`+sandbox+`","resource_type":"http","decision":"deny","methods":["`+
			strings.Join(r.Methods, `","`)+`"],"http_targets":[{"host":"`+r.Host+`","path":"`+r.Path+`"}]}`)
	}
	return `{"rules":[` + strings.Join(parts, ",") + `]}`
}

// TestNoticeScriptReplacesItsBlock runs the real script with sh and awk on a copy of the file.
func TestNoticeScriptReplacesItsBlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh and awk")
	}
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("no awk")
	}
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "CLAUDE.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("# mine\nkeep this\n"), 0o644)
	run := func(stdin string) string {
		cmd := exec.Command("sh", "-c", noticeScript)
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(stdin)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		data, _ := os.ReadFile(path)
		return string(data)
	}
	notice := restrictNotice(&RestrictConfig{NoPush: true})
	run(notice)
	got := run(notice)
	if strings.Count(got, noticeStart) != 1 || !strings.HasPrefix(got, "# mine\nkeep this\n") {
		t.Fatalf("after two writes:\n%s", got)
	}
	got = run(restrictNotice(&RestrictConfig{NoDelete: true}))
	if strings.Contains(got, "No push") || !strings.Contains(got, "No delete") || strings.Count(got, noticeStart) != 1 {
		t.Fatalf("after a change:\n%s", got)
	}
	if got = run(""); got != "# mine\nkeep this\n" {
		t.Fatalf("after removal:\n%q", got)
	}
}

func TestJoinSandboxName(t *testing.T) {
	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"claude-AbcVersion"}, "claude-AbcVersion"},
		{[]string{"my_app 2", "claude"}, "my-app-2--claude"},
		{[]string{"proj.v2", "claude"}, "proj-v2--claude"},
		{[]string{"antipiracy", "antipiracy_simplescraper", "claude"}, "antipiracy--antipiracy-simplescraper--claude"},
		{[]string{"a--b_", "-c-"}, "a-b--c"},
		{[]string{"__", "x", "y"}, "x--y"},
		{[]string{"-x"}, "sbx-x"},
		{[]string{"_x", "y"}, "x--y"},
		{[]string{"default"}, "sbx-default"},
		{[]string{"_"}, "sbx"},
	} {
		got := joinSandboxName(tc.parts...)
		if got != tc.want {
			t.Errorf("joinSandboxName(%q) = %q, want %q", tc.parts, got, tc.want)
		}
		if !validSandboxName(got) {
			t.Errorf("joinSandboxName(%q) = %q is not a valid name", tc.parts, got)
		}
	}
	// Sandboxes named by older sbxup releases fold to the same key, so a later resolveSandboxName finds them.
	for _, old := range []string{"my_app.claude", "my-app_claude"} {
		if foldName(joinSandboxName("my_app", "claude")) != foldName(old) {
			t.Errorf("a later resolveSandboxName would not find %s", old)
		}
	}
}

func TestSplitRunArgsAndCreateArgs(t *testing.T) {
	create, run := splitRunArgs([]string{"--cpus", "4", "-d", "--detach-keys", "ctrl-q", "--rm", "--env", "A=1", "--", "--resume"})
	if strings.Join(create, " ") != "--cpus 4 --env A=1" || strings.Join(run, " ") != "-d --detach-keys ctrl-q --rm -- --resume" {
		t.Fatalf("create %v run %v", create, run)
	}
	args := buildCreateArgs("sbx-claude-dotnet10:0.2.8", "claude", "claude-x", true, []string{"/c/.sbx-cache"}, create)
	want := "create --name claude-x --template sbx-claude-dotnet10:0.2.8 claude --clone . /c/.sbx-cache --cpus 4 --env A=1"
	if strings.Join(args, " ") != want {
		t.Fatalf("create args\n got %s\nwant %s", strings.Join(args, " "), want)
	}
}

func TestSandboxName(t *testing.T) {
	p := filepath.FromSlash
	for _, tc := range []struct{ cwd, agent, override, want string }{
		{"/w/deneblab/sbx-templates", "claude", "", "deneblab--sbx-templates--claude"},
		{"/home/u/my_org/My App", "claude", "", "my-org--My-App--claude"},
		{"/w/antipiracy/simplescraper.v2", "claude", "", "antipiracy--simplescraper-v2--claude"},
		{"/w/deneblab/antipiracy_simplescraper", "claude", "", "deneblab--antipiracy-simplescraper--claude"},
		{"/proj", "claude", "", "proj--claude"},
		{"/w/deneblab/sbx-templates", "claude", "mine-x", "mine-x"},
	} {
		if got := sandboxName(p(tc.cwd), tc.agent, tc.override); got != tc.want {
			t.Errorf("sandboxName(%q, %q, %q) = %q, want %q", tc.cwd, tc.agent, tc.override, got, tc.want)
		}
	}
	// The old agent-folder name folds differently, so an old sandbox is never resumed by mistake.
	if foldName(sandboxName(p("/w/deneblab/sbx-templates"), "claude", "")) == foldName("claude-sbx-templates") {
		t.Error("the new name matches an old sandbox")
	}
	// The dotted name of an earlier release folds the same, so that sandbox is resumed, not orphaned.
	if foldName(sandboxName(p("/w/deneblab/sbx-templates"), "claude", "")) != foldName("deneblab.sbx-templates.claude") {
		t.Error("a sandbox with the dotted name is not found")
	}
	// Every default name passes the config's own check and holds no '_', which `sbx create --name` refuses.
	for _, cwd := range []string{"/w/antipiracy/antipiracy_simplescraper", "/home/u/my_org/My App", "/proj"} {
		if name := sandboxName(p(cwd), "claude", ""); !validSandboxName(name) || strings.ContainsAny(name, "_.") {
			t.Errorf("sandboxName(%q) = %q cannot be given to sbx", cwd, name)
		}
	}
}

func TestValidSandboxName(t *testing.T) {
	for name, want := range map[string]bool{
		"deneblab--sbx-templates--claude": true,
		"ab":                              true,
		"my-app":                          true,
		"my_app":                          false,
		"deneblab_sbx-templates_claude":   false,
		"a":                               false,
		"default":                         false,
		"x/y":                             false,
		"deneblab.sbx-templates.claude":   false,
		"-x":                              false,
		"x-":                              false,
		"_x":                              false,
	} {
		if got := validSandboxName(name); got != want {
			t.Errorf("validSandboxName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLoadConfigSandbox(t *testing.T) {
	chdir(t)
	write(t, "c.yaml", "template: dotnet10\nsandbox: app-claude\n")
	if cfg, err := loadConfig("c.yaml"); err != nil || cfg.Sandbox != "app-claude" {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
	write(t, "c.yaml", "template: dotnet10\nsandbox: app_claude\n")
	if _, err := loadConfig("c.yaml"); err == nil || !strings.Contains(err.Error(), "sandbox 'app_claude'") {
		t.Fatalf("err = %v", err)
	}
	write(t, "c.yaml", "template: dotnet10\nsandbox: deneblab/app\n")
	if _, err := loadConfig("c.yaml"); err == nil || !strings.Contains(err.Error(), "sandbox 'deneblab/app'") {
		t.Fatalf("err = %v", err)
	}
}
