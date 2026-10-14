package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const testClientID = "Iv23liTest"

// configHome points os.UserConfigDir at a temp dir on every platform.
func configHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // linux
	t.Setenv("AppData", dir)         // windows
	t.Setenv("HOME", dir)            // darwin: $HOME/Library/Application Support
	return dir
}

// fakeLogin serves the OAuth endpoints of github.com and GET /user of the API.
type fakeLogin struct {
	mu         sync.Mutex
	disabled   bool     // answer the device code request with device_flow_disabled
	unknown    bool     // answer it as github.com does for an unknown client ID
	device     []string // scripted answers to device-code polls: an error code, or "" for tokens
	polls      int
	refreshes  int
	refreshing time.Duration // how long a refresh takes, to make callers overlap
	issued     int
	validRT    string // the refresh token GitHub currently accepts
}

func (f *fakeLogin) server(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/user" {
			w.Write([]byte(`{"login":"octo"}`))
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected %s %s (Accept %q)", r.Method, r.URL.Path, r.Header.Get("Accept"))
			w.WriteHeader(http.StatusTeapot)
			return
		}
		r.ParseForm()
		if r.Form.Get("client_secret") != "" {
			t.Error("sbxup sent a client secret")
		}
		if r.Form.Get("client_id") != testClientID {
			t.Errorf("client_id = %q", r.Form.Get("client_id"))
		}
		reply := func(v any) { json.NewEncoder(w).Encode(v) }
		oauthErr := func(code string) { reply(map[string]string{"error": code, "error_description": "about " + code}) }

		switch {
		case r.URL.Path == "/login/device/code":
			if f.disabled {
				oauthErr("device_flow_disabled")
				return
			}
			if f.unknown {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":"Not Found"}`))
				return
			}
			reply(map[string]any{"device_code": "dev-1", "user_code": "WDJB-MJHT",
				"verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 5})
		case r.URL.Path == "/login/oauth/access_token" && r.Form.Get("grant_type") == "urn:ietf:params:oauth:grant-type:device_code":
			f.mu.Lock()
			step := ""
			if f.polls < len(f.device) {
				step = f.device[f.polls]
			}
			f.polls++
			f.mu.Unlock()
			if step != "" {
				oauthErr(step)
				return
			}
			reply(f.issue())
		case r.URL.Path == "/login/oauth/access_token" && r.Form.Get("grant_type") == "refresh_token":
			time.Sleep(f.refreshing)
			f.mu.Lock()
			f.refreshes++
			ok := r.Form.Get("refresh_token") == f.validRT
			f.mu.Unlock()
			if !ok {
				oauthErr("bad_refresh_token")
				return
			}
			reply(f.issue())
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	origLogin, origAPI := githubLoginBase, githubAPIBase
	t.Cleanup(func() { githubLoginBase, githubAPIBase = origLogin, origAPI })
	githubLoginBase, githubAPIBase = srv.URL, srv.URL
}

// issue hands out the next pair; only the newest refresh token stays valid, as on GitHub.
func (f *fakeLogin) issue() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issued++
	n := string(rune('0' + f.issued))
	f.validRT = "ghr_" + n
	return map[string]any{"access_token": "ghu_" + n, "expires_in": 28800,
		"refresh_token": "ghr_" + n, "refresh_token_expires_in": 15897600, "token_type": "bearer"}
}

// noSleep makes polling instant and records the waits asked for.
func noSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	waits := &[]time.Duration{}
	orig := sleepCtx
	t.Cleanup(func() { sleepCtx = orig })
	sleepCtx = func(ctx context.Context, d time.Duration) bool {
		*waits = append(*waits, d)
		return ctx.Err() == nil
	}
	return waits
}

