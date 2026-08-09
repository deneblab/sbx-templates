#!/bin/bash
# manifest.sh — assemble the templates-v* release manifest, and optionally stage its assets.
#
# Usage: ./scripts/release/manifest.sh [--stage DIR]
#   (no args)      print manifest.json to stdout — used by `task templates:manifest` to validate
#   --stage DIR    write manifest.json and templates-{version}.tar.gz of the whole src/
#                  tree into DIR — sbxup builds from the tarball, so the Dockerfiles are
#                  not published individually (see schemaVersion 2 below)
#
# The manifest is built from src/*/template.yaml, so adding a template is adding a directory.
# CI and the Taskfile both call this script; there is no second copy of the logic to drift.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

STAGE=""
while [[ $# -gt 0 ]]; do
  case $1 in
    --stage) STAGE="$2"; shift 2 ;;
    *) shift ;;
  esac
done

ABCVERSION_MIN="1.2.18" # first release with --scope

if ! command -v abcversion >/dev/null 2>&1; then
  echo "Error: abcversion not found on PATH — install it from" >&2
  echo "  https://github.com/deneblab/abcversion/releases/latest" >&2
  exit 1
fi

abcversion_have="$(abcversion --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
if [ -z "${abcversion_have}" ] ||
  [ "$(printf '%s\n%s\n' "${ABCVERSION_MIN}" "${abcversion_have}" | sort -V | head -1)" != "${ABCVERSION_MIN}" ]; then
  echo "Error: abcversion ${ABCVERSION_MIN}+ required for --scope (found ${abcversion_have:-none})" >&2
  echo "  update from https://github.com/deneblab/abcversion/releases/latest" >&2
  exit 1
fi

# Versions are BaseVersion plus the commits touching a subtree. --scope does that narrowing
# directly, so a template needs no .abcversion.json entry and adding one stays "add a directory".
# A scope matching no commits is a hard error, so a typo cannot quietly yield a repo-wide number.
scope_version() {
  local scope="$1"
  if ! abcversion -p semversion --scope "$scope" 2>/dev/null; then
    echo "Error: abcversion --scope '${scope}' failed — no commits touch that path?" >&2
    exit 1
  fi
}

# Read a key from a simple `key: value` YAML file, keeping inner whitespace so descriptions
# like ".NET SDK 10.0, Node.js 24.x" survive intact.
yaml_val() {
  local file="$1" key="$2"
  grep -E "^${key}:" "$file" 2>/dev/null | head -1 \
    | sed "s/^${key}:[[:space:]]*//" \
    | sed 's/[[:space:]]*$//'
}

# Escape a value for embedding in a JSON string.
json_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

RELEASE_VERSION="$(scope_version src)"
RELEASE_TAG="templates-v${RELEASE_VERSION}"
TARBALL="templates-${RELEASE_VERSION}.tar.gz"

entries=""
for meta in src/*/template.yaml; do
  [ -e "$meta" ] || continue
  dir="$(dirname "$meta")"
  base="$(basename "$dir")"

  if [ ! -f "${dir}/Dockerfile" ]; then
    echo "Error: ${dir} has template.yaml but no Dockerfile" >&2
    exit 1
  fi

  name="$(yaml_val "$meta" name)"
  short="$(yaml_val "$meta" short)"
  description="$(yaml_val "$meta" description)"
  [ -z "$name" ] && name="$base"
  [ -z "$short" ] && short="$base"

  if [ "$name" != "$base" ]; then
    echo "Error: ${meta} declares name '${name}' but lives in src/${base}" >&2
    exit 1
  fi

  # Per-template version: only commits touching src/${base} bump it, so an unchanged template
  # keeps its tag across releases and sbxup reuses the image users already built.
  version="$(scope_version "src/${base}")"

  [ -n "$entries" ] && entries="${entries},"
  entries="${entries}
    {
      \"name\": \"$(json_escape "$name")\",
      \"short\": \"$(json_escape "$short")\",
      \"description\": \"$(json_escape "$description")\",
      \"version\": \"$(json_escape "$version")\",
      \"registryImage\": \"docker.io/pkudrel/$(json_escape "$name"):latest\"
    }"
done

if [ -z "$entries" ]; then
  echo "Error: no src/*/template.yaml found" >&2
  exit 1
fi

# schemaVersion 2 means "the Dockerfiles ship only inside the tarball". sbxup 0.2.6+ builds
# from the tarball and accepts it; older builds fetch <name>.Dockerfile as its own asset, and
# refuse a schema they do not understand with a 'run sbxup --self-update' hint — which is a far
# better failure than a 404 on an asset that is no longer published.
MANIFEST="{
  \"schemaVersion\": 2,
  \"release\": \"${RELEASE_TAG}\",
  \"version\": \"${RELEASE_VERSION}\",
  \"tarball\": \"${TARBALL}\",
  \"templates\": [${entries}
  ]
}"

if [ -z "${STAGE}" ]; then
  printf '%s\n' "${MANIFEST}"
  exit 0
fi

mkdir -p "${STAGE}"
printf '%s\n' "${MANIFEST}" > "${STAGE}/manifest.json"

# Reproducible-ish tarball: sorted entries, no owner/timestamp noise from the checkout.
tar --sort=name --owner=0 --group=0 --numeric-owner -czf "${STAGE}/${TARBALL}" src

echo "Staged ${RELEASE_TAG} into ${STAGE}:" >&2
ls -1 "${STAGE}" >&2
