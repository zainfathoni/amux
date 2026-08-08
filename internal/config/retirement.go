package config

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	RetirementStreamSchemaVersion     = 1
	RetirementCanonicalizationVersion = 1
	retirementStreamFileSuffix        = ".jsonl"
)

const (
	RetirementResourceClassAmpThread          RetirementResourceClass = "amp_thread"
	RetirementResourceClassTmuxClientProcess  RetirementResourceClass = "tmux_client_process"
	RetirementResourceClassGitWorktree        RetirementResourceClass = "git_worktree"
	RetirementResourceClassCatalogRecoveryPtr RetirementResourceClass = "catalog_recovery_pointer"
	RetirementResourceClassProviderEvidence   RetirementResourceClass = "provider_evidence"
	RetirementResourceClassDescendant         RetirementResourceClass = "descendant"
)

var retirementResourceClasses = map[RetirementResourceClass]struct{}{
	RetirementResourceClassAmpThread:          {},
	RetirementResourceClassTmuxClientProcess:  {},
	RetirementResourceClassGitWorktree:        {},
	RetirementResourceClassCatalogRecoveryPtr: {},
	RetirementResourceClassProviderEvidence:   {},
	RetirementResourceClassDescendant:         {},
}

var retirementRecordIDRe = regexp.MustCompile(`^ret_[a-zA-Z0-9._-]{8,128}$`)

type RetirementResourceClass string

type RetirementEventKind string

const (
	RetirementEventHeader           RetirementEventKind = "retirement-record-header-v1"
	RetirementEventPrepareManifest  RetirementEventKind = "retirement-prepare-manifest-v1"
	RetirementEventFinalizeEvent    RetirementEventKind = "retirement-finalize-event-v1"
	RetirementEventOperationSummary RetirementEventKind = "retirement-operation-summary-v1"
)

type RetirementRecordSubject struct {
	ThreadID                string `json:"thread_id"`
	WorkerBinding           string `json:"worker_binding"`
	WorkspaceIdentity       string `json:"workspace_identity"`
	InitialWorktreeIdentity string `json:"initial_worktree_identity"`
}

type RetirementResourceID struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
}

type RetirementPlanItem struct {
	ResourceID          RetirementResourceID `json:"resource_id"`
	ExpectedDisposition string               `json:"expected_disposition"`
	DecisionOwner       string               `json:"decision_owner"`
}

type RetirementPlanContainer struct {
	Class         RetirementResourceClass `json:"class"`
	Items         []RetirementPlanItem    `json:"items"`
	DerivedStatus string                  `json:"derived_status,omitempty"`
}

type RetirementRecordHeader struct {
	SchemaVersion           int                       `json:"schema_version"`
	CanonicalizationVersion int                       `json:"canonicalization_version"`
	RetirementRecordID      string                    `json:"retirement_record_id"`
	CreatedBy               string                    `json:"created_by"`
	Subject                 RetirementRecordSubject   `json:"subject"`
	PhysicalWorktreeOwner   string                    `json:"physical_worktree_owner"`
	InitialPlan             []RetirementPlanContainer `json:"initial_plan"`
	InitialDescendantState  string                    `json:"initial_descendant_state"`
	CreatedAt               time.Time                 `json:"created_at"`
}

type RetirementDepartureTarget struct {
	ThreadID           string `json:"thread_id"`
	WorktreeIdentity   string `json:"worktree_identity"`
	AttachmentIdentity string `json:"attachment_identity"`
	ObservedGeneration int64  `json:"observed_generation"`
}

type RetirementAttachmentSnapshot struct {
	WorktreeIdentity           string   `json:"worktree_identity"`
	Generation                 int64    `json:"generation"`
	ExactLiveAttachments       []string `json:"exact_live_attachments"`
	StaleOrUnprovenAttachments []string `json:"stale_or_unproven_attachments"`
}

type RetirementAuthorityScope struct {
	Principal string `json:"principal"`
	Resource  string `json:"resource"`
	Action    string `json:"action"`
	Operation string `json:"operation"`
}

