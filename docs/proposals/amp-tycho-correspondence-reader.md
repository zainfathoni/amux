---
status: experimental-proposal
---

# Amp-side Tycho correspondence reader

## Decision boundary

This proposal records how an Amp thread may **read** the correspondence of a
[Tycho](https://github.com/firewalker06/tycho) managed agent, and where that
reader must live. It does not change [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md)
worker or runner identity, amend [ADR 0002](../adr/0002-post-lifecycle-long-term-vision.md)
or [ADR 0005](../adr/0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md),
add a row to [provider executor readiness](../provider-executor-readiness.md),
or grant any route new execution authority.

Reading is the entire scope. This proposal does not propose creating,
running, stopping, answering, or scheduling Tycho agents from amux, and it
does not propose the provider-neutral Amp↔Tycho task/result package that
ADR 0005 explicitly defers to a later ADR.

## Problem

Tycho owns provider execution for Claude Code and Codex on a host. amux owns
the local tmux worker and runner lifecycle on the same host. When a Tycho
managed agent has been working, the resulting correspondence — user turns,
assistant turns, tool summaries, run outcomes — is currently only readable by
opening Tycho's TUI or its Remote UI by hand.

An Amp coordinator that already has a session on that host should be able to
read that correspondence as data. Today there is no documented path, so the
recurring temptation is to teach amux to parse Tycho state. That is the wrong
owner, and this proposal exists to say so and to record the correct path
instead.

## Evidence: Tycho's read surfaces

Surveyed against Tycho `ff8ba5e` (2026-08-01), released line v0.9.0
(2026-07-31). Line references are into the Tycho repository, not this one.

### `memory.jsonl` is canonical

`docs/AGENT_MEMORY.md` states that `raw.log` is the firehose and
`memory.jsonl` is the canonical transcript — the only source of truth for both
the chat viewport and prompt composition. `managed_agents.json` no longer
carries a `messages` field; agent state persists there, conversation content
does not.

```
~/.tycho/logs/agents/<project-key>-<created-at>-<nonce>.memory.jsonl
```

One JSON object per line. The event vocabulary written by
`lib/hq/domain/agent_memory.rb` is `system_prompt`, `user_message`,
`assistant_message`, `tool_summary`, `token_usage`, `run_summary`,
`inquiry_request`, `inquiry_response`, and `attachment`.

Two properties constrain any reader:

- **The path is not derivable from the agent key.** New stems embed the
  project key, a creation timestamp, and a nonce, specifically so a reused key
  cannot resolve to an older log lineage. A reader must resolve the path
  through `~/.tycho/logs/managed_agents.json` or `tycho agent status <key>`.
- **It is not live.** `ManagedAgent#capture_run_memory!` writes once per run,
  after the agent process exits. Correspondence from an in-flight run exists
  only in `raw.log`, which is why Tycho's own viewport renders
  `memory_chat_blocks + live_run_chat_blocks` and parses `raw.log` only while
  `pid` is set and `finished_at` is nil.

### The Remote API is the only cross-machine surface

`lib/hq/remote_server.rb` routes the relevant reads:

| Route | Returns |
| --- | --- |
| `GET /agents` | agent list |
| `GET /agents/:key` | agent status |
| `GET /agents/:key/conversation` | correspondence blocks |
| `GET /agents/:key/logs` | raw log, accepts query params |
| `GET /agents/:key/debug` | diagnostic payload |
| `GET /projects`, `GET /resources`, `GET /search` | host-level state |

`GET /agents/:key/conversation` is backed by `AgentChatLog#chat_blocks`, so it
returns the memory-plus-live concatenation rather than the settled transcript
alone. For a reader that must observe an in-flight run, this is strictly
better than reading `memory.jsonl` directly.

Access requires `tycho serve` and a `TYCHO_REMOTE_TOKEN` bearer token. An
unset token means the API accepts unauthenticated requests, so the token must
be set before binding to any non-loopback address.

v0.9.0 additionally shipped persistent multiserver agents and projects, with
`GET /servers`, `GET /servers/resources`, and peer proxy routes. One Tycho
host can already federate reads from peer Tycho hosts. Any amux-adjacent
design should assume that federation exists rather than reinventing it.

### The CLI is not a parsing surface

`tycho agent logs <key> --type conversation|system [--follow]` is convenient
for a human or for an agent reading prose. It is not a structured surface:
`--json` is defined only on the `project` commands in `lib/hq/cli_command.rb`,
and no `agent` subcommand accepts it. The `.conversation.log` and
`.system.log` files are on-demand snapshots produced by
`AgentChatLog#ensure_generated`, documented as being for external tools and
human inspection.

A reader that screen-scrapes `tycho agent list` or `tycho agent status` will
break on the next table change. This proposal treats the CLI as a resolver for
log paths and nothing more.

### Tycho's repo-local skill is not an integration contract

Tycho carries `.claude/skills/tycho/SKILL.md`, a CLI cheat sheet for agents
working on Tycho itself. At `ff8ba5e` it has drifted from main: its artifact
table still lists the pre-nonce `<key>.raw.log` filename scheme and never
mentions `memory.jsonl`; its workflow section still instructs the reader to
run `tycho app list`, a surface removed by the same commit that last touched
the skill; and its model example predates the current harness catalog. It also
does not mention `tycho serve`, the Remote API, or multiserver.

The conclusion is not that the skill is bad — it is repo-local and serves its
own audience. The conclusion is that amux must not treat it as a stable
integration contract, and must pin its own understanding to Tycho's source and
`docs/AGENT_MEMORY.md`.

## Proposed approach

1. **Prefer the Remote API when a token and reachable host exist.** It is the
   only cross-machine surface, it includes in-flight run content, and it is
   already the surface Tycho's own multiserver federation uses.
2. **Fall back to `memory.jsonl` when co-located on the Tycho host.** Resolve
   the path through `managed_agents.json`; do not construct it from the agent
   key. Tail `raw.log` only for an agent whose run has not finished.
3. **Use the CLI only to resolve paths and keys**, never as a structured data
   source, until Tycho grows `--json` on the `agent` commands.
4. **Keep the reader in Amp.** It belongs in an Amp skill or tool that runs in
   a thread on, or with network reach to, the Tycho host.

## Ownership boundary

The reader does not belong in amux, and the reason is already recorded:

- ADR 0002 states that amux does not launch or supervise Tycho-style headless
  task runs, and that amux never derives task meaning from tmux activity alone
  or takes ownership of agent execution as Tycho does.
- ADR 0005 places provider execution outside amux's maintained core and allows
  Tycho to own machine and provider routing for Claude Code and Pi/Codex
  Spark.
- ADR 0005 also lists a provider-neutral Amp↔Tycho task/result package as
  deferred, with its routing, schema, authority, transport, and promotion
  criteria unspecified.

A correspondence reader inside amux would be a first step into exactly that
undesigned package, taken without the ADR that is supposed to define it.

The one amux-side affordance this proposal does rely on already exists and
needs no change: a runner may be pinned to any existing directory, including a
tool configuration directory. Pinning a runner whose workdir is the Tycho
configuration or project root gives an Amp session a machine and workdir on
the Tycho host. amux provisions; Amp reads; Tycho executes.

```sh
amux runner pin --workspace tycho --workdir ~/.tycho
amux runner launch --workdir ~/.tycho
```

## Goals

- Document a stable, source-pinned way for an Amp thread to read Tycho
  managed-agent correspondence.
- Prefer the surface that survives Tycho refactors over the one that is
  easiest to shell out to.
- Keep the in-flight versus settled distinction explicit so a reader does not
  silently report a partial transcript as final.
- Record the ownership boundary once, so the amux-side reader question does
  not have to be relitigated per task.

## Non-goals

- Creating, running, stopping, answering, cloning, archiving, or scheduling
  Tycho agents from amux.
- Adding a Tycho resource type, registry row, selector, or lifecycle route to
  amux.
- Supervising `tycho serve` as an amux worker or runner. Process supervision
  for the Tycho server belongs to launchd or systemd.
- Defining the deferred Amp↔Tycho task/result package, its schema, or its
  authority model.
- Asserting anything about Tycho's provider entitlement, quota, or billing.
  Tycho's cost snapshots are list-price estimates and are documented as not
  being the operator's actual invoice or subscription charge.

## Open questions and promotion gates

- **No real run is recorded yet.** Every claim here comes from reading Tycho
  source and documentation. Promotion past `experimental-proposal` requires at
  least one bounded run that resolves an agent's log path, reads its settled
  transcript, and reads an in-flight run, with the observed shapes recorded.
- **Event-shape stability is unverified.** The `agent_memory.rb` event
  vocabulary is read from source at one commit. A reader should tolerate
  unknown event types rather than fail on them.
- **Token handling is unspecified here.** `TYCHO_REMOTE_TOKEN` is Tycho's
  secret; this proposal does not propose that amux store, forward, or observe
  it.
- **Multiserver reads are untested from Amp.** Whether an Amp reader should
  ever traverse peer proxy routes, or only query the host it can reach, is
  unresolved.
- **Whether this warrants an ADR.** If a reader is built and used routinely,
  the ownership boundary asserted here should be promoted into the deferred
  ADR rather than remaining a proposal.

## Sources

- Tycho repository at `ff8ba5e` (2026-08-01), release line v0.9.0
  (2026-07-31): `docs/AGENT_MEMORY.md`, `docs/HARNESS_INVENTORY.md`,
  `docs/REMOTE_SERVER.md`, `lib/hq/domain/agent_memory.rb`,
  `lib/hq/domain/agent_command_builder.rb`, `lib/hq/domain/managed_agent.rb`,
  `lib/hq/domain/harness_catalog.rb`, `lib/hq/remote_server.rb`,
  `lib/hq/cli_command.rb`, `.claude/skills/tycho/SKILL.md`, `CHANGELOG.md`.
- This repository: [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md),
  [ADR 0002](../adr/0002-post-lifecycle-long-term-vision.md),
  [ADR 0005](../adr/0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md),
  [provider executor readiness](../provider-executor-readiness.md).
