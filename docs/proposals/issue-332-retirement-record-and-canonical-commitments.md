---
status: implemented
issue: 332
parent: 331
---

# Retirement slice 1 implementation notes

## Durable record decisions

- Retirement streams use `Directory.Path/retirement-records/<retirement_record_id>.jsonl`.
- `retirement_record_id` must match `^ret_[a-zA-Z0-9._-]{8,128}$`.
- Stream schema version is `1` (`RetirementStreamSchemaVersion`).
- Canonicalization version is `1` (`RetirementCanonicalizationVersion`).
- Stream events are JSONL, one object per line:
  - `retirement-record-header-v1`
  - `retirement-prepare-manifest-v1`
  - `retirement-finalize-event-v1`
  - `retirement-operation-summary-v1`
- Event IDs are persisted as `sha256:<hex>`, derived from canonicalized payload when absent.
- Header and manifest are immutable once written.

## Canonical digest and append rules

- Canonical manifests are deterministic by sorting plan containers by class and items by `(kind, identity)`.
- Manifest digest is SHA-256 over the canonicalized manifest JSON (`manifest_digest`) and excludes only the existing `manifest_digest` field.
- Append is immutable and sequence-checked; replay with identical event ID/payload is idempotent and does not rewrite history.
- Conflicting replay reuses existing event ID but changes payload and is rejected.
- Non-header-first writes are rejected.
- Missing schema versions are defaulted.
- Missing `event_id` values in existing stream lines are tolerated; a deterministic value is derived during read.

## Integrity, compatibility, and crash behavior

- Loads validate JSON parsing, event schema, sequence continuity, class/resource validation, and duplicate event IDs.
- Interrupted/partial writes (including truncated lines) fail closed on load and do not hide prior valid events.
- Stream writes use `O_APPEND|O_CREATE`, then `fsync` on the file and parent directory for durability.
- No migration path is introduced in this slice; existing durable files remain unchanged.

## Locking decision

- Stream storage is durable and append-safe by implementation.
- Locking composition with the mutation/worktree generation locks is intentionally deferred to later slices.
