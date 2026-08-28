# amux-tycho trigger checklist

This experimental skill is explicit-only. Incidental mentions of Tycho, Claude, Pi, agents, projects, harnesses, models, reports, available capacity, or generic code review do not activate it.

| Explicit owner request | Route | Contract |
| --- | --- | --- |
| `Use /amux-tycho to route this bounded task through an existing Tycho agent` | [`../SKILL.md`](../SKILL.md#explicit-only-workflow), then the canonical [bridge protocol](tycho-report-bridge.md) | Owner selects the existing agent/project/harness/model; current Amp thread remains coordinator; Tycho is `report_only`. An immutable remote artifact is pinned by repository, PR, full head SHA, and full tree SHA with no fallback. |
| `Create an exact Tycho route and use /amux-tycho for this bounded task` | [`../SKILL.md`](../SKILL.md#route-selection), then the canonical [bridge protocol](tycho-report-bridge.md) | Explicit owner authority may create exactly one owner-authorized prepared route without provider execution. Freeze the exact route and immutable remote artifact, create the receipt before the first run, and stop on indeterminate creation with no fallback. |
| `Authoritative Amp /team-review with one Opus second opinion` | [`team-review-second-opinion.md`](team-review-second-opinion.md) | Native Amp review remains authoritative. Use one exact owner-authorized Tycho route only when Opus could change a high-impact conclusion; the ordinary lane owns preflight, one receipt/report, independent verification, and stop conditions. PENDING mutation, recovery, and field-evidence ceremony disclose only when those branches activate. |
| `Recover this /amux-tycho receipt` | [`tycho-report-bridge.md#optional-notification-and-recovery`](tycho-report-bridge.md#optional-notification-and-recovery) | Current canonical Amp thread must exactly equal the immutable bound origin; owner authorization or custody possession cannot transfer authority. Private store is durable truth; notification and polling are never delivery. |
| `Abandon this created-only /amux-tycho receipt because coordinator custody is irrecoverably lost` | [`tycho-report-bridge.md#created-only-lost-token-abandonment`](tycho-report-bridge.md#created-only-lost-token-abandonment) | Exact owner-authorized bound capability only; preserve history and reject every broader cleanup interpretation. |

Do not substitute `/amux-claude`, `/amux-pi`, a direct provider run, an unapproved or already-running new Tycho route, stable Amux group/report/callback mutation, or ad hoc log/state recovery when this exact bridge was requested.
