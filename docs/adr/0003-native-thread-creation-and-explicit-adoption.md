---
status: superseded
superseded-by: 0007
---

# Historical: native Amp thread creation followed by explicit Amux adoption

> [!CAUTION]
> **Historical only — superseded by [ADR 0007](0007-retire-amux-through-native-cutover-and-staged-drain.md).** This file preserves the rationale and evidence behind the former native-create → Amux-adopt design. Every command, workflow step, preflight, ordering or recovery action, failure-matrix behavior, migration gate, and other imperative statement below is superseded and non-current; do not use it as operational guidance.
>
> **Current boundary:** New native children remain unmanaged by Amux and retain native parent/reply routing only. Only an exact persisted pre-cutover drain-eligible adoption operation may continue its exact allowed next transition under ADR 0007. Do not native-create then adopt, group, report, or otherwise enroll new work into Amux.

## Historical decision (superseded; non-current)

### Historical projectless physical-host exception (issue #297)

Issue #327 narrows this exception around the Amp coordinator's authenticated existing-thread message tool. Core amux does not call or emulate that server tool. The coordinator capability-checks it before mutation, then uses durable `prepare`, `arm`, and `finalize` CLI boundaries around exactly one native message to the exact locally created thread. `prepare` runs one local projectless `amp threads new` in the canonical cwd and retains ownership without presenting a pane; `arm` makes interruption indeterminate and non-retryable; finalize records explicit rejection, indeterminate outcome, or authenticated acceptance with `latestCursor` before a presentation-only client is created. The prompt is never persisted or transported through tmux; only a binding digest is stored. Native success proves exact-thread acceptance/queueing only, not message ID, inference, physical executor/workdir affinity, or execution. The command does not register a project, dispatch to an Orb, require an amux Runner, inspect an optional local `--runner-id`, search for another receiver, or clean up uncertain state. Existing `retained_indeterminate` rows remain unchanged and are never upgraded. `amux worker spawn` remains an exact alias.

The superseded decision modeled worker assignment as a two-owner protocol:

1. Amp's authenticated native thread-creation tool, invoked by the `/amux` skill, owns the single creation invocation and submission of its initial prompt. The skill must choose an explicit executor (`orb`, local execution where supported, or one exact Amp runner ID) and an explicit mode under the current invocation policy. Omission remains backward-compatible `medium`; known linked ChatGPT routing plus known target-mode availability permits task-shaped `low`, `medium`, or `high`, while `ultra` and special modes remain owner-explicit. It never silently falls back to another executor or mode.
2. `amux worker adopt` accepts the exact already-created thread and owns only deterministic local adoption: workspace/session, semantic window, an admission-canonicalized workdir for the new row, tmux client, worker catalog, and optional exact durable group-member intent.

Native creation owns intended execution placement. Adoption does not re-home, migrate, or retarget the thread's native executor, does not verify continued affinity, and a local continued-thread pane is not evidence that later turns run in that pane's workdir. Create the thread directly on the executor and physical workdir intended for its work. In particular, Orb creation followed by physical adoption is never an execution-migration mechanism.

The amux CLI does not invoke, wrap, or pretend to invoke an Amp server tool. Adoption sends no message, presses no Enter, parses no composer or box-drawing frame, and reads no transcript. At the time of this decision, legacy `amux spawn` and `amux worker spawn` became deterministic, non-mutating exit-2 migration tombstones; the issue #297 exception above later restored only the lean historical local route.

The non-current prototype command was:

```sh
amux worker adopt --thread <exact-id-or-url> --workspace <workspace> \
  --window <semantic-window> --workdir <canonical-existing-directory> \
  [--group <one-exact-group>] [--dry-run] [--json]
```

In that historical contract, JSON identified the exact thread as the worker resource and reported workspace, window, workdir, local state, and receipt source `amp_native_create_thread`. New adoption canonicalized its owner-supplied workdir before persistence, pane identity, and output. Doctor reported the authoritative catalog spelling unchanged; a preserved legacy relative value was not canonicalized against the invoking directory and was not a physical-location claim. Adoption and doctor reported native executor, native runner ID, and execution affinity as `unknown`, because amux had no authoritative source for them. The local workspace, window, workdir, and local-state fields described only local ownership. The receipt source named caller-side provenance; it was not proof of fields absent from the native receipt contract.

## Historical trust boundary (superseded; non-current)

The native creation receipt is deliberately two-part: the authenticated request supplied by the coordinator and the successful tool result returned for that one invocation. The current `create_thread` contract binds the complete initial prompt, project, explicit executor, exact Amp runner ID when `runner`, and explicit agent mode; success returns the created thread ID and URL and echoes the selected project, executor, mode, and runner ID where applicable. Those request fields plus the successful result are trusted caller-side receipt data. The coordinator issues one invocation and, if its result is indeterminate, preserves the invocation evidence rather than guessing or retrying. Amp owns handling of the initial prompt within that invocation; message delivery and inference completion are neither separate receipt fields nor facts re-proven by amux.

