package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

func TestWorkerSpawnDryRunDoesNotResumeStartedOperation(t *testing.T) {
	for _, thread := range []string{"", "T-bound"} {
		t.Run(map[bool]string{true: "unbound", false: "bound"}[thread == ""], func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
			sum := sha256.Sum256([]byte(request))
			now := time.Now().UTC()
			record := config.OperationRecord{Key: "dry-spawn", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseCreatingThread, Resource: config.OperationResource{Kind: "worker", Thread: thread}, CreatedAt: now, UpdatedAt: now}
			if thread != "" {
				record.Phase = config.OperationPhaseThreadBound
			}
			path := filepath.Join(dir, config.OperationsFile)
			if _, err := config.StoreOperation(path, record); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "dry-spawn")
			if len(got.Planned) != 1 {
				t.Fatalf("dry-run result = %+v", got)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("operations changed: err=%v\nbefore=%s\nafter=%s", err, before, after)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("dry-run called amp or tmux: %v", err)
			}
		})
	}
}

func TestWorkerSpawnRejectsDuplicateIssueIdentityBeforeSideEffects(t *testing.T) {
	for _, test := range []struct {
		window     string
		suggestion string
	}{
		{window: "issue-119", suggestion: "<semantic-slug>"},
		{window: "issue-119-install-update-diagnostics", suggestion: "install-update-diagnostics"},
		{window: "#119", suggestion: "<semantic-slug>"},
		{window: "#119 install-update-diagnostics", suggestion: "install-update-diagnostics"},
	} {
		t.Run(test.window, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			var stdout bytes.Buffer
			err := (app{stdout: &stdout}).execute([]string{"--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", test.window, "--workdir", workdir, "--mode", "medium", "--title-prefix", "#119", "--message", "hello", "--idempotency-key", "duplicate-title"})
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "duplicates issue identity #119") || !strings.Contains(err.Error(), test.suggestion) {
				t.Fatalf("duplicate title error = %v, exit = %d", err, result.ExitCode(err))
			}
			var envelope result.Envelope
			if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if len(envelope.Failed) != 1 || envelope.Failed[0].Error == nil || envelope.Failed[0].Error.Kind != result.ErrorPreflight {
				t.Fatalf("duplicate title JSON = %+v", envelope)
			}
			if _, statErr := os.Stat(called); !os.IsNotExist(statErr) {
				t.Fatalf("duplicate title called amp or tmux: %v", statErr)
			}
			for _, name := range []string{config.WorkersFile, config.OperationsFile} {
				if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
					t.Fatalf("duplicate title created %s: %v", name, statErr)
				}
			}
		})
	}
}

func TestWorkerSpawnIssueTitleDryRunUsesSemanticWindow(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "install-update-diagnostics", "--workdir", workdir, "--mode", "medium", "--title-prefix", "#119", "--message", "hello", "--idempotency-key", "issue-title")
	if len(got.Planned) != 1 || !strings.Contains(got.Planned[0].Message, "alpha/#119 install-update-diagnostics") {
		t.Fatalf("issue title dry-run = %+v", got)
	}
}

func TestWorkerSpawnGenericTitlePrefixDoesNotApplyIssueRules(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "issue-119-install", "--workdir", workdir, "--title-prefix", "release", "--message", "hello", "--idempotency-key", "generic-title")
	if len(got.Planned) != 1 || !strings.Contains(got.Planned[0].Message, "alpha/release issue-119-install") {
		t.Fatalf("generic title dry-run = %+v", got)
	}
}

func TestWorkerSpawnDeliveryReplayNeverResubmitsOrSearchesOtherThreads(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "delivery-started",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationStarted,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-bound"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-bound"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: "T-bound"}, row)
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2 $3" = "threads export T-bound" ]; then printf '%s\n' '{"id":"T-bound","messages":[]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf '%s\t@1\t%s\n' worker `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "delivery-started")
	if err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("delivery replay error = %v", err)
	}
	updated, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "delivery-started")
	if loadErr != nil || !found || updated.State != config.OperationIndeterminate {
		t.Fatalf("delivery operation = %+v found=%t err=%v", updated, found, loadErr)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if len(log) != 0 {
		t.Fatalf("delivery replay performed external work:\n%s", log)
	}
}

