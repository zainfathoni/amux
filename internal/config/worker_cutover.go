package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	WorkersSchemaV1 = "workers/v1"
	WorkersSchemaV2 = "workers/v2"

	WorkerCutoverManifestFile          = "worker-cutover.json"
	WorkerCutoverManifestSchemaVersion = 1
	WorkerCutoverManifestKind          = "amux-worker-cutover"

	workerSchemaHeaderV1 = "# amux-schema: " + WorkersSchemaV1
	workerSchemaHeaderV2 = "# amux-schema: " + WorkersSchemaV2

	workerCutoverControlWorkspace = "@amux-control"
	workerCutoverControlWindow    = WorkersSchemaV2
	workerCutoverControlWorkdir   = "downgrade-fence"
)

var workerCutoverGenerationPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

type WorkerCutoverRow struct {
	Workspace       string                `json:"workspace"`
	Window          string                `json:"window"`
	Workdir         string                `json:"workdir"`
	Thread          string                `json:"thread"`
	AssignmentState WorkerAssignmentState `json:"assignment_state,omitempty"`
}

type WorkerCutoverAdoption struct {
	Key            string         `json:"key"`
	Kind           string         `json:"kind"`
	RequestHash    string         `json:"request_hash"`
	Thread         string         `json:"thread"`
	StateAtCutover OperationState `json:"state_at_cutover"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type WorkerCutoverWorkers struct {
	SourceSchema  string             `json:"source_schema"`
	FencedSchema  string             `json:"fenced_schema"`
	SourcePresent bool               `json:"source_present"`
	SourceSHA256  string             `json:"source_sha256"`
	SourceMode    string             `json:"source_mode"`
	Rows          []WorkerCutoverRow `json:"rows"`
}

// SpawnCutoverReference pins the exact phase-1 provenance interface without
// copying spawn records into the worker-family artifact.
type SpawnCutoverReference struct {
	Generation                   string `json:"generation"`
	AssignmentFile               string `json:"assignment_file"`
	LegacyRecordSchemaVersion    int    `json:"legacy_record_schema_version"`
	ExceptionRecordSchemaVersion int    `json:"exception_record_schema_version"`
	ExceptionAdmission           string `json:"exception_admission"`
}

type WorkerCutoverManifest struct {
	SchemaVersion      int                     `json:"schema_version"`
	Kind               string                  `json:"kind"`
	Generation         string                  `json:"generation"`
	Workers            WorkerCutoverWorkers    `json:"workers"`
	AdoptionOperations []WorkerCutoverAdoption `json:"adoption_operations"`
	SpawnCutover       SpawnCutoverReference   `json:"spawn_cutover_reference"`
}

type WorkerCutoverWorkerClassification struct {
	WorkerCutoverRow
	Classification string `json:"classification"`
}

type WorkerCutoverAdoptionClassification struct {
	Key            string         `json:"key"`
	RequestHash    string         `json:"request_hash"`
	Thread         string         `json:"thread"`
	StateAtCutover OperationState `json:"state_at_cutover,omitempty"`
	CurrentState   OperationState `json:"current_state,omitempty"`
	Classification string         `json:"classification"`
}

type WorkerCutoverRegistryStatus struct {
	Path          string `json:"path"`
	Schema        string `json:"schema"`
	FenceSHA256   string `json:"fence_sha256,omitempty"`
	Mode          string `json:"mode,omitempty"`
	SourcePresent bool   `json:"source_present"`
}

type WorkerCutoverExport struct {
	SchemaVersion      int                                   `json:"schema_version"`
	State              string                                `json:"state"`
	Generation         string                                `json:"generation,omitempty"`
	ManifestPath       string                                `json:"manifest_path"`
	ManifestSHA256     string                                `json:"manifest_sha256,omitempty"`
	Registry           WorkerCutoverRegistryStatus           `json:"registry"`
	Manifest           *WorkerCutoverManifest                `json:"manifest,omitempty"`
	Workers            []WorkerCutoverWorkerClassification   `json:"workers"`
	AdoptionOperations []WorkerCutoverAdoptionClassification `json:"adoption_operations"`
	Blockers           []string                              `json:"blockers"`
}

type WorkerCutoverPublishAction string

const (
	WorkerCutoverPublishNew       WorkerCutoverPublishAction = "publish"
	WorkerCutoverRecoverFence     WorkerCutoverPublishAction = "recover_fence"
	WorkerCutoverPublishDuplicate WorkerCutoverPublishAction = "duplicate"
)

type WorkerCutoverPublication struct {
	Action WorkerCutoverPublishAction
	Export WorkerCutoverExport
}

func (d Directory) WorkerCutoverManifestPath() string {
	return filepath.Join(d.Path, WorkerCutoverManifestFile)
}

func validateWorkerCutoverGeneration(generation string) error {
	if !workerCutoverGenerationPattern.MatchString(generation) {
		return errors.New("worker cutover generation must match [a-z0-9][a-z0-9._-]{0,127}")
	}
	return nil
}

func workerCutoverControlLine(digest string) string {
	return strings.Join([]string{workerCutoverControlWorkspace, workerCutoverControlWindow, workerCutoverControlWorkdir, digest}, "\t")
}

func isWorkerCutoverControlCandidate(line string) bool {
	fields := strings.Split(line, "\t")
	return len(fields) > 0 && strings.EqualFold(fields[0], workerCutoverControlWorkspace)
}

func parseWorkerCutoverControlLine(line string) (string, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 4 || fields[0] != workerCutoverControlWorkspace || fields[1] != workerCutoverControlWindow || fields[2] != workerCutoverControlWorkdir {
		return "", errors.New("control row must be the exact canonical workers/v2 downgrade-fence row")
	}
	if err := validateSHA256(fields[3]); err != nil {
		return "", fmt.Errorf("manifest digest: %w", err)
	}
	return fields[3], nil
}

func validateSHA256(value string) error {
	hexDigest := strings.TrimPrefix(value, "sha256:")
	if hexDigest == value || len(hexDigest) != sha256.Size*2 {
		return errors.New("must be sha256 followed by 64 lowercase hexadecimal characters")
	}
	decoded, err := hex.DecodeString(hexDigest)
	if err != nil || hex.EncodeToString(decoded) != hexDigest {
		return errors.New("must be sha256 followed by 64 lowercase hexadecimal characters")
	}
	return nil
}

func sha256Digest(data []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}

func canonicalWorkerCutoverManifestBytes(manifest WorkerCutoverManifest) ([]byte, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func spawnCutoverReference() SpawnCutoverReference {
	return SpawnCutoverReference{
		Generation:                   SpawnCutoverGeneration,
		AssignmentFile:               SpawnAssignmentsFile,
		LegacyRecordSchemaVersion:    SpawnAssignmentSchemaVersion,
		ExceptionRecordSchemaVersion: SpawnAssignmentProjectlessHostSchemaVersion,
		ExceptionAdmission:           SpawnAssignmentProjectlessHostAdmission,
	}
}

type workerRegistrySnapshot struct {
	data     []byte
	document workerRegistryDocument
	present  bool
	mode     os.FileMode
}

func readWorkerRegistrySnapshot(path string, absentAsV1 bool) (workerRegistrySnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && absentAsV1 {
		data := []byte(defaultConfig)
		document, parseErr := parseWorkerRegistry(data, true)
		return workerRegistrySnapshot{data: data, document: document, mode: 0o600}, parseErr
	}
	if err != nil {
		return workerRegistrySnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return workerRegistrySnapshot{}, fmt.Errorf("workers registry %s must be a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return workerRegistrySnapshot{}, err
	}
	document, err := parseWorkerRegistry(data, true)
	if err != nil {
		return workerRegistrySnapshot{}, err
	}
	return workerRegistrySnapshot{data: data, document: document, present: true, mode: info.Mode().Perm()}, nil
}

func workerRowsForManifest(rows []Row) []WorkerCutoverRow {
	out := make([]WorkerCutoverRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, WorkerCutoverRow{Workspace: row.Workspace, Window: row.Window, Workdir: row.Workdir, Thread: row.Thread, AssignmentState: row.AssignmentState})
	}
	return out
}

func workerAdoptionsForManifest(operations []OperationRecord) []WorkerCutoverAdoption {
	out := make([]WorkerCutoverAdoption, 0)
	for _, operation := range operations {
		if operation.Kind != "worker-adopt" {
			continue
		}
		out = append(out, WorkerCutoverAdoption{
			Key: operation.Key, Kind: operation.Kind, RequestHash: operation.RequestHash,
			Thread: operation.Resource.Thread, StateAtCutover: operation.State,
			CreatedAt: operation.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt: operation.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func prepareWorkerCutoverManifest(dir Directory, generation string) (WorkerCutoverManifest, error) {
	if err := validateWorkerCutoverGeneration(generation); err != nil {
		return WorkerCutoverManifest{}, err
	}
	snapshot, err := readWorkerRegistrySnapshot(dir.WorkersPath(), true)
	if err != nil {
		return WorkerCutoverManifest{}, fmt.Errorf("read deterministic workers/v1 source: %w", err)
	}
	if snapshot.document.Schema != WorkersSchemaV1 || snapshot.document.FenceDigest != "" {
		return WorkerCutoverManifest{}, fmt.Errorf("worker cutover publication requires an unfenced %s registry, found %s", WorkersSchemaV1, snapshot.document.Schema)
	}
	operations, err := LoadOperationsReadOnly(dir.OperationsPath())
	if err != nil {
		return WorkerCutoverManifest{}, fmt.Errorf("read worker-adopt operations: %w", err)
	}
	manifest := WorkerCutoverManifest{
		SchemaVersion: WorkerCutoverManifestSchemaVersion,
		Kind:          WorkerCutoverManifestKind,
		Generation:    generation,
		Workers: WorkerCutoverWorkers{
			SourceSchema: WorkersSchemaV1, FencedSchema: WorkersSchemaV2,
			SourcePresent: snapshot.present, SourceSHA256: sha256Digest(snapshot.data),
			SourceMode: fmt.Sprintf("%04o", snapshot.mode.Perm()), Rows: workerRowsForManifest(snapshot.document.Rows),
		},
		AdoptionOperations: workerAdoptionsForManifest(operations),
		SpawnCutover:       spawnCutoverReference(),
	}
	if err := validateWorkerCutoverManifest(manifest); err != nil {
		return WorkerCutoverManifest{}, err
	}
	return manifest, nil
}

func validateWorkerCutoverManifest(manifest WorkerCutoverManifest) error {
	if manifest.SchemaVersion != WorkerCutoverManifestSchemaVersion || manifest.Kind != WorkerCutoverManifestKind {
		return errors.New("unsupported worker cutover manifest identity")
	}
	if err := validateWorkerCutoverGeneration(manifest.Generation); err != nil {
		return err
	}
	if manifest.Workers.SourceSchema != WorkersSchemaV1 || manifest.Workers.FencedSchema != WorkersSchemaV2 {
		return errors.New("worker cutover manifest must bind workers/v1 to workers/v2")
	}
	if manifest.Workers.Rows == nil || manifest.AdoptionOperations == nil {
		return errors.New("worker cutover rows and adoption_operations must be canonical arrays")
	}
	if err := validateSHA256(manifest.Workers.SourceSHA256); err != nil {
		return fmt.Errorf("invalid workers source digest: %w", err)
	}
	mode, err := strconv.ParseUint(manifest.Workers.SourceMode, 8, 32)
	if err != nil || manifest.Workers.SourceMode != fmt.Sprintf("%04o", os.FileMode(mode).Perm()) {
		return errors.New("worker cutover source mode must be four canonical octal permission digits")
	}
	seenWindows := make(map[string]bool)
	seenThreads := make(map[string]bool)
	for index, record := range manifest.Workers.Rows {
		row := Row{Workspace: record.Workspace, Window: record.Window, Workdir: record.Workdir, Thread: record.Thread, AssignmentState: record.AssignmentState}
		if err := row.Validate(); err != nil {
			return fmt.Errorf("invalid worker cutover row %d: %w", index+1, err)
		}
		canonicalThread, _ := CanonicalThreadID(row.Thread)
		if canonicalThread != row.Thread {
			return fmt.Errorf("worker cutover row %d must use the canonical thread ID", index+1)
		}
		windowKey := row.Workspace + "\x00" + row.Window
		if seenWindows[windowKey] || seenThreads[row.Thread] {
			return fmt.Errorf("duplicate worker identity in cutover row %d", index+1)
		}
		seenWindows[windowKey] = true
		seenThreads[row.Thread] = true
	}
	seenAdoptions := make(map[string]bool)
	for index, adoption := range manifest.AdoptionOperations {
		if adoption.Kind != "worker-adopt" || adoption.Key != "worker-adopt:"+adoption.Thread {
			return fmt.Errorf("invalid worker-adopt cutover identity %d", index+1)
		}
		if err := validateField("worker-adopt request hash", adoption.RequestHash); err != nil {
			return err
		}
		canonicalThread, err := CanonicalThreadID(adoption.Thread)
		if err != nil {
			return err
		}
		if canonicalThread != adoption.Thread {
			return fmt.Errorf("worker-adopt cutover identity %d must use the canonical thread ID", index+1)
		}
		if adoption.StateAtCutover != OperationStarted && adoption.StateAtCutover != OperationSucceeded && adoption.StateAtCutover != OperationFailed && adoption.StateAtCutover != OperationIndeterminate {
			return fmt.Errorf("invalid worker-adopt state %q", adoption.StateAtCutover)
		}
		created, createdErr := time.Parse(time.RFC3339Nano, adoption.CreatedAt)
		updated, updatedErr := time.Parse(time.RFC3339Nano, adoption.UpdatedAt)
		if createdErr != nil || updatedErr != nil || updated.Before(created) || created.UTC().Format(time.RFC3339Nano) != adoption.CreatedAt || updated.UTC().Format(time.RFC3339Nano) != adoption.UpdatedAt {
			return fmt.Errorf("invalid worker-adopt timestamps for %q", adoption.Key)
		}
		if seenAdoptions[adoption.Key] || index > 0 && manifest.AdoptionOperations[index-1].Key >= adoption.Key {
			return errors.New("worker-adopt cutover operations must be unique and sorted by key")
		}
		seenAdoptions[adoption.Key] = true
	}
	if manifest.SpawnCutover != spawnCutoverReference() {
		return errors.New("worker cutover manifest does not reference the exact phase-1 spawn provenance interface")
	}
	return nil
}

func loadWorkerCutoverManifest(path string) (WorkerCutoverManifest, []byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return WorkerCutoverManifest{}, nil, err
	}
	if !info.Mode().IsRegular() {
		return WorkerCutoverManifest{}, nil, fmt.Errorf("worker cutover manifest %s must be a regular file", path)
	}
	if info.Mode().Perm() != 0o600 {
		return WorkerCutoverManifest{}, nil, fmt.Errorf("worker cutover manifest %s must have mode 0600, found %04o", path, info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return WorkerCutoverManifest{}, nil, err
	}
	var manifest WorkerCutoverManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, data, fmt.Errorf("parse worker cutover manifest: %w", err)
	}
	if err := validateWorkerCutoverManifest(manifest); err != nil {
		return manifest, data, err
	}
	canonical, err := canonicalWorkerCutoverManifestBytes(manifest)
	if err != nil {
		return manifest, data, err
	}
	if !bytes.Equal(data, canonical) {
		return manifest, data, errors.New("worker cutover manifest is not its exact canonical immutable encoding")
	}
	return manifest, data, nil
}

func currentWorkerAdoptions(path string) ([]OperationRecord, error) {
	operations, err := LoadOperationsReadOnly(path)
	if err != nil {
		return nil, err
	}
	out := make([]OperationRecord, 0)
	for _, operation := range operations {
		if operation.Kind == "worker-adopt" {
			out = append(out, operation)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func immutableAdoptionMatches(manifest WorkerCutoverAdoption, current OperationRecord) bool {
	cutoverUpdatedAt, err := time.Parse(time.RFC3339Nano, manifest.UpdatedAt)
	return err == nil && current.Kind == manifest.Kind && current.Key == manifest.Key && current.RequestHash == manifest.RequestHash && current.Resource.Kind == "worker" && current.Resource.Thread == manifest.Thread && current.CreatedAt.UTC().Format(time.RFC3339Nano) == manifest.CreatedAt && !current.UpdatedAt.Before(cutoverUpdatedAt)
}

func validateFenceRecoveryAdoptions(manifest WorkerCutoverManifest, current []OperationRecord) error {
	if len(manifest.AdoptionOperations) != len(current) {
		return errors.New("worker-adopt operation set changed after manifest creation; downgrade fence recovery fails closed")
	}
	for index, expected := range manifest.AdoptionOperations {
		actual := current[index]
		if !immutableAdoptionMatches(expected, actual) || !operationTransitionAllowed(expected.StateAtCutover, actual.State) {
			return fmt.Errorf("worker-adopt operation %q mismatches its immutable cutover binding", expected.Key)
		}
	}
	return nil
}

func classifyWorkerCutover(manifest *WorkerCutoverManifest, rows []Row, operations []OperationRecord, published bool) ([]WorkerCutoverWorkerClassification, []WorkerCutoverAdoptionClassification, []string, error) {
	workers := make([]WorkerCutoverWorkerClassification, 0)
	adoptions := make([]WorkerCutoverAdoptionClassification, 0)
	blockers := make([]string, 0)
	if manifest == nil {
		for _, row := range rows {
			workers = append(workers, WorkerCutoverWorkerClassification{WorkerCutoverRow: workerRowsForManifest([]Row{row})[0], Classification: "not_yet_cut_over"})
		}
		for _, operation := range operations {
			adoptions = append(adoptions, WorkerCutoverAdoptionClassification{Key: operation.Key, RequestHash: operation.RequestHash, Thread: operation.Resource.Thread, CurrentState: operation.State, Classification: "not_yet_cut_over"})
		}
		return workers, adoptions, blockers, nil
	}
	currentByWindow := make(map[string]Row, len(rows))
	currentByThread := make(map[string]Row, len(rows))
	for _, row := range rows {
		currentByWindow[row.Workspace+"\x00"+row.Window] = row
		currentByThread[row.Thread] = row
	}
	seenRows := make(map[string]bool)
	for _, expected := range manifest.Workers.Rows {
		key := expected.Workspace + "\x00" + expected.Window
		current, present := currentByWindow[key]
		if !present {
			if conflict, exists := currentByThread[expected.Thread]; exists {
				return nil, nil, nil, fmt.Errorf("worker thread %s was rebound from %s/%s to %s/%s after cutover", expected.Thread, expected.Workspace, expected.Window, conflict.Workspace, conflict.Window)
			}
			workers = append(workers, WorkerCutoverWorkerClassification{WorkerCutoverRow: expected, Classification: "pre_cutover_absent"})
			continue
		}
		actual := workerRowsForManifest([]Row{current})[0]
		if actual != expected {
			return nil, nil, nil, fmt.Errorf("worker %s/%s mismatches its immutable cutover row", expected.Workspace, expected.Window)
		}
		seenRows[key] = true
		workers = append(workers, WorkerCutoverWorkerClassification{WorkerCutoverRow: expected, Classification: "pre_cutover_present"})
	}
	for _, row := range rows {
		key := row.Workspace + "\x00" + row.Window
		if seenRows[key] {
			continue
		}
		classification := "not_yet_cut_over"
		if published {
			classification = "not_in_pre_cutover_manifest"
			blockers = append(blockers, "worker:"+row.Thread+":not_in_pre_cutover_manifest")
		}
		workers = append(workers, WorkerCutoverWorkerClassification{WorkerCutoverRow: workerRowsForManifest([]Row{row})[0], Classification: classification})
	}
	currentByKey := make(map[string]OperationRecord, len(operations))
	for _, operation := range operations {
		currentByKey[operation.Key] = operation
	}
	seenOperations := make(map[string]bool)
	for _, expected := range manifest.AdoptionOperations {
		actual, present := currentByKey[expected.Key]
		classification := WorkerCutoverAdoptionClassification{Key: expected.Key, RequestHash: expected.RequestHash, Thread: expected.Thread, StateAtCutover: expected.StateAtCutover, Classification: "pre_cutover_absent"}
		if present {
			if !immutableAdoptionMatches(expected, actual) || !operationTransitionAllowed(expected.StateAtCutover, actual.State) {
				return nil, nil, nil, fmt.Errorf("worker-adopt operation %q mismatches its immutable cutover binding", expected.Key)
			}
			classification.CurrentState = actual.State
			classification.Classification = "pre_cutover_present"
			seenOperations[expected.Key] = true
		}
		adoptions = append(adoptions, classification)
	}
	for _, operation := range operations {
		if seenOperations[operation.Key] {
			continue
		}
		classification := "not_yet_cut_over"
		if published {
			classification = "not_in_pre_cutover_manifest"
			blockers = append(blockers, "worker-adopt:"+operation.Key+":not_in_pre_cutover_manifest")
		}
		adoptions = append(adoptions, WorkerCutoverAdoptionClassification{Key: operation.Key, RequestHash: operation.RequestHash, Thread: operation.Resource.Thread, CurrentState: operation.State, Classification: classification})
	}
	sort.Slice(workers, func(i, j int) bool {
		left := workers[i].Workspace + "\x00" + workers[i].Window + "\x00" + workers[i].Thread + "\x00" + workers[i].Classification
		right := workers[j].Workspace + "\x00" + workers[j].Window + "\x00" + workers[j].Thread + "\x00" + workers[j].Classification
		return left < right
	})
	sort.Slice(adoptions, func(i, j int) bool { return adoptions[i].Key < adoptions[j].Key })
	sort.Strings(blockers)
	return workers, adoptions, blockers, nil
}

// InspectWorkerCutover is a strictly read-only compatibility export. It reads
// only workers.tsv, worker-adopt operation evidence, and the cutover manifest.
func InspectWorkerCutover(dir Directory) (WorkerCutoverExport, error) {
	export := WorkerCutoverExport{
		SchemaVersion:      WorkerCutoverManifestSchemaVersion,
		State:              "not_published",
		ManifestPath:       dir.WorkerCutoverManifestPath(),
		Registry:           WorkerCutoverRegistryStatus{Path: dir.WorkersPath()},
		Workers:            make([]WorkerCutoverWorkerClassification, 0),
		AdoptionOperations: make([]WorkerCutoverAdoptionClassification, 0),
		Blockers:           make([]string, 0),
	}
	manifest, manifestData, manifestErr := loadWorkerCutoverManifest(dir.WorkerCutoverManifestPath())
	manifestPresent := manifestErr == nil
	if manifestErr != nil && !errors.Is(manifestErr, os.ErrNotExist) {
		return export, manifestErr
	}
	snapshot, registryErr := readWorkerRegistrySnapshot(dir.WorkersPath(), !manifestPresent)
	if registryErr != nil {
		if errors.Is(registryErr, os.ErrNotExist) && manifestPresent && !manifest.Workers.SourcePresent {
			snapshot, registryErr = readWorkerRegistrySnapshot(dir.WorkersPath(), true)
		} else {
			return export, registryErr
		}
	}
	if registryErr != nil {
		return export, registryErr
	}
	export.Registry.Schema = snapshot.document.Schema
	export.Registry.FenceSHA256 = snapshot.document.FenceDigest
	export.Registry.Mode = fmt.Sprintf("%04o", snapshot.mode.Perm())
	export.Registry.SourcePresent = snapshot.present
	operations, err := currentWorkerAdoptions(dir.OperationsPath())
	if err != nil {
		return export, fmt.Errorf("read worker-adopt operations: %w", err)
	}
	if !manifestPresent {
		if snapshot.document.Schema != WorkersSchemaV1 || snapshot.document.FenceDigest != "" {
			return export, errors.New("workers/v2 downgrade fence exists without its immutable worker cutover manifest")
		}
		export.Workers, export.AdoptionOperations, export.Blockers, err = classifyWorkerCutover(nil, snapshot.document.Rows, operations, false)
		return export, err
	}
	export.Generation = manifest.Generation
	export.Manifest = &manifest
	export.ManifestSHA256 = sha256Digest(manifestData)
	switch snapshot.document.Schema {
	case WorkersSchemaV1:
		if snapshot.present != manifest.Workers.SourcePresent || sha256Digest(snapshot.data) != manifest.Workers.SourceSHA256 || fmt.Sprintf("%04o", snapshot.mode.Perm()) != manifest.Workers.SourceMode {
			return export, errors.New("workers/v1 source mismatches the immutable worker cutover manifest in presence, content, or mode; downgrade fence recovery fails closed")
		}
		if err := validateFenceRecoveryAdoptions(manifest, operations); err != nil {
			return export, err
		}
		export.State = "recovery_required"
	case WorkersSchemaV2:
		if snapshot.document.FenceDigest != export.ManifestSHA256 {
			return export, errors.New("workers/v2 downgrade fence does not match the immutable worker cutover manifest")
		}
		export.State = "published"
	default:
		return export, fmt.Errorf("unsupported workers registry schema %q", snapshot.document.Schema)
	}
	export.Workers, export.AdoptionOperations, export.Blockers, err = classifyWorkerCutover(&manifest, snapshot.document.Rows, operations, export.State == "published")
	return export, err
}

func PlanWorkerCutover(dir Directory, generation string) (WorkerCutoverPublication, error) {
	if err := validateWorkerCutoverGeneration(generation); err != nil {
		return WorkerCutoverPublication{}, err
	}
	inspected, err := InspectWorkerCutover(dir)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	if inspected.Manifest != nil {
		if inspected.Generation != generation {
			return WorkerCutoverPublication{}, fmt.Errorf("worker cutover is immutably bound to generation %q, not %q", inspected.Generation, generation)
		}
		action := WorkerCutoverPublishDuplicate
		if inspected.State == "recovery_required" {
			action = WorkerCutoverRecoverFence
		}
		return WorkerCutoverPublication{Action: action, Export: inspected}, nil
	}
	manifest, err := prepareWorkerCutoverManifest(dir, generation)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	manifestData, err := canonicalWorkerCutoverManifestBytes(manifest)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	inspected.Generation = generation
	inspected.Manifest = &manifest
	inspected.ManifestSHA256 = sha256Digest(manifestData)
	return WorkerCutoverPublication{Action: WorkerCutoverPublishNew, Export: inspected}, nil
}

func writeImmutableWorkerCutoverManifest(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
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
	if err := os.Link(tmp, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func fencedWorkerRegistry(source []byte, manifestDigest string) ([]byte, error) {
	document, err := parseWorkerRegistry(source, true)
	if err != nil {
		return nil, err
	}
	if document.Schema != WorkersSchemaV1 || document.FenceDigest != "" {
		return nil, errors.New("only an exact unfenced workers/v1 registry can be fenced")
	}
	if strings.Count(string(source), workerSchemaHeaderV1) != 1 {
		return nil, errors.New("workers/v1 source must contain exactly one canonical schema header")
	}
	replacement := workerSchemaHeaderV2 + "\n" + workerCutoverControlLine(manifestDigest)
	return []byte(strings.Replace(string(source), workerSchemaHeaderV1, replacement, 1)), nil
}

func writeFencedWorkerRegistry(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(mode.Perm()); err != nil {
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
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func installWorkerCutoverFence(dir Directory, manifest WorkerCutoverManifest, manifestData []byte) error {
	snapshot, err := readWorkerRegistrySnapshot(dir.WorkersPath(), !manifest.Workers.SourcePresent)
	if err != nil {
		return err
	}
	manifestDigest := sha256Digest(manifestData)
	if snapshot.document.Schema == WorkersSchemaV2 {
		if snapshot.document.FenceDigest != manifestDigest {
			return errors.New("existing workers/v2 downgrade fence does not match the immutable manifest")
		}
		return nil
	}
	if snapshot.present != manifest.Workers.SourcePresent || sha256Digest(snapshot.data) != manifest.Workers.SourceSHA256 || fmt.Sprintf("%04o", snapshot.mode.Perm()) != manifest.Workers.SourceMode {
		return errors.New("workers/v1 source changed after immutable manifest creation; fence installation fails closed")
	}
	operations, err := currentWorkerAdoptions(dir.OperationsPath())
	if err != nil {
		return err
	}
	if err := validateFenceRecoveryAdoptions(manifest, operations); err != nil {
		return err
	}
	fenced, err := fencedWorkerRegistry(snapshot.data, manifestDigest)
	if err != nil {
		return err
	}
	return writeFencedWorkerRegistry(dir.WorkersPath(), fenced, snapshot.mode)
}

func PublishWorkerCutover(dir Directory, generation string) (WorkerCutoverPublication, error) {
	plan, err := PlanWorkerCutover(dir, generation)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	if plan.Action == WorkerCutoverPublishDuplicate {
		return plan, nil
	}
	var manifest WorkerCutoverManifest
	var manifestData []byte
	if plan.Action == WorkerCutoverPublishNew {
		manifest = *plan.Export.Manifest
		manifestData, err = canonicalWorkerCutoverManifestBytes(manifest)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
		// Rebuild immediately before the first durable write so stale plans never
		// turn changed worker or operation state into pre-cutover evidence.
		revalidated, err := prepareWorkerCutoverManifest(dir, generation)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
		revalidatedData, err := canonicalWorkerCutoverManifestBytes(revalidated)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
		if !bytes.Equal(manifestData, revalidatedData) {
			return WorkerCutoverPublication{}, errors.New("worker cutover inputs changed before immutable publication")
		}
		if err := writeImmutableWorkerCutoverManifest(dir.WorkerCutoverManifestPath(), manifestData); err != nil {
			return WorkerCutoverPublication{}, fmt.Errorf("durably publish immutable worker cutover manifest: %w", err)
		}
	} else {
		manifest, manifestData, err = loadWorkerCutoverManifest(dir.WorkerCutoverManifestPath())
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
	}
	if err := installWorkerCutoverFence(dir, manifest, manifestData); err != nil {
		return WorkerCutoverPublication{}, fmt.Errorf("durably install workers/v2 downgrade fence: %w", err)
	}
	inspected, err := InspectWorkerCutover(dir)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	return WorkerCutoverPublication{Action: plan.Action, Export: inspected}, nil
}
