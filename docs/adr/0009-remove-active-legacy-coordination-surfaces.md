---
status: accepted
date: 2026-08-30
supersedes-active-drain-routes: 0008
---

# Remove active legacy coordination surfaces

## Decision

Remove the active Amux worker and legacy coordination command surfaces now. Retain the thin machine-local runner host selected by ADR 0008.

The removed active surface includes:

- `amux worker` and worker participation in aggregate lifecycle commands;
- generalized and projectless spawn/adoption;
- shelves and worker teardown;
- groups, worker reports, callbacks, deadlines, and finish authorization; and
- worker cutover publication and other command-reachable coordination migration machinery.

Existing legacy store files are inert evidence. Amux does not migrate, rewrite, drain, or delete them. Read-only parsing needed by the protected #360 inventory remains until that inventory's existing owner-acceptance/disposition plus no-repeat sunset condition activates.

Native Amp remains the only route for new task creation and coordination. `/amux` must not translate requests into removed generalized spawn/adoption or manufacture legacy state.

## Retained product

ADR 0008's runner host remains active without qualification: exact-workdir registry, runner lifecycle and safety, `amux launch --all`, tmux/Amp launch, diagnostics, maintenance, installation/update, and systemd/launchd activation.

No runner-admission closure, full-product retirement, product archive, cutover date, or compatibility-reader date is selected.

## Provider boundaries

`/amux-tycho` is independent of removed worker reports. It retains its receipt consume/acknowledge bridge unchanged until #328's direct-return gate passes.

Provider-specific Claude and Pi evidence remains under those explicit-only skills and separate gates. Any provider route that would require core Amux worker teardown must reject before provider parking, retirement, teardown, or other lifecycle mutation. This decision grants no provider execution, evidence rewrite, lifecycle mutation, cleanup, or alternate teardown authority.

## #360 boundary

The one-time #360 inventory remains protected and read-only. This decision neither runs it nor activates its deletion condition. Its helper, tests, and minimum legacy parsers remain until a separately authorized inventory is accepted or dispositioned and the owner explicitly confirms no repeat is needed.

## Consequences

The product has one current ownership model:

- **Native Amp:** all new task creation and coordination.
- **Amux:** machine-local automatic runners.
- **Legacy stores:** inert evidence, readable only where protected compatibility inspection requires it.
- **Provider bridges:** separate explicit-only evidence routes under their own gates.

Removed commands fail before process or store mutation. No compatibility mutation route remains merely because historical bytes exist.

This ADR supersedes ADR 0008's expectation that core worker/coordination stores retain active drain mutations until family-specific gates. It preserves ADR 0008's runner-host destination and ADR 0007's native-new-work, no-dual-write, provider-gate, and Git/worktree safety decisions.
