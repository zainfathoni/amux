# amux-tycho trigger checklist

This experimental skill is explicit-only. Incidental mentions of Tycho, Claude, Pi, agents, projects, harnesses, models, reports, available capacity, or generic code review do not activate it.

| Explicit owner request | Route | Contract |
| --- | --- | --- |
| `Use /amux-tycho to route this bounded task through an existing Tycho agent` | [`../SKILL.md`](../SKILL.md#explicit-only-workflow), then the canonical [bridge protocol](tycho-report-bridge.md) | Owner selects the existing agent/project/harness/model; current Amp thread remains coordinator; Tycho is `report_only`. |
| `Authoritative Amp /team-review with one Opus second opinion` | [`team-review-second-opinion.md`](team-review-second-opinion.md) | Amp independent first pass; owner-exact existing Tycho route (normally `claude-opus-5`); immutable receipt before execution; one typed `complete`/`blocked` report; Amp-only PENDING review mutation after verification; does not promote readiness. |
| `Recover this /amux-tycho receipt` | [`tycho-report-bridge.md#optional-notification-and-recovery`](tycho-report-bridge.md#optional-notification-and-recovery) | Current canonical Amp thread must exactly equal the immutable bound origin; owner authorization or custody possession cannot transfer authority. Private store is durable truth; notification and polling are never delivery. |
| `Abandon this created-only /amux-tycho receipt because coordinator custody is irrecoverably lost` | [`tycho-report-bridge.md#created-only-lost-token-abandonment`](tycho-report-bridge.md#created-only-lost-token-abandonment) | Exact owner-authorized bound capability only; preserve history and reject every broader cleanup interpretation. |

Do not substitute `/amux-claude`, `/amux-pi`, a direct provider run, a new Tycho route, stable Amux group/report/callback mutation, or ad hoc log/state recovery when this exact bridge was requested.
