package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/zainfathoni/amux/internal/lock"
	"golang.org/x/sys/unix"
)

const (
	RetirementSchemaVersion     = 1
	RetirementCanonicalVersion  = 1
	RetirementRecordCreated     = "record_created"
	RetirementOperationDeclared = "operation_declared"
	RetirementIdentityRecorded  = "identity_commitment_recorded"
	RetirementNoteAppended      = "record_note_appended"
	RetirementRecordNotFound    = "retirement_record_not_found"
	RetirementRecordInvalid     = "retirement_record_invalid"
	RetirementRecordCorrupt     = "retirement_record_corrupt"
	RetirementRecordRecovery    = "retirement_record_recovery_required"
	RetirementRecordLockBusy    = "retirement_record_lock_busy"
	retirementMaxLineBytes      = 64 << 10
	retirementMaxFileBytes      = 1 << 20
	retirementMaxEvents         = 1024
	retirementMaxIdentityBytes  = 4096
	retirementMaxDiscriminator  = 96
	retirementMaxCommitments    = 256
	retirementDefaultLockWait   = 2 * time.Second
)

var (
	retirementRecordIDPattern      = regexp.MustCompile(`^ret_[0-9a-f]{32}$`)
	retirementOperationPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	retirementDiscriminatorPattern = regexp.MustCompile(`^[a-z][a-z0-9_:-]{0,95}$`)
)

var retirementClasses = []string{
	"amp_thread",
	"tmux_client_process",
	"git_worktree",
	"catalog_recovery_pointer",
	"provider_evidence",
	"descendant",
}

var retirementDispositions = map[string]map[string]bool{
	"amp_thread":               {"archive": true, "retain": true, "already_absent_or_archived": true},
	"tmux_client_process":      {"stop_exact": true, "retain": true, "already_absent": true},
	"git_worktree":             {"remove_normal": true, "retain_preserved": true, "retain_blocked": true, "already_absent": true},
	"catalog_recovery_pointer": {"remove": true, "retain": true, "already_absent": true},
	"provider_evidence":        {"retain": true, "quarantined": true, "owner_abandoned": true, "settled": true},
	"descendant":               {"retain": true, "reference_child_result": true, "blocked": true},
}

type RetirementError struct {
	Code string
	Err  error
}

func (e *RetirementError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}
func (e *RetirementError) Unwrap() error { return e.Err }

type RetirementIntentItem struct {
	ResourceCommitment      string
	ExpectedDisposition     string
	DecisionOwnerCommitment string
}

type RetirementClassIntent struct {
	Class string
	Items []RetirementIntentItem
}

type RetirementSubject struct {
	ThreadCommitment          string
	WorkerBindingCommitment   string
	CreatedBy                 string
	WorkspaceCommitment       string
	InitialWorktreeCommitment string
	PhysicalOwnerCommitment   string
}

type RetirementRecordCreatedPayload struct {
	CanonicalizationVersion     int
	Subject                     RetirementSubject
	InitialIntent               []RetirementClassIntent
	InitialDescendantState      string
	InitialDescendantCommitment string
}

type RetirementOperationDeclaredPayload struct {
	RecordID              string
	RecordCommitment      string
	OperationID           string
	SubjectCommitment     string
	OperationDigest       string
	Scope                 string
	Intent                []RetirementClassIntent
	AttachmentCommitments []string
	EvidenceCommitments   []string
	AuthorityCommitments  []string
	SupersedesOperationID string
}

type RetirementIdentityCommitmentPayload struct {
	IdentityKind       string
	IdentityCommitment string
	SupersedesSequence int64
}

type RetirementNotePayload struct {
	NoteKind           string
	NoteCommitment     string
	SupersedesSequence int64
}

type RetirementEventRequest struct {
	RecordID    string
	OperationID string
	EventType   string
	Payload     any
	WrittenAt   time.Time
}

type RetirementEvent struct {
	SchemaVersion       int
	RecordID            string
	Sequence            int64
	EventType           string
	OperationID         string
	PreviousEventDigest string
	Payload             any
	EventDigest         string
	WrittenAt           time.Time
}

type RetirementInspection struct {
	SchemaVersion      int
	RecordID           string
	VerifiedEventCount int
	LastSequence       int64
	LastEventDigest    string
	IntegrityStatus    string
	RecoverableTail    bool
	RecoveryRequired   bool
	Subject            RetirementSubject
	LatestOperation    *RetirementOperationDeclaredPayload
}

type RetirementStore struct {
	LockWait time.Duration
	write    func(*os.File, []byte) (int, error)
	syncFile func(*os.File) error
	syncDir  func(string) error
}

func NewRetirementRecordID() (string, error) {
	random := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", fmt.Errorf("allocate retirement record ID: %w", err)
	}
	return "ret_" + hex.EncodeToString(random), nil
}

func DefaultRetirementStore() RetirementStore { return RetirementStore{} }

