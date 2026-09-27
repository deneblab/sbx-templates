package main

// Importing the user's skills. sbx mounts one shared `agent-skills` store into every sandbox and
// fills it only on request, so sbxup asks it to: `sbx skills import` copies the skills installed for
// the coding agents on this machine into the store, and the store is a live mount, so a sandbox that
// is already running sees the result too.
//
// A new sandbox gets an import without a question. A resumed one gets a question, and only when the
// skills on the host changed since the last import: the checksum of the source directories is kept
// in the user cache.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// skillsImportArgs is the whole command. --force is needed because without it sbx asks before
// replacing every skill that is already installed; a re-import replaces a skill completely, so
// repeating it is safe.
var skillsImportArgs = []string{"skills", "import", "--force"}

// skillSources are the directories `sbx skills import` reads, in its order, relative to the home
// directory. The checksum covers the same set, so a change sbx would import is a change sbxup sees.
var skillSources = []string{
	".agents/skills",
	".claude/skills",
	".config/opencode/skills",
	".copilot/skills",
	".cursor/skills",
	".factory/skills",
}

// sbxSkillsImport and askSkills are variables so tests never call sbx and can answer the prompt.
var (
	sbxSkillsImport = func() (string, error) {
		out, err := exec.Command("sbx", skillsImportArgs...).CombinedOutput()
		return string(out), err
	}
	askSkills = func(question string) (bool, bool) { return askYesNo(os.Stdin, os.Stdout, question) }
)

// skillsChecksum hashes what `sbx skills import` would copy: every regular file inside each skill
// folder of each source directory, by path and content. It follows the importer's rules — a
// top-level symlink to a directory counts, loose top-level files and symlinks inside a skill do not —
// so a change sbx would ignore does not trigger a question. A missing source contributes nothing.
func skillsChecksum(home string) (string, error) {
	h := sha256.New()
	for _, src := range skillSources {
		root := filepath.Join(home, filepath.FromSlash(src))
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		for _, e := range entries {
			dir := filepath.Join(root, e.Name())
			info, err := os.Stat(dir) // follows a top-level symlink
			if err != nil || !info.IsDir() {
				continue
			}
			var files []string
			err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.Type().IsRegular() {
					files = append(files, p)
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			sort.Strings(files)
			for _, p := range files {
				rel, _ := filepath.Rel(root, p)
				fmt.Fprintf(h, "%s\x00%s\x00", src, filepath.ToSlash(rel))
				f, err := os.Open(p)
				if err != nil {
					return "", err
				}
				_, err = io.Copy(h, f)
				f.Close()
				if err != nil {
					return "", err
				}
				h.Write([]byte{0})
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// skillsRecord is what sbxup remembers about the skills: the checksum of the sources at the last
// import, or at the last "no" (so the same state is not asked about twice). It lives in the user
// cache, one per user, because the store is shared by every sandbox.
type skillsRecord struct {
	Checksum string    `json:"checksum"`
	Imported bool      `json:"imported"`
	At       time.Time `json:"at"`
}

func skillsRecordPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "skills-import.json"), nil
}

func readSkillsRecord() (skillsRecord, bool) {
	var rec skillsRecord
	path, err := skillsRecordPath()
	if err != nil {
		return rec, false
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &rec) != nil || rec.Checksum == "" {
		return rec, false
	}
	return rec, true
}

func writeSkillsRecord(sum string, imported bool) {
	path, err := skillsRecordPath()
	if err == nil {
		var data []byte
		if data, err = json.Marshal(skillsRecord{Checksum: sum, Imported: imported, At: now()}); err == nil {
			err = writeCache(path, data)
		}
	}
	if err != nil {
		warnf("cannot remember the skills checksum (%v); you may be asked again next time.", err)
	}
}

// importSkills keeps the shared store in step with the skills on the host. It never fails the
// start: `sbx skills import` is experimental and skills are not worth stopping a session for.
//
//   - creating a sandbox: import, no question.
//   - resuming one: nothing if the sources are unchanged since the last import (or the last no);
//     otherwise ask. Yes imports, no is remembered for this state of the sources. No terminal or no
//     answer skips the import and remembers nothing.
func importSkills(resuming, disabled, dryRun bool) {
	if disabled {
		return
	}
	cmdLine := "sbx " + strings.Join(skillsImportArgs, " ")

	sum := ""
	if home, err := os.UserHomeDir(); err != nil {
		warnf("cannot find your home directory to check the skills: %v", err)
	} else if sum, err = skillsChecksum(home); err != nil {
		warnf("cannot read the skills to check them for changes: %v", err)
		sum = ""
	}

	if resuming {
		if rec, ok := readSkillsRecord(); ok && sum != "" && rec.Checksum == sum {
			return
		}
		if dryRun {
			fmt.Println("[dry-run] your skills changed since the last import; would ask before running:")
			fmt.Printf("[dry-run] %s\n", cmdLine)
			return
		}
		fmt.Println("Your skills changed since the last import (~/.claude/skills and the other agents' skill directories).")
		if !isInteractive() {
			warnf("Not importing them: there is no terminal to ask on. Run '%s' or sbxup from a terminal.", cmdLine)
			return
		}
		yes, answered := askSkills("Import them into the store shared by all sandboxes? [y/N] ")
		if !answered {
			warnf("Not importing skills: no answer.")
			return
		}
		if !yes {
			if sum != "" {
				writeSkillsRecord(sum, false)
			}
			fmt.Println("Skills: not imported; you will be asked again when they change.")
			return
		}
	} else if dryRun {
		fmt.Printf("[dry-run] %s\n", cmdLine)
		return
	}

	out, err := sbxSkillsImport()
	if err != nil {
		warnf("%s failed: %v", cmdLine, err)
		if out = strings.TrimSpace(out); out != "" {
			fmt.Fprintln(os.Stderr, out)
		}
		return
	}
	if sum != "" {
		writeSkillsRecord(sum, true)
	}
	fmt.Printf("Skills: imported (%s)\n", cmdLine)
}
