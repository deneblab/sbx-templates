package main

// GitHub App user access tokens through the device flow (`github: client-id:`). `sbxup --gh-login`
// shows a code to enter on github.com once per machine; from then on a start costs one refresh call,
// with no Bitwarden, no password and no private key on the host. The token acts as the user, limited
// to what the app may do, in every organization where the app is installed.
//
// Things that shape this file:
//   - No client secret. GitHub requires none for the device flow, nor to refresh a token it issued.
//   - A refresh ends the old access token as well as the refresh token. Every sandbox of one app
//     therefore shares one token: the file holds the current access token, a start reuses it while it
//     has time left, and only one process at a time may refresh (lockFile).
//   - The file lives in the user config directory, mode 0600, never in a repository.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// githubLoginBase hosts the OAuth endpoints (github.com, not api.github.com); a variable for tests.
var githubLoginBase = "https://github.com"

// errNeedLogin marks a missing or dead login, which only `sbxup --gh-login` can fix.
var errNeedLogin = errors.New("run 'sbxup --gh-login'")

// clientIDPattern is what a GitHub App client ID looks like (Iv23li..., or Iv1.abc for older apps).
// It becomes a file name, so nothing else is accepted.
var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// userTokens is the stored login. A zero AccessExpiresAt means the app issues tokens that do not
// expire, which then come without a refresh token.
type userTokens struct {
	ClientID         string    `json:"clientId"`
	Login            string    `json:"login,omitempty"`
	AccessToken      string    `json:"accessToken"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// due reports whether the access token should be replaced now.
func (t *userTokens) due() bool {
	return !t.AccessExpiresAt.IsZero() && t.AccessExpiresAt.Sub(now()) <= refreshBefore
}

// oauthError is an OAuth reply with an `error` field, which GitHub sends with status 200.
type oauthError struct {
	Code        string
	Description string
}

func (e *oauthError) Error() string {
	if e.Description == "" {
		return "GitHub: " + e.Code
	}
	return fmt.Sprintf("GitHub: %s (%s)", e.Description, e.Code)
}

func oauthCode(err error) string {
	var oe *oauthError
	if errors.As(err, &oe) {
		return oe.Code
	}
	return ""
}

// oauthPost sends a form to a github.com OAuth endpoint and decodes the JSON reply into out.
func oauthPost(client *http.Client, path string, form url.Values, out any) error {
	req, err := http.NewRequest(http.MethodPost, githubLoginBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sbxup/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(data, &e)
	if e.Error != "" {
		return &oauthError{Code: e.Error, Description: e.Description}
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s%s returned %d", githubLoginBase, path, resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

// tokenReply is what both the device flow and a refresh return.
type tokenReply struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
}

func (r *tokenReply) tokens(clientID string) (*userTokens, error) {
	if r.AccessToken == "" {
		return nil, errors.New("GitHub returned no token")
	}
	t := &userTokens{ClientID: clientID, AccessToken: r.AccessToken, RefreshToken: r.RefreshToken, UpdatedAt: now()}
	if r.ExpiresIn > 0 {
		t.AccessExpiresAt = now().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	if r.RefreshTokenExpiresIn > 0 {
		t.RefreshExpiresAt = now().Add(time.Duration(r.RefreshTokenExpiresIn) * time.Second)
	}
	return t, nil
}

// deviceLogin runs the device flow: it prints the code and where to enter it, then polls until the
// user approves, declines or lets the code expire.
func deviceLogin(ctx context.Context, client *http.Client, clientID string, out io.Writer) (*userTokens, error) {
	var code struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int64  `json:"expires_in"`
		Interval        int64  `json:"interval"`
	}
	err := oauthPost(client, "/login/device/code", url.Values{"client_id": {clientID}}, &code)
	switch oauthCode(err) {
	case "device_flow_disabled":
		return nil, fmt.Errorf("the device flow is not enabled for app %s; turn on 'Enable Device Flow' in the app's settings", clientID)
	case "Not Found": // what github.com answers (with 404) for a client ID it does not know
		return nil, fmt.Errorf("GitHub knows no app with client ID %s; check 'github.client-id' (the Client ID on the app's settings page, not the App ID)", clientID)
	}
	if err != nil {
		return nil, err
	}
	if code.DeviceCode == "" || code.UserCode == "" {
		return nil, errors.New("GitHub returned no device code")
	}
	fmt.Fprintf(out, "Open %s and enter the code: %s\n", code.VerificationURI, code.UserCode)
	fmt.Fprintln(out, "Waiting for approval...")

	interval := time.Duration(max(code.Interval, 5)) * time.Second
	deadline := now().Add(time.Duration(max(code.ExpiresIn, 60)) * time.Second)
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {code.DeviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	for {
		if !sleepCtx(ctx, interval) {
			return nil, ctx.Err()
		}
		var r tokenReply
		err := oauthPost(client, "/login/oauth/access_token", form, &r)
		switch oauthCode(err) {
		case "authorization_pending":
			if now().After(deadline) {
				return nil, errors.New("the code expired before it was entered; run 'sbxup --gh-login' again")
			}
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "expired_token":
			return nil, errors.New("the code expired before it was entered; run 'sbxup --gh-login' again")
		case "access_denied":
			return nil, errors.New("the login was declined on GitHub")
		}
		if err != nil {
			return nil, err
		}
		return r.tokens(clientID)
	}
}

// refreshUserToken swaps a refresh token for a new pair. The old refresh token and the old access
// token stop working, which is why callers hold the lock.
func refreshUserToken(client *http.Client, clientID, refresh string) (*userTokens, error) {
	var r tokenReply
	err := oauthPost(client, "/login/oauth/access_token", url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
	}, &r)
	if oauthCode(err) == "bad_refresh_token" {
		return nil, fmt.Errorf("GitHub no longer accepts the saved login (expired or revoked); %w", errNeedLogin)
	}
	if err != nil {
		return nil, err
	}
	return r.tokens(clientID)
}

// userAuthPath is where the login for one app is kept.
func userAuthPath(clientID string) (string, error) {
	if !clientIDPattern.MatchString(clientID) {
		return "", fmt.Errorf("%q is not a GitHub App client ID", clientID)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a config directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "github-auth", clientID+".json"), nil
}

func loadUserTokens(path string) (*userTokens, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("not logged in on this machine; %w", errNeedLogin)
	}
	if err != nil {
		return nil, err
	}
	var t userTokens
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return nil, fmt.Errorf("the saved login in %s is unreadable; %w", path, errNeedLogin)
	}
	return &t, nil
}

func saveUserTokens(path string, t *userTokens) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600, 0o700)
}

// withUserLock runs fn while holding the lock of the login file at path.
func withUserLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return fmt.Errorf("cannot lock %s: %w", path, err)
	}
	defer unlock()
	return fn()
}

// currentUserToken returns the shared token for the app, refreshing it first when it is about to
// expire. The file is read under the lock, so a process that waited sees what the holder wrote and
// does not spend the refresh token a second time.
func currentUserToken(client *http.Client, clientID string) (*userTokens, error) {
	path, err := userAuthPath(clientID)
	if err != nil {
		return nil, err
	}
	var t *userTokens
	err = withUserLock(path, func() error {
		cur, err := loadUserTokens(path)
		if err != nil {
			return err
		}
		if !cur.due() {
			t = cur
			return nil
		}
		if cur.RefreshToken == "" {
			return fmt.Errorf("the saved login has expired; %w", errNeedLogin)
		}
		if !cur.RefreshExpiresAt.IsZero() && now().After(cur.RefreshExpiresAt) {
			return fmt.Errorf("the saved login expired on %s; %w", cur.RefreshExpiresAt.Local().Format("2006-01-02"), errNeedLogin)
		}
		next, err := refreshUserToken(client, clientID, cur.RefreshToken)
		if err != nil {
			return err
		}
		next.Login = cur.Login
		if err := saveUserTokens(path, next); err != nil {
			// The old pair is already dead, so an unsaved new one means a new login next time.
			return fmt.Errorf("cannot save the refreshed login: %w", err)
		}
		t = next
		return nil
	})
	return t, err
}

// githubLogin names the account a token belongs to, or "" when GitHub does not say.
func githubLogin(client *http.Client, token string) string {
	var u struct {
		Login string `json:"login"`
	}
	if err := githubCall(client, http.MethodGet, "/user", token, nil, &u); err != nil {
		return ""
	}
	return u.Login
}

// ghLoginCmd is `sbxup --gh-login`: the device flow for the app in the config.
func ghLoginCmd(g *GitHubConfig, dryRun bool) error {
	if g == nil || g.ClientID == "" {
		return errors.New("--gh-login needs 'github:' with 'client-id' in the config")
	}
	path, err := userAuthPath(g.ClientID)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Printf("[dry-run] would log in to GitHub App %s with the device flow and save the login in %s\n", g.ClientID, path)
		return nil
	}
	client := &http.Client{Timeout: httpClient}
	t, err := deviceLogin(context.Background(), client, g.ClientID, os.Stdout)
	if err != nil {
		return err
	}
	t.Login = githubLogin(client, t.AccessToken)
	if err := withUserLock(path, func() error { return saveUserTokens(path, t) }); err != nil {
		return fmt.Errorf("cannot save the login: %w", err)
	}
	who := t.Login
	if who == "" {
		who = "your account"
	}
	until := "it does not expire"
	if !t.RefreshExpiresAt.IsZero() {
		until = "valid until " + t.RefreshExpiresAt.Local().Format("2006-01-02")
	}
	fmt.Printf("GitHub: logged in as %s for app %s (%s)\n", who, g.ClientID, until)
	return nil
}

// ghLogoutCmd is `sbxup --gh-logout`. It forgets the login on this machine; ending it on GitHub needs
// the app's secret, which sbxup does not have, so the user is pointed to the settings page.
func ghLogoutCmd(g *GitHubConfig, dryRun bool) error {
	if g == nil || g.ClientID == "" {
		return errors.New("--gh-logout needs 'github:' with 'client-id' in the config")
	}
	path, err := userAuthPath(g.ClientID)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Printf("[dry-run] would remove %s\n", path)
		return nil
	}
	err = withUserLock(path, func() error { return os.Remove(path) })
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Printf("GitHub: not logged in for app %s on this machine\n", g.ClientID)
	case err != nil:
		return err
	default:
		fmt.Printf("GitHub: logged out of app %s on this machine\n", g.ClientID)
	}
	fmt.Println("  To end the login on GitHub too (all machines): https://github.com/settings/apps/authorizations")
	return nil
}
