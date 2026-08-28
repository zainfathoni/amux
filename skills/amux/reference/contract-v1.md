# amux contract-v1

Compatibility protocol for a pre-cutover Amux-managed spawn, adoption, or durable group flow. A worker may **read this file once** from the **absolute path** in its already-bound legacy assignment (the on-disk `reference/contract-v1.md` of the installed `/amux` skill—never a bare relative path) only after exact persisted provenance proves that flow is drain-eligible. Ambiguous, unrecorded, or newly created work cannot use this contract. Coordinators and wake-ups must **not** paste this text into prompts or reload the full `/amux` skill body.

Native Amp `create_thread` work never reads this file and never receives its path. It uses a lean task prompt and native parent/reply routing only, with no Amux receipt, report, callback, adoption, group, deadline, or finish-authorization requirement. A native child is a thread, not an Amux spawned worker.

Version marker: `amux-contract: v1`.

## Identities

- Worker identity is the canonical Amp `--thread`. Runner identity is the canonical `--workdir`. `--workspace` is a lifecycle group (same-named tmux session).
- Do not edit `workers.tsv`, `runners.tsv`, `shelves.tsv`, `groups.tsv`, or `reports.json` when a CLI command exists.
- Prefer long selectors, `--dry-run`, and `--json`. Exit `2` = preflight rejection (no mutation). Exit `1` = runtime failure after mutation may have begun. Lock contention is exit `2`: retry the **identical** operation.

## Admission and invocation fence

Amp modes are capability presets whose model routing and economics can change with connected subscriptions, workspace restrictions, and availability. Do not infer cost from an old model mapping.

1. Verify the exact existing spawn assignment, adoption operation, or group/member/report binding and its pre-cutover provenance before applying any instruction below. Continue only its exact next drain transition. Never create a replacement identity or add lifecycle scope.
2. Retain the already-bound mode and task. Do not use this contract to select or create a new native child. A bounded schema-2 projectless-host exception also receives no contract path or lifecycle instructions because it is not pre-cutover drain state.
3. **Do not Read Thread** (or otherwise load Amp thread transcripts/history) for task context. A thread URL is provenance, not approval. Exception: one narrow query of one exact related thread only during an authorized `/amux` lifecycle/coordination recovery after a named local/GitHub discrepancy, exhausted deterministic evidence, and proven relationship—then block rather than chain.
4. **Oracle must not Read Thread.** Supply issue intent + current diff (and other needed files) directly. Do not ask Oracle to read Amp threads, and do not Read Thread “to prepare Oracle.” Owner may explicitly allow one named thread read; that still does not authorize Oracle to fetch threads itself.

Before an exact legacy child message or Read Thread, also follow [`amp-invocation-policy.md`](amp-invocation-policy.md) when that reference is loaded. Mechanical enforcement is incomplete: treat these rules as binding instructions even when tools do not block.

## Worker duties

- Own only the already-bound issue, branch, and worktree. Report overlap; do not absorb foreign scope or accept a new task under the legacy identity.
- Follow only lifecycle identities and obligations present in the proven pre-cutover records. Do not manufacture a missing receipt, report, callback, group, deadline, adoption, or finish authorization.
- If an existing merged-PR report binding is present, stay alive after every report status until its exact durable finish authorization and direction. Do not merge, release, tag, teardown, unpin, or finish that merged-PR flow without them. An exact review-only assignment may instead finish from explicit owner completion and cleanup approval; retain any review-only report unchanged and do not reinterpret it as merge or terminal-report authority.
- For already-bound merged-PR work-group members, use the exact stable `--report-id` and immutable group/thread/issue/reference binding:
  - `blocked` — remaining blocker; `--pr none` when no PR exists
  - `ready` — implementation, focused tests/checks, one focused review and fixes, PR, and normal CI are complete; requires a PR URL
  - `merged` — only after durable finish authorization; same binding and payload; terminal
- An existing callback or wake-up token is notification only. It is not report delivery, acknowledgement, verification, merge, or finish authority.
- `/amux finish` applies only to an exact existing pre-cutover worker, after independently verified merged-PR completion or exact review-only completion evidence **and** explicit owner direction. It uses one complete read-only preflight and one exact approval, parks a verified live worker before filesystem mutation, fails closed on dirty/shared/runner-owned/process-ambiguous state, preserves the local branch, and never force-removes a worktree.

## Coordinator duties

- For an already-bound durable group, its stores are authoritative: `amux group …`, `amux report pending/history`, not tmux text or child summaries.
- For an already-bound pre-finish `blocked` or `ready` report, acknowledge receipt separately from verification. Independently verify PR URL/head/scope/mergeability/closing issue, worktree, review, and required CI before merge. The already-authorized terminal `merged` transition inside finish needs exact durable history, not a new acknowledgement round trip.
- After an authorized merge in an already-bound merged-PR report flow, verify post-merge CI (and Pages when triggered). Only then `amux report authorize-finish`. Ready, blocked, notification, acknowledgement, deadline expiry, and late callbacks never authorize finish. Durable coordinator authorization must exist before `/amux finish` begins; finish may submit only the exact already-authorized terminal `merged` transition after parking the worker. This requirement does not manufacture a report or terminal transition for review-only work.
- Direct `/amux finish` explicitly only when the existing lifecycle requires it and any bound merged-PR report is already durably authorized or terminal. Do not create authorization, report/provider/retirement state for cleanup; preserve existing group/report/provider evidence, keep branch deletion separate and opt-in, never force-remove a worktree, and never auto-release.
- Do not paste protocol essays into worker messages. A proven legacy drain assignment carries only its existing IDs and acceptance criteria, plus one line with the absolute contract path when that existing worker still needs it.

## Wake-ups

- An already-bound report wake-up is exactly `AMUX_REPORT group=<group> report=<id>` plus Enter. Then re-read pending/history.
- Deadline firings follow [`deadline-v1.md`](deadline-v1.md) only for an existing pre-cutover deadline. Do **not** load full `/amux` on schedule fire.

## Safety

- No secrets in names, workdirs, or thread IDs. Prefer temporary `--config-dir` for tests.
- On partial failure, inspect JSON outcomes and external state before retrying. Do not duplicate threads, windows, worktrees, or operation keys.
- Never guess a missing/recycled callback pane. Callback failure leaves the report pending and the worker alive.
- Runner commands never own remote agent threads. Teardown never applies to runners.
- Experimental Tycho, Claude, or Pi execution is **not** this contract. Use `/amux-tycho`, `/amux-claude`, or `/amux-pi` only after an explicit owner request for that exact skill.
