# Plan — SBXT-001-BuildDockerImagesWithVersionAndPush

## Goal
CI/CD pipeline (GitHub Actions) + local build script to:
1. Compute semver via `scripts/build/version.sh`
2. Build `src/sbx-claude-dotnet10/Dockerfile`
3. Tag and push to `docker.io/pkudrel/sbx-claude-dotnet10`
4. Bake `VERSION`, `SHORT_SHA`, and `BUILD_DATE` as image labels

## Files to create / modify

| # | Path | Action | Status |
|---|------|--------|--------|
| 1 | `.github/workflows/build-push.yml` | create | pending |
| 2 | `scripts/build/build-push.sh` | create | pending |
| 3 | `src/sbx-claude-dotnet10/Dockerfile` | modify (add ARG+LABEL) | pending |

## Key decisions

- `version.sh` already writes `key=value` to `$GITHUB_OUTPUT` when that env var is set — no wrapper needed
- `fetch-depth: 0` required in checkout so git commit count is correct
- Use `docker/build-push-action@v6` (handles buildx, multi-platform, cache)
- Docker Hub secrets: `DOCKER_USERNAME` + `DOCKER_TOKEN` (use access token, not password)
- Tags: `:v{VERSION}` and `:latest`
- Labels follow OCI spec: `org.opencontainers.image.*`
- Local script mirrors GHA logic but accepts `--dry-run` to print without pushing

## Creation order
1 → 3 → 2  (workflow first, then Dockerfile update, then local helper)
