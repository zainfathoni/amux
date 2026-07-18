# Experimental read-only Claude delegation contract

This is the single normative contract for the skill-owned local experiment. It has **no compatibility guarantee**, adds no `amux` CLI resource, and does not make Claude an Amp worker, runner, group member, or synthetic thread. Use it only after an explicit request to delegate read-only analysis. The helper is [`../experimental/claude-delegation/claude_delegation.py`](../experimental/claude-delegation/claude_delegation.py).

## Authority and launch surface

A **thinker** is one fresh, delegation-scoped, full interactive Claude Code process in a dedicated same-machine tmux window. Its authority is `read_only`: it may use only Claude's `Read`, `Grep`, and `Glob` tools plus the experiment's bounded semantic-submission MCP tools. The launch denies shell, edit, write, notebook, agent, web, skill, non-managed inherited settings and hooks, extra directories, slash commands, and automatic interactive input. Managed policy has higher precedence and cannot be disabled by session settings, so its runtime effect remains explicit diagnostic evidence rather than an assumed guarantee. This is policy confinement, not an OS sandbox; disclose both limitations and constrain the supplied sources accordingly.

The coordinator supplies a private UTF-8 launch packet. Never commit or copy that packet, prompts, transcripts, pane captures, tool streams, secrets, raw receipts, or complete artifacts. Pane text is diagnostic only. A valid helper-submitted semantic message is authoritative.

Before a read-only packet is finalized, `launch policy-digest` returns the deterministic digest of the read-only launch policy without reading a packet, inspecting a worktree or tmux, or writing private state. It rejects other workflows. The final packet must include that exact digest in every semantic envelope template, and the final read-only launch request must carry the same value as `expected_launch_policy_digest`. `launch plan` validates that expectation against the selected policy before any platform, tmux, packet, or worktree probe, then verifies Darwin, tmux, the installed Claude flags, exact canonical repository, base commit, clean dedicated linked worktree without optional Git locks, private packet mode, and deterministic packet, launch-policy, and launch-command digests. It is a dry run: it writes no receipt, runtime configuration, tmux window, or Git index metadata. Missing or mismatched expectation stops the workflow before receipt creation. `launch execute` validates the same expectation before receipt or runtime inspection, then requires an already-created receipt whose immutable digests match and a stable event ID. It durably reserves launch intent before writing owner-only MCP/settings files and launching one detached tmux window. A completed exact replay returns the original identity without relaunching; an intent lacking a result is indeterminate and forbids automatic relaunch.

## Receipt and event rules

All receipt mutations use one blocking `experimental.lock` domain. The private state parent is mode `0700`; lock, receipt, and generated runtime files are mode `0600`. Commits flush a temporary file, atomically replace the receipt file, and flush its parent directory.

The immutable binding contains protocol version, delegation ID, nonce, task/question identities, opaque origin Amp thread, repository/base/canonical workdir, thinker/read-only authority, bounded task reference, and packet/policy/launch digests. Mutable routing contains only current target and optional machine-local recovery route. Routing changes never rewrite provenance or binding.

Every event has a caller-supplied stable ID. Exact replay returns `duplicate`; reuse for different content fails before mutation. Events remain append-only even though the receipt also materializes current state. Report transitions are exactly:

`valid_report → delivered → acknowledged → verified_parked`

The machine-local inbox consumption creates delivery. Coordinator acknowledgement is separate and only permits parking. A wake-up notification is not delivery or acknowledgement.

An acknowledged and identity-verified parked receipt records a date 30 days later when it becomes cleanup-eligible. Eligibility never authorizes deletion; this experiment performs no automatic cleanup.

## Semantic envelopes

Both `thinker_report` and `input_request` envelopes bind protocol version, delegation ID, nonce, message ID, `in_reply_to`, task ID, origin thread, repository, base, workdir, thinker/read-only authority, launch-policy digest, and timestamp to the receipt.

A report explicitly accepts the role and exclusions and records `complete` or `blocked`, bounded verdict and rationale, and bounded lists of evidence, assumptions, unsupported claims, blockers, verification, and references. `changed_artifacts` must be empty. Validation establishes shape and correlation, not truth or acceptance.

An `input_request` records one of `clarification`, `decision`, or `missing_evidence`, plus a bounded question and blocking reason. Inbox consumption may mark it seen but not resolved. Only an explicitly observed correlated Claude-side acceptance (`input accept`) or a later explicit superseding source event resolves it. There is no automatic response injection or automatic follow-up.

Semantic fields must contain concise non-content evidence only. Keys for prompts, transcripts, pane captures, tool streams, secrets, artifact content, and complete artifacts are rejected recursively. This enforces shape, not meaning: forbidden content embedded in an otherwise allowed string cannot be detected mechanically and still requires deliberate privacy review. References must point only to deliberately allowed sources; never use private operational evidence as a reference.

## Identity, notification, and diagnostics

Session acquisition requires the exact tmux session/window/window ID/pane ID recorded by this receipt's single `launch_completed` event, then binds pane PID and creation identity, canonical workdir, Darwin kernel process start identity and argv digest, one exact Claude `--session-id` pair, and launch-command digest. A matching Claude process in another pane is rejected. Explicit parking is allowed only after acknowledgement with no unresolved input request. It durably reserves intent, re-verifies the complete identity immediately before killing that exact pane, and records a separate result. An interrupted intent requires explicit `{"recover":true}` reconciliation; exact confirmed pane absence may complete that authorized intent, while ambiguous inspection remains unresolved. Failure preserves the acknowledged receipt for recovery.

Optional Amp wake-up first requires `amp inspect` of one exact live pane whose tmux and process arguments identify the expected origin thread. Notification sends only SHA-256 correlation tokens after the report commit. Its durable intent prevents concurrent or interrupted replay from sending another token. If an interrupted attempt has no result, exact replay durably records deterministic `unavailable` evidence without resending. Missing, stale, ambiguous, or changed identity fails closed and leaves the machine-local inbox recoverable. Notification is not delivery.

`diagnose` is read-only and classifies each capability as `supported`, `unavailable`, or `untested`. Capacity output is bounded to provider source/confidence and utilization windows; account and subscription identity is excluded. An untested runtime capability must never be promoted to supported by assumption.
