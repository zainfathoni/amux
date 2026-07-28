# Run one bounded mutating Claude Opus workflow in a fresh Orb

Use only after the owner explicitly requests one fresh-Orb repository mutation and separately authorizes any required unknown-capacity acknowledgement. This provider-owned experimental route implements [issue #254](../../../docs/proposals/issue-254-fresh-orb-mutating-opus-workflow.md); it is not a core `amux` resource, a generic delegation route, or authorization for a real pilot. Set `ORB_HELPER` to `experimental/fresh-orb-mutating/fresh_orb_workflow.py` in the installed `/amux-claude` skill and keep `STATE_DIR` in durable owner-private coordinator storage.

The helper never invokes Claude, provisions or archives an Orb, integrates a commit, or cleans a workspace. Native Amp performs provisioning, file transfer, notification, and any separately authorized archive. The helper supplies the durable default-deny gates around those actions. Every mutating command consumes one JSON object on stdin; generate each `event_id` once and replay only the byte-equivalent operation after interruption.

## Authority and singular bounds

Before provisioning, prepare exactly one immutable binding with one canonical origin thread, one distinct fresh Orb thread, one repository, immutable base SHA, dedicated branch and opaque worktree identity, task digest, exact executable path/version and normalized argv, authentication route, execution identity, one child, depth zero, one attempt, allowed paths, verification argv, and this exact authority:

```json
{
  "mutation":"one_bounded_commit_in_dedicated_worktree",
  "integration":"amp_coordinator_only",
  "forbidden":["push","pull_request","merge","release","issue_mutation","secret_management","infrastructure","archive","cleanup","recursive_delegation"]
}
```

The model field and the single `--model` argv value must both be the literal `claude-opus-4-8`. The argv must contain exact `json`, `dontAsk`, bounded-turn, safe-mode, strict-empty-MCP, disabled-slash-command, non-interactive, and no-session-persistence controls; continuation, resume, session, fallback, and permission-bypass controls are rejected in split or `--flag=value` form. Put the private task body on bounded stdin to Claude; bind only its SHA-256 in the receipt. Never put a prompt, transcript, token, credential, raw provider output, or session metadata in helper input. The generated packet is private launch metadata, not a prompt.

Persist coordinator intent **before mutation authority**:

```sh
python3 "$ORB_HELPER" --state-dir "$STATE_DIR" intent <intent.json
```

This atomically creates a privacy-safe coordinator receipt and exportable binding packet. Exact replay is `duplicate`; changed reuse blocks. The receipt and transferred handoff directory must remain durable before Orb disposal.

Record `cli_support`, `authentication`, `entitlement`, `availability`, `capacity`, and `charge_route` independently with `capability`. Every stage must durably pass before `authorize`. `unknown` or `unsupported` capacity/charge evidence requires one exact owner acknowledgement digest. A known floor failure is non-overridable. Then record one `launch` with `attempt:1`; a second event or attempt blocks. Do not retry an interrupted or indeterminate launch.

Use the read-only executor's fresh-Orb provisioning, fail-closed OAuth preflight, exact executable checks, safe-mode controls, output/process bounds, and result validator unchanged. For this route only, replace its read-only tool profile with the task's smallest explicit mutation profile, retain `dontAsk`, deny `Agent`, web, MCP, and lifecycle/forge authority, use the dedicated clean branch/worktree, and pass the task on bounded stdin. Tool confinement is a logical authority boundary; the commit-bearing handoff is still untrusted until coordinator verification. Do not run auth, login, quota, setup-token, or installer commands during a pilot unless a later exact pilot authorization explicitly includes the existing preflight recipe.

## Export and independently verify one handoff

After the validated Claude result has a single `modelUsage` key of exactly `claude-opus-4-8`, run `export` inside the Orb with the coordinator packet, packet digest, dedicated worktree, declared `complete` or `blocked` outcome, and a new empty output directory. It creates only privacy-safe `handoff.json` and, for `complete`, `result.bundle`.

- `complete` requires a clean branch at exactly one direct-child commit of the bound base.
- `blocked` requires clean HEAD equal to the base and zero changed artifacts.
- dirty, detached, wrong-branch, divergent, multi-commit, malformed, or indeterminate evidence blocks without reset, stash, clean, or repair.

Record `semantic` from the bounded child report, including the artifact digest, independently of `process-absence`. A headless process is `terminated_or_absent`, never parked. Transfer the complete handoff directory with native Amp file transfer, then record `transfer`; exact replay deduplicates and changed bytes block.

Run `verify` only in the origin, naming the transferred directory and trusted coordinator repository containing the immutable base. It independently verifies the exact regular-file set and bounds, binding and model provenance, bundle checksum/advertised ref/object inventory, single-parent base and one-commit range, and changed-path equality/allowlist. Verification records no integration authority.

Do **not** execute repository-controlled checks directly with coordinator credentials or inherited network/filesystem authority. Reconstruct the verified commit in a separate hardened verifier with an allowlisted environment, no credentials, no network, read-only host mounts, and only its temporary reconstruction writable. Run exactly the predeclared argv there, preserve a bounded result attestation, and record `checks` with the ordered argv digests, zero statuses, verifier-policy digest, and evidence digest. `deliver` rejects until this separate attestation is durable. A text patch, narrative, missing/extra/symlinked bundle entry, changed bundle, failed/unattested check, or out-of-scope path is not accepted.

## Delivery, acknowledgement, archive, and cleanup are separate

Only a verified handoff may become `deliver`. Native notification is later and is recorded separately with `notify`; notification failure does not undo delivery. `acknowledge` requires that exact durable delivery and is idempotent only for the same event and bytes.

`authorize-archive` and `authorize-cleanup` both reject before acknowledgement and require a fresh exact Orb/repository/branch/worktree identity plus a bounded clean-HEAD observation matching the verified handoff. Cleanup additionally requires durable headless process absence. They only record separate single-use authority; they perform no external action. After the exact native action, record `archive-result` or `cleanup-result` against that authorization with a result-evidence digest. Cleanup is never automatic. A cleanup failure needs a bounded privacy-safe failure digest and remains durable non-success; a later attempt requires a new authorization and appends its result without erasing the failure. Never discard unexpected workspace evidence. Orb archive, workspace cleanup, secret rotation, and integration remain unrelated decisions.

## Recovery and promotion boundary

Inspect with `show --operation-id <id>`. Resume from the last durable event with the same event ID and exact payload. A duplicate does not advance another stage; conflicting event reuse blocks. Preserve receipts, packet, report, bundle, branch/worktree, and unresolved evidence. Never use local tmux parking, the local Claude receipt helper, or process/session inference to fill an Orb event.

No implementation test authorizes a real provider call. A real pilot still requires the separate exact authorization and predecessor evidence named in issue #254. Generic fresh-Orb mutation remains disabled until the issue's complete promotion gate and a named owner `promote`, `repeat`, or `stop/narrow` decision.
