# Agent Safety Rules

## Destructive command guard

This repository expects [Destructive Command Guard](https://github.com/Dicklesworthstone/destructive_command_guard) (`dcg`) to be installed for agent shell-command hooks and commit-time scans.

Never run destructive commands without explicit user approval. In particular:

- Do not run `git reset --hard`, `git reset --merge`, `git clean -fd`, `git checkout -- <path>`, or `git restore <path>` to discard work unless the user explicitly asks for that exact destructive action.
- Do not run `git push --force`; prefer `git push --force-with-lease` only when the user explicitly approves rewriting remote history.
- Do not run broad `rm -rf` commands outside temporary directories. Prefer targeted deletes, dry runs, and listing paths first.
- Do not drop databases, destroy containers/volumes, or remove cloud/infrastructure resources unless the user explicitly approves the exact target and action.

Prefer safe alternatives:

- Inspect first with `git status`, `git diff`, `git clean -nd`, and `ls`.
- Preserve work with commits, branches, patches, or stashes before risky operations.
- Use dry-run flags whenever available.

If `dcg` blocks a command, treat the block as authoritative. Explain the safer alternative rather than trying to bypass it.
