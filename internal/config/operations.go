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

const (
	OperationStarted       OperationState = "started"
	OperationSucceeded     OperationState = "succeeded"
	OperationFailed        OperationState = "failed"
	OperationIndeterminate OperationState = "indeterminate"
)

type OperationResource struct {
	Kind    string `json:"kind"`
	Thread  string `json:"thread,omitempty"`
	Workdir string `json:"workdir,omitempty"`
}

type OperationRecord struct {
	SchemaVersion int               `json:"schema_version"`
	Key           string            `json:"key"`
	Kind          string            `json:"kind"`
	RequestHash   string            `json:"request_hash"`
	State         OperationState    `json:"state"`
	Resource      OperationResource `json:"resource"`
	Error         string            `json:"error,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
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
		if existing.Kind != operation.Kind || existing.RequestHash != operation.RequestHash || existing.Resource.Kind != operation.Resource.Kind {
			return false, fmt.Errorf("idempotency key %q is already bound to a different request", operation.Key)
		}
		if !existing.CreatedAt.Equal(operation.CreatedAt) {
			return false, fmt.Errorf("idempotency key %q cannot change its creation time", operation.Key)
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
