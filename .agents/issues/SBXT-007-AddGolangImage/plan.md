# Plan: SBXT-007-AddGolangImage

## Summary

Create a new sandbox Docker template `sbx-claude-golang` with Go LTS, following the existing template pattern.

## Files to Create/Modify

### 1. `src/sbx-claude-golang/Dockerfile` — NEW
- Extend `docker/sandbox-templates:claude-code`
- Install Go LTS via official tarball from go.dev
- Cache dirs at `/workspace/.sbx-cache/go/`
- Same structure as dotnet10-node: root install, user switch, env vars, version labels

### 2. `src/sbx-claude-golang/version.yaml` — NEW
- `baseVersion: 0.1.0` (new image, start fresh)

### 3. `Taskfile.yml` — MODIFY
- Add `version:golang`, `build:golang`, `push:golang` task entries following dotnet10-node pattern

## Implementation Order
1. Create Dockerfile
2. Create version.yaml
3. Add Taskfile entries
