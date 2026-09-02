# Experimental Claude delegation recovery

Use only after explicit owner recovery authorization. Core Amux workers and worker teardown no longer exist, so historical Claude evidence bound to an Amux worker has **no executable completion or cleanup route**.

## Required disposition

1. Inspect only the exact owner-supplied private store and immutable origin. Do not search for another store, worker, pane, process, or ownership binding.
2. Preserve the Claude process, receipt, report, packet, runtime metadata, artifacts, worktree, branch, provider lifecycle registry, and durable origin fence byte-for-byte.
3. Reject before provider parking, retirement, quarantine, detach, exact-pane disposal, teardown, or fence release when the requested outcome would require a later core Amux worker transition.
4. Do not invoke `lifecycle worker-teardown`, `worker-teardown-release`, `detach-indeterminate-worker`, any pair-retirement/disposal route, or an older Amux binary. Those historical routes cannot complete after core worker removal and must not partially mutate provider state first.
5. Do not kill a pane or PID, inject input, acknowledge or manufacture a report, rewrite evidence, infer process absence, launch a replacement, or reinterpret an old receipt as a native Amp thread.

## Read-only diagnosis

The installed helper exposes only bounded `diagnose` and exact historical `receipt show` inspection. It contains no legacy mutating dispatcher or environment-variable bypass. Exact receipt inspection may identify the existing blocker and preserved evidence. Pane prose, logs, idle state, process exit, and wake-up tokens are not semantic reports or lifecycle authority. A malformed, missing, unreadable, ambiguous, or drifted store remains indeterminate; preserve it.

Provider-specific replacement routes may be introduced only after their own authenticated field evidence and owner acceptance. Until then, the only safe recovery result for worker-bound historical evidence is a bounded blocker stating that the process and all evidence remain retained. Do not substitute native task coordination, the retained runner registry, `/amux-tycho`, or OS service supervision as cleanup authority.
