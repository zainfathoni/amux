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
	file     workerCutoverFileSnapshot
}

func readWorkerRegistrySnapshotPinned(directory *workerCutoverDirectory, name string, absentAsV1 bool) (workerRegistrySnapshot, error) {
	file, err := directory.readRegularFile(name, "workers registry", absentAsV1, nil)
	if err != nil {
		return workerRegistrySnapshot{}, err
	}
	if !file.present && absentAsV1 {
		data := []byte(defaultConfig)
		document, parseErr := parseWorkerRegistry(data, true)
		return workerRegistrySnapshot{data: data, document: document, mode: 0o600, file: file}, parseErr
	}
	document, err := parseWorkerRegistry(file.data, true)
	if err != nil {
		return workerRegistrySnapshot{}, err
	}
	return workerRegistrySnapshot{data: file.data, document: document, present: true, mode: file.mode.Perm(), file: file}, nil
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
	directory, err := openWorkerCutoverDirectory(dir.Path, false)
	if err != nil {
		return WorkerCutoverManifest{}, err
	}
	defer directory.Close()
	manifest, _, _, _, err := prepareWorkerCutoverManifestPinned(directory, generation)
	return manifest, err
}

func prepareWorkerCutoverManifestPinned(directory *workerCutoverDirectory, generation string) (WorkerCutoverManifest, workerRegistrySnapshot, []OperationRecord, workerCutoverFileSnapshot, error) {
	if err := validateWorkerCutoverGeneration(generation); err != nil {
		return WorkerCutoverManifest{}, workerRegistrySnapshot{}, nil, workerCutoverFileSnapshot{}, err
	}
	snapshot, err := readWorkerRegistrySnapshotPinned(directory, WorkersFile, true)
	if err != nil {
		return WorkerCutoverManifest{}, workerRegistrySnapshot{}, nil, workerCutoverFileSnapshot{}, fmt.Errorf("read deterministic workers/v1 source: %w", err)
	}
	if snapshot.document.Schema != WorkersSchemaV1 || snapshot.document.FenceDigest != "" {
		return WorkerCutoverManifest{}, workerRegistrySnapshot{}, nil, workerCutoverFileSnapshot{}, fmt.Errorf("worker cutover publication requires an unfenced %s registry, found %s", WorkersSchemaV1, snapshot.document.Schema)
	}
	operations, operationFile, err := readWorkerCutoverOperations(directory)
	if err != nil {
		return WorkerCutoverManifest{}, workerRegistrySnapshot{}, nil, workerCutoverFileSnapshot{}, fmt.Errorf("read worker-adopt operations: %w", err)
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
		return WorkerCutoverManifest{}, workerRegistrySnapshot{}, nil, workerCutoverFileSnapshot{}, err
	}
	return manifest, snapshot, operations, operationFile, nil
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
	directory, err := openWorkerCutoverDirectory(filepath.Dir(path), false)
	if err != nil {
		return WorkerCutoverManifest{}, nil, err
	}
	defer directory.Close()
	manifest, data, _, err := loadWorkerCutoverManifestPinned(directory, filepath.Base(path), false)
	return manifest, data, err
}

func loadWorkerCutoverManifestPinned(directory *workerCutoverDirectory, name string, allowMissing bool) (WorkerCutoverManifest, []byte, workerCutoverFileSnapshot, error) {
	requiredMode := os.FileMode(0o600)
	file, err := directory.readRegularFile(name, "worker cutover manifest", allowMissing, &requiredMode)
	if err != nil {
		return WorkerCutoverManifest{}, nil, workerCutoverFileSnapshot{}, err
	}
	if !file.present {
		return WorkerCutoverManifest{}, nil, file, os.ErrNotExist
	}
	data := file.data
	var manifest WorkerCutoverManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, data, file, fmt.Errorf("parse worker cutover manifest: %w", err)
	}
	if err := validateWorkerCutoverManifest(manifest); err != nil {
		return manifest, data, file, err
	}
	canonical, err := canonicalWorkerCutoverManifestBytes(manifest)
	if err != nil {
		return manifest, data, file, err
	}
	if !bytes.Equal(data, canonical) {
		return manifest, data, file, errors.New("worker cutover manifest is not its exact canonical immutable encoding")
	}
	return manifest, data, file, nil
}

