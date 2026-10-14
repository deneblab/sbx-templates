package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseGitHubRemote(t *testing.T) {
	ok := []string{
		"https://github.com/deneblab/sbx-templates.git",
		"https://github.com/deneblab/sbx-templates",
		"https://github.com/deneblab/sbx-templates/",
		"https://user@github.com/deneblab/sbx-templates.git",
		"git@github.com:deneblab/sbx-templates.git",
		"ssh://git@github.com/deneblab/sbx-templates.git",
	}
	for _, r := range ok {
		o, n, err := parseGitHubRemote(r)
		if err != nil || o != "deneblab" || n != "sbx-templates" {
			t.Errorf("%s: %s/%s %v", r, o, n, err)
		}
	}
	bad := []string{"https://gitlab.com/a/b.git", "git@bitbucket.org:a/b.git", "/srv/repo.git", "https://github.com/onlyowner"}
	for _, r := range bad {
		if _, _, err := parseGitHubRemote(r); err == nil {
			t.Errorf("%s accepted", r)
		}
	}
}

func TestTokenRepos(t *testing.T) {
	got, err := tokenRepos("deneblab", "sbx-templates", []string{"docs", "Deneblab/abcversion", "sbx-templates", "DOCS"})
	if err != nil || strings.Join(got, ",") != "sbx-templates,docs,abcversion" {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := tokenRepos("deneblab", "sbx-templates", []string{"other/thing"}); err == nil || !strings.Contains(err.Error(), "other/thing") {
		t.Fatalf("other owner accepted: %v", err)
	}
	if _, err := tokenRepos("deneblab", "sbx-templates", []string{"deneblab/a/b"}); err == nil {
		t.Fatal("nested path accepted")
	}
}

// stubGitHubFlow wires every seam prepareGitHubToken touches: origin, the prompt, Bitwarden (with
// a real key) and a fake GitHub. It returns the fake GitHub and the number of approval questions.
func stubGitHubFlow(t *testing.T, answer bool) (*fakeGitHub, *fakeBw, *int) {
	t.Helper()
	cacheHome(t)
	f := &fakeGitHub{}
	f.server(t)
	bw := agentBw("unlocked")
	bw.answers["get attachment att-1 --itemid item-1 --raw"] = string(pkcs1PEM(testKey(t)))
	stubBw(t, bw, true, "")
	t.Setenv("BW_SESSION", "mine")

	asked := new(int)
	origOrigin, origAsk := gitOriginURL, askGitHub
	t.Cleanup(func() { gitOriginURL, askGitHub = origOrigin, origAsk })
	gitOriginURL = func(string) (string, error) { return "git@github.com:deneblab/sbx-templates.git", nil }
	askGitHub = func(string) (bool, bool) { *asked++; return answer, true }
	return f, bw, asked
}

func TestPrepareGitHubTokenAsksOnceAndMints(t *testing.T) {
	f, _, asked := stubGitHubFlow(t, true)
	project := t.TempDir()
	g := agentConfig()
	g.Permissions = defaultGitHubPermissions

	var s *ghSession
	capture(t, func() { s = prepareGitHubToken(g, project, false, false) })
	if s == nil || s.token != "ghs_1" || *asked != 1 || s.installation != 42 {
		t.Fatalf("s=%+v asked=%d", s, *asked)
	}
	capture(t, func() { s = prepareGitHubToken(g, project, false, false) })
	if s == nil || *asked != 1 {
		t.Fatalf("approved request asked again: asked=%d", *asked)
	}
	// A wider request is a new question.
	g.Repositories = []string{"docs"}
	capture(t, func() { prepareGitHubToken(g, project, false, false) })
	if *asked != 2 {
		t.Fatalf("a new repository did not ask again: asked=%d", *asked)
	}
	if f.minted != 3 {
		t.Fatalf("minted %d", f.minted)
	}
}

func TestPrepareGitHubTokenNoIsNotRemembered(t *testing.T) {
	f, bw, asked := stubGitHubFlow(t, false)
	project := t.TempDir()
	g := agentConfig()
	g.Permissions = defaultGitHubPermissions
	for i := 0; i < 2; i++ {
		var s *ghSession
		out := capture(t, func() { s = prepareGitHubToken(g, project, false, false) })
		if s != nil || !strings.Contains(out, "skipped") {
			t.Fatalf("no gave a token: %s", out)
		}
	}
	if *asked != 2 || f.minted != 0 || len(bw.calls) != 0 {
		t.Fatalf("asked=%d minted=%d bw=%v", *asked, f.minted, bw.calls)
	}
}

func TestPrepareGitHubTokenOffDryRunAndFailures(t *testing.T) {
	f, bw, asked := stubGitHubFlow(t, true)
	project := t.TempDir()
	g := agentConfig()
	g.Permissions = defaultGitHubPermissions

	if prepareGitHubToken(nil, project, false, false) != nil || prepareGitHubToken(g, project, true, false) != nil {
		t.Fatal("token without config or with --no-gh-token")
	}
	out := capture(t, func() {
		if prepareGitHubToken(g, project, false, true) != nil {
			t.Error("token under --dry-run")
		}
	})
	if !strings.Contains(out, "deneblab/sbx-templates") || !strings.Contains(out, "[dry-run]") {
		t.Fatalf("dry-run output: %s", out)
	}
	if *asked != 0 || len(bw.calls) != 0 || f.minted != 0 {
		t.Fatalf("dry-run touched something: asked=%d bw=%v minted=%d", *asked, bw.calls, f.minted)
	}

	gitOriginURL = func(string) (string, error) { return "", errors.New("no origin") }
	out = capture(t, func() {
		if prepareGitHubToken(g, project, false, false) != nil {
			t.Error("token without origin")
		}
	})
	if !strings.Contains(out, "WARNING") {
		t.Fatalf("no warning: %s", out)
	}
}

func TestPrepareGitHubTokenNoTerminalSkips(t *testing.T) {
	_, _, asked := stubGitHubFlow(t, true)
	isInteractive = func() bool { return false }
	g := agentConfig()
	g.Permissions = defaultGitHubPermissions
	out := capture(t, func() {
		if prepareGitHubToken(g, t.TempDir(), false, false) != nil {
			t.Error("unapproved token without a terminal")
		}
	})
	if *asked != 0 || !strings.Contains(out, "no terminal") {
		t.Fatalf("asked=%d out=%s", *asked, out)
	}
}

func TestGHApprovalIsInTheUserCache(t *testing.T) {
	cache := cacheHome(t)
	key := ghApprovalKey("/p", "item", "o", []string{"r"}, map[string]string{"contents": "write"})
	if err := recordGHApproval(key, "/p", "item", "o", []string{"r"}, map[string]string{"contents": "write"}); err != nil {
		t.Fatal(err)
	}
	if !ghApproved(key) {
		t.Fatal("not approved after recording")
	}
	p, _ := ghApprovalPath(key)
	if !strings.HasPrefix(p, cache) {
		t.Fatalf("approval outside the cache: %s", p)
	}
	if ghApprovalKey("/p", "item", "o", []string{"r"}, map[string]string{"contents": "read"}) == key {
		t.Fatal("permissions not part of the key")
	}
}

func TestLoadConfigGitHubBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	write(t, path, `template: dotnet10
github:
  bitwarden-item: github-deneblab-agent
  repositories: [docs, deneblab/abcversion]
  permissions:
    contents: write
    pull_requests: READ
`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	g := cfg.GitHub
	if g == nil || g.BitwardenItem != "github-deneblab-agent" || g.ClientIDField != "client-id" ||
		strings.Join(g.Repositories, ",") != "docs,deneblab/abcversion" || g.Permissions["pull_requests"] != "read" {
		t.Fatalf("parsed %+v", g)
	}

	write(t, path, "template: dotnet10\ngithub:\n  client-id: Iv23liAbc123\n")
	if cfg, err := loadConfig(path); err != nil || cfg.GitHub.ClientID != "Iv23liAbc123" ||
		cfg.GitHub.BitwardenItem != "" || cfg.GitHub.ClientIDField != "" || cfg.GitHub.Permissions != nil {
		t.Fatalf("client-id mode: %+v, %v", cfg.GitHub, err)
	}

	write(t, path, "template: dotnet10\ngithub:\n  bitwarden-item: x\n")
	if cfg, _ := loadConfig(path); formatPermissions(cfg.GitHub.Permissions) != "contents:write, issues:write, pull_requests:write" {
		t.Fatalf("default permissions: %v", cfg.GitHub.Permissions)
	}

	bad := map[string]string{
		"github:\n  repositories: [docs]\n":                                "needs 'client-id'",
		"github:\n  bitwarden-item: x\n  permisions: {contents: write}\n":  "unknown key 'github.permisions'",
		"github:\n  bitwarden-item: x\n  permissions: {contents: admin}\n": "use read or write",
		"github: yes\n": "must be a block",
		"github:\n  client-id: Iv23li\n  bitwarden-item: x\n":            "both 'client-id' and 'bitwarden-item'",
		"github:\n  client-id: Iv23li\n  repositories: [docs]\n":         "'github.repositories' in",
		"github:\n  client-id: Iv23li\n  permissions: {issues: write}\n": "cannot be narrowed",
		"github:\n  client-id: ../x\n":                                   "not a GitHub App client ID",
	}
	for body, want := range bad {
		write(t, path, "template: dotnet10\n"+body)
		if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", body, want, err)
		}
	}
}

