package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// runKeybindingsScript runs the real script with sh and jq against home and returns the file afterwards.
func runKeybindingsScript(t *testing.T, home string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", keybindingsScript)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = errors.New(err.Error() + " " + string(out))
	}
	data, _ := os.ReadFile(filepath.Join(home, ".claude", "keybindings.json"))
	return string(data), err
}

func writeKeybindings(t *testing.T, home, content string) {
	t.Helper()
	path := filepath.Join(home, ".claude", "keybindings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// chatBindings returns the merged Chat bindings of every Chat context, and how many there are.
func chatBindings(t *testing.T, file string) (map[string]any, int) {
	t.Helper()
	var doc struct {
		Bindings []struct {
			Context  string         `json:"context"`
			Bindings map[string]any `json:"bindings"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal([]byte(file), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, file)
	}
	merged, n := map[string]any{}, 0
	for _, b := range doc.Bindings {
		if b.Context == "Chat" {
			n++
			for k, v := range b.Bindings {
				merged[k] = v
			}
		}
	}
	return merged, n
}

func TestKeybindingsScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("no jq")
	}

	t.Run("no file", func(t *testing.T) {
		home := t.TempDir()
		got, err := runKeybindingsScript(t, home)
		if err != nil {
			t.Fatal(err)
		}
		chat, n := chatBindings(t, got)
		if n != 1 || chat["alt+v"] != "chat:imagePaste" || !strings.Contains(got, "schemastore.org/claude-code-keybindings.json") {
			t.Fatalf("created:\n%s", got)
		}
		again, err := runKeybindingsScript(t, home)
		if err != nil || again != got {
			t.Fatalf("second run changed the file (%v):\n%s", err, again)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		home := t.TempDir()
		writeKeybindings(t, home, "  \n")
		got, err := runKeybindingsScript(t, home)
		if chat, _ := chatBindings(t, got); err != nil || chat["alt+v"] != "chat:imagePaste" {
			t.Fatalf("%v\n%s", err, got)
		}
	})

	t.Run("other bindings kept", func(t *testing.T) {
		home := t.TempDir()
		writeKeybindings(t, home, `{"bindings":[{"context":"Global","bindings":{"ctrl+k":"app:x"}},`+
			`{"context":"Chat","bindings":{"ctrl+e":"chat:externalEditor"}}]}`)
		got, err := runKeybindingsScript(t, home)
		if err != nil {
			t.Fatal(err)
		}
		chat, n := chatBindings(t, got)
		want := map[string]any{"ctrl+e": "chat:externalEditor", "alt+v": "chat:imagePaste"}
		if n != 1 || !reflect.DeepEqual(chat, want) || !strings.Contains(got, `"ctrl+k": "app:x"`) {
			t.Fatalf("merged:\n%s", got)
		}
	})

	t.Run("Chat without bindings", func(t *testing.T) {
		home := t.TempDir()
		writeKeybindings(t, home, `{"bindings":[{"context":"Chat","bindings":null}]}`)
		got, err := runKeybindingsScript(t, home)
		if chat, n := chatBindings(t, got); err != nil || n != 1 || chat["alt+v"] != "chat:imagePaste" {
			t.Fatalf("%v\n%s", err, got)
		}
	})

	for name, own := range map[string]string{"own action": `"chat:submit"`, "unbound": `null`} {
		t.Run("user's alt+v: "+name, func(t *testing.T) {
			home := t.TempDir()
			writeKeybindings(t, home, `{"bindings":[{"context":"Chat","bindings":{"alt+v":`+own+`}}]}`)
			got, err := runKeybindingsScript(t, home)
			if err != nil {
				t.Fatal(err)
			}
			chat, n := chatBindings(t, got)
			if n != 1 || len(chat) != 1 || !strings.Contains(got, `"alt+v": `+own) {
				t.Fatalf("the user's alt+v was changed:\n%s", got)
			}
		})
	}

	t.Run("not JSON", func(t *testing.T) {
		home := t.TempDir()
		writeKeybindings(t, home, "{broken")
		got, err := runKeybindingsScript(t, home)
		if err == nil || got != "{broken" {
			t.Fatalf("a broken file must fail and stay (%v):\n%s", err, got)
		}
		if _, err := os.Stat(filepath.Join(home, ".claude", "keybindings.json.sbxup")); err == nil {
			t.Fatal("temp file left behind")
		}
	})
}

// fakeKeybindingsExec counts the execs; fail makes them fail.
func fakeKeybindingsExec(t *testing.T, fail *bool) *int {
	t.Helper()
	cacheHome(t)
	n := 0
	orig := sbxExecStdin
	t.Cleanup(func() { sbxExecStdin = orig })
	sbxExecStdin = func(sandbox, script, stdin string) error {
		n++
		if script != keybindingsScript {
			t.Errorf("unexpected script %q", script)
		}
		if *fail {
			return errors.New("refused")
		}
		return nil
	}
	return &n
}

func TestApplyKeybindings(t *testing.T) {
	fail := false
	n := fakeKeybindingsExec(t, &fail)

	// A new sandbox: merged once, then a resume with the record costs nothing.
	capture(t, func() { applyKeybindings("x--y--claude", true, false) })
	capture(t, func() { applyKeybindings("x--y--claude", false, false) })
	if *n != 1 {
		t.Fatalf("new + resume: %d execs", *n)
	}

	// The same name created again (after sbx rm): the old record does not count.
	capture(t, func() { applyKeybindings("x--y--claude", true, false) })
	if *n != 2 {
		t.Fatalf("fresh with a stale record: %d execs", *n)
	}

	// A sandbox made by an older sbxup (no record) gets it on resume.
	capture(t, func() { applyKeybindings("old", false, false) })
	if *n != 3 {
		t.Fatalf("resume without a record: %d execs", *n)
	}
}

func TestApplyKeybindingsFailureAndDryRun(t *testing.T) {
	fail := true
	n := fakeKeybindingsExec(t, &fail)

	out := capture(t, func() { applyKeybindings("s", true, false) })
	if *n != 1 || !strings.Contains(out, "WARNING: cannot add the alt+v image-paste key") {
		t.Fatalf("failure: %d execs\n%s", *n, out)
	}
	// Nothing was recorded, so the next start tries again.
	fail = false
	capture(t, func() { applyKeybindings("s", false, false) })
	if *n != 2 {
		t.Fatalf("retry after a failure: %d execs", *n)
	}

	out = capture(t, func() { applyKeybindings("d", true, true) })
	if *n != 2 || !strings.Contains(out, "[dry-run] sbx exec d sh -c") {
		t.Fatalf("dry-run: %d execs\n%s", *n, out)
	}
	if path, _ := keybindingsRecordPath("d"); func() bool { _, err := os.Stat(path); return err == nil }() {
		t.Fatal("dry-run wrote a record")
	}
}