func TestWorkerSpawnInterruptedAdoptionReplayFailsClosedWithBothThreadIdentities(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "adoption-started",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationStarted,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		ThreadAdoption: &config.OperationThreadAdoption{
			ProvisionedThread: "T-provisioned",
			ReceivingThread:   "T-receiving",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "adoption-started")
	if err == nil || !strings.Contains(err.Error(), "T-provisioned") || !strings.Contains(err.Error(), "T-receiving") || !strings.Contains(err.Error(), "do not resubmit") {
		t.Fatalf("interrupted adoption replay error = %v", err)
	}
	updated, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), record.Key)
	if loadErr != nil || !found || updated.State != config.OperationIndeterminate || updated.ThreadAdoption == nil {
		t.Fatalf("interrupted adoption operation = %+v found=%t err=%v", updated, found, loadErr)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("interrupted adoption replay called amp or tmux: %v", err)
	}
}

func TestWorkerListIsDeterministicLocalJSONAndFiltersShelfIntent(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir,
		"zeta\tz\t/tmp/z\tT-z\n"+
			"alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, "shelves.tsv"), []byte("# amux-schema: shelves/v1\nT-z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch "+called+"\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch "+called+"\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "list", "--shelf", "unshelved"})
	if err != nil {
		t.Fatal(err)
	}
	var got result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Successful) != 1 || got.Successful[0].Resource.Thread != "T-a" || got.Successful[0].Message != "unshelved" {
		t.Fatalf("worker list result = %+v", got)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("local worker list invoked amp or tmux: %v", err)
	}
}

func TestWorkerShelveWritesIntentBeforeArchiveAndRetriesRemoteRepair(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, "shelves.tsv"), []byte("# amux-schema: shelves/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "amp.log")
	attempt := filepath.Join(bin, "attempt")
	script := "#!/bin/sh\ngrep -q '^T-a$' '" + filepath.Join(dir, "shelves.tsv") + "' || exit 88\necho \"$*\" >> '" + log + "'\nif [ ! -e '" + attempt + "' ]; then touch '" + attempt + "'; exit 42; fi\n"
	writeExecutable(t, filepath.Join(bin, "amp"), script)
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "shelve", "--thread", "T-a"}); err == nil {
		t.Fatal("first archive succeeded, want injected failure")
	}
	stdout.Reset()
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "shelve", "--thread", "T-a"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "threads archive T-a"); got != 2 {
		t.Fatalf("archive calls = %d, log=%q", got, data)
	}
}

func TestWorkerPinIsIdempotentAndDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "pin", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--thread", "T-a"}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 1 || len(first.Skipped) != 0 {
		t.Fatalf("first pin = %+v", first)
	}
	second := executeWorkerJSON(t, args...)
	if len(second.Successful) != 0 || len(second.Skipped) != 1 || second.Skipped[0].Message != "already pinned" {
		t.Fatalf("second pin = %+v", second)
	}

	dryDir := t.TempDir()
	dryArgs := append([]string{"--dry-run"}, args...)
	for i, arg := range dryArgs {
		if arg == dir {
			dryArgs[i] = dryDir
		}
	}
	dry := executeWorkerJSON(t, dryArgs...)
	if len(dry.Planned) != 1 {
		t.Fatalf("dry-run pin = %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(dryDir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run pin wrote workers registry: %v", err)
	}
}