func readWorkerCutoverOperations(directory *workerCutoverDirectory) ([]OperationRecord, workerCutoverFileSnapshot, error) {
	requiredMode := os.FileMode(0o600)
	file, err := directory.readRegularFile(OperationsFile, "worker cutover operations evidence", true, &requiredMode)
	if err != nil {
		return nil, workerCutoverFileSnapshot{}, err
	}
	if !file.present {
		return nil, file, nil
	}
	var operationsFile operationFile
	if err := json.Unmarshal(file.data, &operationsFile); err != nil {
		return nil, file, fmt.Errorf("parse operation records: %w", err)
	}
	if operationsFile.SchemaVersion != OperationSchemaVersion {
		return nil, file, fmt.Errorf("unsupported operations file schema version %d", operationsFile.SchemaVersion)
	}
	seen := make(map[string]bool)
	for i, operation := range operationsFile.Operations {
		canonical, err := canonicalOperation(operation)
		if err != nil {
			return nil, file, fmt.Errorf("invalid operation record %d: %w", i+1, err)
		}
		if seen[canonical.Key] {
			return nil, file, fmt.Errorf("duplicate idempotency key %q", canonical.Key)
		}
		seen[canonical.Key] = true
		operationsFile.Operations[i] = canonical
	}
	return operationsFile.Operations, file, nil
}

