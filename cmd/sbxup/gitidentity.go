package main

// Git author (SBXT-025, GitHub #8): a new sandbox has no git identity (user.name and user.email are
// empty), so `git commit` fails, and Claude will not set git config on its own. sbxup copies the host's
// identity, as git on the host would commit from this project (repository config over global), into the
// sandbox's global config.
//
// --global, not the repository: in direct mode .git/config is the host's own file, so writing there would
// change the host repository. It cannot be baked into the image either: the identity is the user's, and
// one image serves every user. Not a config key, for the same reason.
//
// Only missing keys are set: an identity already in the sandbox (set by hand, or by an earlier start)
// stays. A convenience, so every failure is a warning.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitIdentity is the seam for hostGitIdentity, so tests need no git.
var gitIdentity = hostGitIdentity

// hostGitIdentity returns user.name and user.email as git resolves them in dir. An unset key is "", not
// an error.
func hostGitIdentity(dir string) (name, email string, err error) {
	get := func(key string) (string, error) {
		out, err := exec.Command("git", "-C", dir, "config", "--get", key).Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil // the key is not set
		}
		if err != nil {
			return "", fmt.Errorf("git config --get %s: %w", key, err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	if name, err = get("user.name"); err != nil {
		return "", "", err
	}
	if email, err = get("user.email"); err != nil {
		return "", "", err
	}
	return name, email, nil
}

// gitIdentityScript reads the name and the email from stdin, one line each, so no user text is quoted
// into the command line, and sets each one in ~/.gitconfig only when the sandbox has none.
const gitIdentityScript = `IFS= read -r name; IFS= read -r email; ` +
	`[ -n "$(git config --global --get user.name)" ] || git config --global user.name "$name" || exit 1; ` +
	`[ -n "$(git config --global --get user.email)" ] || git config --global user.email "$email" || exit 1`

// gitIdentityRecord is the identity sbxup last handed to a sandbox; a resume with the same one costs no
// sbx call.
type gitIdentityRecord struct {
	Sandbox string `json:"sandbox"`
	Name    string `json:"name"`
	Email   string `json:"email"`
}

func gitIdentityRecordPath(sandbox string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "git-identity", strings.ToLower(sandbox)+".json"), nil
}

// applyGitIdentity gives the sandbox the host's git author. It runs on a new sandbox, on one sbxup has no
// record of, and when the host's identity changed since the record; fresh ignores a record left by an
// earlier sandbox of the same name.
func applyGitIdentity(sandbox, project string, fresh, dryRun bool) {
	name, email, err := gitIdentity(project)
	if err != nil {
		warnf("git author: %v", err)
		return
	}
	if name == "" || email == "" || strings.ContainsAny(name+email, "\r\n") {
		warnf("git has no author on this host (user.name and user.email), so commits in the sandbox will fail; " +
			"set them with `git config --global user.name \"...\"` and `git config --global user.email ...`.")
		return
	}
	want := gitIdentityRecord{Sandbox: sandbox, Name: name, Email: email}
	path, err := gitIdentityRecordPath(sandbox)
	if err != nil {
		warnf("git author: %v", err)
		return
	}
	if !fresh {
		var have gitIdentityRecord
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &have) == nil && have == want {
			return
		}
	}
	if dryRun {
		fmt.Printf("[dry-run] sbx exec %s sh -c …  # git author %s <%s> in ~/.gitconfig, if unset\n", sandbox, name, email)
		return
	}
	if err := sbxExecStdin(sandbox, gitIdentityScript, name+"\n"+email+"\n"); err != nil {
		warnf("cannot set the git author in the sandbox: %v", err)
		return
	}
	data, _ := json.MarshalIndent(want, "", "  ")
	if err := writeCache(path, data); err != nil {
		warnf("cannot record the git author (%v); sbxup will set it again on the next start.", err)
	}
	fmt.Printf("Git author: %s <%s> (~/.gitconfig in the sandbox, unless already set)\n", name, email)
}
