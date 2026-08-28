---
status: superseded
superseded-by: ../adr/0007-retire-amux-through-native-cutover-and-staged-drain.md
---

# Historical: stable, recognizable Amp runner IDs

This proposal once designed stable IDs for runners launched and managed by Amux. [ADR 0007](../adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) supersedes that productization: Amux is draining its runner lifecycle rather than adding runner identity configuration, migration, and presentation machinery.

## Lean current decision

Do not implement issues #212–#216 as an Amux feature graph. In particular, do not add:

- an Amux `--runner-id` selector;
- `runner-identity.json` or machine-alias commands;
- an Amux runner-ID generator or registry field;
- named/unnamed compatibility classifications, migration status, or JSON output fields; or
- launch, restart, reconcile, doctor, completion, or scheduled-maintenance behavior for named runners.

Canonical workdir remains the identity of an existing Amux runner while that runner drains. ADR 0007's #232 boundary requires bounded fail-closed evidence to stop an exact old process and prove process and native-catalog absence before removing its row. The existing implementation classifies that evidence but does not yet execute the drain. Conflict, ambiguity, or unreadable evidence retains the old row and blocks replacement.

Only after old-runner absence is proven may the owner start a replacement directly under an owner-selected operating-system service:

```sh
amp --no-tui --runner-id <stable-owner-selected-id>
```

That ID and service configuration are native Amp/OS state, not Amux configuration or output. The ID must satisfy Amp's current runner-ID contract. Amux does not generate it, persist it, attest it, migrate it, or launch the replacement.

## `/amux` native selection boundary

For fresh work, `/amux` may select that already-live native runner through authenticated Amp child creation. A requested `--runner-id <id>` is skill-level intent for the native `create_thread` `runner_id` argument, not an Amux CLI flag.

The coordinator must list live runners immediately before creation, require an exact ID match whose reported working directory is the intended canonical workdir, and make one native request with executor `runner` and that exact `runner_id`. Missing, mismatched, rejected, or indeterminate selection stops without fallback, retry, adoption, or Amux lifecycle state.

## Historical design, not implementation authority

The superseded design proposed IDs shaped as:

```text
<short-hostname>-<normalized-workspace>-<12-hex-path-hash>
```

It also proposed a machine-alias store, generated identity, compatibility classifier, lifecycle migration, output expansion, and Amp Web smoke test. Those choices depended on permanent Amux runner lifecycle ownership and are intentionally not carried forward. They may inform an independently owned native/OS configuration, but they do not authorize Amux code or schema changes.

## Current completion boundary

1. Close #212–#216 as superseded product expansion.
2. Keep runner removal fail-closed until Amp exposes authoritative process-bound native-catalog presence and positive-absence evidence; then complete all six #232 drain cases before runner admission closes.
3. After exact old absence, configure the replacement runner natively under the selected OS service with one stable explicit ID.
4. Use the exact live ID only through native Amp runner selection for new child threads.

No new runner-ID store, migration framework, lifecycle classifier, or Amux CLI surface is required.
