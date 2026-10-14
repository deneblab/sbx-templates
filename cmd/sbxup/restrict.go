package main

// Repository restrictions (`restrict:` in the config): HTTP deny rules in the sandbox's sbx policy, so
// the sandbox proxy refuses a git push or a delete on GitHub. Checked on a host with sbx 0.48:
//   - path rules match case-sensitively, so a rule naming one repository is bypassed by changing the
//     case of the URL. Only rules with a wildcard for owner and repository are offered ("no push at all");
//   - "push only to X" cannot be written (no negation, deny beats allow, the default is allow-all). That
//     is the token's job: an installation token narrowed with `github.repositories`;
//   - adding a rule that exists is a no-op ("Already covered"), so rules are simply added on every start.
//
// These are guardrails, not a boundary: GraphQL (createCommitOnBranch) and SSH stay open. Deny rules only
// narrow what a sandbox may do, so a repository may ask for them without the user's approval. A rule that
// cannot be applied stops the start, since running without it is the unsafe direction.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// RestrictConfig is the `restrict:` block.
type RestrictConfig struct {
	NoPush   bool // no git push over HTTPS, no GitHub API writes that create commits
	NoDelete bool // no DELETE under api.github.com/repos/**
}

// policyRule is one HTTP deny rule: one host, one or more methods, one path (sbx takes one per rule).
type policyRule struct {
	Host    string   `json:"host"`
	Methods []string `json:"methods"`
	Path    string   `json:"path"`
}

func (r policyRule) key() string {
	m := append([]string(nil), r.Methods...)
	for i := range m {
		m[i] = strings.ToUpper(m[i])
	}
	sort.Strings(m)
	return r.Host + "|" + strings.Join(m, ",") + "|" + r.Path
}

func (r policyRule) String() string {
	return strings.Join(r.Methods, ",") + " " + r.Host + r.Path
}

// restrictRules is the fixed rule set for the config. Every owner and repository segment is a wildcard,
// which matches any case (host check E).
func restrictRules(c *RestrictConfig) []policyRule {
	if c == nil {
		return nil
	}
	var rules []policyRule
	if c.NoPush {
		rules = append(rules,
			policyRule{"github.com", []string{"POST"}, "/*/*/git-receive-pack"}, // also covers repo.git
			policyRule{"api.github.com", []string{"PUT"}, "/repos/*/*/contents/**"},
			policyRule{"api.github.com", []string{"POST", "PATCH"}, "/repos/*/*/git/**"},
			policyRule{"api.github.com", []string{"PUT"}, "/repos/*/*/pulls/*/merge"},
			policyRule{"api.github.com", []string{"POST"}, "/repos/*/*/merges"},
			policyRule{"api.github.com", []string{"POST"}, "/repos/*/*/merge-upstream"},
		)
	}
	if c.NoDelete {
		rules = append(rules, policyRule{"api.github.com", []string{"DELETE"}, "/repos/**"})
	}
	return rules
}

// restrictSummary is the one-line description: "no push, no delete".
func restrictSummary(c *RestrictConfig) string {
	var parts []string
	if c != nil && c.NoPush {
		parts = append(parts, "no push")
	}
	if c != nil && c.NoDelete {
		parts = append(parts, "no delete")
	}
	return strings.Join(parts, ", ")
}

