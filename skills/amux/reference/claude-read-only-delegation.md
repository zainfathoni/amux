# Delegate read-only analysis to Claude experimentally

Load [`claude-delegation-contract.md`](claude-delegation-contract.md) before taking any action. This capability-gated Darwin/Linux workflow is explicit-only and unstable. It orchestrates a local helper beside the skill; do not invent `amux claude` commands, Amp identities, groups, autonomous selection, mutating Linux support, or stable API promises.

Set `HELPER` to `experimental/claude-delegation/claude_delegation.py` within the installed skill and optionally pass `--state-dir <private-directory>` before the helper subcommands below. Each mutating request is one JSON object on stdin. Generate event IDs and the 32-byte nonce once, persist them outside the repository, and reuse the same values only for exact retries.

## 1. Preflight

Run `python3 "$HELPER" diagnose` and show the bounded result. Require a supported Darwin or Linux exact-process-identity capability, the documented Claude flags, and tmux. On Linux, support requires readable `/proc/<pid>/stat`, NUL-delimited `cmdline`, and `exe`; a missing, denied, unstable, or ambiguous source blocks launch. `untested` remains untested; do not call it supported.

Capacity is independent. When trustworthy provider-reported capacity is unavailable, autonomous delegation is unavailable. A user-requested read-only pilot may proceed only when the user visibly acknowledges the unavailable capacity in the authorizing conversation; restate that acknowledgement and the unavailable diagnostic before planning. This does not create quota evidence, bypass a configured reserve floor, or authorize mutating delegation. Otherwise stop. Prepare one clean dedicated linked worktree at the exact base and one owner-only launch-packet file containing the complete initial task, allowed source roots, correlation fields, exclusions, and report schema.

Pass its path and the intended repository, base, tmux session/window, Claude session UUID, delegation ID, and one stable launch event ID to `launch plan`. Record only the returned SHA-256 digests. Stop on any unavailable required capability or mismatch; do not launch.

Completion: diagnostics are disclosed, the worktree/packet are private and exact, and the dry-run digests are available without any local mutation.

## 2. Create the receipt

Submit `receipt create` with the contract's complete immutable binding and routing such as `{"target":"machine_local_inbox"}`. The origin thread is immutable provenance; target and recovery routing are mutable. Exact create replay must return `duplicate`; never change an identity to work around a conflict.

Completion: the owner-only receipt durably exists before tmux launch and `receipt show --delegation-id <id>` matches the intended binding and route.

## 3. Launch and acquire

Submit the identical launch-plan request to `launch execute`. Do not type, paste, or inject anything afterward. Using the exact returned pane ID and chosen Claude session UUID, submit a fresh event to `session acquire`. Acquisition must match the receipt's canonical workdir and launch-command digest.

Completion: the receipt contains one exact `session_acquired` incarnation; pane echo, idle state, and process existence do not establish semantic receipt.

## 4. Recover and deliver

Claude may call only `submit_report` or `submit_input_request` through the private strict MCP configuration. Inspect `receipt show` from the machine-local inbox rather than scraping the pane.

For a valid report, optionally inspect a separately identified live origin Amp pane with `amp inspect --pane <pane-id> --origin-thread <thread-id>`, then pass that exact target to `notify amp-pane`. Notification is not delivery and failure is benign recovery state. Consume the exact report with a fresh `inbox consume` event to create `delivered`.

For an input request, consume it to mark it seen, answer only through deliberate manual Claude interaction, and record `input accept` only after explicit correlated Claude-side acceptance. Never perform automatic composer response injection or automatic follow-up.

Completion: the exact semantic message is durably visible and explicitly consumed, or the factual unresolved input/recovery state remains recorded.

## 5. Acknowledge

Independently inspect the consumed report. Submit `report acknowledge` with its exact message ID only when consumption is confirmed. A notification, pane text, or report validity is insufficient.

Completion: the receipt state is `acknowledged`; this does not itself park Claude.

## 6. Park explicitly

Only after acknowledgement and an explicit parking decision, submit `session park` with a fresh event ID. The helper durably reserves parking, re-verifies the full acquired incarnation, and kills only its exact tmux pane. It records `verified_parked` and 30-day cleanup eligibility, but performs no cleanup. If the first call is interrupted after intent persistence, do not issue a new event: inspect and retry that exact event with `"recover":true` only as the recovery reference directs.

Completion: the receipt records `verified_parked`, or retains the acknowledged state and a factual park failure for recovery. Do not automatically park, remove files, worktrees, branches, or receipts.

On any failed criterion, load [`claude-delegation-recovery.md`](claude-delegation-recovery.md). A completed implementation or successful command sequence is not evidence that a real delegation is useful and does not authorize any subsequent pilot.
