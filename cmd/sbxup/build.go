package main

// Building a template locally, so a sandbox can start without pulling a published image.
// The build-arg contract matches scripts/build/build-push.sh --no-push exactly, so an image
// built here and one built from a clone carry the same OCI labels.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// buildArgs assembles the `docker buildx build` argument list. Kept pure so tests can assert
// the exact command line against the shell script's, which is the anti-drift criterion.
func buildArgs(dockerfile, context, tag, version, shortSHA, buildDate string, updateClaude bool) []string {
	args := []string{"buildx", "build", "--load"}
	if updateClaude {
		// Re-run only the `claude` stage; the deps stage (runtimes, apt) stays cached.
		args = append(args, "--no-cache-filter", "claude")
	} else {
		// Re-check the floating base image, so a rebuild picks up a new one. Not with
		// updateClaude: a moved base would rebuild the deps stage as well.
		args = append(args, "--pull")
	}
	args = append(args,
		"-f", dockerfile,
		"-t", tag,
		"--build-arg", "VERSION="+version,
		"--build-arg", "SHORT_SHA="+shortSHA,
		"--build-arg", "BUILD_DATE="+buildDate,
		context,
	)
	return args
}

// saveArgs / loadArgs are the fallback route into the sandbox runtime's image store.
func saveArgs(tag, tarPath string) []string {
	return []string{"image", "save", tag, "-o", tarPath}
}

func loadArgs(tarPath string) []string {
	return []string{"template", "load", tarPath}
}

// runTool executes a command with stdio attached so build output streams to the user.
func runTool(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// dockerImageExists reports whether the tag is already in the local Docker daemon. It needs a
// running daemon to answer, so it must never be the first question asked — see buildTemplate.
var dockerImageExists = func(tag string) bool {
	return exec.Command("docker", "image", "inspect", tag).Run() == nil
}

// dockerAvailable reports whether a Docker daemon is reachable. Checked before a build so the
// user gets an actionable message instead of a raw npipe/socket error from the builder.
var dockerAvailable = func() bool {
	return exec.Command("docker", "version", "--format", "{{.Server.Version}}").Run() == nil
}

// errDockerUnavailable explains the one situation that genuinely needs Docker running.
var errDockerUnavailable = fmt.Errorf(
	"Docker daemon not reachable — start Docker Desktop and retry.\n" +
		"Docker is needed only to build or update a template; running an already-built one is not affected")

// canonicalRef normalises an image reference the way a registry client would, so that the
// short tag we build (`sbx-claude-dotnet10:0.1.3`) compares equal to the fully qualified
// repository `sbx template ls` prints for it (`docker.io/library/sbx-claude-dotnet10`).
func canonicalRef(repo, tag string) string {
	if tag == "" {
		// A digest or an embedded tag: split on the last colon that is not inside a host:port.
		if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
			repo, tag = repo[:i], repo[i+1:]
		} else {
			tag = "latest"
		}
	}
	switch parts := strings.Split(repo, "/"); {
	case len(parts) == 1:
		// Bare name: Docker Hub's official-images namespace.
		repo = "docker.io/library/" + repo
	case !strings.ContainsAny(parts[0], ".:") && parts[0] != "localhost":
		// First component is a Hub user, not a registry host.
		repo = "docker.io/" + repo
	}
	return repo + ":" + tag
}

// templateListedIn reports whether `sbx template ls` table output contains tag. A substring match
// over the raw output silently never matches: REPOSITORY and TAG are separate columns, so the
// `name:tag` form we look for never appears in the text.
func templateListedIn(out, tag string) bool {
	want := canonicalRef(tag, "")
	for _, r := range templateTableRefs(out) {
		if r == want {
			return true
		}
	}
	return false
}

