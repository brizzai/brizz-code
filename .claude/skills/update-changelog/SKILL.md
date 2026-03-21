---
name: update-changelog
description: Updates CHANGELOG.md under [Unreleased] following Keep a Changelog format. Use when the user says "changelog", "update changelog", or after completing a feature/fix that needs a changelog entry. Can also add the no-changelog label to skip.
disable-model-invocation: true
argument-hint: "[description or 'skip']"
---

## Current Branch Context
!`git log --oneline $(git merge-base HEAD master)..HEAD 2>/dev/null || echo "no commits"`
!`git diff --name-only $(git merge-base HEAD master)..HEAD 2>/dev/null || echo "no changes"`

## Task

Update `CHANGELOG.md` with an entry under `## [Unreleased]`, or skip with the `no-changelog` label.

### If argument is "skip", "no-changelog", or "none"
Add the `no-changelog` label to the current PR and stop:
```
gh pr edit --add-label "no-changelog"
```

### Otherwise, add a changelog entry

1. **Read** `CHANGELOG.md`
2. **Determine changes** from the argument, branch commits, and changed files above
3. **Classify** each change into the correct category:
   - **Added** — new features
   - **Changed** — changes in existing functionality
   - **Deprecated** — soon-to-be removed features
   - **Removed** — now removed features
   - **Fixed** — bug fixes
   - **Security** — vulnerability fixes
4. **Edit** the `## [Unreleased]` section:
   - Add category headers (`### Added`, etc.) only for categories that have entries
   - If a category header already exists under `[Unreleased]`, append to it
   - Each entry is a single `- ` bullet, concise, written for end users (not developers)
   - Use backticks for keys, commands, config values
   - Do NOT duplicate entries that already exist
5. **Do not** modify any released version sections or the link references at the bottom

### Writing style
- **One bullet per logical feature/fix** — don't split implementation details into separate entries. Docs, tests, config, and opt-out mechanisms are part of the feature, not separate items.
- **Write for the user, not the developer** — "Anonymous usage analytics (opt out via Settings or `DO_NOT_TRACK=1`)" not "Add Amplitude SDK with device ID hashing and silent logger"
- **Lead with the outcome**, not the implementation — what changed for someone using the app?
- **Skip internal-only changes** — new internal packages, refactors, docs files are not changelog-worthy on their own
- Keep bullets under ~120 chars
- No commit hashes, PR numbers, file paths, or internal jargon (DAU/MAU, SDK names, etc.)
