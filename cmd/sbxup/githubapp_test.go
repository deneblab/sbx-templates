package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	testKeyOnce sync.Once
	testKeyVal  *rsa.PrivateKey
)

// testKey is generated once per test binary: RSA generation is the slow part of these tests.
func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testKeyVal = k
	})
	return testKeyVal
}

func pkcs1PEM(k *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
}

func TestParsePrivateKey(t *testing.T) {
	k := testKey(t)
	if got, err := parsePrivateKey(pkcs1PEM(k)); err != nil || !got.Equal(k) {
		t.Fatalf("PKCS#1: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	p8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if got, err := parsePrivateKey(p8); err != nil || !got.Equal(k) {
		t.Fatalf("PKCS#8: %v", err)
	}
	if _, err := parsePrivateKey([]byte("not a key")); err == nil {
		t.Fatal("non-PEM accepted")
	}
}

func TestAppJWT(t *testing.T) {
	k := testKey(t)
	fixed := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	orig := now
	t.Cleanup(func() { now = orig })
	now = func() time.Time { return fixed }

	jwt, err := appJWT("Iv23client", k)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	enc := base64.RawURLEncoding
	hdr, _ := enc.DecodeString(parts[0])
	if string(hdr) != `{"alg":"RS256","typ":"JWT"}` {
		t.Fatalf("header %s", hdr)
	}
	var claims struct {
		Iat, Exp int64
		Iss      string
	}
	body, _ := enc.DecodeString(parts[1])
	if err := json.Unmarshal(body, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "Iv23client" || claims.Iat != fixed.Unix()-60 || claims.Exp != fixed.Add(9*time.Minute).Unix() {
		t.Fatalf("claims %+v", claims)
	}
	sig, _ := enc.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&k.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature: %v", err)
	}
}

// fakeGitHub serves the three endpoints sbxup uses and records what it was sent.
type fakeGitHub struct {
	mu        sync.Mutex
	minted    int
	lastBody  map[string]any
	revoked   []string
	mintFails int // status to answer mint with, 0 = success
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/deneblab/sbx-templates/installation":
			if strings.Count(auth, ".") != 2 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"id":42}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/installation"):
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/42/access_tokens":
			if f.mintFails != 0 {
				w.WriteHeader(f.mintFails)
				w.Write([]byte(`{"message":"There is at least one repository that does not exist or is not accessible"}`))
				return
			}
			f.lastBody = nil
			json.NewDecoder(r.Body).Decode(&f.lastBody)
			f.minted++
			exp := now().Add(time.Hour).UTC().Format(time.RFC3339)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"token":"ghs_` + string(rune('0'+f.minted)) + `","expires_at":"` + exp + `"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/installation/token":
			f.revoked = append(f.revoked, auth)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	orig := githubAPIBase
	t.Cleanup(func() { githubAPIBase = orig })
	githubAPIBase = srv.URL
	return srv
}

func (f *fakeGitHub) httpClient() *http.Client { return &http.Client{} }

func TestInstallationAndMint(t *testing.T) {
	f := &fakeGitHub{}
	f.server(t)
	client := &http.Client{}
	jwt, _ := appJWT("Iv23client", testKey(t))

	id, err := installationID(client, jwt, "deneblab", "sbx-templates")
	if err != nil || id != 42 {
		t.Fatalf("installation: %d %v", id, err)
	}
	if _, err := installationID(client, jwt, "deneblab", "elsewhere"); err == nil || !strings.Contains(err.Error(), "not installed on deneblab/elsewhere") {
		t.Fatalf("404 not worded: %v", err)
	}
	if _, err := installationID(client, "not-a-jwt", "deneblab", "sbx-templates"); err == nil || !strings.Contains(err.Error(), "client ID") {
		t.Fatalf("401 not worded: %v", err)
	}

	tok, exp, err := mintToken(client, jwt, 42, []string{"sbx-templates", "docs"}, map[string]string{"contents": "write"})
	if err != nil || tok != "ghs_1" || exp.IsZero() {
		t.Fatalf("mint: %q %v %v", tok, exp, err)
	}
	if got := f.lastBody["repositories"]; len(got.([]any)) != 2 {
		t.Fatalf("repositories sent: %v", got)
	}
	if got := f.lastBody["permissions"].(map[string]any)["contents"]; got != "write" {
		t.Fatalf("permissions sent: %v", f.lastBody["permissions"])
	}

	f.mintFails = http.StatusUnprocessableEntity
	if _, _, err := mintToken(client, jwt, 42, []string{"sbx-templates", "gone"}, map[string]string{"contents": "write"}); err == nil ||
		!strings.Contains(err.Error(), "sbx-templates, gone") {
		t.Fatalf("422 does not list the repos: %v", err)
	}

	if err := revokeToken(client, "ghs_1"); err != nil || len(f.revoked) != 1 || f.revoked[0] != "ghs_1" {
		t.Fatalf("revoke: %v %v", err, f.revoked)
	}
}

func TestFormatPermissionsIsSorted(t *testing.T) {
	got := formatPermissions(map[string]string{"pull_requests": "write", "contents": "read"})
	if got != "contents:read, pull_requests:write" {
		t.Fatalf("got %q", got)
	}
}