func (s RetirementStore) Append(ctx context.Context, dir Directory, request RetirementEventRequest) (RetirementInspection, bool, error) {
	if err := validateEventRequest(request); err != nil {
		return RetirementInspection{}, false, retirementError(RetirementRecordInvalid, err)
	}
	machinePath, err := lock.MachinePath()
	if err != nil {
		return RetirementInspection{}, false, retirementError(RetirementRecordInvalid, errors.New("machine lock unavailable"))
	}
	lockContext, cancel := context.WithTimeout(ctx, s.lockWait())
	defer cancel()
	machine, err := lock.Acquire(lockContext, machinePath, lock.Owner{Command: "amux retirement append"})
	if err != nil {
		return RetirementInspection{}, false, retirementError(RetirementRecordLockBusy, errors.New("machine lock is busy"))
	}
	defer machine.Release()

	if request.EventType != RetirementRecordCreated {
		if _, err := os.Lstat(dir.RetirementRecordPath(request.RecordID)); errors.Is(err, os.ErrNotExist) {
			return RetirementInspection{}, false, retirementError(RetirementRecordNotFound, errors.New("record does not exist"))
		} else if err != nil {
			return RetirementInspection{}, false, retirementError(RetirementRecordInvalid, errors.New("record cannot be inspected safely"))
		}
	}
	filesystem, err := openRetirementFilesystem(dir, true)
	if err != nil {
		return RetirementInspection{}, false, err
	}
	defer filesystem.close()
	if !filesystem.matchesPaths(dir) {
		return RetirementInspection{}, false, retirementError(RetirementRecordInvalid, errors.New("retirement directory identity changed"))
	}
	lockName := request.RecordID + ".lock"
	if err := ensureLockFileAt(filesystem.locks, lockName, dir.RetirementLockPath(request.RecordID)); err != nil {
		return RetirementInspection{}, false, err
	}
	lockFile, err := openFileAt(filesystem.locks, lockName, os.O_RDWR, 0)
	if err != nil || validateOpenFile(lockFile, 0o600) != nil {
		if lockFile != nil {
			_ = lockFile.Close()
		}
		return RetirementInspection{}, false, retirementError(RetirementRecordInvalid, errors.New("record lock cannot be opened safely"))
	}
	recordLock, err := lock.AcquireOpenFile(lockContext, lockFile, dir.RetirementLockPath(request.RecordID), lock.Owner{Command: "amux retirement append"}, lock.Exclusive, true)
	if err != nil {
		return RetirementInspection{}, false, retirementError(RetirementRecordLockBusy, errors.New("record lock is busy"))
	}
	defer recordLock.Release()
	lockInfo, err := recordLock.FileInfo()
	if err != nil || !sameFileAt(filesystem.locks, lockName, lockInfo) {
		return RetirementInspection{}, false, retirementError(RetirementRecordRecovery, errors.New("record lock identity changed"))
	}

	path := dir.RetirementRecordPath(request.RecordID)
	recordName := request.RecordID + ".jsonl"
	loaded, exists, err := loadRetirementFile(filesystem.records, recordName, request.RecordID)
	if err != nil {
		var retirementErr *RetirementError
		if errors.As(err, &retirementErr) && retirementErr.Code == RetirementRecordRecovery {
			return loaded.inspection(), false, err
		}
		return RetirementInspection{}, false, err
	}
	if loaded.recoverableTail {
		return loaded.inspection(), false, retirementError(RetirementRecordRecovery, errors.New("record has an unterminated tail"))
	}
	if !exists && request.EventType != RetirementRecordCreated {
		return RetirementInspection{}, false, retirementError(RetirementRecordNotFound, errors.New("record does not exist"))
	}
	if exists && request.EventType == RetirementRecordCreated {
		if replayEvent(loaded.events, request) {
			if err := s.syncReplay(filesystem.records, recordName, path, loaded.fileInfo); err != nil {
				return loaded.inspection(), false, err
			}
			if !filesystem.matchesPaths(dir) {
				return loaded.inspection(), false, retirementError(RetirementRecordRecovery, errors.New("retirement directory identity changed during replay"))
			}
			if !sameFileAt(filesystem.locks, lockName, lockInfo) {
				return loaded.inspection(), false, retirementError(RetirementRecordRecovery, errors.New("record lock identity changed during replay"))
			}
			return loaded.inspection(), true, nil
		}
		return loaded.inspection(), false, retirementError(RetirementRecordInvalid, errors.New("record already exists with different creation intent"))
	}
	if replayEvent(loaded.events, request) {
		if err := s.syncReplay(filesystem.records, recordName, path, loaded.fileInfo); err != nil {
			return loaded.inspection(), false, err
		}
		if !filesystem.matchesPaths(dir) {
			return loaded.inspection(), false, retirementError(RetirementRecordRecovery, errors.New("retirement directory identity changed during replay"))
		}
		if !sameFileAt(filesystem.locks, lockName, lockInfo) {
			return loaded.inspection(), false, retirementError(RetirementRecordRecovery, errors.New("record lock identity changed during replay"))
		}
		return loaded.inspection(), true, nil
	}
	if operationConflict(loaded.events, request) {
		return loaded.inspection(), false, retirementError(RetirementRecordInvalid, errors.New("operation ID conflicts with an existing event"))
	}
	if len(loaded.events) >= retirementMaxEvents {
		return loaded.inspection(), false, retirementError(RetirementRecordInvalid, errors.New("record has reached the event limit"))
	}

	event := RetirementEvent{
		SchemaVersion: RetirementSchemaVersion,
		RecordID:      request.RecordID,
		Sequence:      int64(len(loaded.events) + 1),
		EventType:     request.EventType,
		OperationID:   request.OperationID,
		Payload:       request.Payload,
		WrittenAt:     request.WrittenAt.UTC().Truncate(time.Microsecond),
	}
	if len(loaded.events) != 0 {
		event.PreviousEventDigest = loaded.events[len(loaded.events)-1].EventDigest
	}
	if err := verifyRetirementSemantics(event, loaded.events); err != nil {
		return loaded.inspection(), false, retirementError(RetirementRecordInvalid, err)
	}
	event.EventDigest, err = eventDigest(event)
	if err != nil {
		return loaded.inspection(), false, retirementError(RetirementRecordInvalid, err)
	}
	encoded, err := encodeRetirementEvent(event, false)
	if err != nil {
		return loaded.inspection(), false, retirementError(RetirementRecordInvalid, err)
	}
	if err := s.appendBytes(filesystem.records, recordName, path, encoded, exists, loaded.completeWithoutNewline, loaded.fileInfo); err != nil {
		return loaded.inspection(), false, err
	}
	if !filesystem.matchesPaths(dir) {
		return loaded.inspection(), false, retirementError(RetirementRecordRecovery, errors.New("retirement directory identity changed during append"))
	}
	if !sameFileAt(filesystem.locks, lockName, lockInfo) {
		return loaded.inspection(), false, retirementError(RetirementRecordRecovery, errors.New("record lock identity changed during append"))
	}
	loaded.events = append(loaded.events, event)
	return loaded.inspection(), false, nil
}

func (s RetirementStore) Inspect(ctx context.Context, dir Directory, recordID string) (RetirementInspection, error) {
	if err := ValidateRetirementRecordID(recordID); err != nil {
		return RetirementInspection{}, retirementError(RetirementRecordInvalid, err)
	}
	filesystem, err := openRetirementFilesystem(dir, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RetirementInspection{}, retirementError(RetirementRecordNotFound, errors.New("record does not exist"))
		}
		return RetirementInspection{}, err
	}
	defer filesystem.close()
	if !filesystem.matchesPaths(dir) {
		return RetirementInspection{}, retirementError(RetirementRecordInvalid, errors.New("retirement directory identity changed"))
	}
	recordName := recordID + ".jsonl"
	if _, err := statAt(filesystem.records, recordName); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RetirementInspection{}, retirementError(RetirementRecordNotFound, errors.New("record does not exist"))
		}
		return RetirementInspection{}, retirementError(RetirementRecordInvalid, errors.New("record cannot be inspected safely"))
	}
	lockName := recordID + ".lock"
	if err := validatePrivateRegularAt(filesystem.locks, lockName, 0o600); err != nil {
		return RetirementInspection{}, err
	}
	lockContext, cancel := context.WithTimeout(ctx, s.lockWait())
	defer cancel()
	lockFile, err := openFileAt(filesystem.locks, lockName, os.O_RDONLY, 0)
	if err != nil || validateOpenFile(lockFile, 0o600) != nil {
		if lockFile != nil {
			_ = lockFile.Close()
		}
		return RetirementInspection{}, retirementError(RetirementRecordInvalid, errors.New("record lock cannot be opened safely"))
	}
	held, err := lock.AcquireOpenFile(lockContext, lockFile, dir.RetirementLockPath(recordID), lock.Owner{}, lock.Shared, false)
	if err != nil {
		return RetirementInspection{}, retirementError(RetirementRecordLockBusy, errors.New("record lock is busy"))
	}
	defer held.Release()
	lockInfo, err := held.FileInfo()
	if err != nil || !sameFileAt(filesystem.locks, lockName, lockInfo) {
		return RetirementInspection{}, retirementError(RetirementRecordInvalid, errors.New("record lock identity changed"))
	}
	loaded, exists, err := loadRetirementFile(filesystem.records, recordName, recordID)
	if !exists && err == nil {
		return RetirementInspection{}, retirementError(RetirementRecordNotFound, errors.New("record disappeared during inspection"))
	}
	inspection := loaded.inspection()
	if err != nil {
		var retirementErr *RetirementError
		if errors.As(err, &retirementErr) && retirementErr.Code == RetirementRecordRecovery {
			return inspection, err
		}
		return RetirementInspection{}, err
	}
	if loaded.recoverableTail {
		return inspection, retirementError(RetirementRecordRecovery, errors.New("record has an unterminated tail"))
	}
	if !sameFileAt(filesystem.locks, lockName, lockInfo) {
		return RetirementInspection{}, retirementError(RetirementRecordInvalid, errors.New("record lock identity changed during inspection"))
	}
	if !filesystem.matchesPaths(dir) {
		return RetirementInspection{}, retirementError(RetirementRecordInvalid, errors.New("retirement directory identity changed during inspection"))
	}
	return inspection, nil
}

