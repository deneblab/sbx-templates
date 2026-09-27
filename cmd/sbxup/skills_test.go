package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubSkills gives each test a private home (the skill sources) and cache (the record), and makes
// the import and the prompt scriptable. It returns the home directory and counters for how many
// times sbx was asked to import and the user was asked.
func stubSkills(t *testing.T, interactiveTerm, answer, answered bool, importErr error) (home string, imports, asked *int) {
	t.Helper()
	cacheHome(t)
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	imports, asked = new(int), new(int)
	origImp, origAsk, origInt := sbxSkillsImport, askSkills, isInteractive
	t.Cleanup(func() { sbxSkillsImport, askSkills, isInteractive = origImp, origAsk, origInt })
	sbxSkillsImport = func() (string, error) { *imports++; return "boom output", importErr }
	askSkills = func(string) (bool, bool) { *asked++; return answer, answered }
	isInteractive = func() bool { return interactiveTerm }
	return home, imports, asked
}

// skill writes one skill file under a source directory of home.
func skill(t *testing.T, home, source, name, body string) {
	t.Helper()
	dir := filepath.Join(home, filepath.FromSlash(source), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// capture runs f with stdout and stderr replaced, and returns what was printed to both.
func capture(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	f()
	os.Stdout, os.Stderr = origOut, origErr
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestImportSkillsArgs(t *testing.T) {
	// --force is what keeps sbx from prompting for every installed skill.
	if got := strings.Join(skillsImportArgs, " "); got != "skills import --force" {
		t.Fatalf("args = %q", got)
	}
}

func TestSkillsChecksum(t *testing.T) {
	home := t.TempDir()
	empty, err := skillsChecksum(home)
	if err != nil {
		t.Fatal(err)
	}

	skill(t, home, ".claude/skills", "commit", "v1")
	one, _ := skillsChecksum(home)
	if one == empty {
		t.Fatal("adding a skill did not change the checksum")
	}
	if again, _ := skillsChecksum(home); again != one {
		t.Fatal("the checksum is not stable")
	}

	skill(t, home, ".claude/skills", "commit", "v2")
	if edited, _ := skillsChecksum(home); edited == one {
		t.Error("editing a skill did not change the checksum")
	}
	skill(t, home, ".claude/skills", "commit", "v1")

	// The same content in another agent's directory is a different state: sbx reads both.
	skill(t, home, ".cursor/skills", "review", "x")
	if other, _ := skillsChecksum(home); other == one {
		t.Error("a skill in another source directory was not seen")
	}
	os.RemoveAll(filepath.Join(home, ".cursor"))

	// A loose file at the top level is skipped by sbx, so it must not count.
	os.WriteFile(filepath.Join(home, ".claude", "skills", "notes.txt"), []byte("x"), 0o644)
	if loose, _ := skillsChecksum(home); loose != one {
		t.Error("a loose top-level file changed the checksum")
	}
}

func TestImportSkills(t *testing.T) {
	t.Run("a new sandbox imports without asking and records the checksum", func(t *testing.T) {
		home, imports, asked := stubSkills(t, true, false, true, nil)
		skill(t, home, ".claude/skills", "commit", "v1")
		capture(t, func() { importSkills(false, false, false) })
		if *imports != 1 || *asked != 0 {
			t.Fatalf("imported %d, asked %d; want 1 and 0", *imports, *asked)
		}
		rec, ok := readSkillsRecord()
		if sum, _ := skillsChecksum(home); !ok || rec.Checksum != sum || !rec.Imported {
			t.Fatalf("record = %+v, %v", rec, ok)
		}
	})

	t.Run("a resume with unchanged skills does nothing", func(t *testing.T) {
		home, imports, asked := stubSkills(t, true, true, true, nil)
		skill(t, home, ".claude/skills", "commit", "v1")
		capture(t, func() { importSkills(false, false, false) })
		out := capture(t, func() { importSkills(true, false, false) })
		if *imports != 1 || *asked != 0 || out != "" {
			t.Fatalf("imported %d, asked %d, printed %q", *imports, *asked, out)
		}
	})

	t.Run("a resume with changed skills asks, and yes imports", func(t *testing.T) {
		home, imports, asked := stubSkills(t, true, true, true, nil)
		skill(t, home, ".claude/skills", "commit", "v1")
		capture(t, func() { importSkills(false, false, false) })
		skill(t, home, ".claude/skills", "commit", "v2")
		capture(t, func() { importSkills(true, false, false) })
		if *imports != 2 || *asked != 1 {
			t.Fatalf("imported %d, asked %d; want 2 and 1", *imports, *asked)
		}
		capture(t, func() { importSkills(true, false, false) })
		if *asked != 1 {
			t.Error("asked again after the change was imported")
		}
	})

	t.Run("no is remembered until the skills change again", func(t *testing.T) {
		home, imports, asked := stubSkills(t, true, false, true, nil)
		skill(t, home, ".claude/skills", "commit", "v1")
		capture(t, func() { importSkills(false, false, false) })
		skill(t, home, ".claude/skills", "commit", "v2")
		capture(t, func() { importSkills(true, false, false); importSkills(true, false, false) })
		if *imports != 1 || *asked != 1 {
			t.Fatalf("imported %d, asked %d; want 1 and 1", *imports, *asked)
		}
		skill(t, home, ".claude/skills", "commit", "v3")
		capture(t, func() { importSkills(true, false, false) })
		if *asked != 2 {
			t.Error("not asked after a further change")
		}
	})

	t.Run("a resume with no record counts as changed", func(t *testing.T) {
		_, imports, asked := stubSkills(t, true, true, true, nil)
		capture(t, func() { importSkills(true, false, false) })
		if *asked != 1 || *imports != 1 {
			t.Fatalf("asked %d, imported %d", *asked, *imports)
		}
	})

	t.Run("no terminal or no answer skips and remembers nothing", func(t *testing.T) {
		for _, tc := range []struct {
			name           string
			term, answered bool
		}{{"no terminal", false, true}, {"no answer", true, false}} {
			home, imports, _ := stubSkills(t, tc.term, true, tc.answered, nil)
			skill(t, home, ".claude/skills", "commit", "v1")
			capture(t, func() { importSkills(true, false, false) })
			if _, ok := readSkillsRecord(); *imports != 0 || ok {
				t.Errorf("%s: imported %d, recorded %v", tc.name, *imports, ok)
			}
		}
	})

	t.Run("--no-skills does nothing", func(t *testing.T) {
		_, imports, asked := stubSkills(t, true, true, true, nil)
		importSkills(false, true, false)
		importSkills(true, true, false)
		if *imports != 0 || *asked != 0 {
			t.Fatalf("imported %d, asked %d", *imports, *asked)
		}
	})

	t.Run("a failed import is a warning with sbx's output and is not recorded", func(t *testing.T) {
		_, imports, _ := stubSkills(t, true, true, true, errors.New("exit status 1"))
		out := capture(t, func() { importSkills(false, false, false) })
		if *imports != 1 {
			t.Fatalf("imported %d times", *imports)
		}
		if !strings.Contains(out, "WARNING") || !strings.Contains(out, "exit status 1") || !strings.Contains(out, "boom output") {
			t.Errorf("failure not reported: %q", out)
		}
		if _, ok := readSkillsRecord(); ok {
			t.Error("a failed import was recorded")
		}
	})

	t.Run("a successful import does not print sbx's output", func(t *testing.T) {
		stubSkills(t, true, true, true, nil)
		out := capture(t, func() { importSkills(false, false, false) })
		if strings.Contains(out, "boom output") || strings.Contains(out, "WARNING") {
			t.Errorf("noise on success: %q", out)
		}
	})

	t.Run("--dry-run shows the command and touches nothing", func(t *testing.T) {
		_, imports, asked := stubSkills(t, true, true, true, nil)
		out := capture(t, func() { importSkills(false, false, true); importSkills(true, false, true) })
		if _, ok := readSkillsRecord(); *imports != 0 || *asked != 0 || ok {
			t.Fatalf("imported %d, asked %d, recorded %v", *imports, *asked, ok)
		}
		if strings.Count(out, "[dry-run] sbx skills import --force") != 2 {
			t.Errorf("command not shown for both cases: %q", out)
		}
	})
}

func TestParseArgsNoSkills(t *testing.T) {
	o, err := parseArgs([]string{"--no-skills", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.noSkills || !o.dryRun || len(o.extra) != 0 {
		t.Fatalf("unexpected options: %+v", o)
	}
}