// saveLogin stores a login for testClientID whose access token expires after left.
func saveLogin(t *testing.T, access, refresh string, left time.Duration) string {
	t.Helper()
	path, err := userAuthPath(testClientID)
	if err != nil {
		t.Fatal(err)
	}
	err = saveUserTokens(path, &userTokens{ClientID: testClientID, Login: "octo", AccessToken: access,
		AccessExpiresAt: now().Add(left), RefreshToken: refresh, RefreshExpiresAt: now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDeviceLoginPollsAndSlowsDown(t *testing.T) {
	f := &fakeLogin{device: []string{"authorization_pending", "slow_down"}}
	f.server(t)
	waits := noSleep(t)
	var out strings.Builder
	tok, err := deviceLogin(context.Background(), &http.Client{}, testClientID, &out)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "ghu_1" || tok.RefreshToken != "ghr_1" ||
		tok.AccessExpiresAt.Sub(now()) < 7*time.Hour || tok.RefreshExpiresAt.Sub(now()) < 150*24*time.Hour {
		t.Fatalf("tokens %+v", tok)
	}
	if want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second}; !equalDurations(*waits, want) {
		t.Fatalf("waits %v, want %v", *waits, want)
	}
	if !strings.Contains(out.String(), "WDJB-MJHT") || !strings.Contains(out.String(), "https://github.com/login/device") {
		t.Fatalf("output: %s", out.String())
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDeviceLoginErrors(t *testing.T) {
	noSleep(t)
	for code, want := range map[string]string{
		"access_denied":  "declined",
		"expired_token":  "code expired",
		"incorrect_code": "about incorrect_code",
	} {
		f := &fakeLogin{device: []string{code}}
		f.server(t)
		if _, err := deviceLogin(context.Background(), &http.Client{}, testClientID, &strings.Builder{}); err == nil ||
			!strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", code, want, err)
		}
	}
	f := &fakeLogin{disabled: true}
	f.server(t)
	if _, err := deviceLogin(context.Background(), &http.Client{}, testClientID, &strings.Builder{}); err == nil ||
		!strings.Contains(err.Error(), "Enable Device Flow") {
		t.Errorf("disabled: %v", err)
	}
	f = &fakeLogin{unknown: true}
	f.server(t)
	if _, err := deviceLogin(context.Background(), &http.Client{}, testClientID, &strings.Builder{}); err == nil ||
		!strings.Contains(err.Error(), "no app with client ID") {
		t.Errorf("unknown app: %v", err)
	}
}

func TestUserTokensStoreIsPrivateAndRoundTrips(t *testing.T) {
	dir := configHome(t)
	path := saveLogin(t, "ghu_a", "ghr_a", time.Hour)
	if !strings.HasPrefix(path, dir) || filepath.Base(filepath.Dir(path)) != "github-auth" {
		t.Fatalf("path %s is not in the user config dir %s", path, dir)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v, %v", fi.Mode().Perm(), err)
		}
	}
	got, err := loadUserTokens(path)
	if err != nil || got.AccessToken != "ghu_a" || got.RefreshToken != "ghr_a" || got.Login != "octo" {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	if _, err := userAuthPath("../evil"); err == nil {
		t.Fatal("a client ID with a path in it was accepted")
	}
}

func TestCurrentUserTokenReusesAFreshToken(t *testing.T) {
	configHome(t)
	f := &fakeLogin{}
	f.server(t)
	saveLogin(t, "ghu_a", "ghr_a", 2*time.Hour)
	tok, err := currentUserToken(&http.Client{}, testClientID)
	if err != nil || tok.AccessToken != "ghu_a" || f.refreshes != 0 {
		t.Fatalf("tok=%+v err=%v refreshes=%d", tok, err, f.refreshes)
	}
}

func TestCurrentUserTokenRefreshesWhenDue(t *testing.T) {
	configHome(t)
	f := &fakeLogin{validRT: "ghr_a"}
	f.server(t)
	path := saveLogin(t, "ghu_a", "ghr_a", 5*time.Minute)
	tok, err := currentUserToken(&http.Client{}, testClientID)
	if err != nil || tok.AccessToken != "ghu_1" || f.refreshes != 1 {
		t.Fatalf("tok=%+v err=%v refreshes=%d", tok, err, f.refreshes)
	}
	saved, _ := loadUserTokens(path)
	if saved.AccessToken != "ghu_1" || saved.RefreshToken != "ghr_1" || saved.Login != "octo" {
		t.Fatalf("saved %+v", saved)
	}
}

func TestCurrentUserTokenNeedsLogin(t *testing.T) {
	configHome(t)
	f := &fakeLogin{validRT: "ghr_other"}
	f.server(t)
	if _, err := currentUserToken(&http.Client{}, testClientID); !errors.Is(err, errNeedLogin) {
		t.Fatalf("not logged in: %v", err)
	}
	saveLogin(t, "ghu_a", "ghr_a", time.Minute)
	if _, err := currentUserToken(&http.Client{}, testClientID); !errors.Is(err, errNeedLogin) ||
		!strings.Contains(err.Error(), "revoked") {
		t.Fatalf("rejected refresh token: %v", err)
	}

	path, _ := userAuthPath(testClientID)
	saveUserTokens(path, &userTokens{ClientID: testClientID, AccessToken: "ghu_a", AccessExpiresAt: now(),
		RefreshToken: "ghr_a", RefreshExpiresAt: now().Add(-time.Hour)})
	before := f.refreshes
	if _, err := currentUserToken(&http.Client{}, testClientID); !errors.Is(err, errNeedLogin) || f.refreshes != before {
		t.Fatalf("expired login: %v, refreshes %d -> %d", err, before, f.refreshes)
	}
}

// TestCurrentUserTokenRefreshesOnceForManyCallers: a refresh ends the old tokens, so callers that
// overlap must wait for the first and take what it saved.
func TestCurrentUserTokenRefreshesOnceForManyCallers(t *testing.T) {
	configHome(t)
	f := &fakeLogin{validRT: "ghr_a", refreshing: 50 * time.Millisecond}
	f.server(t)
	saveLogin(t, "ghu_a", "ghr_a", time.Minute)

	var wg sync.WaitGroup
	got := make([]string, 5)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := currentUserToken(&http.Client{}, testClientID)
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
				return
			}
			got[i] = tok.AccessToken
		}(i)
	}
	wg.Wait()
	if f.refreshes != 1 || strings.Join(got, ",") != "ghu_1,ghu_1,ghu_1,ghu_1,ghu_1" {
		t.Fatalf("refreshes=%d tokens=%v", f.refreshes, got)
	}
}