func ValidateRetirementRecordID(recordID string) error {
	if !retirementRecordIDPattern.MatchString(recordID) {
		return errors.New("record ID must be ret_ followed by 32 lowercase hex characters")
	}
	return nil
}

func (s RetirementStore) lockWait() time.Duration {
	if s.LockWait > 0 {
		return s.LockWait
	}
	return retirementDefaultLockWait
}

func retirementError(code string, err error) error { return &RetirementError{Code: code, Err: err} }

type loadedRetirement struct {
	events                 []RetirementEvent
	recoverableTail        bool
	completeWithoutNewline bool
	fileInfo               os.FileInfo
}

func (l loadedRetirement) inspection() RetirementInspection {
	inspection := RetirementInspection{
		SchemaVersion:      RetirementSchemaVersion,
		VerifiedEventCount: len(l.events),
		IntegrityStatus:    "verified",
		RecoverableTail:    l.recoverableTail,
		RecoveryRequired:   l.recoverableTail,
	}
	if l.recoverableTail {
		inspection.IntegrityStatus = "degraded"
	}
	for _, event := range l.events {
		inspection.RecordID = event.RecordID
		inspection.LastSequence = event.Sequence
		inspection.LastEventDigest = event.EventDigest
		switch payload := event.Payload.(type) {
		case RetirementRecordCreatedPayload:
			inspection.Subject = payload.Subject
		case RetirementOperationDeclaredPayload:
			copy := payload
			inspection.LatestOperation = &copy
		}
	}
	return inspection
}

func loadRetirementFile(records *os.File, name, recordID string) (loadedRetirement, bool, error) {
	var loaded loadedRetirement
	file, err := openFileAt(records, name, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return loaded, false, nil
	}
	if err != nil {
		return loaded, false, retirementError(RetirementRecordInvalid, errors.New("record cannot be opened safely"))
	}
	defer file.Close()
	if err := validateOpenFile(file, 0o600); err != nil {
		return loaded, true, err
	}
	infoBefore, _ := file.Stat()
	if infoBefore.Size() > retirementMaxFileBytes {
		return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record exceeds size limit"))
	}
	data, err := io.ReadAll(io.LimitReader(file, retirementMaxFileBytes+1))
	if err != nil {
		return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record cannot be read"))
	}
	infoAfter, statErr := file.Stat()
	if statErr != nil || !os.SameFile(infoBefore, infoAfter) || int64(len(data)) != infoAfter.Size() {
		return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record changed during inspection"))
	}
	pathInfo, pathErr := statAt(records, name)
	if pathErr != nil || !os.SameFile(infoAfter, pathInfo) {
		return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record path identity changed during inspection"))
	}
	loaded.fileInfo = infoAfter
	if err := validateOpenFile(file, 0o600); err != nil {
		return loaded, true, err
	}
	if len(data) == 0 {
		return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record is empty"))
	}
	endsNewline := data[len(data)-1] == '\n'
	lines := strings.Split(string(data), "\n")
	if endsNewline {
		lines = lines[:len(lines)-1]
	}
	completeLineCount := len(lines)
	if !endsNewline {
		completeLineCount--
	}
	if completeLineCount > retirementMaxEvents {
		return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record has too many events"))
	}
	for index, line := range lines {
		if len(line) == 0 || len(line) > retirementMaxLineBytes {
			if index == len(lines)-1 && !endsNewline {
				loaded.recoverableTail = true
				break
			}
			return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record contains an invalid line"))
		}
		event, parseErr := decodeRetirementEvent([]byte(line))
		if parseErr != nil {
			if index == len(lines)-1 && !endsNewline {
				if _, syntaxErr := parseCanonicalJSON([]byte(line)); syntaxErr != nil {
					loaded.recoverableTail = true
					break
				}
			}
			return loaded, true, retirementError(RetirementRecordCorrupt, fmt.Errorf("event %d is malformed", index+1))
		}
		if index >= retirementMaxEvents {
			return loaded, true, retirementError(RetirementRecordCorrupt, errors.New("record has too many events"))
		}
		canonical, canonicalErr := encodeRetirementEvent(event, false)
		if canonicalErr != nil || string(canonical) != line {
			return loaded, true, retirementError(RetirementRecordCorrupt, fmt.Errorf("event %d is not canonically encoded", index+1))
		}
		if err := verifyRetirementEvent(event, loaded.events, recordID); err != nil {
			return loaded, true, retirementError(RetirementRecordCorrupt, fmt.Errorf("event %d failed integrity validation: %w", index+1, err))
		}
		loaded.events = append(loaded.events, event)
	}
	loaded.completeWithoutNewline = !endsNewline && !loaded.recoverableTail
	return loaded, true, nil
}

func verifyRetirementEvent(event RetirementEvent, prior []RetirementEvent, recordID string) error {
	if event.RecordID != recordID {
		return errors.New("filename and event record IDs differ")
	}
	wantSequence := int64(len(prior) + 1)
	if event.Sequence != wantSequence {
		return fmt.Errorf("sequence is %d, expected %d", event.Sequence, wantSequence)
	}
	wantPrevious := ""
	if len(prior) != 0 {
		wantPrevious = prior[len(prior)-1].EventDigest
	}
	if event.PreviousEventDigest != wantPrevious {
		return errors.New("previous event digest does not match")
	}
	if event.Sequence == 1 && event.EventType != RetirementRecordCreated {
		return errors.New("first event is not record_created")
	}
	if event.Sequence != 1 && event.EventType == RetirementRecordCreated {
		return errors.New("record_created appears more than once")
	}
	for _, previous := range prior {
		if previous.OperationID == event.OperationID {
			return errors.New("operation ID appears more than once")
		}
	}
	wantDigest, err := eventDigest(event)
	if err != nil {
		return err
	}
	if event.EventDigest != wantDigest {
		return errors.New("event digest does not match canonical event")
	}
	return verifyRetirementSemantics(event, prior)
}

