package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.2.9", "0.2.8", 1},
		{"0.2.8", "0.2.9", -1},
		{"0.2.8", "0.2.8", 0},
		{"0.10.0", "0.9.9", 1}, // numeric, not lexical
		{"1.0", "1.0.0", 0},
		{"0.2.8", "local", 1}, // a non-version sorts below every version
		{"local", "0.0.1", -1},
		{"local", "latest", 0},
	} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNewestVersion(t *testing.T) {
	if got := newestVersion([]string{"0.2.7", "0.10.1", "local", "0.9.0", "latest"}); got != "0.10.1" {
		t.Errorf("newestVersion = %q", got)
	}
	if got := newestVersion([]string{"local", "latest"}); got != "" {
		t.Errorf("newestVersion of non-versions = %q, want empty", got)
	}
	if got := newestVersion(nil); got != "" {
		t.Errorf("newestVersion(nil) = %q", got)
	}
}

func TestTemplateTagsIn(t *testing.T) {
	out := sbxTemplateLsOutput +
		"docker.io/library/sbx-claude-dotnet10           0.2.8                c0ffee000000   claude-code          2 minutes ago\n"

	got := templateTagsIn(out, "sbx-claude-dotnet10")
	if strings.Join(got, ",") != "0.1.3,0.2.8" {
		t.Errorf("tags = %v, want the locally built 0.1.3 and 0.2.8", got)
	}
	// The frozen Docker Hub image shares the repository *name* but not the repository: its
	// `latest` must not be mistaken for a built version.
	for _, tag := range got {
		if tag == "latest" {
			t.Errorf("counted the pkudrel/ Docker Hub image: %v", got)
		}
	}
	if got := templateTagsIn(out, "sbx-claude-golang124-node24"); len(got) != 0 {
		t.Errorf("tags for an unbuilt template = %v", got)
	}
}

func TestLocalRepoMatchesLocalTag(t *testing.T) {
	e := TemplateEntry{Name: "sbx-claude-dotnet10", Version: "0.2.8"}
	if got := e.LocalRepo(defaultRef("")); got != "sbx-claude-dotnet10" {
		t.Errorf("LocalRepo = %q", got)
	}
	fork := releaseRef{Owner: "Someone", Repo: "x"}
	if got := e.LocalRepo(fork); got != "someone-sbx-claude-dotnet10" {
		t.Errorf("fork LocalRepo = %q", got)
	}
	if got, want := e.LocalTag(fork), e.LocalRepo(fork)+":0.2.8"; got != want {
		t.Errorf("LocalTag = %q, want %q", got, want)
	}
}

func TestAskYesNo(t *testing.T) {
	for in, want := range map[string]bool{
		"y\n": true, "Y\n": true, "yes\n": true, " YES \n": true, "y": true,
		"\n": false, "": false, "n\n": false, "no\n": false, "sure\n": false,
	} {
		var out strings.Builder
		got, answered := askYesNo(strings.NewReader(in), &out, "Q? ")
		if got != want {
			t.Errorf("askYesNo(%q) = %v, want %v", in, got, want)
		}
		// Input that ends with nothing typed is no answer; anything else, even a bare Enter, is one.
		if wantAnswered := in != ""; answered != wantAnswered {
			t.Errorf("askYesNo(%q) answered = %v, want %v", in, answered, wantAnswered)
		}
		if !strings.HasPrefix(out.String(), "Q? ") {
			t.Errorf("question printed as %q", out.String())
		}
	}
}

// updateEnv stubs everything offerUpdate consults and returns a handle on what it was asked.
type updateEnv struct {
	asked int
}

func stubUpdate(t *testing.T, installed []string, interactiveTerm, docker bool, answer bool) *updateEnv {
	return stubUpdateAnswer(t, installed, interactiveTerm, docker, answer, true)
}

