---
status: implemented-phase-1
decision: adr-0007
cutover-generation: spawn-native-cutover-v1
baseline: fa5a79c2b1742a12e48d22e2bf3c43e41288745c
date: 2026-08-12
---

# Generalized spawn cutover inventory

This is the bounded phase-1 inventory for ADR 0007. It covers executable `amux spawn` admission, both aliases, every repository-owned active skill/public-doc route that initiated new work, deterministic tests, compatibility readers, and historical references present at baseline `fa5a79c`.

## Cutover boundary

`spawn-native-cutover-v1` is the exact spawn-family cutover generation.

- A `spawn-assignments.json` record with record `schema_version: 1` and no post-cutover admission fields proves pre-cutover admission. It may only follow its exact existing next transition. A prepared record may arm once; an armed record may finalize without resend. The transition updates only `spawn-assignments.json`.
- A record with `schema_version: 2`, admission `spawn-native-cutover-v1/projectless-physical-host-exception`, and an exact physical host/workdir is post-cutover exception state. Older binaries reject record schema 2 instead of silently dropping the host binding.
- Missing, zero, unknown, mixed, or malformed record provenance proves neither class and fails closed.
- Generalized `prepare` is exit 2 before the mutation lock, state write, thread creation, tmux access, or native message.

Ordinary new work is one authenticated native Amp creation on the exact intended Workspace Project and Orb, or one exact currently live runner whose working directory is the intended canonical workdir. The assignment is delivered in that native request. The returned thread keeps Amp's parent/child and reply route and is not automatically adopted, pinned, grouped, shelved, reported, or represented in Amux.

## Active call sites and disposition

| Surface | Baseline call/admission path | Phase-1 disposition |
| --- | --- | --- |
| `cmd/amux/cli.go` | Top-level `amux spawn` and `amux worker spawn` command specs | Both aliases remain exact. Help now describes drain/exception only and exposes the exact-host authorization flags. |
| `cmd/amux/worker.go` | Both aliases dispatch `spawn` to `workerSpawn` | Dispatch retained; all admission is centralized in the cutover implementation. It never invokes `workerAdopt`. |
| `cmd/amux/spawn.go` | General prepare/arm/native-message/finalize protocol; prepare wrote worker/group state and finalize created a pane | General prepare rejected. Schema-1 continuations are drain-only and single-store. Schema-2 exception requires explicit owner authorization and exact local host/workdir, forbids groups, and writes no worker/group/operation/shelf/pane state. |
| `internal/config/spawn_assignments.go` | Schema-1 assignment store | Schema 1 is the pre-cutover marker; schema 2 carries the exception admission and host. File path and file schema remain compatible; existing records are not rewritten until an authorized exact transition. |
| `cmd/amux/completion.go` | Bash/zsh/fish exposed generalized phase flags | Descriptions and flags now expose drain/exception semantics and exact host authorization. |
| `README.md` | Workspace Project native-create → adopt; projectless generalized exception | Ordinary work is native-only and unmanaged by Amux. Exception and schema-1 drain are documented exactly. |
| `skills/amux/SKILL.md` | `Spawn a worker`, `/amux sprawl`, and issue coordination routed native creation into adoption/group state | Ordinary routes now use authenticated native creation with exact executor/workdir and no Amux lifecycle write. Exception requires an exact owner instruction. |
| `skills/amux/reference/workflows.md` | Fresh worker, sprawl, and durable issue-group onboarding all created then adopted | Fresh work and sprawl are native-only. New issue coordination uses native parent/child/reply/wait. The durable group procedure is explicitly existing-state drain only. |
| `skills/amux/reference/{commands,troubleshooting,contract-v1,deadline-v1,trigger-phrases,amp-invocation-policy}.md` | Active create/adopt, deadline-at-spawn, and projectless recovery guidance | Updated to the same cutover generation, native route, no-adoption rule, exact exception, and schema-1 drain contract. |
| `CONTEXT.md` | Sprawl/finish glossary described new work as Amux interactive-worker lifecycle | Sprawl now names native creation with no Amux representation; finish is explicitly an existing-worker drain workflow. |
| `docs/index.html`, `docs/skill/index.html` | Public examples instructed native create → adopt/group/report | Updated to native-only ordinary creation and drain-only legacy coordination. |
| `cmd/amux/main.go` legacy `app.run` help | Stale positional spawn documentation, though `app.run` had no spawn dispatch | Removed so no repository-owned help path advertises the retired transport. |
| `cmd/amux/{spawn,cli,main,worker}_test.go`, `internal/config/config_test.go`, and `scripts/amux_skill_test.go` | Exercised generalized creation, worker writes, group writes, presentation, help, and active skill routes | Replaced/updated with deterministic rejection, exact-host binding, indeterminacy, no-retry, no-lifecycle-write, schema-1 drain, no-dual-write, unprovable-schema rejection, help, completion, and documentation checks. |

