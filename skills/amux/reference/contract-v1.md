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
- If an existing report binding is present, stay alive after every report status. Do not merge, release, tag, teardown, unpin, or finish without its explicit durable authorization and direction.
- For already-bound work-group members, use the exact stable `--report-id` and immutable group/thread/issue/reference binding:
  - `blocked` — remaining blocker; `--pr none` when no PR exists
  - `ready` — implementation, focused tests/checks, one focused review and fixes, PR, and normal CI are complete; requires a PR URL
  - `merged` — only after durable finish authorization; same binding and payload; terminal
- An existing callback or wake-up token is notification only. It is not report delivery, acknowledgement, verification, merge, or finish authority.
- `/amux finish` applies only when that lifecycle already exists, after independently verified merge **and** explicit coordinator/owner direction. Finish fails closed on unexpected runner ownership of the worktree.

## Coordinator duties

- For an already-bound durable group, its stores are authoritative: `amux group …`, `amux report pending/history`, not tmux text or child summaries.
- For an already-bound report, acknowledge receipt separately from verification. Independently verify PR URL/head/scope/mergeability/closing issue, worktree, review, and required CI before merge.
- After an authorized merge in that existing report flow, verify post-merge CI (and Pages when triggered). Only then `amux report authorize-finish`. Ready, blocked, notification, acknowledgement, deadline expiry, and late callbacks never authorize finish.
- Direct `/amux finish` explicitly only when the existing lifecycle requires it. Never force-delete a branch, auto-release, or erase group/report history during finish.
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
