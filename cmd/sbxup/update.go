package main

// Asking before a template is updated. A newer release can carry a newer version of a template
// than the one already built, and building it takes minutes and needs Docker. That is the user's
// call, not something to do silently once the cached "latest" expires: with an older version
// installed, sbxup keeps running it and asks.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// isInteractive and askUpdate are variables so tests can drive the prompt and the no-terminal path.
var (
	isInteractive = interactive
	askUpdate     = func(question string) (bool, bool) { return askYesNo(os.Stdin, os.Stdout, question) }
)

// parseVersion reads "0.2.8" as numeric parts. Anything else ("local", "latest") is not a
// version: it never counts as the newest one installed.
func parseVersion(s string) ([]int, bool) {
	if s == "" {
		return nil, false
	}
	parts := strings.Split(s, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		nums[i] = n
	}
	return nums, true
}

// compareVersions orders dotted numeric versions; a non-version sorts below every version.
func compareVersions(a, b string) int {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return -1
	case !bok:
		return 1
	}
	for i := 0; i < len(av) || i < len(bv); i++ {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// newestVersion returns the highest real version among tags, or "" if none is one.
func newestVersion(tags []string) string {
	best := ""
	for _, t := range tags {
		if _, ok := parseVersion(t); ok && (best == "" || compareVersions(t, best) > 0) {
			best = t
		}
	}
	return best
}

// askYesNo prints question and reads one line. Only y/yes says yes, so a bare Enter declines:
// starting a multi-minute build has to be a deliberate act. answered is false when the input ended
// before anything was typed (stdin from /dev/null, a closed pipe): that is no answer at all, not a
// "no", and must not be remembered as one.
func askYesNo(in io.Reader, out io.Writer, question string) (yes, answered bool) {
	fmt.Fprint(out, question)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(out)
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, true
	}
	return false, true
}

// declineUpdate remembers, in the resolved-latest record, that the user said no to tag. The
// timestamp is left alone so the re-check still happens at the usual time.
func declineUpdate(ref releaseRef, tag string) error {
	rec, ok := readLatestRecord(ref)
	if !ok {
		return fmt.Errorf("no resolved-release record to note it in")
	}
	for _, d := range rec.Declined {
		if d == tag {
			return nil
		}
	}
	rec.Declined = append(rec.Declined, tag)
	path, err := latestRecordPath(ref)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return writeCache(path, data)
}

// offerUpdate decides what to do when the release carries a template version that is not built
// yet. It returns the tag to run and true when nothing needs building now; false means "build".
//
// Nothing is offered on a first start (there is no older version to fall back to), and the caller
// never gets here for a pinned release: that is exactly what the config asked for.
func offerUpdate(entry *TemplateEntry, ref releaseRef, sandbox string, o *options) (string, bool) {
	repo := entry.LocalRepo(ref)
	tags := sbxTemplateTags(repo)
	for _, t := range tags {
		if t == entry.Version {
			return "", false // already built; the caller's own check handles it
		}
	}
	newest := newestVersion(tags)
	if newest == "" {
		return "", false // first start: build without asking
	}
	current := repo + ":" + newest

	// The release is not ahead of what is installed (another project already built a newer one):
	// run that, there is nothing to update to.
	if compareVersions(entry.Version, newest) <= 0 {
		fmt.Printf("Reusing template: %s\n", current)
		return current, true
	}

	newTag := entry.LocalTag(ref)
	fmt.Printf("New version of template %s is available: %s (using %s)\n", entry.Short, entry.Version, newest)
	fmt.Printf("  %s/tag/%s\n", ref.webURL(), ref.Tag)

	keep := func(why string) (string, bool) {
		fmt.Printf("Using %s. %s\n", current, why)
		return current, true
	}
	if o.dryRun {
		return keep("[dry-run] would ask whether to build the new version first.")
	}
	if rec, ok := readLatestRecord(ref); ok {
		for _, d := range rec.Declined {
			if d == newTag {
				return keep("Run 'sbxup --update' to build the new version.")
			}
		}
	}
	if !isInteractive() {
		return keep("Run 'sbxup --update' to build the new version.")
	}
	if !dockerAvailable() {
		return keep("Building it needs Docker, which is not reachable; start Docker Desktop and run 'sbxup --update'.")
	}

	if sandbox != "" {
		fmt.Printf("Sandbox %s keeps the image it was created with; the new version applies to a new sandbox.\n", sandbox)
	}
	yes, answered := askUpdate("Building takes a few minutes and needs Docker. Update? [y/N] ")
	if yes {
		return "", false
	}
	if !answered {
		return keep("No answer was given. Run 'sbxup --update' to build the new version.")
	}
	if err := declineUpdate(ref, newTag); err != nil {
		warnf("cannot remember the answer (%v); you will be asked again next time.", err)
	}
	return keep("Asking again after the next release check, or run 'sbxup --update' to build it.")
}