// stubSecrets records `sbx secret set` calls.
func stubSecrets(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	set := &[]string{}
	orig := sbxSecretSet
	t.Cleanup(func() { sbxSecretSet = orig })
	sbxSecretSet = func(sandbox, token string) error {
		mu.Lock()
		defer mu.Unlock()
		*set = append(*set, sandbox+"="+token)
		return nil
	}
	return set
}

func TestKeepFreshRefreshesAndRevokesTheOldToken(t *testing.T) {
	f := &fakeGitHub{}
	f.server(t)
	set := stubSecrets(t)
	s := &ghSession{client: f.httpClient(), key: testKey(t), issuer: "c", owner: "deneblab",
		installation: 42, repos: []string{"sbx-templates"}, perms: map[string]string{"contents": "write"},
		token: "ghs_0", expires: now().Add(time.Hour)}

	ctx, cancel := context.WithCancel(context.Background())
	sleeps := 0
	orig := sleepCtx
	t.Cleanup(func() { sleepCtx = orig })
	sleepCtx = func(ctx context.Context, d time.Duration) bool {
		sleeps++
		if sleeps == 1 && d != time.Hour-refreshBefore && d < time.Hour-refreshBefore-time.Second {
			t.Errorf("first wait %v, want about %v", d, time.Hour-refreshBefore)
		}
		if sleeps > 2 {
			cancel()
			return false
		}
		return true
	}
	s.keepFresh(ctx, "claude-sbx", "claude-sbx")

	if strings.Join(*set, " ") != "claude-sbx=ghs_1 claude-sbx=ghs_2" {
		t.Fatalf("secrets set: %v", *set)
	}
	if strings.Join(f.revoked, " ") != "ghs_0 ghs_1" {
		t.Fatalf("old tokens revoked: %v", f.revoked)
	}
}

