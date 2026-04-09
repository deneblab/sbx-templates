#!/bin/bash
# build-push.sh — build (and optionally push) a sandbox image with computed semver.
# Usage: ./scripts/build/build-push.sh [--image NAME] [--no-push] [--dry-run]
#   --image NAME  image directory name under src/ (default: sbx-claude-dotnet10)
#   --no-push     build and load into local Docker daemon, do not push
#   --dry-run     print the docker command, do not execute

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
IMAGE_NAME="sbx-claude-dotnet10"
NO_PUSH=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --image)   IMAGE_NAME="$2"; shift 2 ;;
    --no-push) NO_PUSH=true; shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    *) shift ;;
  esac
done

CONTEXT="${REPO_ROOT}/src/${IMAGE_NAME}"

# Compute version (global version.yaml at repo root)
eval "$(bash "${REPO_ROOT}/scripts/build/version.sh")"

IMAGE="docker.io/pkudrel/${IMAGE_NAME}"

if [ "${NO_PUSH}" = true ]; then
  PUSH_FLAG="--load"
else
  PUSH_FLAG="--push"
fi

echo "Version : ${version}"
echo "Tag     : ${tag}"
echo "SHA     : ${short_sha}"
echo "Image   : ${IMAGE}:${tag}"
echo ""

if [ "${DRY_RUN}" = true ]; then
  echo "[dry-run] would run:"
  echo "  docker buildx build ${PUSH_FLAG} \\"
  echo "    -t ${IMAGE}:${tag} \\"
  echo "    -t ${IMAGE}:latest \\"
  echo "    --build-arg VERSION=${version} \\"
  echo "    --build-arg SHORT_SHA=${short_sha} \\"
  echo "    --build-arg BUILD_DATE=${build_date_utc} \\"
  echo "    ${CONTEXT}"
  exit 0
fi

docker buildx build ${PUSH_FLAG} \
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
