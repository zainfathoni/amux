---
status: proposal-awaiting-owner-review
issue: 254
base: e4e1bba734fb1acd112676f4250653443b1c503f
promotion: not-authorized
---

# Issue 254 fresh-Orb mutating Claude Opus workflow

This document specifies a bounded mutating Claude Opus workflow for a fresh Amp Orb, as requested by [issue #254](https://github.com/zainfathoni/amux/issues/254). It is a design specification only. It does not implement the workflow, authorize a mutating Orb launch, or promote fresh-Orb delegation to generic use. It extends the read-only [`claude-opus-orb-executor.md`](../../skills/amux/reference/claude-opus-orb-executor.md) recipe with a distinct Orb lifecycle and does not reuse local tmux receipt or interactive-parking semantics by analogy.

The scope is exactly one explicitly requested bounded repository mutation by exact `claude-opus-4-8` in a fresh Orb, returning independently verifiable commit evidence to the originating coordinator while keeping delivery, acknowledgement, archive, and cleanup as separate decisions. It preserves the `stop/narrow` posture recorded for the local mutating pilots and adds no autonomous admission.

## Conclusion

A fresh-Orb mutating workflow is designable as a strict superset of the existing read-only Orb recipe plus a durable commit-handoff and lifecycle contract. It is not authorized for implementation by this proposal, and generic issue delegation remains disabled until the promotion gate below is met.

Three properties separate this workflow from the local Darwin Stage A path and from the read-only Orb recipe:

1. **Orb identity is not tmux identity.** A disposable Orb has its own thread, workspace, and disposal semantics. Local tmux receipts and local interactive parking are not evidence of Orb execution, persistence, or cleanup.
2. **The commit must survive the Orb.** A mutating result is worthless unless its commit-bearing evidence is durably exported to the coordinator before the Orb is disposed. Text patches are not accepted.
3. **Every lifecycle stage is a distinct, separately gated event.** CLI support, authentication, entitlement, availability, capacity, charge route, launch provenance, semantic completion, process termination, durable delivery, owner acknowledgement, Orb archival, and workspace cleanup are twelve distinct outcomes; none implies the next.

## Scope and non-claims

This proposal covers:

- durable Orb receipt persistence or export before disposal;
- exact `claude-opus-4-8` binding and exact owner/coordinator authority binding;
- the one-clean-commit or clean-zero-commit blocked handoff;
- commit-bearing artifact transfer and independent coordinator verification;
- headless process termination/absence versus local interactive parking;
- durable delivery separated from owner acknowledgement;
- acknowledgement-gated Orb archive and destructive workspace cleanup, with cleanup failure durable and non-successful;
- interruption and duplicate-safe replay across the full lifecycle;
- synthetic and curated real-pilot evidence; and
- explicit promotion gates and non-goals.

It does not:

- implement any workflow, helper, skill, or CLI change;
- authorize a mutating Orb launch or a pilot run;
- grant Claude push, PR, merge, release, issue-mutation, secret, infrastructure, archive, cleanup, or recursive-delegation authority;
- broaden [#205](https://github.com/zainfathoni/amux/issues/205) or [#207](https://github.com/zainfathoni/amux/issues/207) implicitly;
- claim Orb disposal revokes the project OAuth secret or any provider-side token;
- integrate Pi or a provider-neutral executor; or
- supersede the local pilots' `stop/narrow` decision.

## Verified starting point

At the recorded base:

- The read-only Orb recipe in [`claude-opus-orb-executor.md`](../../skills/amux/reference/claude-opus-orb-executor.md) provisions a fresh Orb with `CLAUDE_CODE_OAUTH_TOKEN` as a project secret, runs a fail-closed credential/auth preflight, pins exact `claude-opus-4-8`, selects one tool profile, enforces process/output bounds, validates single-key `modelUsage`, and reports through native Amp messaging. Mutation is unavailable by default in that recipe.
- The [`claude-opus-result-validator`](../../skills/amux/reference/claude-opus-orb-executor.md) already enforces the single-key `modelUsage`, turn-bound, permission-denial, and result-shape checks reused below.
- The local mutating reference [`claude-mutating-delegation.md`](../../skills/amux/reference/claude-mutating-delegation.md) already separates an exclusive-writer frozen handoff from Amp-owned preparation, validation, delivery, acknowledgement, and parking. Its handoff invariants are portable; its tmux/process and parking identity semantics are not portable to an Orb.
- The [#247 design grill](issue-247-mutating-opus-workflow-design-grill.md) established that autonomous capacity admission is externally blocked and that an unknown-capacity path requires one fresh, single-use owner acknowledgement. That result is preserved unchanged here.

## Workflow specification

### 1. Admission and exact authority binding

The coordinator admits at most one operation with all of these singular bounds: one origin thread, one fresh Orb, one repository, one immutable base SHA, one dedicated clean worktree/branch, one bounded task packet, one child, depth zero, and one attempt.

Model binding requires the literal identifier:

```text
claude-opus-4-8
```

Omission, aliases, normalization, a default model, provider substitution, model fallback, and automatic retry all fail before mutation authority is granted. Exact binding must cover request validation, the launch policy, the exact argv, the task/packet/launch digests, the immutable receipt, durable launch intent, startup and session-acquisition identity, report and frozen-handoff provenance, and any recovery revalidation. As in the read-only recipe, the enforcing evidence for exclusive model use is a single-key `modelUsage` of exactly `claude-opus-4-8` in the validated result; `--model` alone proves syntax only.

Authority binding is exact and asymmetric:

| Authority | Holder |
| --- | --- |
| Explicit trigger, task suitability, capacity acknowledgement | Amp coordinator |
| Fresh-Orb provisioning, launch-intent persistence, packet authoring | Amp coordinator |
| Bounded repository mutation and exactly one commit inside the dedicated worktree | Claude Opus in the fresh Orb (exclusive logical writer) |
| Artifact receipt, provenance/base/commit/diff/check/scope verification | Amp coordinator |
| Delivery confirmation, owner acknowledgement, integration, archive, cleanup | Amp coordinator and owner |

Claude gains no push, PR, merge, release, issue-mutation, secret-management, infrastructure, archive, cleanup, or recursive-delegation authority. Capacity authorization is separate from authentication and billing evidence: when the provider capacity pool, charge route, or maximum admitted impact is unsupported or ambiguous, the exact single-use owner acknowledgement from [#247](issue-247-mutating-opus-workflow-design-grill.md) is required, and a configured known floor violation remains non-overridable.

### 2. Durable Orb receipt: persist before authority, export before disposal

Launch intent is persisted **before** Claude receives mutation authority, so an Orb that never reports still leaves a durable coordinator-side record of what was attempted.

The receipt is privacy-safe and binds: origin thread, Orb thread, repository, base SHA, worktree/branch identity, task digest, executable path/version, normalized argv, exact model, authentication route class, and execution identity. It contains no secrets, prompts, transcripts, raw session metadata, tokens, or account identity.

Because the Orb workspace is disposable, the receipt must **survive or be durably exported before Orb disposal**. Survival lives on the coordinator side (durable launch-intent record) rather than only in the Orb's ephemeral root. The receipt records lifecycle-stage transitions so the coordinator can distinguish, for each stage, "not reached", "reached", and "reached-and-durably-recorded".

### 3. Handoff: one clean direct-child commit, or a clean zero-commit blocked baseline

The mutating result is exactly one of:

- **complete** — exactly one clean commit whose parent is exactly the declared immutable base SHA, inside the dedicated worktree/branch, with a clean working tree; or
- **blocked** — HEAD still equals the declared base SHA, zero commits beyond baseline, a clean worktree, and no changed artifacts, with a reported blocker.

Any dirty, divergent, multi-commit, ambiguous, or indeterminate repository state is **unresolved evidence**, not a handoff. The coordinator must not reset, stash, discard, or clean the Orb workspace to manufacture a valid handoff; normal repository hooks apply during the Orb's own commit.

```text
declared base ── exactly one clean child commit ──▶ complete
declared base ── zero commits, clean, no changes ──▶ blocked
declared base ── dirty / divergent / multi-commit ──▶ unresolved (not a handoff)
```

### 4. Commit-bearing artifact transfer and independent verification

The Orb transfers **commit-bearing evidence**, preferably an exact Git bundle (or an equivalent artifact) carrying source and base metadata, via native Amp file transfer. An unverified text patch is not accepted, because it cannot independently prove parentage, tree identity, or authorship.

The bundle metadata declares at least: repository, declared base SHA, branch/ref, the single commit SHA, and the Orb/origin identities that match the receipt.

The originating coordinator then **independently verifies**, from the transferred artifact rather than from Claude's narrative:

1. **provenance** — the artifact's declared origin, Orb identity, and model match the durable receipt;
2. **base** — the commit's parent is exactly the declared immutable base SHA;
3. **commit** — exactly one commit is present and its object verifies from the bundle;
4. **diff** — the applied change matches the reported scope and touches only permitted paths;
5. **checks** — the repository's own risk-appropriate checks (for a Go change, `gofmt` and `go test ./...` per the CI workflow; for a docs change, the applicable documentation checks) pass on the reconstructed commit; and
6. **scope** — no push, PR, merge, release, issue mutation, secret access, or out-of-worktree effect occurred.

Verification failure at any step keeps the result non-integrated and is reported as such. Verification is a coordinator action; a successful verification is not an integration, acceptance, or merge decision.

### 5. Headless termination is not local interactive parking

A headless Orb process that exits is **terminated/absent**, not "parked". Local interactive parking (keeping a live client attached to a reusable local session) has no analogue in a disposable Orb and must not be recorded as if it did.

The receipt records exact terminal or absence evidence separately from semantic completion:

- **semantic completion** — a valid report/artifact was produced;
- **process termination/absence** — the headless process has exited or is provably gone;
- neither implies the other: a terminated process may have produced no report, and a produced report does not by itself prove the process is gone.

Do not label a headless exit as parking, and do not treat a local tmux/interactive-parking receipt as proof of Orb process absence.

### 6. Durable delivery is separate from owner acknowledgement

Report and artifact **delivery** to the origin is durable and precedes any notification. **Owner acknowledgement** is a distinct, later event.

```text
durable delivery ─▶ notification ─▶ owner acknowledgement
(state: delivered)                  (state: acknowledged)
```

A delivered-but-unacknowledged result is a valid, durable resting state. Notification failure does not undo delivery. Acknowledgement is never inferred from delivery, from a read notification, or from elapsed time.

### 7. Acknowledgement-gated archive and destructive cleanup; cleanup failure is durable non-success

Orb thread **archive** and **destructive workspace cleanup** occur only after owner acknowledgement and a fresh revalidation of identity and state at cleanup time. Cleanup is never automatic and never part of the handoff.

Before any destructive workspace cleanup, the coordinator inspects the dedicated worktree and repository status and never discards unexpected changes automatically; unexpected evidence is surfaced, not erased.

A cleanup failure is a **new factual outcome that is durable and non-successful**: the coordinator preserves the exact failed target and sends a bounded follow-up rather than treating the earlier delivered report as proof of cleanup. Archive or cleanup success is recorded as its own stage transition.

Orb disposal, thread archive, or workspace cleanup does **not** remove or revoke the Amp project secret and is not evidence of provider-side token revocation. Secret rotation is a separate owner action outside this workflow.

### 8. Interruption and duplicate-safe replay across the full lifecycle

Every lifecycle stage — launch, report/artifact transfer, delivery, acknowledgement, archive, and cleanup — must be interruption-safe and duplicate-safe. An interruption between any two stages resumes from the last durably recorded stage without losing receipt, report, commit, or cleanup state, and without fabricating a skipped stage.

Each stage keys on the immutable operation identity (origin, Orb, repository, base SHA, worktree/branch, task digest) so a replayed event is recognized and made idempotent rather than re-executed:

| Stage | Replay rule |
| --- | --- |
| Launch | A second launch for the same identity is rejected; one attempt only. |
| Report/artifact transfer | A re-received artifact with matching identity and commit SHA is deduplicated, not re-verified as new. |
| Delivery | Re-delivery is idempotent; it does not reset acknowledgement state. |
| Acknowledgement | A replayed acknowledgement for an already-acknowledged operation is a no-op; it is single-use and cannot cross operations. |
| Archive | Archiving an already-archived Orb is a no-op. |
| Cleanup | Re-running cleanup on a cleaned workspace is a no-op; a prior recorded cleanup failure stays non-success until a fresh cleanup succeeds. |

A duplicate never advances state past its stage, and a replay never manufactures semantic completion, delivery, acknowledgement, or cleanup that did not durably occur.

### 9. Test and pilot evidence

**Synthetic tests** (no live provider call) must cover: model mismatch; changed base, worktree, or Orb identity; duplicate events at each stage; interrupted launch; process timeout/termination; report and artifact delivery replay; acknowledgement replay; transfer corruption of the bundle; dirty or divergent handoff; archive-before-acknowledgement rejection; cleanup failure; and receipt survival across Orb disposal.

**Real-pilot evidence** is curated and gated:

- Complete the remaining [#205](https://github.com/zainfathoni/amux/issues/205) current-version exact headless Opus evidence first or in parallel.
- Run one curated real pilot only after the [#253](https://github.com/zainfathoni/amux/issues/253) Darwin pilot passes and the owner supplies a separate exact pilot authorization.
- A successful pilot is evidence, not generic promotion. Pilot artifacts are privacy-reviewed before any inclusion in the repository.

### 10. Promotion gates and non-goals

Generic issue delegation to fresh Orbs remains **disabled** until all of the following exist:

- at least two useful complete runs on independent fresh Orbs;
- one clean blocked or non-complete run;
- one real or faithfully injected interruption recovered without losing receipt, report, commit, or cleanup state;
- mechanically enforced single-child, depth-zero, time, output, and tool bounds;
- duplicate-safe launch, handoff, acknowledgement, archive, and cleanup; and
- an explicit owner `promote`, `repeat`, or `stop/narrow` decision naming the supported image, CLI range, exact model, task class, and limits.

**Non-goals:**

- broadening [#205](https://github.com/zainfathoni/amux/issues/205) or [#207](https://github.com/zainfathoni/amux/issues/207) implicitly;
- reusing local tmux or process receipt semantics by analogy;
- autonomous issue selection, recursive delegation, model routing, push, PR, merge, release, or automatic cleanup;
- claiming Orb disposal revokes project OAuth secrets; and
- Pi or provider-neutral executor integration.

## Lifecycle summary

```text
persist launch intent (receipt bound, before authority)
  → provision fresh Orb + fail-closed preflight
  → exact claude-opus-4-8 mutation inside dedicated worktree
  → one clean child commit  |  clean zero-commit blocked
  → export commit-bearing bundle (survives Orb disposal)
  → coordinator verifies provenance/base/commit/diff/checks/scope
  → record headless termination/absence (not parking)
  → durable delivery → notification → owner acknowledgement
  → acknowledgement-gated archive + destructive cleanup
       (cleanup failure = durable non-success)
```

## ADR decision

No ADR is added. This proposal is a specification for an experimental, externally gated workflow; no hard-to-reverse implementation or stable public interface is accepted. If a fresh-Orb mutating path is later implemented and a stable interface is chosen, that trade-off may justify an ADR at that time.

## Public references

- [Issue #254](https://github.com/zainfathoni/amux/issues/254)
- [Fresh-Orb exact-Opus recipe, issue #205](https://github.com/zainfathoni/amux/issues/205)
- [Advisory multi-provider orchestration, issue #207](https://github.com/zainfathoni/amux/issues/207)
- [Local Darwin Stage A, issue #253](https://github.com/zainfathoni/amux/issues/253)
- [Mutating Opus workflow design grill, issue #247](issue-247-mutating-opus-workflow-design-grill.md)
- [Mutating Pilot 2 evaluation, issue #151](issue-151-mutating-claude-pilot-2-evaluation.md)
- [Read-only Orb executor recipe](../../skills/amux/reference/claude-opus-orb-executor.md)
- [Local mutating delegation reference](../../skills/amux/reference/claude-mutating-delegation.md)
