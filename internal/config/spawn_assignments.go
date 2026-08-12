package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const SpawnAssignmentSchemaVersion = 1

type SpawnAssignmentPhase string
type SpawnAssignmentOutcome string

const (
	SpawnAssignmentCreationArmed SpawnAssignmentPhase = "creation_armed"
	SpawnAssignmentPrepared      SpawnAssignmentPhase = "prepared"
	SpawnAssignmentArmed         SpawnAssignmentPhase = "armed"
	SpawnAssignmentFinalized     SpawnAssignmentPhase = "finalized"
)

const (
	SpawnAssignmentNotAttempted          SpawnAssignmentOutcome = "not_attempted"
	SpawnAssignmentRejected              SpawnAssignmentOutcome = "rejected"
	SpawnAssignmentIndeterminate         SpawnAssignmentOutcome = "indeterminate"
	SpawnAssignmentAuthenticatedAccepted SpawnAssignmentOutcome = "authenticated_accepted"
)

type SpawnAssignmentRecord struct {
	SchemaVersion int                    `json:"schema_version"`
	Workspace     string                 `json:"workspace"`
	Window        string                 `json:"window"`
	Workdir       string                 `json:"workdir"`
	Thread        string                 `json:"thread,omitempty"`
	Mode          string                 `json:"mode"`
	Group         string                 `json:"group,omitempty"`
	PromptDigest  string                 `json:"prompt_digest"`
	Phase         SpawnAssignmentPhase   `json:"phase"`
	Outcome       SpawnAssignmentOutcome `json:"assignment"`
	ReceiptCursor string                 `json:"receipt_cursor,omitempty"`
}

type spawnAssignmentFile struct {
	SchemaVersion int                     `json:"schema_version"`
	Assignments   []SpawnAssignmentRecord `json:"assignments"`
}

func LoadSpawnAssignments(path string) ([]SpawnAssignmentRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file spawnAssignmentFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse spawn assignments: %w", err)
	}
	if file.SchemaVersion != SpawnAssignmentSchemaVersion {
		return nil, fmt.Errorf("unsupported spawn assignment schema version %d", file.SchemaVersion)
	}
	seen := make(map[string]bool, len(file.Assignments))
	for i := range file.Assignments {
		if err := file.Assignments[i].Validate(); err != nil {
			return nil, fmt.Errorf("invalid spawn assignment %d: %w", i+1, err)
		}
		key := file.Assignments[i].Workspace + "\x00" + file.Assignments[i].Window
		if seen[key] {
			return nil, fmt.Errorf("duplicate spawn assignment %s/%s", file.Assignments[i].Workspace, file.Assignments[i].Window)
		}
		seen[key] = true
	}
	return file.Assignments, nil
}

func StoreSpawnAssignment(path string, record SpawnAssignmentRecord) error {
	if record.SchemaVersion == 0 {
		record.SchemaVersion = SpawnAssignmentSchemaVersion
	}
	if err := record.Validate(); err != nil {
		return err
	}
	record.Workdir, _ = CanonicalWorkdir(record.Workdir)
	if record.Thread != "" {
		record.Thread, _ = CanonicalThreadID(record.Thread)
	}
	records, err := LoadSpawnAssignments(path)
	if err != nil {
		return err
	}
	found := false
	for i := range records {
		if records[i].Workspace == record.Workspace && records[i].Window == record.Window {
			records[i] = record
			found = true
			break
		}
	}
	if !found {
		records = append(records, record)
	}
	return writeSpawnAssignments(path, records)
}

func (r SpawnAssignmentRecord) Validate() error {
	if r.SchemaVersion != 0 && r.SchemaVersion != SpawnAssignmentSchemaVersion {
		return fmt.Errorf("unsupported spawn assignment schema version %d", r.SchemaVersion)
	}
	for name, value := range map[string]string{"workspace": r.Workspace, "window": r.Window, "workdir": r.Workdir, "mode": r.Mode, "prompt digest": r.PromptDigest} {
		if err := validateField(name, value); err != nil {
			return err
		}
	}
	digest := strings.TrimPrefix(r.PromptDigest, "sha256:")
	if len(digest) != sha256.Size*2 || digest == r.PromptDigest {
		return errors.New("prompt digest must be sha256 followed by 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return errors.New("prompt digest must be sha256 followed by 64 hexadecimal characters")
	}
	if _, err := CanonicalWorkdir(r.Workdir); err != nil {
		return err
	}
	if r.Group != "" {
		if err := ValidateGroupID(r.Group); err != nil {
			return err
		}
	}
	switch r.Phase {
	case SpawnAssignmentCreationArmed:
		if r.Thread != "" || r.Outcome != SpawnAssignmentNotAttempted || r.ReceiptCursor != "" {
			return errors.New("creation-armed assignment has inconsistent evidence")
		}
	case SpawnAssignmentPrepared:
		if r.Outcome != SpawnAssignmentNotAttempted || r.ReceiptCursor != "" {
			return errors.New("prepared assignment has inconsistent evidence")
		}
	case SpawnAssignmentArmed:
		if r.Outcome != SpawnAssignmentIndeterminate || r.ReceiptCursor != "" {
			return errors.New("armed assignment must durably remain indeterminate")
		}
	case SpawnAssignmentFinalized:
		switch r.Outcome {
		case SpawnAssignmentRejected, SpawnAssignmentIndeterminate:
			if r.ReceiptCursor != "" {
				return errors.New("non-accepted assignment must not carry an acceptance cursor")
			}
		case SpawnAssignmentAuthenticatedAccepted:
			if r.ReceiptCursor == "" {
				return errors.New("authenticated acceptance requires latestCursor")
			}
		default:
			return fmt.Errorf("invalid finalized assignment outcome %q", r.Outcome)
		}
	default:
		return fmt.Errorf("invalid spawn assignment phase %q", r.Phase)
	}
	if r.Phase != SpawnAssignmentCreationArmed {
		if _, err := CanonicalThreadID(r.Thread); err != nil {
			return err
		}
	}
	return nil
}

func writeSpawnAssignments(path string, records []SpawnAssignmentRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(spawnAssignmentFile{SchemaVersion: SpawnAssignmentSchemaVersion, Assignments: records})
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
	return os.Rename(tmp, path)
}
