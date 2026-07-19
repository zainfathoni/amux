---
status: experimental-evidence
---

# Issue 175 Amp invocation-policy probes

## Scope and method

These public-safe probes cover Amp CLI `0.0.1784477831-g57f050` on macOS and the skill-owned automatic spawn seam. They used public Amp documentation, `amp tools list/show`, `amp permissions test`, and a temporary delegated helper. The helper probe ran no tool and changed no user settings. No model-backed Amp probe, thread/history access, private policy, account evidence, receipt, or runtime dossier was used.

Hard enforcement requires trusted normalized fields and interception before side effects for the exact tuple. Documentation or a synthetic permission test alone does not establish native approval binding, one-call scope, replay behavior, or coverage of a server/plugin action.

## Reproducible public-safe fixture

The permission protocol claims below come from [Amp's public permissions note](https://ampcode.com/notes/permissions), which documents checks before tool calls, JSON arguments on stdin, `AGENT_TOOL_NAME`, and helper exits `0`/`1`/`2` as allow/ask/reject. The [current public manual's permissions section](https://ampcode.com/manual#permissions) identifies `amp.permissions` as a legacy internal plugin. The manual's [built-in agent example](https://ampcode.com/manual#use-a-built-in-agent) documents plugin `createThread(...)`, which is why plugin-created threads remain a bypass rather than inheriting the delegated-helper result.

The following fixture mutates only a temporary settings file and captures only synthetic arguments. Run it from any repository with the named Amp version; it performs no tool call:

```sh
PROBE=$(mktemp -d)
trap 'find "$PROBE" -type f -delete; rmdir "$PROBE"' EXIT
cat >"$PROBE/helper.sh" <<'EOF'
#!/bin/sh
cat >"$PROBE_ARGS"
printf '%s\n' "$AGENT_TOOL_NAME" >"$PROBE_TOOL"
exit "$PROBE_EXIT"
EOF
chmod 700 "$PROBE/helper.sh"
printf '{}\n' >"$PROBE/settings.json"
amp --settings-file "$PROBE/settings.json" permissions add delegate --to "$PROBE/helper.sh" '*'
```

The generated temporary config has this shape; `$PROBE` is the temporary directory, not a retained path:

```json
{"amp.permissions":[{"tool":"*","action":"delegate","to":"$PROBE/helper.sh"}]}
```

For each `PROBE_EXIT` in `0 1 2`, run:

```sh
export PROBE_EXIT PROBE_ARGS="$PROBE/args-$PROBE_EXIT" PROBE_TOOL="$PROBE/tool-$PROBE_EXIT"
set +e
amp --settings-file "$PROBE/settings.json" permissions test shell_command \
  --command 'printf probe-safe' --workdir /tmp --json >"$PROBE/out-$PROBE_EXIT" 2>"$PROBE/err-$PROBE_EXIT"
CLI_EXIT=$?
set -e
STDOUT=$(python3 - "$PROBE/out-$PROBE_EXIT" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
print(json.dumps({key: value[key] for key in ("tool", "arguments", "action", "source")}, sort_keys=True, separators=(",", ":")))
PY
)
printf 'helper=%s cli=%s name=%s stdin=%s stdout=%s stderr=%s\n' \
  "$PROBE_EXIT" "$CLI_EXIT" "$(cat "$PROBE_TOOL")" "$(cat "$PROBE_ARGS")" \
  "$STDOUT" "$(cat "$PROBE/err-$PROBE_EXIT")"
```

Observed exact bounded transcript after projecting `out-*` to `tool`, `arguments`, `action`, and `source`:

```text
helper=0 cli=0 name=shell_command stdin={"command":"printf probe-safe","workdir":"/tmp"} stdout={"action":"allow","arguments":{"command":"printf probe-safe","workdir":"/tmp"},"source":"user","tool":"shell_command"} stderr=
helper=1 cli=1 name=shell_command stdin={"command":"printf probe-safe","workdir":"/tmp"} stdout={"action":"ask","arguments":{"command":"printf probe-safe","workdir":"/tmp"},"source":"user","tool":"shell_command"} stderr=
helper=2 cli=2 name=shell_command stdin={"command":"printf probe-safe","workdir":"/tmp"} stdout={"action":"reject","arguments":{"command":"printf probe-safe","workdir":"/tmp"},"source":"user","tool":"shell_command"} stderr=
```

Tool-absence capture for the same version used this bounded command for each name below (Amp emits terminal reset bytes on stderr, so they are discarded):

```sh
for NAME in create_thread read_thread oracle Task send_message_to_thread; do
  set +e
  OUTPUT=$(amp --no-color tools show "$NAME" --json 2>/dev/null)
  CLI_EXIT=$?
  set -e
  printf 'name=%s exit=%s stdout=%s\n' "$NAME" "$CLI_EXIT" "$OUTPUT"
done
```

```text
name=create_thread exit=1 stdout=No such tool: create_thread
name=read_thread exit=1 stdout=No such tool: read_thread
name=oracle exit=1 stdout=No such tool: oracle
name=Task exit=1 stdout=No such tool: Task
name=send_message_to_thread exit=1 stdout=No such tool: send_message_to_thread
```

These absence rows are reported observations for only the named CLI build and active tool inventory; they are not a public Amp compatibility promise and remain unverified on other clients, modes, settings, and future versions. The docs establish generic pre-call permission semantics, not coverage of these absent tools or plugin APIs.

## Results

| Tuple | Evidence | Outcome |
| --- | --- | --- |
| skill-owned automatic `/amux spawn` × mode × explicit resolver preflight | The skill selects the mode and can invoke a pure resolver before its shell call. Existing examples already pass `--mode medium`. | **deterministic** for `automatic:true`: allow `medium`; reject another mode without rewriting. Direct CLI use remains a documented bypass outside this skill tuple. |
| Amp delegated permissions × active `shell_command` × helper exit mapping | Temporary `amp.permissions` delegation received exact JSON arguments on stdin and `AGENT_TOOL_NAME=shell_command`; helper exits `0/1/2` produced allow/ask/reject. Public docs say rules run before tool calls. | Protocol supported, but not promoted for unrelated actions. This does not prove any native model-backed tuple. |
| native child creation × local/orb/Amp `runner(id)` × legacy permission adapter | `create_thread` was absent from `amp tools list/show`; no argument/default/interception probe was possible. Public plugin APIs can call `createThread` directly. | **observed**; require explicit executor in policy intent, but no hard enforcement. Plugin creation is a known bypass. |
| Read Thread × exact target × legacy permission adapter | `read_thread` was absent from `amp tools list/show`; automatic mention extraction and direct retrieval paths are separate. Native prompt binding and replay were not proven. | **observed**; preserve exact approval and the one-query discrepancy exception as resolver intent only. |
| Oracle/Search/Review/Librarian and generic Task × legacy permission adapter | Their model-backed tool names/schemas were absent from the active CLI tool inventory. Internal fan-out and capacity charge routes were not exposed. | **observed** for capacity outcomes; semantic escalation remains instruction-only. |
| native child message × legacy permission adapter | No stable source, target, action/message ID, parent route, or retry identity was exposed in the active inventory. | **instruction-only**. Do not parse prose or count amux/Claude lifecycle traffic. |
| capacity source × provider/provider-version/schema/pool/window/unit/amount/freshness/confidence/charge-route/reservation/reserve-status | A bounded helper observation reported source/confidence and windows, but it is not publicly reproducible evidence here. Provider version, units/amounts for some model-specific windows, resets, charge-route, reservation, and reserve impacts were unknown. | **reported/unverified** and non-promoting. Unknown/missing/drifted fields and complete-looking evidence for an unpromoted capacity tuple yield `would_ask`, never binding `ask` or automatic allow. |

## Permission and privacy boundary

The thin permission adapter receives and ignores raw arguments without parsing, logging, or emitting them. Because no Amp-native action tuple passed all probes, it emits only a bounded `permission_tool_unproven` capability diagnostic and exits allow. Its public result is `would_allow`, not binding `allow`. The resolver's exact public projection contains only `action`, `result`, `reason`, `capability`, and `sources`; no raw target or private capacity value appears.

No owner-local decision ledger is implemented: stable typed native action identities were not proven. The existing `/amux` discrepancy-recovery Read Thread exception is unchanged, #147/#177 Claude delegation remains independent, and blocked #176 is not performed or absorbed here.
