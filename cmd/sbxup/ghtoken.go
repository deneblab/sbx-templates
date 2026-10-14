package main

// Short-lived GitHub tokens for the sandbox (`github:` in the config), in two modes:
//   - `client-id`: the user's own login from `sbxup --gh-login` (userauth.go). One token, shared by
//     every sandbox of that app and refreshed through a file; it is never revoked on exit, because
//     another sandbox may be using it.
//   - `bitwarden-item`: sbxup reads the GitHub App's key from Bitwarden, mints an installation token
//     for the repository in `origin` (plus the config's `repositories`), mints a fresh one before the
//     hour is up for as long as `sbx run` lasts, and revokes the last one when the session ends.
// Either way the token reaches the sandbox with `sbx secret set github`.
//
// Every failure is a warning: the sandbox starts without a token rather than not at all. The config
// comes from the repository, so the first use of a given app, item, set of repositories and
// permissions is approved once by the user, and the approval is kept in the user cache, never in the repo.

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// refreshBefore is how long before expiry a new token is minted: about every 45 minutes for
	// GitHub's one-hour tokens.
	refreshBefore = 15 * time.Minute
	retryAfter    = time.Minute
)

// userPollEvery is how often a session with a shared user token looks at the login file, so a token
// refreshed by another sbxup reaches this sandbox too. A variable for tests.
var userPollEvery = time.Minute

