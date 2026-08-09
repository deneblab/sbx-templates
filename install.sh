#!/bin/sh
# Installer for the sbxup binary.
#
#   curl -sSL https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.sh | sh
#
# Environment:
#   SBXUP_VERSION      version to install, e.g. 0.1.4 (default: latest sbxup release)
#   SBXUP_INSTALL_DIR  where to put the binary (default: $HOME/.local/bin)
#   SBXUP_BASE_URL     release base URL (default: GitHub releases; override for mirrors/tests)
#
# The whole script lives in main(), called on the very last line, so that a download
# truncated midway cannot execute a partial program.

set -eu

REPO_URL="https://github.com/deneblab/sbx-templates"
API_URL="https://api.github.com/repos/deneblab/sbx-templates/releases"
TAG_PREFIX="sbxup-v"

log() { printf '%s\n' "$*"; }
err() { printf 'install.sh: %s\n' "$*" >&2; }

die() {
    err "$*"
    exit 1
}

# The binary is built with CGO_ENABLED=0, so it is statically linked and runs on musl
# (Alpine) as well as glibc. No libc detection is needed.
detect_platform() {
    os=$(uname -s)
    arch=$(uname -m)

    case "$os" in
        Linux)  goos=linux ;;
        Darwin) goos=darwin ;;
        *)
            die "unsupported operating system '$os'. Supported: Linux, macOS.
For Windows use install.ps1; see $REPO_URL"
            ;;
    esac

    case "$arch" in
        x86_64 | amd64)  goarch=amd64 ;;
        arm64 | aarch64) goarch=arm64 ;;
        *) die "unsupported architecture '$arch'. Published: amd64, arm64. See $REPO_URL/releases" ;;
    esac

    echo "$goos-$goarch"
}

fetch() {
    # fetch <url> <destination>; must fail on HTTP errors rather than saving an error page.
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$1" -O "$2"
    else
        die "neither curl nor wget is available"
    fi
}

fetch_stdout() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$1"
    else
        die "neither curl nor wget is available"
    fi
}

# This repository also publishes Docker-image releases, so /releases/latest is not
# necessarily an sbxup release. Pick the newest tag carrying the sbxup prefix.
latest_tag() {
    fetch_stdout "$API_URL?per_page=50" |
        tr ',' '\n' |
        sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\('"$TAG_PREFIX"'[^"]*\)".*/\1/p' |
        head -1
}

verify_checksum() {
    # verify_checksum <directory> <filename>; the .sha256 records a bare filename,
    # so verification runs from inside that directory.
    dir=$1
    name=$2
    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$dir" && sha256sum -c "$name.sha256" >/dev/null 2>&1)
    elif command -v shasum >/dev/null 2>&1; then
        (cd "$dir" && shasum -a 256 -c "$name.sha256" >/dev/null 2>&1)
    else
        die "no sha256 tool found (need sha256sum or shasum); cannot verify the download"
    fi
}

main() {
    version=${SBXUP_VERSION:-}
    install_dir=${SBXUP_INSTALL_DIR:-"$HOME/.local/bin"}
    base_url=${SBXUP_BASE_URL:-"$REPO_URL/releases"}

    platform=$(detect_platform)
    asset="sbxup-$platform"

    if [ -n "$version" ]; then
        tag="$TAG_PREFIX${version#v}"
    else
        tag=$(latest_tag) || true
        [ -n "$tag" ] || die "cannot determine the latest sbxup release.
Set SBXUP_VERSION to pin one explicitly; see $REPO_URL/releases"
    fi

    url="$base_url/download/$tag/$asset"
    log "Installing sbxup ($platform) from $tag"

    tmp=$(mktemp -d)
    # shellcheck disable=SC2064  # $tmp must expand now, not when the trap fires
    trap "rm -rf '$tmp'" EXIT INT TERM

    fetch "$url" "$tmp/$asset" ||
        die "download failed: $url
If you pinned a version, check it exists at $REPO_URL/releases"

    fetch "$url.sha256" "$tmp/$asset.sha256" ||
        die "no checksum published for this release ($url.sha256)."

    verify_checksum "$tmp" "$asset" ||
        die "checksum mismatch - the download is corrupt or has been tampered with. Nothing was installed."

    chmod +x "$tmp/$asset"

    mkdir -p "$install_dir" || die "cannot create '$install_dir'. Set SBXUP_INSTALL_DIR to a writable path."
    [ -w "$install_dir" ] || die "'$install_dir' is not writable. Set SBXUP_INSTALL_DIR to a writable path."

    # Move into place only after verification, so a failure never leaves a half-installed binary.
    mv -f "$tmp/$asset" "$install_dir/sbxup"

    log "Installed sbxup $("$install_dir/sbxup" --version 2>/dev/null || echo '') to $install_dir/sbxup"

    case ":$PATH:" in
        *":$install_dir:"*) ;;
        *)
            log ""
            log "$install_dir is not on your PATH. Add it with:"
            log "    export PATH=\"$install_dir:\$PATH\""
            ;;
    esac
}

main "$@"
