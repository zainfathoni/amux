# Applied GitHub issue direction corrections

These ADR 0008 direction corrections were applied on 2026-08-30: [#331](https://github.com/zainfathoni/amux/issues/331) and [#339](https://github.com/zainfathoni/amux/issues/339) received the exact title/body rewrites below, and closed [#374](https://github.com/zainfathoni/amux/issues/374#issuecomment-5469174178) received the exact corrective comment below without being reopened.

> **Later direction:** ADR 0009 removes the active core worker/coordination drain routes and leaves their stores inert. The text below is retained as an exact historical record of edits already applied to GitHub, not current command guidance. Any follow-up issue rewrite requires separate authorization.

## Applied rewrite #331

**Applied title:** `Thin Amux host + legacy coordination drain umbrella (ADR 0008)`

```markdown
## Purpose

Umbrella for shrinking Amux to its retained machine-local host layer while draining legacy coordination/provider lifecycle surfaces.

Authoritative sources:

- ADR 0008: retained machine-local runner host
- ADR 0007: native-new-work and staged legacy drain decisions that ADR 0008 preserves
- `docs/staged-drain-disposition.md`

## Retained active Amux layer

- Machine-local runner registry and exact canonical-workdir bindings
- Runner pin/list/doctor/park/remove and minimum fail-closed reconcile/safety
- Automatic `amux launch --all` at graphical login/boot
- tmux and `amp --no-tui` process launch
- Install/update/maintenance/doctor diagnostics
- systemd/launchd activation and process-group retention; OS services do not replace Amux

## Drain-only layer

- Generalized worker spawn/adoption and native-work coordination
- Groups, reports, callbacks, deadlines, finish authorization, and shelving coordination state
- Provider-specific lifecycle bridges only after their replacement gates are proven
- One future #360 inventory only on a separate explicit owner request; sweep-surface deletion only after acceptance/disposition + no-repeat confirmation

## Related work

| Item | Role |
| --- | --- |
| #339 | Rollout/documentation alignment |
| #328 | Tycho direct structured-return field gate; keep open |
| #221 | Drain already-bound Claude pairs only |
| #318 | Selector boundary on retained drain commands |
| #311 | Digest-drift fix only if it blocks receipt drain |
| #232 / PR #366 | Retained runner-operation safety evidence; classifier is preflight-only |
| PR #360 / #351 | One-time sweep requires separate owner authorization; this correction does not run it, and deletion remains inactive |

## Acceptance

- [ ] Public/contributor/skill guidance has one destination: native Amp for new task coordination, retained Amux for machine-local automatic runners, legacy stores drain-only
- [ ] Generalized spawn/adoption and native-work lifecycle enrollment remain closed
- [ ] Retained runner launch/registry/diagnostics have an explicit supported boundary
- [ ] Legacy store writers are removed only after family-specific evidence gates
- [ ] Provider bridges close only after their existing replacement gates pass
- [ ] #360 sunset remains inactive until owner acceptance/disposition plus no-repeat confirmation

## Non-goals

- No complete Amux product archive
- No runner-admission closure or native/OS replacement launcher
- No append-only retirement stream, finalizer, permanent Lead, or provider-neutral control plane
- No replacement cutover or reader-window dates until separately chosen

## History

Originally the symmetric-retirement umbrella, then rewritten under ADR 0007 in PR #375. Corrected under ADR 0008 to retain the machine-local runner-launch core while preserving the valid legacy coordination drain.
```

## Applied rewrite #339

**Applied title:** `ADR 0008 rollout docs: retained runner host and legacy drain gates`

```markdown
## Parent

#331 thin-host and legacy-drain umbrella.

## Goal

Publish operating guidance with one explicit ownership boundary:

- native Amp owns all new task coordination;
- Amux retains machine-local runner registration, automatic launch, tmux/Amp process lifecycle, maintenance, and diagnostics;
- legacy worker/coordination/provider stores are compatibility and drain-only.

## Acceptance criteria

- [ ] Document runner pin/list/doctor/park/remove and minimum safe reconcile as retained active operations
- [ ] Document graphical-login/boot `amux launch --all`
- [ ] Document verified activation patterns: systemd `Type=oneshot` + `RemainAfterExit=yes`; RunAtLoad LaunchAgent + `AbandonProcessGroup=true`
- [ ] State that OS service managers activate/retain the Amux-launched process group and do not replace Amux
- [ ] Preserve native-new-work rules: no generalized spawn/adoption or automatic group/report/callback/deadline/finish enrollment
- [ ] Publish family-specific export/freeze/writer-removal gates only for legacy stores
- [ ] Keep #328 and provider bridges on their existing proof gates; no dual route
- [ ] Keep #360 sunset inactive until owner acceptance/disposition plus no-repeat confirmation
- [ ] Update public skill/docs only for merged and verified behavior

## Withdrawn proposals

- No 2026-09-01 cutover
- No 2026-11-30 reader window
- No replacement dates yet
- No complete product archive or runner-admission closure

## Non-goals

- No new coordination lifecycle engine or retirement record stream
- No resident Amux supervisor
- No direct OS/native replacement for Amux runner launch
- No #360 inventory run or sweep-surface deletion

## History

Originally “Retirement slice 8,” then rewritten under ADR 0007 in PR #375. Corrected under ADR 0008 after the owner retained the thin machine-local runner host.
```

## Applied corrective comment on closed #374 (not reopened)

```markdown
Direction correction under ADR 0008; this issue remains closed.

The optional resident `amux supervise --all` daemon is still not required. However, the prior closure rationale that native/OS supervision replaces Amux runner launch is superseded. The retained destination is automatic `amux launch --all` at graphical login/boot, with Amux owning the machine-local workdir registry and tmux/Amp process launch. systemd (`Type=oneshot`, `RemainAfterExit=yes`) or a RunAtLoad LaunchAgent (`AbandonProcessGroup=true`) activates Amux and retains its launched process group; it does not launch replacement runners in place of Amux.

#232 / PR #366 remains preflight-only safety evidence for retained runner operations, not a mandate to close runner admission or migrate away from the launcher. No issue reopening or runtime action is requested by this correction.
```

## No body edits proposed

- **#328:** already narrowly scoped to its still-open direct structured-return gate.
- **#221:** already narrowly scoped to draining bound Claude pairs.
- **#311:** already narrowly scoped to a compatibility fix only when an existing receipt cannot drain.
- **#318:** already narrowly scoped to retained compatibility/drain selectors.
- **#232 / PR #366:** retain as closed safety evidence and preflight-only classifier text; do not reinterpret as runner migration authority.
- **PR #360 / #351:** retain the existing conditional sunset wording; do not run inventory or delete sweep surfaces.
