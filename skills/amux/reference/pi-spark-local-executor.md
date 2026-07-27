# Run one bounded local Pi/Spark code microtask

Use this provider-specific route only when the owner explicitly asks for a local Pi Spark code microtask. It is an unstable Darwin-only persistent-host executor, not an amux worker, runner, daemon, scheduler, or provider-neutral delegation API. The exact model is `openai-codex/gpt-5.3-codex-spark`; no alias, fallback, or automatic retry is allowed.

This route deliberately differs from [`pi-spark-orb-executor.md`](pi-spark-orb-executor.md): it reuses owner-established host authentication, never creates an isolated home, and never logs out, copies, links, rewrites, or deletes shared OAuth state. It is also independent of every Claude receipt, session, lifecycle, and recovery implementation.

Pi receives no tools or filesystem authority. The helper sends only the contents of explicitly allowed existing files, validates a strict JSON replacement envelope and exact JSON-event provenance, then atomically applies only returned allowed paths. Pi output and edits remain untrusted until the Amp coordinator reviews the diff and runs the packet's validation commands.

## 1. Prepare explicit admission evidence

Use [`../experimental/pi-spark-local/pi_spark_local.py`](../experimental/pi-spark-local/pi_spark_local.py). State and task packets are owner-private runtime artifacts outside the repository. Do not put credentials, account identity, balances, private thread/session IDs, or raw quota-provider output in them.

The owner must independently confirm that the existing Pi login is the `openai-codex` OAuth route. Pi 0.80.10 has no stock noninteractive credential-status command, so the helper does not infer OAuth type from file presence and never reads `auth.json`. The evidence path must be exactly `auth.json` in the selected `PI_CODING_AGENT_DIR`, or the default `~/.pi/agent` when that variable is absent. Record only a fresh owner confirmation plus filesystem metadata obtained without opening the credential file:

```bash
AUTH_PATH=$HOME/.pi/agent/auth.json
python3 - "$AUTH_PATH" <<'PY'
import datetime, json, os, pathlib, stat, sys

path = pathlib.Path(sys.argv[1]).expanduser()
if path.is_symlink() or not path.is_file() or stat.S_IMODE(path.stat().st_mode) != 0o600:
    raise SystemExit("blocked: expected a non-symlink 0600 auth file")
path = path.resolve(strict=True)
info = path.stat()
print(json.dumps({
    "provider": "openai-codex",
    "type": "oauth",
    "source": "owner-confirmed-metadata",
    "observed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "path": str(path),
    "identity": {
        "path": str(path), "device": str(info.st_dev), "inode": str(info.st_ino),
        "mode": stat.S_IMODE(info.st_mode), "size": info.st_size,
        "mtime_ns": str(info.st_mtime_ns),
    },
}, separators=(",", ":")))
PY
```

This emits metadata, not credential contents. Do not replace it with `cat`, JSON parsing, hashing, copying, or an auth-file fixture. Missing owner confirmation blocks.

Before execution, obtain a trusted observation no more than five minutes old for the ChatGPT Codex OAuth Spark pool. Record only:

```json
{
  "route": "chatgpt-codex-oauth-spark",
  "source_confidence": "trusted",
  "observed_at": "<RFC3339 UTC>",
  "reset_at": "<RFC3339 UTC>",
  "available": true
}
```

Missing, stale, unavailable, or ambiguous quota evidence blocks. Model catalog availability does not prove authentication, entitlement, quota, billing route, or actual inference provenance.

The shared Pi `settings.json` must already disable compaction and both retry layers:

```json
{
  "compaction": {"enabled": false},
  "retry": {
    "enabled": false,
    "maxRetries": 0,
    "provider": {"maxRetries": 0}
  }
}
```

The helper may read and hash this non-credential settings file to bind immutable intent. It never changes it. Do not alter shared settings merely to admit a run.

## 2. Create one exact task packet

Use one dedicated non-default-branch Git worktree and exactly one existing non-symlink file. New-file and multi-file replacement are intentionally unsupported by this first bounded seam; one file permits race-safe `openat`/`renameat` replacement without inventing a rollback transaction. The path is relative to the exact worktree root.

```json
{
  "schema": 1,
  "operation_id": "<unique-public-safe-id>",
  "owner_authorized": true,
  "goal": "<one self-contained code microtask>",
  "workdir": "<absolute canonical dedicated worktree root>",
  "allowed_paths": ["<existing-relative-file>"],
  "expected_output": "<specific expected edit>",
  "validation_commands": ["<coordinator-run command>"],
  "exclusions": [
    "amp_thread_read",
    "cross_worker_communication",
    "recursive_delegation",
    "network_publish",
    "pr",
    "push",
    "merge",
    "release",
    "install",
    "cleanup",
    "teardown"
  ],
  "timeout_seconds": 300,
  "stdout_limit": 65536,
  "stderr_limit": 16384,
  "event_limit": 1000,
  "auth_evidence": {"<copy the metadata object from step 1>": "<without credential data>"},
  "quota_evidence": {"<copy the trusted before observation>": "<without account data>"}
}
```

