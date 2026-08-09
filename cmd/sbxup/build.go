package main

// Building a template locally, so a sandbox can start without pulling a published image.
// The build-arg contract matches scripts/build/build-push.sh --no-push exactly, so an image
// built here and one built from a clone carry the same OCI labels.

import (
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

// templateListedIn reports whether `sbx template ls` output contains tag. Split out from the
// exec wrapper so the table parsing is testable — and because a substring match over the raw
// output silently never matches: REPOSITORY and TAG are separate columns, so the `name:tag`
// form we look for never appears in the text.
func templateListedIn(out, tag string) bool {
	want := canonicalRef(tag, "")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "REPOSITORY" {
			continue
		}
		if canonicalRef(fields[0], fields[1]) == want {
			return true
		}
	}
	return false
}

// sbxTemplateListed reports whether `sbx` can already see the tag as a template.
//
// The sandbox runtime keeps its own image store, separate from the host Docker daemon: `sbx`
// runs sandboxes with Docker Desktop closed, while `docker build` cannot. So this is the
// authoritative "is it ready to run?" check, and — unlike dockerImageExists — it answers
// without a Docker daemon. Everything else keys off it.
var sbxTemplateListed = func(tag string) bool {
	out, err := exec.Command("sbx", "template", "ls").CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return templateListedIn(string(out), tag)
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
func buildTemplate(dockerfile string, t *TemplateEntry, release string, force, updateClaude, dryRun bool) (string, error) {
	tag := t.LocalTag()

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

	// The cache directory holds only Dockerfiles, which makes it a deliberately minimal build
	// context: these templates never COPY from it, and a small context keeps the build fast.
	context := filepath.Dir(dockerfile)
	args := buildArgs(dockerfile, context, tag, t.Version, release, time.Now().UTC().Format("2006-01-02T15:04:05Z"), updateClaude)

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
