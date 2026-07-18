---
status: experimental-evidence
issue: 151
base: 7017411094e3dc947923992ec62aa7d08c4435ee
decision: stop-narrow
---

# Issue 151 mutating Claude delegation Pilot 2 evaluation

This is experimental evidence from one bounded run, not an accepted contract, a merge decision, or a promotion decision. It follows the pilot sequence and evidence shape described in the [interactive delegation proposal](claude-code-interactive-delegation-pilot.md) and the [design grill](issue-147-claude-delegation-design-grill.md), and reuses the authority split defined in [`claude-mutating-delegation.md`](../../skills/amux/reference/claude-mutating-delegation.md).

Claude authored the first one-clean-commit draft as `93206a45d7cb014793abca793c5d8bacacc8dc34`. Amp then independently inspected that frozen commit, ran verification, consumed and acknowledged the semantic report, explicitly parked the verified Claude client, and authored the assessment and decision recorded below. This preserves attribution while keeping integration outside delegation completion.

## Bounded task and handoff

The task was documentation-only: draft this evaluation file itself at a fixed path in a dedicated, exclusively owned worktree, using only the public sources listed in the task packet, then hand off with the declared shape.

- **Handoff contract:** one clean local commit beyond the bound baseline, or a zero-commit `blocked` report naming no commit and no changed artifacts. No other shape is permitted.
- **Excluded from this delegation:** integration, push, pull-request or other GitHub mutation, merge, release, worktree/branch cleanup, and parking. These remain Amp/coordinator-only actions, deliberately left separate from this document.
- **Excluded from this document:** prompts, transcripts, pane captures, tool streams, secrets, private receipt contents, private capacity values or policy, private dossier metadata, and local process/tmux identifiers.

## Evidence

| Category | Observed result |
| --- | --- |
| Baseline and isolation | Dedicated worktree bound to baseline `7017411094e3dc947923992ec62aa7d08c4435ee` on branch `docs/issue-151-mutating-pilot`; working tree was clean before this delegation's edits began. |
| Capacity gate | Evaluated by the coordinator immediately before planning under owner-only policy. Public-safe conclusion only: the owner-approved capacity gate authorized this one bounded run. No capacity values or policy details are recorded here. |
| Authority split | This process held `producer_role:mutating_delegate` and `authority:exclusive_writer` for edits inside the bound worktree only. Amp retains sole authority for worktree/branch preparation, integration, GitHub actions, parking, and cleanup, matching the ownership matrix in the mutating-delegation reference. |
| Objective handoff validation | Passed at semantic report submission and again before delivery: `93206a45d7cb014793abca793c5d8bacacc8dc34` was the only commit beyond the baseline, its direct parent was the bound baseline, it changed only this document on the expected branch, and the worktree was clean. This proves objective shape only. |
| Claude-performed verification | `git diff --check` was run against the one staged/committed change to catch whitespace errors; the change is docs-only, so no build, type, or test suite applies. No repository mutation outside the target file was made or observed. |
| Independent Amp verification | Amp inspected the exact commit and reran objective parent/count/path/cleanliness checks, `git diff --check`, a targeted `go test -p 1 ./scripts -count=1`, the full `TMPDIR=/tmp go test -p 1 ./... -count=1`, a privacy scan, and the applicable gofmt scope check. All passed. |
| Delivery, acknowledgement, parking | The helper-submitted report was revalidated and consumed through the machine-local inbox, then acknowledged as a separate durable event. Explicit parking reverified and stopped the exact acquired client; notification was not treated as delivery or acknowledgement. |
| Utility and coordination cost | The draft was useful enough to retain and substantially reduced the work needed to structure this evaluation. The deterministic loop succeeded without clarification, but setup, capacity interpretation, evidence handling, and independent verification remained material coordination work for a small documentation change. |
| Unsupported or untested capabilities | Automatic tmux input, teleport/adoption, input-request follow-up, and automatic parking were not exercised in this run and remain untested here, consistent with their status in the interactive delegation proposal. This run did not encounter a usage-limit pause, capture invalidation, or process/tmux restart. |
| Integration and cleanup | Deliberately out of scope for this delegation and this document. No push, PR, merge, release, worktree removal, or branch deletion was performed or requested. |

The bounded post-run capacity observation failed the owner-approved capacity gate. Exact policy and capacity values remain owner-only. This does not retroactively invalidate the authorized run, but it prohibits another launch under current conditions.

## Allowed final decisions and gate logic

Per the pilot sequence, exactly one of three outcomes follows from this and any other Pilot 2 evidence:

1. **Repeat** — run another real isolated mutating task before deciding further, because one successful run is evidence but does not by itself establish a repeatable pattern.
2. **Stop or narrow** — halt or reduce the experiment's scope if interactive subscription use is not reliably preserved, results do not repay coordination and context cost, workdir ownership proves ambiguous, private data cannot be bounded, or the helper duplicates existing #118 mechanics without a genuinely different contract.
3. **Propose** — draft a promotion proposal, which requires repeated useful evidence across both read-only and mutating delegations, at least one real recovery path, bounded coordination cost, and its own separate accepted decision. One successful Pilot 2 run cannot manufacture this outcome by itself.

## Decision: stop/narrow

Stop further mutating launches under the current capacity conditions and keep the seam experimental. The run produced a useful, objectively valid handoff and completed durable delivery, separate acknowledgement, and identity-verified parking, but that success is insufficient for promotion: post-run capacity fails the governing policy, coordination cost was material relative to the task, and no real delivery or lifecycle recovery path was exercised. `repeat` is therefore not currently authorized, while `propose` lacks its required repeated evidence and recovery evidence.

This decision narrows only the experiment. It does not remove the merged seam, authorize a stable API proposal, create Pilot 3, or begin teleport/adoption work. Any future repeat requires a new explicit decision after capacity policy permits it; recovery evidence must arise from a real bounded failure rather than a synthetic exercise.

## Delegate perspective (attributed, non-binding)

From the delegate's bounded view in the original draft: the task was completed within the declared one-clean-commit handoff shape using only public sources, and no ambiguity in worktree, branch, or baseline identity was encountered during the run. This remains an attributed delegate-side observation, not a claim of correctness, acceptance, merge readiness, or promotion.

## Non-claims

This document does not claim that its own handoff was accepted, that the drafted content is correct or complete, that it is merge-ready, that this run demonstrates promotion readiness, or that any cleanup or integration authority follows from it. Those determinations remain with Amp and the coordinator after independent review.
