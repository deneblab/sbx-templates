#!/bin/bash
# build-push.sh — build (and optionally push) a sandbox image with computed semver.
# Usage: ./scripts/build/build-push.sh [--image NAME] [--no-push] [--dry-run] [--update-claude]
#   --image NAME      image directory name under src/ (default: sbx-claude-dotnet10)
#   --no-push         build and load into local Docker daemon, do not push
#   --dry-run         print the docker command, do not execute
#   --update-claude   skip cache for the 'claude' stage only (fast Claude Code update)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
IMAGE_NAME="sbx-claude-dotnet10"
NO_PUSH=false
DRY_RUN=false
UPDATE_CLAUDE=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --image)          IMAGE_NAME="$2"; shift 2 ;;
    --no-push)        NO_PUSH=true; shift ;;
    --dry-run)        DRY_RUN=true; shift ;;
    --update-claude)  UPDATE_CLAUDE=true; shift ;;
    *) shift ;;
  esac
done

CONTEXT="${REPO_ROOT}/src/${IMAGE_NAME}"

if ! command -v abcversion >/dev/null 2>&1; then
  echo "Error: abcversion not found on PATH — install it from" >&2
  echo "  https://github.com/deneblab/abcversion/releases/latest" >&2
  exit 1
fi

# Version scoped to this image's directory. AbcVersion keys that scoping on a named project
# (see .abcversion.json), and the project name is the template's `short` — the same name the
# Taskfile and the release manifest use.
PROJECT="$(grep -E '^short:' "${CONTEXT}/template.yaml" 2>/dev/null | head -1 \
  | sed -e 's/^short:[[:space:]]*//' -e 's/[[:space:]]*$//')"
[ -z "${PROJECT}" ] && PROJECT="${IMAGE_NAME}"

if ! version="$(abcversion -p semversion --project "${PROJECT}" 2>/dev/null)"; then
  echo "Error: no '${PROJECT}' project in .abcversion.json" >&2
  echo "  add: \"${PROJECT}\": { \"Name\": \"${PROJECT}\", \"Path\": \"src/${IMAGE_NAME}\", \"BaseVersion\": \"0.2.0\" }" >&2
  exit 1
fi
tag="v${version}"
short_sha="$(git -C "${REPO_ROOT}" rev-parse --short=7 HEAD)"
build_date_utc="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

IMAGE="docker.io/pkudrel/${IMAGE_NAME}"

if [ "${NO_PUSH}" = true ]; then
  PUSH_FLAG="--load"
else
  PUSH_FLAG="--push"
fi

NO_CACHE_FLAG=""
if [ "${UPDATE_CLAUDE}" = true ]; then
  NO_CACHE_FLAG="--no-cache-filter claude"
fi

echo "Version       : ${version}"
echo "Tag           : ${tag}"
echo "SHA           : ${short_sha}"
echo "Image         : ${IMAGE}:${tag}"
echo "Update claude : ${UPDATE_CLAUDE}"
echo ""

if [ "${DRY_RUN}" = true ]; then
  echo "[dry-run] would run:"
  echo "  docker buildx build ${PUSH_FLAG} ${NO_CACHE_FLAG} \\"
  echo "    -t ${IMAGE}:${tag} \\"
  echo "    -t ${IMAGE}:latest \\"
  echo "    --build-arg VERSION=${version} \\"
  echo "    --build-arg SHORT_SHA=${short_sha} \\"
  echo "    --build-arg BUILD_DATE=${build_date_utc} \\"
  echo "    ${CONTEXT}"
  exit 0
fi

docker buildx build ${PUSH_FLAG} ${NO_CACHE_FLAG} \
  -t "${IMAGE}:${tag}" \
  -t "${IMAGE}:latest" \
  --build-arg "VERSION=${version}" \
  --build-arg "SHORT_SHA=${short_sha}" \
  --build-arg "BUILD_DATE=${build_date_utc}" \
  "${CONTEXT}"

echo ""
if [ "${NO_PUSH}" = true ]; then
  echo "Built: ${IMAGE}:${tag} (loaded to local daemon)"
else
  echo "Pushed: ${IMAGE}:${tag}"
  echo "Pushed: ${IMAGE}:latest"
fi
