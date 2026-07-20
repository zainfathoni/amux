# Delegate isolated mutating work to Claude experimentally

This is the separate mutating workflow and authority contract for the unstable, skill-owned Claude experiment. Use it only after an explicit request for mutating delegation and only while the public Pilot 1 decision remains `pass`. It does not run Pilot 2, add an `amux` lifecycle resource, broaden thinker authority, or authorize Claude to integrate, push, mutate GitHub, merge, release, park, clean up, or tear down anything.

Load [`claude-delegation-contract.md`](claude-delegation-contract.md) only for the common private receipt, exact incarnation, semantic delivery, acknowledgement, and explicit parking mechanics. Where this reference differs, its mutating writer and handoff rules are the authority boundary. Set `HELPER` to `experimental/claude-delegation/claude_delegation.py` within the installed skill. Requests are one JSON object on stdin; raw receipts, capacity observations, task packets, and operational evidence remain owner-only local data.

## Authority matrix and non-claims

The Claude **mutating delegate** receives one fresh interactive process and exclusive logical write ownership of one prepared worktree. Amp owns worktree/branch preparation, immutable baseline capture, integration, independent verification, GitHub actions, parking, cleanup, and teardown. Claude may edit and run checks in the bound worktree and may create or amend exactly one local commit beyond baseline before report submission. Claude may not push, alter a pull request, merge, release, stash, reset, force-clean, create/remove worktrees, delete branches, or clean up the delegation. The launch removes common forge-token environment variables and denies direct Git/GitHub/lifecycle command patterns, but this remains logical policy confinement rather than an OS sandbox; objective handoff validation cannot prove that no bypass or remote side effect occurred.

The worktree must not be shared writable. Receipt creation durably leases its canonical worktree identity to one unresolved mutating delegation, including across path or symlink aliases; a distinct receipt cannot reacquire that logical lease until the existing receipt reaches verified explicit parking. Amp freezes its own writes before preparation and does not reacquire write authority merely because a report exists. Report submission freezes Claude's logical writer authority and commit identity. Only Amp may independently revalidate the frozen handoff, consume and acknowledge the report, explicitly park the exact client, and then deliberately reacquire ownership. A valid report proves only objective report and Git shape. It never proves correctness, acceptance, merge readiness, or cleanup authority.

## 1. Decide capacity without autonomous guesses

Run `diagnose`, then load provisional owner-only machine reserve-floor configuration for the five-hour, weekly, and every applicable model-specific window. Do not invent defaults. Submit the bounded diagnostic capacity object, all configured floors, and `acknowledged_unknown_capacity:false` to `capacity decide-mutating`. The currently documented CodexBar usage payload has no source-contract or schema-version discriminator, so diagnostics classify it as unavailable and never manufacture those fields. Autonomous selection requires a separately established exact supported Claude provider, reported source, source-contract version, schema version, and bounded window shape; missing, extra, duplicated, malformed, non-finite, unbounded, stale, wrong-class, or unsupported input remains non-autonomous.

Every available window must have a floor, and every configured model-specific floor must have exactly one correctly classified bounded window. A reset is reliable only while it is strictly in the future and no later than one declared window duration from the decision's single UTC observation; every plan and execute re-decision checks this again. Remaining capacity is `100 - used_percent`; the smallest `remaining - floor` margin is the governing window. Any known negative margin blocks launch as a hard reserve violation even when provenance or another window is missing, unsupported, or low-confidence. Missing five-hour, weekly, applicable model-specific, or unsupported capacity returns `acknowledgement_required`, disables autonomous selection, and cannot proceed. Only the owner/coordinator's explicit acknowledgement of unknown reserve impact permits an identical decision request with `acknowledged_unknown_capacity:true` and `acknowledgement_of` set to that prior decision digest; that digest binds the rejected provenance, schema, windows, and floors exactly, and the resulting decision remains `autonomous_selection:false`. Bind the returned decision digest to the receipt.

Completion: the decision says `may_proceed:true`, every known available window remains at or above its floor, and either reliable data selected the tightest window or explicit acknowledgement—not autonomous inference—accepted unknown impact.

## 2. Prepare and bind the immutable baseline

Amp creates one dedicated linked worktree and branch at the intended baseline. Submit `mutation prepare` with its canonical path and repository plus exactly:

```json
{
  "writer_owner": "claude_mutating_delegate",
  "integration_owner": "amp_coordinator",
  "coordinator_write_frozen": true,
  "shared_writable": false,
  "handoff": "one_clean_local_commit"
}
```