The [Amp manual](https://ampcode.com/manual) independently documents the four built-in modes, `createThread({ executor: "orb" })`, exact runner selection through `executor: { type: "runner", id }`, and that a runner accepts remotely created threads in the directory where it was started. It also documents the narrower message boundary: appending a user message returns when Amp accepts it, not when inference completes. The creation result does **not** contain a prompt digest, transcript proof, inference completion, separate delivery acknowledgement, canonical workdir, or Amp installation identity. Native creation therefore must not claim those absent fields.

For a physical runner, the coordinator selects one exact live runner immediately before creation and records its authoritative runner ID and reported working directory. The manual's current-directory contract plus the exact Linux/Darwin physical proofs establish physical runner create/adopt dispatch evidence for the migration; they do not establish finish or full removal parity. The returned thread ID still remains the only remote identity passed to adoption. On a new adoption path, amux locally verifies its admission-canonicalized workdir and tmux/catalog ownership. A worker report may corroborate the remote checkout, but it is operational evidence rather than an amux receipt field.

If physical state matters—including a dirty worktree that exists only on one machine—the native creation request, retained caller-side receipt, and assignment name the exact physical runner ID and canonical workdir. amux adoption and doctor still report native runner identity and affinity as `unknown`. Recovery creates the worker on that exact runner, or uses a separate explicit handoff that leaves immutable worktree ownership with the physical worker. It never creates in an Orb and adopts physically to imply migration. If the native API cannot authoritatively report current affinity, the value remains `unknown`; thread environment, local panes, and adoption state must not be used to infer it.

For the historical adoption path, amux locally revalidated all facts it owned:

- thread ID/URL canonicalization and current active status through bounded `amp threads list --json` inventories;
- existing canonical workdir directory;
- exact worker workspace/window/thread/workdir catalog ownership;
- absence of canonical-workdir ownership by an amux Runner;
- absence of another worker using the canonical workdir on this new path;
- requested tmux window identity and absence of another amux-managed pane for the thread;
- exact group syntax and existing local membership.

Adoption does not export, search, continue semantically, or otherwise read the thread. Consequently it cannot locally prove the native thread's remote workdir or initial message. A future richer native receipt must be documented before those become adoption inputs. The skill must pass the exact returned thread and the intended local workdir without claiming that amux revalidated a remote workdir.

An exact Amp runner ID was native dispatch identity: it selected one live Amp `--no-tui --runner-id` process for server-owned thread creation. An amux Runner was a separate local lifecycle resource whose identity was a canonical workdir and whose tmux window was generated. Neither identity implied, mapped to, or owned the other. The historical adoption path rejected canonical-workdir overlap with an amux Runner but never treated its workspace or generated window as an Amp runner ID.

## Historical preflight, ordering, and recovery (superseded; non-current)

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

## Historical failure matrix (superseded; non-current)

The behaviors in this table record the former contract and are not current instructions for native work.

| Native/adoption state | Historical required behavior (non-current) |
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

## Historical migration and removal gate (superseded; non-current)

At the time of this superseded decision, the `/amux` skill used native create → adopt for new workers. That route is no longer current: new native children remain unmanaged, and only exact persisted pre-cutover drain-eligible adoption operations may continue their exact allowed next transition under ADR 0007. The former decision kept existing workers, groups, reports, callbacks, shelves, restart, and finish compatible. Legacy operation records remained readable, immutable diagnostic evidence through v0.3.x: they were never reinterpreted, retried, deleted, converted, or marked successful.

Compatibility spawn may be removed only in a later explicit change after all of the following are recorded:

- one successful Orb native-create/adopt proof with no TUI delivery — recorded on [#269](https://github.com/zainfathoni/amux/issues/269);
- one exact physical Amp runner proof on Linux and one on Darwin — recorded on [#269](https://github.com/zainfathoni/amux/issues/269);
- documented stable native request/result semantics sufficient for physical native-create/adopt parity, including explicit executor selection and the runner current-directory contract — documented above, without inventing a richer receipt;
- interruption, duplicate, inactive, conflict, group/report, restart, and finish parity — unit coverage plus bounded physical evidence are recorded on [#269](https://github.com/zainfathoni/amux/issues/269);
- an explicit migration window and rollback plan — tracked by [#272](https://github.com/zainfathoni/amux/issues/272);
- fail-closed disposition for preserved legacy operations — recorded and closed as superseded by the native workflow in [#259](https://github.com/zainfathoni/amux/issues/259).

The migration window keeps deprecated compatibility spawn in v0.2.x and targets v0.3.0 for a reject-only, non-mutating tombstone before later schema cleanup. During v0.3.x, legacy operation files remain readable and diagnosable but never auto-convert, retry, delete, or become successful. Before release, rollback may revert the v0.3 tombstone/removal only while retaining #259's fail-closed legacy-operation disposition. After release, downgrade is permitted only to a pinned v0.2.x build containing or backporting that disposition; if no such build exists, rollback is a forward fix rather than a binary downgrade. No rollback path may resubmit an indeterminate operation. The complete implementation boundary and acceptance criteria live in #272.

## Historical consequences

Assignment delivery no longer depends on terminal size, composer geometry, pane text, or Enter safety in the preferred architecture. amux remains a deterministic local lifecycle manager rather than a remote model router. Native request/result semantics and physical creation parity are now recorded without expanding amux's trust boundary: absent digest, acknowledgement, transcript, installation, and remote-workdir fields remain absent rather than fabricated. Legacy TUI removal proceeds only through the explicit migration in #272.