func stubUpdateAnswer(t *testing.T, installed []string, interactiveTerm, docker, answer, answered bool) *updateEnv {
	t.Helper()
	cacheHome(t)
	env := &updateEnv{}

	origTags, origInt, origAsk, origDocker := sbxTemplateTags, isInteractive, askUpdate, dockerAvailable
	t.Cleanup(func() {
		sbxTemplateTags, isInteractive, askUpdate, dockerAvailable = origTags, origInt, origAsk, origDocker
	})
	sbxTemplateTags = func(string) []string { return installed }
	isInteractive = func() bool { return interactiveTerm }
	dockerAvailable = func() bool { return docker }
	askUpdate = func(string) (bool, bool) { env.asked++; return answer, answered }

	// The record a real run has just written after resolving latest.
	if err := writeLatestRecord(defaultRef(""), "templates-v0.4.0"); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestOfferUpdate(t *testing.T) {
	entry := &TemplateEntry{Name: "sbx-claude-dotnet10", Short: "dotnet10", Version: "0.2.9"}
	ref := defaultRef("templates-v0.4.0")
	const old = "sbx-claude-dotnet10:0.2.8"

	t.Run("first start builds without asking", func(t *testing.T) {
		env := stubUpdate(t, nil, true, true, true)
		if use, done := offerUpdate(entry, ref, "", &options{}); done || use != "" {
			t.Fatalf("got (%q, %v), want a build", use, done)
		}
		if env.asked != 0 {
			t.Error("asked on a first start")
		}
	})

	t.Run("only non-version tags count as a first start", func(t *testing.T) {
		env := stubUpdate(t, []string{"local"}, true, true, true)
		if _, done := offerUpdate(entry, ref, "", &options{}); done {
			t.Fatal("treated a `local` tag as an installed version")
		}
		if env.asked != 0 {
			t.Error("asked")
		}
	})

	t.Run("the wanted version is already built", func(t *testing.T) {
		stubUpdate(t, []string{"0.2.8", "0.2.9"}, true, true, true)
		if _, done := offerUpdate(entry, ref, "", &options{}); done {
			t.Fatal("offered an update to a version that is installed")
		}
	})

	t.Run("a newer version is already installed", func(t *testing.T) {
		env := stubUpdate(t, []string{"0.2.7", "0.3.0"}, true, true, true)
		use, done := offerUpdate(entry, ref, "", &options{})
		if !done || use != "sbx-claude-dotnet10:0.3.0" {
			t.Fatalf("got (%q, %v), want the newest installed", use, done)
		}
		if env.asked != 0 {
			t.Error("asked about a downgrade")
		}
	})

	t.Run("yes builds", func(t *testing.T) {
		env := stubUpdate(t, []string{"0.2.8"}, true, true, true)
		if use, done := offerUpdate(entry, ref, "", &options{}); done || use != "" {
			t.Fatalf("got (%q, %v), want a build", use, done)
		}
		if env.asked != 1 {
			t.Errorf("asked %d times", env.asked)
		}
		if rec, _ := readLatestRecord(ref); len(rec.Declined) != 0 {
			t.Errorf("recorded a decline after a yes: %v", rec.Declined)
		}
	})

	t.Run("no keeps the old version, and is remembered", func(t *testing.T) {
		env := stubUpdate(t, []string{"0.2.8"}, true, true, false)
		use, done := offerUpdate(entry, ref, "", &options{})
		if !done || use != old {
			t.Fatalf("got (%q, %v), want %q", use, done, old)
		}
		if rec, _ := readLatestRecord(ref); len(rec.Declined) != 1 || rec.Declined[0] != "sbx-claude-dotnet10:0.2.9" {
			t.Fatalf("Declined = %v", rec.Declined)
		}
		// The next start must not ask again.
		use, done = offerUpdate(entry, ref, "", &options{})
		if !done || use != old {
			t.Fatalf("second run got (%q, %v)", use, done)
		}
		if env.asked != 1 {
			t.Errorf("asked %d times, want once", env.asked)
		}
	})

	t.Run("end of input is no answer, and is not remembered", func(t *testing.T) {
		env := stubUpdateAnswer(t, []string{"0.2.8"}, true, true, false, false)
		use, done := offerUpdate(entry, ref, "", &options{})
		if !done || use != old {
			t.Fatalf("got (%q, %v), want %q", use, done, old)
		}
		if rec, _ := readLatestRecord(ref); len(rec.Declined) != 0 {
			t.Errorf("recorded a decline nobody typed: %v", rec.Declined)
		}
		offerUpdate(entry, ref, "", &options{})
		if env.asked != 2 {
			t.Errorf("asked %d times, want to ask again", env.asked)
		}
	})

	t.Run("the next release check forgets a no", func(t *testing.T) {
		stubUpdate(t, []string{"0.2.8"}, true, true, false)
		offerUpdate(entry, ref, "", &options{})
		// Resolving latest from the network writes a fresh record.
		if err := writeLatestRecord(ref, "templates-v0.5.0"); err != nil {
			t.Fatal(err)
		}
		if rec, _ := readLatestRecord(ref); len(rec.Declined) != 0 {
			t.Errorf("a re-resolved record kept the decline: %v", rec.Declined)
		}
	})

	t.Run("a decline survives in the record's own JSON", func(t *testing.T) {
		stubUpdate(t, []string{"0.2.8"}, true, true, false)
		offerUpdate(entry, ref, "", &options{})
		path, _ := latestRecordPath(ref)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if _, ok := raw["resolvedAt"]; !ok {
			t.Errorf("the timestamp was dropped: %s", data)
		}
	})

	t.Run("no terminal: no question, no build", func(t *testing.T) {
		env := stubUpdate(t, []string{"0.2.8"}, false, true, true)
		use, done := offerUpdate(entry, ref, "", &options{})
		if !done || use != old {
			t.Fatalf("got (%q, %v), want %q", use, done, old)
		}
		if env.asked != 0 {
			t.Error("prompted without a terminal")
		}
	})

	t.Run("no Docker: no question, no build", func(t *testing.T) {
		env := stubUpdate(t, []string{"0.2.8"}, true, false, true)
		use, done := offerUpdate(entry, ref, "", &options{})
		if !done || use != old {
			t.Fatalf("got (%q, %v), want %q", use, done, old)
		}
		if env.asked != 0 {
			t.Error("asked a question that cannot be carried out")
		}
	})

	t.Run("dry-run changes nothing", func(t *testing.T) {
		env := stubUpdate(t, []string{"0.2.8"}, true, true, true)
		use, done := offerUpdate(entry, ref, "", &options{dryRun: true})
		if !done || use != old {
			t.Fatalf("got (%q, %v), want %q", use, done, old)
		}
		if env.asked != 0 {
			t.Error("prompted under --dry-run")
		}
		if rec, _ := readLatestRecord(ref); len(rec.Declined) != 0 {
			t.Errorf("--dry-run wrote a decline: %v", rec.Declined)
		}
	})
}

// manifestFor caches a manifest for release tag so ensureLocalTemplate needs no network.
func manifestFor(t *testing.T, tag, version string) {
	t.Helper()
	ref := defaultRef(tag)
	dir, err := templatesCacheDir(ref)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":2,"release":"` + tag + `","version":"0.4.0","tarball":"t.tar.gz","templates":[` +
		`{"name":"sbx-claude-dotnet10","short":"dotnet10","version":"` + version + `"}]}`
	if err := writeCache(filepath.Join(dir, "manifest.json"), []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureLocalTemplateKeepsTheInstalledVersionUntilAsked(t *testing.T) {
	// Any attempt to fetch a Dockerfile would go here, and fail loudly.
	var fetched int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	origWeb := repoWeb
	repoWeb = srv.URL
	t.Cleanup(func() { repoWeb = origWeb })

	setup := func(t *testing.T, installed []string, listed bool) {
		stubUpdate(t, installed, false, true, false) // no terminal
		manifestFor(t, "templates-v0.4.0", "0.2.9")
		orig := sbxTemplateListed
		sbxTemplateListed = func(string) bool { return listed }
		t.Cleanup(func() { sbxTemplateListed = orig })
		fetched = 0
	}

	t.Run("a newer release with an older build installed runs the old one", func(t *testing.T) {
		setup(t, []string{"0.2.8"}, false)
		tag, err := ensureLocalTemplate("dotnet10", "", "", &options{})
		if err != nil {
			t.Fatal(err)
		}
		if tag != "sbx-claude-dotnet10:0.2.8" {
			t.Errorf("tag = %q, want the installed 0.2.8", tag)
		}
		if fetched != 0 {
			t.Errorf("fetched %d assets before the user agreed", fetched)
		}
	})

	t.Run("--update is consent: it goes on to fetch", func(t *testing.T) {
		setup(t, []string{"0.2.8"}, false)
		if _, err := ensureLocalTemplate("dotnet10", "", "", &options{update: true}); err == nil {
			t.Fatal("expected the (unreachable) Dockerfile fetch to fail")
		}
		if fetched == 0 {
			t.Error("--update did not try to fetch the new version")
		}
	})

	t.Run("a pinned release is never a question", func(t *testing.T) {
		setup(t, []string{"0.2.8"}, false)
		sbxTemplateTags = func(string) []string { t.Fatal("asked what is installed for a pinned release"); return nil }
		if _, err := ensureLocalTemplate("dotnet10", "templates-v0.4.0", "", &options{}); err == nil {
			t.Fatal("expected the (unreachable) Dockerfile fetch to fail")
		}
		if fetched == 0 {
			t.Error("a pinned release was not built")
		}
	})

	t.Run("the version is already built: reused, nothing fetched", func(t *testing.T) {
		setup(t, []string{"0.2.8", "0.2.9"}, true)
		tag, err := ensureLocalTemplate("dotnet10", "", "", &options{})
		if err != nil {
			t.Fatal(err)
		}
		if tag != "sbx-claude-dotnet10:0.2.9" || fetched != 0 {
			t.Errorf("tag = %q, fetched = %d", tag, fetched)
		}
	})
}
