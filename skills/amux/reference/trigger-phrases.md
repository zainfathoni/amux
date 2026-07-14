# amux skill trigger phrase checklist

This fixture keeps the bundled `/amux` skill vocabulary aligned with the behavior an agent should choose. Use it when editing [`../SKILL.md`](../SKILL.md), command references, workflow references, or examples that teach natural-language amux routing.

It is intentionally documentation-only: it does not exercise live tmux, Amp, GitHub, or local restore config.

## Checklist

- Keep each trigger phrase below represented in the `description` frontmatter of [`../SKILL.md`](../SKILL.md) when the phrase should load the skill directly.
- Keep each command mapping represented in the **Common trigger routing** section of [`../SKILL.md`](../SKILL.md).
- If a mapping needs exact CLI syntax or side-effect details, link to the disclosed reference instead of expanding the top-level skill.
- Preserve the side-effect domain in the expected behavior. Do not replace `pin` with `spawn`, `park` with `teardown`, or `teardown` with `park`.

| Trigger phrase | Expected routing | Must preserve | Reference |
| --- | --- | --- | --- |
| `Pin it` | `amux pin-current <thread-id-or-url>` | Mutates restore config only; ask for the current thread ID/URL if it is not available. | [`commands.md#pin-and-unpin`](commands.md#pin-and-unpin) |
| `Unpin it` | `amux unpin-current` | Removes the current pane from restore config only; does not stop tmux and does not archive the thread. | [`commands.md#pin-and-unpin`](commands.md#pin-and-unpin) |
| `forget this on restore` | `amux unpin-current` | Same behavior as `Unpin it`; this phrase means stop restoring later, not stop working now. | [`commands.md#pin-and-unpin`](commands.md#pin-and-unpin) |
| `Park it` | `amux park-current` | Stops only the verified live local tmux/Amp window; preserves restore row and active remote thread. | [`commands.md#park`](commands.md#park) |
| `Shelve this` | `amux shelve-current [workspace] <thread-id-or-url>` from the pane, or a verified row/thread/workspace `amux shelve ...` form for already-pinned work. | Defers work by archiving/hiding selected remote thread(s), preserving restore rows, and stopping only verified local windows; do not substitute `park-current`. | [`commands.md#shelve-and-unshelve`](commands.md#shelve-and-unshelve) |
| `defer this workspace` | Same as `Shelve this`, usually scoped with `amux shelve --workspace <workspace> [--session <session>]` for pinned rows. | Means hidden/deferred remote work, not merely local parking; preserve restore rows for explicit unshelve/launch later. | [`commands.md#shelve-and-unshelve`](commands.md#shelve-and-unshelve) |
| `hide it for now` | Same as `Shelve this`; choose `shelve-current` for the current pane or row/thread forms for existing restore rows. | Archive/hide the selected thread(s) from Amp while keeping amux restore intent; fail closed on ambiguous targets. | [`commands.md#shelve-and-unshelve`](commands.md#shelve-and-unshelve) |
| `Restore my workspace` | `amux launch`, `amux launch <workspace>`, or `amux launch <workspace> <session>` | Uses session-defaulting rules; does not create, archive, unarchive, or delete remote Amp threads. | [`commands.md#launch`](commands.md#launch) |
| `Spawn a worker for ...` | Load [`workflows.md#spawn-a-fresh-interactive-session`](workflows.md#spawn-a-fresh-interactive-session), then use `amux spawn --mode medium ...` for a fresh interactive local Amp/tmux worker unless the user explicitly requested another mode. | `spawn` creates a new Amp thread, tmux window, and restore row; the user's explicit mode always wins, but never infer `high` or `ultra` from complexity, size, urgency, or duration; do not use spawn for restoring, pinning, or Amp-native remote agent creation. | [`workflows.md#spawn-a-fresh-interactive-session`](workflows.md#spawn-a-fresh-interactive-session) |
| `/amux sprawl #12 #34 ...` | Load [`workflows.md#sprawl-independent-issue-workers`](workflows.md#sprawl-independent-issue-workers), inspect dependencies, then spawn each accepted worker with `amux spawn --mode medium --title-prefix '#<issue>' ...` unless the user explicitly requested another mode. | Every worker gets an explicit mode; do not infer a higher mode from issue complexity or expected duration. Sprawl remains skill-only worker orchestration, not an amux CLI command. | [`workflows.md#sprawl-independent-issue-workers`](workflows.md#sprawl-independent-issue-workers) |
| `Teardown this worker` | `amux teardown` from a spawned worker, or a verified explicit/thread form when identity is unavailable. | Full cleanup only: archive verified thread, remove restore row, and stop verified local window; fail closed on ambiguity. | [`workflows.md#tear-down-a-spawned-worker`](workflows.md#tear-down-a-spawned-worker) |
| `Doctor amux` | `amux doctor`, `amux doctor <workspace>`, or `amux doctor <workspace> <session>` | Inspect and report drift; do not mutate restore config, tmux, or remote thread state. | [`commands.md#list-shelved-and-doctor`](commands.md#list-shelved-and-doctor) |
| `/amux health <workspace>` | Load [`workflows.md#health-check-interactive-workers-before-replacement`](workflows.md#health-check-interactive-workers-before-replacement), then perform the skill-only health workflow. | Inspect amux/tmux state, ping only verified Amp panes with one submitted read-only prompt and unique tokens, classify `healthy` / `no-live-window` / `no-response` / `mismatched` / `ambiguous` / `shelved`, and report before any replacement; do not archive, unpin, park, kill, or spawn. | [`workflows.md#health-check-interactive-workers-before-replacement`](workflows.md#health-check-interactive-workers-before-replacement) |

## Drift check procedure

When reviewing a vocabulary change:

1. Confirm the trigger phrase still appears in the skill `description` when direct activation is intended.
2. Confirm top-level routing in [`../SKILL.md`](../SKILL.md) names the same command family as this checklist.
3. Confirm the linked reference documents the command form and side effects.
4. If behavior changed intentionally, update this checklist in the same commit so reviewers can see the new agent contract.
