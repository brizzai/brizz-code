---
type: improved
---
Worktree dialogs now guide you toward a valid git branch name as you type: spaces become `-`, and chars git forbids anywhere (`~ ^ : ? * [ \` and control chars) are dropped live. Rules that can't be fixed silently (leading `-`, `..`, trailing `.lock`, etc.) show a friendly inline error on submit instead of a cryptic `git worktree add` failure.
