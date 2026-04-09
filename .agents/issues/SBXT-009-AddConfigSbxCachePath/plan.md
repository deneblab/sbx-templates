# Plan: SBXT-009-AddConfigSbxCachePath

## Summary

Add optional `cache` key to sbx-runner.yaml that mounts a local cache directory into the sandbox.

## Files Modified

- `shells/sbx-runner.ps1` — read `cache` key, resolve path, create dir, pass as extra workspace arg
- `README.md` — document cache config option
