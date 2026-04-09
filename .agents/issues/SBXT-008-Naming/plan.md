# Plan: SBXT-008-Naming

## Summary

Rename image templates to include version numbers for all runtimes.

## Renames

- `src/sbx-claude-dotnet10-node/` → `src/sbx-claude-dotnet10-node24/`
- `src/sbx-claude-golang-node/` → `src/sbx-claude-golang124-node24/`

## Files Modified

- `src/` directories (renamed)
- `Taskfile.yml` — task names and IMAGE vars
- `CLAUDE.md` — image references and build commands
- `README.md` — image table and build commands
