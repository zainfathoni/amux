package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetirementStreamAppendAndLoadRoundTrip(t *testing.T) {
	dir := Directory{Path: t.TempDir()}
	recordID := "ret_12345678"
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	header := sampleRetirementHeader(recordID, now)
	headerCopy := header
	streamEvent := RetirementStreamEvent{
		Kind:               RetirementEventHeader,
		RetirementRecordID: recordID,
		Header:             &headerCopy,
	}
	appended, wrote, err := AppendRetirementRecordEvent(dir, recordID, streamEvent, now)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatalf("first append not recorded: wrote=%v, event=%+v", wrote, appended)
	}
	if got, want := appended.Sequence, 1; got != want {
		t.Fatalf("append sequence=%d, want %d", got, want)
	}

	manifest := sampleRetirementPrepareManifest(recordID, now.Add(time.Minute), header.Subject.ThreadID, "op_1")
	manifestEvent := RetirementStreamEvent{
		SchemaVersion:      RetirementStreamSchemaVersion,
		Kind:               RetirementEventPrepareManifest,
		RetirementRecordID: recordID,
		PrepareManifest:    &manifest,
	}
	manifestEvent.PrepareManifest.PreparedAt = now.Add(2 * time.Minute)
	appendedManifest, wrote, err := AppendRetirementRecordEvent(dir, recordID, manifestEvent, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatalf("manifest append unexpectedly deduped first call")
	}
	if got, want := appendedManifest.Sequence, 2; got != want {
		t.Fatalf("manifest sequence=%d, want %d", got, want)
	}
	if !strings.HasPrefix(appendedManifest.PrepareManifest.ManifestDigest, "sha256:") {
		t.Fatalf("prepare manifest digest missing: %q", appendedManifest.PrepareManifest.ManifestDigest)
	}

	roundTrip, err := LoadRetirementRecordStream(dir, recordID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(roundTrip), 2; got != want {
		t.Fatalf("loaded stream count=%d, want %d", got, want)
	}

	replayed, wrote, err := AppendRetirementRecordEvent(dir, recordID, manifestEvent, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatalf("replay append unexpectedly wrote new line")
	}
	if got, want := replayed.Sequence, 2; got != want {
		t.Fatalf("replay sequence=%d, want %d", got, want)
	}

	afterReplay, err := LoadRetirementRecordStream(dir, recordID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(afterReplay), 2; got != want {
		t.Fatalf("stream count after replay=%d, want %d", got, want)
	}
}

func TestRetirementStreamRejectsNonHeaderFirst(t *testing.T) {
	dir := Directory{Path: t.TempDir()}
	recordID := "ret_12345678"
	manifestEvent := sampleRetirementPrepareManifest(recordID, time.Now().UTC(), "T-worker", "op_1")
	streamEvent := RetirementStreamEvent{
		SchemaVersion:      RetirementStreamSchemaVersion,
		Kind:               RetirementEventPrepareManifest,
		RetirementRecordID: recordID,
		PrepareManifest:    &manifestEvent,
	}
	_, _, err := AppendRetirementRecordEvent(dir, recordID, streamEvent, time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "must begin with a header") {
		t.Fatalf("AppendRetirementRecordEvent() error=%v", err)
	}
}

func TestRetirementStreamRejectsInvalidSequenceGap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gap-stream.jsonl")
	now := time.Now().UTC()
	header := sampleRetirementHeader("ret_22345678", now)
	headerEvent := RetirementStreamEvent{
		SchemaVersion:      RetirementStreamSchemaVersion,
		Kind:               RetirementEventHeader,
		Sequence:           2,
		RetirementRecordID: header.RetirementRecordID,
		Header:             &header,
		EventID:            "sha256:seed",
		OccurredAt:         now,
	}
	raw, err := json.Marshal(headerEvent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRetirementStream(path); err == nil || !strings.Contains(err.Error(), "expected sequence 1") {
		t.Fatalf("LoadRetirementStream() error=%v, want sequence error", err)
	}
}

func TestRetirementStreamRejectsCorruptPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.jsonl")
	if err := os.WriteFile(path, []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRetirementStream(path); err == nil {
		t.Fatalf("LoadRetirementStream accepted corrupt payload")
	}
}

func TestRetirementStreamLoadInjectsStableEventIDWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	now := time.Now().UTC()
	header := sampleRetirementHeader("ret_42345678", now)
	headerCopy := header
	headerEvent := RetirementStreamEvent{
		SchemaVersion:      RetirementStreamSchemaVersion,
		Kind:               RetirementEventHeader,
		Sequence:           1,
		RetirementRecordID: header.RetirementRecordID,
		Header:             &headerCopy,
		OccurredAt:         now,
	}
	raw, err := json.Marshal(headerEvent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	events, err := LoadRetirementStream(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("LoadRetirementStream() len=%d, want 1", len(events))
	}
	if events[0].EventID == "" || !strings.HasPrefix(events[0].EventID, "sha256:") {
		t.Fatalf("derived event id not injected: %q", events[0].EventID)
	}
}

func TestAppendRetirementRecordEventRequiresDirectoryPath(t *testing.T) {
	header := sampleRetirementHeader("ret_52345678", time.Now().UTC())
	headerCopy := header
	_, _, err := AppendRetirementStreamEvent("retirement.jsonl", RetirementStreamEvent{
		Kind:               RetirementEventHeader,
		RetirementRecordID: "ret_52345678",
		Header:             &headerCopy,
	}, time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "empty retirement stream directory") {
		t.Fatalf("AppendRetirementStreamEvent() error=%v", err)
	}
}

func TestRetirementPrepareManifestDigestIsStable(t *testing.T) {
	recordID := "ret_32345678"
	now := time.Now().UTC()
	first := sampleRetirementPrepareManifest(recordID, now, "T-worker", "op_digest")
	second := sampleRetirementPrepareManifest(recordID, now, "T-worker", "op_digest")
	// The second manifest intentionally reorders plan output; digest should still match.
	second.Resources = []RetirementPlanContainer{
		{
			Class: RetirementResourceClassAmpThread,
			Items: []RetirementPlanItem{{ResourceID: RetirementResourceID{Kind: "amp_thread", Identity: "T-worker"}, ExpectedDisposition: "archive", DecisionOwner: "test-owner"}},
		},
		{
			Class: RetirementResourceClassGitWorktree,
			Items: []RetirementPlanItem{{ResourceID: RetirementResourceID{Kind: "git_worktree", Identity: "gw-1"}, ExpectedDisposition: "remove_normal", DecisionOwner: "test-owner"}},
		},
	}
	firstDigest, err := RetirementPrepareManifestDigest(&first)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := RetirementPrepareManifestDigest(&second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("manifest digest mismatch: %s != %s", firstDigest, secondDigest)
	}
}

func sampleRetirementHeader(recordID string, now time.Time) RetirementRecordHeader {
	return RetirementRecordHeader{
		SchemaVersion:           RetirementStreamSchemaVersion,
		CanonicalizationVersion: RetirementCanonicalizationVersion,
		RetirementRecordID:      recordID,
		CreatedBy:               "spawn",
		PhysicalWorktreeOwner:   "test-owner",
		CreatedAt:               now,
		Subject: RetirementRecordSubject{
			ThreadID:                "T-worker",
			WorkerBinding:           "worker_bind",
			WorkspaceIdentity:       "repo://example/workspace",
			InitialWorktreeIdentity: "worktree-id",
		},
		InitialPlan: []RetirementPlanContainer{
			{
				Class: RetirementResourceClassAmpThread,
				Items: []RetirementPlanItem{
					{
						ResourceID:          RetirementResourceID{Kind: "amp_thread", Identity: "T-worker"},
						ExpectedDisposition: "archive",
						DecisionOwner:       "test-owner",
					},
				},
			},
			{
				Class: RetirementResourceClassGitWorktree,
				Items: []RetirementPlanItem{
					{
						ResourceID:          RetirementResourceID{Kind: "git_worktree", Identity: "gw-1"},
						ExpectedDisposition: "remove_normal",
						DecisionOwner:       "test-owner",
					},
				},
			},
		},
		InitialDescendantState: "",
	}
}

func sampleRetirementPrepareManifest(recordID string, now time.Time, threadID, operationID string) RetirementPrepareManifest {
	return RetirementPrepareManifest{
		CanonicalizationVersion: RetirementCanonicalizationVersion,
		RetirementRecordID:      recordID,
		OperationID:             operationID,
		Attempt:                 1,
		PreparedByExactThread:   threadID,
		IntendedFinalizerClass:  "worker",
		DepartureTarget: RetirementDepartureTarget{
			ThreadID:           threadID,
			WorktreeIdentity:   "worktree-id",
			AttachmentIdentity: "attach-1",
			ObservedGeneration: 1,
		},
		AttachmentSnapshot: RetirementAttachmentSnapshot{
			WorktreeIdentity:     "worktree-id",
			Generation:           1,
			ExactLiveAttachments: []string{"a"},
		},
		RequiredAuthorityScopes: []RetirementAuthorityScope{
			{Principal: "op", Resource: "thread", Action: "read", Operation: "retire"},
		},
		EvidenceCommitments: []string{"evidence-1"},
		Resources: []RetirementPlanContainer{
			{
				Class: RetirementResourceClassAmpThread,
				Items: []RetirementPlanItem{
					{
						ResourceID:          RetirementResourceID{Kind: "amp_thread", Identity: "T-worker"},
						ExpectedDisposition: "archive",
						DecisionOwner:       "test-owner",
					},
				},
			},
			{
				Class: RetirementResourceClassGitWorktree,
				Items: []RetirementPlanItem{
					{
						ResourceID:          RetirementResourceID{Kind: "git_worktree", Identity: "gw-1"},
						ExpectedDisposition: "remove_normal",
						DecisionOwner:       "test-owner",
					},
				},
			},
		},
		PreparedAt: now,
	}
}
