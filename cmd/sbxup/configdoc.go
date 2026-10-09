package main

// The commented config that `--init` writes. Every key sbxup reads is listed here once, with an
// example and a comment, so the generated file documents itself; a test fails when a `Config` field
// has no entry. Only `template:` (and `version:` for a fork) is live: everything else is a comment,
// because a written-down default would stop following sbxup when the default changes.

import (
	"fmt"
	"strings"
)

// configDoc is one optional key as `--init` shows it. Lines is the example exactly as it would be
// uncommented, with its end-of-line comment; Para is a paragraph above it, for keys whose meaning
// does not fit on a line.
type configDoc struct {
	Key   string
	Para  []string
	Lines []string
}

// configDocs follows `template` and `version`, which renderConfig writes itself because their values
// come from the release.
var configDocs = []configDoc{
	{Key: "agent", Lines: []string{
		"agent: claude      # agent passed to 'sbx run' (default: claude)",
	}},
	{Key: "clone", Lines: []string{
		"clone: true        # run on a private in-container git clone (default: off)",
	}},
	{Key: "cache", Lines: []string{
		"cache: .sbx-cache  # project directory mounted into the sandbox for package caches",
	}},
	{Key: "mounts",
		Para: []string{
			"Extra host directories mounted into the sandbox; append :ro for read-only.",
			"Directories outside the project are approved once; your home directory and",
			"credential directories such as ~/.ssh are refused.",
		},
		Lines: []string{
			"mounts:",
			"  - ../shared-libs:ro",
		}},
	{Key: "github",
		Para: []string{
			"Short-lived GitHub App token for the repo in 'origin', renewed while 'sbx run' lasts.",
			"The app's private key is read from Bitwarden on the host ('bw'); only the token",
			"reaches the sandbox. The first use per project asks for your approval.",
		},
		Lines: []string{
			"github:",
			"  bitwarden-item: github-deneblab-agent  # item with a .pem attachment and a client-id field",
			"  key-attachment: app.private-key.pem    # default: the item's only .pem attachment",
			"  client-id-field: client-id             # default: client-id",
			"  app-id-field: app-id                   # used when the client-id field is missing",
			"  repositories: [docs]                   # more repos of the same owner as 'origin'",
			"  permissions:                           # default: contents, pull_requests, issues: write",
			"    contents: write",
			"    pull_requests: write",
		}},
}

// renderConfig writes the config body. templateLine is the live `template:` line; liveVersion, when
// set, is a live `version:` value (a fork must name its source); pinExample is the release offered as
// a commented pin, and "latest" when there is none, which is valid when uncommented.
func renderConfig(templateLine, liveVersion, pinExample string) string {
	var b strings.Builder
	b.WriteString(templateLine + "\n")
	b.WriteString("\n")
	if liveVersion != "" {
		// A commented pin as well would be a second `version:` once uncommented, which YAML rejects;
		// the comment says how to pin this one instead.
		fmt.Fprintf(&b, "version: %s   # this template comes from a fork; replace 'latest' with a release to pin it\n", liveVersion)
	} else {
		if pinExample == "" {
			pinExample = "latest"
		}
		// Padded to the comment column of the keys below, whatever the length of the release.
		fmt.Fprintf(&b, "# %-18s # pin the release to freeze the environment; without it sbxup follows the latest\n", "version: "+pinExample)
	}
	for _, d := range configDocs {
		if len(d.Para) > 0 {
			b.WriteString("\n")
			for _, p := range d.Para {
				b.WriteString("# " + p + "\n")
			}
		}
		for _, l := range d.Lines {
			b.WriteString("# " + l + "\n")
		}
	}
	return b.String()
}
