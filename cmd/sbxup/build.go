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

// dockerImageExists reports whether the tag is already in the local Docker daemon, which is
// what makes a second `sbxup` run reuse the image instead of rebuilding it.
var dockerImageExists = func(tag string) bool {
	return exec.Command("docker", "image", "inspect", tag).Run() == nil
}

// sbxTemplateListed reports whether `sbx` can already see the tag as a template.
//
// Whether a locally built image is visible to `sbx` without an explicit import depends on
// whether the sandbox runtime shares the host daemon's image store — undocumented, and it may
// differ by platform. Rather than assume either way, ask: if the tag is listed, the build is
// done; if not, import it. Correct under both behaviours, and costs one cheap list call.
var sbxTemplateListed = func(tag string) bool {
	out, err := exec.Command("sbx", "template", "ls").CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.Contains(string(out), tag)
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

	if !force && !updateClaude && !dryRun && dockerImageExists(tag) {
		fmt.Printf("Reusing local image: %s\n", tag)
		return tag, ensureTemplate(tag, dryRun)
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
