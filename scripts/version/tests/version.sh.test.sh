#!/bin/bash
# Tests for scripts/version/version.sh
# Usage: bash scripts/version/tests/version.sh.test.sh
#
# Creates a temporary git repo, runs version.sh against it, and verifies output.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION_SH="$(cd "$SCRIPT_DIR/.." && pwd)/version.sh"

PASS=0
FAIL=0
TMPDIRS=()
ORIG_DIR="$(pwd)"

# ── Helpers ──────────────────────────────────────────────────────────────────

cleanup() {
    cd "$ORIG_DIR"
    for d in "${TMPDIRS[@]}"; do
        [ -d "$d" ] && rm -rf "$d"
    done
}
trap cleanup EXIT

setup_repo() {
    cd "$ORIG_DIR"
    local tmp
    tmp="$(mktemp -d)"
    TMPDIRS+=("$tmp")
    cd "$tmp"
    git init -q
    git config user.email "test@test.com"
    git config user.name "Test"
}

assert_eq() {
    local label="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        echo "  PASS: $label"
        PASS=$((PASS + 1))
    else
        echo "  FAIL: $label — expected '$expected', got '$actual'"
        FAIL=$((FAIL + 1))
    fi
}

get_version() {
    bash "$VERSION_SH" --version-only "$@"
}

get_key() {
    local key="$1"; shift
    bash "$VERSION_SH" "$@" | grep "^${key}=" | head -1 | sed "s/^${key}=//"
}

# ── Tests ────────────────────────────────────────────────────────────────────

test_basic_commit_counting() {
    echo "TEST: basic commit counting"
    setup_repo

    echo 'baseVersion: 1.0.0' > version.yaml
    git add . && git commit -q -m "first"
    git commit -q --allow-empty -m "second"
    git commit -q --allow-empty -m "third"

    local v
    v=$(get_version)
    assert_eq "3 commits → 1.0.3" "1.0.3" "$v"
}

test_base_patch_added_to_count() {
    echo "TEST: base patch is added to commit count"
    setup_repo

    echo 'baseVersion: 2.3.5' > version.yaml
    git add . && git commit -q -m "first"
    git commit -q --allow-empty -m "second"

    local v
    v=$(get_version)
    assert_eq "baseVersion 2.3.5 + 2 commits → 2.3.7" "2.3.7" "$v"
}

test_base_commit_sha() {
    echo "TEST: baseCommitSha scopes counting"
    setup_repo

    echo 'baseVersion: 0.1.0' > version.yaml
    git add . && git commit -q -m "first"
    git commit -q --allow-empty -m "second"

    local sha
    sha=$(git rev-parse HEAD)

    git commit -q --allow-empty -m "third"
    git commit -q --allow-empty -m "fourth"

    # Update version.yaml with the sha
    echo "baseVersion: 0.1.0" > version.yaml
    echo "baseCommitSha: $sha" >> version.yaml
    git add . && git commit -q -m "update config"

    local v
    v=$(get_version)
    # 3 commits after sha: third, fourth, "update config"
    assert_eq "commits after SHA → 0.1.3" "0.1.3" "$v"
}

test_version_only_flag() {
    echo "TEST: --version-only outputs just the version"
    setup_repo

    echo 'baseVersion: 1.0.0' > version.yaml
    git add . && git commit -q -m "first"

    local output
    output=$(bash "$VERSION_SH" --version-only)
    assert_eq "single line output" "1.0.1" "$output"

    # Full output should have key=value lines
    local full_lines
    full_lines=$(bash "$VERSION_SH" | wc -l)
    [ "$full_lines" -gt 1 ] && assert_eq "full output has multiple lines" "true" "true" \
                             || assert_eq "full output has multiple lines" "true" "false"
}

test_override() {
    echo "TEST: --override ignores version.yaml"
    setup_repo

    echo 'baseVersion: 1.0.0' > version.yaml
    git add . && git commit -q -m "first"
    git commit -q --allow-empty -m "second"

    local v
    v=$(get_version --override "5.0.0")
    assert_eq "override 5.0.0 + 2 commits → 5.0.2" "5.0.2" "$v"
}

test_custom_file() {
    echo "TEST: --file uses alternate config"
    setup_repo

    echo 'baseVersion: 1.0.0' > version.yaml
    echo 'baseVersion: 3.0.0' > custom.yaml
    git add . && git commit -q -m "first"

    local v
    v=$(get_version --file custom.yaml)
    assert_eq "custom file 3.0.0 + 1 commit → 3.0.1" "3.0.1" "$v"
}

