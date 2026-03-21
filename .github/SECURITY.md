# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in brizz-code, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please use [GitHub Security Advisories](https://github.com/brizzai/brizz-code/security/advisories/new) to report the vulnerability privately. This allows us to assess and address the issue before it becomes public.

## What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

## Response

We will acknowledge your report within 48 hours and aim to release a fix promptly for confirmed vulnerabilities.

## Scope

brizz-code runs locally on macOS and interacts with:
- tmux sessions
- Claude Code processes
- Local SQLite database
- GitHub API (via `gh` CLI)
- Chrome extension (via Unix socket)

Security issues in any of these interaction points are in scope.