type RetirementPrepareManifest struct {
	CanonicalizationVersion        int                          `json:"canonicalization_version"`
	RetirementRecordID             string                       `json:"retirement_record_id"`
	OperationID                    string                       `json:"operation_id"`
	Attempt                        int                          `json:"attempt"`
	PreparedByExactThread          string                       `json:"prepared_by_exact_thread"`
	IntendedFinalizerClass         string                       `json:"intended_finalizer_class"`
	DepartureTarget                RetirementDepartureTarget    `json:"departure_target"`
	AttachmentSnapshot             RetirementAttachmentSnapshot `json:"attachment_snapshot"`
	RequiredAuthorityScopes        []RetirementAuthorityScope   `json:"required_authority_scopes"`
	EvidenceCommitments            []string                     `json:"evidence_commitments"`
	DirtyDiscardManifestCommitment string                       `json:"dirty_discard_manifest_commitment,omitempty"`
	Resources                      []RetirementPlanContainer    `json:"resources"`
	ManifestDigest                 string                       `json:"manifest_digest"`
	PreparedAt                     time.Time                    `json:"prepared_at"`
}

type RetirementFinalizeEvent struct {
	ManifestDigest   string                  `json:"manifest_digest"`
	ManifestSequence int                     `json:"manifest_sequence"`
	ResourceClass    RetirementResourceClass `json:"resource_class"`
	ResourceID       RetirementResourceID    `json:"resource_id"`
	Outcome          string                  `json:"outcome"`
	ReasonCode       string                  `json:"reason_code,omitempty"`
}

type RetirementOperationSummary struct {
	RetirementRecordID   string `json:"retirement_record_id"`
	OperationID          string `json:"operation_id"`
	ManifestDigest       string `json:"manifest_digest"`
	PlanSatisfaction     string `json:"plan_satisfaction"`
	RetirementCompletion string `json:"retirement_completion"`
}

type RetirementStreamEvent struct {
	SchemaVersion      int                 `json:"schema_version"`
	Kind               RetirementEventKind `json:"kind"`
	Sequence           int                 `json:"sequence"`
	RetirementRecordID string              `json:"retirement_record_id"`
	EventID            string              `json:"event_id"`
	OccurredAt         time.Time           `json:"occurred_at"`

	Header           *RetirementRecordHeader     `json:"header,omitempty"`
	PrepareManifest  *RetirementPrepareManifest  `json:"prepare_manifest,omitempty"`
	FinalizeEvent    *RetirementFinalizeEvent    `json:"finalize_event,omitempty"`
	OperationSummary *RetirementOperationSummary `json:"operation_summary,omitempty"`
}

func RetirementRecordStreamPath(dir Directory, recordID string) (string, error) {
	if err := ValidateRetirementRecordID(recordID); err != nil {
		return "", err
	}
	return filepath.Join(dir.Path, RetirementRecordsDirectory, recordID+retirementStreamFileSuffix), nil
}

func ValidateRetirementRecordID(recordID string) error {
	if recordID == "" {
		return errors.New("missing retirement record id")
	}
	if !retirementRecordIDRe.MatchString(recordID) {
		return fmt.Errorf("invalid retirement record id %q", recordID)
	}
	return nil
}

func LoadRetirementRecordStream(dir Directory, recordID string) ([]RetirementStreamEvent, error) {
	path, err := RetirementRecordStreamPath(dir, recordID)
	if err != nil {
		return nil, err
	}
	return LoadRetirementStream(path)
}

func LoadRetirementStream(path string) ([]RetirementStreamEvent, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1024<<10)
	events := []RetirementStreamEvent{}
	seenEventIDs := map[string]int{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event RetirementStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("parse retirement stream line %d: %w", lineNo, err)
		}
		if err := canonicalizeRetirementStreamEvent(&event, time.Time{}); err != nil {
			return nil, fmt.Errorf("invalid retirement stream line %d: %w", lineNo, err)
		}
		if event.EventID == "" {
			eventID, err := retirementStreamEventPayloadID(event)
			if err != nil {
				return nil, fmt.Errorf("invalid retirement stream line %d: %w", lineNo, err)
			}
			event.EventID = eventID
		}
		if err := validateRetirementStreamEvent(events, event); err != nil {
			return nil, fmt.Errorf("invalid retirement stream line %d: %w", lineNo, err)
		}
		if _, exists := seenEventIDs[event.EventID]; exists {
			return nil, fmt.Errorf("invalid retirement stream line %d: duplicate event id %q", lineNo, event.EventID)
		}
		seenEventIDs[event.EventID] = lineNo
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func AppendRetirementRecordEvent(dir Directory, recordID string, event RetirementStreamEvent, now time.Time) (RetirementStreamEvent, bool, error) {
	path, err := RetirementRecordStreamPath(dir, recordID)
	if err != nil {
		return RetirementStreamEvent{}, false, err
	}
	return AppendRetirementStreamEvent(path, event, now)
}