func verifyRetirementSemantics(event RetirementEvent, prior []RetirementEvent) error {
	switch payload := event.Payload.(type) {
	case RetirementOperationDeclaredPayload:
		if payload.RecordID != event.RecordID || payload.OperationID != event.OperationID {
			return errors.New("operation payload identity differs from its envelope")
		}
		if len(prior) == 0 {
			return errors.New("operation declaration has no record header")
		}
		header, ok := prior[0].Payload.(RetirementRecordCreatedPayload)
		if !ok {
			return errors.New("record header payload is invalid")
		}
		subjectCommitment, err := RetirementSubjectCommitment(header.Subject)
		if err != nil || payload.SubjectCommitment != subjectCommitment {
			return errors.New("operation subject commitment differs from the immutable header")
		}
		if payload.SupersedesOperationID != "" {
			found := false
			for _, previous := range prior {
				if previous.EventType == RetirementOperationDeclared && previous.OperationID == payload.SupersedesOperationID {
					found = true
					break
				}
			}
			if !found {
				return errors.New("superseded operation does not exist in this record")
			}
		}
	case RetirementIdentityCommitmentPayload:
		if payload.SupersedesSequence >= event.Sequence {
			return errors.New("superseded sequence must identify an earlier event")
		}
	case RetirementNotePayload:
		if payload.SupersedesSequence >= event.Sequence {
			return errors.New("superseded sequence must identify an earlier event")
		}
	}
	return nil
}

func validateEventRequest(request RetirementEventRequest) error {
	if err := ValidateRetirementRecordID(request.RecordID); err != nil {
		return err
	}
	if !retirementOperationPattern.MatchString(request.OperationID) {
		return errors.New("operation ID is invalid or unbounded")
	}
	if err := validateNFC("operation ID", request.OperationID); err != nil {
		return err
	}
	formattedTime := request.WrittenAt.Format("2006-01-02T15:04:05.000000Z")
	parsedTime, parseErr := time.Parse("2006-01-02T15:04:05.000000Z", formattedTime)
	if request.WrittenAt.IsZero() || request.WrittenAt.Location() != time.UTC || request.WrittenAt.Nanosecond()%1000 != 0 || parseErr != nil || !parsedTime.Equal(request.WrittenAt) {
		return errors.New("written_at must be UTC with microsecond precision")
	}
	_, err := payloadCanonical(request.EventType, request.Payload)
	if err == nil && request.EventType == RetirementOperationDeclared {
		payload := request.Payload.(RetirementOperationDeclaredPayload)
		if payload.RecordID != request.RecordID || payload.OperationID != request.OperationID {
			return errors.New("operation payload identity differs from its request")
		}
	}
	return err
}

func replayEvent(events []RetirementEvent, request RetirementEventRequest) bool {
	want, err := payloadCanonical(request.EventType, request.Payload)
	if err != nil {
		return false
	}
	wantBytes, _ := encodeCanonical(want)
	for _, event := range events {
		if event.OperationID != request.OperationID || event.EventType != request.EventType {
			continue
		}
		got, _ := payloadCanonical(event.EventType, event.Payload)
		gotBytes, _ := encodeCanonical(got)
		return string(gotBytes) == string(wantBytes)
	}
	return false
}

func operationConflict(events []RetirementEvent, request RetirementEventRequest) bool {
	for _, event := range events {
		if event.OperationID == request.OperationID {
			return true
		}
	}
	return false
}

type retirementFilesystem struct {
	config, retirement, version, records, locks *os.File
}

func (filesystem retirementFilesystem) close() {
	for _, directory := range []*os.File{filesystem.locks, filesystem.records, filesystem.version, filesystem.retirement, filesystem.config} {
		if directory != nil {
			_ = directory.Close()
		}
	}
}

func (filesystem retirementFilesystem) matchesPaths(dir Directory) bool {
	checks := []struct {
		file *os.File
		path string
	}{
		{filesystem.config, dir.Path},
		{filesystem.retirement, filepath.Join(dir.Path, RetirementDirectory)},
		{filesystem.version, dir.RetirementRootPath()},
		{filesystem.records, dir.RetirementRecordsPath()},
		{filesystem.locks, dir.RetirementLocksPath()},
	}
	for _, check := range checks {
		opened, err := check.file.Stat()
		pathInfo, pathErr := os.Lstat(check.path)
		if err != nil || pathErr != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, pathInfo) {
			return false
		}
	}
	return true
}

func openRetirementFilesystem(dir Directory, create bool) (retirementFilesystem, error) {
	var filesystem retirementFilesystem
	config, err := openDirectoryNoFollow(dir.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !create {
			return filesystem, err
		}
		return filesystem, retirementError(RetirementRecordInvalid, errors.New("config directory cannot be opened safely"))
	}
	filesystem.config = config
	steps := []struct {
		parent **os.File
		name   string
		target **os.File
	}{
		{&filesystem.config, RetirementDirectory, &filesystem.retirement},
		{&filesystem.retirement, RetirementVersionDirectory, &filesystem.version},
		{&filesystem.version, RetirementRecordsDirectory, &filesystem.records},
		{&filesystem.version, RetirementLocksDirectory, &filesystem.locks},
	}
	for _, step := range steps {
		opened, openErr := openDirectoryAt(*step.parent, step.name)
		if errors.Is(openErr, os.ErrNotExist) && create {
			if mkdirErr := unix.Mkdirat(int((*step.parent).Fd()), step.name, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, syscall.EEXIST) {
				filesystem.close()
				return retirementFilesystem{}, retirementError(RetirementRecordInvalid, errors.New("retirement directory cannot be created safely"))
			}
			opened, openErr = openDirectoryAt(*step.parent, step.name)
		}
		if openErr != nil {
			filesystem.close()
			if errors.Is(openErr, os.ErrNotExist) {
				return retirementFilesystem{}, openErr
			}
			return retirementFilesystem{}, retirementError(RetirementRecordInvalid, errors.New("retirement directories must be private real directories"))
		}
		*step.target = opened
	}
	if create {
		for _, directory := range []*os.File{filesystem.config, filesystem.retirement, filesystem.version} {
			if err := directory.Sync(); err != nil {
				filesystem.close()
				return retirementFilesystem{}, retirementError(RetirementRecordRecovery, errors.New("retirement directory durability is uncertain"))
			}
		}
	}
	return filesystem, nil
}

func openDirectoryNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openDirectoryAt(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(fd), name)
	info, statErr := file.Stat()
	if statErr != nil || !info.IsDir() || privateMode(info.Mode()) != 0o700 {
		_ = file.Close()
		return nil, errors.New("directory is not a private real directory")
	}
	return file, nil
}

