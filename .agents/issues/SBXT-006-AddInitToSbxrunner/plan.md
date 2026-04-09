# Plan: SBXT-006-AddInitToSbxrunner

## Summary

Add `--init` flag to `sbx-runner` and convert all user-facing text to Linux-style `--double-dash` convention.

## Files to Modify

### 1. `shells/sbx-runner.ps1` — Main implementation
Changes:
- Add `-Init` switch parameter (PowerShell maps `--init` → `-Init`)
- Add init handler block (before config resolution) that:
  - Determines target path: if `-Config` given, use it; otherwise `.agents/sbx-runner.yaml`
  - Creates `.agents` directory if needed
  - Writes default YAML (`template`, `agent`, `branch: auto`) if file doesn't exist
  - Warns if file already exists
- Update help text to use `--double-dash` for all arguments
- Update error messages (agent missing, template missing, config not found) to use `--double-dash` and mention `--init`
- Add `--init` to help text usage section

### No new files needed
This is a single-file modification.

## Implementation Order
1. Add `-Init` switch parameter
2. Add init handler block
3. Convert all help text to `--double-dash` convention
4. Update error messages to mention `--init`

## Default config template
```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
branch: auto
```
