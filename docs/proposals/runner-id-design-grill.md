---
status: superseded
superseded-by: ../adr/0008-retain-machine-local-runner-host-and-drain-coordination.md
---

# Historical: stable, recognizable Amp runner IDs

This proposal once designed a new stable-ID product graph for runners launched and managed by Amux. [ADR 0008](../adr/0008-retain-machine-local-runner-host-and-drain-coordination.md) retains Amux runner launch while continuing to reject that extra identity configuration, migration, and presentation machinery.

## Lean current decision

Do not implement issues #212–#216 as an Amux feature graph. In particular, do not add:

- an Amux `--runner-id` selector;
- `runner-identity.json` or machine-alias commands;
- an Amux runner-ID generator or registry field;
- named/unnamed compatibility classifications, migration status, or JSON output fields; or
- new launch, restart, reconcile, doctor, completion, or scheduled-maintenance behavior keyed by a second Amux runner-ID identity.

Canonical workdir remains the identity of a retained Amux runner. ADR 0008's #232 boundary requires bounded fail-closed evidence to stop an exact process and prove process and native-catalog absence before removing its row. The existing implementation classifies that evidence but does not yet execute all retained remove/reconcile behavior. Conflict, ambiguity, or unreadable evidence retains the row and process.

Future Amux launch support could pass an explicit native ID to the Amp process without making it a second Amux identity:

```sh
amp --no-tui --runner-id <stable-owner-selected-id>
```

That passthrough is not implemented: current Amux launch and exact-process validation require exactly `amp --no-tui` and reject extra arguments. If added separately, the ID would remain native Amp process configuration rather than a second Amux identity or selector. Amux need not generate, persist, attest, or migrate it; Amux would still retain the canonical-workdir registry and launch the tmux/Amp process, while the OS service activates `amux launch --all` and retains its process group.

## `/amux` native selection boundary

For fresh work, `/amux` may select that already-live native runner through authenticated Amp child creation. A requested `--runner-id <id>` is skill-level intent for the native `create_thread` `runner_id` argument, not an Amux CLI flag.

The coordinator must list live runners immediately before creation, require an exact ID match whose reported working directory is the intended canonical workdir, and make one native request with executor `runner` and that exact `runner_id`. Missing, mismatched, rejected, or indeterminate selection stops without fallback, retry, adoption, or Amux lifecycle state.

## Historical design, not implementation authority

The superseded design proposed IDs shaped as:

```text
<short-hostname>-<normalized-workspace>-<12-hex-path-hash>
```

It also proposed a machine-alias store, generated identity, compatibility classifier, lifecycle migration, output expansion, and Amp Web smoke test. Those choices are intentionally not carried forward. They may inform Amp launch arguments, but they do not authorize a second Amux identity, schema, or selector.

## Current completion boundary

1. Close #212–#216 as superseded product expansion.
2. Keep retained runner removal fail-closed until authoritative process-bound native-catalog presence and positive-absence evidence are available; use #232/#366 as preflight safety evidence, not a runner-admission closure gate.
3. Keep automatic runner launch in Amux; systemd/launchd activates `amux launch --all` and retains its process group.
4. Use the exact live ID only through native Amp runner selection for new child threads.

No new runner-ID store, migration framework, lifecycle classifier, or Amux CLI surface is required.
