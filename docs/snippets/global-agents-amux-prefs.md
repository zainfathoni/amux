# Snippet: Global AGENTS workflow defaults

Paste into **Amp → Settings → Advanced → Global AGENTS.md** and/or
`$HOME/.config/amp/AGENTS.md`. Keep this short. Do **not** @-include skill
reference files (that defeats progressive disclosure). This repository file is
only a copyable policy snippet: editing it does not update the live web Global
AGENTS.md or any machine-local copy.

```markdown
# Workflow defaults

- Delegate through native Amp threads. Use the exact project or live runner/workdir and native reply route.
- If thread creation is unavailable, rejected, or indeterminate, stop and report it; do not retry or switch executors.
- Use `low` for mechanical tasks, `medium` for ordinary implementation, and `high` for hard architecture, debugging, or review. Other modes require my explicit request.
- Use Tycho only when I explicitly request it; follow the requested route's guidance and keep Amp as coordinator.
```
