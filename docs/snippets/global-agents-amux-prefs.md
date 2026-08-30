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
- Amux worker, spawn/adoption, group, report, callback, deadline, shelf, teardown, and finish routes are removed. Never revive them through an older binary, direct store edits, or synthetic compatibility state.
- Call native-created work a child or thread, not an Amux worker or spawned worker, and never adopt it into Amux.
- When my ChatGPT subscription is linked and the target mode is available, choose Amp `low` for small mechanical tasks, `medium` for ordinary implementation, and `high` for hard architecture, debugging, or review. If routing or availability is unknown, use `medium`. Keep `ultra`, plugin, and other premium/special modes explicit-only.
- Do not Read Thread/history unless I explicitly approve that exact thread; a URL alone is not approval. For Oracle, supply diff/context only.
- Provider execution is outside Amux's maintained core. Only an explicitly requested exact existing Tycho route or one owner-authorized prepared route created without provider execution and bound before its first run may own internal machine/provider routing; the real Amp thread remains coordinator and consume/ack authority, while Tycho remains report-only.
- `/amux-tycho` is the experimental explicit-only report bridge. Its receipt admission remains open only until the authenticated direct structured-return gate passes; never run the bridge and direct route for the same task.
- Forgex is experimental and requires my explicit request.
- `/amux-claude` and `/amux-pi` remain experimental fallback/reference paths and require my explicit request.
- Historical worker/coordination files are inert evidence. Leave them byte-identical; only the separately owner-gated #360 inventory may inspect them read-only.
```