func openFileAt(parent *os.File, name string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

func statAt(parent *os.File, name string) (os.FileInfo, error) {
	file, err := openFileAt(parent, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

func sameFileAt(parent *os.File, name string, expected os.FileInfo) bool {
	info, err := statAt(parent, name)
	return err == nil && expected != nil && os.SameFile(info, expected)
}

func ensureLockFileAt(directory *os.File, name, path string) error {
	file, err := openFileAt(directory, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		if err := validatePrivateRegularAt(directory, name, 0o600); err != nil {
			return err
		}
		if err := directory.Sync(); err != nil {
			return retirementError(RetirementRecordRecovery, errors.New("record lock durability is uncertain"))
		}
		return nil
	}
	if err != nil {
		return retirementError(RetirementRecordInvalid, errors.New("record lock cannot be created safely"))
	}
	if closeErr := file.Close(); closeErr != nil {
		return retirementError(RetirementRecordInvalid, errors.New("record lock cannot be closed"))
	}
	if err := validatePrivateRegularAt(directory, name, 0o600); err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record lock durability is uncertain"))
	}
	return nil
}

func validatePrivateRegularAt(directory *os.File, name string, mode os.FileMode) error {
	file, err := openFileAt(directory, name, os.O_RDONLY, 0)
	if err != nil {
		return retirementError(RetirementRecordInvalid, errors.New("retirement file must be a private regular file"))
	}
	defer file.Close()
	return validateOpenFile(file, mode)
}

func validateOpenFile(file *os.File, mode os.FileMode) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || privateMode(info.Mode()) != mode {
		return retirementError(RetirementRecordInvalid, errors.New("retirement file must be a private regular file"))
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return retirementError(RetirementRecordInvalid, errors.New("retirement file must have exactly one link"))
	}
	return nil
}

func privateMode(mode os.FileMode) os.FileMode {
	return mode & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
}

func (s RetirementStore) appendBytes(records *os.File, name, path string, encoded []byte, exists, needsSeparator bool, expected os.FileInfo) error {
	if len(encoded) == 0 || len(encoded) > retirementMaxLineBytes {
		return retirementError(RetirementRecordInvalid, errors.New("event exceeds line size limit"))
	}
	flags := os.O_WRONLY | os.O_APPEND
	if !exists {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := openFileAt(records, name, flags, 0o600)
	if err != nil {
		return retirementError(RetirementRecordInvalid, errors.New("record cannot be opened for append"))
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return retirementError(RetirementRecordInvalid, errors.New("record identity cannot be verified"))
	}
	if err := validateOpenFile(file, 0o600); err != nil {
		return err
	}
	if exists && (expected == nil || !os.SameFile(before, expected)) {
		return retirementError(RetirementRecordRecovery, errors.New("record identity changed before append"))
	}
	bytes := append([]byte(nil), encoded...)
	if needsSeparator {
		bytes = append([]byte{'\n'}, bytes...)
	}
	bytes = append(bytes, '\n')
	if before.Size()+int64(len(bytes)) > retirementMaxFileBytes {
		return retirementError(RetirementRecordInvalid, errors.New("record would exceed size limit"))
	}
	write := s.write
	if write == nil {
		write = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
	}
	written, err := write(file, bytes)
	if err != nil || written != len(bytes) {
		return retirementError(RetirementRecordRecovery, errors.New("append was interrupted; inspection is required"))
	}
	syncFile := s.syncFile
	if syncFile == nil {
		syncFile = func(file *os.File) error { return file.Sync() }
	}
	if err := syncFile(file); err != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record durability is uncertain; inspection is required"))
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || validateOpenFile(file, 0o600) != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record identity changed during append"))
	}
	pathInfo, err := statAt(records, name)
	if err != nil || !os.SameFile(after, pathInfo) {
		return retirementError(RetirementRecordRecovery, errors.New("record path identity changed during append"))
	}
	syncDir := s.syncDir
	var syncErr error
	if syncDir == nil {
		syncErr = records.Sync()
	} else {
		syncErr = syncDir(filepath.Dir(path))
	}
	if syncErr != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record directory durability is uncertain"))
	}
	return nil
}

func (s RetirementStore) syncReplay(records *os.File, name, path string, expected os.FileInfo) error {
	file, err := openFileAt(records, name, os.O_RDONLY, 0)
	if err != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record cannot be reopened for durability verification"))
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || expected == nil || !os.SameFile(info, expected) || validateOpenFile(file, 0o600) != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record identity changed before replay verification"))
	}
	syncFile := s.syncFile
	if syncFile == nil {
		syncFile = func(file *os.File) error { return file.Sync() }
	}
	if err := syncFile(file); err != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record durability is uncertain; inspection is required"))
	}
	syncDir := s.syncDir
	var syncErr error
	if syncDir == nil {
		syncErr = records.Sync()
	} else {
		syncErr = syncDir(filepath.Dir(path))
	}
	if syncErr != nil {
		return retirementError(RetirementRecordRecovery, errors.New("record directory durability is uncertain"))
	}
	return nil
}

func validateDiscriminator(name, value string) error {
	if len(value) > retirementMaxDiscriminator || !retirementDiscriminatorPattern.MatchString(value) {
		return fmt.Errorf("%s is invalid or unbounded", name)
	}
	return validateNFC(name, value)
}

func validateCommitments(name string, values []string) error {
	if len(values) > retirementMaxCommitments {
		return fmt.Errorf("%s exceeds commitment count limit", name)
	}
	for index, value := range values {
		if err := validateDigest(fmt.Sprintf("%s %d", name, index+1), value, retirementIdentityDomain); err != nil {
			return err
		}
	}
	return nil
}

func validateIntent(intent []RetirementClassIntent) error {
	if len(intent) != len(retirementClasses) {
		return errors.New("intent must contain exactly six resource classes")
	}
	for index, class := range intent {
		if class.Class != retirementClasses[index] {
			return errors.New("intent classes must use the normative order")
		}
		if len(class.Items) > retirementMaxCommitments {
			return errors.New("intent class has too many items")
		}
		seen := make(map[string]bool)
		allowed := retirementDispositions[class.Class]
		for _, item := range class.Items {
			if err := validateDigest("resource commitment", item.ResourceCommitment, retirementIdentityDomain); err != nil {
				return err
			}
			if seen[item.ResourceCommitment] {
				return errors.New("intent has a duplicate resource commitment")
			}
			seen[item.ResourceCommitment] = true
			if err := validateDiscriminator("expected disposition", item.ExpectedDisposition); err != nil {
				return err
			}
			if !allowed[item.ExpectedDisposition] {
				return fmt.Errorf("disposition %q is not valid for class %s", item.ExpectedDisposition, class.Class)
			}
			if err := validateDigest("decision owner commitment", item.DecisionOwnerCommitment, retirementIdentityDomain); err != nil {
				return err
			}
		}
	}
	return nil
}

func intentCanonical(intent []RetirementClassIntent) canonicalValue {
	classes := make([]canonicalValue, 0, len(intent))
	for _, class := range intent {
		items := make([]canonicalValue, 0, len(class.Items))
		for _, item := range class.Items {
			items = append(items, cObject(map[string]canonicalValue{
				"decision_owner_commitment": cString(item.DecisionOwnerCommitment),
				"expected_disposition":      cString(item.ExpectedDisposition),
				"resource_commitment":       cString(item.ResourceCommitment),
			}))
		}
		classes = append(classes, cObject(map[string]canonicalValue{"class": cString(class.Class), "items": cArray(items...)}))
	}
	return cArray(classes...)
}

