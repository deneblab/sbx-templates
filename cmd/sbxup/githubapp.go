package main

// GitHub App installation tokens. The app's private key signs a short JWT; GitHub swaps the JWT for
// an installation token that lasts an hour and can be narrowed to a few repositories and
// permissions. Only that token ever reaches a sandbox; the key stays in sbxup's memory.
//
// Standard library only: an RS256 JWT is a few lines of crypto/rsa.

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// githubAPIBase is a variable so tests can point it at an httptest server.
var githubAPIBase = "https://api.github.com"

// parsePrivateKey accepts the PKCS#1 key GitHub hands out ("BEGIN RSA PRIVATE KEY") and the PKCS#8
// form a conversion produces ("BEGIN PRIVATE KEY").
func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("the private key is not PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse the private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the private key is not RSA; GitHub App keys are")
	}
	return rk, nil
}

// appJWT signs the app's JWT. iat is a minute in the past to absorb clock drift, and exp stays under
// GitHub's ten-minute ceiling. The issuer is the client ID, which GitHub recommends over the app ID;
// both are accepted.
func appJWT(issuer string, key *rsa.PrivateKey) (string, error) {
	enc := base64.RawURLEncoding
	t := now()
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": t.Add(-60 * time.Second).Unix(),
		"exp": t.Add(9 * time.Minute).Unix(),
		"iss": issuer,
	})
	if err != nil {
		return "", err
	}
	signed := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signed + "." + enc.EncodeToString(sig), nil
}

// githubCall sends one API request with the given bearer token and decodes a JSON reply into out
// (when out is not nil). A non-2xx status comes back as *apiError so callers can word it.
func githubCall(client *http.Client, method, path, bearer string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, githubAPIBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "sbxup/"+version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var msg struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &msg)
		return &apiError{Status: resp.StatusCode, Message: msg.Message}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitHub API returned %d", e.Status)
	}
	return fmt.Sprintf("GitHub API returned %d: %s", e.Status, e.Message)
}

func apiStatus(err error) int {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// installationID finds the app's installation that covers owner/repo.
func installationID(client *http.Client, jwt, owner, repo string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	err := githubCall(client, http.MethodGet, "/repos/"+owner+"/"+repo+"/installation", jwt, nil, &out)
	if err != nil {
		switch apiStatus(err) {
		case http.StatusNotFound:
			return 0, fmt.Errorf("the GitHub App is not installed on %s/%s; install it there or remove 'github:' from the config", owner, repo)
		case http.StatusUnauthorized:
			return 0, fmt.Errorf("GitHub refused the app's JWT (%v); check the client ID and the private key in Bitwarden", err)
		}
		return 0, err
	}
	return out.ID, nil
}

// mintToken creates an installation token narrowed to repos (names without the owner) and perms.
func mintToken(client *http.Client, jwt string, installation int64, repos []string, perms map[string]string) (string, time.Time, error) {
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	body := map[string]any{"repositories": repos, "permissions": perms}
	err := githubCall(client, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installation), jwt, body, &out)
	if s := apiStatus(err); s == http.StatusUnprocessableEntity || s == http.StatusForbidden {
		// GitHub does not say which repository or permission it refused, so list what was asked.
		return "", time.Time{}, fmt.Errorf("%v — the app cannot grant this token: repositories %s, permissions %s. "+
			"Check that the app is installed on all of them and has these permissions",
			err, strings.Join(repos, ", "), formatPermissions(perms))
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if out.Token == "" {
		return "", time.Time{}, errors.New("GitHub returned no token")
	}
	return out.Token, out.ExpiresAt, nil
}

// revokeToken ends a token before its hour is up. A token that has already expired answers 401,
// which is the outcome wanted anyway.
func revokeToken(client *http.Client, token string) error {
	err := githubCall(client, http.MethodDelete, "/installation/token", token, nil, nil)
	if apiStatus(err) == http.StatusUnauthorized {
		return nil
	}
	return err
}

// formatPermissions renders permissions in a stable order: "contents:write, issues:write".
func formatPermissions(perms map[string]string) string {
	names := make([]string, 0, len(perms))
	for n := range perms {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+":"+perms[n])
	}
	return strings.Join(parts, ", ")
}
