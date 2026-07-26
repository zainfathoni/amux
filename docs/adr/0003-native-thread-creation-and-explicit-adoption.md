---
status: accepted
---

# Use native Amp thread creation followed by explicit amux adoption

## Decision

Worker assignment becomes a two-owner protocol:

1. Amp's authenticated native thread-creation tool, invoked by the `/amux` skill, owns exactly one thread creation and its initial-message delivery. The skill must choose an explicit executor (`orb`, local execution where supported, or one exact Amp runner ID) and an explicit mode (`medium` unless the owner names another mode). It never silently falls back to another executor or mode.
2. `amux worker adopt` accepts the exact already-created thread and owns only deterministic local adoption: canonical workspace/session, semantic window, canonical workdir, tmux client, worker catalog, and optional exact durable group-member intent.

The amux CLI does not invoke, wrap, or pretend to invoke an Amp server tool. Adoption sends no message, presses no Enter, parses no composer or box-drawing frame, and reads no transcript. Legacy `amux spawn` remains available as a deprecated compatibility path until the removal gate below is satisfied.

The prototype command is:

```sh
amux worker adopt --thread <exact-id-or-url> --workspace <workspace> \
  --window <semantic-window> --workdir <canonical-existing-directory> \
  [--group <one-exact-group>] [--dry-run] [--json]
```

JSON identifies the exact thread as the worker resource and reports workspace, window, canonical workdir, local state, and receipt source `amp_native_create_thread`. This source names the caller-side provenance; it is not proof of fields absent from the native receipt contract.

## Trust boundary

The locally available native `create_thread` tool contract proves these properties: the caller supplies the initial prompt, project, explicit executor and (when `runner`) exact Amp runner ID, and explicit agent mode; a successful tool result returns the created thread ID and URL. Those request fields plus the returned exact ID/URL are trusted native receipt data. Exactly-once creation and initial delivery are owned by that one native invocation, not re-proven by amux.

No authoritative local/API documentation currently proves a stable richer receipt containing selected executor, selected mode, canonical workdir, initial-message digest, delivery acknowledgement, or Amp runner installation identity. Those fields are an explicit blocker for an amux-owned creation receipt. The prototype therefore implements only adoption and never invents, persists, or validates such fields.

amux locally revalidates all facts it owns:

- thread ID/URL canonicalization and current active status through bounded `amp threads list --json` inventories;
- existing canonical workdir directory;
- exact worker workspace/window/thread/workdir catalog ownership;
- absence of canonical-workdir ownership by an amux Runner;
- absence of another worker using the canonical workdir on this new path;
- requested tmux window identity and absence of another amux-managed pane for the thread;
- exact group syntax and existing local membership.

Adoption does not export, search, continue semantically, or otherwise read the thread. Consequently it cannot locally prove the native thread's remote workdir or initial message. A future richer native receipt must be documented before those become adoption inputs. The skill must pass the exact returned thread and the intended local workdir without claiming that amux revalidated a remote workdir.

An exact Amp runner ID is native dispatch identity: it selects one live Amp `--no-tui --runner-id` process for server-owned thread creation. An amux Runner is a separate local lifecycle resource whose identity is a canonical workdir and whose tmux window is generated. Neither identity implies, maps to, or owns the other. Adoption rejects canonical-workdir overlap with an amux Runner but never treats its workspace or generated window as an Amp runner ID.

## Preflight, ordering, and recovery

One machine mutation lock covers adoption. Before mutation, amux loads and validates worker, Runner, group, Amp active/archive, and tmux ownership state. Every known conflict rejects with exit `2`; no catalog, group, tmux, label, message, or remote-thread mutation occurs.

After complete preflight, mutation ordering is deterministic:

1. atomically persist a deterministic `worker-adopt:<thread>` operation binding a hash of the exact thread, workspace, window, canonical workdir, and optional group;
2. atomically persist the exact worker row;
3. atomically persist optional exact group-member intent;
4. add-only ensure the optional member label using the existing group API;
5. create and post-verify the local tmux client that runs only `amp threads continue <exact-thread>` in the canonical workdir;
6. mark the bound adoption operation succeeded.