func AppendRetirementStreamEvent(path string, event RetirementStreamEvent, now time.Time) (RetirementStreamEvent, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := validateRetirementPathForStream(path); err != nil {
		return RetirementStreamEvent{}, false, err
	}
	if err := canonicalizeRetirementStreamEvent(&event, now); err != nil {
		return RetirementStreamEvent{}, false, err
	}

	existing, err := LoadRetirementStream(path)
	if err != nil {
		return RetirementStreamEvent{}, false, err
	}
	if event.Sequence == 0 {
		event.Sequence = len(existing) + 1
	}
	if err := validateRetirementStreamEvent(existing, event); err != nil {
		return RetirementStreamEvent{}, false, err
	}

	eventID := event.EventID
	if eventID == "" {
		eventID, err = retirementStreamEventPayloadID(event)
		if err != nil {
			return RetirementStreamEvent{}, false, err
		}
		event.EventID = eventID
	}

	for _, prior := range existing {
		if prior.EventID != eventID {
			continue
		}
		if priorEventEqual(prior, event) {
			return prior, false, nil
		}
		return RetirementStreamEvent{}, false, fmt.Errorf("conflicting replay for event id %q", eventID)
	}

	canonical, err := encodeRetirementStreamEvent(event)
	if err != nil {
		return RetirementStreamEvent{}, false, err
	}
	if err := writeRetirementStreamEvent(path, canonical); err != nil {
		return RetirementStreamEvent{}, false, err
	}
	return event, true, nil
}

