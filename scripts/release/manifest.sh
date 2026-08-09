#!/bin/bash
# manifest.sh — assemble the templates-v* release manifest, and optionally stage its assets.
#
# Usage: ./scripts/release/manifest.sh [--stage DIR]
#   (no args)      print manifest.json to stdout — used by `task templates:manifest` to validate
#   --stage DIR    write manifest.json, <name>.Dockerfile for every template, and
#                  templates-{version}.tar.gz of the whole src/ tree into DIR
#
# The manifest is built from src/*/template.yaml, so adding a template is adding a directory —
# plus an .abcversion.json project keyed by its `short` name, which is where its version comes from.
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

if ! command -v abcversion >/dev/null 2>&1; then
  echo "Error: abcversion not found on PATH — install it from" >&2
  echo "  https://github.com/deneblab/abcversion/releases/latest" >&2
  exit 1
fi

# Versions come from named .abcversion.json projects. AbcVersion scopes commit counting by
# `Projects[].Path`; its own --path flag selects the repository, not a subtree — so every
# template needs an entry keyed by its `short` name, and adding a template means adding one.
# An unknown project is a hard error there, which is why a missing entry cannot silently fall
# back to the repo-wide version.
project_version() {
  local project="$1"
  if ! abcversion -p semversion --project "$project" 2>/dev/null; then
    echo "Error: no '${project}' project in .abcversion.json" >&2
    echo "  add: \"${project}\": { \"Name\": \"${project}\", \"Path\": \"src/<dir>\", \"BaseVersion\": \"0.2.0\" }" >&2
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

RELEASE_VERSION="$(project_version templates)"
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
  version="$(project_version "$short")"

  [ -n "$entries" ] && entries="${entries},"
  entries="${entries}
    {
      \"name\": \"$(json_escape "$name")\",
      \"short\": \"$(json_escape "$short")\",
      \"description\": \"$(json_escape "$description")\",
      \"dockerfile\": \"$(json_escape "${name}.Dockerfile")\",
      \"version\": \"$(json_escape "$version")\",
      \"registryImage\": \"docker.io/pkudrel/$(json_escape "$name"):latest\"
    }"
done

if [ -z "$entries" ]; then
  echo "Error: no src/*/template.yaml found" >&2
  exit 1
fi

MANIFEST="{
  \"schemaVersion\": 1,
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

for meta in src/*/template.yaml; do
  [ -e "$meta" ] || continue
  base="$(basename "$(dirname "$meta")")"
  cp "src/${base}/Dockerfile" "${STAGE}/${base}.Dockerfile"
  # Normalise the mode: some working trees report 0755 for tracked 0644 files, and the
  # release should not hand out Dockerfiles that look executable.
  chmod 0644 "${STAGE}/${base}.Dockerfile"
done

# Reproducible-ish tarball: sorted entries, no owner/timestamp noise from the checkout.
tar --sort=name --owner=0 --group=0 --numeric-owner -czf "${STAGE}/${TARBALL}" src

echo "Staged ${RELEASE_TAG} into ${STAGE}:" >&2
ls -1 "${STAGE}" >&2