var (
	// gitOriginURL returns `git remote get-url origin` for dir. A variable so tests need no repo.
	gitOriginURL = func(dir string) (string, error) {
		out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
		if err != nil {
			return "", fmt.Errorf("cannot read the 'origin' remote: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	// sbxSecretSet stores the token as the sandbox's GitHub secret; the proxy injects it from there.
	// -f is required: without it sbx refuses to replace an existing secret, which every refresh and
	// every resumed sandbox has.
	sbxSecretSet = func(sandbox, token string) error {
		args := sbxSecretSetArgs(sandbox, token)
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("sbx secret set github --sandbox %s: %v %s", sandbox, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	askGitHub = func(question string) (bool, bool) { return askYesNo(os.Stdin, os.Stdout, question) }
	sleepCtx  = func(ctx context.Context, d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		}
	}
)

// sbxSecretSetArgs is the whole command, kept apart so a test can pin the -f.
func sbxSecretSetArgs(sandbox, token string) []string {
	return []string{"sbx", "secret", "set", "github", "--sandbox", sandbox, "-f", "-t", token}
}

// parseGitHubRemote turns an origin URL into owner and repo. Accepted: https://github.com/o/r(.git),
// git@github.com:o/r(.git) and ssh://git@github.com/o/r(.git).
func parseGitHubRemote(remote string) (owner, repo string, err error) {
	host, p := "", ""
	switch {
	case strings.Contains(remote, "://"):
		u, perr := url.Parse(remote)
		if perr != nil {
			return "", "", fmt.Errorf("cannot parse the 'origin' URL %q", remote)
		}
		host, p = u.Hostname(), u.Path
	case strings.Contains(remote, ":"):
		// scp-like: [user@]host:owner/repo
		i := strings.Index(remote, ":")
		host, p = remote[:i], remote[i+1:]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
	default:
		return "", "", fmt.Errorf("'origin' is %q, not a GitHub URL", remote)
	}
	if !strings.EqualFold(host, "github.com") {
		return "", "", fmt.Errorf("'origin' is on %s, not github.com; GitHub App tokens need a GitHub repository", host)
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(strings.TrimSuffix(p, "/"), ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot find owner/repo in the 'origin' URL %q", remote)
	}
	return parts[0], parts[1], nil
}

// tokenRepos is the repository in origin followed by the config's extras, as names without the
// owner. One installation mints one token and a sandbox holds one GitHub secret, so every extra must
// belong to the same owner.
func tokenRepos(owner, repo string, extra []string) ([]string, error) {
	out := []string{repo}
	seen := map[string]bool{strings.ToLower(repo): true}
	for _, e := range extra {
		name := e
		if i := strings.Index(e, "/"); i >= 0 {
			if !strings.EqualFold(e[:i], owner) {
				return nil, fmt.Errorf("'github.repositories' lists %q, but the token can only cover repositories of %s (the owner of 'origin')", e, owner)
			}
			name = e[i+1:]
		}
		if name == "" || strings.Contains(name, "/") {
			return nil, fmt.Errorf("'github.repositories' entry %q is not a repository name", e)
		}
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// ghApprovalKey identifies a project together with what it asks for, so naming another item, another
// repository or a wider permission is a new question.
func ghApprovalKey(project, item, owner string, repos []string, perms map[string]string) string {
	sorted := append([]string(nil), repos...)
	sort.Strings(sorted)
	body := strings.Join([]string{resolveExisting(project), item, owner, strings.Join(sorted, ","), formatPermissions(perms)}, "\n")
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func ghApprovalPath(key string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "github", key+".json"), nil
}

func ghApproved(key string) bool {
	path, err := ghApprovalPath(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func recordGHApproval(key, project, item, owner string, repos []string, perms map[string]string) error {
	path, err := ghApprovalPath(key)
	if err != nil {
		return err
	}
	// The body is only for a human looking in the cache; the file's name is the approval.
	data, err := json.Marshal(map[string]any{
		"project": project, "bitwardenItem": item, "owner": owner, "repositories": repos,
		"permissions": perms, "approvedAt": now(),
	})
	if err != nil {
		return err
	}
	return writeCache(path, data)
}

// userApprovalKey is ghApprovalKey for the user-token mode: the project and the app, so a repository
// that switches to an app with wider permissions is asked about again.
func userApprovalKey(project, clientID string) string {
	return ghApprovalKey(project, "client-id:"+clientID, "", nil, nil)
}

func recordUserApproval(key, project, clientID string) error {
	path, err := ghApprovalPath(key)
	if err != nil {
		return err
	}
	data, err := json.Marshal(map[string]any{"project": project, "clientId": clientID, "approvedAt": now()})
	if err != nil {
		return err
	}
	return writeCache(path, data)
}

// approveGitHub asks once per key; record keeps a "yes". It reports whether to go on.
func approveGitHub(key string, record func() error) bool {
	if ghApproved(key) {
		return true
	}
	if !isInteractive() {
		warnf("GitHub token: this project's request has not been approved and there is no terminal to ask on; starting without one.")
		return false
	}
	yes, answered := askGitHub("  Allow this project to use it? [y/N] ")
	if !answered || !yes {
		fmt.Println("GitHub token: skipped; you will be asked again next time.")
		return false
	}
	if err := record(); err != nil {
		warnf("cannot remember the approval (%v); you will be asked again next time.", err)
	}
	return true
}

// ghSession holds what is needed to renew the token. In the Bitwarden mode the key stays in memory
// for the session; in the user-token mode (clientID set) the login file is the source.
type ghSession struct {
	client       *http.Client
	clientID     string
	key          *rsa.PrivateKey
	issuer       string
	owner        string
	installation int64
	repos        []string
	perms        map[string]string
	token        string
	expires      time.Time
	sent         string // the token last given to the sandbox
}

// renew brings the token up to date: a new installation token, or the shared user token (which
// another sbxup may already have refreshed).
func (s *ghSession) renew() error {
	if s.clientID == "" {
		return s.mint()
	}
	t, err := currentUserToken(s.client, s.clientID)
	if err != nil {
		return err
	}
	s.token, s.expires = t.AccessToken, t.AccessExpiresAt
	return nil
}

func (s *ghSession) mint() error {
	jwt, err := appJWT(s.issuer, s.key)
	if err != nil {
		return err
	}
	if s.installation == 0 {
		if s.installation, err = installationID(s.client, jwt, s.owner, s.repos[0]); err != nil {
			return err
		}
	}
	token, expires, err := mintToken(s.client, jwt, s.installation, s.repos, s.perms)
	if err != nil {
		return err
	}
	s.token, s.expires = token, expires
	return nil
}

// revoke ends the last token, so a sandbox left running keeps no valid one. A user token is left
// alone: other sandboxes share it, and ending it needs the app's secret.
func (s *ghSession) revoke() {
	if s == nil || s.token == "" || s.clientID != "" {
		return
	}
	if err := revokeToken(s.client, s.token); err != nil {
		warnf("cannot revoke the GitHub token (%v); it expires at %s anyway.", err, s.expires.Local().Format("15:04"))
		return
	}
	s.token = ""
	fmt.Println("GitHub token: revoked")
}

// prepareGitHubToken gets the first token, or returns nil when there is none to give: no `github:`
// block, --no-gh-token, --dry-run, a "no", an empty password, no login or any failure (a warning).
func prepareGitHubToken(g *GitHubConfig, project string, disabled, dryRun bool) *ghSession {
	if g == nil || disabled {
		return nil
	}
	if g.ClientID != "" {
		return prepareUserToken(g, project, dryRun)
	}
	return prepareAppToken(g, project, dryRun)
}

// prepareUserToken takes the shared token of the user's login for the app, refreshing it if due.
func prepareUserToken(g *GitHubConfig, project string, dryRun bool) *ghSession {
	fmt.Printf("GitHub token: your login via app %s (every organization where it is installed)\n", g.ClientID)
	if dryRun {
		fmt.Println("[dry-run] would use the login from 'sbxup --gh-login' (refreshed if due) and set it with 'sbx secret set github'.")
		return nil
	}
	key := userApprovalKey(project, g.ClientID)
	if !approveGitHub(key, func() error { return recordUserApproval(key, project, g.ClientID) }) {
		return nil
	}
	client := &http.Client{Timeout: httpClient}
	t, err := currentUserToken(client, g.ClientID)
	if err != nil {
		warnf("GitHub token: %v; starting without one.", err)
		return nil
	}
	who := ""
	if t.Login != "" {
		who = " for " + t.Login
	}
	expires := "does not expire"
	if !t.AccessExpiresAt.IsZero() {
		expires = "expires " + t.AccessExpiresAt.Local().Format("15:04")
	}
	fmt.Printf("GitHub token: ready%s (%s)\n", who, expires)
	return &ghSession{client: client, clientID: g.ClientID, token: t.AccessToken, expires: t.AccessExpiresAt}
}

// prepareAppToken mints an installation token with the app's key from Bitwarden.
func prepareAppToken(g *GitHubConfig, project string, dryRun bool) *ghSession {
	remote, err := gitOriginURL(project)
	if err != nil {
		warnf("GitHub token: %v; starting without one.", err)
		return nil
	}
	owner, repo, err := parseGitHubRemote(remote)
	if err != nil {
		warnf("GitHub token: %v; starting without one.", err)
		return nil
	}
	repos, err := tokenRepos(owner, repo, g.Repositories)
	if err != nil {
		warnf("GitHub token: %v; starting without one.", err)
		return nil
	}

	full := make([]string, len(repos))
	for i, r := range repos {
		full[i] = owner + "/" + r
	}
	fmt.Printf("GitHub token: %s from Bitwarden %q\n", strings.Join(full, ", "), g.BitwardenItem)
	fmt.Printf("  permissions: %s\n", formatPermissions(g.Permissions))
	if dryRun {
		fmt.Println("[dry-run] would read the GitHub App key from Bitwarden, mint the token and set it with 'sbx secret set github'.")
		return nil
	}

	key := ghApprovalKey(project, g.BitwardenItem, owner, repos, g.Permissions)
	if !approveGitHub(key, func() error {
		return recordGHApproval(key, project, g.BitwardenItem, owner, repos, g.Permissions)
	}) {
		return nil
	}

	vault, err := openVault()
	if errors.Is(err, errNoToken) {
		fmt.Println("GitHub token: skipped")
		return nil
	}
	if err != nil {
		warnf("GitHub token: %v; starting without one.", err)
		return nil
	}
	k, err := readAppKey(vault, g)
	vault.close()
	if err != nil {
		warnf("GitHub token: %v; starting without one.", err)
		return nil
	}
	rk, err := parsePrivateKey(k.pem)
	if err != nil {
		warnf("GitHub token: the key in Bitwarden item %q: %v; starting without one.", g.BitwardenItem, err)
		return nil
	}

	s := &ghSession{
		client: &http.Client{Timeout: httpClient}, key: rk, issuer: k.issuer,
		owner: owner, repos: repos, perms: g.Permissions,
	}
	if err := s.mint(); err != nil {
		warnf("GitHub token: %v; starting without one.", err)
		return nil
	}
	fmt.Printf("GitHub token: ready (expires %s)\n", s.expires.Local().Format("15:04"))
	return s
}

// keepFresh replaces the token before it expires, until ctx ends; run() gave the sandbox the first one.
// A user token is shared, so that session looks at the login file every userPollEvery and passes on
// a token that another sbxup refreshed.
func (s *ghSession) keepFresh(ctx context.Context, name string) {
	s.sent = s.token
	lastErr := ""
	for {
		wait := userPollEvery
		if s.clientID == "" {
			if wait = s.expires.Sub(now()) - refreshBefore; wait < retryAfter {
				wait = retryAfter
			}
		}
		if !sleepCtx(ctx, wait) {
			return
		}
		old := s.token
		err := s.renew()
		if err == nil && s.token != s.sent {
			if err = sbxSecretSet(name, s.token); err == nil {
				s.sent = s.token
			}
		}
		if err != nil {
			if s.clientID == "" {
				warnf("GitHub token: refresh failed (%v); retrying in a minute.", err)
				s.expires = now().Add(refreshBefore + retryAfter)
			} else if msg := err.Error(); msg != lastErr {
				// Checked every minute, so the same failure is reported once.
				warnf("GitHub token: %v", err)
				lastErr = msg
			}
			continue
		}
		lastErr = ""
		if s.clientID == "" && old != "" && old != s.token {
			_ = revokeToken(s.client, old)
		}
	}
}

// ghTokenCmd is `sbxup --gh-token`: one token for the sandbox that already runs for this project,
// e.g. from a second terminal. It is not refreshed or revoked; it simply expires.
func ghTokenCmd(g *GitHubConfig, project, sandbox string, dryRun bool) error {
	if g == nil {
		return errors.New("--gh-token needs a 'github:' block with 'client-id' or 'bitwarden-item' in the config")
	}
	target := resolveSandboxName(sandbox)
	if target == "" && !dryRun {
		return fmt.Errorf("no sandbox found matching '%s'; start it with sbxup first", sandbox)
	}
	s := prepareGitHubToken(g, project, false, dryRun)
	if s == nil {
		return nil
	}
	if err := sbxSecretSet(target, s.token); err != nil {
		s.revoke()
		return err
	}
	fmt.Printf("GitHub token: set in %s, valid until %s (not refreshed; run --gh-token again after that)\n",
		target, s.expires.Local().Format("15:04"))
	return nil
}