The remaining `internal/config/directory.go` path accessor and `internal/result/result.go` JSON assignment-detail type are data plumbing, not creation or admission call sites. They remain because drain and exception outcomes still use the existing store path and result envelope.

## Call sites intentionally not migrated or removed in this phase

These are the exact remaining surfaces that cannot safely be removed as part of the spawn-only cutover:

1. **`amux worker adopt` admission in `cmd/amux/worker.go` and the existing-worker recovery reference in `skills/amux/reference/troubleshooting.md`.** It is no longer called by any active new-work workflow, but the command can still finish or replay already-persisted `worker-adopt` operations. Those records have no ADR 0007 worker-family cutover marker that can distinguish an existing partial request from a new manual request. Rejecting all calls now could strand a request after intent or worker-row persistence; allowing only guessed cases could admit or corrupt state. Close it in the worker-admission phase after publishing a worker cutover generation and classifying existing adoption operations. Until then it is compatibility-only, never automatic, and a missing compatible drain command is a blocker rather than authority to bootstrap or re-adopt.
2. **Pre-cutover schema-1 spawn assignments.** They cannot migrate to native creation because a prepared record already owns one exact created thread and an armed record may already have sent its message. Native recreation, adoption, or message replay would duplicate work or erase indeterminacy. They remain exact, no-resend, single-store drain transitions.
3. **The schema-2 projectless physical-host exception.** Native authenticated project-backed creation cannot yet express every owner-selected projectless physical-host placement. The exception is retained only for that gap and has no fallback, reroute, rebind, adoption, group, pane, or unrelated lifecycle write. Remove it when native creation can bind the same host/workdir directly.
4. **`skills/amux/scripts/resolve-amp-invocation-policy` and `scripts/amp_invocation_policy_test.go` action name `amux_spawn`.** This is a pure, no-side-effect compatibility resolver for installed historical skill material. It cannot create a thread and an allow result cannot bypass CLI admission. Removing the tuple in this phase would break old installed preflight callers without reducing runtime admission. Sunset it with the invocation-policy compatibility material.
5. **Legacy `worker-spawn` operation readers in `cmd/amux/{aggregate,worker}.go` and `internal/config/operations.go`.** They are immutable diagnostics for an older spawn protocol, not creation call sites. Removing them before store export/freeze would make indeterminate evidence unreadable. They remain read-only and are never retried, reconciled, adopted, converted, or marked successful.
6. **Historical decision/proposal text:** `docs/adr/0001-*`, `0002-*`, `0003-*`, `0005-*`; `docs/proposals/amp-internal-invocation-policy.md`; `issue-175-invocation-policy-probes.md`; `skill-token-efficiency-design-grill.md`; and `symmetric-retirement-disposition-design.md`. These files describe decisions or probes at their recorded time and are not executable guidance. Rewriting them would falsify history. ADR 0007, this inventory, the active skill, README, and public pages are the current contract.
7. **Generic tmux `SendLiteral` and its `amux-spawn-*` temporary buffer prefix in `internal/tmux/tmux.go`.** No spawn path calls it. It remains a generic tested tmux primitive; renaming or deleting it is unrelated to admission and would add risk without closing a call site.

## Recommended next phase

Cut over worker admission separately: publish an exact worker-family generation; classify persisted `worker-adopt` operations and worker rows; reject new `worker pin` and `worker adopt` without stranding partial operations; and retain only exact drain transitions for existing workers. Do not combine that phase with runner admission, group/report/callback admission, state cleanup, store freeze, process stopping, worktree removal, or external issue/PR mutation.
