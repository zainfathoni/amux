package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// SpawnAssignmentSchemaVersion is both the file schema and the legacy record
	// schema. A record at this version is proof that its admission predates the
	// native spawn cutover.
	SpawnAssignmentSchemaVersion = 1

	// SpawnAssignmentProjectlessHostSchemaVersion marks the sole post-cutover
	// admission route. Keeping it newer than the file schema makes older clients
	// reject rather than silently erase the exact-host binding.
	SpawnAssignmentProjectlessHostSchemaVersion = 2
	SpawnCutoverGeneration                      = "spawn-native-cutover-v1"
	SpawnAssignmentProjectlessHostAdmission     = SpawnCutoverGeneration + "/projectless-physical-host-exception"
)

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
	Admission     string                 `json:"admission,omitempty"`
	PhysicalHost  string                 `json:"physical_host,omitempty"`
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
	if err := validateSpawnAssignmentJSON(data); err != nil {
		return nil, fmt.Errorf("parse spawn assignments: %w", err)
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

type spawnJSONMember struct {
	name  string
	value json.RawMessage
}

func validateSpawnAssignmentJSON(data []byte) error {
	members, err := decodeSpawnJSONObject(data, "spawn assignment file")
	if err != nil {
		return err
	}
	var schema, assignments json.RawMessage
	for _, member := range members {
		canonical, provenance := canonicalSpawnProvenanceName(member.name)
		if provenance && member.name != canonical {
			return fmt.Errorf("non-canonical spawn provenance member %q; use %q", member.name, canonical)
		}
		if canonical, known := canonicalSpawnFileMemberName(member.name); known && member.name != canonical {
			return fmt.Errorf("non-canonical spawn assignment file member %q; use %q", member.name, canonical)
		}
		switch member.name {
		case "schema_version":
			schema = member.value
		case "assignments":
			assignments = member.value
		case "admission", "physical_host":
			return fmt.Errorf("spawn provenance member %q is valid only inside an assignment record", member.name)
		}
	}
	fileSchema, err := decodeSpawnJSONInt(schema, "file schema_version")
	if err != nil {
		return err
	}
	if fileSchema != SpawnAssignmentSchemaVersion {
		return fmt.Errorf("unsupported spawn assignment schema version %d", fileSchema)
	}
	if len(assignments) == 0 {
		return errors.New("missing assignments member")
	}
	var records []json.RawMessage
	if err := json.Unmarshal(assignments, &records); err != nil || len(bytes.TrimSpace(assignments)) == 0 || bytes.TrimSpace(assignments)[0] != '[' {
		if err == nil {
			err = errors.New("must be an array")
		}
		return fmt.Errorf("invalid assignments member: %w", err)
	}
	for i, raw := range records {
		if err := validateSpawnAssignmentProvenance(raw); err != nil {
			return fmt.Errorf("invalid spawn assignment %d provenance: %w", i+1, err)
		}
	}
	return nil
}

func validateSpawnAssignmentProvenance(data []byte) error {
	members, err := decodeSpawnJSONObject(data, "spawn assignment record")
	if err != nil {
		return err
	}
	provenance := make(map[string]json.RawMessage, 3)
	for _, member := range members {
		canonical, isProvenance := canonicalSpawnProvenanceName(member.name)
		if !isProvenance {
			if canonical, known := canonicalSpawnRecordMemberName(member.name); known && member.name != canonical {
				return fmt.Errorf("non-canonical spawn assignment record member %q; use %q", member.name, canonical)
			}
			continue
		}
		if member.name != canonical {
			return fmt.Errorf("non-canonical spawn provenance member %q; use %q", member.name, canonical)
		}
		provenance[canonical] = member.value
	}
	schema, err := decodeSpawnJSONInt(provenance["schema_version"], "record schema_version")
	if err != nil {
		return err
	}
	switch schema {
	case SpawnAssignmentSchemaVersion:
		for _, forbidden := range []string{"admission", "physical_host"} {
			if _, present := provenance[forbidden]; present {
				return fmt.Errorf("legacy schema-1 spawn assignment must not contain %q, including empty or null values", forbidden)
			}
		}
	case SpawnAssignmentProjectlessHostSchemaVersion:
		admission, err := decodeSpawnJSONString(provenance["admission"], "schema-2 admission")
		if err != nil {
			return err
		}
		if admission != SpawnAssignmentProjectlessHostAdmission {
			return fmt.Errorf("schema-2 admission must be exactly %q", SpawnAssignmentProjectlessHostAdmission)
		}
		host, err := decodeSpawnJSONString(provenance["physical_host"], "schema-2 physical_host")
		if err != nil {
			return err
		}
		if err := validateField("physical host", host); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported spawn assignment record schema version %d", schema)
	}
	return nil
}

func decodeSpawnJSONObject(data []byte, context string) ([]spawnJSONMember, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("%s must be a JSON object", context)
	}
	seen := make(map[string]bool)
	var members []spawnJSONMember
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("%s contains a non-string member name", context)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate JSON member %q in %s", name, context)
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		members = append(members, spawnJSONMember{name: name, value: value})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("trailing JSON data after %s: %v", context, token)
	}
	return members, nil
}