// templateTagsIn returns the tags the `sbx template ls` table lists for one repository, in listing
// order. Repositories are compared in canonical form, so `docker.io/library/sbx-claude-dotnet10`
// matches the bare name the local tag uses — while `docker.io/pkudrel/sbx-claude-dotnet10`, the
// frozen Docker Hub image, correctly does not.
func templateTagsIn(out, repo string) []string {
	return tagsOf(templateTableRefs(out), repo)
}

// templateRefsJSON reads `sbx template ls --json` into canonical references. The shape is an array
// since sbx 0.48 and `{"images": [...]}` before; the field names are not documented, so each image
// is read from whichever of the usual forms it carries: repository + tag, a name:tag reference, or a
// list of them. An image list in which none of these is found is an error, not "nothing built":
// that is what turns a format change into a warning instead of a rebuild on every start.
func templateRefsJSON(data []byte) ([]string, error) {
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		var wrapped struct {
			Images *[]map[string]any `json:"images"`
		}
		if json.Unmarshal(data, &wrapped) != nil || wrapped.Images == nil {
			return nil, errors.New("not a list of images")
		}
		rows = *wrapped.Images
	}
	var refs []string
	for _, row := range rows {
		f := map[string]any{}
		for k, v := range row {
			f[strings.ToLower(k)] = v
		}
		str := func(keys ...string) string {
			for _, k := range keys {
				if v, ok := f[k].(string); ok && v != "" {
					return v
				}
			}
			return ""
		}
		repo, tag := str("repository", "repo"), str("tag")
		switch {
		case repo != "" && tag != "":
			refs = append(refs, canonicalRef(repo, tag))
		case str("reference", "ref", "image", "name") != "":
			refs = append(refs, canonicalRef(str("reference", "ref", "image", "name"), ""))
		default:
			for _, k := range []string{"tags", "repo_tags", "repotags"} {
				list, _ := f[k].([]any)
				for _, item := range list {
					if r, ok := item.(string); ok && r != "" {
						refs = append(refs, canonicalRef(r, ""))
					}
				}
			}
		}
	}
	if len(refs) == 0 && len(rows) > 0 {
		return nil, fmt.Errorf("none of its %d images names a repository and tag", len(rows))
	}
	return refs, nil
}

// templateJSONWarned keeps the format warning to one per run.
var templateJSONWarned bool

// sbxTemplateRefs lists what the sandbox runtime holds, as canonical references: from
// `sbx template ls --json` when it can be read, otherwise from the table, which is all an sbx
// without --json offers. ok is false when sbx gave no answer at all.
var sbxTemplateRefs = func() (refs []string, ok bool) {
	if out, err := sbxOutput("template", "ls", "--json"); err == nil {
		refs, perr := templateRefsJSON(out)
		if perr == nil {
			return refs, true
		}
		if !templateJSONWarned {
			templateJSONWarned = true
			warnf("cannot read 'sbx template ls --json' (%v); reading its table instead. Please report this with the output of 'sbx version'.", perr)
		}
	}
	out, err := sbxOutput("template", "ls")
	if err != nil && len(out) == 0 {
		return nil, false
	}
	return templateTableRefs(string(out)), true
}

// templateTableRefs reads the REPOSITORY and TAG columns of the `sbx template ls` table.
func templateTableRefs(out string) []string {
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "REPOSITORY" {
			continue
		}
		refs = append(refs, canonicalRef(fields[0], fields[1]))
	}
	return refs
}

