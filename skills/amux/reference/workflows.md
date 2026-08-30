# amux workflows

## Create fresh native work

Load [`amp-invocation-policy.md`](amp-invocation-policy.md). Use one authenticated native Amp `create_thread` call on the exact intended Workspace Project/Orb or exact live runner/workdir. Send a lean task prompt with task, acceptance criteria, relevant constraints, validation, and expected reply. Keep native parent/reply routing only.

Never call `amux spawn`, create or adopt an Amux worker, or write group/report/callback/deadline/shelf/finish state. If native creation is rejected or indeterminate, stop without fallback or retry.

## Sprawl independent issue threads

Use only when the user explicitly asks for `/amux sprawl`. Select independent issues, apply the invocation preflight separately, and create native children only where approved. Keep each child independently scoped and preserve native replies. Do not create Amux coordination state.

## Health runners

`/amux health` is skill-only. Run read-only runner diagnostics first:

```sh
amux --json runner list --all
amux --json runner doctor --all
amux --json install doctor
```

Report exact workdir, registry/runtime state, and blocker. Do not mutate processes or rows unless the owner separately requests the corresponding runner command.

## Sweep worktree inventory

`/amux sweep` is the protected one-time read-only migration inventory for the staged Amux drain. It is a read-only skill workflow, not a CLI command. This documentation does not authorize a run; a future run requires a separate explicit owner request.

After that authorization, run the existing helper with every intended filesystem root. Coverage is explicit and uncapped. It emits a full outer join over four independent authorities: Git worktree registrations, `workers.tsv`, `reports.json member_thread joined through workers.tsv`, and filesystem presence. `report_id` and `groups.tsv` never establish a workdir. Every row retains `removal_verdict=NOT_EVALUATED`.

The workflow performs no fetch, cleanup, reconciliation, removal, unlock, prune, backup-ref mutation, lifecycle/drain/provider action, or store write. Preservation-locked historical resources remain inert evidence. Parsing does not revive any removed command or authorize migration, reconciliation, teardown, or deletion.

Then, after the owner records acceptance of one complete staged-drain inventory, or records an explicit incomplete/error disposition, and confirms that no repeat inventory is required, delete `scripts/sweep-inventory`, its sweep-only tests, and every `/amux sweep` route/reference before the next Amux release. Do not promote the helper into the CLI, retain it as a standing diagnostic, schedule it, or extend it for post-drain monitoring.