Preparation rejects a primary or dirty worktree, detached/ambiguous branch, repository mismatch, shared writable state, ambiguous ownership, or any handoff other than one clean local commit. Create the receipt with `producer_role:mutating_delegate`, `authority:exclusive_writer`, the returned baseline commit/branch and ownership values, the capacity decision digest, the common correlation/launch digests, and `origin_thread` equal to the canonical identity of the Amp worker that explicitly owns the delegation. Never infer that lifecycle owner from worktree, branch, names, PID, issue number, tmux placement, or Claude session ID. Exact replay may return `duplicate`; conflicting baseline or ownership reuse fails closed.

Completion: the private receipt immutably binds the clean dedicated worktree, branch, baseline, exclusive writer, Amp integration owner, one-commit handoff, and capacity decision before launch.

## 3. Plan, launch, and acquire the explicit writer route

Create an owner-only self-contained packet that states the authority matrix, baseline, branch, one-commit or zero-commit-blocked outcomes, submission freeze, normal hook policy, privacy exclusions, and prohibited Git/GitHub/lifecycle actions. Use only approved public repository sources and independently safe summaries. Call `launch plan` with `workflow:mutating`, the original capacity observation/floors/acknowledgement request, and the exact preparation fields. The helper recomputes and returns the capacity decision; callers cannot supply a preconstructed launch decision. The mutating policy is distinct from the read-only thinker policy and must report `writer_authority:exclusive`.

After creating the matching receipt, call `launch execute` with the identical request and then `session acquire` for the exact returned pane and Claude session. Under the final receipt-store mutation lock immediately before durable launch intent, launch execution rechecks the canonical worktree, repository, branch, HEAD, cleanliness, exact receipt lease ownership, and receipt digests. An interrupted launch intent remains indeterminate and must not be relaunched automatically.

Completion: one receipt-bound fresh Claude incarnation owns only the declared worktree edits; Amp remains sole integration and lifecycle authority.

## 4. Submit only a clean one-commit handoff or clean zero-commit blockage

Claude may edit, run declared checks, and create/amend its local commit. Normal repository hooks apply. The semantic `mutating_report` must disclaim correctness, acceptance, merge readiness, and cleanup authority. A `complete` report names the exact handoff commit and changed artifacts. A `blocked` report names no commit and no changed artifacts.

`submit_report` derives the only permitted report kind from the immutable receipt role and requires the receipt's one completed and acquired mutating Claude session. It validates the current bound branch and clean status before durably recording the report and submission freeze. `complete` requires HEAD to be exactly one direct child of baseline and to match the reported commit. `blocked` requires HEAD to equal baseline with zero commits. Staged, unstaged, or non-ignored untracked files; missing, multiple, divergent, mismatched, or wrong-branch commits; and dirty blocked attempts remain unresolved for Amp inspection. Claude must not reset, discard, or clean evidence to manufacture validity.

Completion: the receipt contains one frozen `valid_report` with objective handoff validation, or remains unresolved without claiming a handoff.

## 5. Independently validate, deliver, acknowledge, and park

Before consumption, Amp calls `mutation validate-handoff` for the exact delegation. This reruns objective Git validation against the frozen report; any post-freeze write or commit change fails closed. Amp then independently reviews the commit and runs risk-appropriate verification without treating report validity as acceptance.

Use the common receipt ordering unchanged: optional wake-up only after durable report storage; machine-local inbox consumption reruns and compares frozen handoff validation before recording `delivered`; separate acknowledgement records receipt; explicit identity-verified parking is allowed only after acknowledgement and unresolved input handling. Notification is never delivery. Report validity, delivery, acknowledgement, callback, or parking never authorizes integration, cleanup, worktree removal, branch deletion, worker teardown, merge, release, or another pilot.

Completion: the exact frozen handoff remains objectively valid through Amp revalidation, then the existing durable delivery and acknowledgement order is preserved. Parking occurs only by a separate explicit Amp decision. Keep the client and worktree intact on every unresolved or invalid path.

## Recovery boundaries

- Retry receipt and lifecycle events only with the same event ID and exact payload; exact replay is duplicate-safe and conflicting reuse performs no mutation.
- A capacity acknowledgement cannot override a known hard-floor violation or missing floor configuration.
- A crash after report commit leaves a recoverable frozen receipt; recover through the local inbox and revalidate the handoff before delivery.
- A dirty or unexpected handoff is evidence for Amp, not permission to reset, stash, clean, patch, or accept a dirty handoff.
- A restarted Claude process receives no writer ownership until Amp proves the prior writer inactive and explicitly prepares a new unambiguous authority boundary. No teleport or adoption path exists here.
- Never park automatically. Never infer correctness, acceptance, integration authority, or cleanup authority from a valid report.
