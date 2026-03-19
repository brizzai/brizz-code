# Contributing to brizz-code

Thanks for your interest in contributing! This guide will help you get started.

## Discussion First

For new features or significant changes, please open an issue to discuss your idea before submitting a PR. This helps avoid wasted effort and ensures alignment with the project direction.

Bug fixes and small improvements can go straight to a PR.

## Development Setup

### Requirements

- **macOS** (brizz-code is macOS-only)
- **Go 1.24+**
- **tmux** (`brew install tmux`)
- **Claude Code** (for end-to-end testing)
- **Git**

### Getting Started

```bash
git clone https://github.com/brizzai/brizz-code.git
cd brizz-code
make build    # Build to build/brizz-code
make test     # Run tests with race detector
make lint     # Run linter (requires golangci-lint)
```

### Useful Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build binary to `build/brizz-code` |
| `make test` | Run tests with `-race` |
| `make lint` | Run golangci-lint |
| `make coverage` | Run tests with coverage report |
| `make fmt` | Format code |
| `make vet` | Run go vet |

## Code Style

- Run `go fmt` before committing
- Follow existing patterns in the codebase
- All blocking I/O (tmux, git, gh) must run in the background worker goroutine, never in Bubble Tea `Update()`

## Commit Convention

We use [Conventional Commits](https://www.conventionalcommits.org/). This drives automatic version bumps on merge to main.

```
feat: add new feature          # minor bump
fix: resolve bug               # patch bump
feat!: breaking change         # major bump
chore: update dependencies     # patch bump
docs: update README            # patch bump
refactor: restructure code     # patch bump
test: add tests                # patch bump
```

Scopes are optional: `fix(hooks): ...`, `feat(ui): ...`

Add `[skip release]` in commit message to skip version bump.

## Pull Requests

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Ensure `make build && make test && make lint` pass
5. Submit a PR with a clear description

## Testing

- Add tests for new features and bug fixes
- Run `make test` to verify all tests pass
- Run `make coverage` to check coverage

## Project Structure

See `CLAUDE.md` for a complete package overview and architecture details.
