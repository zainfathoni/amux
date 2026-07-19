---
status: experimental-evidence
---

# Issue 175 Amp invocation-policy probes

## Scope and method

These public-safe probes cover Amp CLI `0.0.1784463238-g66cb95` on macOS and the skill-owned automatic spawn seam. They used public Amp documentation, `amp tools list/show`, `amp permissions test`, and a temporary delegated helper. The helper probe ran no tool and changed no user settings. No model-backed Amp probe, thread/history access, private policy, account evidence, receipt, or runtime dossier was used.

Hard enforcement requires trusted normalized fields and interception before side effects for the exact tuple. Documentation or a synthetic permission test alone does not establish native approval binding, one-call scope, replay behavior, or coverage of a server/plugin action.

## Results

| Tuple | Evidence | Outcome |
| --- | --- | --- |
| skill-owned automatic `/amux spawn` × mode × explicit resolver preflight | The skill selects the mode and can invoke a pure resolver before its shell call. Existing examples already pass `--mode medium`. | **deterministic** for `automatic:true`: allow `medium`; reject another mode without rewriting. Direct CLI use remains a documented bypass outside this skill tuple. |
| Amp delegated permissions × active `shell_command` × helper exit mapping | Temporary `amp.permissions` delegation received exact JSON arguments on stdin and `AGENT_TOOL_NAME=shell_command`; helper exits `0/1/2` produced allow/ask/reject. Public docs say rules run before tool calls. | Protocol supported, but not promoted for unrelated actions. This does not prove any native model-backed tuple. |
| native child creation × local/orb/Amp `runner(id)` × legacy permission adapter | `create_thread` was absent from `amp tools list/show`; no argument/default/interception probe was possible. Public plugin APIs can call `createThread` directly. | **observed**; require explicit executor in policy intent, but no hard enforcement. Plugin creation is a known bypass. |
| Read Thread × exact target × legacy permission adapter | `read_thread` was absent from `amp tools list/show`; automatic mention extraction and direct retrieval paths are separate. Native prompt binding and replay were not proven. | **observed**; preserve exact approval and the one-query discrepancy exception as resolver intent only. |
| Oracle/Search/Review/Librarian and generic Task × legacy permission adapter | Their model-backed tool names/schemas were absent from the active CLI tool inventory. Internal fan-out and capacity charge routes were not exposed. | **observed** for capacity outcomes; semantic escalation remains instruction-only. |
| native child message × legacy permission adapter | No stable source, target, action/message ID, parent route, or retry identity was exposed in the active inventory. | **instruction-only**. Do not parse prose or count amux/Claude lifecycle traffic. |
| capacity source × provider/provider-version/schema/pool/window/unit/amount/freshness/confidence/charge-route/reservation/reserve-status | The bounded helper observation reported a source/confidence and windows, but provider version, units/amounts for some model-specific windows, resets, charge-route, reservation, and reserve impacts were unknown. | **observed**. Unknown/missing/drifted fields and complete-looking evidence for an unpromoted capacity tuple yield `would_ask`, never binding `ask` or automatic allow. |

## Permission and privacy boundary

The thin permission adapter receives and ignores raw arguments without parsing, logging, or emitting them. Because no Amp-native action tuple passed all probes, it emits only a bounded `permission_tool_unproven` capability diagnostic and exits allow. The resolver similarly emits only action/result/reason/capability and the public source class; no raw target or private capacity value appears.

No owner-local decision ledger is implemented: stable typed native action identities were not proven. The existing `/amux` discrepancy-recovery Read Thread exception is unchanged, #147/#177 Claude delegation remains independent, and blocked #176 is not performed or absorbed here.