func stringsCanonical(values []string) canonicalValue {
	items := make([]canonicalValue, len(values))
	for index, value := range values {
		items[index] = cString(value)
	}
	return cArray(items...)
}

func payloadCanonical(eventType string, payload any) (canonicalValue, error) {
	switch eventType {
	case RetirementRecordCreated:
		value, ok := payload.(RetirementRecordCreatedPayload)
		if !ok {
			return canonicalValue{}, errors.New("record_created payload has the wrong type")
		}
		if value.CanonicalizationVersion != RetirementCanonicalVersion {
			return canonicalValue{}, errors.New("unsupported canonicalization version")
		}
		if err := validateSubject(value.Subject); err != nil {
			return canonicalValue{}, err
		}
		if err := validateIntent(value.InitialIntent); err != nil {
			return canonicalValue{}, err
		}
		if value.InitialDescendantState != "exact_bindings" && value.InitialDescendantState != "explicit_none" && value.InitialDescendantState != "explicit_unknown" {
			return canonicalValue{}, errors.New("invalid initial descendant state")
		}
		if err := validateDigest("initial descendant commitment", value.InitialDescendantCommitment, retirementIdentityDomain); err != nil {
			return canonicalValue{}, err
		}
		return cObject(map[string]canonicalValue{
			"canonicalization_version":      cInt(int64(value.CanonicalizationVersion)),
			"initial_descendant_commitment": cString(value.InitialDescendantCommitment),
			"initial_descendant_state":      cString(value.InitialDescendantState),
			"initial_intent":                intentCanonical(value.InitialIntent),
			"subject":                       subjectCanonical(value.Subject),
		}), nil
	case RetirementOperationDeclared:
		value, ok := payload.(RetirementOperationDeclaredPayload)
		if !ok {
			return canonicalValue{}, errors.New("operation_declared payload has the wrong type")
		}
		if err := validateDiscriminator("operation scope", value.Scope); err != nil {
			return canonicalValue{}, err
		}
		if err := ValidateRetirementRecordID(value.RecordID); err != nil {
			return canonicalValue{}, err
		}
		recordCommitment, err := RetirementRecordCommitment(value.RecordID)
		if err != nil || value.RecordCommitment != recordCommitment {
			return canonicalValue{}, errors.New("record commitment does not match record ID")
		}
		if !retirementOperationPattern.MatchString(value.OperationID) {
			return canonicalValue{}, errors.New("operation payload ID is invalid")
		}
		if err := validateDigest("subject commitment", value.SubjectCommitment, retirementIdentityDomain); err != nil {
			return canonicalValue{}, err
		}
		if err := validateIntent(value.Intent); err != nil {
			return canonicalValue{}, err
		}
		for name, commitments := range map[string][]string{"attachment commitments": value.AttachmentCommitments, "evidence commitments": value.EvidenceCommitments, "authority commitments": value.AuthorityCommitments} {
			if err := validateCommitments(name, commitments); err != nil {
				return canonicalValue{}, err
			}
		}
		if value.SupersedesOperationID != "" && !retirementOperationPattern.MatchString(value.SupersedesOperationID) {
			return canonicalValue{}, errors.New("superseded operation ID is invalid")
		}
		projection := operationProjection(value)
		digest, err := domainDigest(retirementOperationDomain, projection)
		if err != nil || value.OperationDigest != digest {
			return canonicalValue{}, errors.New("operation digest does not match canonical operation")
		}
		fields := projection.obj
		fields["operation_digest"] = cString(value.OperationDigest)
		return cObject(fields), nil
	case RetirementIdentityRecorded:
		value, ok := payload.(RetirementIdentityCommitmentPayload)
		if !ok {
			return canonicalValue{}, errors.New("identity commitment payload has the wrong type")
		}
		if err := validateDiscriminator("identity kind", value.IdentityKind); err != nil {
			return canonicalValue{}, err
		}
		if err := validateDigest("identity commitment", value.IdentityCommitment, retirementIdentityDomain); err != nil {
			return canonicalValue{}, err
		}
		if value.SupersedesSequence < 0 {
			return canonicalValue{}, errors.New("supersedes sequence cannot be negative")
		}
		fields := map[string]canonicalValue{"identity_commitment": cString(value.IdentityCommitment), "identity_kind": cString(value.IdentityKind)}
		if value.SupersedesSequence != 0 {
			fields["supersedes_sequence"] = cInt(value.SupersedesSequence)
		}
		return cObject(fields), nil
	case RetirementNoteAppended:
		value, ok := payload.(RetirementNotePayload)
		if !ok {
			return canonicalValue{}, errors.New("record note payload has the wrong type")
		}
		if err := validateDiscriminator("note kind", value.NoteKind); err != nil {
			return canonicalValue{}, err
		}
		if err := validateDigest("note commitment", value.NoteCommitment, retirementIdentityDomain); err != nil {
			return canonicalValue{}, err
		}
		if value.SupersedesSequence < 0 {
			return canonicalValue{}, errors.New("supersedes sequence cannot be negative")
		}
		fields := map[string]canonicalValue{"note_commitment": cString(value.NoteCommitment), "note_kind": cString(value.NoteKind)}
		if value.SupersedesSequence != 0 {
			fields["supersedes_sequence"] = cInt(value.SupersedesSequence)
		}
		return cObject(fields), nil
	default:
		return canonicalValue{}, fmt.Errorf("unsupported retirement event type %q", eventType)
	}
}

func validateSubject(subject RetirementSubject) error {
	if subject.CreatedBy != "spawn" && subject.CreatedBy != "adopt" && subject.CreatedBy != "legacy_admission" {
		return errors.New("subject created_by is invalid")
	}
	for name, value := range map[string]string{
		"thread commitment":           subject.ThreadCommitment,
		"worker binding commitment":   subject.WorkerBindingCommitment,
		"workspace commitment":        subject.WorkspaceCommitment,
		"initial worktree commitment": subject.InitialWorktreeCommitment,
		"physical owner commitment":   subject.PhysicalOwnerCommitment,
	} {
		if err := validateDigest(name, value, retirementIdentityDomain); err != nil {
			return err
		}
	}
	return nil
}

func subjectCanonical(subject RetirementSubject) canonicalValue {
	return cObject(map[string]canonicalValue{
		"created_by":                  cString(subject.CreatedBy),
		"initial_worktree_commitment": cString(subject.InitialWorktreeCommitment),
		"physical_owner_commitment":   cString(subject.PhysicalOwnerCommitment),
		"thread_commitment":           cString(subject.ThreadCommitment),
		"worker_binding_commitment":   cString(subject.WorkerBindingCommitment),
		"workspace_commitment":        cString(subject.WorkspaceCommitment),
	})
}

func RetirementSubjectCommitment(subject RetirementSubject) (string, error) {
	if err := validateSubject(subject); err != nil {
		return "", err
	}
	return domainDigest(retirementIdentityDomain, cObject(map[string]canonicalValue{
		"kind":    cString("retirement_subject"),
		"subject": subjectCanonical(subject),
	}))
}

