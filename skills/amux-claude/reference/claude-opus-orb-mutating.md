# Fresh-Orb mutating Claude Opus route is blocked

This provider-owned `/amux-claude` route is a **non-authorizing scaffold** for [issue #254](../../../docs/proposals/issue-254-fresh-orb-mutating-opus-workflow.md). It does not permit a repository mutation, create an Orb, invoke Claude, persist launch intent, import an artifact, verify a result, acknowledge capacity, archive a thread, or clean a workspace. Generic fresh-Orb mutation remains disabled and issue #254 remains open.

Run the privacy-safe diagnostic only:

```sh
python3 experimental/fresh-orb-mutating/fresh_orb_workflow.py diagnose
```

It exits `2` with `native_fresh_orb_mutation_adapter_unavailable`. Every authorizing or lifecycle command is absent and fails before state or external mutation.

## Why current primitives cannot authorize this workflow

Repository inspection found useful local primitives, but none establishes the required fresh-Orb boundaries:

- Native Amp `create_thread` creates an Orb, but this skill helper has no authenticated, durable launch-intent/completion receipt API to consume. Core `worker adopt` observes an active thread and local tmux identity while intentionally leaving native executor placement unknown.
- Native Amp file tools can transfer a named file, but this helper has no authenticated import receipt binding the origin, Orb, operation, report, artifact bytes, and native transfer event.
- Core and local Claude receipt stores provide replay-safe local operations and exact tmux/process observations. They do not observe a headless Orb process, Orb workspace, or native cleanup lifecycle.
- The local Claude mutation route explicitly provides logical policy confinement, not OS isolation. Its general shell authority cannot prove absence of push, issue/infrastructure mutation, credential access, out-of-worktree writes, or recursive launch.
- The narrow Pi adapter validates a constrained file diff but neither provides the credentialless/networkless repository mutation-and-commit executor nor the native Orb receipts required here.
- Core acknowledgement and operation records durably store caller actions; they do not provide an operation-bound, single-use native owner challenge for unknown capacity.

Caller-provided paths, identities, hashes, capability claims, model-usage summaries, process assertions, or lifecycle assertions cannot fill these gaps. Final Git inspection cannot prove that external effects did not occur.

## Required native adapter/API

Implementation must remain blocked until an Amp-owned boundary provides all of these as authenticated, durable, operation-bound receipts rather than caller JSON:

1. A single-use owner challenge for unknown capacity, bound to origin, exact task, exact `claude-opus-4-8`, repository, immutable base, and one launch attempt.
2. Atomic native launch intent plus completion for exactly one newly created Orb, including project/repository/base and executor identity.
3. A credentialless, networkless mutation executor with narrow owned edit/check/commit operations, one dedicated clean worktree/branch, no general shell, no outside-worktree writes, and no recursive launch.
4. The complete existing Claude result-validator output bound without rewriting to the exact invocation and native execution receipt.
5. Native artifact/report import that binds exact bytes and source identity before Orb disposal.
6. Authenticated headless-process absence plus replay-safe archive and workspace-cleanup intent/result transactions, with acknowledgement gates and durable failures.

After those APIs exist, the provider workflow can add commit-bearing transfer and independent NUL-safe Git object/tree/mode verification. Until then, do not add an `intent`, `authorize`, `launch`, `transfer`, `verify`, `acknowledge`, `archive`, or `cleanup` command to this scaffold and do not run a real pilot.

The existing read-only fresh-Orb recipe and local tmux provider workflows remain unchanged.
