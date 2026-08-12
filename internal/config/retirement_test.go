package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/lock"
)

const testRecordID = "ret_00112233445566778899aabbccddeeff"

func TestCanonicalJSONGoldenAndStrictDecode(t *testing.T) {
	value, err := parseCanonicalJSON([]byte(`{"z":2,"a":[true,"é",-1]}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encodeCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"a":[true,"é",-1],"z":2}`; string(got) != want {
		t.Fatalf("canonical JSON = %s, want %s", got, want)
	}
	for name, input := range map[string][]byte{
		"duplicate":     []byte(`{"a":1,"a":2}`),
		"negative zero": []byte(`-0`),
		"float":         []byte(`1.0`),
		"exponent":      []byte(`1e2`),
		"null":          []byte(`null`),
		"invalid utf8":  {'"', 0xff, '"'},
		"trailing":      []byte(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCanonicalJSON(input); err == nil {
				t.Fatalf("parseCanonicalJSON(%q) succeeded", input)
			}
		})
	}
	encoded, err := encodeCanonical(cObject(map[string]canonicalValue{"quoted\nkey": cString("line\n\t\"\\")}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"quoted\nkey":"line\n\t\"\\"}`; string(encoded) != want {
		t.Fatalf("escaped canonical JSON = %s, want %s", encoded, want)
	}
	if _, err := encodeCanonical(cString(string([]byte{0xff}))); err == nil {
		t.Fatal("canonical encoder accepted malformed UTF-8")
	}
}

func TestIdentityCommitmentNormalizesNFCAndSeparatesDomains(t *testing.T) {
	composed, err := IdentityCommitment("subject", "Café")
	if err != nil {
		t.Fatal(err)
	}
	decomposed, err := IdentityCommitment("subject", "Cafe\u0301")
	if err != nil {
		t.Fatal(err)
	}
	if composed != decomposed {
		t.Fatalf("NFC-equivalent commitments differ: %s != %s", composed, decomposed)
	}
	identity, _ := domainDigest(retirementIdentityDomain, cString("same"))
	event, _ := domainDigest(retirementEventDomain, cString("same"))
	operation, _ := domainDigest(retirementOperationDomain, cString("same"))
	manifest, _ := domainDigest(retirementManifestDomain, cString("same"))
	record, _ := domainDigest(retirementRecordIDDomain, cString("same"))
	seen := map[string]bool{}
	for _, digest := range []string{identity, event, operation, manifest, record} {
		if seen[digest] {
			t.Fatal("cross-domain digest collision")
		}
		seen[digest] = true
	}
	if err := validateDigest("identity", identity, retirementIdentityDomain); err != nil {
		t.Fatal(err)
	}
	if err := validateDigest("identity", identity, retirementEventDomain); err == nil {
		t.Fatal("identity digest was accepted in the event domain")
	}
	if !strings.HasSuffix(identity, ";domain=amux.retirement.identity.v1") {
		t.Fatalf("identity digest lacks its serialized domain: %q", identity)
	}
}

func TestNewRetirementRecordIDUsesExactRandomFormat(t *testing.T) {
	seen := make(map[string]bool)
	for index := 0; index < 64; index++ {
		recordID, err := NewRetirementRecordID()
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateRetirementRecordID(recordID); err != nil {
			t.Fatalf("generated ID %q: %v", recordID, err)
		}
		if seen[recordID] {
			t.Fatalf("duplicate generated ID %q", recordID)
		}
		seen[recordID] = true
	}
}

func TestOperationDigestBindsEverySafetyFieldAndExcludesTime(t *testing.T) {
	base := testOperation(t, testIntent(t))
	baseDigest := base.OperationDigest
	mutations := map[string]func(*RetirementOperationDeclaredPayload){
		"record ID": func(value *RetirementOperationDeclaredPayload) {
			value.RecordID = "ret_ffeeddccbbaa99887766554433221100"
			value.RecordCommitment, _ = RetirementRecordCommitment(value.RecordID)
		},
		"record commitment": func(value *RetirementOperationDeclaredPayload) {
			value.RecordCommitment = testCommitment(t, "record", "changed")
		},
		"operation ID": func(value *RetirementOperationDeclaredPayload) { value.OperationID = "changed-operation" },
		"subject": func(value *RetirementOperationDeclaredPayload) {
			value.SubjectCommitment = testCommitment(t, "retirement_subject", "changed")
		},
		"scope": func(value *RetirementOperationDeclaredPayload) { value.Scope = "corrected_scope" },
		"intent class item": func(value *RetirementOperationDeclaredPayload) {
			value.Intent[0].Items[0].ExpectedDisposition = "archive"
		},
		"attachment": func(value *RetirementOperationDeclaredPayload) {
			value.AttachmentCommitments = append(value.AttachmentCommitments, testCommitment(t, "attachment", "second"))
		},
		"evidence": func(value *RetirementOperationDeclaredPayload) {
			value.EvidenceCommitments[0] = testCommitment(t, "evidence", "changed")
		},
		"authority": func(value *RetirementOperationDeclaredPayload) {
			value.AuthorityCommitments[0] = testCommitment(t, "authority", "changed")
		},
		"supersedes": func(value *RetirementOperationDeclaredPayload) { value.SupersedesOperationID = "prior-operation" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copy := cloneOperation(base)
			mutate(&copy)
			digest, err := RetirementOperationDigest(copy)
			if err != nil {
				return
			}
			if digest == baseDigest {
				t.Fatal("bound-field mutation did not change operation digest")
			}
		})
	}
}

func TestRetirementAppendInspectReplayAndConflict(t *testing.T) {
	dir, store := testRetirementStore(t)
	created := testCreationRequest(t)
	inspection, replay, err := store.Append(context.Background(), dir, created)
	if err != nil || replay || inspection.VerifiedEventCount != 1 || inspection.Subject.CreatedBy != "adopt" {
		t.Fatalf("create inspection=%+v replay=%t err=%v", inspection, replay, err)
	}
	assertPrivateMode(t, dir.RetirementRecordPath(testRecordID), 0o600)
	for _, path := range []string{filepath.Join(dir.Path, RetirementDirectory), dir.RetirementRootPath(), dir.RetirementRecordsPath(), dir.RetirementLocksPath()} {
		assertPrivateMode(t, path, 0o700)
	}

	inspection, replay, err = store.Append(context.Background(), dir, created)
	if err != nil || !replay || inspection.VerifiedEventCount != 1 {
		t.Fatalf("exact replay inspection=%+v replay=%t err=%v", inspection, replay, err)
	}
	conflict := created
	payload := conflict.Payload.(RetirementRecordCreatedPayload)
	payload.Subject.CreatedBy = "spawn"
	conflict.Payload = payload
	if _, _, err := store.Append(context.Background(), dir, conflict); retirementCode(err) != RetirementRecordInvalid {
		t.Fatalf("conflicting replay error=%v", err)
	}

	operation := testOperation(t, testIntent(t))
	request := RetirementEventRequest{RecordID: testRecordID, OperationID: "operation-two", EventType: RetirementOperationDeclared, Payload: operation, WrittenAt: testTime(2)}
	inspection, replay, err = store.Append(context.Background(), dir, request)
	if err != nil || replay || inspection.VerifiedEventCount != 2 || inspection.LatestOperation == nil || inspection.LatestOperation.OperationDigest != operation.OperationDigest {
		t.Fatalf("append inspection=%+v replay=%t err=%v", inspection, replay, err)
	}
	verified, err := store.Inspect(context.Background(), dir, testRecordID)
	if err != nil || verified.IntegrityStatus != "verified" || verified.LastSequence != 2 {
		t.Fatalf("inspect=%+v err=%v", verified, err)
	}
	data, err := os.ReadFile(dir.RetirementRecordPath(testRecordID))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/synthetic/private/worktree", "secret-prompt", "manifest_digest"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("record leaked or emitted forbidden value %q", forbidden)
		}
	}
}

func TestRetirementAllV1EventsAndSupersedingCorrections(t *testing.T) {
	dir, store := testRetirementStore(t)
	if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
		t.Fatal(err)
	}
	identity := RetirementEventRequest{RecordID: testRecordID, OperationID: "identity-operation", EventType: RetirementIdentityRecorded, Payload: RetirementIdentityCommitmentPayload{IdentityKind: "catalog_binding", IdentityCommitment: testCommitment(t, "catalog_binding", "synthetic")}, WrittenAt: testTime(2)}
	if _, _, err := store.Append(context.Background(), dir, identity); err != nil {
		t.Fatal(err)
	}
	note := RetirementEventRequest{RecordID: testRecordID, OperationID: "note-operation", EventType: RetirementNoteAppended, Payload: RetirementNotePayload{NoteKind: "correction", NoteCommitment: testCommitment(t, "note", "synthetic-correction"), SupersedesSequence: 2}, WrittenAt: testTime(3)}
	if _, _, err := store.Append(context.Background(), dir, note); err != nil {
		t.Fatal(err)
	}
	operation := testOperation(t, testIntent(t))
	operation.OperationID = "corrected-operation"
	operation.SupersedesOperationID = "missing-operation"
	operation.OperationDigest, _ = RetirementOperationDigest(operation)
	request := RetirementEventRequest{RecordID: testRecordID, OperationID: operation.OperationID, EventType: RetirementOperationDeclared, Payload: operation, WrittenAt: testTime(4)}
	if _, _, err := store.Append(context.Background(), dir, request); retirementCode(err) != RetirementRecordInvalid {
		t.Fatalf("missing superseded operation error=%v", err)
	}
	if got, err := store.Inspect(context.Background(), dir, testRecordID); err != nil || got.VerifiedEventCount != 3 {
		t.Fatalf("inspection=%+v err=%v", got, err)
	}
}

func TestRetirementRejectsOperationIDReuseAcrossEventTypes(t *testing.T) {
	dir, store := testRetirementStore(t)
	if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
		t.Fatal(err)
	}
	identity := RetirementEventRequest{RecordID: testRecordID, OperationID: "shared-operation", EventType: RetirementIdentityRecorded, Payload: RetirementIdentityCommitmentPayload{IdentityKind: "catalog_binding", IdentityCommitment: testCommitment(t, "catalog_binding", "one")}, WrittenAt: testTime(2)}
	if _, _, err := store.Append(context.Background(), dir, identity); err != nil {
		t.Fatal(err)
	}
	note := RetirementEventRequest{RecordID: testRecordID, OperationID: identity.OperationID, EventType: RetirementNoteAppended, Payload: RetirementNotePayload{NoteKind: "audit", NoteCommitment: testCommitment(t, "note", "one")}, WrittenAt: testTime(3)}
	if _, _, err := store.Append(context.Background(), dir, note); retirementCode(err) != RetirementRecordInvalid {
		t.Fatalf("cross-event operation ID reuse error=%v", err)
	}
}

func TestRetirementInspectRejectsDuplicateOperationIDInStoredChain(t *testing.T) {
	dir, store := testRetirementStore(t)
	if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
		t.Fatal(err)
	}
	note := RetirementEventRequest{RecordID: testRecordID, OperationID: "note-operation", EventType: RetirementNoteAppended, Payload: RetirementNotePayload{NoteKind: "audit", NoteCommitment: testCommitment(t, "note", "one")}, WrittenAt: testTime(2)}
	if _, _, err := store.Append(context.Background(), dir, note); err != nil {
		t.Fatal(err)
	}
	path := dir.RetirementRecordPath(testRecordID)
	data, _ := os.ReadFile(path)
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	event, err := decodeRetirementEvent(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	event.OperationID = testCreationRequest(t).OperationID
	event.EventDigest, _ = eventDigest(event)
	lines[1], _ = encodeRetirementEvent(event, false)
	if err := os.WriteFile(path, append(bytes.Join(lines, []byte("\n")), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordCorrupt {
		t.Fatalf("duplicate operation ID error=%v", err)
	}
}

func TestEventDigestBindsEveryEnvelopeField(t *testing.T) {
	base := RetirementEvent{SchemaVersion: RetirementSchemaVersion, RecordID: testRecordID, Sequence: 2, EventType: RetirementNoteAppended, OperationID: "note-operation", PreviousEventDigest: testCommitment(t, "event", "previous"), Payload: RetirementNotePayload{NoteKind: "audit", NoteCommitment: testCommitment(t, "note", "one")}, WrittenAt: testTime(2)}
	baseDigest, err := eventDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*RetirementEvent){
		"schema":   func(event *RetirementEvent) { event.SchemaVersion++ },
		"record":   func(event *RetirementEvent) { event.RecordID = "ret_ffeeddccbbaa99887766554433221100" },
		"sequence": func(event *RetirementEvent) { event.Sequence++ },
		"event type": func(event *RetirementEvent) {
			event.EventType = RetirementIdentityRecorded
			event.Payload = RetirementIdentityCommitmentPayload{IdentityKind: "audit", IdentityCommitment: testCommitment(t, "identity", "one")}
		},
		"operation":       func(event *RetirementEvent) { event.OperationID = "other-operation" },
		"previous digest": func(event *RetirementEvent) { event.PreviousEventDigest = testCommitment(t, "event", "other") },
		"payload": func(event *RetirementEvent) {
			event.Payload = RetirementNotePayload{NoteKind: "audit", NoteCommitment: testCommitment(t, "note", "two")}
		},
		"written at": func(event *RetirementEvent) { event.WrittenAt = testTime(3) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			digest, err := eventDigest(changed)
			if err != nil {
				t.Fatal(err)
			}
			if digest == baseDigest {
				t.Fatal("envelope mutation did not change event digest")
			}
		})
	}
}

func TestRetirementRejectsCorruptionUnknownFieldsAndChainChanges(t *testing.T) {
	mutations := map[string]func([]byte) []byte{
		"noncanonical whitespace": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`:`), []byte(`: `), 1)
		},
		"unknown field": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`{"event_digest"`), []byte(`{"extra":1,"event_digest"`), 1)
		},
		"duplicate field": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`{"event_digest"`), []byte(`{"schema_version":1,"event_digest"`), 1)
		},
		"unsupported version": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1)
		},
		"sequence gap": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"sequence":1`), []byte(`"sequence":2`), 1)
		},
		"digest change": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"created_by":"adopt"`), []byte(`"created_by":"spawn"`), 1)
		},
		"float type": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"sequence":1`), []byte(`"sequence":1.0`), 1)
		},
		"null": func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"operation_id":"create-operation"`), []byte(`"operation_id":null`), 1)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			dir, store := testRetirementStore(t)
			if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
				t.Fatal(err)
			}
			path := dir.RetirementRecordPath(testRecordID)
			data, _ := os.ReadFile(path)
			if err := os.WriteFile(path, mutate(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordCorrupt {
				t.Fatalf("inspection error=%v", err)
			}
		})
	}
}

func TestRetirementTailAndCompleteNoNewlinePolicy(t *testing.T) {
	t.Run("complete line", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		path := dir.RetirementRecordPath(testRecordID)
		data, _ := os.ReadFile(path)
		if err := os.WriteFile(path, bytes.TrimSuffix(data, []byte("\n")), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Inspect(context.Background(), dir, testRecordID); err != nil || got.RecoverableTail {
			t.Fatalf("inspect=%+v err=%v", got, err)
		}
		request := RetirementEventRequest{RecordID: testRecordID, OperationID: "note-operation", EventType: RetirementNoteAppended, Payload: RetirementNotePayload{NoteKind: "audit", NoteCommitment: testCommitment(t, "note", "bounded"), SupersedesSequence: 0}, WrittenAt: testTime(2)}
		if _, _, err := store.Append(context.Background(), dir, request); err != nil {
			t.Fatal(err)
		}
		data, _ = os.ReadFile(path)
		if bytes.Count(data, []byte("\n")) != 2 {
			t.Fatalf("expected separated JSONL lines, got %q", data)
		}
	})
	t.Run("partial tail", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		path := dir.RetirementRecordPath(testRecordID)
		file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		_, _ = file.WriteString(`{"schema_version":1`)
		_ = file.Close()
		inspection, err := store.Inspect(context.Background(), dir, testRecordID)
		if retirementCode(err) != RetirementRecordRecovery || !inspection.RecoverableTail || inspection.VerifiedEventCount != 1 || inspection.IntegrityStatus != "degraded" {
			t.Fatalf("inspection=%+v err=%v", inspection, err)
		}
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); retirementCode(err) != RetirementRecordRecovery {
			t.Fatalf("append error=%v", err)
		}
	})
	t.Run("complete invalid event without newline is corrupt", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		path := dir.RetirementRecordPath(testRecordID)
		data, _ := os.ReadFile(path)
		data = bytes.TrimSuffix(data, []byte("\n"))
		data = bytes.Replace(data, []byte(`"sequence":1`), []byte(`"sequence":2`), 1)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		inspection, err := store.Inspect(context.Background(), dir, testRecordID)
		if retirementCode(err) != RetirementRecordCorrupt || inspection.VerifiedEventCount != 0 || inspection.RecoverableTail {
			t.Fatalf("inspection=%+v err=%v", inspection, err)
		}
	})
}

func TestRetirementFilesystemSafety(t *testing.T) {
	t.Run("ancestor symlink does not mutate target", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		target := t.TempDir()
		if err := os.Symlink(target, filepath.Join(dir.Path, RetirementDirectory)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); retirementCode(err) != RetirementRecordInvalid {
			t.Fatalf("error=%v", err)
		}
		entries, err := os.ReadDir(target)
		if err != nil || len(entries) != 0 {
			t.Fatalf("symlink target was mutated: entries=%v err=%v", entries, err)
		}
	})
	t.Run("record permissions", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir.RetirementRecordPath(testRecordID), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordInvalid {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("directory permissions", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir.RetirementRootPath(), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordInvalid {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("special permission bits", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		path := dir.RetirementRecordPath(testRecordID)
		if err := os.Chmod(path, 0o4600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSetuid == 0 {
			t.Skip("filesystem did not preserve setuid bit")
		}
		if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordInvalid {
			t.Fatalf("error=%v mode=%v", err, info.Mode())
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		path := dir.RetirementRecordPath(testRecordID)
		target := filepath.Join(t.TempDir(), "target")
		data, _ := os.ReadFile(path)
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordInvalid {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(dir.RetirementRecordPath(testRecordID), filepath.Join(t.TempDir(), "second-link")); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordInvalid {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("non regular", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		path := dir.RetirementRecordPath(testRecordID)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordInvalid {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestRetirementInterruptedBoundariesAndLockContention(t *testing.T) {
	t.Run("partial write", func(t *testing.T) {
		dir, _ := testRetirementStore(t)
		store := RetirementStore{write: func(file *os.File, data []byte) (int, error) { return file.Write(data[:len(data)/2]) }}
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); retirementCode(err) != RetirementRecordRecovery {
			t.Fatalf("error=%v", err)
		}
		if inspection, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordRecovery || !inspection.RecoverableTail {
			t.Fatalf("partial creation inspection=%+v err=%v", inspection, err)
		}
	})
	for name, store := range map[string]RetirementStore{
		"file fsync":      {syncFile: func(*os.File) error { return errors.New("injected") }},
		"directory fsync": {syncDir: func(string) error { return errors.New("injected") }},
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := testRetirementStore(t)
			if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); retirementCode(err) != RetirementRecordRecovery {
				t.Fatalf("error=%v", err)
			}
		})
	}
	t.Run("record lock", func(t *testing.T) {
		dir, store := testRetirementStore(t)
		if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
			t.Fatal(err)
		}
		held, err := lock.AcquireMode(context.Background(), dir.RetirementLockPath(testRecordID), lock.Owner{}, lock.Exclusive, false)
		if err != nil {
			t.Fatal(err)
		}
		defer held.Release()
		store.LockWait = 20 * time.Millisecond
		if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordLockBusy {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestRetirementReplayRetriesDurability(t *testing.T) {
	dir, _ := testRetirementStore(t)
	failSync := true
	store := RetirementStore{syncFile: func(file *os.File) error {
		if failSync {
			return errors.New("injected")
		}
		return file.Sync()
	}}
	request := testCreationRequest(t)
	if _, _, err := store.Append(context.Background(), dir, request); retirementCode(err) != RetirementRecordRecovery {
		t.Fatalf("initial append error=%v", err)
	}
	if _, _, err := store.Append(context.Background(), dir, request); retirementCode(err) != RetirementRecordRecovery {
		t.Fatalf("replay while sync fails error=%v", err)
	}
	failSync = false
	if inspection, replay, err := store.Append(context.Background(), dir, request); err != nil || !replay || inspection.VerifiedEventCount != 1 {
		t.Fatalf("durable replay inspection=%+v replay=%t err=%v", inspection, replay, err)
	}
}

func TestRetirementConcurrentAppendIsSequenceSafe(t *testing.T) {
	dir, store := testRetirementStore(t)
	if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
		t.Fatal(err)
	}
	const writers = 12
	var wait sync.WaitGroup
	errors := make(chan error, writers)
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			commitment, _ := IdentityCommitment("note", "synthetic-note-"+string(rune('a'+index)))
			request := RetirementEventRequest{RecordID: testRecordID, OperationID: "note-operation-" + string(rune('a'+index)), EventType: RetirementNoteAppended, Payload: RetirementNotePayload{NoteKind: "audit", NoteCommitment: commitment}, WrittenAt: testTime(index + 2)}
			_, _, err := store.Append(context.Background(), dir, request)
			errors <- err
		}(index)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := store.Inspect(context.Background(), dir, testRecordID)
	if err != nil || inspection.VerifiedEventCount != writers+1 || inspection.LastSequence != writers+1 {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
}

func TestRetirementRejectsOversizedAndMissingWithoutMutation(t *testing.T) {
	dir, store := testRetirementStore(t)
	before, err := directorySnapshot(dir.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordNotFound {
		t.Fatalf("error=%v", err)
	}
	after, err := directorySnapshot(dir.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("read-only inspect mutated config: before=%v after=%v", before, after)
	}

	if _, _, err := store.Append(context.Background(), dir, testCreationRequest(t)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir.RetirementRecordPath(testRecordID), bytes.Repeat([]byte("x"), retirementMaxFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Inspect(context.Background(), dir, testRecordID); retirementCode(err) != RetirementRecordCorrupt {
		t.Fatalf("error=%v", err)
	}
}

func TestRetirementAppendEnforcesLimitsBeforeWriting(t *testing.T) {
	dir, store := testRetirementStore(t)
	filesystem, err := openRetirementFilesystem(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer filesystem.close()
	path := dir.RetirementRecordPath(testRecordID)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), retirementMaxFileBytes-1), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	name := testRecordID + ".jsonl"
	if err := store.appendBytes(filesystem.records, name, path, []byte(`{}`), true, false, info); retirementCode(err) != RetirementRecordInvalid {
		t.Fatalf("file limit error=%v", err)
	}
	after, err := os.Lstat(path)
	if err != nil || after.Size() != info.Size() {
		t.Fatalf("oversized append changed file: before=%d after=%v err=%v", info.Size(), after, err)
	}
	if err := store.appendBytes(filesystem.records, name, path, bytes.Repeat([]byte("x"), retirementMaxLineBytes+1), true, false, after); retirementCode(err) != RetirementRecordInvalid {
		t.Fatalf("line limit error=%v", err)
	}
}

func TestRetirementRejectsUnrepresentableTimestampBeforeWriting(t *testing.T) {
	dir, store := testRetirementStore(t)
	request := testCreationRequest(t)
	request.WrittenAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := store.Append(context.Background(), dir, request); retirementCode(err) != RetirementRecordInvalid {
		t.Fatalf("timestamp error=%v", err)
	}
	if _, err := os.Lstat(dir.RetirementRecordPath(testRecordID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid timestamp created a record: %v", err)
	}
}

func FuzzRetirementCanonicalDecoder(f *testing.F) {
	for _, seed := range [][]byte{[]byte(`{}`), []byte(`{"a":1}`), []byte(`{"a":1,"a":2}`), {'"', 0xff, '"'}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > retirementMaxLineBytes {
			t.Skip()
		}
		value, err := parseCanonicalJSON(data)
		if err != nil {
			return
		}
		first, err := encodeCanonical(value)
		if err != nil {
			t.Fatal(err)
		}
		reparsed, err := parseCanonicalJSON(first)
		if err != nil {
			t.Fatal(err)
		}
		second, err := encodeCanonical(reparsed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("canonical encoding is not stable: %q != %q", first, second)
		}
	})
}

func testRetirementStore(t *testing.T) (Directory, RetirementStore) {
	t.Helper()
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	return Directory{Path: t.TempDir()}, DefaultRetirementStore()
}

func testCreationRequest(t *testing.T) RetirementEventRequest {
	t.Helper()
	return RetirementEventRequest{RecordID: testRecordID, OperationID: "create-operation", EventType: RetirementRecordCreated, Payload: RetirementRecordCreatedPayload{
		CanonicalizationVersion: RetirementCanonicalVersion,
		Subject:                 testSubject(t),
		InitialIntent:           testIntent(t), InitialDescendantState: "explicit_none", InitialDescendantCommitment: testCommitment(t, "descendants", "none"),
	}, WrittenAt: testTime(1)}
}

func testIntent(t *testing.T) []RetirementClassIntent {
	t.Helper()
	dispositions := map[string]string{"amp_thread": "retain", "tmux_client_process": "retain", "git_worktree": "retain_preserved", "catalog_recovery_pointer": "retain", "provider_evidence": "retain", "descendant": "retain"}
	intent := make([]RetirementClassIntent, len(retirementClasses))
	for index, class := range retirementClasses {
		intent[index] = RetirementClassIntent{Class: class, Items: []RetirementIntentItem{{
			ResourceCommitment: testCommitment(t, class, "synthetic-resource"), ExpectedDisposition: dispositions[class], DecisionOwnerCommitment: testCommitment(t, "owner", class),
		}}}
	}
	return intent
}

func testOperation(t *testing.T, intent []RetirementClassIntent) RetirementOperationDeclaredPayload {
	t.Helper()
	subjectCommitment, err := RetirementSubjectCommitment(testSubject(t))
	if err != nil {
		t.Fatal(err)
	}
	recordCommitment, err := RetirementRecordCommitment(testRecordID)
	if err != nil {
		t.Fatal(err)
	}
	value := RetirementOperationDeclaredPayload{RecordID: testRecordID, RecordCommitment: recordCommitment, OperationID: "operation-two", SubjectCommitment: subjectCommitment, Scope: "retirement_intent", Intent: intent, AttachmentCommitments: []string{testCommitment(t, "attachment", "one")}, EvidenceCommitments: []string{testCommitment(t, "evidence", "one")}, AuthorityCommitments: []string{testCommitment(t, "authority", "one")}}
	digest, err := RetirementOperationDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	value.OperationDigest = digest
	return value
}

func testSubject(t *testing.T) RetirementSubject {
	t.Helper()
	return RetirementSubject{
		ThreadCommitment:          testCommitment(t, "thread", "synthetic-thread"),
		WorkerBindingCommitment:   testCommitment(t, "worker_binding", "synthetic-binding"),
		CreatedBy:                 "adopt",
		WorkspaceCommitment:       testCommitment(t, "workspace", "synthetic-workspace"),
		InitialWorktreeCommitment: testCommitment(t, "worktree", "/synthetic/private/worktree"),
		PhysicalOwnerCommitment:   testCommitment(t, "owner", "synthetic-owner"),
	}
}

func cloneOperation(value RetirementOperationDeclaredPayload) RetirementOperationDeclaredPayload {
	copy := value
	copy.AttachmentCommitments = append([]string(nil), value.AttachmentCommitments...)
	copy.EvidenceCommitments = append([]string(nil), value.EvidenceCommitments...)
	copy.AuthorityCommitments = append([]string(nil), value.AuthorityCommitments...)
	copy.Intent = make([]RetirementClassIntent, len(value.Intent))
	for index, class := range value.Intent {
		copy.Intent[index] = RetirementClassIntent{Class: class.Class, Items: append([]RetirementIntentItem(nil), class.Items...)}
	}
	return copy
}

func testCommitment(t *testing.T, kind, value string) string {
	t.Helper()
	commitment, err := IdentityCommitment(kind, value)
	if err != nil {
		t.Fatal(err)
	}
	return commitment
}

func testTime(offset int) time.Time {
	return time.Date(2026, 8, 12, 10, 0, offset, 123456000, time.UTC)
}

func retirementCode(err error) string {
	var retirementErr *RetirementError
	if errors.As(err, &retirementErr) {
		return retirementErr.Code
	}
	return ""
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode=%o, want %o", filepath.Base(path), info.Mode().Perm(), want)
	}
}

func directorySnapshot(root string) ([]string, error) {
	var entries []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, relative)
		return nil
	})
	return entries, err
}
