# brz-env Workspace Provider for brizz-code

Configure brizz-code to use `brz-env` environments instead of raw git worktrees for the brizzai repo.

## Prerequisites

- `brz-env` installed (via `uv run --project <path-to-brizzai>/scripts/brz-env brz-env`)
- Registry file exists at `~/.brz-env/registry.json`
- brizz-code built and working

## Setup

Create `.bc.local.json` in your brizzai repo root (`~/code/brizzai/.bc.local.json`):

```json
{
  "workspace": {
    "list": "python3 -c \"import json,pathlib; r=json.loads(pathlib.Path.home().joinpath('.brz-env/registry.json').read_text()); print(json.dumps([{'name':e['name'],'path':e['worktree'],'branch':e['branch'],'status':e.get('status','')} for e in r.get('environments',{}).values()]))\"",
    "create": "uv run --project /Users/you/code/brizzai/scripts/brz-env brz-env create {{name}} --branch {{branch}}",
    "destroy": "uv run --project /Users/you/code/brizzai/scripts/brz-env brz-env destroy {{name}} --yes"
  }
}
```

Replace `/Users/you/code/brizzai` with your actual brizzai repo path.

### Why `.bc.local.json`?

`.bc.local.json` is gitignored by convention — it's for personal/machine-specific config. Since the brizzai repo path varies per developer, this keeps it out of version control.

### Shell aliases don't work

`ShellProvider` runs commands via `sh -c`, which doesn't load shell aliases. You must use the full `uv run --project ...` invocation — not the `brz-env` alias.

## How It Works

| Command   | What happens |
|-----------|-------------|
| **list**  | Reads `~/.brz-env/registry.json` directly with Python and outputs the JSON array that brizz-code's `ShellProvider` expects (`[{name, path, branch, status}]`). This bypasses `brz-env list` which outputs a human-readable Rich table with no `--json` flag. |
| **create** | Runs `brz-env create <name> --branch <branch>` via uv, setting up a full environment: git worktree + databases + port allocation + config patching. If branch is omitted, brz-env defaults to `env/<name>`. |
| **destroy** | Runs `brz-env destroy <name> --yes` via uv, tearing down everything (worktree, databases, ports). `--yes` skips the confirmation prompt. |

## Verification

1. Test the list command in your shell:

   ```sh
   python3 -c "import json,pathlib; r=json.loads(pathlib.Path.home().joinpath('.brz-env/registry.json').read_text()); print(json.dumps([{'name':e['name'],'path':e['worktree'],'branch':e['branch'],'status':e.get('status','')} for e in r.get('environments',{}).values()]))"
   ```

   Should output a JSON array like:
   ```json
   [{"name": "BRZ684", "path": "/Users/you/code/brizzai-BRZ684", "branch": "env/BRZ684", "status": "ready"}]
   ```

2. Launch brizz-code, navigate to a brizzai session, press `a` — should show brz-env environments in the workspace picker.

3. Create/destroy from the picker should invoke `brz-env` commands.
