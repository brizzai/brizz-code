# Changelog Fragments

Each PR that makes user-facing changes should add a fragment file in `changelog/unreleased/`.

## Format

Create a file: `changelog/unreleased/<any-name>.md`

```yaml
---
type: added
---
Description of the change for end users
```

## Valid Types

| Type | Section |
|------|---------|
| `added` | ### Added |
| `improved` | ### Improved |
| `fixed` | ### Fixed |
| `changed` | ### Changed |
| `removed` | ### Removed |
| `deprecated` | ### Deprecated |
| `security` | ### Security |

## Examples

`changelog/unreleased/faster-status.md`:
```yaml
---
type: improved
---
Status updates now respond in ~150ms instead of up to 2s
```

`changelog/unreleased/fix-detach-stale.md`:
```yaml
---
type: fixed
---
Status showing stale data immediately after detaching from a session
```

## Skip

If your change doesn't need a changelog entry (CI, typos, deps), comment `/no-changelog` on the PR.

## Release

At release time (`/ship`), fragments are automatically merged into CHANGELOG.md and deleted.