func TestWorkerPinTreatsCanonicalWorkdirAsAlreadyPinned(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t~/project\tT-a\n")
	t.Setenv("HOME", home)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "pin", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--thread", "T-a")
	if len(result.Skipped) != 1 || result.Skipped[0].Message != "already pinned" || len(result.Successful) != 0 {
		t.Fatalf("canonical pin result = %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil || !strings.Contains(string(data), "~/project") {
		t.Fatalf("canonical pin rewrote row: data=%q err=%v", data, err)
	}
}

func TestWorkerUnshelveRemovesIntentOnlyAfterRemoteSuccess(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	shelfPath := filepath.Join(dir, config.ShelvesFile)
	if err := os.WriteFile(shelfPath, []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	attempt := filepath.Join(bin, "attempt")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ngrep -q '^T-a$' '"+shelfPath+"' || exit 88\nif [ ! -e '"+attempt+"' ]; then touch '"+attempt+"'; exit 42; fi\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "unshelve", "--thread", "T-a"}

	if err := executeWorkerJSONError(t, args...); err == nil {
		t.Fatal("first unshelve succeeded, want injected remote failure")
	}
	if data, err := os.ReadFile(shelfPath); err != nil || !strings.Contains(string(data), "T-a\n") {
		t.Fatalf("failed unshelve removed intent: data=%q err=%v", data, err)
	}
	result := executeWorkerJSON(t, args...)
	if len(result.Successful) != 1 {
		t.Fatalf("retried unshelve = %+v", result)
	}
	if data, err := os.ReadFile(shelfPath); err != nil || strings.Contains(string(data), "T-a\n") {
		t.Fatalf("successful unshelve retained intent: data=%q err=%v", data, err)
	}
}

func TestWorkerSpawnPersistsOperationAndReplaysWithoutDuplicateCreation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	delivered := filepath.Join(bin, "delivered")
	renameAttempted := filepath.Join(bin, "rename-attempted")
	row := config.Row{Workspace: "alpha", Window: "#119 worker", Workdir: workdir, Thread: "T-spawned"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "#119 worker", Thread: "T-spawned"}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-spawned; exit 0; fi
if [ "$1 $2 $3" = "threads export T-spawned" ]; then
  if [ -e "`+delivered+`" ]; then printf '%s\n' '{"id":"T-spawned","messages":[{"role":"user","content":"hello"}]}'; else printf '%s\n' '{"id":"T-spawned","messages":[]}'; fi
  exit 0
fi
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-spawned"}]'; exit 0; fi
if [ "$1 $2" = "threads search" ]; then printf '%s\n' '[{"id":"T-spawned"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads rename T-spawned" ] && [ "$4" = "#119 worker" ]; then
  if [ ! -e "`+renameAttempted+`" ]; then touch "`+renameAttempted+`"; exit 42; fi
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf '%s\t@1\t%s\n' '#119 worker' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--title-prefix", "#119", "--message", "hello", "--idempotency-key", "spawn-1"}

	if err := executeWorkerJSONError(t, args...); err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("first spawn rename error = %v", err)
	}
	failedRecord, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "spawn-1")
	if err != nil || !found || failedRecord.State != config.OperationStarted || failedRecord.Phase != config.OperationPhaseMessageVerified {
		t.Fatalf("rename-failed operation = %+v found=%t err=%v", failedRecord, found, err)
	}
	if rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile)); loadErr != nil || len(rows) != 0 {
		t.Fatalf("rename failure stored worker rows = %+v err=%v", rows, loadErr)
	}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 1 || first.Successful[0].Resource.Thread != "T-spawned" {
		t.Fatalf("spawn result = %+v", first)
	}
	record, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "spawn-1")
	if err != nil || !found || record.State != config.OperationSucceeded || record.Resource.Thread != "T-spawned" {
		t.Fatalf("spawn operation = %+v found=%t err=%v", record, found, err)
	}
	second := executeWorkerJSON(t, args...)
	if len(second.Skipped) != 1 || second.Skipped[0].Resource.Thread != "T-spawned" {
		t.Fatalf("spawn replay = %+v", second)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(log), "amp threads new"); got != 1 {
		t.Fatalf("spawn creation calls = %d\n%s", got, log)
	}
	if got := strings.Count(string(log), "amp threads rename T-spawned #119 worker"); got != 2 {
		t.Fatalf("spawn rename calls = %d\n%s", got, log)
	}
	withoutPrefix := append([]string(nil), args...)
	for i := 0; i < len(withoutPrefix); i++ {
		if withoutPrefix[i] == "--window" {
			withoutPrefix[i+1] = "#119 worker"
		}
		if withoutPrefix[i] == "--title-prefix" {
			withoutPrefix = append(withoutPrefix[:i], withoutPrefix[i+2:]...)
			break
		}
	}
	if err := executeWorkerJSONError(t, withoutPrefix...); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("prefix rename intent key mismatch error = %v", err)
	}

	mismatch := append([]string(nil), args...)
	for i, arg := range mismatch {
		if arg == "hello" {
			mismatch[i] = "different"
		}
	}
	if err := executeWorkerJSONError(t, mismatch...); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("spawn key mismatch error = %v", err)
	}
}

func TestWorkerSpawnAdoptsSoleFreshActiveReceivingThread(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	delivered := filepath.Join(bin, "delivered")
	identity := filepath.Join(bin, "identity")
	oldRow := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-created"}
	newRow := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-receiver"}
	oldStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: oldRow.Thread}, oldRow)
	newStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: newRow.Thread}, newRow)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-created; exit 0; fi
if [ "$1 $2" = "threads list" ]; then
  if [ -e "`+delivered+`" ]; then printf '%s\n' '[{"id":"T-created"},{"id":"T-receiver"}]'; else printf '%s\n' '[{"id":"T-created"}]'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-created" ]; then printf '%s\n' '{"id":"T-created","messages":[]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export T-receiver" ]; then printf '%s\n' '{"id":"T-receiver","env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"hello"}]}'; exit 0; fi