func encodeRetirementStreamEvent(event RetirementStreamEvent) ([]byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeRetirementStreamEvent(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	_, err = file.Write(payload)
	if err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	dirFile, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dirFile.Close()
	if err := dirFile.Sync(); err != nil {
		return err
	}
	return nil
}

func validateRetirementPathForStream(path string) error {
	dir := filepath.Clean(filepath.Dir(path))
	if dir == "" || dir == "." {
		return errors.New("empty retirement stream directory")
	}
	return nil
}

func canonicalizeRetirementStreamEvent(event *RetirementStreamEvent, now time.Time) error {
	if event.SchemaVersion == 0 {
		event.SchemaVersion = RetirementStreamSchemaVersion
	}
	if event.SchemaVersion != RetirementStreamSchemaVersion {
		return fmt.Errorf("unsupported stream schema version %d", event.SchemaVersion)
	}
	if event.RetirementRecordID == "" {
		return errors.New("missing retirement record id")
	}
	if err := ValidateRetirementRecordID(event.RetirementRecordID); err != nil {
		return err
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	if event.OccurredAt.IsZero() {
		return errors.New("missing occurred_at")
	}
	if event.EventID == "" {
		// event id is derived below so this is optional for caller-provided payloads
	}
	if event.Kind == "" {
		return errors.New("missing event kind")
	}

	switch event.Kind {
	case RetirementEventHeader:
		if event.Header == nil {
			return errors.New("header event requires header payload")
		}
		if event.Header.RetirementRecordID == "" {
			event.Header.RetirementRecordID = event.RetirementRecordID
		}
		if event.Header.RetirementRecordID != event.RetirementRecordID {
			return errors.New("header retirement_record_id does not match event record id")
		}
		if err := normalizeRetirementRecordHeader(event.Header); err != nil {
			return err
		}
		event.Header.RetirementRecordID = event.RetirementRecordID
		event.PrepareManifest = nil
		event.FinalizeEvent = nil
		event.OperationSummary = nil
	case RetirementEventPrepareManifest:
		if event.PrepareManifest == nil {
			return errors.New("prepare event requires prepare_manifest payload")
		}
		if event.PrepareManifest.RetirementRecordID == "" {
			event.PrepareManifest.RetirementRecordID = event.RetirementRecordID
		}
		if event.PrepareManifest.RetirementRecordID != event.RetirementRecordID {
			return errors.New("prepare manifest retirement_record_id does not match event record id")
		}
		if err := normalizeRetirementPrepareManifest(event.PrepareManifest); err != nil {
			return err
		}
		digest, err := RetirementPrepareManifestDigest(event.PrepareManifest)
		if err != nil {
			return err
		}
		event.PrepareManifest.ManifestDigest = digest
		event.Header = nil
		event.FinalizeEvent = nil
		event.OperationSummary = nil
	case RetirementEventFinalizeEvent:
		if event.FinalizeEvent == nil {
			return errors.New("finalize event requires finalize_event payload")
		}
		if err := validateField("manifest_digest", event.FinalizeEvent.ManifestDigest); err != nil {
			return err
		}
		if event.FinalizeEvent.ManifestSequence <= 0 {
			return errors.New("manifest_sequence must be > 0")
		}
		if err := validateField("outcome", event.FinalizeEvent.Outcome); err != nil {
			return err
		}
		if err := validateRetirementResourceClass(event.FinalizeEvent.ResourceClass); err != nil {
			return err
		}
		if err := validateRetirementResourceID(event.FinalizeEvent.ResourceID); err != nil {
			return err
		}
		event.Header = nil
		event.PrepareManifest = nil
		event.OperationSummary = nil
	case RetirementEventOperationSummary:
		if event.OperationSummary == nil {
			return errors.New("operation summary event requires operation_summary payload")
		}
		if err := validateRetirementRecordSummary(event.OperationSummary, event.RetirementRecordID); err != nil {
			return err
		}
		event.Header = nil
		event.PrepareManifest = nil
		event.FinalizeEvent = nil
	default:
		return fmt.Errorf("unsupported event kind %q", event.Kind)
	}

	return nil
}

func validateRetirementStreamEvent(existing []RetirementStreamEvent, event RetirementStreamEvent) error {
	if event.Sequence <= 0 {
		return errors.New("invalid sequence")
	}
	expected := len(existing) + 1
	if event.Sequence != expected {
		return fmt.Errorf("expected sequence %d, got %d", expected, event.Sequence)
	}
	if len(existing) == 0 {
		if event.Kind != RetirementEventHeader {
			return errors.New("retirement stream must begin with a header event")
		}
		return nil
	}
	if existing[len(existing)-1].RetirementRecordID != event.RetirementRecordID {
		return errors.New("retirement stream record id cannot change")
	}
	if existing[len(existing)-1].Sequence != len(existing) {
		return fmt.Errorf("invalid prior sequence %d", existing[len(existing)-1].Sequence)
	}

	switch event.Kind {
	case RetirementEventHeader:
		// stream records cannot be rewritten; header is immutable.
		if event.Header == nil {
			return errors.New("header event requires header payload")
		}
		return errors.New("header event cannot be emitted after stream start")
	default:
	}
	return nil
}

func validateRetirementResourceClass(class RetirementResourceClass) error {
	if class == "" {
		return errors.New("missing resource class")
	}
	if _, ok := retirementResourceClasses[class]; !ok {
		return fmt.Errorf("invalid resource class %q", class)
	}
	return nil
}

func validateRetirementResourceID(resource RetirementResourceID) error {
	if err := validateField("resource kind", resource.Kind); err != nil {
		return err
	}
	if err := validateField("resource identity", resource.Identity); err != nil {
		return err
	}
	return nil
}

func validateRetirementRecordHeader(header RetirementRecordHeader) error {
	if header.SchemaVersion == 0 {
		header.SchemaVersion = RetirementStreamSchemaVersion
	}
	if header.SchemaVersion != RetirementStreamSchemaVersion {
		return fmt.Errorf("unsupported header schema version %d", header.SchemaVersion)
	}
	if header.CanonicalizationVersion == 0 {
		header.CanonicalizationVersion = RetirementCanonicalizationVersion
	}
	if header.CanonicalizationVersion != RetirementCanonicalizationVersion {
		return fmt.Errorf("unsupported canonicalization version %d", header.CanonicalizationVersion)
	}
	if err := ValidateRetirementRecordID(header.RetirementRecordID); err != nil {
		return err
	}
	if err := validateField("created_by", header.CreatedBy); err != nil {
		return err
	}
	if err := validateField("physical_worktree_owner", header.PhysicalWorktreeOwner); err != nil {
		return err
	}
	if err := validateField("worker_binding", header.Subject.WorkerBinding); err != nil {
		return err
	}
	if err := validateField("workspace_identity", header.Subject.WorkspaceIdentity); err != nil {
		return err
	}
	if err := validateField("thread_id", header.Subject.ThreadID); err != nil {
		return err
	}
	if err := validateThreadID(header.Subject.ThreadID); err != nil {
		return err
	}
	if header.CreatedAt.IsZero() {
		return errors.New("header created_at is required")
	}
	if err := validateRetirementPlanContainers(header.InitialPlan); err != nil {
		return err
	}
	return nil
}

func validateRetirementPlanContainers(containers []RetirementPlanContainer) error {
	seen := map[RetirementResourceClass]struct{}{}
	for _, container := range containers {
		if err := validateRetirementResourceClass(container.Class); err != nil {
			return err
		}
		if _, ok := seen[container.Class]; ok {
			return fmt.Errorf("duplicate plan class %q", container.Class)
		}
		seen[container.Class] = struct{}{}
		for _, item := range container.Items {
			if err := validateRetirementResourceID(item.ResourceID); err != nil {
				return err
			}
			if err := validateField("expected_disposition", item.ExpectedDisposition); err != nil {
				return err
			}
			if err := validateField("decision_owner", item.DecisionOwner); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeRetirementRecordHeader(header *RetirementRecordHeader) error {
	if err := validateRetirementRecordHeader(*header); err != nil {
		return err
	}
	header.InitialPlan = cloneAndSortPlanContainers(header.InitialPlan)
	return nil
}

func normalizeRetirementPrepareManifest(manifest *RetirementPrepareManifest) error {
	if err := validateField("retirement_record_id", manifest.RetirementRecordID); err != nil {
		return err
	}
	if manifest.CanonicalizationVersion == 0 {
		manifest.CanonicalizationVersion = RetirementCanonicalizationVersion
	}
	if manifest.CanonicalizationVersion != RetirementCanonicalizationVersion {
		return fmt.Errorf("unsupported canonicalization version %d", manifest.CanonicalizationVersion)
	}
	if err := validateField("operation_id", manifest.OperationID); err != nil {
		return err
	}
	if manifest.OperationID == "" {
		return errors.New("missing operation_id")
	}
	if err := validateField("prepared_by_exact_thread", manifest.PreparedByExactThread); err != nil {
		return err
	}
	if err := validateThreadID(manifest.PreparedByExactThread); err != nil {
		return err
	}
	if manifest.Attempt <= 0 {
		return errors.New("attempt must be > 0")
	}
	if err := validateField("intended_finalizer_class", manifest.IntendedFinalizerClass); err != nil {
		return err
	}
	if err := validateField("departure_target.thread_id", manifest.DepartureTarget.ThreadID); err != nil {
		return err
	}
	if err := validateThreadID(manifest.DepartureTarget.ThreadID); err != nil {
		return err
	}
	if manifest.DepartureTarget.WorktreeIdentity == "" {
		return errors.New("missing departure_target.worktree_identity")
	}
	if manifest.DepartureTarget.AttachmentIdentity == "" {
		return errors.New("missing departure_target.attachment_identity")
	}
	if manifest.AttachmentSnapshot.WorktreeIdentity == "" {
		return errors.New("missing attachment_snapshot.worktree_identity")
	}
	if manifest.PreparedAt.IsZero() {
		return errors.New("prepared_at is required")
	}
	manifest.Resources = cloneAndSortPlanContainers(manifest.Resources)
	if err := validateRetirementPlanContainers(manifest.Resources); err != nil {
		return err
	}
	for _, commitment := range manifest.EvidenceCommitments {
		if err := validateField("evidence_commitment", commitment); err != nil {
			return err
		}
	}
	for _, scope := range manifest.RequiredAuthorityScopes {
		if err := validateField("authority_principal", scope.Principal); err != nil {
			return err
		}
		if err := validateField("authority_resource", scope.Resource); err != nil {
			return err
		}
		if err := validateField("authority_action", scope.Action); err != nil {
			return err
		}
		if err := validateField("authority_operation", scope.Operation); err != nil {
			return err
		}
	}
	return nil
}

func validateThreadID(value string) error {
	if _, err := CanonicalThreadID(value); err != nil {
		return err
	}
	return nil
}

func validateRetirementRecordSummary(summary *RetirementOperationSummary, recordID string) error {
	if summary.RetirementRecordID == "" {
		return errors.New("missing retirement_record_id")
	}
	if err := validateField("retirement_record_id", summary.RetirementRecordID); err != nil {
		return err
	}
	if summary.RetirementRecordID != recordID {
		return errors.New("summary retirement record id does not match event record id")
	}
	if err := validateField("operation_id", summary.OperationID); err != nil {
		return err
	}
	if err := validateField("manifest_digest", summary.ManifestDigest); err != nil {
		return err
	}
	if summary.PlanSatisfaction == "" {
		return errors.New("missing plan_satisfaction")
	}
	if summary.RetirementCompletion == "" {
		return errors.New("missing retirement_completion")
	}
	return nil
}

func cloneAndSortPlanContainers(containers []RetirementPlanContainer) []RetirementPlanContainer {
	out := append([]RetirementPlanContainer(nil), containers...)
	sort.Slice(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	for i := range out {
		items := append([]RetirementPlanItem(nil), out[i].Items...)
		sort.Slice(items, func(a, b int) bool {
			if items[a].ResourceID.Kind == items[b].ResourceID.Kind {
				return items[a].ResourceID.Identity < items[b].ResourceID.Identity
			}
			return items[a].ResourceID.Kind < items[b].ResourceID.Kind
		})
		out[i].Items = items
	}
	return out
}

func retirementStreamEventPayloadID(event RetirementStreamEvent) (string, error) {
	event.Sequence = 0
	event.EventID = ""
	event.OccurredAt = time.Time{}
	payload := struct {
		SchemaVersion      int                         `json:"schema_version"`
		Kind               RetirementEventKind         `json:"kind"`
		RetirementRecordID string                      `json:"retirement_record_id"`
		Header             *RetirementRecordHeader     `json:"header,omitempty"`
		PrepareManifest    *RetirementPrepareManifest  `json:"prepare_manifest,omitempty"`
		FinalizeEvent      *RetirementFinalizeEvent    `json:"finalize_event,omitempty"`
		OperationSummary   *RetirementOperationSummary `json:"operation_summary,omitempty"`
	}{
		SchemaVersion:      event.SchemaVersion,
		Kind:               event.Kind,
		RetirementRecordID: event.RetirementRecordID,
		Header:             event.Header,
		PrepareManifest:    event.PrepareManifest,
		FinalizeEvent:      event.FinalizeEvent,
		OperationSummary:   event.OperationSummary,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%s", hex.EncodeToString(sum[:])), nil
}

func RetirementPrepareManifestDigest(manifest *RetirementPrepareManifest) (string, error) {
	if manifest == nil {
		return "", errors.New("missing manifest")
	}
	if err := normalizeRetirementPrepareManifest(manifest); err != nil {
		return "", err
	}
	masked := *manifest
	masked.ManifestDigest = ""
	serialized, err := json.Marshal(masked)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(serialized)
	return fmt.Sprintf("sha256:%s", hex.EncodeToString(sum[:])), nil
}

func priorEventEqual(a, b RetirementStreamEvent) bool {
	aDigest, errA := retirementStreamEventPayloadID(a)
	bDigest, errB := retirementStreamEventPayloadID(b)
	if errA != nil || errB != nil {
		return false
	}
	return aDigest == bDigest
}
