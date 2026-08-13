# Snippet: Global AGENTS workflow defaults

Paste into **Amp → Settings → Advanced → Global AGENTS.md** and/or
`$HOME/.config/amp/AGENTS.md`. Keep this short. Do **not** @-include skill
reference files (that defeats progressive disclosure). This repository file is
only a copyable policy snippet: editing it does not update the live web Global
AGENTS.md or any machine-local copy.

```markdown
# Workflow defaults

- Use native Amp `create_thread` for ordinary delegated work. Select the exact Workspace Project for Orb execution, or the exact live runner/workdir, then retain the native parent/reply route.
- If native creation is unavailable, rejected, or indeterminate, stop. Do not retry, use an executor fallback, or create any Amux lifecycle state.
- Keep native child prompts lean: task, acceptance criteria, relevant context and constraints, validation, and expected result. Never include an Amux `reference/contract-v1.md` path or require an Amux receipt, report, callback, adoption, group, deadline, or finish authorization.
- Apply Amux contract-v1 and lifecycle instructions only when exact persisted records prove both an existing Amux-managed spawn, adoption, or group flow's pre-cutover admission and its exact allowed next drain transition. Ambiguous or newly created work does not qualify.
- Generalized Amux spawn admission is closed. Call native-created work a child or thread, not an Amux worker or spawned worker, and never automatically adopt it.
- When my ChatGPT subscription is linked and the target mode is available, choose Amp `low` for small mechanical tasks, `medium` for ordinary implementation, and `high` for hard architecture, debugging, or review. If routing or availability is unknown, use `medium`. Keep `ultra`, plugin, and other premium/special modes explicit-only.
- Do not Read Thread/history unless I explicitly approve that exact thread; a URL alone is not approval. For Oracle, supply diff/context only.
- Provider execution is outside Amux's maintained core. Only an explicitly requested existing Tycho route may own its internal machine/provider routing; the real Amp thread remains coordinator and consume/ack authority, while Tycho remains report-only.
- `/amux-tycho` is the experimental explicit-only report bridge. Its receipt admission remains open only until the authenticated direct structured-return gate passes; never run the bridge and direct route for the same task.
- Forgex is experimental and requires my explicit request.
- `/amux-claude` and `/amux-pi` remain experimental fallback/reference paths and require my explicit request.
- For a proven pre-cutover drain only, wake-ups are tokens and durable state comes from the existing `amux group` / `amux report` records; load `reference/deadline-v1.md` only for an already-bound deadline firing, never for native work.
```
