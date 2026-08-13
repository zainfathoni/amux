package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const workerCutoverTestGeneration = "worker-native-cutover-v1"

func TestWorkerCutoverPublishesImmutableManifestAndDowngradeFence(t *testing.T) {
	dir := Directory{Path: t.TempDir()}
	source := "# amux-schema: workers/v1\n# retained comment\nalpha\tworker\t/tmp/alpha\tT-alpha\n"
	if err := os.WriteFile(dir.WorkersPath(), []byte(source), 0o640); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 1, 2, 3, 4, time.UTC)
	adoption := OperationRecord{
		Key: "worker-adopt:T-alpha", Kind: "worker-adopt", RequestHash: "request-alpha",
		State: OperationStarted, Resource: OperationResource{Kind: "worker", Thread: "T-alpha"},
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := StoreOperation(dir.OperationsPath(), adoption); err != nil {
		t.Fatal(err)
	}
	spawn := OperationRecord{
		Key: "legacy-spawn", Kind: "worker-spawn", RequestHash: "spawn-request",
		State: OperationStarted, Phase: OperationPhaseThreadBound,
		Resource: OperationResource{Kind: "worker", Thread: "T-spawn"}, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := StoreOperation(dir.OperationsPath(), spawn); err != nil {
		t.Fatal(err)
	}
	operationsBefore, err := os.ReadFile(dir.OperationsPath())
	if err != nil {
		t.Fatal(err)
	}

	publication, err := PublishWorkerCutover(dir, workerCutoverTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if publication.Action != WorkerCutoverPublishNew || publication.Export.State != "published" || len(publication.Export.Blockers) != 0 {
		t.Fatalf("publication=%+v", publication)
	}
	manifest := publication.Export.Manifest
	if manifest == nil || manifest.Generation != workerCutoverTestGeneration || manifest.Workers.SourceSHA256 != sha256Digest([]byte(source)) || manifest.Workers.SourceMode != "0640" || len(manifest.Workers.Rows) != 1 || len(manifest.AdoptionOperations) != 1 {
		t.Fatalf("manifest=%+v", manifest)
	}
	if manifest.SpawnCutover != spawnCutoverReference() || manifest.SpawnCutover.Generation != SpawnCutoverGeneration || manifest.SpawnCutover.ExceptionAdmission != SpawnAssignmentProjectlessHostAdmission {
		t.Fatalf("spawn reference=%+v", manifest.SpawnCutover)
	}
	if manifest.AdoptionOperations[0].Key != adoption.Key || manifest.AdoptionOperations[0].RequestHash != adoption.RequestHash {
		t.Fatalf("adoption snapshot=%+v", manifest.AdoptionOperations)
	}
	manifestInfo, err := os.Stat(dir.WorkerCutoverManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	workerInfo, err := os.Stat(dir.WorkersPath())
	if err != nil {
		t.Fatal(err)
	}
	if manifestInfo.Mode().Perm() != 0o600 || workerInfo.Mode().Perm() != 0o640 {
		t.Fatalf("modes manifest=%04o workers=%04o", manifestInfo.Mode().Perm(), workerInfo.Mode().Perm())
	}
	workersV2, err := os.ReadFile(dir.WorkersPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(workersV2), workerSchemaHeaderV2+"\n"+workerCutoverControlLine(publication.Export.ManifestSHA256)+"\n") || !strings.Contains(string(workersV2), "# retained comment\nalpha\tworker") {
		t.Fatalf("workers/v2=%q", workersV2)
	}
	rows, err := Parse(bytes.NewReader(workersV2))
	if err != nil || len(rows) != 1 || rows[0].Thread != "T-alpha" {
		t.Fatalf("compatibility rows=%+v err=%v", rows, err)
	}
	if _, err := parseWorkersWithPriorReader(bytes.NewReader(workersV2)); err == nil || !strings.Contains(err.Error(), "invalid Amp thread ID") {
		t.Fatalf("prior workers/v1 reader bypassed downgrade fence: %v", err)
	}
	operationsAfter, err := os.ReadFile(dir.OperationsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(operationsBefore, operationsAfter) {
		t.Fatalf("worker cutover mutated operations.json\nbefore=%s\nafter=%s", operationsBefore, operationsAfter)
	}
}

// parseWorkersWithPriorReader freezes the workers/v1 reader shipped before the
// cutover. The workers/v2 control row must be data, not a comment, so this
// reader rejects the registry before returning any worker rows.
func parseWorkersWithPriorReader(r io.Reader) ([]Row, error) {
	scanner := bufio.NewScanner(r)
	var rows []Row
	seenThreads := make(map[string]string)
	seenWindows := make(map[string]bool)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 && len(fields) != 5 {
			return nil, fmt.Errorf("invalid row on line %d: expected 4 or 5 tab-separated fields", lineNo)
		}
		thread, err := CanonicalThreadID(fields[3])
		if err != nil {
			return nil, fmt.Errorf("invalid row on line %d: %w", lineNo, err)
		}
		row := Row{Workspace: fields[0], Window: fields[1], Workdir: fields[2], Thread: thread}
		if len(fields) == 5 {
			row.AssignmentState = WorkerAssignmentState(fields[4])
		}
		if err := row.Validate(); err != nil {
			return nil, fmt.Errorf("invalid row on line %d: %w", lineNo, err)
		}
		windowKey := row.Workspace + "\x00" + row.Window
		if seenWindows[windowKey] {
			return nil, fmt.Errorf("duplicate worker row %s/%s", row.Workspace, row.Window)
		}
		if previous, exists := seenThreads[row.Thread]; exists {
			return nil, fmt.Errorf("worker thread %s is already configured as %s", row.Thread, previous)
		}
		seenWindows[windowKey] = true
		seenThreads[row.Thread] = row.Workspace + "/" + row.Window
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func TestWorkerCutoverPlanIsReadOnlyAndMissingRegistryPublishesCanonicalEmptyFence(t *testing.T) {
	dir := Directory{Path: filepath.Join(t.TempDir(), "missing-config")}
	plan, err := PlanWorkerCutover(dir, workerCutoverTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != WorkerCutoverPublishNew || plan.Export.Manifest == nil || plan.Export.Manifest.Workers.SourcePresent || len(plan.Export.Manifest.Workers.Rows) != 0 {
		t.Fatalf("plan=%+v", plan)
	}
	plannedData, err := canonicalWorkerCutoverManifestBytes(*plan.Export.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Export.ManifestSHA256 != sha256Digest(plannedData) {
		t.Fatalf("planned manifest digest=%q, want %q", plan.Export.ManifestSHA256, sha256Digest(plannedData))
	}
	if _, err := os.Stat(dir.Path); !os.IsNotExist(err) {
		t.Fatalf("read-only plan created config directory: %v", err)
	}

	published, err := PublishWorkerCutover(dir, workerCutoverTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if published.Export.State != "published" || published.Export.Registry.SourcePresent != true {
		t.Fatalf("published=%+v", published)
	}
	info, err := os.Stat(dir.WorkersPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("new workers/v2 mode=%04o", info.Mode().Perm())
	}
	if _, err := LoadReadOnly(dir.WorkersPath()); err != nil {
		t.Fatalf("new compatibility reader rejected empty fenced registry: %v", err)
	}
	if _, err := os.Stat(dir.OperationsPath()); !os.IsNotExist(err) {
		t.Fatalf("publication created operations store: %v", err)
	}
}

func TestWorkerCutoverExactReplayDoesNotRewriteAndConflictingGenerationFails(t *testing.T) {
	dir := Directory{Path: t.TempDir()}
	if err := os.WriteFile(dir.WorkersPath(), []byte(defaultConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishWorkerCutover(dir, workerCutoverTestGeneration); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.Stat(dir.WorkerCutoverManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	workersBefore, err := os.Stat(dir.WorkersPath())
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, _ := os.ReadFile(dir.WorkerCutoverManifestPath())
	workerBytes, _ := os.ReadFile(dir.WorkersPath())

	replay, err := PublishWorkerCutover(dir, workerCutoverTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Action != WorkerCutoverPublishDuplicate {
		t.Fatalf("replay action=%s", replay.Action)
	}
	manifestAfter, _ := os.Stat(dir.WorkerCutoverManifestPath())
	workersAfter, _ := os.Stat(dir.WorkersPath())
	manifestBytesAfter, _ := os.ReadFile(dir.WorkerCutoverManifestPath())
	workerBytesAfter, _ := os.ReadFile(dir.WorkersPath())
	if !os.SameFile(manifestBefore, manifestAfter) || !os.SameFile(workersBefore, workersAfter) || !bytes.Equal(manifestBytes, manifestBytesAfter) || !bytes.Equal(workerBytes, workerBytesAfter) {
		t.Fatal("exact replay rewrote immutable cutover artifacts")
	}
	if _, err := PublishWorkerCutover(dir, "different-generation"); err == nil || !strings.Contains(err.Error(), "immutably bound") {
		t.Fatalf("conflicting generation error=%v", err)
	}
}

func TestWorkerCutoverRecoversManifestOnlyCrashAndFailsClosedOnMismatch(t *testing.T) {
	t.Run("matching recovery", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		source := []byte("# amux-schema: workers/v1\nalpha\tworker\t/tmp\tT-alpha\n")
		if err := os.WriteFile(dir.WorkersPath(), source, 0o620); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir.WorkersPath(), 0o620); err != nil {
			t.Fatal(err)
		}
		manifest, err := prepareWorkerCutoverManifest(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		manifestData, _ := canonicalWorkerCutoverManifestBytes(manifest)
		if err := writeImmutableWorkerCutoverManifest(dir.WorkerCutoverManifestPath(), manifestData); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(dir.WorkerCutoverManifestPath())
		status, err := InspectWorkerCutover(dir)
		if err != nil || status.State != "recovery_required" {
			t.Fatalf("status=%+v err=%v", status, err)
		}
		recovered, err := PublishWorkerCutover(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		if recovered.Action != WorkerCutoverRecoverFence || recovered.Export.State != "published" {
			t.Fatalf("recovered=%+v", recovered)
		}
		after, _ := os.ReadFile(dir.WorkerCutoverManifestPath())
		info, _ := os.Stat(dir.WorkersPath())
		if !bytes.Equal(before, after) || info.Mode().Perm() != 0o620 {
			t.Fatalf("recovery rewrote manifest or changed worker mode: mode=%04o", info.Mode().Perm())
		}
	})

	t.Run("source mismatch", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		if err := os.WriteFile(dir.WorkersPath(), []byte(defaultConfig), 0o600); err != nil {
			t.Fatal(err)
		}
		manifest, err := prepareWorkerCutoverManifest(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		manifestData, _ := canonicalWorkerCutoverManifestBytes(manifest)
		if err := writeImmutableWorkerCutoverManifest(dir.WorkerCutoverManifestPath(), manifestData); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir.WorkersPath(), append([]byte(defaultConfig), []byte("alpha\tworker\t/tmp\tT-alpha\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectWorkerCutover(dir); err == nil || !strings.Contains(err.Error(), "source mismatches") {
			t.Fatalf("source mismatch error=%v", err)
		}
		contents, _ := os.ReadFile(dir.WorkersPath())
		if strings.Contains(string(contents), WorkersSchemaV2) {
			t.Fatalf("mismatch installed fence: %s", contents)
		}
	})

	t.Run("source mode mismatch", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		if err := os.WriteFile(dir.WorkersPath(), []byte(defaultConfig), 0o640); err != nil {
			t.Fatal(err)
		}
		manifest, err := prepareWorkerCutoverManifest(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		manifestData, _ := canonicalWorkerCutoverManifestBytes(manifest)
		if err := writeImmutableWorkerCutoverManifest(dir.WorkerCutoverManifestPath(), manifestData); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir.WorkersPath(), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectWorkerCutover(dir); err == nil || !strings.Contains(err.Error(), "presence, content, or mode") {
			t.Fatalf("source mode mismatch error=%v", err)
		}
		contents, _ := os.ReadFile(dir.WorkersPath())
		if strings.Contains(string(contents), WorkersSchemaV2) {
			t.Fatalf("mode mismatch installed fence: %s", contents)
		}
	})

	t.Run("new adoption mismatch", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		manifest, err := prepareWorkerCutoverManifest(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		manifestData, _ := canonicalWorkerCutoverManifestBytes(manifest)
		if err := writeImmutableWorkerCutoverManifest(dir.WorkerCutoverManifestPath(), manifestData); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if _, err := StoreOperation(dir.OperationsPath(), OperationRecord{Key: "worker-adopt:T-new", Kind: "worker-adopt", RequestHash: "new", State: OperationStarted, Resource: OperationResource{Kind: "worker", Thread: "T-new"}, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectWorkerCutover(dir); err == nil || !strings.Contains(err.Error(), "operation set changed") {
			t.Fatalf("adoption mismatch error=%v", err)
		}
	})
}

func TestWorkerCutoverClassificationExportIsReadOnlyAndFlagsPostCutoverRows(t *testing.T) {
	dir := Directory{Path: t.TempDir()}
	if err := os.WriteFile(dir.WorkersPath(), []byte("# amux-schema: workers/v1\nalpha\tworker\t/tmp/a\tT-alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	operation := OperationRecord{Key: "worker-adopt:T-alpha", Kind: "worker-adopt", RequestHash: "alpha", State: OperationStarted, Resource: OperationResource{Kind: "worker", Thread: "T-alpha"}, CreatedAt: now, UpdatedAt: now}
	if _, err := StoreOperation(dir.OperationsPath(), operation); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishWorkerCutover(dir, workerCutoverTestGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := Store(dir.WorkersPath(), Row{Workspace: "beta", Window: "worker", Workdir: "/tmp/b", Thread: "T-beta"}); err != nil {
		t.Fatal(err)
	}
	operation.State = OperationSucceeded
	operation.UpdatedAt = now.Add(time.Second)
	if _, err := StoreOperation(dir.OperationsPath(), operation); err != nil {
		t.Fatal(err)
	}
	newOperation := OperationRecord{Key: "worker-adopt:T-beta", Kind: "worker-adopt", RequestHash: "beta", State: OperationStarted, Resource: OperationResource{Kind: "worker", Thread: "T-beta"}, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)}
	if _, err := StoreOperation(dir.OperationsPath(), newOperation); err != nil {
		t.Fatal(err)
	}
	beforeWorkers, _ := os.ReadFile(dir.WorkersPath())
	beforeOperations, _ := os.ReadFile(dir.OperationsPath())
	beforeManifest, _ := os.ReadFile(dir.WorkerCutoverManifestPath())

	exported, err := InspectWorkerCutover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if exported.State != "published" || len(exported.Workers) != 2 || len(exported.AdoptionOperations) != 2 || len(exported.Blockers) != 2 {
		t.Fatalf("export=%+v", exported)
	}
	workerClasses := map[string]string{}
	for _, classification := range exported.Workers {
		workerClasses[classification.Thread] = classification.Classification
	}
	adoptionClasses := map[string]string{}
	for _, classification := range exported.AdoptionOperations {
		adoptionClasses[classification.Key] = classification.Classification
	}
	if workerClasses["T-alpha"] != "pre_cutover_present" || workerClasses["T-beta"] != "not_in_pre_cutover_manifest" || adoptionClasses[operation.Key] != "pre_cutover_present" || adoptionClasses[newOperation.Key] != "not_in_pre_cutover_manifest" {
		t.Fatalf("worker classes=%v adoption classes=%v", workerClasses, adoptionClasses)
	}
	afterWorkers, _ := os.ReadFile(dir.WorkersPath())
	afterOperations, _ := os.ReadFile(dir.OperationsPath())
	afterManifest, _ := os.ReadFile(dir.WorkerCutoverManifestPath())
	if !bytes.Equal(beforeWorkers, afterWorkers) || !bytes.Equal(beforeOperations, afterOperations) || !bytes.Equal(beforeManifest, afterManifest) {
		t.Fatal("read-only classification export mutated state")
	}
}

func TestWorkerCutoverRejectsNonCanonicalSourceManifestAndFence(t *testing.T) {
	t.Run("unversioned source", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		if err := os.WriteFile(dir.WorkersPath(), []byte("alpha\tworker\t/tmp\tT-alpha\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanWorkerCutover(dir, workerCutoverTestGeneration); err == nil || !strings.Contains(err.Error(), "schema header") {
			t.Fatalf("unversioned source error=%v", err)
		}
	})

	t.Run("noncanonical manifest", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		manifest, err := prepareWorkerCutoverManifest(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := canonicalWorkerCutoverManifestBytes(manifest)
		data = bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)
		if err := os.WriteFile(dir.WorkerCutoverManifestPath(), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectWorkerCutover(dir); err == nil || !strings.Contains(err.Error(), "canonical immutable encoding") {
			t.Fatalf("noncanonical manifest error=%v", err)
		}
	})

	t.Run("noncanonical null manifest arrays", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		manifest, err := prepareWorkerCutoverManifest(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := canonicalWorkerCutoverManifestBytes(manifest)
		data = bytes.Replace(data, []byte(`"adoption_operations":[]`), []byte(`"adoption_operations":null`), 1)
		if err := os.WriteFile(dir.WorkerCutoverManifestPath(), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectWorkerCutover(dir); err == nil || !strings.Contains(err.Error(), "canonical arrays") {
			t.Fatalf("null manifest array error=%v", err)
		}
	})

	t.Run("fence mismatch", func(t *testing.T) {
		dir := Directory{Path: t.TempDir()}
		published, err := PublishWorkerCutover(dir, workerCutoverTestGeneration)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(dir.WorkersPath())
		wrong := "sha256:" + strings.Repeat("0", 64)
		data = bytes.Replace(data, []byte(published.Export.ManifestSHA256), []byte(wrong), 1)
		if err := os.WriteFile(dir.WorkersPath(), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectWorkerCutover(dir); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("fence mismatch error=%v", err)
		}
	})
}