if [ "$1 $2" = "threads search" ]; then printf '%s\n' '[{"id":"T-receiver"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads archive T-created" ]; then exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; echo T-created > "`+identity+`"; exit 0; fi
if [ "$1" = list-panes ]; then
  if grep -q T-receiver "`+identity+`"; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(newStart)+`; else printf 'worker\t@1\t%s\n' `+shellSingleQuote(oldStart)+`; fi
  exit 0
fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
if [ "$1" = respawn-pane ]; then echo T-receiver > "`+identity+`"; exit 0; fi
if [ "$1" = kill-window ]; then rm -f "`+running+`"; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "adopt-1"}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 1 || first.Successful[0].Resource.Thread != "T-receiver" {
		t.Fatalf("adopted spawn result = %+v", first)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Thread != "T-receiver" {
		t.Fatalf("adopted worker registry = %+v err=%v", rows, err)
	}
	record, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "adopt-1")
	if err != nil || !found || record.State != config.OperationSucceeded || record.Resource.Thread != "T-receiver" {
		t.Fatalf("adopted operation = %+v found=%t err=%v", record, found, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tmux respawn-pane -k -t @1", "amp threads archive T-created"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("adoption log missing %q:\n%s", want, log)
		}
	}
	beforeReplay := string(log)
	second := executeWorkerJSON(t, args...)
	if len(second.Skipped) != 1 || second.Skipped[0].Resource.Thread != "T-receiver" {
		t.Fatalf("adopted spawn replay = %+v", second)
	}
	afterReplay, err := os.ReadFile(logPath)
	if err != nil || string(afterReplay) != beforeReplay {
		t.Fatalf("adopted replay performed external work: err=%v\nbefore=%s\nafter=%s", err, beforeReplay, afterReplay)
	}
}

func TestWorkerSpawnAlternateDeliveryFailsClosedWhenOwnershipIsAmbiguous(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
	}{
		{name: "archived", wantErr: "is archived"},
		{name: "duplicate", wantErr: "multiple fresh receiving threads T-receiver, T-second"},
		{name: "bound-duplicate", wantErr: "identity conflict between provisioned thread T-created and fresh receiving thread(s) T-receiver"},
		{name: "delayed-bound-duplicate", wantErr: "identity conflict between provisioned thread T-created and fresh receiving thread(s) T-receiver"},
		{name: "timeout", wantErr: "list fresh receiving threads after delivery"},
		{name: "identity-conflict", wantErr: "receiving thread T-receiver is already configured as beta/existing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			bin := t.TempDir()
			logPath := filepath.Join(bin, "calls.log")
			running := filepath.Join(bin, "running")
			delivered := filepath.Join(bin, "delivered")
			listCount := filepath.Join(bin, "list-count")
			row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-created"}
			start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: row.Thread}, row)
			if tt.name == "identity-conflict" {
				writeWorkerRegistry(t, dir, config.Row{Workspace: "beta", Window: "existing", Workdir: workdir, Thread: "T-receiver"}.String()+"\n")
			}
			writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-created; exit 0; fi
if [ "$1 $2" = "threads list" ]; then
  count=0; if [ -f "`+listCount+`" ]; then count=$(cat "`+listCount+`"); fi; count=$((count + 1)); printf '%s\n' "$count" > "`+listCount+`"
  if [ ! -e "`+delivered+`" ]; then printf '%s\n' '[{"id":"T-created"}]'; exit 0; fi
  if [ "`+tt.name+`" = timeout ]; then echo 'thread list timed out' >&2; exit 1; fi
  if [ "`+tt.name+`" = delayed-bound-duplicate ] && [ "$count" -lt 4 ]; then printf '%s\n' '[{"id":"T-created"}]'; exit 0; fi
  if [ "`+tt.name+`" = archived ] && ! echo "$*" | grep -q -- --include-archived; then printf '%s\n' '[{"id":"T-created"}]'; exit 0; fi
  if [ "`+tt.name+`" = duplicate ]; then printf '%s\n' '[{"id":"T-created"},{"id":"T-receiver"},{"id":"T-second"}]'; else printf '%s\n' '[{"id":"T-created"},{"id":"T-receiver"}]'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-created" ]; then
  if { [ "`+tt.name+`" = bound-duplicate ] || [ "`+tt.name+`" = delayed-bound-duplicate ]; } && [ -e "`+delivered+`" ]; then printf '%s\n' '{"id":"T-created","messages":[{"role":"user","content":"hello"}]}'; else printf '%s\n' '{"id":"T-created","messages":[]}'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-receiver" ] || [ "$1 $2 $3" = "threads export T-second" ]; then
  printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"hello"}]}'
  exit 0
fi
if [ "$1 $2" = "threads search" ]; then
  if [ "`+tt.name+`" = timeout ]; then echo 'search timed out' >&2; exit 1; fi
  if [ "`+tt.name+`" = duplicate ]; then printf '%s\n' '[{"id":"T-receiver"},{"id":"T-second"}]'; else printf '%s\n' '[{"id":"T-receiver"}]'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads archive T-created" ]; then exit 0; fi
exit 2
`)
			writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