Do not include commands that grant Pi validation or repository authority. `validation_commands` are coordinator instructions and are never run by Pi or automatically by the helper.

## 3. Preflight, plan, and execute exactly once

```bash
HELPER=skills/amux/experimental/pi-spark-local/pi_spark_local.py
STATE=<owner-private-state-directory>
PACKET=<owner-private-task-packet>

python3 "$HELPER" --state-dir "$STATE" preflight --packet "$PACKET"
python3 "$HELPER" --state-dir "$STATE" plan --packet "$PACKET"
python3 "$HELPER" --state-dir "$STATE" execute --operation-id <exact-operation-id>
```

Preflight and execution reject API-key environment presence without printing values. They verify:

- the exact canonical worktree and dedicated branch;
- allowed path containment and pre-run content identity;
- fresh owner-confirmed OAuth metadata without reading credential contents;
- fresh trusted OAuth Spark capacity evidence;
- retry/compaction settings;
- exact Pi 0.80.10 CLI object/content, resolved Node interpreter object/content, model catalog entry, normalized argv, and microsecond Darwin kernel process incarnation;
- immutable mode-0400 execution intent separate from mutable operation state, with exact schemas and digest binding;
- one global local-Pi execution lock;
- one irreversibly recorded process attempt, whole-operation monotonic deadline, asynchronous bounded stdin, immediate output-overflow stop, and event count;
- one source-faithful session/agent lifecycle with `agent_end.willRetry=false`, no unknown/retry/tool/compaction events, and one terminal `agent_settled`;
- one completed assistant message from exactly `openai-codex` / `gpt-5.3-codex-spark` in the admitted canonical worktree;
- a strict replacement envelope containing only allowed files unchanged since plan time.

The helper invokes the absolute resolved Node interpreter and exact Pi CLI in JSON mode with `--no-session`, `--no-tools`, `--no-extensions`, `--no-skills`, `--no-prompt-templates`, `--no-themes`, `--no-context-files`, and `--no-approve`. It removes `NODE_OPTIONS` and `NODE_PATH`, uses a reviewed PATH rooted at the planned Node directory, and sets `PI_OFFLINE=1` plus `PI_SKIP_VERSION_CHECK=1`; offline mode suppresses unrelated startup traffic, not the selected inference request.

On timeout it signals only the `Popen` handle after byte-equal process-incarnation revalidation. It never uses process-name matching, guessed PIDs, broad stops, fallback, alias substitution, or automatic retry. Changed identity produces an indeterminate terminal record instead of a guessed stop.

Successful inference and replacement application initially returns `awaiting_quota_confirmation`, not success. Immediately obtain a trusted after observation in the same reset window:

```json
{
  "route": "chatgpt-codex-oauth-spark",
  "source_confidence": "trusted",
  "observed_at": "<RFC3339 UTC after execution>",
  "reset_at": "<same reset_at as the baseline>",
  "usage_increased": true
}
```

Then finalize:

```bash
python3 "$HELPER" --state-dir "$STATE" finalize \
  --operation-id <exact-operation-id> --quota-after <owner-private-after-evidence>
```

Only matching fresh, non-future, pre-reset after evidence records `status=success` and `billing_route=chatgpt-codex-oauth-spark`. The private operation state preserves the sanitized after object plus its file identity and digest. Never manufacture a debit from a response or model name.

## 4. Inspect, recover, and independently validate

Inspection is read-only and sanitized:

```bash
python3 "$HELPER" --state-dir "$STATE" inspect --operation-id <exact-operation-id>
```

After interruption, use `recover`. It records exact process absence as `indeterminate` because absence cannot manufacture semantic completion. It refuses a changed live identity and refuses to stop an exact still-live process automatically:

```bash
python3 "$HELPER" --state-dir "$STATE" recover --operation-id <exact-operation-id>
```

The coordinator must inspect `git status` and the exact diff, reject unrelated or low-quality changes, run every packet validation command, and decide whether to retain the worktree edits. Pi cannot commit, push, open a PR, merge, release, install, clean, or tear down anything.

Completed operation state is durable provenance, not disposable runtime residue. The helper's atomic replacement temporaries are removed on every handled path. Never delete shared authentication/configuration or unrelated host sessions. Remove owner-private smoke packet/quota artifacts only after independently listing and proving their exact operation ownership; preserve requested worktree edits for review.