func canonicalSpawnProvenanceName(name string) (string, bool) {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(name)
	switch {
	case strings.EqualFold(normalized, "schemaversion"):
		return "schema_version", true
	case strings.EqualFold(normalized, "admission"):
		return "admission", true
	case strings.EqualFold(normalized, "physicalhost"):
		return "physical_host", true
	default:
		return "", false
	}
}

func canonicalSpawnFileMemberName(name string) (string, bool) {
	for _, canonical := range []string{"schema_version", "assignments"} {
		if strings.EqualFold(name, canonical) {
			return canonical, true
		}
	}
	return "", false
}

func canonicalSpawnRecordMemberName(name string) (string, bool) {
	for _, canonical := range []string{
		"schema_version", "admission", "physical_host", "workspace", "window", "workdir",
		"thread", "mode", "group", "prompt_digest", "phase", "assignment", "receipt_cursor",
	} {
		if strings.EqualFold(name, canonical) {
			return canonical, true
		}
	}
	return "", false
}

func decodeSpawnJSONInt(data json.RawMessage, name string) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("missing %s", name)
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return 0, fmt.Errorf("%s must not be null", name)
	}
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return value, nil
}

func decodeSpawnJSONString(data json.RawMessage, name string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("missing %s", name)
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return "", fmt.Errorf("%s must not be null", name)
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("%s must be a string: %w", name, err)
	}
	return value, nil
}

func StoreSpawnAssignment(path string, record SpawnAssignmentRecord) error {
	if record.SchemaVersion == 0 {
		record.SchemaVersion = SpawnAssignmentSchemaVersion
	}
	workdir, err := CanonicalWorkdir(record.Workdir)
	if err != nil {
		return err
	}
	record.Workdir = workdir
	if record.Thread != "" {
		thread, err := CanonicalThreadID(record.Thread)
		if err != nil {
			return err
		}
		record.Thread = thread
	}
	if err := record.Validate(); err != nil {
		return err
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
	if r.SchemaVersion != SpawnAssignmentSchemaVersion && r.SchemaVersion != SpawnAssignmentProjectlessHostSchemaVersion {
		return fmt.Errorf("unsupported spawn assignment schema version %d", r.SchemaVersion)
	}
	switch r.SchemaVersion {
	case SpawnAssignmentSchemaVersion:
		if r.Admission != "" || r.PhysicalHost != "" {
			return errors.New("legacy spawn assignment must not carry post-cutover admission fields")
		}
	case SpawnAssignmentProjectlessHostSchemaVersion:
		if r.Admission != SpawnAssignmentProjectlessHostAdmission {
			return fmt.Errorf("invalid post-cutover spawn admission %q", r.Admission)
		}
		if err := validateField("physical host", r.PhysicalHost); err != nil {
			return err
		}
		if r.Group != "" {
			return errors.New("projectless physical-host assignment must not carry group intent")
		}
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
	workdir, err := CanonicalWorkdir(r.Workdir)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(r.Workdir) || r.Workdir != workdir {
		return fmt.Errorf("spawn assignment workdir must be the canonical absolute path %s", workdir)
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