func currentWorkerAdoptionsPinned(directory *workerCutoverDirectory) ([]OperationRecord, workerCutoverFileSnapshot, error) {
	operations, file, err := readWorkerCutoverOperations(directory)
	if err != nil {
		return nil, workerCutoverFileSnapshot{}, err
	}
	out := make([]OperationRecord, 0)
	for _, operation := range operations {
		if operation.Kind == "worker-adopt" {
			out = append(out, operation)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, file, nil
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
	directory, err := openWorkerCutoverDirectory(dir.Path, false)
	if err != nil {
		return WorkerCutoverExport{}, err
	}
	defer directory.Close()
	return inspectWorkerCutoverPinned(dir, directory)
}

func workerCutoverPreservationName(manifestDigest string) string {
	return "." + WorkersFile + ".worker-cutover-source." + strings.TrimPrefix(manifestDigest, "sha256:")
}

func validateWorkerCutoverSourceSnapshot(snapshot workerRegistrySnapshot, manifest WorkerCutoverManifest) error {
	if snapshot.present != manifest.Workers.SourcePresent || sha256Digest(snapshot.data) != manifest.Workers.SourceSHA256 || fmt.Sprintf("%04o", snapshot.mode.Perm()) != manifest.Workers.SourceMode {
		return errors.New("workers/v1 source mismatches the immutable worker cutover manifest in presence, content, or mode; downgrade fence recovery fails closed")
	}
	if snapshot.document.Schema != WorkersSchemaV1 || snapshot.document.FenceDigest != "" {
		return fmt.Errorf("worker cutover source must remain an unfenced %s registry", WorkersSchemaV1)
	}
	return nil
}

func inspectWorkerCutoverPinned(dir Directory, directory *workerCutoverDirectory) (WorkerCutoverExport, error) {
	export := WorkerCutoverExport{
		SchemaVersion:      WorkerCutoverManifestSchemaVersion,
		State:              "not_published",
		ManifestPath:       dir.WorkerCutoverManifestPath(),
		Registry:           WorkerCutoverRegistryStatus{Path: dir.WorkersPath()},
		Workers:            make([]WorkerCutoverWorkerClassification, 0),
		AdoptionOperations: make([]WorkerCutoverAdoptionClassification, 0),
		Blockers:           make([]string, 0),
	}
	manifest, manifestData, manifestFile, manifestErr := loadWorkerCutoverManifestPinned(directory, WorkerCutoverManifestFile, true)
	manifestPresent := manifestErr == nil
	if manifestErr != nil && !errors.Is(manifestErr, os.ErrNotExist) {
		return export, manifestErr
	}
	visible, registryErr := readWorkerRegistrySnapshotPinned(directory, WorkersFile, true)
	if registryErr != nil {
		return export, registryErr
	}
	export.Registry.Schema = visible.document.Schema
	export.Registry.FenceSHA256 = visible.document.FenceDigest
	export.Registry.Mode = fmt.Sprintf("%04o", visible.mode.Perm())
	export.Registry.SourcePresent = visible.present
	operations, operationFile, err := currentWorkerAdoptionsPinned(directory)
	if err != nil {
		return export, fmt.Errorf("read worker-adopt operations: %w", err)
	}
	requiredPrivateMode := os.FileMode(0o600)
	verifyInputs := func(preservation *workerRegistrySnapshot) error {
		if err := directory.verifyPathIdentity(); err != nil {
			return err
		}
		if err := directory.verifyFileSnapshot(manifestFile, "worker cutover manifest", &requiredPrivateMode); err != nil {
			return err
		}
		if err := directory.verifyFileSnapshot(visible.file, "workers registry", nil); err != nil {
			return err
		}
		if err := directory.verifyFileSnapshot(operationFile, "worker cutover operations evidence", &requiredPrivateMode); err != nil {
			return err
		}
		if preservation != nil {
			if err := directory.verifyFileSnapshot(preservation.file, "preserved workers/v1 cutover source", nil); err != nil {
				return err
			}
		}
		return nil
	}
	if !manifestPresent {
		if visible.document.Schema != WorkersSchemaV1 || visible.document.FenceDigest != "" {
			return export, errors.New("workers/v2 downgrade fence exists without its immutable worker cutover manifest")
		}
		export.Workers, export.AdoptionOperations, export.Blockers, err = classifyWorkerCutover(nil, visible.document.Rows, operations, false)
		if err != nil {
			return export, err
		}
		return export, verifyInputs(nil)
	}
	export.Generation = manifest.Generation
	export.Manifest = &manifest
	export.ManifestSHA256 = sha256Digest(manifestData)
	preservation, preservationErr := readWorkerRegistrySnapshotPinned(directory, workerCutoverPreservationName(export.ManifestSHA256), true)
	if preservationErr != nil {
		return export, fmt.Errorf("read preserved workers/v1 cutover source: %w", preservationErr)
	}
	if preservation.present {
		if !manifest.Workers.SourcePresent {
			return export, errors.New("preserved workers/v1 source exists for a manifest that records an absent source")
		}
		if err := validateWorkerCutoverSourceSnapshot(preservation, manifest); err != nil {
			return export, fmt.Errorf("preserved workers/v1 source: %w", err)
		}
	}
	effective := visible
	if !visible.present && preservation.present {
		effective = preservation
	}
	switch visible.document.Schema {
	case WorkersSchemaV1:
		if visible.present && preservation.present {
			return export, errors.New("preserved workers/v1 source exists alongside an unfenced workers pathname; downgrade fence recovery fails closed")
		}
		if err := validateWorkerCutoverSourceSnapshot(effective, manifest); err != nil {
			return export, err
		}
		if err := validateFenceRecoveryAdoptions(manifest, operations); err != nil {
			return export, err
		}
		export.State = "recovery_required"
	case WorkersSchemaV2:
		if !visible.present || visible.document.FenceDigest != export.ManifestSHA256 {
			return export, errors.New("workers/v2 downgrade fence does not match the immutable worker cutover manifest")
		}
		if preservation.present {
			if err := validateFenceRecoveryAdoptions(manifest, operations); err != nil {
				return export, err
			}
			export.State = "recovery_required"
		} else {
			export.State = "published"
		}
	default:
		return export, fmt.Errorf("unsupported workers registry schema %q", visible.document.Schema)
	}
	export.Workers, export.AdoptionOperations, export.Blockers, err = classifyWorkerCutover(&manifest, effective.document.Rows, operations, visible.document.Schema == WorkersSchemaV2)
	if err != nil {
		return export, err
	}
	return export, verifyInputs(&preservation)
}

func PlanWorkerCutover(dir Directory, generation string) (WorkerCutoverPublication, error) {
	if err := validateWorkerCutoverGeneration(generation); err != nil {
		return WorkerCutoverPublication{}, err
	}
	directory, err := openWorkerCutoverDirectory(dir.Path, false)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	defer directory.Close()
	inspected, err := inspectWorkerCutoverPinned(dir, directory)
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
	manifest, source, operationRecords, operations, err := prepareWorkerCutoverManifestPinned(directory, generation)
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
	inspected.Registry.Schema = source.document.Schema
	inspected.Registry.FenceSHA256 = source.document.FenceDigest
	inspected.Registry.Mode = fmt.Sprintf("%04o", source.mode.Perm())
	inspected.Registry.SourcePresent = source.present
	currentAdoptions := make([]OperationRecord, 0)
	for _, operation := range operationRecords {
		if operation.Kind == "worker-adopt" {
			currentAdoptions = append(currentAdoptions, operation)
		}
	}
	inspected.Workers, inspected.AdoptionOperations, inspected.Blockers, err = classifyWorkerCutover(nil, source.document.Rows, currentAdoptions, false)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	requiredPrivateMode := os.FileMode(0o600)
	if err := directory.verifyPathIdentity(); err != nil {
		return WorkerCutoverPublication{}, err
	}
	if err := directory.verifyFileSnapshot(source.file, "workers registry", nil); err != nil {
		return WorkerCutoverPublication{}, err
	}
	if err := directory.verifyFileSnapshot(operations, "worker cutover operations evidence", &requiredPrivateMode); err != nil {
		return WorkerCutoverPublication{}, err
	}
	return WorkerCutoverPublication{Action: WorkerCutoverPublishNew, Export: inspected}, nil
}

func writeImmutableWorkerCutoverManifest(path string, data []byte) error {
	directory, err := openWorkerCutoverDirectory(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer directory.Close()
	return writeImmutableWorkerCutoverManifestPinned(directory, filepath.Base(path), data)
}

func writeImmutableWorkerCutoverManifestPinned(directory *workerCutoverDirectory, name string, data []byte) error {
	if err := directory.verifyPathIdentity(); err != nil {
		return err
	}
	tmp, err := directory.createTemp("."+name+".tmp.", data, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = directory.unlink(tmp)
		}
	}()
	if err := directory.renameNoReplace(tmp, name); err != nil {
		return err
	}
	committed = true
	return directory.sync()
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

func installWorkerCutoverFencePinned(directory *workerCutoverDirectory, manifest WorkerCutoverManifest, manifestData []byte, manifestFile workerCutoverFileSnapshot, boundSource *workerRegistrySnapshot, boundOperations *workerCutoverFileSnapshot) error {
	visible, err := readWorkerRegistrySnapshotPinned(directory, WorkersFile, true)
	if err != nil {
		return err
	}
	manifestDigest := sha256Digest(manifestData)
	preservationName := workerCutoverPreservationName(manifestDigest)
	preservation, err := readWorkerRegistrySnapshotPinned(directory, preservationName, true)
	if err != nil {
		return fmt.Errorf("read preserved workers/v1 cutover source: %w", err)
	}
	operations, operationFile, err := currentWorkerAdoptionsPinned(directory)
	if err != nil {
		return err
	}
	if err := validateFenceRecoveryAdoptions(manifest, operations); err != nil {
		return err
	}
	requiredPrivateMode := os.FileMode(0o600)
	verifyBoundInputs := func() error {
		if err := directory.verifyPathIdentity(); err != nil {
			return err
		}
		if err := directory.verifyFileSnapshot(manifestFile, "worker cutover manifest", &requiredPrivateMode); err != nil {
			return err
		}
		if boundOperations != nil {
			if err := directory.verifyFileSnapshot(*boundOperations, "worker cutover operations evidence", &requiredPrivateMode); err != nil {
				return err
			}
		} else if err := directory.verifyFileSnapshot(operationFile, "worker cutover operations evidence", &requiredPrivateMode); err != nil {
			return err
		}
		return nil
	}
	if visible.document.Schema == WorkersSchemaV2 {
		if !visible.present || visible.document.FenceDigest != manifestDigest {
			return errors.New("existing workers/v2 downgrade fence does not match the immutable manifest")
		}
		if !preservation.present {
			return nil
		}
		if err := validateWorkerCutoverSourceSnapshot(preservation, manifest); err != nil {
			return fmt.Errorf("preserved workers/v1 source: %w", err)
		}
		if workerCutoverFilesystemHook != nil {
			workerCutoverFilesystemHook("before-workers-cleanup")
		}
		if err := verifyBoundInputs(); err != nil {
			return err
		}
		if err := directory.verifyFileSnapshot(visible.file, "workers registry", nil); err != nil {
			return err
		}
		if err := directory.verifyFileSnapshot(preservation.file, "preserved workers/v1 cutover source", nil); err != nil {
			return err
		}
		if err := directory.unlink(preservationName); err != nil {
			return err
		}
		return directory.sync()
	}
	if visible.present && preservation.present {
		return errors.New("preserved workers/v1 source exists alongside an unfenced workers pathname; fence installation fails closed")
	}
	source := visible
	if preservation.present {
		source = preservation
	}
	if err := validateWorkerCutoverSourceSnapshot(source, manifest); err != nil {
		return errors.New("workers/v1 source changed after immutable manifest creation; fence installation fails closed: " + err.Error())
	}
	if boundSource != nil {
		if preservation.present || source.present != boundSource.present {
			return errors.New("workers/v1 source pathname changed after immutable manifest creation; fence installation fails closed")
		}
		if source.present && !os.SameFile(source.file.info, boundSource.file.info) {
			return errors.New("workers/v1 source pathname identity changed after immutable manifest creation; fence installation fails closed")
		}
		if source.mode.Perm() != boundSource.mode.Perm() || !bytes.Equal(source.data, boundSource.data) {
			return errors.New("workers/v1 source content or mode changed after immutable manifest creation; fence installation fails closed")
		}
	}
	fenced, err := fencedWorkerRegistry(source.data, manifestDigest)
	if err != nil {
		return err
	}
	tmp, err := directory.createTemp("."+WorkersFile+".fence.", fenced, source.mode)
	if err != nil {
		return err
	}
	tmpPresent := true
	defer func() {
		if tmpPresent {
			_ = directory.unlink(tmp)
		}
	}()
	preserved := preservation.present
	restore := func(cause error) error {
		if !preserved {
			return cause
		}
		if restoreErr := directory.renameNoReplace(preservationName, WorkersFile); restoreErr != nil {
			return fmt.Errorf("%w; preserved workers/v1 source remains at %s because restoration failed: %v", cause, filepath.Join(directory.path, preservationName), restoreErr)
		}
		preserved = false
		if syncErr := directory.sync(); syncErr != nil {
			return fmt.Errorf("%w; restored workers/v1 source but could not sync its directory entry: %v", cause, syncErr)
		}
		return cause
	}
	if source.present && !preservation.present {
		if workerCutoverFilesystemHook != nil {
			workerCutoverFilesystemHook("before-workers-displace")
		}
		if err := verifyBoundInputs(); err != nil {
			return err
		}
		if err := directory.verifyFileSnapshot(source.file, "workers registry", nil); err != nil {
			return err
		}
		if err := directory.renameNoReplace(WorkersFile, preservationName); err != nil {
			return err
		}
		preserved = true
		if workerCutoverFilesystemHook != nil {
			workerCutoverFilesystemHook("after-workers-displace")
		}
		displaced, err := readWorkerRegistrySnapshotPinned(directory, preservationName, false)
		if err != nil {
			return restore(err)
		}
		if !os.SameFile(source.file.info, displaced.file.info) || source.mode.Perm() != displaced.mode.Perm() || !bytes.Equal(source.data, displaced.data) {
			return restore(errors.New("displaced workers registry is not the exact descriptor-validated source; fence installation fails closed"))
		}
		preservation = displaced
		if err := directory.sync(); err != nil {
			return restore(fmt.Errorf("durably preserve descriptor-validated workers/v1 source: %w", err))
		}
	}
	if workerCutoverFilesystemHook != nil {
		workerCutoverFilesystemHook("before-workers-commit")
	}
	if err := verifyBoundInputs(); err != nil {
		return restore(err)
	}
	if preserved {
		if err := directory.verifyFileSnapshot(preservation.file, "preserved workers/v1 cutover source", nil); err != nil {
			return restore(err)
		}
	} else if err := directory.verifyFileSnapshot(source.file, "workers registry", nil); err != nil {
		return err
	}
	if err := directory.renameNoReplace(tmp, WorkersFile); err != nil {
		return restore(fmt.Errorf("workers pathname changed before descriptor-validated fence commit: %w", err))
	}
	tmpPresent = false
	if err := directory.sync(); err != nil {
		return fmt.Errorf("durably commit workers/v2 downgrade fence: %w", err)
	}
	if preserved {
		if err := directory.unlink(preservationName); err != nil {
			return err
		}
		preserved = false
		if err := directory.sync(); err != nil {
			return fmt.Errorf("durably remove preserved workers/v1 source after fence commit: %w", err)
		}
	}
	return nil
}

func PublishWorkerCutover(dir Directory, generation string) (WorkerCutoverPublication, error) {
	if err := validateWorkerCutoverGeneration(generation); err != nil {
		return WorkerCutoverPublication{}, err
	}
	directory, err := openWorkerCutoverDirectory(dir.Path, false)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	if !directory.present {
		_ = directory.Close()
		directory, err = openWorkerCutoverDirectory(dir.Path, true)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
	}
	defer directory.Close()
	inspected, err := inspectWorkerCutoverPinned(dir, directory)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	action := WorkerCutoverPublishNew
	if inspected.Manifest != nil {
		if inspected.Generation != generation {
			return WorkerCutoverPublication{}, fmt.Errorf("worker cutover is immutably bound to generation %q, not %q", inspected.Generation, generation)
		}
		if inspected.State == "published" {
			return WorkerCutoverPublication{Action: WorkerCutoverPublishDuplicate, Export: inspected}, nil
		}
		action = WorkerCutoverRecoverFence
	}
	var manifest WorkerCutoverManifest
	var manifestData []byte
	var manifestFile workerCutoverFileSnapshot
	var boundSource *workerRegistrySnapshot
	var boundOperations *workerCutoverFileSnapshot
	if action == WorkerCutoverPublishNew {
		var source workerRegistrySnapshot
		var operations workerCutoverFileSnapshot
		manifest, source, _, operations, err = prepareWorkerCutoverManifestPinned(directory, generation)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
		manifestData, err = canonicalWorkerCutoverManifestBytes(manifest)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
		if workerCutoverFilesystemHook != nil {
			workerCutoverFilesystemHook("before-manifest-commit")
		}
		requiredPrivateMode := os.FileMode(0o600)
		if err := directory.verifyPathIdentity(); err != nil {
			return WorkerCutoverPublication{}, err
		}
		if err := directory.verifyFileSnapshot(source.file, "workers registry", nil); err != nil {
			return WorkerCutoverPublication{}, err
		}
		if err := directory.verifyFileSnapshot(operations, "worker cutover operations evidence", &requiredPrivateMode); err != nil {
			return WorkerCutoverPublication{}, err
		}
		if err := writeImmutableWorkerCutoverManifestPinned(directory, WorkerCutoverManifestFile, manifestData); err != nil {
			return WorkerCutoverPublication{}, fmt.Errorf("durably publish immutable worker cutover manifest: %w", err)
		}
		_, _, manifestFile, err = loadWorkerCutoverManifestPinned(directory, WorkerCutoverManifestFile, false)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
		boundSource = &source
		boundOperations = &operations
	} else {
		manifest, manifestData, manifestFile, err = loadWorkerCutoverManifestPinned(directory, WorkerCutoverManifestFile, false)
		if err != nil {
			return WorkerCutoverPublication{}, err
		}
	}
	if err := installWorkerCutoverFencePinned(directory, manifest, manifestData, manifestFile, boundSource, boundOperations); err != nil {
		return WorkerCutoverPublication{}, fmt.Errorf("durably install workers/v2 downgrade fence: %w", err)
	}
	inspected, err = inspectWorkerCutoverPinned(dir, directory)
	if err != nil {
		return WorkerCutoverPublication{}, err
	}
	return WorkerCutoverPublication{Action: action, Export: inspected}, nil
}
