package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

const workerCutoverCLITestGeneration = "worker-native-cutover-v1"

func TestWorkerCutoverCLIHelpAndGenerationValidation(t *testing.T) {
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"help", "worker", "cutover", "publish"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Usage: amux worker cutover publish", "--generation <label>", "workers/v2 downgrade fence"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("publish help missing %q:\n%s", want, stdout.String())
		}
	}
	if err := (app{}).execute([]string{"--config-dir", t.TempDir(), "worker", "cutover", "publish"}); err == nil || !strings.Contains(err.Error(), "requires --generation") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("missing generation error=%v exit=%d", err, result.ExitCode(err))
	}
	if err := (app{}).execute([]string{"--config-dir", t.TempDir(), "worker", "cutover", "publish", "--generation", "INVALID"}); err == nil || !strings.Contains(err.Error(), "must match") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("invalid generation error=%v exit=%d", err, result.ExitCode(err))
	}
}

func TestWorkerCutoverCLIDryRunPlansWithoutConfigWrites(t *testing.T) {
	dir := t.TempDir()
	workers := filepath.Join(dir, config.WorkersFile)
	source := []byte("# amux-schema: workers/v1\nalpha\tworker\t/tmp\tT-alpha\n")
	if err := os.WriteFile(workers, source, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--dry-run", "--config-dir", dir, "worker", "cutover", "publish", "--generation", workerCutoverCLITestGeneration})
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&stdout)
	var envelope result.Envelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("dry-run emitted multiple JSON documents: %v", err)
	}
	if !envelope.DryRun || len(envelope.Planned) != 1 || envelope.Planned[0].Cutover == nil || envelope.Planned[0].Cutover.Manifest == nil || envelope.Planned[0].Cutover.Manifest.Generation != workerCutoverCLITestGeneration {
		t.Fatalf("dry-run envelope=%+v", envelope)
	}
	if !strings.HasPrefix(envelope.Planned[0].Cutover.ManifestSHA256, "sha256:") {
		t.Fatalf("dry-run omitted deterministic manifest digest: %+v", envelope.Planned[0].Cutover)
	}
	after, err := os.ReadFile(workers)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, after) {
		t.Fatalf("dry-run changed workers.tsv: %s", after)
	}
	if _, err := os.Stat(filepath.Join(dir, config.WorkerCutoverManifestFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run created manifest: %v", err)
	}
}

func TestWorkerCutoverCLICompletion(t *testing.T) {
	tests := map[string][]string{
		"bash": {`compgen -W "publish status export"`, `compgen -W "--generation"`},
		"zsh":  {`_values 'worker cutover command' publish status export`, `_arguments '--generation[immutable cutover generation]:generation:'`},
		"fish": {
			"test -z (__fish_amux_worker_cutover_command)' -a 'publish'",
			"test (__fish_amux_worker_cutover_command) = publish' -r -l 'generation'",
		},
	}
	for shell, wants := range tests {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute([]string{"completion", shell}); err != nil {
				t.Fatal(err)
			}
			for _, want := range wants {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("%s completion missing %q", shell, want)
				}
			}
		})
	}
}

func TestWorkerCutoverCLIPublishStatusExportAndExactReplay(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.WorkersFile), []byte("# amux-schema: workers/v1\nalpha\tworker\t/tmp\tT-alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var publish bytes.Buffer
	if err := (app{stdout: &publish}).execute([]string{"--config-dir", dir, "worker", "cutover", "publish", "--generation", workerCutoverCLITestGeneration}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(publish.String(), "state=published") || !strings.Contains(publish.String(), "action=publish") {
		t.Fatalf("publish output=%q", publish.String())
	}

	var statusJSON bytes.Buffer
	if err := (app{stdout: &statusJSON}).execute([]string{"--json", "--config-dir", dir, "worker", "cutover", "status"}); err != nil {
		t.Fatal(err)
	}
	var envelope result.Envelope
	if err := json.NewDecoder(&statusJSON).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Successful) != 1 || envelope.Successful[0].Cutover == nil || envelope.Successful[0].Cutover.State != "published" || envelope.Successful[0].Cutover.Registry.Schema != config.WorkersSchemaV2 {
		t.Fatalf("status envelope=%+v", envelope)
	}

	var exportJSON bytes.Buffer
	if err := (app{stdout: &exportJSON}).execute([]string{"--config-dir", dir, "worker", "cutover", "export"}); err != nil {
		t.Fatal(err)
	}
	var exported config.WorkerCutoverExport
	decoder := json.NewDecoder(&exportJSON)
	if err := decoder.Decode(&exported); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("export emitted multiple documents: %v", err)
	}
	if exported.Generation != workerCutoverCLITestGeneration || len(exported.Workers) != 1 || exported.Workers[0].Classification != "pre_cutover_present" {
		t.Fatalf("export=%+v", exported)
	}

	var replayJSON bytes.Buffer
	if err := (app{stdout: &replayJSON}).execute([]string{"--json", "--config-dir", dir, "worker", "cutover", "publish", "--generation", workerCutoverCLITestGeneration}); err != nil {
		t.Fatal(err)
	}
	envelope = result.Envelope{}
	if err := json.NewDecoder(&replayJSON).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Skipped) != 1 || envelope.Skipped[0].Action != string(config.WorkerCutoverPublishDuplicate) {
		t.Fatalf("replay envelope=%+v", envelope)
	}
}

func TestWorkerCutoverCLIStatusAndExportAreReadOnlyWhenUnpublished(t *testing.T) {
	for _, command := range []string{"status", "export"} {
		t.Run(command, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "missing")
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute([]string{"--config-dir", dir, "worker", "cutover", command}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("%s created config directory: %v", command, err)
			}
			if !strings.Contains(stdout.String(), "not_published") {
				t.Fatalf("%s output=%q", command, stdout.String())
			}
		})
	}
}

func TestWorkerCutoverSliceDoesNotGatePinAndClassifiesItsNewRow(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, config.WorkersFile), []byte("# amux-schema: workers/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (app{}).execute([]string{"--config-dir", dir, "worker", "cutover", "publish", "--generation", workerCutoverCLITestGeneration}); err != nil {
		t.Fatal(err)
	}
	if err := (app{}).execute([]string{"--config-dir", dir, "worker", "pin", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--thread", "T-alpha"}); err != nil {
		t.Fatalf("slice 1 unexpectedly gated worker pin: %v", err)
	}
	exported, err := config.InspectWorkerCutover(config.Directory{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Workers) != 1 || exported.Workers[0].Classification != "not_in_pre_cutover_manifest" || len(exported.Blockers) != 1 {
		t.Fatalf("post-publication classification=%+v", exported)
	}
	workers, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workers), "@amux-control\tworkers/v2\tdowngrade-fence") {
		t.Fatalf("worker pin did not preserve downgrade fence: %s", workers)
	}
}