var (
	// sbxPolicyDeny adds one HTTP deny rule scoped to the sandbox.
	sbxPolicyDeny = func(sandbox string, r policyRule) error {
		args := []string{"policy", "deny", "network", "--sandbox", sandbox, r.Host,
			"--method", strings.Join(r.Methods, ","), "--path", r.Path}
		out, err := exec.Command("sbx", args...).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "Already covered") {
			return fmt.Errorf("sbx policy deny network %s: %v %s", r, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// sbxPolicyRm removes one. -f is required: since sbx 0.48 a confirmation needs a terminal.
	sbxPolicyRm = func(sandbox string, r policyRule) error {
		args := []string{"policy", "rm", "network", "-f", "--sandbox", sandbox, "--resource", r.Host,
			"--method", strings.Join(r.Methods, ","), "--path", r.Path}
		out, err := exec.Command("sbx", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("sbx policy rm network %s: %v %s", r, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// sbxExecStdin runs a shell script in the sandbox with stdin attached.
	sbxExecStdin = func(sandbox, script, stdin string) error {
		cmd := exec.Command("sbx", "exec", sandbox, "sh", "-c", script)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("sbx exec %s: %v %s", sandbox, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

// sandboxDenyRules reads the HTTP deny rules scoped to sandbox from `sbx policy ls SANDBOX --json`
// (sbx 0.48: `{"rules": [...]}` with `resource_type`, `decision`, `scope`, `methods`, `http_targets`).
// Global and kit rules are in the same list and are skipped.
func sandboxDenyRules(data []byte, sandbox string) ([]policyRule, error) {
	var list struct {
		Rules *[]struct {
			ResourceType string   `json:"resource_type"`
			Decision     string   `json:"decision"`
			Scope        string   `json:"scope"`
			Methods      []string `json:"methods"`
			Targets      []struct {
				Host string `json:"host"`
				Path string `json:"path"`
			} `json:"http_targets"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &list); err != nil || list.Rules == nil {
		return nil, errors.New("not a list of policy rules")
	}
	var out []policyRule
	for _, r := range *list.Rules {
		if r.ResourceType != "http" || r.Decision != "deny" || !strings.EqualFold(r.Scope, "sandbox:"+sandbox) {
			continue
		}
		for _, t := range r.Targets {
			out = append(out, policyRule{Host: t.Host, Methods: r.Methods, Path: t.Path})
		}
	}
	return out, nil
}

// restrictRecord is what sbxup added to one sandbox, so it removes only its own rules, never one the
// user added by hand. Kept in the user cache, like the other per-user records.
type restrictRecord struct {
	Sandbox string       `json:"sandbox"`
	Rules   []policyRule `json:"rules"`
	Notice  string       `json:"notice,omitempty"` // the block last written for the agent
}

func restrictRecordPath(sandbox string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a cache directory: %w", err)
	}
	return filepath.Join(base, "sbxup", "restrict", strings.ToLower(sandbox)+".json"), nil
}

func loadRestrictRecord(sandbox string) restrictRecord {
	rec := restrictRecord{Sandbox: sandbox}
	path, err := restrictRecordPath(sandbox)
	if err != nil {
		return rec
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &rec)
	}
	return rec
}

func saveRestrictRecord(rec restrictRecord) error {
	path, err := restrictRecordPath(rec.Sandbox)
	if err != nil {
		return err
	}
	if len(rec.Rules) == 0 && rec.Notice == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return writeCache(path, data)
}

// Markers of sbxup's block in the agent's user instructions inside the sandbox.
const (
	noticeStart = "<!-- sbxup:restrict -->"
	noticeEnd   = "<!-- /sbxup:restrict -->"
)

// noticeScript replaces sbxup's block in ~/.claude/CLAUDE.md with stdin (an empty stdin removes it).
// Replacing, not appending: appending twice left the text there twice on a host.
const noticeScript = `f="$HOME/.claude/CLAUDE.md"; mkdir -p "$HOME/.claude" && touch "$f" && ` +
	`awk -v s='` + noticeStart + `' -v e='` + noticeEnd + `' '$0==s{skip=1} !skip{print} $0==e{skip=0}' "$f" > "$f.sbxup" && ` +
	`cat >> "$f.sbxup" && mv "$f.sbxup" "$f"`

// restrictNotice tells the agent why a push or a delete fails: git hides the proxy's reason and shows
// only "RPC failed; HTTP 403".
func restrictNotice(c *RestrictConfig) string {
	if restrictSummary(c) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(noticeStart + "\n")
	b.WriteString("## Sandbox restrictions (set by sbxup from .sbx/sbxup.config.yaml)\n\n")
	if c.NoPush {
		b.WriteString("- No push: the sandbox proxy blocks `git push` over HTTPS and GitHub API calls that create commits. " +
			"A push fails with \"RPC failed; HTTP 403\". This is intended: do not retry it or look for another way to " +
			"publish the commits; keep them local and tell the user.\n")
	}
	if c.NoDelete {
		b.WriteString("- No delete: DELETE requests to api.github.com/repos/** are blocked (\"Blocked by local rule\"). " +
			"This is intended; tell the user instead of working around it.\n")
	}
	b.WriteString(noticeEnd + "\n")
	return b.String()
}

// applyRestrictions brings the sandbox's policy in line with the config: it adds the missing rules,
// removes the ones sbxup added earlier that the config no longer asks for, and keeps the agent's notice
// in step. A project without `restrict:` and without a record costs no sbx call at all. fresh is set for
// a sandbox sbxup has just created: a record left by an earlier sandbox of the same name does not count.
func applyRestrictions(sandbox string, c *RestrictConfig, fresh, dryRun bool) error {
	want := restrictRules(c)
	rec := loadRestrictRecord(sandbox)
	if fresh {
		rec = restrictRecord{Sandbox: sandbox}
	}
	if len(want) == 0 && len(rec.Rules) == 0 && rec.Notice == "" {
		return nil
	}
	if dryRun {
		for _, r := range want {
			fmt.Printf("[dry-run] sbx policy deny network --sandbox %s %s --method %s --path '%s'\n",
				sandbox, r.Host, strings.Join(r.Methods, ","), r.Path)
		}
		return nil
	}

	have := map[string]bool{}
	if out, err := sbxOutput("policy", "ls", sandbox, "--json"); err == nil {
		if rules, err := sandboxDenyRules(out, sandbox); err == nil {
			for _, r := range rules {
				have[r.key()] = true
			}
		}
	}
	owned := map[string]bool{}
	for _, r := range rec.Rules {
		owned[r.key()] = true
	}
	// mine is what sbxup owns afterwards: rules it added now or earlier, never a rule that was already
	// there (the user's), so dropping it from the config cannot remove the user's own rule.
	var mine []policyRule
	wanted := map[string]bool{}
	for _, r := range want {
		wanted[r.key()] = true
		if have[r.key()] {
			if owned[r.key()] {
				mine = append(mine, r)
			}
			continue
		}
		if err := sbxPolicyDeny(sandbox, r); err != nil {
			return fmt.Errorf("cannot apply the restriction (%v); not starting the sandbox without it", err)
		}
		mine = append(mine, r)
	}
	for _, r := range rec.Rules {
		if wanted[r.key()] {
			continue
		}
		// Left in place, a rule only restricts more, so a failure here is a warning.
		if err := sbxPolicyRm(sandbox, r); err != nil {
			warnf("cannot remove the restriction %s: %v", r, err)
		}
	}

	// Written only when it changes: an exec per start would slow every resume down.
	if notice := restrictNotice(c); notice != rec.Notice {
		if err := sbxExecStdin(sandbox, noticeScript, notice); err != nil {
			warnf("cannot tell the agent about the restrictions: %v", err)
		} else {
			rec.Notice = notice
		}
	}
	rec.Rules = mine
	if err := saveRestrictRecord(rec); err != nil {
		warnf("cannot record the restrictions (%v); they stay in place, but sbxup will not remove them if the config drops them.", err)
	}
	if s := restrictSummary(c); s != "" {
		fmt.Printf("Restrictions: %s (sandbox policy of %s)\n", s, sandbox)
	}
	return nil
}
