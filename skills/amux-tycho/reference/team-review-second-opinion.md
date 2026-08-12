# Authoritative Amp `/team-review` with one Opus second opinion

This `/amux-tycho` application workflow is the progressive-disclosure home for [#328](https://github.com/zainfathoni/amux/issues/328). It reuses the one canonical receipt helper and protocol in [`tycho-report-bridge.md`](tycho-report-bridge.md). It does not widen stable Amux core, formally promote `/amux-tycho`, alter closed [#323](https://github.com/zainfathoni/amux/issues/323), launch Tycho, or authorize a live field cycle from documentation alone.

The generic `/amux-tycho` route is practically usable under its explicit owner-authorized contract. This narrower #328 workflow remains blocked on #327's newly spawned local-worker assignment requirement. Keep a real Amp thread as coordinator and consume/acknowledgement authority.

## Policy categories

- **Route selection:** exact owner-selected host, Tycho agent/project/harness/model, and current availability determine where the review may run.
- **Task-specific validation:** repository, head, tree, worktree, and PENDING-review checks validate this code-review task; they are not generic receipt requirements.
- **Exceptional recovery:** provider stop without a report, notification uncertainty, custody loss, and cleanup replay use the bridge recovery procedure only when those failures occur.
- **Optional formal promotion:** multi-cycle, natural-failure, supported-ingress, privacy, and ADR evidence do not gate ordinary `/amux-tycho` use or #323 closure.

## Settled design decisions

These six decisions are the smallest safe contract for the open items in #328. They do not change the bridge schema version or pre-split receipt bytes. Where this application workflow is stricter than the generic bridge schema, Amp still consumes a bridge-valid report, then rejects application-invalid candidates or whole payloads during independent assessment—never by mutating the durable report.

### 1. Bounded `complete` / `blocked` finding schema and privacy/size limits

Reuse the existing canonical semantic report object. Do not invent a second report type or helper.

| Field | Bridge envelope (unchanged schema v1) | Application invariants for this workflow |
| --- | --- | --- |
| `status` | Exactly `complete` or `blocked` | Same. |
| `summary` | Non-empty string, ≤ 8192 UTF-8 bytes | No raw transcript, pane dump, provider log, or secret material. |
| `findings` | ≤ 32 non-empty strings, each ≤ 2048 UTF-8 bytes | Each item is one independently verifiable candidate. Encode path, symbol, scenario, mechanism, evidence SHA, test gap, and proposed fix in the string when present. Empty findings are allowed for a clean `complete` or a `blocked` stop with no partial candidates. |
| `blockers` | Same array bounds; non-empty required when `status` is `blocked` | **Application:** `complete` must use `blockers: []`. Non-empty blockers with `complete` is application-invalid: Amp consumes, rejects the payload as a finished second opinion, and may acknowledge after recording the handling. |
| `verification` | Same array bounds | **Application:** non-empty for both statuses. Record what was checked at the pinned head (commands, files, negative searches). Empty verification is application-invalid even if the bridge accepts it. |

Privacy and size limits are those already enforced by the bridge: 64 KiB stdin, 1 MiB store, 128 receipts, 16 events per receipt, unknown-field rejection, and no transcript field. Raw logs, Tycho state, process exit, provider prose, hook queues, and pane text are never delivery and never usable review input.

A useful finding shape for Amp verification, still encoded as one bounded string per candidate:

```text
[severity] path:symbol — scenario; mechanism; evidence at <full-or-short-sha>; test gap; proposed fix
```

Do not embed secrets, tokens, owner-private paths outside the reviewed worktree, full diffs, or multi-kilobyte excerpts.

### 2. Producer-only submit capability delivery

At `create`, the Amp coordinator keeps owner-private:

- coordinator token and coordinator-custody directory;
- abandonment token and abandonment-capability directory;
- any notification target binding;
- GitHub credentials or tokens capable of review/comment mutation.

The existing Tycho producer receives only:

- the exact immutable proof fields required by `submit` (`receipt_id`, `origin_thread`, `correlation_id`, `producer_nonce`, `tycho_agent_key`, nullable `claude_session_id`, `run_id`, `task_message_id`, canonical Tycho `workdir`, `task_reference`, `task_digest`, `producer_role`, exact `authority: "report_only"`);
- the owner-private **state-directory** path;
- the one-shot instruction to call the canonical helper's `submit` once with a typed `complete` or `blocked` report;
- read access to the reviewed Tycho worktree contents only as already present for that existing agent—no new GitHub write credentials, no `gh` auth for reviews, and no instruction to mutate GitHub.

The producer never receives coordinator custody, abandonment capability, consume/ack credentials, group finish authority, GitHub credentials intended for review mutation, or permission to search owner-private directories. `submit` and `show` reject owner-directory arguments. Optional group reference remains correlation metadata only.

### 3. Truthful blocked-report behavior near provider stop

While the provider can still act, the producer must prefer one durable `blocked` submit over silence when the second opinion cannot finish. A valid application `blocked` report is durable input: Amp may consume it, independently verify any partial findings, and must treat named blockers as incomplete scope—not as accepted defects.

If the provider stops, is killed, times out, or exits without a successful durable `submit`, that run contributes **no Tycho finding**. Exit codes (including `143`), raw logs, recovered Tycho state, and prose are not a report and must not be reconstructed into one. Retain the immutable receipt truthfully in `created` (or whatever state `show` reports) and do not invent recovery input.

Exact replay of an already-submitted report is duplicate-safe. Conflicting replay fails closed. There is no second submit with a different body under the same event ID.

### 4. Task-specific validation: same-head proof across Amp and Tycho worktrees

Path or directory-name equality is not identity. The bridge helper does **not** attest Git state; Amp (or the owner-operated coordinator) must prove attachment with ordinary `git`/`gh` reads outside the helper.

**When to prove (all mandatory):**

- **Pre-create / pre-Tycho** — before receipt `create` and before Tycho starts.
- **Post-Tycho / pre-consume** — after Tycho finishes (or fails to submit) and **before** `consume`, candidate acceptance, PENDING mutation, or any claim that the #328 cycle passed its task-specific checks.

Each proof requires all of:

1. **Reviewed repository identity** — both Amp and Tycho worktrees resolve to the same remote repository the PR belongs to (normalize `git remote get-url origin` to `owner/repo`; reject mismatch).
2. **Canonical workdir identity** — Amp worktree path and Tycho worktree path still equal the paths frozen in the task/receipt (Tycho path equals bound `workdir`). Path rename or substitute worktree fails closed.
3. **Pinned head object** — both worktrees report the same full 40-character commit SHA, equal to the owner-pinned reviewed PR head, via `git rev-parse HEAD`. Short SHAs are insufficient.
4. **Committed tree object identity** — both worktrees report the same `git rev-parse HEAD^{tree}` (full tree SHA), equal to the frozen tree. This binds the committed tree under `HEAD`; it does **not** detect dirty index or worktree content by itself.
5. **Clean attachment on both worktrees** — default policy: both Amp and Tycho review worktrees must be clean for tracked content (`git status --porcelain` empty for staged and unstaged tracked changes). This cleanliness check is what detects local dirt; untracked files block unless the owner explicitly records a bounded allowlist of untracked paths that cannot affect the reviewed tree. Any non-allowlisted dirt blocks.
6. **Distinct worktree paths are allowed** — Amp and Tycho paths may differ from each other. Bind the Tycho canonical absolute `workdir` into the receipt; never rebind to the Amp path or treat basename equality as proof.
7. **Route and head are frozen in the task digest** — the bounded task bytes MUST include, as exact literal fields: repository `owner/repo`, PR number, full head SHA, full tree SHA, Amp worktree path, Tycho worktree path, Tycho agent key, project, harness, model (normally exact `claude-opus-5`), `producer_role`, and `report_only`. `task_digest` is SHA-256 of those exact task bytes. Changing head, model, project, harness, agent, or either worktree path requires a newly authorized receipt, never mutation of the old one.

If any pre-create check fails, do not create a receipt and do not start Tycho. If any post-Tycho/pre-consume check fails (including read failure), **reject the application payload**: do not treat candidates as usable review input, mutate GitHub, or claim the #328 cycle passed—even if a durable `valid_report` exists. The receipt may still be consumed/acknowledged for bridge hygiene with an explicit “application-rejected: attachment drift” handling record.

### 5. Stale / concurrent PENDING-review generation protection

Only the authoritative Amp `/team-review` worker may create or reconcile the single current-user PENDING GitHub review, and only after independent verification of every candidate. Tycho must never call GitHub review or comment mutation APIs for this workflow.

GitHub's pull-request review REST API does not provide atomic compare-and-swap or `If-Match` on review update. This workflow therefore uses **snapshot revalidation plus fail-closed conflict**, not a claim of perfect concurrency exclusion. Residual TOCTOU between final re-read and write remains a documented residual risk; treat any 404/422/indeterminate mutation response as conflict.

#### Ownership

- The worker may mutate only a PENDING review it **owns for this assignment**.
- Ownership requires either: (a) the worker created the PENDING review in this assignment after the Amp first pass and recorded its review ID; or (b) the owner explicitly designates an existing current-user PENDING review ID for this assignment before reconciliation.
- An unowned pre-existing current-user PENDING review is always a conflict—even when it is the only one. Do not overwrite a human or foreign agent review.

#### Canonical PENDING snapshot

Use these REST reads (or equivalent GraphQL fields carrying the same identities):

- list current-user PENDING reviews on the PR;
- GET the single candidate review by ID;
- list all review comments for that review with pagination complete.

Build one canonical snapshot digest as SHA-256 over a single UTF-8 JSON object with **exactly** these keys, no extras, and JSON `null` for absent optional fields:

```json
{
  "review_id": "4864634225",
  "commit_id": "<full sha>",
  "body": "<raw review body>",
  "state": "PENDING",
  "comments": [
    {
      "id": "1",
      "path": "pkg/file.ts",
      "line": 10,
      "original_line": null,
      "side": "RIGHT",
      "start_side": null,
      "commit_id": "<full sha>",
      "body": "<raw comment body>",
      "updated_at": "2026-08-04T12:00:00Z"
    }
  ]
}
```

**Comment canonicalization:** before encoding, sort the `comments` array by comment `id` ascending as a decimal integer string comparison that yields the same order as numeric ID order for GitHub IDs (zero-pad-free decimal strings compared by integer value, with equal IDs forbidden). Digest equality must not depend on API return order: reversed or shuffled comment pages that contain the same comment set must yield the **same** digest after sort. Serialize with sorted object keys at every level; the digest input is the compact form from a deterministic encoder (no key reordering, no HTML entity decoding). Integer IDs are decimal strings. Do **not** use review `submitted_at` as a freshness signal for PENDING edits; PENDING reviews are not submitted. Comment `updated_at` values are part of the digest instead. An ambiguous, partial, or failed read is a deny (no mutation).

#### Pinned PR head revalidation (every write)

Immediately before **every** GitHub review or comment write—including the first create when baseline is “none—will create” and every subsequent edit of an owned PENDING review—re-read the PR’s current full head SHA from GitHub (for example REST `GET /repos/{owner}/{repo}/pulls/{pull_number}` → `head.sha`, full 40-character form).

- Require exact equality with the owner-pinned reviewed SHA frozen in the task/receipt.
- Mismatch (PR head advanced, force-pushed, or retargeted) or any read failure is **conflict**: do not write.
- This check is independent of the PENDING snapshot digest and applies on both the baseline-none/create path and the existing-owned-review reconciliation path.
- Record the pre-write PR head read (SHA or failure) in the evidence package.

#### Mutation procedure

1. Record baseline snapshot digest and owned review ID (or “none—will create”), plus the pinned reviewed SHA.
2. Immediately before each write:
   - re-read the PR current full head SHA and require exact equality with the pinned reviewed SHA (or fail closed on read failure);
   - re-read and recompute the PENDING snapshot when an owned review exists (create path: confirm still no owned/unowned conflicting PENDING that would violate ownership rules).
3. **Stop rather than overwrite** when any of these hold:
   - PR head SHA ≠ pinned reviewed SHA, or PR head read failed;
   - owned review ID missing, 404, or no longer PENDING;
   - snapshot digest drifted from baseline;
   - a different current-user PENDING review ID appeared;
   - more than one current-user PENDING review exists and ownership is not uniquely satisfied;
   - an unowned PENDING review is the only candidate;
   - on create path, an unexpected current-user PENDING appeared before create;
   - mutation returns 404, 422, or indeterminate error.
4. On conflict: leave GitHub untouched, preserve the bridge receipt state, and report the conflict. Do not dismiss foreign reviews, force-publish, or guess.
5. After each successful write, re-read and record the new snapshot as the next baseline before any further edit; re-check PR head before the next write again.
6. Publication (`COMMENT` / `APPROVE` / `REQUEST_CHANGES`), worker finish, and bridge acknowledgement remain separately authorized transitions. Wake-ups and schedules never imply them.

### 6. Exact evidence required for the #328 workflow

A useful provider opinion is not a complete #328 workflow. Count a #328 field run only when **all** of the following are recorded against the same receipt:

1. **#328-specific #327 prerequisite** — do not run this newly spawned local-worker workflow until [#327](https://github.com/zainfathoni/amux/issues/327) has an **accepted and merged** solution and that exact accepted version is installed and verified on the host. Local, unmerged, draft, or merely “available” routes do not satisfy this #328 prerequisite. This does not block generic `/amux-tycho` use with an existing coordinator or alter closed #323.
2. After the #327 gate clears: prove the authoritative Amp `/team-review` worker's initial assignment through that exact accepted merged delivery semantics (version/commit recorded).
3. Owner authorization naming exact Tycho agent, project, harness, model, and worktree; adjacent revalidation that the route still matches.
4. **Pre-Tycho** both-worktree repository, canonical workdir, full head SHA, full tree SHA, and cleanliness proofs from decision 4, plus frozen task bytes and recomputed `task_digest`.
5. Immutable `create` before Tycho execution with every required binding and `report_only`.
6. Separate-process recovery of the same receipt and owner capabilities from the original canonical directories is either performed in the cycle or already proven for that install and re-validated by `show` before consume.
7. Exactly one producer/session/run/task-bound durable `valid_report` (`complete` or `blocked`) through the canonical helper—not logs or exit codes.
8. **Post-Tycho / pre-consume** same-head proof (decision 4) on both Amp and Tycho attachments; failure rejects the application payload and the #328 cycle.
9. Explicit Amp-origin `consume` establishing `delivered` (bridge delivery only; application rejection may still follow).
10. Amp independent verification of every candidate against the pinned head, with unsupported and application-invalid claims rejected.
11. Amp-only reconciliation of at most one **owned** current-user PENDING review without publication unless separately authorized, including pre/post PENDING snapshots **and** per-write PR head equality checks (decision 5).
12. Pre-Tycho and post-Tycho GitHub snapshots (or equivalent audit) showing no Tycho-phase review/comment mutation.
13. Separate bridge `acknowledge` after handling.
14. **Cleanup evidence from acknowledge output, not `show`:** record the `acknowledge` result JSON (and any identical same-directory replay results) for `custody_cleanup` (`removed` or truthful `pending`). The helper does not persist cleanup status into the receipt store; `show` cannot supply it.
15. After acknowledge (and any cleanup replay), final `show` only to inspect terminal `acknowledged` state and append-only event order.
16. Explicit mapping that all six PR #11886 compliance gaps are closed for this cycle.
17. Record the cycle for owner assessment; it does not by itself change formal promotion status.

Natural-failure recovery without report loss may inform optional formal promotion, but it is not a normal-use or #323 closure gate. The bridge helper never attests Git or GitHub head state.

## Authority boundaries

| Principal | May | Must not |
| --- | --- | --- |
| Authoritative Amp `/team-review` worker | Independently review the pinned head first; own the review conclusion; consume and assess the Tycho report; create/reconcile the single owned current-user PENDING review after verification | Treat Tycho output as authoritative; publish or finish without separate authorization; skip independent verification; overwrite unowned PENDING reviews |
| Amp origin / coordinator | Create the receipt; hold custody; consume; acknowledge; optional wake-up schedule | Transfer coordinator identity to Tycho; treat notification as delivery |
| Owner | Approve the exact existing Tycho agent/project/harness/model/worktree (normally exact `claude-opus-5`); designate owned PENDING review ID when reusing one; authorize publication/finish separately | Infer aliases, fallback models, new agents, or alternate heads |
| Tycho producer | Run the bounded second-opinion task; submit one typed `complete` or `blocked` report | Mutate GitHub reviews/comments; choose publication state; join Amux groups; receive callback/finish/lifecycle authority; mutate the reviewed head; become coordinator; turn exit/logs/state into delivery |

## Workflow

Perform steps in order. Stop on any failed check.

### A. Authoritative Amp first pass

1. **#328-specific #327 prerequisite first:** stop this newly spawned local-worker workflow unless #327 is accepted, merged, and that exact accepted version is installed and verified. Local/unmerged routes are insufficient for this workflow only.
2. Create or adopt the authoritative Amp `/team-review` worker on the exact review worktree.
3. Prove initial assignment through the exact accepted merged #327 delivery semantics; record the installed version/commit.
4. Pin the PR repository, full head SHA, and full tree SHA; prove Amp worktree cleanliness per decision 4.
5. Complete an independent Amp first-pass review of that head **before** any Tycho input. Record that the first pass finished with no Tycho candidates consumed.
6. If a PENDING review will be reused, obtain an explicit owner-designated review ID now; otherwise plan to create one only after verification.

### B. Owner-select exact Tycho route

1. Require explicit owner authorization naming the existing Tycho **agent**, **project**, **harness**, **model**, and **worktree**.
2. Normal model is exact `claude-opus-5` when the owner authorizes that exact identifier. No aliases, defaults, fallbacks, or alternate models.
3. Adjacent-revalidate that the named route still exists and is the one selected. Missing, ambiguous, or unapproved selection blocks.
4. Prove same-head attachment per decision 4. Do not create a Tycho agent/project or switch harness/model under this workflow.

### C. Freeze task and create one immutable receipt

1. Write one bounded task containing at least the literal fields required by decision 4 item 7, plus second-opinion scope, acceptance criteria, report schema pointer (this document + bridge protocol), explicit `report_only` ban on GitHub mutation, producer-only submit instructions, and the requirement to submit one application-valid `complete` or `blocked` report before stop.
2. Compute `task_digest` as SHA-256 of the exact task bytes.
3. Create the receipt **before** Tycho execution using the canonical helper, binding:
   - canonical Amp `origin_thread`;
   - opaque `correlation_id` and unguessable `producer_nonce`;
   - exact `tycho_agent_key`, nullable provider session ID, `run_id`, `task_message_id`;
   - canonical Tycho absolute `workdir`;
   - `task_reference` / `task_digest`;
   - `producer_role` (for example `team_review_second_opinion`);
   - exact `authority: "report_only"`;
   - optional reference-only `group_reference` and optional coordinator-selected notification target.
4. Keep coordinator and abandonment capabilities owner-private. Deliver producer-only submit materials per decision 2. Do not give Tycho GitHub review-mutation credentials or tools.

### D. Tycho second opinion once

1. Capture a pre-Tycho GitHub snapshot of current-user PENDING reviews/comments for later no-mutation proof.
2. Run the bounded task once on the selected route. This skill does not launch or own the provider process.
3. Require exactly one durable `submit` of a typed `complete` or `blocked` report through the canonical bridge.
4. If no valid submit arrives, retain the receipt, record “no Tycho finding,” and skip candidate consumption. Do not mine logs or state (decision 3).
5. Capture a post-Tycho GitHub snapshot; any review/comment mutation during the Tycho phase is a compliance failure even if a report exists.
6. **Post-Tycho / pre-consume same-head proof** (decision 4) on both Amp and Tycho attachments. On failure, reject the application payload and the #328 cycle.

### E. Explicit consume, independent verify, PENDING reconcile

1. Only after post-Tycho attachment proof succeeds: from the immutable Amp origin, `consume` the `valid_report` and verify `delivered`. (If attachment proof failed, optional bridge hygiene consume/ack may still run with an explicit application-rejection record and no GitHub mutation.)
2. Enforce application report invariants (decision 1). Independently reproduce or reject **every** candidate against the pinned head. Unsupported, latent, out-of-scope, or application-invalid claims are dropped.
3. Reconcile at most one **owned** current-user PENDING GitHub review using only Amp-verified findings, with snapshot protection **and** per-write PR head equality (decision 5)—including the baseline-none/create path. Leave it unpublished unless the owner separately authorizes publication.
4. Bridge acknowledgement is a **later** distinct coordinator action after verification/handling. It is not consumption, publication, finish, or acceptance of every candidate.
5. Optional notification and one-time schedules are wake-up only.

### F. Terminal truth and #328 evidence package

1. Complete separate `acknowledge` when handling is done; **retain the acknowledge JSON** as cleanup evidence (`custody_cleanup`).
2. If cleanup is `pending`, replay the identical terminal event against the same original capability directory until `removed` or retain the exact blocker; retain each replay JSON.
3. Final `show` after acknowledge/cleanup confirms only terminal `acknowledged` state and append-only event order—not cleanup status.
4. Package evidence for owner assessment under decision 6. Do not change the readiness matrix row.

## Six PR #11886 gaps this workflow closes

| # | Gap in the field run | Compliant control |
| --- | --- | --- |
| 1 | No pre-created immutable receipt | Step C creates the receipt before Tycho starts with full binding |
| 2 | No typed `valid_report` | Step D requires one durable `complete` or `blocked` submit |
| 3 | No explicit bridge consume | Step E `consume` establishes `delivered` |
| 4 | No separate bridge acknowledgement | Step E/F acknowledge after handling only |
| 5 | Direct PENDING-review mutation boundary | Tycho is report-only; only Amp mutates an owned PENDING after verification |
| 6 | Ad hoc exit-143 recovery | Stop without submit ⇒ no finding; partial work only via `blocked` |

## Troubleshooting

- **#327 not accepted/merged/installed** — stop this #328 newly spawned local-worker workflow. This does not block generic `/amux-tycho` use with an existing coordinator or alter closed #323.
- **Tycho stopped / exit 143 / no submit** — `show` should remain without a new report. Do not reconstruct findings. Owner may authorize a **new** receipt and run only after re-validating head and route.
- **`blocked` report consumed** — verify any partial findings independently; treat blockers as incomplete scope; do not publish solely because Tycho blocked.
- **`complete` with blockers or empty verification** — application-invalid; consume, reject as finished second opinion, record handling, optionally acknowledge; do not treat blockers as accepted defects.
- **Wrong head, tree mismatch, dirty Amp/Tycho tree, or post-Tycho attachment drift** — fail before `create`, or reject the application payload before consume/GitHub mutation; never rebind; the #328 cycle fails.
- **PR head advanced before write** — conflict; leave GitHub untouched (create and reconcile paths alike).
- **Producer asks for custody/ack/GitHub write credentials** — refuse; resupply only producer proof + state dir.
- **Unowned or concurrent PENDING drift** — stop GitHub mutation; preserve receipt; report conflict.
- **Notification succeeded but nobody consumed** — notification is not delivery; `consume` explicitly.
- **Desire to formally promote the transport** — refused here; formal promotion requires a separate owner decision.
- **Pre-split receipt** — preserve original paths and identities; continue with the installed `/amux-tycho` helper without migration mutation.

## Out of scope

- Stable `cmd/` or `internal/` changes, Go CLI promotion, or versioned Tycho ingress implementation.
- Live Tycho/provider/BTA runs authorized by this document alone.
- Multi-producer fan-out, recurring watchers, polling-as-delivery, or Tycho group membership.
- Automatic publication, finish, label, archive, teardown, or worktree cleanup from report state.
- Broadening `/amux-claude` or `/amux-pi` as substitutes for this report-only bridge workflow.
- Claiming atomic GitHub review CAS that the platform does not provide.
