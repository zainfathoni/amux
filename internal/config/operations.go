package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const OperationSchemaVersion = 1

type OperationState string
type OperationPhase string
type OperationMessageSource string
type OperationSubmissionStatus string
type OperationDeliveryStatus string

const (
	OperationStarted       OperationState = "started"
	OperationSucceeded     OperationState = "succeeded"
	OperationFailed        OperationState = "failed"
	OperationIndeterminate OperationState = "indeterminate"
)

const (
	OperationPhaseCreatingThread  OperationPhase = "creating_thread"
	OperationPhaseThreadBound     OperationPhase = "thread_bound"
	OperationPhaseRetryArmed      OperationPhase = "pre_submission_retry_armed"
	OperationPhaseDeliveryStarted OperationPhase = "delivery_started"
	OperationPhaseMessageVerified OperationPhase = "message_verified"
	OperationPhaseConfigured      OperationPhase = "configured"
	OperationPhaseGroupIntent     OperationPhase = "group_intent_persisted"
	OperationPhaseGrouped         OperationPhase = "grouped"
)

const (
	OperationMessageSourceMessage OperationMessageSource = "message"
	OperationMessageSourceFile    OperationMessageSource = "file"
	OperationMessageSourceStdin   OperationMessageSource = "stdin"
)

const (
	OperationSubmissionComposerUnavailable    OperationSubmissionStatus = "composer_unavailable"
	OperationSubmissionComposerCaptureUnknown OperationSubmissionStatus = "composer_capture_unknown"
	OperationSubmissionInputNotVisible        OperationSubmissionStatus = "input_not_visible"
	OperationSubmissionInputVisibilityUnknown OperationSubmissionStatus = "input_visibility_unknown"
	OperationSubmissionEnterAttempted         OperationSubmissionStatus = "enter_attempted"
	OperationSubmissionTypedOnly              OperationSubmissionStatus = "typed_only"
	OperationSubmissionTransitioned           OperationSubmissionStatus = "composer_transitioned"
	OperationSubmissionCaptureUnknown         OperationSubmissionStatus = "capture_unknown"
	OperationSubmissionError                  OperationSubmissionStatus = "submission_error"
)

const (
	OperationDeliveryPersisted         OperationDeliveryStatus = "persisted"
	OperationDeliveryAlternateReceiver OperationDeliveryStatus = "alternate_receiver"
	OperationDeliveryMissing           OperationDeliveryStatus = "missing"
	OperationDeliveryUnknown           OperationDeliveryStatus = "unknown"
)

const (
	OperationErrorPreSubmissionRetryArmed    = "pre-submission retry armed; Enter not attempted"
	OperationErrorPreSubmissionRetryConsumed = "pre-submission retry consumed; no further retry authorized"
)

type OperationResource struct {
	Kind    string `json:"kind"`
	Thread  string `json:"thread,omitempty"`
	Workdir string `json:"workdir,omitempty"`
}

type OperationThreadAdoption struct {
	ProvisionedThread string `json:"provisioned_thread"`
	ReceivingThread   string `json:"receiving_thread"`
}

