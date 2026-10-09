package main

// Reading the GitHub App's key from Bitwarden through its CLI, `bw`. The vault has to be unlocked:
// with BW_SESSION in the environment nothing is asked; without it sbxup asks for the master password
// once, and an empty answer means "no GitHub token this time".
//
// The password goes to `bw unlock --passwordenv` through the child's environment only, never as an
// argument (visible in the process list), and the session `bw` returns lives in memory. When sbxup
// did the unlocking it locks the vault again once the key is read.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"golang.org/x/term"
)

// errNoToken marks a deliberate skip (an empty password), which is not worth a warning.
var errNoToken = errors.New("no GitHub token this time")

var (
	// bwRun runs `bw` with extra environment entries and returns its stdout. A variable so tests
	// never run the real CLI. --nointeraction keeps bw from prompting on a terminal sbxup owns.
	bwRun = func(env []string, args ...string) ([]byte, error) {
		cmd := exec.Command("bw", append(args, "--nointeraction")...)
		cmd.Env = append(os.Environ(), env...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return nil, errBwMissing
			}
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return out, errors.New(msg)
			}
		}
		return out, err
	}
	// readPassword asks without echo; a variable so tests can answer.
	readPassword = func(prompt string) (string, error) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
)

var errBwMissing = errors.New("the Bitwarden CLI 'bw' is not installed or not on PATH (https://bitwarden.com/help/cli/)")

// bwVault is an open view on the vault: the session to pass to every command, and whether sbxup
// unlocked it (and so must lock it again).
type bwVault struct {
	session  string
	unlocked bool
}

func (v *bwVault) env() []string {
	if v.session == "" {
		return nil
	}
	return []string{"BW_SESSION=" + v.session}
}

// openVault returns a usable session, asking for the master password when the vault is locked.
func openVault() (*bwVault, error) {
	v := &bwVault{session: os.Getenv("BW_SESSION")}
	out, err := bwRun(v.env(), "status")
	if err != nil {
		return nil, err
	}
	var st struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, fmt.Errorf("cannot read 'bw status': %w", err)
	}
	switch st.Status {
	case "unlocked":
		return v, nil
	case "unauthenticated":
		return nil, errors.New("the Bitwarden CLI is not logged in; run 'bw login' once")
	case "locked":
	default:
		return nil, fmt.Errorf("unexpected 'bw status': %q", st.Status)
	}

	if !isInteractive() {
		return nil, errors.New("Bitwarden is locked and there is no terminal to ask for the password; set BW_SESSION (bw unlock --raw)")
	}
	pw, err := readPassword("Bitwarden is locked. Master password (Enter = no GitHub token this time): ")
	if err != nil {
		return nil, fmt.Errorf("cannot read the password: %w", err)
	}
	if pw == "" {
		return nil, errNoToken
	}
	out, err = bwRun([]string{"BW_SBXUP_PASSWORD=" + pw}, "unlock", "--passwordenv", "BW_SBXUP_PASSWORD", "--raw")
	if err != nil {
		return nil, fmt.Errorf("cannot unlock Bitwarden: %w", err)
	}
	session := strings.TrimSpace(string(out))
	if session == "" {
		return nil, errors.New("cannot unlock Bitwarden: bw returned no session")
	}
	return &bwVault{session: session, unlocked: true}, nil
}

// close locks the vault if sbxup unlocked it, so the CLI is not left open behind it.
func (v *bwVault) close() {
	if !v.unlocked {
		return
	}
	if _, err := bwRun(v.env(), "lock"); err != nil {
		warnf("cannot lock Bitwarden again: %v", err)
	}
}

// appKey is what the Bitwarden item holds.
type appKey struct {
	issuer string // client ID, or app ID as a fallback
	pem    []byte
}

// readAppKey reads the issuer field and the private-key attachment of the configured item.
func readAppKey(v *bwVault, g *GitHubConfig) (*appKey, error) {
	out, err := bwRun(v.env(), "get", "item", g.BitwardenItem)
	if err != nil {
		return nil, fmt.Errorf("cannot read Bitwarden item %q: %w", g.BitwardenItem, err)
	}
	var item struct {
		ID     string `json:"id"`
		Fields []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"fields"`
		Attachments []struct {
			ID       string `json:"id"`
			FileName string `json:"fileName"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(out, &item); err != nil {
		return nil, fmt.Errorf("cannot parse Bitwarden item %q: %w", g.BitwardenItem, err)
	}

	field := func(name string) string {
		for _, f := range item.Fields {
			if strings.EqualFold(f.Name, name) {
				return strings.TrimSpace(f.Value)
			}
		}
		return ""
	}
	issuer := field(g.ClientIDField)
	if issuer == "" {
		issuer = field(g.AppIDField)
	}
	if issuer == "" {
		return nil, fmt.Errorf("Bitwarden item %q has neither a %q nor an %q field", g.BitwardenItem, g.ClientIDField, g.AppIDField)
	}

	var attID, names []string
	for _, a := range item.Attachments {
		names = append(names, a.FileName)
		if g.KeyAttachment != "" {
			if a.FileName == g.KeyAttachment || a.ID == g.KeyAttachment {
				attID = append(attID, a.ID)
			}
		} else if strings.EqualFold(path.Ext(a.FileName), ".pem") {
			attID = append(attID, a.ID)
		}
	}
	switch {
	case len(attID) == 0 && g.KeyAttachment != "":
		return nil, fmt.Errorf("Bitwarden item %q has no attachment %q (it has: %s)", g.BitwardenItem, g.KeyAttachment, listOrNone(names))
	case len(attID) == 0:
		return nil, fmt.Errorf("Bitwarden item %q has no .pem attachment (it has: %s)", g.BitwardenItem, listOrNone(names))
	case len(attID) > 1:
		return nil, fmt.Errorf("Bitwarden item %q has several matching attachments (%s); name one with 'key-attachment'", g.BitwardenItem, strings.Join(names, ", "))
	}

	pem, err := bwRun(v.env(), "get", "attachment", attID[0], "--itemid", item.ID, "--raw")
	if err != nil {
		return nil, fmt.Errorf("cannot read the private key from Bitwarden item %q: %w", g.BitwardenItem, err)
	}
	return &appKey{issuer: issuer, pem: pem}, nil
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
