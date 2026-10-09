package main

import (
	"errors"
	"strings"
	"testing"
)

// fakeBw answers `bw` commands from a table keyed by the joined arguments, and records each call
// with the environment it was given.
type fakeBw struct {
	status  string            // what `bw status` reports
	answers map[string]string // "get item x" -> stdout
	calls   []string
	envs    [][]string
	failOn  string
}

func stubBw(t *testing.T, f *fakeBw, interactiveTerm bool, password string) (asked *int) {
	t.Helper()
	asked = new(int)
	origRun, origPw, origInt := bwRun, readPassword, isInteractive
	t.Cleanup(func() { bwRun, readPassword, isInteractive = origRun, origPw, origInt })
	t.Setenv("BW_SESSION", "")
	isInteractive = func() bool { return interactiveTerm }
	readPassword = func(string) (string, error) { *asked++; return password, nil }
	bwRun = func(env []string, args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		f.calls = append(f.calls, cmd)
		f.envs = append(f.envs, env)
		if f.failOn != "" && strings.HasPrefix(cmd, f.failOn) {
			return nil, errors.New("Invalid master password.")
		}
		switch {
		case cmd == "status":
			return []byte(`{"status":"` + f.status + `"}`), nil
		case strings.HasPrefix(cmd, "unlock"):
			f.status = "unlocked"
			return []byte("SESSIONKEY\n"), nil
		case cmd == "lock":
			return nil, nil
		}
		if out, ok := f.answers[cmd]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("Not found.")
	}
	return asked
}

const agentItem = `{"id":"item-1","name":"github-deneblab-agent",
 "fields":[{"name":"app-id","value":"123"},{"name":"client-id","value":"Iv23client"}],
 "attachments":[{"id":"att-1","fileName":"deneblab-agent.private-key.pem"}]}`

func agentBw(status string) *fakeBw {
	return &fakeBw{status: status, answers: map[string]string{
		"get item github-deneblab-agent":             agentItem,
		"get attachment att-1 --itemid item-1 --raw": "PEMDATA",
	}}
}

func agentConfig() *GitHubConfig {
	return &GitHubConfig{BitwardenItem: "github-deneblab-agent", ClientIDField: "client-id", AppIDField: "app-id"}
}

func TestOpenVaultUnlockedNeedsNoPassword(t *testing.T) {
	f := agentBw("unlocked")
	asked := stubBw(t, f, true, "secret")
	t.Setenv("BW_SESSION", "mine")
	v, err := openVault()
	if err != nil || *asked != 0 || v.unlocked {
		t.Fatalf("err=%v asked=%d unlocked=%v", err, *asked, v.unlocked)
	}
	v.close()
	for _, c := range f.calls {
		if c == "lock" {
			t.Fatal("locked a vault sbxup did not unlock")
		}
	}
	k, err := readAppKey(v, agentConfig())
	if err != nil || k.issuer != "Iv23client" || string(k.pem) != "PEMDATA" {
		t.Fatalf("readAppKey: %+v %v", k, err)
	}
	if got := f.envs[len(f.envs)-1]; len(got) != 1 || got[0] != "BW_SESSION=mine" {
		t.Fatalf("session not passed: %v", got)
	}
}

func TestOpenVaultEmptyPasswordSkips(t *testing.T) {
	f := agentBw("locked")
	asked := stubBw(t, f, true, "")
	if _, err := openVault(); !errors.Is(err, errNoToken) || *asked != 1 {
		t.Fatalf("err=%v asked=%d", err, *asked)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "unlock") {
			t.Fatal("unlock called after an empty password")
		}
	}
}

func TestOpenVaultUnlocksAndLocksAgain(t *testing.T) {
	f := agentBw("locked")
	stubBw(t, f, true, "hunter2")
	v, err := openVault()
	if err != nil || !v.unlocked || v.session != "SESSIONKEY" {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	// The password travels in the child's environment, never in its arguments.
	for i, c := range f.calls {
		if strings.Contains(c, "hunter2") {
			t.Fatalf("password in arguments: %q", c)
		}
		if strings.HasPrefix(c, "unlock") && (len(f.envs[i]) != 1 || f.envs[i][0] != "BW_SBXUP_PASSWORD=hunter2") {
			t.Fatalf("unlock env: %v", f.envs[i])
		}
	}
	v.close()
	if f.calls[len(f.calls)-1] != "lock" {
		t.Fatalf("vault not locked again: %v", f.calls)
	}
}

func TestOpenVaultRefusals(t *testing.T) {
	stubBw(t, agentBw("unauthenticated"), true, "x")
	if _, err := openVault(); err == nil || !strings.Contains(err.Error(), "bw login") {
		t.Fatalf("unauthenticated: %v", err)
	}
	stubBw(t, agentBw("locked"), false, "x")
	if _, err := openVault(); err == nil || !strings.Contains(err.Error(), "BW_SESSION") {
		t.Fatalf("no terminal: %v", err)
	}
	f := agentBw("locked")
	f.failOn = "unlock"
	stubBw(t, f, true, "wrong")
	if _, err := openVault(); err == nil || !strings.Contains(err.Error(), "Invalid master password") {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestReadAppKeyChoices(t *testing.T) {
	v := &bwVault{session: "s"}
	cases := []struct {
		name, item, want string
		cfg              func(*GitHubConfig)
	}{
		{"app-id fallback", `{"id":"item-1","fields":[{"name":"app-id","value":"123"}],"attachments":[{"id":"att-1","fileName":"k.pem"}]}`, "issuer 123", nil},
		{"no id field", `{"id":"item-1","fields":[],"attachments":[{"id":"att-1","fileName":"k.pem"}]}`, "neither", nil},
		{"no pem", `{"id":"item-1","fields":[{"name":"client-id","value":"c"}],"attachments":[{"id":"x","fileName":"notes.txt"}]}`, "no .pem attachment (it has: notes.txt)", nil},
		{"two pems", `{"id":"item-1","fields":[{"name":"client-id","value":"c"}],"attachments":[{"id":"att-1","fileName":"a.pem"},{"id":"b","fileName":"b.pem"}]}`, "several", nil},
		{"named attachment", `{"id":"item-1","fields":[{"name":"client-id","value":"c"}],"attachments":[{"id":"att-1","fileName":"a.pem"},{"id":"b","fileName":"b.pem"}]}`, "issuer c",
			func(g *GitHubConfig) { g.KeyAttachment = "a.pem" }},
		{"named attachment missing", `{"id":"item-1","fields":[{"name":"client-id","value":"c"}],"attachments":[{"id":"att-1","fileName":"a.pem"}]}`, `no attachment "z.pem"`,
			func(g *GitHubConfig) { g.KeyAttachment = "z.pem" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeBw{answers: map[string]string{
				"get item github-deneblab-agent":             c.item,
				"get attachment att-1 --itemid item-1 --raw": "PEM",
			}}
			stubBw(t, f, true, "")
			g := agentConfig()
			if c.cfg != nil {
				c.cfg(g)
			}
			k, err := readAppKey(v, g)
			if strings.HasPrefix(c.want, "issuer ") {
				if err != nil || k.issuer != strings.TrimPrefix(c.want, "issuer ") {
					t.Fatalf("k=%+v err=%v", k, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}
