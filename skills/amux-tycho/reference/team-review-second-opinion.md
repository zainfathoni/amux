# Authoritative Amp `/team-review` with one material Opus second opinion

Native Amp review is the default. Add one Tycho-routed Opus second opinion only when an independent judgment could change a high-impact conclusion—for example a disputed security, concurrency, data-integrity, or cross-boundary finding that Amp cannot settle confidently from direct evidence. Cost, prestige, generic complexity, or a routine request for “another review” is not materiality.

This is the ordinary lane for explicit owner-authorized use. It reuses the canonical [`tycho-report-bridge.md`](tycho-report-bridge.md) helper and receipt schema. It does not widen stable Amux core, promote `/amux-tycho`, alter closed [#323](https://github.com/zainfathoni/amux/issues/323), authorize route preparation implicitly, or give Tycho lifecycle or GitHub authority.

## Authority and privacy

| Principal | Authority |
| --- | --- |
| Amp reviewer/coordinator | Owns the first pass and final conclusion; binds the task/artifact; creates, consumes, and acknowledges the receipt; independently verifies every candidate; alone may reconcile an owned current-user PENDING review. |
| Owner | Authorizes the exact existing route or one prepared route, exact model (normally `claude-opus-5`), worktree, harness, project, and artifact mode. Publication and finish remain separate decisions. |
| Tycho producer | Reads the bounded artifact and submits one typed `complete` or `blocked` report with `authority: "report_only"`. It has no Amp, group, callback, finish, publication, cleanup, GitHub mutation, or provider-identity authority. |

Keep coordinator custody, abandonment capability, notification binding, and GitHub write credentials owner-private. Give Tycho only its immutable submit proof, state-directory path, bounded task, existing read access, and one-shot submit instruction. The helper's owner-only permissions protect against other OS users, not another process with the same UID; never expose owner directories or ask Tycho to search for them.

Reports use the existing bridge limits. `complete` requires `blockers: []`; `blocked` requires at least one blocker; both statuses require non-empty `verification`. Each finding is one independently checkable candidate. Exclude secrets, credentials, transcripts, pane dumps, provider logs, full diffs, and owner-private paths outside the reviewed worktree. Process exit, logs, prose, hooks, and Tycho state are not findings or delivery.

## Ordinary review flow

Perform these steps in order. A failed completion criterion activates a stop condition; it does not authorize fallback.

### 1. Finish the native Amp review

Review the exact candidate independently on this real Amp coordinator or one authenticated native `create_thread` child. Pin repository, PR, full head SHA, and full tree SHA. Record Amp's findings before any Tycho input.

**Complete when:** the native review conclusion and pinned artifact identity are recorded, with no Tycho candidates seen. If this conclusion has no material unresolved judgment, stop successfully and use native Amp review alone.

### 2. Preflight one exact route and artifact

Require explicit owner authorization for an exact existing route or one owner-authorized prepared route: host, Tycho project, agent, worktree, harness, and exact model. Confirm the readiness matrix still permits the route and live entitlement, capacity, model identity, and host suitability pass. Prefer an existing route.

If the owner authorized preparation, create exactly one dormant project/agent without provider execution, then immediately re-read and freeze its project key, agent key, initial task-message identity, workdir, harness, and model. Rejected, ambiguous, already-running, or indeterminate preparation stops without retry or alternate selection.

Choose exactly one artifact mode:

- `dual-local-attachment`: both clean worktrees resolve to the pinned full head and `HEAD^{tree}`.
- `immutable-remote`: the exact pinned commit/tree is available from a frozen source whose identity is recorded; every local comparison attachment has a recorded canonical path, remains clean, and is separately pinned by full HEAD/tree. The task tells Tycho to inspect that pinned commit/diff instead of treating worktree `HEAD` as the reviewed artifact.

For either mode, normalize every local remote to the PR repository, freeze canonical paths and route coordinates, and re-read the current remote PR head. Path equality alone is not identity, and `HEAD^{tree}` does **not** detect dirty index or worktree content.

**Complete when:** owner authorization, route readiness, canonical identities, artifact mode, full head/tree, frozen immutable source and comparison attachments when applicable, remote equality, and cleanliness are all exact and recorded. Any missing proof, alias, drift, unapproved route, or provider fallback stops before receipt creation.

### 3. Freeze the task and create the receipt

Write one bounded task containing the review question, acceptance criteria, report schema pointer, `report_only` boundary, producer-only submit instruction, and exact literal route/artifact fields: artifact mode, repository, PR, full head/tree, Amp and Tycho paths, agent, project, harness, model, and producer role. For `immutable-remote`, also include the frozen immutable-source identity and every local comparison attachment's canonical path and full HEAD/tree. Compute `task_digest` as SHA-256 of those exact task bytes. Any change to a frozen field requires a newly authorized receipt.

Create one immutable receipt with the canonical helper **before** provider execution. Bind the canonical Amp origin, correlation and producer identities, route/run/task identities, canonical Tycho workdir, task reference/digest, producer role, and exact `authority: "report_only"`. Keep coordinator and abandonment capabilities private.

**Complete when:** helper output reports `state: "created"`, the digest recomputes from the frozen bytes, and Tycho has only producer submit material. A create rejection or indeterminate route state stops; do not mutate bindings or create a fallback receipt.

### 4. Run once and require one durable report

Run the bounded task once through the separately authorized Tycho lifecycle. Require exactly one durable bridge-valid `complete` or `blocked` report. Application validity is assessed after `consume`. No fallback model, route, retry, log mining, or inferred provider result is allowed.

**Complete when:** the receipt is `valid_report` for the exact producer/session/run/task. A provider nearing stop should submit `blocked` while it can. Exit without submit is a terminal ordinary-lane stop: record “no Tycho finding,” preserve the receipt in its exact state for canonical recovery, and skip steps 5–6.

### 5. Revalidate, consume, and decide

Before `consume`, repeat the step 2 artifact proof and current PR-head check, including the frozen immutable source and every comparison attachment when applicable. If it fails, reject the application payload and make no GitHub mutation; the receipt may still be drained for bridge hygiene with that rejection recorded.

From the immutable Amp origin, explicitly `consume` a valid report and verify `delivered`. Enforce the application report invariants, then independently reproduce or reject **every** candidate against the pinned head. Tycho output is evidence to assess, never the review conclusion.

If the report is application-invalid, reject it as review input and continue to acknowledgement; it supplies no findings for a review write.

If verified findings will change a current-user PENDING review, load [`team-review-pending-reconciliation.md`](team-review-pending-reconciliation.md) before the first write. Publication remains separately authorized.

**Complete when:** every candidate is marked verified or rejected, any permitted PENDING mutation contains only Amp-verified findings, and no conclusion depends on provider prose outside the consumed report.

### 6. Acknowledge after handling

After assessment and any permitted reconciliation, issue a distinct `acknowledge` from the same Amp origin. Consumption is not acceptance; acknowledgement is not publication, finish, merge, or approval.

**Complete when:** the receipt is durably `acknowledged` and custody cleanup reports `removed`. Cleanup `pending` activates the canonical identical-event replay branch; retain the truthful blocker if it cannot be removed.

## Stop conditions

Stop and preserve exact state on any unapproved or ambiguous route, model alias/default/fallback, entitlement or host-readiness failure, artifact/head/tree/path drift, dirty attachment, malformed or conflicting receipt proof, missing submit, or wrong Amp origin. Never repair by rebinding, inventing Amp identity, mining logs, or resending notification.

## Branch references

- Receipt restart, notification uncertainty, no-submit state, lock contention, created-only custody loss, pre-split compatibility, and cleanup replay: [`tycho-report-bridge.md`](tycho-report-bridge.md#exceptional-notification-and-recovery). These branches activate only from the exact observed receipt state.
- PENDING ownership, canonical snapshot digest, per-write head revalidation, and fail-closed mutation: [`team-review-pending-reconciliation.md`](team-review-pending-reconciliation.md). Load only when Amp will write a PENDING review.
- Historical #327 disposition, #328 field-evidence package, PR #11886 gap mapping, and formal-promotion evidence: [`team-review-field-evidence.md`](team-review-field-evidence.md). Load only for an explicit field-evidence or promotion assessment.

Stable `cmd/`/`internal/` changes, live execution authorized by documentation alone, recurring watchers, multi-producer fan-out, automatic publication/finish/cleanup, and provider-route substitution are out of scope.
