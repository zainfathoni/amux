# Snippet: Global AGENTS workflow defaults

Paste into **Amp → Settings → Advanced → Global AGENTS.md** and/or
`$HOME/.config/amp/AGENTS.md`. Keep this short. Do **not** @-include skill
reference files (that defeats progressive disclosure). This repository file is
only a copyable policy snippet: editing it does not update the live web Global
AGENTS.md or any machine-local copy.

```markdown
# Workflow defaults

- Use native Amp thread creation when available. Use Amux for local Amp/tmux lifecycle, exact adoption/recovery, and when native creation is unavailable; do not invent parallel orchestrators.
- Use `--mode medium` unless I explicitly choose otherwise; never use `low` unless I name it.
- Do not Read Thread/history unless I explicitly approve that exact thread; a URL alone is not approval. For Oracle, supply diff/context only.
- Keep child prompts concise: task, acceptance criteria, and the absolute path to the loaded `/amux` skill's `reference/contract-v1.md`; never paste protocols or send a bare relative path.
- Provider execution is outside Amux's maintained core. Tycho may own machine/provider routing for Claude Code and Pi/Codex Spark.
- Forgex is experimental and requires my explicit request.
- `/amux-claude` and `/amux-pi` remain experimental fallback/reference paths and require my explicit request.
- Wake-ups are tokens only; durable state comes from the `amux group` / `amux report` CLI, not a skill reload.
- Load the `/amux` skill's `reference/deadline-v1.md` only when arming or handling deadline firings—not full `/amux`.
```
