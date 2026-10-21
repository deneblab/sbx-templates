package main

// Image paste (SBXT-020, GitHub #3): Claude Code pastes an image from the clipboard on ctrl+v
// (`chat:imagePaste`), but Windows Terminal takes ctrl+v as a text paste, so the key never reaches the
// sandbox. sbxup adds alt+v for the same action to the sandbox's ~/.claude/keybindings.json; ctrl+v stays.
//
// It cannot be baked into the image: sbx writes ~/.claude at sandbox start, and managed settings
// (/etc/claude-code) hold no keybindings. The clipboard bridge itself is sbx's (`sbx settings set
// clipboard.imagePaste true` on the host).
//
// The file is merged with jq (in every template), never overwritten: a user's own alt+v stays, null
// included, which is how a user unbinds a key on purpose. A convenience, so a failure is a warning.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// keybindingsFilter adds alt+v to the first Chat context (or a new one) unless any Chat context binds
// alt+v already. Its input is the file slurped (-s), so a missing or empty file reads as [].
const keybindingsFilter = `(.[0] // {})
| .["$schema"] //= "https://www.schemastore.org/claude-code-keybindings.json"
| .["$docs"] //= "https://code.claude.com/docs/en/keybindings"
| .bindings //= []
| if any(.bindings[]; .context == "Chat" and ((.bindings // {}) | has("alt+v"))) then .
  elif any(.bindings[]; .context == "Chat") then
    first(.bindings | to_entries[] | select(.value.context == "Chat") | .key) as $i
    | .bindings[$i].bindings = ((.bindings[$i].bindings // {}) + {"alt+v": "chat:imagePaste"})
  else .bindings += [{"context": "Chat", "bindings": {"alt+v": "chat:imagePaste"}}]
  end`

// keybindingsScript merges the binding into ~/.claude/keybindings.json through a temp file. A file jq
// cannot read (not JSON, not an object) makes the script fail and stays as it was; an unchanged result
// leaves the file untouched.
var keybindingsScript = `f="$HOME/.claude/keybindings.json"; mkdir -p "$HOME/.claude" && ` +
	`{ cat "$f" 2>/dev/null; true; } | jq -s '` + keybindingsFilter + `' > "$f.sbxup" ` +
	`|| { rm -f "$f.sbxup"; exit 1; }; ` +
	`if cmp -s "$f.sbxup" "$f"; then rm -f "$f.sbxup"; else mv "$f.sbxup" "$f"; fi`

// keybindingsRecord marks a sandbox whose keybindings sbxup has merged; its existence is the record.
type keybindingsRecord struct {
	Sandbox string `json:"sandbox"`
	Added   string `json:"added"`
}

func keybindingsRecordPath(sandbox string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "keybindings", strings.ToLower(sandbox)+".json"), nil
}

// applyKeybindings merges alt+v into a new sandbox, or into one sbxup has no record of (created by an
// older sbxup). A resume with a record costs no sbx call: the file lives in the sandbox's ~/.claude,
// which the restrict and hide notices also count on. fresh ignores a record left by an earlier sandbox
// of the same name.
func applyKeybindings(sandbox string, fresh, dryRun bool) {
	path, err := keybindingsRecordPath(sandbox)
	if err != nil {
		warnf("keybindings: %v", err)
		return
	}
	if !fresh {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	if dryRun {
		fmt.Printf("[dry-run] sbx exec %s sh -c …  # alt+v -> chat:imagePaste in ~/.claude/keybindings.json\n", sandbox)
		return
	}
	if err := sbxExecStdin(sandbox, keybindingsScript, ""); err != nil {
		warnf("cannot add the alt+v image-paste key: %v", err)
		return
	}
	data, _ := json.MarshalIndent(keybindingsRecord{Sandbox: sandbox, Added: "alt+v chat:imagePaste"}, "", "  ")
	if err := writeCache(path, data); err != nil {
		warnf("cannot record the keybindings (%v); sbxup will merge them again on the next start.", err)
	}
	fmt.Println("Keybindings: alt+v pastes an image (~/.claude/keybindings.json)")
}
