# Snippet: Global AGENTS workflow defaults

Paste into **Amp → Settings → Advanced → Global AGENTS.md** and/or
`$HOME/.config/amp/AGENTS.md`. Keep this short. Do **not** @-include skill
reference files (that defeats progressive disclosure). This repository file is
only a copyable policy snippet: editing it does not update the live web Global
AGENTS.md or any machine-local copy.

```markdown
# Workflow defaults

- Use native Amp `create_thread` for ordinary delegated work. Select the exact Workspace Project/Orb or exact live runner/workdir, then retain the native parent/reply route; do not fall back to Amux creation when native creation is unavailable or indeterminate.
- Keep native child prompts lean: task, acceptance criteria, relevant context and constraints, validation, and expected result. Never include an Amux `reference/contract-v1.md` path or require an Amux receipt, report, callback, adoption, group, deadline, or finish authorization.
- Apply Amux contract-v1 and lifecycle instructions only when exact persisted records prove an existing pre-cutover Amux-managed spawn, adoption, or group flow is drain-eligible. Ambiguous or newly created work does not qualify.
- Generalized Amux spawn admission is closed. Call native-created work a child or thread, not an Amux worker or spawned worker, and never automatically adopt it.
- When my ChatGPT subscription is linked and the target mode is available, choose Amp `low` for small mechanical tasks, `medium` for ordinary implementation, and `high` for hard architecture, debugging, or review. If routing or availability is unknown, use `medium`. Keep `ultra`, plugin, and other premium/special modes explicit-only.
- Do not Read Thread/history unless I explicitly approve that exact thread; a URL alone is not approval. For Oracle, supply diff/context only.
- Provider execution is outside Amux's maintained core. Tycho may own machine/provider routing for Claude Code and Pi/Codex Spark.
- `/amux-tycho` is the experimental explicit-only report bridge; the real Amp thread remains coordinator and consume/ack authority, while Tycho receives report-only authority.
- Forgex is experimental and requires my explicit request.
- `/amux-claude` and `/amux-pi` remain experimental fallback/reference paths and require my explicit request.
- For a proven pre-cutover drain only, wake-ups are tokens and durable state comes from the existing `amux group` / `amux report` records; load `reference/deadline-v1.md` only for an already-bound deadline firing, never for native work.
```
