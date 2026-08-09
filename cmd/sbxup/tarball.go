package main

// One verified download per release instead of one per template.
//
// A templates-v* release ships templates-{version}.tar.gz containing the whole src/ tree, so
// extracting it once makes every template in that release available without further network
// access, and gives each build the same context directory `build-push.sh` uses locally —
// src/<name>/, with the template's Dockerfile and template.yaml beside each other.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// Bounds on what one extraction may produce. The real archive is ~2 KB across a dozen
	// entries, so these constrain only a corrupt or hostile one.
	maxTarBytes   = 32 << 20
	maxTarEntries = 512
)

// extractTarGz writes the archive's src/ tree under dest, and returns how many files it wrote.
//
// It is deliberately strict, because the archive is a remote artifact whose contents become
// `docker build` input: only directories and regular files are accepted, anything outside
// src/ is ignored, and an entry that would escape dest — through "..", an absolute path, or a
// symlink — aborts the whole extraction rather than being sanitised into something safe.
func extractTarGz(data []byte, dest string) (int, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("tarball is not gzip data: %w", err)
	}
	defer zr.Close()

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return 0, err
	}

	tr := tar.NewReader(zr)
	var (
		files   int
		entries int
		written int64
	)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return files, fmt.Errorf("corrupt tarball: %w", err)
		}
		if entries++; entries > maxTarEntries {
			return files, fmt.Errorf("tarball has more than %d entries — refusing to extract", maxTarEntries)
		}

		// tar paths are always slash-separated, whatever the host. Clean resolves any ".."
		// before the prefix test, so "src/../../etc/passwd" becomes "../etc/passwd" and is
		// no longer under src/ — traversal is rejected by the same check that selects the tree.
		name := path.Clean(hdr.Name)
		if name != "src" && !strings.HasPrefix(name, "src/") {
			continue // ignore anything the release adds outside the template tree
		}

		target := filepath.Join(destAbs, filepath.FromSlash(name))
		if target != destAbs && !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) {
			return files, fmt.Errorf("tarball entry %q escapes the extraction directory", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return files, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return files, err
			}
			remaining := maxTarBytes - written
			if remaining <= 0 {
				return files, fmt.Errorf("tarball expands beyond %d bytes — refusing to extract", int64(maxTarBytes))
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return files, err
			}
			n, err := io.Copy(f, io.LimitReader(tr, remaining))
			f.Close()
			if err != nil {
				return files, err
			}
			written += n
			files++
		default:
			// Symlinks, hard links and devices have no business in a Dockerfile archive, and a
			// symlink is the classic way to write outside the destination on extraction.
			return files, fmt.Errorf("tarball entry %q has unsupported type %q", hdr.Name, string(hdr.Typeflag))
		}
	}
	return files, nil
}

// ensureTemplateTree makes a release's src/ tree available on disk and returns its path,
// downloading and verifying the tarball only when the tree is missing or refresh is set.
//
// Extraction lands in a sibling temp directory that is swapped into place once complete, so an
// interrupted run can never leave a half-populated tree that a later run would build from.
func ensureTemplateTree(client *http.Client, tag string, m *Manifest, refresh bool) (string, error) {
	dir, err := templatesCacheDir(tag)
	if err != nil {
		return "", err
	}
	tree := filepath.Join(dir, "src")

	if !refresh {
		if info, err := os.Stat(tree); err == nil && info.IsDir() {
			return tree, nil
		}
	}

	data, err := fetchVerified(client, tag, m.Tarball)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".sbxup-extract-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // no-op for the part already renamed away

	if _, err := extractTarGz(data, tmp); err != nil {
		return "", fmt.Errorf("cannot extract %s: %w", m.Tarball, err)
	}
	staged := filepath.Join(tmp, "src")
	if info, err := os.Stat(staged); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s contains no src/ tree", m.Tarball)
	}

	// Rename cannot merge into an existing directory, so clear the old tree first. Both paths
	// are inside the same cache directory, hence the same filesystem, so the swap is a rename.
	if err := os.RemoveAll(tree); err != nil {
		return "", err
	}
	if err := os.Rename(staged, tree); err != nil {
		return "", fmt.Errorf("cannot install the extracted tree: %w", err)
	}
	return tree, nil
}
