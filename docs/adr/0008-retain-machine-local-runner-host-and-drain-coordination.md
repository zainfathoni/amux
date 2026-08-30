---
status: accepted
date: 2026-08-30
partially-supersedes: 0007
---

# Retain the machine-local runner host and drain legacy coordination

## Decision

Amux remains an active, thin machine-local host layer for Amp runners. It is not being fully deprecated or archived.

Native Amp owns new task coordination: thread creation, executor placement, parent/reply routing, messaging, waiting, and thread archive state. Amux must not create workers, adoption records, groups, reports, callbacks, deadlines, finish authorization, shelves, or provider receipts merely to mirror native work.

Amux retains:

- the machine-local runner registry and exact canonical-workdir bindings;
- runner pin, list, doctor, park, remove, and the minimum fail-closed reconcile and safety behavior needed to operate those bindings;
- `amux launch --all` as the automatic graphical-login/boot launcher;
- tmux and `amp --no-tui` process launch;
- install, update, maintenance, and doctor diagnostics; and
- systemd and launchd integration for activation and process-group retention.

Legacy coordination remains drain-only. Generalized worker spawn/adoption, work groups, reports, callbacks, deadlines, finish authorization, shelving coordination state, and their stores admit no ordinary native work. They retain only the exact compatibility reads and mutations needed to preserve or truthfully drain pre-cutover records. Provider-specific lifecycle bridges drain only after their replacements satisfy their existing proof gates.

## OS activation boundary

An operating-system service manager activates Amux; it does not replace Amux as the runner launcher or registry owner.

At graphical login or boot, the managed job runs `amux launch --all`. Amux reads its exact machine-local bindings and launches the tmux/Amp process group. The service manager keeps that process group associated with the login service after the one-shot command returns:

- systemd uses a user service with `Type=oneshot` and `RemainAfterExit=yes`;
- macOS uses a RunAtLoad LaunchAgent with `AbandonProcessGroup=true`.

Those patterns are deployment contracts verified by Fleet. Their machine-specific unit definitions remain outside this repository. Direct service-manager launch of replacement `amp --no-tui` processes is not the destination.

Runner maintenance remains a separate short-lived scheduled Amux operation. It may update Amp and recycle verified runners when the executable changes; it is not a resident supervisor and does not displace login/boot launch.

## Safety boundary

Issue #232 and PR #366 provide safety evidence for retained runner operations. PR #366 is a preflight-only classifier: it does not stop processes, mutate or remove rows, close runner admission, or mandate migration away from Amux.

Retained runner operations continue to fail closed:

- canonical workdir is the machine-local Amux runner identity;
- exact tmux pane, process incarnation, executable, argv, workdir, and native-catalog evidence are revalidated where the operation requires them;
- conflicting, unreadable, or unproven evidence retains the row and process;
- row-absent processes are never inferred, adopted, or stopped without exact prior Amux ownership evidence; and
- runner commands never own or mutate remote agent threads.

Stable native Amp runner IDs may still be useful for selecting an already-live runner in `create_thread`, but they do not replace Amux's workdir registry or launcher and do not require the retired #212–#216 Amux runner-ID product graph.

## Legacy drain boundary

ADR 0007 remains authoritative for these decisions:

1. ordinary new work uses native Amp creation and is never automatically adopted into Amux;
2. generalized spawn/adoption and native-work coordination do not reopen;
3. old state follows only an exact persisted pre-cutover next transition, with no dual-write or manufactured terminal truth;
4. groups, reports, callbacks, deadlines, finish authorization, shelves, and provider receipts freeze and lose writers only after their evidence-based drain gates;
5. `/amux-tycho` remains unchanged until #328's authenticated direct structured-return gate passes, and old/new routes never run for the same task; and
6. Git/worktree removal safety remains independent of coordination lifecycle authority.

This ADR supersedes ADR 0007 only where ADR 0007 selected full Amux product retirement, runner-admission closure, native/OS replacement runner launch, a complete-product compatibility-reader sunset, or product archive. No replacement cutover dates or reader-window dates are selected here.

## Temporary sweep boundary

PR #360 remains a one-time, read-only migration inventory under its existing owner gate. Do not run it or delete its helper, tests, or `/amux sweep` routes unless the owner first accepts one complete inventory or explicitly dispositions an incomplete/error result **and** confirms no repeat is needed. Only then does the existing delete-before-next-release condition activate.

## Consequences

The destination has three unambiguous ownership layers:

- **Native Amp:** all new task creation and coordination.
- **Retained Amux:** automatic machine-local runner registration, launch, process lifecycle, reconciliation safety, maintenance, and diagnostics.
- **Legacy Amux stores:** compatibility and drain only; no admission from ordinary native work.

Amux can shrink as legacy coordination stores reach their gates without removing the runner-launch core. No complete Amux product archive is planned by this decision.

## Non-goals

- reopening generalized spawn, adoption, groups, reports, callbacks, deadlines, finish authorization, or shelving for new work;
- promoting a provider-specific bridge without its existing proof gate;
- adding a resident Amux supervisor or replacing service-manager behavior with one;
- moving the retained runner registry or launcher into Fleet, native Amp, systemd, or launchd;
- running the #360 inventory or activating its sunset condition;
- choosing cutover dates, reader-window dates, or a release; or
- changing machine-specific Fleet, nix-home, or omarchy-home configuration from this repository.

## Follow-up authority

[The staged-drain disposition ledger](../staged-drain-disposition.md) tracks the remaining coordination/provider drain and retained-host work. Documentation does not itself authorize process operations, store mutation, issue edits, releases, or deployment changes.