test_tag_prefix() {
    echo "TEST: --prefix changes tag prefix"
    setup_repo

    echo 'baseVersion: 1.0.0' > version.yaml
    git add . && git commit -q -m "first"

    local tag
    tag=$(get_key "tag" --prefix "release-")
    assert_eq "custom prefix" "release-1.0.1" "$tag"
}

test_path_scoping() {
    echo "TEST: --path scopes commit counting"
    setup_repo

    mkdir -p src/imageA src/imageB
    echo 'baseVersion: 0.1.0' > version.yaml
    echo "a" > src/imageA/Dockerfile
    git add . && git commit -q -m "init"

    echo "b" > src/imageB/Dockerfile
    git add . && git commit -q -m "add imageB"

    echo "a2" > src/imageA/Dockerfile
    git add . && git commit -q -m "update imageA"

    # Total: 3 commits
    # src/imageA touched in: init, update imageA → 2
    # src/imageB touched in: add imageB → 1

    local v_all v_a v_b
    v_all=$(get_version)
    v_a=$(get_version --path src/imageA)
    v_b=$(get_version --path src/imageB)

    assert_eq "global count → 0.1.3" "0.1.3" "$v_all"
    assert_eq "imageA path → 0.1.2" "0.1.2" "$v_a"
    assert_eq "imageB path → 0.1.1" "0.1.1" "$v_b"
}

test_path_with_base_commit_sha() {
    echo "TEST: --path with baseCommitSha"
    setup_repo

    mkdir -p src/app
    echo 'baseVersion: 0.1.0' > version.yaml
    echo "v1" > src/app/file.txt
    git add . && git commit -q -m "init"

    local sha
    sha=$(git rev-parse HEAD)

    echo "v2" > src/app/file.txt
    git add . && git commit -q -m "update app"

    git commit -q --allow-empty -m "unrelated"

    echo "baseVersion: 0.1.0" > version.yaml
    echo "baseCommitSha: $sha" >> version.yaml
    git add . && git commit -q -m "set sha"

    # After sha: "update app" (touches src/app), "unrelated" (no path), "set sha" (no path)
    local v_path v_global
    v_path=$(get_version --path src/app)
    v_global=$(get_version)

    assert_eq "path-scoped after SHA → 0.1.1" "0.1.1" "$v_path"
    assert_eq "global after SHA → 0.1.3" "0.1.3" "$v_global"
}

test_path_no_matching_commits() {
    echo "TEST: --path with no matching commits"
    setup_repo

    mkdir -p src/empty
    echo 'baseVersion: 0.5.0' > version.yaml
    git add . && git commit -q -m "init"
    git commit -q --allow-empty -m "empty"

    local v
    v=$(get_version --path src/empty)
    assert_eq "no commits touch path → 0.5.0" "0.5.0" "$v"
}

test_output_keys() {
    echo "TEST: full output contains all expected keys"
    setup_repo

    echo 'baseVersion: 1.0.0' > version.yaml
    git add . && git commit -q -m "first"

    local output
    output=$(bash "$VERSION_SH")

    for key in version major minor patch tag sha short_sha branch build_date_utc; do
        if echo "$output" | grep -q "^${key}="; then
            assert_eq "output has $key" "true" "true"
        else
            assert_eq "output has $key" "true" "false"
        fi
    done
}

test_missing_config_fails() {
    echo "TEST: missing config file exits with error"
    setup_repo

    git commit -q --allow-empty -m "init"

    if bash "$VERSION_SH" --file nonexistent.yaml --version-only 2>/dev/null; then
        assert_eq "should fail with missing config" "fail" "success"
    else
        assert_eq "exits non-zero for missing config" "true" "true"
    fi
}

# ── Run ──────────────────────────────────────────────────────────────────────

echo "=== version.sh tests ==="
echo ""

test_basic_commit_counting
test_base_patch_added_to_count
test_base_commit_sha
test_version_only_flag
test_override
test_custom_file
test_tag_prefix
test_path_scoping
test_path_with_base_commit_sha
test_path_no_matching_commits
test_output_keys
test_missing_config_fails

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="

[ "$FAIL" -eq 0 ] || exit 1
