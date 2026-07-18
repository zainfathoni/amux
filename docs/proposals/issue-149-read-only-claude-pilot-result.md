---
status: completed
issue: 149
base: 827dd390add096e90bf14588b0c38bf5ad239233
decision: pass
---

# Issue 149 read-only Claude delegation pilot result

## Decision

**Pass.** One real bounded gate-risk review satisfied every registered read-only gate: acknowledged capacity, exact client identity, receipt replay and conflict behavior, a valid durable semantic report, inbox delivery, separate acknowledgement, useful independent analysis, and acknowledgement-gated identity-verified parking.

This decision only unlocks consideration of issue #150. It does not authorize or start mutating delegation, teleport, promotion, or a stable Claude lifecycle API.

## Curated pilot evidence

The task was an independent gate-risk review of the merged experimental seam and public contract. Claude was selected because the task benefited from a second model reviewing implementation, tests, and contract boundaries without editing them. Its sources were limited by instruction to the public proposal, three public delegation references, the experimental helper, and its focused tests.

| Evidence | Observed result |
| --- | --- |
| Base and worktree | Clean dedicated linked worktree at `827dd390add096e90bf14588b0c38bf5ad239233` before launch and after parking; no repository mutation observed. |
| Capacity before | CodexBar source `web`, confidence `reported`; five-hour window 0% used over 300 minutes with reset unavailable; weekly window 9% used over 10,080 minutes, resetting `2026-07-21T06:00:00Z`; two model-specific entries had unavailable utilization, duration, and reset. The weekly window governed as the highest known utilization. The coordinator explicitly acknowledged these values and limitations before launch. |
| Capacity after | Five-hour window 3% used, resetting `2026-07-18T18:49:59Z`; weekly window remained 9% used, resetting `2026-07-21T05:59:59Z`; the same two model-specific entries remained unavailable. The weekly window still governed. |
| Versions and capability diagnostics | Darwin; Claude Code 2.1.214; tmux 3.7b; experimental protocol version 1. No session hook was used, so its version and contract were unavailable. Initial interactive input and semantic submission were supported. Automatic interactive input was unavailable. Managed-policy runtime effect, strict-MCP runtime behavior, and read-confinement runtime remained explicitly `untested`; policy confinement was not treated as an OS sandbox. |
| Receipt determinism | Initial create recorded; exact create replay returned `duplicate`; conflicting immutable reuse was rejected; the original receipt remained unchanged. |
| Client identity | Acquisition matched the receipt-created tmux pane, Claude session, process incarnation, canonical workdir, base, and launch-command digest. Parking reverified that complete incarnation. |
| Semantic report | Schema and correlation validation produced one `complete` report with an empty `changed_artifacts` list and six public repository references. The report envelope was 6,858 bytes. |
| Delivery and acknowledgement | The valid report was recovered from the durable machine-local inbox. Consumption recorded `delivered`; a separate coordinator action then recorded `acknowledged`. Notification was not used as delivery evidence. |
| Parking | Explicit parking occurred only after acknowledgement. Exact identity revalidation succeeded, the exact pane was parked, and no absence-recovery path was used. |
| Size and timing | Initial packet: 4,365 bytes. Follow-ups: zero. Token counts were unsupported and not estimated. Receipt creation through verified parking took 221.863 seconds. |
| Coordination | One initial delegation, one semantic report, zero follow-up turns, and zero human escalations during the successful run. Capacity observation and explicit acknowledgement required two coordinator messages. One earlier pre-launch attempt required owner-directed recovery after failing closed on a missing tmux session; issue #165 moved that check before durable launch intent. That attempt produced no Claude report and contributes no usefulness evidence. |
| Amp assessment | **Revised.** Amp retained the report's useful distinction between implementation-backed lifecycle properties and unproven runtime confinement. Amp did not adopt its stricter claim that every runtime diagnostic must cease being `untested` before issue #150 may even be considered, because that is not part of issue #149's registered pass gate. |
| Usefulness | Useful. The report independently connected contract claims to focused tests, confirmed the missing-session fix's boundary, and identified policy confinement and real-runtime behavior as limitations rather than guarantees. |

## Boundaries left unproven

No input request, manual follow-up, automatic input, optional Amp-pane notification, notification failure, usage-limit pause, authentication prompt, process death, resumed session, tmux-server restart, capture invalidation, interrupted park recovery, cleanup, OS-level read confinement, mutation, or teleport occurred. Those paths remain unavailable or untested exactly as classified; none was manufactured to satisfy the gate.

The initial task, semantic report, receipt, runtime files, pane content, tool stream, correlation identities, and other raw evidence remain private and are not represented here. This result makes no claim that the analysis was correct merely because its delivery mechanics passed.