type OperationRecord struct {
	SchemaVersion    int                       `json:"schema_version"`
	Key              string                    `json:"key"`
	Kind             string                    `json:"kind"`
	RequestHash      string                    `json:"request_hash"`
	MessageSource    OperationMessageSource    `json:"message_source,omitempty"`
	SubmissionStatus OperationSubmissionStatus `json:"submission_status,omitempty"`
	DeliveryStatus   OperationDeliveryStatus   `json:"delivery_status,omitempty"`
	State            OperationState            `json:"state"`
	Phase            OperationPhase            `json:"phase,omitempty"`
	Resource         OperationResource         `json:"resource"`
	ThreadAdoption   *OperationThreadAdoption  `json:"thread_adoption,omitempty"`
	Error            string                    `json:"error,omitempty"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
}

type operationFile struct {
	SchemaVersion int               `json:"schema_version"`
	Operations    []OperationRecord `json:"operations"`
}

func LoadOperation(path, key string) (OperationRecord, bool, error) {
	operations, err := loadOperations(path)
	if err != nil {
		return OperationRecord{}, false, err
	}
	for _, operation := range operations {
		if operation.Key == key {
			return operation, true, nil
		}
	}
	return OperationRecord{}, false, nil
}

func StoreOperation(path string, operation OperationRecord) (bool, error) {
	operation, err := canonicalOperation(operation)
	if err != nil {
		return false, err
	}
	operations, err := loadOperations(path)
	if err != nil {
		return false, err
	}
	created := true
	for i, existing := range operations {
		if existing.Key != operation.Key {
			continue
		}
		created = false
		if existing.Kind != operation.Kind || existing.RequestHash != operation.RequestHash || existing.MessageSource != operation.MessageSource || existing.Resource.Kind != operation.Resource.Kind {
			return false, fmt.Errorf("idempotency key %q is already bound to a different request", operation.Key)
		}
		if !existing.CreatedAt.Equal(operation.CreatedAt) {
			return false, fmt.Errorf("idempotency key %q cannot change its creation time", operation.Key)
		}
		if !operationTransitionAllowed(existing.State, operation.State) {
			return false, fmt.Errorf("idempotency key %q cannot transition from %s to %s", operation.Key, existing.State, operation.State)
		}
		if operationIdentity(existing.Resource) != "" && existing.Resource != operation.Resource {
			return false, fmt.Errorf("idempotency key %q resource identity cannot be rebound", operation.Key)
		}
		operations[i] = operation
		break
	}
	if created {
		operations = append(operations, operation)
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Key < operations[j].Key })
	return created, writeOperations(path, operations)
}

func BeginOperationThreadAdoption(path, key, provisionedThread, receivingThread string) (OperationRecord, error) {
	provisionedThread, err := CanonicalThreadID(provisionedThread)
	if err != nil {
		return OperationRecord{}, err
	}
	receivingThread, err = CanonicalThreadID(receivingThread)
	if err != nil {
		return OperationRecord{}, err
	}
	operations, err := loadOperations(path)
	if err != nil {
		return OperationRecord{}, err
	}
	for i, operation := range operations {
		if operation.Key != key {
			continue
		}
		if operation.Kind != "worker-spawn" || operation.State != OperationStarted || operation.Phase != OperationPhaseDeliveryStarted {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is not awaiting worker-spawn delivery verification", key)
		}
		if operation.Resource.Thread != provisionedThread {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is bound to thread %s, not provisioned thread %s", key, operation.Resource.Thread, provisionedThread)
		}
		if provisionedThread == receivingThread {
			return OperationRecord{}, fmt.Errorf("receiving thread %s is already bound to idempotency key %q", receivingThread, key)
		}
		if operation.ThreadAdoption != nil {
			return OperationRecord{}, fmt.Errorf("idempotency key %q already has thread-adoption evidence", key)
		}
		operation.ThreadAdoption = &OperationThreadAdoption{ProvisionedThread: provisionedThread, ReceivingThread: receivingThread}
		operation.UpdatedAt = time.Now().UTC()
		return writeOperationMutation(path, operations, i, operation)
	}
	return OperationRecord{}, fmt.Errorf("idempotency key %q was not found", key)
}

func BeginIndeterminateWorkerSpawnThreadAdoption(path, key, provisionedThread, receivingThread string) (OperationRecord, error) {
	provisionedThread, err := CanonicalThreadID(provisionedThread)
	if err != nil {
		return OperationRecord{}, err
	}
	receivingThread, err = CanonicalThreadID(receivingThread)
	if err != nil {
		return OperationRecord{}, err
	}
	operations, err := loadOperations(path)
	if err != nil {
		return OperationRecord{}, err
	}
	for i, operation := range operations {
		if operation.Key != key {
			continue
		}
		if operation.Kind != "worker-spawn" || operation.State != OperationIndeterminate || operation.Phase != OperationPhaseDeliveryStarted || operation.ThreadAdoption != nil {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is not an indeterminate provisioned worker-spawn delivery", key)
		}
		if operation.Resource.Thread != provisionedThread {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is bound to thread %s, not provisioned thread %s", key, operation.Resource.Thread, provisionedThread)
		}
		wantError := fmt.Sprintf("initial assignment was not found in provisioned thread %s or one unambiguous fresh receiving thread; recovery: inspect thread %s and do not resubmit", provisionedThread, provisionedThread)
		if operation.DeliveryStatus != OperationDeliveryMissing && (operation.DeliveryStatus != "" || operation.Error != wantError) {
			return OperationRecord{}, fmt.Errorf("idempotency key %q does not have the recoverable provisioned-thread verification failure", key)
		}
		if provisionedThread == receivingThread {
			return OperationRecord{}, fmt.Errorf("receiving thread %s is already bound to idempotency key %q", receivingThread, key)
		}
		operation.State = OperationStarted
		operation.Error = ""
		operation.ThreadAdoption = &OperationThreadAdoption{ProvisionedThread: provisionedThread, ReceivingThread: receivingThread}
		operation.UpdatedAt = time.Now().UTC()
		return writeOperationMutation(path, operations, i, operation)
	}
	return OperationRecord{}, fmt.Errorf("idempotency key %q was not found", key)
}

func CompleteOperationThreadAdoption(path, key string) (OperationRecord, error) {
	operations, err := loadOperations(path)
	if err != nil {
		return OperationRecord{}, err
	}
	for i, operation := range operations {
		if operation.Key != key {
			continue
		}
		adoption := operation.ThreadAdoption
		if operation.Kind != "worker-spawn" || operation.State != OperationStarted || operation.Phase != OperationPhaseDeliveryStarted || adoption == nil {
			return OperationRecord{}, fmt.Errorf("idempotency key %q has no pending worker-spawn thread adoption", key)
		}
		if operation.Resource.Thread != adoption.ProvisionedThread {
			return OperationRecord{}, fmt.Errorf("idempotency key %q no longer binds provisioned thread %s", key, adoption.ProvisionedThread)
		}
		operation.Resource.Thread = adoption.ReceivingThread
		operation.Phase = OperationPhaseMessageVerified
		if operation.SubmissionStatus != "" {
			operation.DeliveryStatus = OperationDeliveryAlternateReceiver
		}
		operation.UpdatedAt = time.Now().UTC()
		return writeOperationMutation(path, operations, i, operation)
	}
	return OperationRecord{}, fmt.Errorf("idempotency key %q was not found", key)
}

func RecoverIndeterminateWorkerSpawn(path, key, provisionedThread string) (OperationRecord, error) {
	provisionedThread, err := CanonicalThreadID(provisionedThread)
	if err != nil {
		return OperationRecord{}, err
	}
	operations, err := loadOperations(path)
	if err != nil {
		return OperationRecord{}, err
	}
	for i, operation := range operations {
		if operation.Key != key {
			continue
		}
		if operation.Kind != "worker-spawn" || operation.State != OperationIndeterminate || operation.Phase != OperationPhaseDeliveryStarted || operation.ThreadAdoption != nil {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is not an indeterminate provisioned worker-spawn delivery", key)
		}
		if operation.Resource.Thread != provisionedThread {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is bound to thread %s, not provisioned thread %s", key, operation.Resource.Thread, provisionedThread)
		}
		wantError := fmt.Sprintf("initial assignment was not found in provisioned thread %s or one unambiguous fresh receiving thread; recovery: inspect thread %s and do not resubmit", provisionedThread, provisionedThread)
		if operation.DeliveryStatus != OperationDeliveryMissing && (operation.DeliveryStatus != "" || operation.Error != wantError) {
			return OperationRecord{}, fmt.Errorf("idempotency key %q does not have the recoverable provisioned-thread verification failure", key)
		}
		operation.State = OperationStarted
		operation.Phase = OperationPhaseMessageVerified
		if operation.SubmissionStatus != "" {
			operation.DeliveryStatus = OperationDeliveryPersisted
		}
		operation.Error = ""
		operation.UpdatedAt = time.Now().UTC()
		return writeOperationMutation(path, operations, i, operation)
	}
	return OperationRecord{}, fmt.Errorf("idempotency key %q was not found", key)
}

func RetryPreSubmissionWorkerSpawn(path, key, requestHash, provisionedThread string, multiline bool) (OperationRecord, error) {
	if !multiline {
		return OperationRecord{}, errors.New("pre-submission worker spawn retry requires a multiline request")
	}
	provisionedThread, err := CanonicalThreadID(provisionedThread)
	if err != nil {
		return OperationRecord{}, err
	}
	operations, err := loadOperations(path)
	if err != nil {
		return OperationRecord{}, err
	}
	for i, operation := range operations {
		if operation.Key != key {
			continue
		}
		if operation.Kind != "worker-spawn" || operation.State != OperationIndeterminate || operation.Phase != OperationPhaseDeliveryStarted || operation.ThreadAdoption != nil {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is not an indeterminate pre-submission worker spawn", key)
		}
		if operation.Resource.Thread != provisionedThread {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is bound to thread %s, not provisioned thread %s", key, operation.Resource.Thread, provisionedThread)
		}
		if operation.RequestHash != requestHash {
			return OperationRecord{}, fmt.Errorf("idempotency key %q no longer matches the exact spawn request", key)
		}
		if !preSubmissionRetryStatus(operation.SubmissionStatus) || operation.DeliveryStatus != OperationDeliveryUnknown {
			return OperationRecord{}, fmt.Errorf("idempotency key %q does not prove that Enter was not attempted", key)
		}
		operation.State = OperationStarted
		operation.Phase = OperationPhaseRetryArmed
		operation.SubmissionStatus = ""
		operation.DeliveryStatus = ""
		operation.Error = OperationErrorPreSubmissionRetryArmed
		operation.UpdatedAt = time.Now().UTC()
		return writeOperationMutation(path, operations, i, operation)
	}
	return OperationRecord{}, fmt.Errorf("idempotency key %q was not found", key)
}

func ConsumePreSubmissionWorkerSpawnRetry(path, key, requestHash, provisionedThread string) (OperationRecord, error) {
	provisionedThread, err := CanonicalThreadID(provisionedThread)
	if err != nil {
		return OperationRecord{}, err
	}
	operations, err := loadOperations(path)
	if err != nil {
		return OperationRecord{}, err
	}
	for i, operation := range operations {
		if operation.Key != key {
			continue
		}
		if operation.Kind != "worker-spawn" || operation.State != OperationStarted || operation.Phase != OperationPhaseRetryArmed || operation.ThreadAdoption != nil || operation.SubmissionStatus != "" || operation.DeliveryStatus != "" || operation.Error != OperationErrorPreSubmissionRetryArmed {
			return OperationRecord{}, fmt.Errorf("idempotency key %q is not an armed pre-submission worker spawn retry", key)
		}
		if operation.Resource.Thread != provisionedThread || operation.RequestHash != requestHash {
			return OperationRecord{}, fmt.Errorf("idempotency key %q no longer matches the exact provisioned spawn request", key)
		}
		operation.Phase = OperationPhaseDeliveryStarted
		operation.Error = OperationErrorPreSubmissionRetryConsumed
		operation.UpdatedAt = time.Now().UTC()
		return writeOperationMutation(path, operations, i, operation)
	}
	return OperationRecord{}, fmt.Errorf("idempotency key %q was not found", key)
}

func preSubmissionRetryStatus(status OperationSubmissionStatus) bool {
	switch status {
	case OperationSubmissionComposerUnavailable, OperationSubmissionComposerCaptureUnknown, OperationSubmissionInputNotVisible, OperationSubmissionInputVisibilityUnknown:
		return true
	default:
		return false
	}
}

func writeOperationMutation(path string, operations []OperationRecord, index int, operation OperationRecord) (OperationRecord, error) {
	canonical, err := canonicalOperation(operation)
	if err != nil {
		return OperationRecord{}, err
	}
	operations[index] = canonical
	if err := writeOperations(path, operations); err != nil {
		return OperationRecord{}, err
	}
	return canonical, nil
}

func operationTransitionAllowed(from, to OperationState) bool {
	return from == to || from == OperationStarted
}

func canonicalOperation(operation OperationRecord) (OperationRecord, error) {
	if operation.SchemaVersion == 0 {
		operation.SchemaVersion = OperationSchemaVersion
	}
	if operation.SchemaVersion != OperationSchemaVersion {
		return operation, fmt.Errorf("unsupported operation schema version %d", operation.SchemaVersion)
	}
	if err := validateField("idempotency key", operation.Key); err != nil {
		return operation, err
	}
	if err := validateField("operation kind", operation.Kind); err != nil {
		return operation, err
	}
	if err := validateField("request hash", operation.RequestHash); err != nil {
		return operation, err
	}
	switch operation.State {
	case OperationStarted, OperationSucceeded, OperationFailed, OperationIndeterminate:
	default:
		return operation, fmt.Errorf("invalid operation state %q", operation.State)
	}
	switch operation.Phase {
	case "", OperationPhaseCreatingThread, OperationPhaseThreadBound, OperationPhaseRetryArmed, OperationPhaseDeliveryStarted, OperationPhaseMessageVerified, OperationPhaseConfigured, OperationPhaseGroupIntent, OperationPhaseGrouped:
	default:
		return operation, fmt.Errorf("invalid operation phase %q", operation.Phase)
	}
	if operation.Phase != "" && operation.Kind != "worker-spawn" {
		return operation, fmt.Errorf("operation phase %q is only valid for worker-spawn", operation.Phase)
	}
	switch operation.MessageSource {
	case "":
	case OperationMessageSourceMessage, OperationMessageSourceFile, OperationMessageSourceStdin:
		if operation.Kind != "worker-spawn" {
			return operation, errors.New("message source is only valid for worker-spawn")
		}
	default:
		return operation, fmt.Errorf("invalid operation message source %q", operation.MessageSource)
	}
	switch operation.SubmissionStatus {
	case "":
	case OperationSubmissionComposerUnavailable, OperationSubmissionComposerCaptureUnknown, OperationSubmissionInputNotVisible, OperationSubmissionInputVisibilityUnknown, OperationSubmissionEnterAttempted, OperationSubmissionTypedOnly, OperationSubmissionTransitioned, OperationSubmissionCaptureUnknown, OperationSubmissionError:
		if operation.Kind != "worker-spawn" || operation.Phase != OperationPhaseDeliveryStarted && operation.Phase != OperationPhaseMessageVerified && operation.Phase != OperationPhaseConfigured && operation.Phase != OperationPhaseGroupIntent && operation.Phase != OperationPhaseGrouped {
			return operation, errors.New("submission status is only valid after worker-spawn delivery starts")
		}
	default:
		return operation, fmt.Errorf("invalid operation submission status %q", operation.SubmissionStatus)
	}
	switch operation.DeliveryStatus {
	case "":
	case OperationDeliveryPersisted, OperationDeliveryAlternateReceiver, OperationDeliveryMissing, OperationDeliveryUnknown:
		if operation.Kind != "worker-spawn" || operation.Phase != OperationPhaseDeliveryStarted && operation.Phase != OperationPhaseMessageVerified && operation.Phase != OperationPhaseConfigured && operation.Phase != OperationPhaseGroupIntent && operation.Phase != OperationPhaseGrouped {
			return operation, errors.New("delivery status is only valid after worker-spawn delivery starts")
		}
	default:
		return operation, fmt.Errorf("invalid operation delivery status %q", operation.DeliveryStatus)
	}
	preSubmission := operation.SubmissionStatus == OperationSubmissionComposerUnavailable ||
		operation.SubmissionStatus == OperationSubmissionComposerCaptureUnknown ||
		operation.SubmissionStatus == OperationSubmissionInputNotVisible ||
		operation.SubmissionStatus == OperationSubmissionInputVisibilityUnknown ||
		operation.SubmissionStatus == OperationSubmissionError
	enteredSubmission := operation.SubmissionStatus == OperationSubmissionEnterAttempted ||
		operation.SubmissionStatus == OperationSubmissionTypedOnly ||
		operation.SubmissionStatus == OperationSubmissionTransitioned ||
		operation.SubmissionStatus == OperationSubmissionCaptureUnknown
	if preSubmission && operation.DeliveryStatus != "" && operation.DeliveryStatus != OperationDeliveryUnknown {
		return operation, errors.New("pre-submission status requires unknown delivery status")
	}
	if operation.DeliveryStatus == OperationDeliveryMissing || operation.DeliveryStatus == OperationDeliveryPersisted || operation.DeliveryStatus == OperationDeliveryAlternateReceiver {
		if !enteredSubmission {
			return operation, errors.New("delivery status requires submission evidence that Enter was attempted")
		}
	}
	if operation.Phase == OperationPhaseMessageVerified || operation.Phase == OperationPhaseConfigured || operation.Phase == OperationPhaseGroupIntent || operation.Phase == OperationPhaseGrouped {
		if operation.SubmissionStatus != "" && operation.DeliveryStatus != OperationDeliveryPersisted && operation.DeliveryStatus != OperationDeliveryAlternateReceiver {
			return operation, errors.New("verified delivery phase requires persisted or alternate-receiver delivery status")
		}
	}
	if operation.ThreadAdoption != nil {
		if operation.Kind != "worker-spawn" || operation.Phase != OperationPhaseDeliveryStarted && operation.Phase != OperationPhaseMessageVerified && operation.Phase != OperationPhaseConfigured && operation.Phase != OperationPhaseGroupIntent && operation.Phase != OperationPhaseGrouped {
			return operation, errors.New("thread adoption is only valid after worker-spawn delivery starts")
		}
		provisioned, err := CanonicalThreadID(operation.ThreadAdoption.ProvisionedThread)
		if err != nil {
			return operation, fmt.Errorf("invalid provisioned adoption thread: %w", err)
		}
		receiving, err := CanonicalThreadID(operation.ThreadAdoption.ReceivingThread)
		if err != nil {
			return operation, fmt.Errorf("invalid receiving adoption thread: %w", err)
		}
		if provisioned == receiving {
			return operation, errors.New("thread adoption requires different provisioned and receiving identities")
		}
		operation.ThreadAdoption.ProvisionedThread = provisioned
		operation.ThreadAdoption.ReceivingThread = receiving
		if operation.Resource.Thread != provisioned && operation.Resource.Thread != receiving {
			return operation, errors.New("worker operation resource must match one thread-adoption identity")
		}
	}
	if operation.Error == OperationErrorPreSubmissionRetryArmed && (operation.Kind != "worker-spawn" || operation.State != OperationStarted || operation.Phase != OperationPhaseRetryArmed || operation.SubmissionStatus != "" || operation.DeliveryStatus != "" || operation.ThreadAdoption != nil || operation.Resource.Thread == "") {
		return operation, errors.New("armed pre-submission retry has inconsistent operation evidence")
	}
	if operation.Error == OperationErrorPreSubmissionRetryConsumed && (operation.Kind != "worker-spawn" || operation.State != OperationStarted && operation.State != OperationIndeterminate || operation.Phase != OperationPhaseDeliveryStarted || operation.ThreadAdoption != nil || operation.Resource.Thread == "") {
		return operation, errors.New("consumed pre-submission retry has inconsistent operation evidence")
	}
	switch operation.Resource.Kind {
	case "worker":
		if operation.Resource.Workdir != "" {
			return operation, errors.New("worker operation resource must not include workdir")
		}
		if operation.Resource.Thread != "" {
			thread, err := CanonicalThreadID(operation.Resource.Thread)
			if err != nil {
				return operation, err
			}
			operation.Resource.Thread = thread
		}
	case "runner":
		if operation.Resource.Thread != "" {
			return operation, errors.New("runner operation resource must not include thread")
		}
		if operation.Resource.Workdir != "" {
			workdir, err := CanonicalWorkdir(operation.Resource.Workdir)
			if err != nil {
				return operation, err
			}
			operation.Resource.Workdir = workdir
		}
	default:
		return operation, fmt.Errorf("invalid operation resource kind %q", operation.Resource.Kind)
	}
	if operation.State == OperationSucceeded && operationIdentity(operation.Resource) == "" {
		return operation, errors.New("successful operation requires a canonical resource identity")
	}
	if operation.CreatedAt.IsZero() || operation.UpdatedAt.IsZero() {
		return operation, errors.New("operation timestamps are required")
	}
	if operation.UpdatedAt.Before(operation.CreatedAt) {
		return operation, errors.New("operation updated_at must not precede created_at")
	}
	return operation, nil
}

func operationIdentity(resource OperationResource) string {
	if resource.Kind == "worker" {
		return resource.Thread
	}
	return resource.Workdir
}

func loadOperations(path string) ([]OperationRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file operationFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse operation records: %w", err)
	}
	if file.SchemaVersion != OperationSchemaVersion {
		return nil, fmt.Errorf("unsupported operations file schema version %d", file.SchemaVersion)
	}
	seen := make(map[string]bool)
	for i, operation := range file.Operations {
		canonical, err := canonicalOperation(operation)
		if err != nil {
			return nil, fmt.Errorf("invalid operation record %d: %w", i+1, err)
		}
		if seen[canonical.Key] {
			return nil, fmt.Errorf("duplicate idempotency key %q", canonical.Key)
		}
		seen[canonical.Key] = true
		file.Operations[i] = canonical
	}
	return file.Operations, nil
}

func writeOperations(path string, operations []OperationRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(operationFile{SchemaVersion: OperationSchemaVersion, Operations: operations})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}
