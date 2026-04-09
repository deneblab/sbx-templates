#!/bin/bash

# version.sh — compute semver from version.yaml + git commit count.
#
# version.yaml format:
#   baseVersion: 0.1.0
#   baseCommitSha: abc1234   # optional
#
# If baseCommitSha is set, counts commits after that SHA.
# If absent, counts all commits.
# If --path is set, only counts commits touching that path.

# --- Default Values ---
CONFIG_FILE="${INPUT_CONFIG_FILE:-version.yaml}"
OVERRIDE="${INPUT_MAJOR_MINOR:-}"
TAG_PREFIX="${INPUT_TAG_PREFIX:-v}"
OUTPUT_DEST="${GITHUB_OUTPUT:-/dev/stdout}"
VERSION_ONLY=false
COUNT_PATH=""

# --- Argument Parser ---
while [[ $# -gt 0 ]]; do
  case $1 in
    -v|--version-only) VERSION_ONLY=true; shift ;;
    --file)     CONFIG_FILE="$2"; shift 2 ;;
    --override) OVERRIDE="$2"; shift 2 ;;
    --prefix)   TAG_PREFIX="$2"; shift 2 ;;
    --path)     COUNT_PATH="$2"; shift 2 ;;
    *) shift ;;
  esac
done

sh_git() {
    git "$@" 2>/dev/null | tr -d '\r' | xargs
}

parse_semver() {
    local input="$1"
    if [[ $input =~ ^([0-9]+)\.([0-9]+)(\.([0-9]+))? ]]; then
        MAJOR="${BASH_REMATCH[1]}"
        MINOR="${BASH_REMATCH[2]}"
        FILE_PATCH="${BASH_REMATCH[4]:-0}"
    else
        echo "Error: Cannot parse semver from '$input'" >&2
        exit 1
    fi
}

# Read a key from a simple YAML file (key: value, one per line).
yaml_val() {
    local file="$1" key="$2"
    grep -E "^${key}:" "$file" 2>/dev/null | head -1 | sed "s/^${key}:[[:space:]]*//" | sed 's/#.*//' | tr -d '[:space:]'
}

# --- Main Logic ---

# 1. Load Base Version
if [ -n "$OVERRIDE" ]; then
    parse_semver "$OVERRIDE"
    BASE_COMMIT_SHA=""
else
    [ ! -f "$CONFIG_FILE" ] && { echo "Error: $CONFIG_FILE not found" >&2; exit 1; }
    BASE_VERSION=$(yaml_val "$CONFIG_FILE" "baseVersion")
    [ -z "$BASE_VERSION" ] && { echo "Error: baseVersion not found in $CONFIG_FILE" >&2; exit 1; }
    parse_semver "$BASE_VERSION"
    BASE_COMMIT_SHA=$(yaml_val "$CONFIG_FILE" "baseCommitSha")
fi

# 2. Calculate Increment
PATH_ARGS=()
if [ -n "$COUNT_PATH" ]; then
    PATH_ARGS=("--" "$COUNT_PATH")
fi

if [ -n "$BASE_COMMIT_SHA" ]; then
    # Validate SHA exists in repo.
    if ! sh_git cat-file -t "$BASE_COMMIT_SHA" | grep -q "commit"; then
        echo "Error: baseCommitSha '$BASE_COMMIT_SHA' not found in git history" >&2
        exit 1
    fi
    INC=$(sh_git rev-list --count "${BASE_COMMIT_SHA}..HEAD" "${PATH_ARGS[@]}")
else
    INC=$(sh_git rev-list --count HEAD "${PATH_ARGS[@]}")
fi
[ -z "$INC" ] && INC=0

# 3. Final Calculation
FINAL_PATCH=$((FILE_PATCH + INC))
VERSION="${MAJOR}.${MINOR}.${FINAL_PATCH}"

# 4. Output Logic
if [ "$VERSION_ONLY" = true ]; then
    echo "$VERSION"
else
    FULL_SHA=$(sh_git rev-parse HEAD)
    SHORT_SHA=$(sh_git rev-parse --short=7 HEAD)
    BRANCH=$(sh_git branch --show-current || sh_git rev-parse --abbrev-ref HEAD)
    BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    TAG="${TAG_PREFIX}${VERSION}"

    {
        echo "version=${VERSION}"
        echo "major=${MAJOR}"
        echo "minor=${MINOR}"
        echo "patch=${FINAL_PATCH}"
        echo "tag=${TAG}"
        echo "sha=${FULL_SHA}"
        echo "short_sha=${SHORT_SHA}"
        echo "branch=${BRANCH}"
        echo "build_date_utc=${BUILD_DATE}"
    } >> "$OUTPUT_DEST"
fi
