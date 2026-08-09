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

# Version scoped to this image's directory: BaseVersion plus the commits touching it, so an
# unchanged template keeps its tag. No .abcversion.json entry needed — --scope is the narrowing.
if ! version="$(abcversion -p semversion --scope "src/${IMAGE_NAME}" 2>/dev/null)"; then
  echo "Error: abcversion --scope 'src/${IMAGE_NAME}' failed — does that directory exist in git?" >&2
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