if [ "$1" = kill-window ]; then rm -f "`+running+`"; exit 0; fi
exit 2
`)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
			args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "ambiguous-1"}

			err := executeWorkerJSONError(t, args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "recovery:") {
				t.Fatalf("ambiguous spawn error = %v, want %q with recovery", err, tt.wantErr)
			}
			rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
			wantRows := 0
			if tt.name == "identity-conflict" {
				wantRows = 1
			}
			if loadErr != nil || len(rows) != wantRows {
				t.Fatalf("ambiguous spawn registry = %+v err=%v", rows, loadErr)
			}
			record, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "ambiguous-1")
			if loadErr != nil || !found || record.State != config.OperationIndeterminate {
				t.Fatalf("ambiguous operation = %+v found=%t err=%v", record, found, loadErr)
			}
			log, readErr := os.ReadFile(logPath)
			if readErr != nil || !strings.Contains(string(log), "tmux kill-window -t @1") || strings.Contains(string(log), "amp threads archive") {
				t.Fatalf("ambiguous spawn did not stop unconfigured window: err=%v\n%s", readErr, log)
			}
			beforeReplay := string(log)
			if replayErr := executeWorkerJSONError(t, args...); replayErr == nil || !strings.Contains(replayErr.Error(), "terminal in state indeterminate") {
				t.Fatalf("ambiguous replay error = %v", replayErr)
			}
			afterReplay, readErr := os.ReadFile(logPath)
			if readErr != nil || string(afterReplay) != beforeReplay {
				t.Fatalf("ambiguous replay performed external work: err=%v\nbefore=%s\nafter=%s", readErr, beforeReplay, afterReplay)
			}
		})
	}
}

func TestWorkerSpawnRecoversCanonicalExactRow(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t~/project\tT-bound\n")
	t.Setenv("HOME", home)
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{Key: "canonical-recovery", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseMessageVerified, Resource: config.OperationResource{Kind: "worker", Thread: "T-bound"}, CreatedAt: now, UpdatedAt: now}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "canonical-recovery")
	if len(result.Skipped) != 1 || result.Skipped[0].Message != "recovered completed spawn" {
		t.Fatalf("canonical recovery result = %+v", result)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("canonical recovery called amp or tmux: %v", err)
	}
}

func TestWorkerRemoveDoesNotArchive(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "amp-called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	removed := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "remove", "--thread", "T-a")
	if len(removed.Successful) != 1 {
		t.Fatalf("remove result = %+v", removed)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("worker remove changed remote archive state: %v", err)
	}
}

func TestWorkerTeardownRequiresExactlyOneConfiguredWorker(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\nalpha\tb\t/tmp/b\tT-b\n")
	shelvesPath := filepath.Join(dir, config.ShelvesFile)
	if err := os.WriteFile(shelvesPath, []byte("# amux-schema: shelves/v1\nT-a\nT-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workersBefore, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil {
		t.Fatal(err)
	}
	shelvesBefore, err := os.ReadFile(shelvesPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var stdout bytes.Buffer
	err = (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "teardown", "--workspace", "alpha"})
	if err == nil || !strings.Contains(err.Error(), "exactly one configured worker") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("multi-worker teardown error = %v, want rejected exact-one preflight", err)
	}
	var got result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&got); decodeErr != nil {
		t.Fatalf("decode multi-worker teardown: %v\nstdout: %s", decodeErr, stdout.String())
	}
	if len(got.Failed) != 1 || got.Failed[0].Error == nil || got.Failed[0].Error.Kind != result.ErrorPreflight {
		t.Fatalf("multi-worker teardown result = %+v", got)
	}
	if _, statErr := os.Stat(called); !os.IsNotExist(statErr) {
		t.Fatalf("multi-worker teardown called amp or tmux: %v", statErr)
	}
	workersAfter, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil || !bytes.Equal(workersBefore, workersAfter) {
		t.Fatalf("multi-worker teardown changed workers: err=%v\nbefore=%s\nafter=%s", err, workersBefore, workersAfter)
	}
	shelvesAfter, err := os.ReadFile(shelvesPath)
	if err != nil || !bytes.Equal(shelvesBefore, shelvesAfter) {
		t.Fatalf("multi-worker teardown changed shelves: err=%v\nbefore=%s\nafter=%s", err, shelvesBefore, shelvesAfter)
	}
}

func TestWorkerTeardownCompletesWhenLocalWorkerIsAlreadyStopped(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> '"+logPath+"'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"tmux $*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "teardown", "--thread", "T-a")
	if len(got.Successful) != 0 || len(got.Skipped) != 1 || got.Skipped[0].Message != "already_stopped" {
		t.Fatalf("missing-window teardown result = %+v", got)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 0 {
		t.Fatalf("missing-window teardown workers = %+v err=%v", rows, err)
	}
	shelves, err := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
	if err != nil || len(shelves) != 0 {
		t.Fatalf("missing-window teardown shelves = %+v err=%v", shelves, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "amp threads archive T-a") || strings.Contains(string(log), "tmux kill-window") {
		t.Fatalf("missing-window teardown calls:\n%s", log)
	}
}

func TestWorkerTeardownFailsClosedForUnverifiedLiveWorker(t *testing.T) {
	row := config.Row{Workspace: "alpha", Window: "a", Workdir: "/tmp/a", Thread: "T-a"}
	exact := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "a", Thread: "T-a"}, row)
	for _, tt := range []struct {
		name  string
		panes string
		want  string
	}{
		{name: "mismatched", panes: "a\t@1\tamp threads continue T-other\n", want: "conflict tmux identity"},
		{name: "ambiguous", panes: "a\t@1\t" + exact + "\na\t@1\t" + exact + "\n", want: "ambiguous tmux identity"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWorkerRegistry(t, dir, row.String()+"\n")
			if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			logPath := filepath.Join(bin, "calls.log")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> '"+logPath+"'\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"tmux $*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 0; fi\nif [ \"$1\" = list-panes ]; then printf %s "+shellSingleQuote(tt.panes)+"; exit 0; fi\nexit 2\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "teardown", "--thread", "T-a")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("teardown error = %v, want %q", err, tt.want)
			}
			log, readErr := os.ReadFile(logPath)
			if readErr != nil || strings.Contains(string(log), "amp threads archive") || strings.Contains(string(log), "tmux kill-window") {
				t.Fatalf("unverified teardown performed mutation: err=%v\n%s", readErr, log)
			}
			rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
			if loadErr != nil || len(rows) != 1 {
				t.Fatalf("unverified teardown workers = %+v err=%v", rows, loadErr)
			}
			shelves, loadErr := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
			if loadErr != nil || len(shelves) != 1 {
				t.Fatalf("unverified teardown shelves = %+v err=%v", shelves, loadErr)
			}
		})
	}
}

func TestWorkerTeardownArchivesRemovesAndStopsLiveWorker(t *testing.T) {
	dir := t.TempDir()
	row := config.Row{Workspace: "alpha", Window: "a", Workdir: "/tmp/a", Thread: "T-a"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "a", Thread: "T-a"}, row)
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> '"+logPath+"'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"tmux $*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 0; fi\nif [ \"$1\" = list-panes ]; then printf '%s\\n' "+shellSingleQuote("a\t@1\t"+start)+"; exit 0; fi\nif [ \"$1\" = kill-window ]; then exit 0; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "teardown", "--thread", "T-a")
	if len(got.Successful) != 1 || len(got.Skipped) != 0 {
		t.Fatalf("live-window teardown result = %+v", got)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 0 {
		t.Fatalf("live-window teardown workers = %+v err=%v", rows, err)
	}
	shelves, err := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
	if err != nil || len(shelves) != 0 {
		t.Fatalf("live-window teardown shelves = %+v err=%v", shelves, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"amp threads archive T-a", "tmux kill-window -t @1"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("live-window teardown missing %q:\n%s", want, log)
		}
	}
}

func TestWorkerPinCurrentUsesCompleteInjectedIdentity(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMUX_WORKSPACE", "alpha")
	t.Setenv("AMUX_SESSION", "alpha")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-current")
	t.Setenv("AMUX_WORKDIR", workdir)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "pin", "--current")
	if len(result.Successful) != 1 || result.Successful[0].Resource.Thread != "T-current" {
		t.Fatalf("pin current = %+v", result)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Workdir != workdir {
		t.Fatalf("current row = %+v err=%v", rows, err)
	}
}

func TestWorkerCurrentMatchesMigratedHomeRelativeWorkdirCanonically(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t~/project\tT-current\n")
	t.Setenv("HOME", home)
	t.Setenv("AMUX_WORKSPACE", "alpha")
	t.Setenv("AMUX_SESSION", "alpha")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-current")
	t.Setenv("AMUX_WORKDIR", workdir)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "list", "--current")
	if len(result.Successful) != 1 || result.Successful[0].Resource.Thread != "T-current" {
		t.Fatalf("current migrated worker = %+v", result)
	}
}

func TestWorkerDryRunKeepsKnownNoOpsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t/tmp/project\tT-current\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	launch := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "launch", "--thread", "T-current")
	if len(launch.Skipped) != 1 || len(launch.Planned) != 0 {
		t.Fatalf("dry-run shelved launch = %+v", launch)
	}

	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unshelve := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "unshelve", "--thread", "T-current")
	if len(unshelve.Skipped) != 1 || len(unshelve.Planned) != 0 {
		t.Fatalf("dry-run unshelved worker = %+v", unshelve)
	}
}

func TestWorkerRestartSkipsAbsentAndShelvedWorkers(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, state := range []string{"absent", "shelved"} {
			t.Run(state+map[bool]string{true: "-dry-run", false: ""}[dryRun], func(t *testing.T) {
				dir := t.TempDir()
				workdir := t.TempDir()
				writeWorkerRegistry(t, dir, "alpha\tworker\t"+workdir+"\tT-a\n")
				if state == "shelved" {
					if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				bin := t.TempDir()
				logPath := filepath.Join(bin, "tmux.log")
				writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"$*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				args := []string{"--json", "--config-dir", dir, "worker", "restart", "--thread", "T-a"}
				if dryRun {
					args = append([]string{"--dry-run"}, args...)
				}

				result := executeWorkerJSON(t, args...)
				if len(result.Skipped) != 1 || len(result.Planned) != 0 || len(result.Successful) != 0 {
					t.Fatalf("restart result = %+v", result)
				}
				log, err := os.ReadFile(logPath)
				if state == "shelved" {
					if !os.IsNotExist(err) {
						t.Fatalf("shelved restart touched tmux: %q err=%v", log, err)
					}
				} else if err != nil || strings.Contains(string(log), "new-session") || strings.Contains(string(log), "new-window") || strings.Contains(string(log), "kill-window") {
					t.Fatalf("absent restart mutated tmux: %q err=%v", log, err)
				}
			})
		}
	}
}

func TestWorkerRemovePlansAndReportsStaleShelfCleanup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "remove", "--thread", "T-stale"}

	dry := executeWorkerJSON(t, append([]string{"--dry-run"}, args...)...)
	if len(dry.Planned) != 1 || len(dry.Skipped) != 0 {
		t.Fatalf("dry-run stale shelf removal = %+v", dry)
	}
	actual := executeWorkerJSON(t, args...)
	if len(actual.Successful) != 1 || len(actual.Skipped) != 0 {
		t.Fatalf("stale shelf removal = %+v", actual)
	}
	shelves, err := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
	if err != nil || len(shelves) != 0 {
		t.Fatalf("remaining shelves = %v err=%v", shelves, err)
	}
}

func TestWorkerRemoveAllCleansShelfOnlyIntentAndEmptyInventoryIsNoOp(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{true: "dry-run", false: "apply"}[dryRun], func(t *testing.T) {
			dir := t.TempDir()
			shelfPath := filepath.Join(dir, config.ShelvesFile)
			if err := os.WriteFile(shelfPath, []byte("# amux-schema: shelves/v1\nT-stale-b\nT-stale-a\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--json", "--config-dir", dir, "worker", "remove", "--all"}
			if dryRun {
				args = append([]string{"--dry-run"}, args...)
			}
			result := executeWorkerJSON(t, args...)
			outcomes := result.Successful
			if dryRun {
				outcomes = result.Planned
			}
			if len(outcomes) != 2 || outcomes[0].Resource.Thread != "T-stale-a" || outcomes[1].Resource.Thread != "T-stale-b" {
				t.Fatalf("remove --all result = %+v", result)
			}
			shelves, err := config.LoadShelvesReadOnly(shelfPath)
			if err != nil || dryRun && len(shelves) != 2 || !dryRun && len(shelves) != 0 {
				t.Fatalf("remaining shelves = %v err=%v", shelves, err)
			}
		})
	}

	empty := executeWorkerJSON(t, "--json", "--config-dir", t.TempDir(), "worker", "remove", "--all")
	if len(empty.Skipped) != 1 || empty.Skipped[0].Message != "already in desired state" {
		t.Fatalf("empty remove --all = %+v", empty)
	}
}

func TestBareAmuxLaunchesWorkersButExplicitAggregateLaunchStaysReserved(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMUX_CONFIG_DIR", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (app{}).execute(nil); err != nil {
		t.Fatalf("bare amux worker launch: %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("shelved bare launch invoked amp or tmux: %v", err)
	}
	if err := (app{}).execute([]string{"launch"}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("explicit aggregate launch error = %v", err)
	}
}

func TestWorkerLaunchPreflightsEveryWorkdirBeforeTmuxMutation(t *testing.T) {
	dir := t.TempDir()
	valid := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	writeWorkerRegistry(t, dir, "alpha\tone\t"+valid+"\tT-one\nalpha\ttwo\t"+missing+"\tT-two\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "tmux-called")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "launch", "--all")
	if err == nil || !strings.Contains(err.Error(), "missing workdir") {
		t.Fatalf("bulk launch preflight error = %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("bulk launch mutated before complete workdir preflight: %v", err)
	}
}

func TestCanonicalWorkerCompletionsAreLeafSpecific(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute([]string{"completion", shell}); err != nil {
				t.Fatal(err)
			}
			output := stdout.String()
			for _, want := range []string{"unpin", "spawn"} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing %q\n%s", shell, want, output)
				}
			}
			idempotencyFlag := "--idempotency-key"
			messageFileFlag := "--message-file"
			titlePrefixFlag := "--title-prefix"
			if shell == "fish" {
				idempotencyFlag = "-l 'idempotency-key'"
				messageFileFlag = "-l 'message-file'"
				titlePrefixFlag = "-r -l 'title-prefix'"
			}
			for _, want := range []string{idempotencyFlag, messageFileFlag, titlePrefixFlag} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing %q\n%s", shell, want, output)
				}
			}
			aliases := []string{"-w", "-W", "-d", "-t"}
			if shell == "fish" {
				aliases = []string{"-s 'w'", "-s 'W'", "-s 'd'", "-s 't'"}
			}
			for _, want := range aliases {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing selector alias %q\n%s", shell, want, output)
				}
			}
			if shell == "bash" {
				if !strings.Contains(output, `unpin) COMPREPLY=( $(compgen -W "--thread --current -t"`) {
					t.Fatalf("bash unpin completion is not leaf-specific:\n%s", output)
				}
				if !strings.Contains(output, `if [[ "$word" == --config-dir || "$word" == -c ]]; then ((i++)); continue; fi`) {
					t.Fatalf("bash completion does not skip global config value:\n%s", output)
				}
			}
			if shell == "zsh" && !strings.Contains(output, `unpin) _arguments '--thread[thread id or URL]:thread:' '--current[current worker]'`) {
				t.Fatalf("zsh unpin completion is not leaf-specific:\n%s", output)
			}
			if shell == "zsh" && strings.Count(output, `'--title-prefix[window and thread title prefix]:prefix:'`) != 2 {
				t.Fatalf("zsh worker and top-level spawn completions do not both require --title-prefix values:\n%s", output)
			}
			if shell == "zsh" && (!strings.Contains(output, `'-c[path to config directory]:directory:_directories'`) || !strings.Contains(output, `--config-dir|-c) (( i += 2 )); continue`)) {
				t.Fatalf("zsh completion does not resolve short global prefixes:\n%s", output)
			}
		})
	}
}

func executeWorkerJSON(t *testing.T, args ...string) result.Envelope {
	t.Helper()
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute(args); err != nil {
		t.Fatalf("execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	var envelope result.Envelope
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	return envelope
}

func executeWorkerJSONError(t *testing.T, args ...string) error {
	t.Helper()
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(args)
	if err == nil {
		return nil
	}
	var envelope result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode failed execute(%q): %v\nstdout: %s", args, decodeErr, stdout.String())
	}
	return err
}

func writeWorkerRegistry(t *testing.T, dir, rows string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "workers.tsv"), []byte("# amux-schema: workers/v1\n"+rows), 0o600); err != nil {
		t.Fatal(err)
	}
}