This is intent-before-side-effect ordering. Interruption after any persistence step leaves sufficient durable ownership for only the identical command to resume; a changed group or local identity conflicts with the request hash. Interruption after tmux creation is recovered by exact tmux inspection. A label failure retains worker and group intent as existing visible drift. The command never rolls back durable ownership by guessing whether a later side effect happened.

An exact repeat is idempotent: exact catalog, group, and tmux state returns a skipped result and performs no external label or message action. A partial exact repeat completes only missing later phases. Any changed thread, workspace/window, canonical workdir, Runner ownership, group, or tmux identity is a conflict rather than a rebind.

## Failure matrix

| Native/adoption state | Required behavior |
| --- | --- |
| Native creation succeeds with exact ID/URL | Invoke adoption once with that exact identity; never create another thread to “confirm” it. |
| Native creation fails before a receipt | Stop. The native tool/runtime owns whether retry is safe; amux has nothing to adopt. |
| Native creation result is indeterminate | Preserve invocation evidence and stop. Never silently invoke creation again and never guess a thread from list/search/transcript state. |
| Receipt contains an unsupported richer field | Ignore it for v1 adoption; do not claim validation. Document and version a future contract before trusting it. |
| Exact thread is active | Continue local preflight. |
| Exact thread is archived or missing | Exit `2`, no mutation. Unarchive or resolve native identity explicitly; do not substitute another thread. |
| Requested thread conflicts with worker catalog or another managed pane | Exit `2`, no mutation. |
| Requested workspace/window belongs to another thread/workdir | Exit `2`, no mutation. |
| Canonical workdir belongs to another worker or an amux Runner | Exit `2`, no mutation. This says nothing about an Amp runner ID. |
| Requested tmux window is ambiguous or has mismatched start identity | Exit `2`, no mutation. |
| Exact worker is already fully adopted | Exit `0` as skipped; no Amp label, message, or TUI action. |
| Adoption or worker intent persisted, then interrupted | Retry the identical command; the request hash rejects changed identity/group intent and resumes later phases without rebinding. |
| Group intent persisted, then label or tmux creation fails | Exit `1`; retain intent and retry identically. Additive label ensure may safely repeat during partial recovery. |
| Label ensure fails | Exit `1`; retain local worker/group/tmux state and visible label drift. |
| Native thread's remote workdir or delivered message is questioned | Block on native receipt/documentation evidence. Adoption must not read a transcript or fabricate parity. |

A creation receipt never authorizes merge, finish, archive, teardown, cleanup, report acknowledgement, or finish authorization. Existing group/report/callback/shelf/restart/finish semantics remain unchanged after adoption.

## Migration and removal gate

The `/amux` skill should prefer native create → adopt for new workers and mark TUI delivery deprecated. Existing workers, operation records, groups, reports, callbacks, shelves, restart, and finish remain compatible. `amux spawn` is not removed or broadly refactored in this prototype, and closed parser work is not revived.

Compatibility spawn may be removed only in a later explicit change after all of the following are recorded:

- one successful Orb native-create/adopt proof with no TUI delivery;
- one exact physical Amp runner proof on Linux and one on Darwin;
- documented stable native receipt semantics sufficient for physical native-create/adopt parity, including executor selection and workdir claims that the migration needs;
- interruption, duplicate, inactive, conflict, group/report, restart, and finish parity;
- an explicit migration window and rollback plan.

## Consequences

Assignment delivery no longer depends on terminal size, composer geometry, pane text, or Enter safety in the preferred architecture. amux remains a deterministic local lifecycle manager rather than a remote model router. The separation intentionally leaves a receipt-proof blocker: local adoption can be shipped and tested now, while physical creation parity cannot be claimed until Amp documents the missing receipt fields and Linux/Darwin runner evidence exists.