// tagsOf returns the tags refs hold for one repository, in listing order and without repeats.
func tagsOf(refs []string, repo string) []string {
	want := strings.TrimSuffix(canonicalRef(repo, "x"), ":x") + ":"
	var tags []string
	seen := map[string]bool{}
	for _, r := range refs {
		if tag, ok := strings.CutPrefix(r, want); ok && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

// sbxTemplateTags lists the versions of a template the sandbox runtime already has. Like
// sbxTemplateListed it needs no Docker daemon, which is what lets an update be *offered* without
// Docker running.
var sbxTemplateTags = func(repo string) []string {
	refs, _ := sbxTemplateRefs()
	return tagsOf(refs, repo)
}

// sbxTemplateListed reports whether `sbx` can already see the tag as a template.
//
// The sandbox runtime keeps its own image store, separate from the host Docker daemon: `sbx`
// runs sandboxes with Docker Desktop closed, while `docker build` cannot. So this is the
// authoritative "is it ready to run?" check, and — unlike dockerImageExists — it answers
// without a Docker daemon. Everything else keys off it.
var sbxTemplateListed = func(tag string) bool {
	refs, _ := sbxTemplateRefs()
	want := canonicalRef(tag, "")
	for _, r := range refs {
		if r == want {
			return true
		}
	}
	return false
}

// ensureTemplate makes tag usable as `sbx run --template tag`, importing it only if needed.
func ensureTemplate(tag string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] sbx template ls  # import via 'docker image save' + 'sbx template load' if %s is absent\n", tag)
		return nil
	}
	if sbxTemplateListed(tag) {
		return nil
	}

	fmt.Printf("Importing %s into the sandbox runtime...\n", tag)
	tar, err := os.CreateTemp("", "sbxup-template-*.tar")
	if err != nil {
		return fmt.Errorf("cannot create a temp file for the image export: %w", err)
	}
	tarPath := tar.Name()
	tar.Close()
	defer os.Remove(tarPath)

	if err := runTool("docker", saveArgs(tag, tarPath)...); err != nil {
		return fmt.Errorf("docker image save failed for %s: %w", tag, err)
	}
	if err := runTool("sbx", loadArgs(tarPath)...); err != nil {
		return fmt.Errorf("sbx template load failed for %s: %w", tag, err)
	}
	return nil
}

// buildTemplate builds a template's Dockerfile into a local image and registers it with the
// sandbox runtime. It returns the tag to hand to `sbx run --template`.
//
// force rebuilds even when the tag already exists; updateClaude refreshes only the Claude
// Code stage. Both are no-ops when the image is present and neither is set.
func buildTemplate(dockerfile string, t *TemplateEntry, ref releaseRef, force, updateClaude, dryRun bool) (string, error) {
	tag := t.LocalTag(ref)

	if !force && !updateClaude && !dryRun {
		// Ask `sbx` first: it answers without a Docker daemon, and a template already in its
		// store is ready to run. Asking Docker first would make a stopped Docker Desktop look
		// like "never built" and trigger a doomed rebuild.
		if sbxTemplateListed(tag) {
			fmt.Printf("Reusing template: %s\n", tag)
			return tag, nil
		}
		// Built earlier but not imported into the sandbox runtime: importing needs Docker,
		// because the image has to be exported from the daemon that holds it.
		if dockerAvailable() && dockerImageExists(tag) {
			fmt.Printf("Reusing local image: %s\n", tag)
			return tag, ensureTemplate(tag, dryRun)
		}
	}

	if !dryRun && !dockerAvailable() {
		return "", errDockerUnavailable
	}

	// The Dockerfile's own directory, extracted from the release tarball as src/<name>/ — the
	// same context `build-push.sh` passes locally. It holds only that template's Dockerfile and
	// template.yaml, so the context stays tiny; these templates never COPY from it anyway.
	context := filepath.Dir(dockerfile)
	args := buildArgs(dockerfile, context, tag, t.Version, ref.Tag, time.Now().UTC().Format("2006-01-02T15:04:05Z"), updateClaude)

	if dryRun {
		fmt.Printf("[dry-run] docker %s\n", strings.Join(args, " "))
		return tag, ensureTemplate(tag, dryRun)
	}

	fmt.Printf("Building %s from %s\n", tag, dockerfile)
	if err := runTool("docker", args...); err != nil {
		return "", fmt.Errorf("docker build failed for %s: %w", tag, err)
	}
	if err := ensureTemplate(tag, dryRun); err != nil {
		return "", err
	}
	fmt.Printf("Built: %s\n", tag)
	return tag, nil
}
