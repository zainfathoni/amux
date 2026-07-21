---
status: accepted-design
---

# Stable, recognizable Amp runner IDs

This proposal records the owner-approved design for passing stable IDs to Amp runners launched by amux. The grill is complete. Runtime implementation remains a separate explicitly authorized step.

## Problem

amux currently launches runners as `amp --no-tui` without Amp's optional `--runner-id`. Amp therefore displays the machine hostname for unnamed runners in the Amp Web thread-creation UI. When several runners share a machine, their adjacent directory paths distinguish them, but their titles do not.

Amp supports `amp --no-tui --runner-id <id>`. The Amp manual states that runner IDs must be valid hostnames and are case-insensitive, while preserving the supplied casing. The desired title should be stable across runner restarts, recognizable at a glance, concise enough for the Web UI, and unique when one amux workspace contains multiple runner workdirs.

## Accepted decisions

### Runner ID shape

Generate each ID as:

```text
<short-hostname>-<normalized-workspace>-<short-path-hash>
```

Use hyphens, not underscores, because the result must be a valid hostname.

The path remains visible beside the title in Amp Web. The hash therefore provides compact, stable disambiguation without repeating a directory slug in the title.

### Machine name

Resolve the short hostname in this order:

```text
configured machine alias -> derived short hostname
```

The configured alias is machine-level state in the selected amux configuration directory so ordinary launches, restarts, and scheduled maintenance resolve the same ID. On the owner's current machines, the intended configured aliases include:

```text
vikas-mac-mini     -> vika
zains-macbook-pro  -> zain
```

When no alias is configured, derive the default from the first hyphen-delimited segment of the machine hostname. For example, `vikas-mac-mini` derives `vikas`, while `mac` remains `mac`.

Configured aliases must already match `^[a-z0-9]+(?:-[a-z0-9]+)*$`. Reject an invalid explicit alias rather than silently changing machine identity. Derived defaults may use the accepted normalization because the user did not explicitly choose them.

Manage the alias through a focused runner-identity CLI rather than a generic settings framework:

```text
amux runner machine-alias show
amux runner machine-alias set --value <alias>
amux runner machine-alias clear
```

Persist it atomically in `runner-identity.json` under the selected amux configuration directory:

```json
{
  "schema_version": 1,
  "machine_alias": "vika"
}
```

The file is runner-specific machine state, not a runner registry. `clear` removes the configured override and restores derived-hostname behavior. Scheduled maintenance uses the same selected configuration directory and therefore resolves the same alias.

### Workspace normalization

Use the configured amux workspace as the human-readable runner-ID component, but normalize only its runner-ID representation:

1. lowercase it;
2. replace each run of non-alphanumeric characters with `-`;
3. trim leading and trailing hyphens; and
4. reject an empty result.

The actual amux workspace name and tmux session name remain unchanged.

### Path hash

Reuse the existing amux runner-window path-hash semantics exactly:

1. canonicalize the workdir with `config.CanonicalWorkdir`, which currently applies absolute-path resolution and lexical cleaning without resolving symlinks;
2. compute SHA-256 over that canonical path; and
3. render the first six bytes as twelve lowercase hexadecimal characters.

This gives the ID 48 bits of path-derived disambiguation. Moving or renaming the workdir produces a new runner ID because canonical workdir is already amux's canonical runner identity.

Do not conditionally add a discriminator based on the number of configured runners. That would silently rename an existing runner when another workdir entered or left its workspace.

### Length limit

The complete generated ID must fit in one 63-character hostname label:

```text
len(machine) + 1 + len(workspace) + 1 + 12 <= 63
```

Reject an overlong ID during preflight with remediation to configure a shorter machine alias or choose a shorter workspace. Do not silently truncate any component.

## Examples

Illustrative IDs, with hashes standing in for the canonical path digest:

```text
vika-nix-91f2c6a80b4e
vika-bta-4d9a3278c101
zain-bta-09c541e2a87f
mac-tycho-d44778a93f12
```

## Preserved amux identity model

The Amp runner ID is presentation and remote-routing identity supplied to Amp; it does not replace amux's existing canonical resource identities:

- an amux runner remains canonically identified by workdir;
- an amux workspace remains a lifecycle group and same-named tmux session; and
- the generated runner window remains a private runtime detail.

Runner registry rows therefore do not need a per-runner display-name field merely to disambiguate multiple workdirs in one workspace.

## Compatibility and migration

New runner processes always receive the generated `--runner-id`. Existing exactly verified unnamed retained-shell runners and legacy direct-`exec` runners remain managed compatibility states rather than conflicts:

