# Retirement record v1 format and compatibility

Status: implemented by issue #332 as a persistence and inspection foundation only.

This format records immutable retirement intent and commitments. It does not prepare, finalize, repair, truncate, migrate, or mutate threads, processes, worktrees, catalogs, providers, descendants, or any other lifecycle resource. A digest is correlation, not authority.

## Location and private filesystem boundary

Each record has exactly one stream:

```text
<config-dir>/retirement/v1/records/ret_<32-lowercase-hex>.jsonl
```

Record IDs contain 128 CSPRNG bits. `retirement`, `v1`, `records`, and `locks` are real directories with mode `0700`. Record and advisory-lock files are real, single-link regular files with mode `0600`. Reads and writes reject symlinks, hard links, wrong modes, non-regular files, unsafe path components, and identity changes. Creation uses exclusive/no-follow opens. The stream does not contain raw worktree paths, prompts, patches, secrets, provider payloads, transcripts, or receipts; callers commit bounded identities before append.

## Strict JSONL envelope

Every bounded line is one object with exactly these fields:

| Field | v1 rule |
| --- | --- |
| `schema_version` | integer `1` |
| `record_id` | exact filename ID |
| `sequence` | integer, starts at 1 and increments by one |
| `event_type` | one of the four types below |
| `operation_id` | caller-stable, bounded NFC idempotency key |
| `previous_event_digest` | empty at sequence 1; otherwise exact preceding digest |
| `payload` | strict event-specific object |
| `event_digest` | `sha256:<64 lowercase hex>` |
| `written_at` | UTC RFC3339 with exactly six fractional digits |

Unknown, missing, duplicate, null, floating-point, exponent, malformed UTF-8, non-NFC identity, unsupported schema/event, oversized, and semantically invalid values reject. The only event types are:

- `record_created`: the sole sequence-1 header. It binds canonicalization v1, immutable subject commitments, physical-owner commitment, exactly the six normative class-intent containers in normative order, and initial descendant state/commitment.
- `operation_declared`: binds the exact record ID and record-domain commitment, operation ID, immutable subject commitment, six-class intent, scope, attachment/evidence/authority commitments, optional superseded operation ID, and recomputed operation digest.
- `identity_commitment_recorded`: appends a typed identity commitment and optional superseded sequence.
- `record_note_appended`: appends a bounded typed note commitment and optional superseded sequence. Raw note text is not stored.

Corrections append a new event with an explicit superseding reference. Existing bytes are never rewritten or deleted. Exact replay of the same operation/event/payload is a no-op; a conflicting replay rejects.

Payload objects are closed and versioned by the envelope. Their exact v1 fields are:

| Event | Required payload fields | Optional payload fields |
| --- | --- | --- |
| `record_created` | `canonicalization_version`, `subject`, `initial_intent`, `initial_descendant_state`, `initial_descendant_commitment` | none |
| `operation_declared` | `record_id`, `record_commitment`, `operation_id`, `subject_commitment`, `operation_digest`, `scope`, `intent`, `attachment_commitments`, `evidence_commitments`, `authority_commitments` | `supersedes_operation_id` |
| `identity_commitment_recorded` | `identity_kind`, `identity_commitment` | `supersedes_sequence` |
| `record_note_appended` | `note_kind`, `note_commitment` | `supersedes_sequence` |

`subject` contains exactly `thread_commitment`, `worker_binding_commitment`, `created_by`, `workspace_commitment`, `initial_worktree_commitment`, and `physical_owner_commitment`. `created_by` is `spawn`, `adopt`, or `legacy_admission`; accepting the discriminator does not admit legacy state. Each intent is an array containing exactly the six classes in the normative order. Every class object contains `class` and ordered `items`; every item contains `resource_commitment`, `expected_disposition`, and `decision_owner_commitment`. Dispositions are closed per class to the values in the approved six-class model. Arrays have at most 256 commitments/items, lines at most 64 KiB, streams at most 1 MiB and 1,024 events, discriminators at most 96 bytes, and pre-commit identity input at most 4 KiB.

## Repository-owned canonical codec and digests

Canonical JSON is UTF-8 with keys sorted lexicographically by Unicode code point, no insignificant whitespace, semantic array order preserved, integers only, and absent optional fields omitted. Strings use NFC at identity-bearing boundaries. The implementation owns parsing, duplicate/type validation, key ordering, and byte emission rather than treating `encoding/json` output as the wire contract.

SHA-256 input begins with one exact domain plus canonical bytes:

```text
amux.retirement.record-id.v1\0
amux.retirement.event.v1\0
amux.retirement.operation.v1\0
amux.retirement.manifest.v1\0
amux.retirement.identity.v1\0
```

The event digest covers every envelope field except `event_digest`, including `previous_event_digest` and the safety timestamp. The operation digest covers exact intent, scope, immutable bindings, and attachment/evidence/authority commitments; it excludes render text and timestamps. Identity commitments use the identity domain. The manifest domain is reserved only: v1 issue #332 emits no manifest digest.

## Integrity, tail, durability, and locking

Every read and every pre-append load recomputes the complete sequence and digest chain before exposing state. A complete verified final line is accepted with or without a trailing newline. Unparseable final bytes without a newline are reported as a recoverable tail only when all preceding complete lines verify. Inspection then reports degraded/recovery-required and exits 2; append refuses it. No command in this slice truncates or repairs the file.

Append lock order is machine mutation lock, then the record lock; locks are held through verification, append, file `fsync`, identity recheck, and parent-directory `fsync`. Inspection takes the existing record lock shared and creates no files. Future nested ordering is machine → record → attachment/worktree and must never be reversed.

## Read-only inspection and compatibility

`amux retirement inspect --record <ret-id>` is the only public surface. `--json` uses the existing result envelope and includes exact record ID, verified count, last sequence/digest, integrity/tail status, immutable subject commitments, and latest operation commitments. It emits no raw paths or unbounded payload. Verified records exit 0. Missing, malformed, unsupported, corrupt, unsafe, busy, and recoverable-tail records exit 2; absence uses `retirement_record_not_found`.

No existing registry or durable file is migrated or admitted. Existing clients behave unchanged. Clients older than this format do not know the `retirement/v1` subtree and leave it inert; rollback retains it as evidence. They must not delete or modify that subtree. New readers reject unknown versions and event families rather than broadening authority. Future compatible readers may add optional result-envelope fields, but changing canonical bytes, payload fields, event meaning, or digest domains requires a new version.