func operationProjection(value RetirementOperationDeclaredPayload) canonicalValue {
	fields := map[string]canonicalValue{
		"attachment_commitments": stringsCanonical(value.AttachmentCommitments),
		"authority_commitments":  stringsCanonical(value.AuthorityCommitments),
		"evidence_commitments":   stringsCanonical(value.EvidenceCommitments),
		"intent":                 intentCanonical(value.Intent),
		"operation_id":           cString(value.OperationID),
		"record_commitment":      cString(value.RecordCommitment),
		"record_id":              cString(value.RecordID),
		"scope":                  cString(value.Scope),
		"subject_commitment":     cString(value.SubjectCommitment),
	}
	if value.SupersedesOperationID != "" {
		fields["supersedes_operation_id"] = cString(value.SupersedesOperationID)
	}
	return cObject(fields)
}

func RetirementOperationDigest(value RetirementOperationDeclaredPayload) (string, error) {
	value.OperationDigest = ""
	if err := validateDiscriminator("operation scope", value.Scope); err != nil {
		return "", err
	}
	if err := validateIntent(value.Intent); err != nil {
		return "", err
	}
	if err := ValidateRetirementRecordID(value.RecordID); err != nil {
		return "", err
	}
	recordCommitment, err := RetirementRecordCommitment(value.RecordID)
	if err != nil || value.RecordCommitment != recordCommitment {
		return "", errors.New("record commitment does not match record ID")
	}
	if !retirementOperationPattern.MatchString(value.OperationID) {
		return "", errors.New("operation payload ID is invalid")
	}
	if err := validateDigest("subject commitment", value.SubjectCommitment, retirementIdentityDomain); err != nil {
		return "", err
	}
	return domainDigest(retirementOperationDomain, operationProjection(value))
}

func encodeRetirementEvent(event RetirementEvent, omitDigest bool) ([]byte, error) {
	payload, err := payloadCanonical(event.EventType, event.Payload)
	if err != nil {
		return nil, err
	}
	fields := map[string]canonicalValue{
		"event_type":            cString(event.EventType),
		"operation_id":          cString(event.OperationID),
		"payload":               payload,
		"previous_event_digest": cString(event.PreviousEventDigest),
		"record_id":             cString(event.RecordID),
		"schema_version":        cInt(int64(event.SchemaVersion)),
		"sequence":              cInt(event.Sequence),
		"written_at":            cString(event.WrittenAt.UTC().Format("2006-01-02T15:04:05.000000Z")),
	}
	if !omitDigest {
		fields["event_digest"] = cString(event.EventDigest)
	}
	return encodeCanonical(cObject(fields))
}

func eventDigest(event RetirementEvent) (string, error) {
	payload, err := payloadCanonical(event.EventType, event.Payload)
	if err != nil {
		return "", err
	}
	return domainDigest(retirementEventDomain, cObject(map[string]canonicalValue{
		"event_type":            cString(event.EventType),
		"operation_id":          cString(event.OperationID),
		"payload":               payload,
		"previous_event_digest": cString(event.PreviousEventDigest),
		"record_id":             cString(event.RecordID),
		"schema_version":        cInt(int64(event.SchemaVersion)),
		"sequence":              cInt(event.Sequence),
		"written_at":            cString(event.WrittenAt.UTC().Format("2006-01-02T15:04:05.000000Z")),
	}))
}

func decodeRetirementEvent(data []byte) (RetirementEvent, error) {
	value, err := parseCanonicalJSON(data)
	if err != nil {
		return RetirementEvent{}, err
	}
	fields, err := exactObject(value, "schema_version", "record_id", "sequence", "event_type", "operation_id", "previous_event_digest", "payload", "event_digest", "written_at")
	if err != nil {
		return RetirementEvent{}, err
	}
	schema, err := integerField(fields, "schema_version")
	if err != nil || schema != RetirementSchemaVersion {
		return RetirementEvent{}, errors.New("unsupported retirement schema version")
	}
	recordID, err := stringField(fields, "record_id")
	if err != nil || ValidateRetirementRecordID(recordID) != nil {
		return RetirementEvent{}, errors.New("invalid record ID")
	}
	sequence, err := integerField(fields, "sequence")
	if err != nil || sequence < 1 {
		return RetirementEvent{}, errors.New("invalid sequence")
	}
	eventType, err := stringField(fields, "event_type")
	if err != nil {
		return RetirementEvent{}, err
	}
	operationID, err := stringField(fields, "operation_id")
	if err != nil || !retirementOperationPattern.MatchString(operationID) || validateNFC("operation ID", operationID) != nil {
		return RetirementEvent{}, errors.New("invalid operation ID")
	}
	previous, err := stringField(fields, "previous_event_digest")
	if err != nil || previous != "" && validateDigest("previous event digest", previous, retirementEventDomain) != nil {
		return RetirementEvent{}, errors.New("invalid previous event digest")
	}
	digest, err := stringField(fields, "event_digest")
	if err != nil || validateDigest("event digest", digest, retirementEventDomain) != nil {
		return RetirementEvent{}, errors.New("invalid event digest")
	}
	written, err := stringField(fields, "written_at")
	if err != nil {
		return RetirementEvent{}, err
	}
	writtenAt, err := time.Parse("2006-01-02T15:04:05.000000Z", written)
	if err != nil || writtenAt.Format("2006-01-02T15:04:05.000000Z") != written {
		return RetirementEvent{}, errors.New("written_at is not fixed-microsecond UTC")
	}
	payload, err := decodeRetirementPayload(eventType, fields["payload"])
	if err != nil {
		return RetirementEvent{}, err
	}
	return RetirementEvent{SchemaVersion: int(schema), RecordID: recordID, Sequence: sequence, EventType: eventType, OperationID: operationID, PreviousEventDigest: previous, Payload: payload, EventDigest: digest, WrittenAt: writtenAt}, nil
}

