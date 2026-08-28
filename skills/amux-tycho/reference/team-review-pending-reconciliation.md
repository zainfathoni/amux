# Amp-owned PENDING review reconciliation

Load this branch only after Amp has consumed and independently verified Tycho candidates and intends to create or edit a current-user PENDING GitHub review. Tycho never calls GitHub review or comment mutation APIs.

GitHub provides no atomic compare-and-swap or `If-Match` for review updates. Use snapshot revalidation plus fail-closed conflict. Residual TOCTOU remains between the final read and write; 404, 422, or indeterminate mutation outcomes are conflicts.

## Ownership

Amp may mutate only a PENDING review it **owns for this assignment**:

- Amp created it during this assignment after its independent first pass and recorded the review ID; or
- the owner explicitly designated an existing current-user PENDING review ID before reconciliation.

An unowned pre-existing current-user PENDING review is a conflict even when it is the only one.

**Complete when:** one owned review ID is recorded, or the baseline is recorded as `none—will create`; any other current-user PENDING review stops mutation.

## Canonical snapshot

Read all current-user PENDING reviews, GET the owned review, and list all its comments with pagination complete. An ambiguous, partial, or failed read is a deny.

Build SHA-256 over deterministic compact UTF-8 JSON with sorted object keys and exactly this shape; absent optional values are JSON `null`:

```json
{
  "review_id": "4864634225",
  "commit_id": "<full sha>",
  "body": "<raw review body>",
  "state": "PENDING",
  "comments": [
    {
      "id": "1",
      "path": "pkg/file.ts",
      "line": 10,
      "original_line": null,
      "side": "RIGHT",
      "start_side": null,
      "commit_id": "<full sha>",
      "body": "<raw comment body>",
      "updated_at": "2026-08-04T12:00:00Z"
    }
  ]
}
```

Sort comments by numeric GitHub ID ascending before encoding; equal IDs reject. API page order must not affect the digest. Keep integer IDs as zero-pad-free decimal strings. Do not decode HTML entities. Do **not** use review `submitted_at` as a freshness signal; PENDING reviews are not submitted. Comment `updated_at` is part of the digest.

**Complete when:** the full baseline snapshot, digest, owned review ID (or none), and pinned reviewed SHA are recorded.

## Every write

Immediately before every review or comment write, including first create:

1. Re-read the PR's current full head SHA and require exact equality with the pinned reviewed SHA.
2. Re-read current-user PENDING reviews. For an existing owned review, recompute and require the baseline digest; for create, require the baseline still has no conflicting PENDING review.
3. Write only Amp-verified findings and leave the review unpublished unless separately authorized.
4. Re-read and record the new snapshot as the next baseline before another write.

Stop rather than overwrite on head read failure or mismatch, missing/non-PENDING owned review, snapshot drift, any different current-user PENDING review, ambiguous ownership, failed/partial reads, or 404/422/indeterminate mutation. Leave GitHub untouched after conflict and preserve the receipt.

**Complete when:** each write has its immediately preceding equal-head and equal-snapshot proof, its succeeding baseline snapshot, and no unowned review was changed. Publication, worker finish, and bridge acknowledgement remain separate transitions; wake-ups never imply them.
