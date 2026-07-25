# amux contract-v1

Durable Amp worker and coordinator protocol. Spawned workers must **read this file once** at start from the **absolute path** the coordinator substituted into the assignment (the on-disk `reference/contract-v1.md` of the installed `/amux` skill—never a bare relative path), then follow only this contract plus their task assignment. Coordinators and wake-ups must **not** paste this text into child prompts or reload the full `/amux` skill body.

Version marker: `amux-contract: v1`.

## Identities

- Worker identity is the canonical Amp `--thread`. Runner identity is the canonical `--workdir`. `--workspace` is a lifecycle group (same-named tmux session).
- Do not edit `workers.tsv`, `runners.tsv`, `shelves.tsv`, `groups.tsv`, or `reports.json` when a CLI command exists.
- Prefer long selectors, `--dry-run`, and `--json`. Exit `2` = preflight rejection (no mutation). Exit `1` = runtime failure after mutation may have begun. Lock contention is exit `2`: retry the **identical** operation.

## Credit and invocation (default deny)

These burn Amp credits quickly (e.g. GLM on `low` / Read Thread). **Default deny** unless the owner gives **explicit permission in this thread** for that exact action.

1. **Do not use `low` mode** for automatic or skill-driven work. Default and automatic spawn mode is `medium` only. Never infer `low`, `high`, or `ultra`. Use another mode only when the owner explicitly names it.
2. **Do not Read Thread** (or otherwise load Amp thread transcripts/history) for task context. A thread URL is provenance, not approval. Exception: one narrow query of one exact related thread only during an authorized `/amux` lifecycle/coordination recovery after a named local/GitHub discrepancy, exhausted deterministic evidence, and proven relationship—then block rather than chain.
3. **Oracle must not Read Thread.** Supply issue intent + current diff (and other needed files) directly. Do not ask Oracle to read Amp threads, and do not Read Thread “to prepare Oracle.” Owner may explicitly allow one named thread read; that still does not authorize Oracle to fetch threads itself.

Before automatic spawn, native child creation, Read Thread, or native child messaging, also follow [`amp-invocation-policy.md`](amp-invocation-policy.md) when that reference is loaded. Mechanical enforcement is incomplete: treat these rules as binding instructions even when tools do not block.

## Worker duties

- Own only the assigned issue, branch, and worktree. Report overlap; do not absorb foreign scope.
- Skill-driven spawn uses `--mode medium` unless the user explicitly requested another mode. Never infer `low`, `high`, or `ultra`.
- Stay alive after every report status. Do not merge, release, tag, teardown, unpin, or finish without explicit durable authorization and direction.
- For work-group members, use the exact stable `--report-id` and immutable group/thread/issue/reference binding:
  - `blocked` — remaining blocker; `--pr none` when no PR exists
  - `ready` — implementation, focused tests/checks, one focused review and fixes, PR, and normal CI are complete; requires a PR URL
  - `merged` — only after durable finish authorization; same binding and payload; terminal
- A callback or wake-up token is notification only. It is not report delivery, acknowledgement, verification, merge, or finish authority.
- `/amux finish` only after independently verified merge **and** explicit coordinator/owner direction. Finish fails closed on unexpected runner ownership of the worktree.

## Coordinator duties

- Durable stores are authoritative: `amux group …`, `amux report pending/history`, not tmux text or child summaries.
- Acknowledge receipt separately from verification. Independently verify PR URL/head/scope/mergeability/closing issue, worktree, review, and required CI before merge.
- After authorized merge, verify post-merge CI (and Pages when triggered). Only then `amux report authorize-finish`. Ready, blocked, notification, acknowledgement, deadline expiry, and late callbacks never authorize finish.
- Direct `/amux finish` explicitly. Never force-delete a branch, auto-release, or erase group/report history during finish.
- Do not paste protocol essays into worker messages. Task assignments carry IDs and acceptance criteria only, plus one line to read this contract once.

## Wake-ups

- Report wake-up is exactly `AMUX_REPORT group=<group> report=<id>` plus Enter. Then re-read pending/history.
- Deadline firings follow [`deadline-v1.md`](deadline-v1.md) only when deadlines are in use. Do **not** load full `/amux` on schedule fire.

## Safety

- No secrets in names, workdirs, or thread IDs. Prefer temporary `--config-dir` for tests.
- On partial failure, inspect JSON outcomes and external state before retrying. Do not duplicate threads, windows, worktrees, or operation keys.
- Never guess a missing/recycled callback pane. Callback failure leaves the report pending and the worker alive.
- Runner commands never own remote agent threads. Teardown never applies to runners.
- Experimental Claude or Pi delegation is **not** this contract. Use `/amux-claude` or `/amux-pi` only after an explicit owner request for those skills.