func decodeRetirementPayload(eventType string, value canonicalValue) (any, error) {
	switch eventType {
	case RetirementRecordCreated:
		fields, err := exactObject(value, "canonicalization_version", "initial_descendant_commitment", "initial_descendant_state", "initial_intent", "subject")
		if err != nil {
			return nil, err
		}
		version, err := integerField(fields, "canonicalization_version")
		if err != nil {
			return nil, err
		}
		subject, err := decodeSubject(fields["subject"])
		if err != nil {
			return nil, err
		}
		intent, err := decodeIntent(fields["initial_intent"])
		if err != nil {
			return nil, err
		}
		state, err := stringField(fields, "initial_descendant_state")
		if err != nil {
			return nil, err
		}
		commitment, err := stringField(fields, "initial_descendant_commitment")
		if err != nil {
			return nil, err
		}
		payload := RetirementRecordCreatedPayload{CanonicalizationVersion: int(version), Subject: subject, InitialIntent: intent, InitialDescendantState: state, InitialDescendantCommitment: commitment}
		_, err = payloadCanonical(eventType, payload)
		return payload, err
	case RetirementOperationDeclared:
		if value.kind != canonicalObject {
			return nil, errors.New("operation payload must be an object")
		}
		allowed := []string{"attachment_commitments", "authority_commitments", "evidence_commitments", "intent", "operation_digest", "operation_id", "record_commitment", "record_id", "scope", "subject_commitment"}
		if _, exists := value.obj["supersedes_operation_id"]; exists {
			allowed = append(allowed, "supersedes_operation_id")
		}
		fields, err := exactObject(value, allowed...)
		if err != nil {
			return nil, err
		}
		intent, err := decodeIntent(fields["intent"])
		if err != nil {
			return nil, err
		}
		payload := RetirementOperationDeclaredPayload{Intent: intent}
		payload.RecordID, err = stringField(fields, "record_id")
		if err != nil {
			return nil, err
		}
		payload.RecordCommitment, err = stringField(fields, "record_commitment")
		if err != nil {
			return nil, err
		}
		payload.OperationID, err = stringField(fields, "operation_id")
		if err != nil {
			return nil, err
		}
		payload.SubjectCommitment, err = stringField(fields, "subject_commitment")
		if err != nil {
			return nil, err
		}
		payload.OperationDigest, err = stringField(fields, "operation_digest")
		if err != nil {
			return nil, err
		}
		payload.Scope, err = stringField(fields, "scope")
		if err != nil {
			return nil, err
		}
		payload.AttachmentCommitments, err = decodeStrings(fields["attachment_commitments"])
		if err != nil {
			return nil, err
		}
		payload.AuthorityCommitments, err = decodeStrings(fields["authority_commitments"])
		if err != nil {
			return nil, err
		}
		payload.EvidenceCommitments, err = decodeStrings(fields["evidence_commitments"])
		if err != nil {
			return nil, err
		}
		if _, exists := fields["supersedes_operation_id"]; exists {
			payload.SupersedesOperationID, err = stringField(fields, "supersedes_operation_id")
			if err != nil {
				return nil, err
			}
		}
		_, err = payloadCanonical(eventType, payload)
		return payload, err
	case RetirementIdentityRecorded:
		if value.kind != canonicalObject {
			return nil, errors.New("identity commitment payload must be an object")
		}
		allowed := []string{"identity_commitment", "identity_kind"}
		if _, exists := value.obj["supersedes_sequence"]; exists {
			allowed = append(allowed, "supersedes_sequence")
		}
		fields, err := exactObject(value, allowed...)
		if err != nil {
			return nil, err
		}
		kind, err := stringField(fields, "identity_kind")
		if err != nil {
			return nil, err
		}
		commitment, err := stringField(fields, "identity_commitment")
		if err != nil {
			return nil, err
		}
		var sequence int64
		if _, exists := fields["supersedes_sequence"]; exists {
			sequence, err = integerField(fields, "supersedes_sequence")
			if err != nil {
				return nil, err
			}
		}
		payload := RetirementIdentityCommitmentPayload{IdentityKind: kind, IdentityCommitment: commitment, SupersedesSequence: sequence}
		_, err = payloadCanonical(eventType, payload)
		return payload, err
	case RetirementNoteAppended:
		if value.kind != canonicalObject {
			return nil, errors.New("record note payload must be an object")
		}
		allowed := []string{"note_commitment", "note_kind"}
		if _, exists := value.obj["supersedes_sequence"]; exists {
			allowed = append(allowed, "supersedes_sequence")
		}
		fields, err := exactObject(value, allowed...)
		if err != nil {
			return nil, err
		}
		kind, err := stringField(fields, "note_kind")
		if err != nil {
			return nil, err
		}
		commitment, err := stringField(fields, "note_commitment")
		if err != nil {
			return nil, err
		}
		var sequence int64
		if _, exists := fields["supersedes_sequence"]; exists {
			sequence, err = integerField(fields, "supersedes_sequence")
			if err != nil {
				return nil, err
			}
		}
		payload := RetirementNotePayload{NoteKind: kind, NoteCommitment: commitment, SupersedesSequence: sequence}
		_, err = payloadCanonical(eventType, payload)
		return payload, err
	default:
		return nil, errors.New("unsupported retirement event type")
	}
}

func decodeSubject(value canonicalValue) (RetirementSubject, error) {
	fields, err := exactObject(value, "created_by", "initial_worktree_commitment", "physical_owner_commitment", "thread_commitment", "worker_binding_commitment", "workspace_commitment")
	if err != nil {
		return RetirementSubject{}, err
	}
	var subject RetirementSubject
	subject.CreatedBy, err = stringField(fields, "created_by")
	if err != nil {
		return subject, err
	}
	subject.InitialWorktreeCommitment, err = stringField(fields, "initial_worktree_commitment")
	if err != nil {
		return subject, err
	}
	subject.PhysicalOwnerCommitment, err = stringField(fields, "physical_owner_commitment")
	if err != nil {
		return subject, err
	}
	subject.ThreadCommitment, err = stringField(fields, "thread_commitment")
	if err != nil {
		return subject, err
	}
	subject.WorkerBindingCommitment, err = stringField(fields, "worker_binding_commitment")
	if err != nil {
		return subject, err
	}
	subject.WorkspaceCommitment, err = stringField(fields, "workspace_commitment")
	if err != nil {
		return subject, err
	}
	return subject, validateSubject(subject)
}

func decodeIntent(value canonicalValue) ([]RetirementClassIntent, error) {
	if value.kind != canonicalArray {
		return nil, errors.New("intent must be an array")
	}
	intent := make([]RetirementClassIntent, 0, len(value.arr))
	for _, classValue := range value.arr {
		fields, err := exactObject(classValue, "class", "items")
		if err != nil {
			return nil, err
		}
		class, err := stringField(fields, "class")
		if err != nil {
			return nil, err
		}
		if fields["items"].kind != canonicalArray {
			return nil, errors.New("intent items must be an array")
		}
		items := make([]RetirementIntentItem, 0, len(fields["items"].arr))
		for _, itemValue := range fields["items"].arr {
			itemFields, err := exactObject(itemValue, "decision_owner_commitment", "expected_disposition", "resource_commitment")
			if err != nil {
				return nil, err
			}
			var item RetirementIntentItem
			item.DecisionOwnerCommitment, err = stringField(itemFields, "decision_owner_commitment")
			if err != nil {
				return nil, err
			}
			item.ExpectedDisposition, err = stringField(itemFields, "expected_disposition")
			if err != nil {
				return nil, err
			}
			item.ResourceCommitment, err = stringField(itemFields, "resource_commitment")
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		intent = append(intent, RetirementClassIntent{Class: class, Items: items})
	}
	return intent, validateIntent(intent)
}

func decodeStrings(value canonicalValue) ([]string, error) {
	if value.kind != canonicalArray {
		return nil, errors.New("commitments must be an array")
	}
	strings := make([]string, 0, len(value.arr))
	for _, item := range value.arr {
		if item.kind != canonicalString {
			return nil, errors.New("commitment must be a string")
		}
		strings = append(strings, item.str)
	}
	return strings, nil
}