func TestKeepFreshWaitsForANewSandbox(t *testing.T) {
	set := stubSecrets(t)
	polls := 0
	origList, origSleep := sbxList, sleepCtx
	t.Cleanup(func() { sbxList, sleepCtx = origList, origSleep })
	sbxList = func() []string {
		polls++
		if polls < 3 {
			return nil
		}
		return []string{"NAME  STATUS", "claude-SBX  running"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	sleepCtx = func(ctx context.Context, d time.Duration) bool {
		if d == pollEvery {
			return true
		}
		cancel()
		return false
	}
	s := &ghSession{token: "ghs_0", expires: now().Add(time.Hour)}
	s.keepFresh(ctx, "", "claude-sbx")
	if strings.Join(*set, " ") != "claude-SBX=ghs_0" {
		t.Fatalf("secrets set: %v", *set)
	}
}

func TestGHTokenCmdRefusals(t *testing.T) {
	if err := ghTokenCmd(nil, ".", "claude-x", false); err == nil || !strings.Contains(err.Error(), "github:") {
		t.Fatalf("no config: %v", err)
	}
	orig := sbxList
	t.Cleanup(func() { sbxList = orig })
	sbxList = func() []string { return nil }
	if err := ghTokenCmd(agentConfig(), ".", "claude-x", false); err == nil || !strings.Contains(err.Error(), "no sandbox") {
		t.Fatalf("no sandbox: %v", err)
	}
}

func TestSbxSecretSetOverwrites(t *testing.T) {
	// Without -f sbx refuses to replace the secret, so every refresh and every resume would fail.
	got := strings.Join(sbxSecretSetArgs("claude-x", "ghs_1"), " ")
	if got != "sbx secret set github --sandbox claude-x -f -t ghs_1" {
		t.Fatalf("got %q", got)
	}
}

func TestGHTokenCmdRevokesWhenTheSecretIsNotSet(t *testing.T) {
	f, _, _ := stubGitHubFlow(t, true)
	origList, origSet := sbxList, sbxSecretSet
	t.Cleanup(func() { sbxList, sbxSecretSet = origList, origSet })
	sbxList = func() []string { return []string{"claude-ghtest running"} }
	sbxSecretSet = func(string, string) error { return errors.New("secret already exists") }

	var err error
	capture(t, func() { err = ghTokenCmd(agentConfig(), t.TempDir(), "claude-ghtest", false) })
	if err == nil || f.minted != 1 || strings.Join(f.revoked, " ") != "ghs_1" {
		t.Fatalf("err=%v minted=%d revoked=%v", err, f.minted, f.revoked)
	}
}