| Operation | Existing verified unnamed or legacy runner |
| --- | --- |
| `launch` | Preserve it and report that runner-ID migration is pending. |
| `list` | Remain configuration-only; perform no runtime inspection. |
| `doctor` | Report the compatible live state, pending migration, and expected generated ID. |
| `park` / `remove` | Permit only after exact compatibility ownership verification. |
| `restart` | Replace it with the canonical window and generated runner ID. |
| `reconcile` | Do not replace a healthy runner merely to rename it. |
| Scheduled maintenance | If an executable change independently requires replacement, launch the replacement with the generated ID. |

Explicit restart is the manual migration path. Ordinary launch and reconcile never cause a surprise process replacement solely to adopt the naming convention.

## Amp capability policy

Do not silently fall back to an unnamed runner when the selected Amp executable lacks `--runner-id` support:

- `runner pin` remains configuration-only and does not inspect Amp;
- launch checks capability before creating a runner;
- restart checks capability before stopping the existing runner;
- bulk replacement preflights capability before stopping any runner;
- scheduled maintenance checks the updated executable before stopping existing runners; and
- doctor reports unsupported capability.

An unsupported executable fails before replacement with remediation to update Amp. Existing verified unnamed compatibility runners may continue running until a supported executable can replace them.

## Ownership verification

Keep one pure runner-ID generator beside `CanonicalWorkdir` and `RunnerWindow`, backed by the same internal canonical-path hash helper. New retained-shell runners use exactly:

```text
amp --no-tui --runner-id <expected-id>
```

Every lifecycle and maintenance path consumes one shared inspection classification:

- `named-exact`: Amp argv contains the exact expected generated ID;
- `unnamed-compatible`: exact retained-shell `amp --no-tui` argv;
- `legacy-compatible`: exact direct-`exec` legacy argv;
- `absent`;
- `conflict`; or
- `ambiguous`.

Never accept an arbitrary `--runner-id` merely because the pane, generated window, and workdir otherwise look plausible.

Changing or clearing the configured machine alias is a configuration-only mutation but must refuse while any configured named runner is live. The safe remediation is to park exactly verified named runners, change the alias, then launch them with their new IDs. Compatible unnamed runners do not block the alias change because their exact argv contains no stale alias.

## Output contract

Canonical machine-readable resource identity remains the discriminated runner workdir. The generated Amp ID is not a selector and does not replace `--workdir`.

Add optional runner details to versioned JSON results:

- `runner_id`: expected generated ID;
- `identity_state`: `named-exact`, `unnamed-compatible`, `legacy-compatible`, `absent`, `conflict`, or `ambiguous`; and
- `migration_pending`: whether a compatible runner still lacks the expected ID.

`runner list` remains runtime-free but computes and displays the expected ID. Pin, launch, and restart report it. Doctor reports expected ID, observed identity state, migration status, and Amp capability. `machine-alias show` supports human and versioned JSON output.

## Minimum implementation boundary

Implementation is incomplete without:

1. unit tests for alias validation, hostname derivation, workspace normalization, length rejection, and the shared 48-bit path hash;
2. strict parsing and atomic-write tests for `runner-identity.json`;
3. CLI tests for machine-alias show/set/clear and live-named-runner refusal;
4. exact named argv and all compatibility classifications;
5. lifecycle tests covering the accepted migration table;
6. capability tests proving restart and maintenance fail before stopping a runner;
7. JSON compatibility tests for optional runner details;
8. README, accepted design/ADR context, CLI help/completion, and bundled skill updates; and
9. a manual Amp Web smoke check, because amux can verify the argv contract but does not own the external Web UI.

## Read-only pair consultation

At the owner's request, Amp launched one fresh experimental read-only Claude thinker in a dedicated clean detached worktree at the recorded base. The launch used only Read/Grep/Glob and the bounded semantic-submission tools. Diagnostics reported the required platform, Claude CLI, tmux, and capacity sources as available; managed-policy effect, strict-MCP runtime behavior, and read-confinement runtime remain explicitly untested.

The thinker was asked to challenge the accepted decisions and recommend answers for configuration shape, source-of-truth placement, compatibility, capability preflight, output, tests, and documentation. It remained blocked behind interactive startup handling, submitted no semantic report or input request, and the owner explicitly quit the pane and directed the grill to continue without Claude. The private unresolved receipt remains recovery evidence, but the abandoned consultation contributes no design decisions; process existence and pane state were not treated as answers.

## Design non-goals

- Do not alter the runner registry schema merely to store generated IDs.
- Do not rename amux workspaces, tmux sessions, or runner windows.
- Do not make runner IDs canonical amux selectors.
- Do not replace healthy compatible runners during ordinary launch or reconcile.