func TestGHLoginAndLogout(t *testing.T) {
	configHome(t)
	f := &fakeLogin{device: []string{"authorization_pending"}}
	f.server(t)
	noSleep(t)
	g := &GitHubConfig{ClientID: testClientID}

	out := capture(t, func() {
		if err := ghLoginCmd(g, false); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "WDJB-MJHT") || !strings.Contains(out, "logged in as octo") {
		t.Fatalf("login output: %s", out)
	}
	path, _ := userAuthPath(testClientID)
	if saved, err := loadUserTokens(path); err != nil || saved.AccessToken != "ghu_1" || saved.Login != "octo" {
		t.Fatalf("saved %+v, %v", saved, err)
	}

	out = capture(t, func() { ghLogoutCmd(g, false) })
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) || !strings.Contains(out, "logged out") {
		t.Fatalf("logout left %s: %s", path, out)
	}
	out = capture(t, func() { ghLogoutCmd(g, false) })
	if !strings.Contains(out, "not logged in") {
		t.Fatalf("second logout: %s", out)
	}

	if err := ghLoginCmd(&GitHubConfig{BitwardenItem: "x"}, false); err == nil || !strings.Contains(err.Error(), "client-id") {
		t.Fatalf("login without client-id: %v", err)
	}
	out = capture(t, func() { ghLoginCmd(g, true) })
	if !strings.Contains(out, "[dry-run]") || f.polls != 2 {
		t.Fatalf("dry-run: polls=%d %s", f.polls, out)
	}
}

func TestPrepareUserToken(t *testing.T) {
	cacheHome(t)
	configHome(t)
	f := &fakeLogin{}
	f.server(t)
	asked := 0
	origAsk, origInt := askGitHub, isInteractive
	t.Cleanup(func() { askGitHub, isInteractive = origAsk, origInt })
	askGitHub = func(string) (bool, bool) { asked++; return true, true }
	isInteractive = func() bool { return true }
	project := t.TempDir()
	g := &GitHubConfig{ClientID: testClientID}

	var s *ghSession
	out := capture(t, func() { s = prepareGitHubToken(g, project, false, false) })
	if s != nil || !strings.Contains(out, "--gh-login") {
		t.Fatalf("not logged in: s=%+v %s", s, out)
	}

	saveLogin(t, "ghu_a", "ghr_a", 2*time.Hour)
	capture(t, func() { s = prepareGitHubToken(g, project, false, false) })
	if s == nil || s.token != "ghu_a" || s.clientID != testClientID || asked != 1 {
		t.Fatalf("s=%+v asked=%d", s, asked)
	}
	capture(t, func() { prepareGitHubToken(g, project, false, false) })
	if asked != 1 {
		t.Fatalf("approved app asked again: %d", asked)
	}
	// Another app is a new question.
	capture(t, func() { prepareGitHubToken(&GitHubConfig{ClientID: "Iv23liOther"}, project, false, false) })
	if asked != 2 {
		t.Fatalf("another app did not ask: %d", asked)
	}

	out = capture(t, func() {
		if prepareGitHubToken(g, project, false, true) != nil {
			t.Error("token under --dry-run")
		}
	})
	if !strings.Contains(out, "[dry-run]") || f.refreshes != 0 {
		t.Fatalf("dry-run: refreshes=%d %s", f.refreshes, out)
	}
}

// TestKeepFreshUserModeFollowsTheFile: a token another sbxup refreshed reaches this sandbox, an
// unchanged one is not set again, and nothing is revoked at the end.
func TestKeepFreshUserModeFollowsTheFile(t *testing.T) {
	configHome(t)
	f := &fakeLogin{}
	f.server(t)
	set := stubSecrets(t)
	path := saveLogin(t, "ghu_a", "ghr_a", 2*time.Hour)
	s := &ghSession{client: &http.Client{}, clientID: testClientID, token: "ghu_a", expires: now().Add(2 * time.Hour)}

	ctx, cancel := context.WithCancel(context.Background())
	ticks := 0
	orig := sleepCtx
	t.Cleanup(func() { sleepCtx = orig })
	sleepCtx = func(ctx context.Context, d time.Duration) bool {
		if d != userPollEvery {
			t.Errorf("wait %v, want %v", d, userPollEvery)
		}
		ticks++
		switch ticks {
		case 2: // another sbxup refreshed in the meantime
			saveUserTokens(path, &userTokens{ClientID: testClientID, AccessToken: "ghu_b",
				AccessExpiresAt: now().Add(8 * time.Hour), RefreshToken: "ghr_b"})
		case 4:
			cancel()
			return false
		}
		return true
	}
	s.keepFresh(ctx, "claude-sbx", "claude-sbx")
	s.revoke() // the fake server fails the test on any unexpected request

	if strings.Join(*set, " ") != "claude-sbx=ghu_b" {
		t.Fatalf("secrets set: %v", *set)
	}
	if f.refreshes != 0 || s.token != "ghu_b" {
		t.Fatalf("refreshes=%d token=%s", f.refreshes, s.token)
	}
}
