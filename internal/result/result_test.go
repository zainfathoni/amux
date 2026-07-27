package result

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestEnvelopeWritesOneVersionedDocumentWithDiscriminatedResources(t *testing.T) {
	workdir := t.TempDir()
	worker, err := WorkerResource("https://ampcode.com/threads/T-worker")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := RunnerResource(filepath.Join(workdir, "."))
	if err != nil {
		t.Fatal(err)
	}
	envelope := NewEnvelope("worker launch", true)
	envelope.Planned = append(envelope.Planned, Outcome{Resource: worker, Action: "launch"})
	envelope.Skipped = append(envelope.Skipped, Outcome{Resource: runner, Action: "launch", Message: "already running"})

	var stdout bytes.Buffer
	if err := envelope.Write(&stdout); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&stdout)
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON stdout contains more than one document: %v", err)
	}
	if got := int(document["schema_version"].(float64)); got != SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", got, SchemaVersion)
	}
	planned := document["planned"].([]any)[0].(map[string]any)
	workerID := planned["resource"].(map[string]any)
	if workerID["kind"] != "worker" || workerID["thread"] != "T-worker" {
		t.Fatalf("worker resource = %#v", workerID)
	}
	skipped := document["skipped"].([]any)[0].(map[string]any)
	runnerID := skipped["resource"].(map[string]any)
	if runnerID["kind"] != "runner" || runnerID["workdir"] != workdir {
		t.Fatalf("runner resource = %#v", runnerID)
	}
	if document["successful"] == nil || document["failed"] == nil {
		t.Fatalf("empty outcome buckets must be arrays: %#v", document)
	}
}

func TestWorkerPlacementFieldsRemainExplicitInSchemaV1(t *testing.T) {
	envelope := NewEnvelope("worker doctor", false)
	worker, err := WorkerResource("T-worker")
	if err != nil {
		t.Fatal(err)
	}
	envelope.Successful = append(envelope.Successful, Outcome{
		Resource: worker,
		Action:   "doctor",
		Worker:   &WorkerDetails{},
	})

	var stdout bytes.Buffer
	if err := envelope.Write(&stdout); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if got := int(document["schema_version"].(float64)); got != SchemaVersion {
		t.Fatalf("schema_version = %d, want additive schema v1", got)
	}
	details := document["successful"].([]any)[0].(map[string]any)["worker"].(map[string]any)
	for _, field := range []string{"native_executor", "native_runner_id", "execution_affinity"} {
		if value, found := details[field]; !found || value != "" {
			t.Fatalf("worker placement field %q = %#v, found=%t; field must remain explicit", field, value, found)
		}
	}
	var legacy struct {
		SchemaVersion int `json:"schema_version"`
		Successful    []struct {
			Worker struct {
				Workdir string `json:"workdir"`
			} `json:"worker"`
		} `json:"successful"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &legacy); err != nil || legacy.SchemaVersion != SchemaVersion || len(legacy.Successful) != 1 {
		t.Fatalf("schema-v1 consumer rejected additive worker placement fields: legacy=%+v err=%v", legacy, err)
	}
}

func TestExitCodeDistinguishesRejectedAndRuntimeFailures(t *testing.T) {
	if got := ExitCode(nil); got != ExitSuccess {
		t.Fatalf("ExitCode(nil) = %d, want %d", got, ExitSuccess)
	}
	if got := ExitCode(Request(errors.New("bad flag"))); got != ExitRejected {
		t.Fatalf("request ExitCode = %d, want %d", got, ExitRejected)
	}
	if got := ExitCode(Preflight(errors.New("conflict"))); got != ExitRejected {
		t.Fatalf("preflight ExitCode = %d, want %d", got, ExitRejected)
	}
	if got := ExitCode(Runtime(errors.New("tmux failed"))); got != ExitRuntimeFailure {
		t.Fatalf("runtime ExitCode = %d, want %d", got, ExitRuntimeFailure)
	}
}
