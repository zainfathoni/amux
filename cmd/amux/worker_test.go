package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

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

func TestWorkerAdoptPersistsIntentBeforeTmuxAndIsIdempotentWithoutDelivery(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	failOnce := filepath.Join(bin, "fail-once")
	row := config.Row{Workspace: "alpha", Window: "native", Workdir: workdir, Thread: "T-native"}
	admissionWorkdir := workdir + string(os.PathSeparator) + "unused" + string(os.PathSeparator) + ".."
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> `+shellSingleQuote(logPath)+`
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-native"}]'; exit 0; fi
if [ "$1" = version ]; then printf '%s\n' '`+minimumGroupAmpVersion+`'; exit 0; fi
if [ "$1 $2 $3" = "threads label --help" ]; then
  printf '%s\n' '`+groupLabelUsageLine+`' '`+groupLabelAdditiveLine+`'
  exit 0
fi
if [ "$1 $2 $3 $4" = "threads label T-native native-group" ]; then exit 0; fi
exit 97
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> `+shellSingleQuote(logPath)+`
case "$1" in
  has-session) [ -e `+shellSingleQuote(running)+` ] && exit 0; echo "can't find session" >&2; exit 1 ;;
  list-panes)
    [ -e `+shellSingleQuote(running)+` ] || exit 0
    if [ "$2" = -a ]; then
      printf '%s\n' `+shellSingleQuote("alpha\tnative\t@1\t"+start)+`
    else
      printf '%s\n' `+shellSingleQuote("native\t@1\t"+start)+`
    fi
    exit 0 ;;
  new-session)
    if [ ! -e `+shellSingleQuote(failOnce)+` ]; then touch `+shellSingleQuote(failOnce)+`; exit 42; fi
    touch `+shellSingleQuote(running)+`; exit 0 ;;
esac
exit 98
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "adopt", "--thread", row.Thread, "--workspace", row.Workspace, "--window", row.Window, "--workdir", admissionWorkdir, "--group", "native-group"}

	dry := executeWorkerJSON(t, append([]string{"--dry-run"}, args...)...)
	if len(dry.Planned) != 5 || dry.Planned[0].Worker == nil || dry.Planned[0].Worker.ReceiptSource != nativeAdoptionReceiptSource || dry.Planned[0].Worker.Workdir != workdir || dry.Planned[0].Worker.NativeExecutor != unknownNativePlacement || dry.Planned[0].Worker.NativeRunnerID != unknownNativePlacement || dry.Planned[0].Worker.ExecutionAffinity != unknownNativePlacement || dry.Planned[3].Action != "ensure-label" || dry.Planned[4].Action != "create-client" {
		t.Fatalf("adoption dry run = %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(dir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote worker intent: %v", err)
	}

	interrupted, interruptionErr := executeWorkerJSONResult(t, args...)
	if interruptionErr == nil || result.ExitCode(interruptionErr) != result.ExitRuntimeFailure || len(interrupted.Successful) != 4 || interrupted.Successful[0].Action != "bind-adoption-request" || interrupted.Successful[1].Action != "persist-worker" || interrupted.Successful[2].Action != "persist-group" || interrupted.Successful[3].Action != "attach-group" {
		t.Fatalf("injected tmux interruption = %v, exit=%d, envelope=%+v", interruptionErr, result.ExitCode(interruptionErr), interrupted)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0] != row {
		t.Fatalf("persisted worker intent = %+v, err=%v", rows, err)
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || len(memberships) != 1 || memberships[0].Group != "native-group" || memberships[0].Thread != row.Thread {
		t.Fatalf("persisted group intent = %+v, err=%v", memberships, err)
	}
	partialDry := executeWorkerJSON(t, append([]string{"--dry-run"}, args...)...)
	if len(partialDry.Planned) != 2 || partialDry.Planned[0].Action != "ensure-label" || partialDry.Planned[1].Action != "create-client" || len(partialDry.Skipped) != 3 {
		t.Fatalf("partial adoption dry run = %+v", partialDry)
	}
	changed := append([]string(nil), args...)
	changed[len(changed)-1] = "changed-group"
	if err := executeWorkerJSONError(t, changed...); err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "bound to different") {
		t.Fatalf("changed-group replay = %v, exit=%d", err, result.ExitCode(err))
	}

	completed := executeWorkerJSON(t, args...)
	if len(completed.Successful) != 3 || completed.Successful[2].Worker == nil || completed.Successful[2].Worker.LocalState != "adopted" || completed.Successful[2].Worker.Workdir != workdir {
		t.Fatalf("completed adoption = %+v", completed)
	}
	beforeReplay, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	replayed := executeWorkerJSON(t, args...)
	if len(replayed.Skipped) != 1 || replayed.Skipped[0].Message != "exact native-created thread already adopted" {
		t.Fatalf("replayed adoption = %+v", replayed)
	}
	afterReplay, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(afterReplay), "amp threads label T-native native-group") != strings.Count(string(beforeReplay), "amp threads label T-native native-group") {
		t.Fatalf("idempotent adoption repeated label mutation:\n%s", afterReplay)
	}
	if len(afterReplay) <= len(beforeReplay) {
		t.Fatal("replay did not perform read-only preflight")
	}
	for _, forbidden := range []string{"threads new", "threads export", "threads search", "send-keys", "paste-buffer", "load-buffer", " Enter"} {
		if strings.Contains(string(afterReplay), forbidden) {
			t.Fatalf("adoption used forbidden delivery/TUI call %q:\n%s", forbidden, afterReplay)
		}
	}
}

func TestWorkerAdoptRejectsCatalogOwnershipConflictsBeforeExternalInspection(t *testing.T) {
	for _, test := range []struct {
		name    string
		workers string
		runners string
		want    string
	}{
		{name: "window", workers: "alpha\tnative\t/tmp/other\tT-other\n", want: "worker window alpha/native"},
		{name: "thread", workers: "other\tother\t/tmp/other\tT-native\n", want: "thread T-native is already configured"},
		{name: "worker-workdir", workers: "other\tother\tWORKDIR\tT-other\n", want: "already owned by worker"},
		{name: "runner-workdir", runners: "runner\tWORKDIR\n", want: "already owned by amux Runner"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			if test.workers != "" {
				writeWorkerRegistry(t, dir, strings.ReplaceAll(test.workers, "WORKDIR", workdir))
			}
			if test.runners != "" {
				if err := os.WriteFile(filepath.Join(dir, config.RunnersFile), []byte("# amux-schema: runners/v1\n"+strings.ReplaceAll(test.runners, "WORKDIR", workdir)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch "+shellSingleQuote(called)+"\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch "+shellSingleQuote(called)+"\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "adopt", "--thread", "T-native", "--workspace", "alpha", "--window", "native", "--workdir", workdir)
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("conflict error = %v, exit=%d, want %q", err, result.ExitCode(err), test.want)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("catalog conflict called amp or tmux: %v", err)
			}
		})
	}
}

func TestWorkerAdoptRejectsInactiveExactThreadWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> `+shellSingleQuote(logPath)+`
case "$*" in
  *--include-archived*) printf '%s\n' '[{"id":"T-native"}]' ;;
  *) printf '%s\n' '[]' ;;
esac
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho tmux >> "+shellSingleQuote(logPath)+"\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "adopt", "--thread", "T-native", "--workspace", "alpha", "--window", "native", "--workdir", workdir)
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "thread T-native is archived") {
		t.Fatalf("inactive error = %v, exit=%d", err, result.ExitCode(err))
	}
	if log, readErr := os.ReadFile(logPath); readErr != nil || strings.Contains(string(log), "tmux") {
		t.Fatalf("inactive adoption reached tmux: log=%q err=%v", log, readErr)
	}
	if _, err := os.Stat(filepath.Join(dir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("inactive adoption wrote worker registry: %v", err)
	}
}

func TestWorkerAdoptRejectsShelvedAndPendingSpawnOwnershipBeforeExternalInspection(t *testing.T) {
	for _, test := range []string{"shelved", "pending-spawn"} {
		t.Run(test, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			if test == "shelved" {
				if _, err := config.StoreShelf(filepath.Join(dir, config.ShelvesFile), "T-native"); err != nil {
					t.Fatal(err)
				}
				writeWorkerRegistry(t, dir, "alpha\tnative\t"+workdir+"\tT-native\n")
			} else {
				now := time.Now().UTC()
				record := config.OperationRecord{Key: "legacy-spawn", Kind: "worker-spawn", RequestHash: "hash", State: config.OperationStarted, Phase: config.OperationPhaseThreadBound, Resource: config.OperationResource{Kind: "worker", Thread: "T-native"}, CreatedAt: now, UpdatedAt: now}
				if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
					t.Fatal(err)
				}
			}
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch "+shellSingleQuote(called)+"\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch "+shellSingleQuote(called)+"\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "adopt", "--thread", "T-native", "--workspace", "alpha", "--window", "native", "--workdir", workdir)
			want := "locally shelved"
			if test == "pending-spawn" {
				want = "still owned by immutable worker-spawn operation"
			}
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), want) {
				t.Fatalf("ownership error = %v, exit=%d, want=%q", err, result.ExitCode(err), want)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("ownership conflict called amp or tmux: %v", err)
			}
		})
	}
}

func TestWorkerDoctorReportsOnlyLegacyOperationWithoutMutationOrSubprocess(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	record := config.OperationRecord{Key: "legacy-evidence", Kind: "worker-spawn", RequestHash: "sensitive-hash", State: config.OperationIndeterminate, Phase: config.OperationPhaseDeliveryStarted, Resource: config.OperationResource{Kind: "worker", Thread: "T-legacy"}, SubmissionStatus: config.OperationSubmissionEnterAttempted, DeliveryStatus: config.OperationDeliveryUnknown, CreatedAt: now, UpdatedAt: now}
	operationsPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationsPath, record); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(operationsPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	for _, name := range []string{"amp", "tmux"} {
		writeExecutable(t, filepath.Join(bin, name), "#!/bin/sh\ntouch "+shellSingleQuote(called)+"\nexit 99\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	envelope := executeWorkerJSON(t, "--json", "--config-dir", dir, "doctor", "--all")
	if len(envelope.Successful) != 1 {
		t.Fatalf("doctor outcomes = %+v", envelope)
	}
	message := envelope.Successful[0].Message
	for _, want := range []string{"key=\"legacy-evidence\"", "state=indeterminate", "phase=delivery_started", "thread=\"T-legacy\"", "immutable-read-only-no-retry"} {
		if !strings.Contains(message, want) {
			t.Fatalf("diagnostic missing %q: %s", want, message)
		}
	}
	if strings.Contains(message, "sensitive-hash") {
		t.Fatalf("diagnostic leaked request hash: %s", message)
	}
	after, err := os.ReadFile(operationsPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("operations changed: err=%v\nbefore=%q\nafter=%q", err, before, after)
	}
	if _, err := os.Stat(called); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("doctor invoked Amp or tmux: %v", err)
	}
}

func TestWorkerDoctorScopesLegacyOperationEvidence(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	for _, record := range []config.OperationRecord{
		{Key: "first-evidence", Kind: "worker-spawn", RequestHash: "first", State: config.OperationIndeterminate, Phase: config.OperationPhaseThreadBound, Resource: config.OperationResource{Kind: "worker", Thread: "T-first"}, CreatedAt: now, UpdatedAt: now},
		{Key: "second-evidence", Kind: "worker-spawn", RequestHash: "second", State: config.OperationIndeterminate, Phase: config.OperationPhaseThreadBound, Resource: config.OperationResource{Kind: "worker", Thread: "T-second"}, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
			t.Fatal(err)
		}
	}

	matched := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "doctor", "--thread", "T-first")
	if len(matched.Successful) != 1 || !strings.Contains(matched.Successful[0].Message, "first-evidence") || strings.Contains(matched.Successful[0].Message, "second-evidence") {
		t.Fatalf("scoped legacy diagnostics = %+v", matched)
	}

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "doctor", "--thread", "T-unrelated")
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "no configured worker matches") {
		t.Fatalf("unrelated doctor error = %v, exit=%d", err, result.ExitCode(err))
	}
}

func TestWorkerAdoptPostVerifiesTmuxSuccessBeforeCompletion(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\nprintf '%s\\n' '[{\"id\":\"T-native\"}]'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) echo "can't find session" >&2; exit 1 ;;
  list-panes) exit 0 ;;
  new-session) exit 0 ;;
esac
exit 98
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	env, err := executeWorkerJSONResult(t, "--json", "--config-dir", dir, "worker", "adopt", "--thread", "T-native", "--workspace", "alpha", "--window", "native", "--workdir", workdir)
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure || !strings.Contains(err.Error(), "post-verify") || len(env.Successful) != 2 {
		t.Fatalf("unverified tmux success = %v, exit=%d, envelope=%+v", err, result.ExitCode(err), env)
	}
	record, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "worker-adopt:T-native")
	if loadErr != nil || !found || record.State != config.OperationStarted {
		t.Fatalf("interrupted adoption record = %+v, found=%t, err=%v", record, found, loadErr)
	}
}

func TestWorkerAdoptRejectsCoordinatorAsMemberIntent(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "native-group", Thread: "T-native", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\nprintf '%s\\n' '[{\"id\":\"T-native\"}]'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then echo \"can't find session\" >&2; exit 1; fi\nif [ \"$1\" = list-panes ]; then exit 0; fi\nexit 98\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "adopt", "--thread", "T-native", "--workspace", "alpha", "--window", "native", "--workdir", workdir, "--group", "native-group")
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "already has coordinator role") {
		t.Fatalf("coordinator member-intent error = %v, exit=%d", err, result.ExitCode(err))
	}
	if _, err := os.Stat(filepath.Join(dir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("coordinator conflict wrote worker registry: %v", err)
	}
}

func TestWorkerAdoptRejectsMismatchedTmuxThreadBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> "+shellSingleQuote(logPath)+"\nprintf '%s\\n' '[{\"id\":\"T-native\"}]'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> `+shellSingleQuote(logPath)+`
case "$1" in
  has-session) exit 0 ;;
  list-panes)
    if [ "$2" = -a ]; then
      printf '%s\n' 'alpha	native	@7	cd `+workdir+` && exec amp threads continue T-other'
    else
      printf '%s\n' 'native	@7	cd `+workdir+` && exec amp threads continue T-other'
    fi ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "adopt", "--thread", "T-native", "--workspace", "alpha", "--window", "native", "--workdir", workdir)
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "conflict tmux identity") {
		t.Fatalf("mismatched tmux error = %v, exit=%d", err, result.ExitCode(err))
	}
	if _, err := os.Stat(filepath.Join(dir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("mismatched tmux adoption wrote worker registry: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil || strings.Contains(string(log), "new-session") || strings.Contains(string(log), "new-window") {
		t.Fatalf("mismatched tmux adoption mutated tmux: log=%q err=%v", log, err)
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

func TestBareAmuxAndExplicitAggregateLaunchPreserveWorkerOnlyWorkspace(t *testing.T) {
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
	if err := (app{}).execute([]string{"launch"}); err != nil {
		t.Fatalf("explicit aggregate launch: %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("shelved aggregate launch invoked amp or tmux: %v", err)
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

func TestThreadArchiveStatusesBoundsAmpThreadsListFailures(t *testing.T) {
	oldTimeout := ampThreadsListTimeout
	oldLimit := ampThreadsListOutputLimit
	t.Cleanup(func() {
		ampThreadsListTimeout = oldTimeout
		ampThreadsListOutputLimit = oldLimit
	})
	ampThreadsListTimeout = 50 * time.Millisecond
	ampThreadsListOutputLimit = 64
	rows := []config.Row{{Workspace: "alpha", Window: "worker", Workdir: "/tmp/worker", Thread: "T-worker"}}

	for _, test := range []struct {
		name      string
		behavior  func([]string) (string, string)
		want      string
		wantCalls int
		limit     int
	}{
		{
			name:      "active timeout",
			behavior:  func([]string) (string, string) { return "timeout", "" },
			want:      "active thread inventory: amp threads list timed out after 50ms",
			wantCalls: 1,
		},
		{
			name: "archive timeout",
			behavior: func(args []string) (string, string) {
				if slicesContain(args, "--include-archived") {
					return "timeout", ""
				}
				return "output", `[]`
			},
			want:      "archived thread inventory: amp threads list timed out after 50ms",
			wantCalls: 2,
		},
		{
			name:      "nonzero exit",
			behavior:  func([]string) (string, string) { return "nonzero", "" },
			want:      "active thread inventory: amp threads list failed with exit code 7",
			wantCalls: 1,
		},
		{
			name:      "malformed JSON",
			behavior:  func([]string) (string, string) { return "malformed", "" },
			want:      "active thread inventory: amp threads list returned malformed JSON",
			wantCalls: 1,
		},
		{
			name:      "output overflow",
			behavior:  func([]string) (string, string) { return "overflow", "" },
			want:      "active thread inventory: amp threads list output exceeded 64-byte limit",
			wantCalls: 1,
		},
		{
			name:      "stderr overflow",
			behavior:  func([]string) (string, string) { return "stderr-overflow", "" },
			want:      "active thread inventory: amp threads list output exceeded 64-byte limit",
			wantCalls: 1,
		},
		{
			name: "archive nonzero exit",
			behavior: func(args []string) (string, string) {
				if slicesContain(args, "--include-archived") {
					return "nonzero", ""
				}
				return "output", `[]`
			},
			want:      "archived thread inventory: amp threads list failed with exit code 7",
			wantCalls: 2,
		},
		{
			name: "archive malformed JSON",
			behavior: func(args []string) (string, string) {
				if slicesContain(args, "--include-archived") {
					return "malformed", ""
				}
				return "output", `[]`
			},
			want:      "archived thread inventory: amp threads list returned malformed JSON",
			wantCalls: 2,
		},
		{
			name: "archive output overflow",
			behavior: func(args []string) (string, string) {
				if slicesContain(args, "--include-archived") {
					return "overflow", ""
				}
				return "output", `[]`
			},
			want:      "archived thread inventory: amp threads list output exceeded 64-byte limit",
			wantCalls: 2,
		},
		{
			name:      "overflow followed by stall",
			behavior:  func([]string) (string, string) { return "overflow-stall", "" },
			want:      "active thread inventory: amp threads list output exceeded 64-byte limit",
			wantCalls: 1,
		},
		{
			name: "pagination timeout",
			behavior: func(args []string) (string, string) {
				if len(args) > 0 && args[len(args)-1] == "500" {
					return "timeout", ""
				}
				page := make([]map[string]string, 500)
				for i := range page {
					page[i] = map[string]string{"id": fmt.Sprintf("T-page-%d", i)}
				}
				encoded, _ := json.Marshal(page)
				return "output", string(encoded)
			},
			want:      "active thread inventory: amp threads list timed out after 50ms",
			wantCalls: 2,
			limit:     32 << 10,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ampThreadsListOutputLimit = test.limit
			if ampThreadsListOutputLimit == 0 {
				ampThreadsListOutputLimit = 64
			}
			calls := injectAmpThreadsListProcess(t, test.behavior)
			started := time.Now()
			_, err := threadArchiveStatuses(rows)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("threadArchiveStatuses error = %v, want %q", err, test.want)
			}
			if strings.Contains(err.Error(), "synthetic-private-output") {
				t.Fatalf("threadArchiveStatuses leaked subprocess output: %v", err)
			}
			if *calls != test.wantCalls {
				t.Fatalf("amp threads list calls = %d, want %d", *calls, test.wantCalls)
			}
			if time.Since(started) > time.Second {
				t.Fatalf("bounded failure took %s", time.Since(started))
			}
		})
	}
}

func TestScopedWorkerDoctorUsesBoundedExactThreadQueries(t *testing.T) {
	dir := t.TempDir()
	legacyWorkdir := "legacy/../worker"
	rows := []config.Row{
		{Workspace: "alpha", Window: "b", Workdir: legacyWorkdir, Thread: "T-b"},
		{Workspace: "alpha", Window: "a", Workdir: "/tmp/a", Thread: "T-a"},
		{Workspace: "beta", Window: "c", Workdir: "/tmp/c", Thread: "T-c"},
	}
	writeWorkerRegistry(t, dir, rows[0].String()+"\n"+rows[1].String()+"\n"+rows[2].String()+"\n")
	installWorkerDoctorTmux(t, rows)
	calls := injectAmpThreadsListProcess(t, func(args []string) (string, string) {
		for _, id := range []string{"T-a", "T-b"} {
			if slicesContain(args, "id:"+id+" archived:false") {
				return "output", `[{"id":"` + id + `"}]`
			}
		}
		return "nonzero", ""
	})

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "doctor", "--workspace", "alpha")
	if *calls != 2 {
		t.Fatalf("amp threads search calls = %d, want one exact active query per selected thread", *calls)
	}
	if len(got.Successful) != 2 || got.Successful[0].Resource.Thread != "T-a" || got.Successful[1].Resource.Thread != "T-b" || len(got.Failed) != 0 {
		t.Fatalf("scoped worker doctor = %+v", got)
	}
	for _, out := range got.Successful {
		if out.Worker == nil || out.Worker.Workdir == "" || out.Worker.NativeExecutor != unknownNativePlacement || out.Worker.NativeRunnerID != unknownNativePlacement || out.Worker.ExecutionAffinity != unknownNativePlacement || !strings.Contains(out.Message, "execution_affinity=unknown") {
			t.Fatalf("worker doctor placement diagnostic = %+v", out)
		}
	}
	if got.Successful[1].Worker.LocalState != "exact" || got.Successful[1].Worker.Workdir != "legacy/../worker" || !strings.Contains(got.Successful[1].Message, "local=exact") {
		t.Fatalf("worker doctor legacy catalog spelling = %+v", got.Successful[1])
	}
}

func TestScopedWorkerDoctorIgnoresOversizedUnrelatedArchivedInventory(t *testing.T) {
	dir := t.TempDir()
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: "/tmp/worker", Thread: "T-active"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	installWorkerDoctorTmux(t, []config.Row{row})
	oldLimit := ampThreadsListOutputLimit
	t.Cleanup(func() { ampThreadsListOutputLimit = oldLimit })
	ampThreadsListOutputLimit = 65_536
	largeArchivedInventory := `[` + strings.Repeat(`{"id":"T-unrelated-archived"},`, 3_000) + `{"id":"T-unrelated-archived"}]`
	if len(largeArchivedInventory) <= ampThreadsListOutputLimit {
		t.Fatalf("archived inventory fixture = %d bytes, want over %d", len(largeArchivedInventory), ampThreadsListOutputLimit)
	}
	calls := injectAmpThreadsListProcess(t, func(args []string) (string, string) {
		if slicesContain(args, "id:T-active archived:false") && slicesContain(args, "--limit") && slicesContain(args, "2") {
			return "output", `[{"id":"T-active"}]`
		}
		if slicesContain(args, "--include-archived") {
			return "output", largeArchivedInventory
		}
		return "nonzero", ""
	})

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "doctor", "--thread", "T-active")
	if *calls != 1 {
		t.Fatalf("amp thread queries = %d, want only one exact active query", *calls)
	}
	if len(got.Successful) != 1 || !strings.Contains(got.Successful[0].Message, "local=exact remote=active") || len(got.Failed) != 0 {
		t.Fatalf("scoped worker doctor = %+v", got)
	}
}

func TestScopedWorkerDoctorBoundsCumulativeExactQueryOutput(t *testing.T) {
	dir := t.TempDir()
	rows := []config.Row{
		{Workspace: "alpha", Window: "a", Workdir: "/tmp/a", Thread: "T-a"},
		{Workspace: "alpha", Window: "b", Workdir: "/tmp/b", Thread: "T-b"},
		{Workspace: "alpha", Window: "c", Workdir: "/tmp/c", Thread: "T-c"},
	}
	writeWorkerRegistry(t, dir, rows[0].String()+"\n"+rows[1].String()+"\n"+rows[2].String()+"\n")
	installWorkerDoctorTmux(t, rows)
	oldTimeout := ampThreadsListTimeout
	oldLimit := ampThreadsListOutputLimit
	t.Cleanup(func() {
		ampThreadsListTimeout = oldTimeout
		ampThreadsListOutputLimit = oldLimit
	})
	ampThreadsListTimeout = 2 * time.Second
	ampThreadsListOutputLimit = 65_536
	exactResult := func(id string) string {
		return `[{"id":"` + id + `","padding":"` + strings.Repeat("x", 40_000) + `"}]`
	}
	first := exactResult("T-a")
	second := exactResult("T-b")
	if len(first) >= ampThreadsListOutputLimit || len(second) >= ampThreadsListOutputLimit || len(first)+len(second) <= ampThreadsListOutputLimit {
		t.Fatalf("exact fixtures = %d + %d bytes, want each below and sum above %d", len(first), len(second), ampThreadsListOutputLimit)
	}
	calls := injectAmpThreadsListProcess(t, func(args []string) (string, string) {
		switch {
		case slicesContain(args, "id:T-a archived:false"):
			return "output", first
		case slicesContain(args, "id:T-b archived:false"):
			return "output-stall", second
		default:
			return "nonzero", ""
		}
	})

	started := time.Now()
	got, err := executeWorkerJSONResult(t, "--json", "--config-dir", dir, "worker", "doctor", "--workspace", "alpha")
	if err == nil || result.ErrorKindOf(err) != result.ErrorRuntime {
		t.Fatalf("scoped worker doctor error = %v", err)
	}
	if time.Since(started) >= time.Second {
		t.Fatalf("cumulative overflow did not stop current query promptly: %s", time.Since(started))
	}
	if *calls != 2 {
		t.Fatalf("amp threads search calls = %d, want overflow on second query and no T-c query", *calls)
	}
	if len(got.Successful) != 3 {
		t.Fatalf("worker outcomes = %+v", got)
	}
	for _, out := range got.Successful {
		if !strings.Contains(out.Message, "remote=unknown") {
			t.Fatalf("worker outcome did not fail closed: %+v", out)
		}
	}
	if len(got.Failed) != 1 || got.Failed[0].Resource.Kind != "command" || got.Failed[0].Error == nil || got.Failed[0].Error.Message != "active thread query: amp threads search output exceeded 65536-byte limit" {
		t.Fatalf("scoped cumulative failure = %+v", got.Failed)
	}
}

func TestExactActiveAndArchivedQueriesShareOutputAllowance(t *testing.T) {
	oldLimit := ampThreadsListOutputLimit
	t.Cleanup(func() { ampThreadsListOutputLimit = oldLimit })
	ampThreadsListOutputLimit = 64
	active := strings.Repeat(" ", 40) + `[]`
	archived := `[{"id":"T-worker","padding":"` + strings.Repeat("x", 20) + `"}]`
	if len(active) >= ampThreadsListOutputLimit || len(archived) >= ampThreadsListOutputLimit || len(active)+len(archived) <= ampThreadsListOutputLimit {
		t.Fatalf("exact fixtures = %d + %d bytes, want each below and sum above %d", len(active), len(archived), ampThreadsListOutputLimit)
	}
	calls := injectAmpThreadsListProcess(t, func(args []string) (string, string) {
		if slicesContain(args, "id:T-worker archived:true") {
			return "output", archived
		}
		return "output", active
	})

	_, err := exactThreadArchiveStatuses([]config.Row{{Workspace: "alpha", Window: "worker", Workdir: "/tmp/worker", Thread: "T-worker"}})
	if err == nil || err.Error() != "archived thread query: amp threads search output exceeded 64-byte limit" {
		t.Fatalf("exactThreadArchiveStatuses error = %v", err)
	}
	if *calls != 2 {
		t.Fatalf("amp threads search calls = %d, want active and archived queries only", *calls)
	}
}

func TestExactThreadArchiveStatusesAreBoundedAndFailClosed(t *testing.T) {
	oldLimit := ampThreadsListOutputLimit
	t.Cleanup(func() { ampThreadsListOutputLimit = oldLimit })
	ampThreadsListOutputLimit = 64
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: "/tmp/worker", Thread: "T-worker"}

	for _, test := range []struct {
		name      string
		behavior  func([]string) (string, string)
		want      threadStatus
		wantError string
		calls     int
	}{
		{
			name: "active",
			behavior: func(args []string) (string, string) {
				if slicesContain(args, "id:T-worker archived:false") {
					return "output", `[{"id":"T-worker"}]`
				}
				return "nonzero", ""
			},
			want: threadStatusActive, calls: 1,
		},
		{
			name: "archived",
			behavior: func(args []string) (string, string) {
				if slicesContain(args, "id:T-worker archived:true") {
					return "output", `[{"id":"T-worker"}]`
				}
				return "output", `[]`
			},
			want: threadStatusArchived, calls: 2,
		},
		{
			name:     "missing",
			behavior: func([]string) (string, string) { return "output", `[]` },
			want:     threadStatusMissing, calls: 2,
		},
		{
			name:      "malformed active result",
			behavior:  func([]string) (string, string) { return "malformed", "" },
			wantError: "active thread query: amp threads search returned malformed JSON", calls: 1,
		},
		{
			name:      "unexpected active result",
			behavior:  func([]string) (string, string) { return "output", `[{"id":"T-other"}]` },
			wantError: "active thread query: amp threads search returned an unexpected exact result", calls: 1,
		},
		{
			name:      "active command failure",
			behavior:  func([]string) (string, string) { return "nonzero", "" },
			wantError: "active thread query: amp threads search failed with exit code 7", calls: 1,
		},
		{
			name: "oversized archived result",
			behavior: func(args []string) (string, string) {
				if slicesContain(args, "id:T-worker archived:true") {
					return "overflow", ""
				}
				return "output", `[]`
			},
			wantError: "archived thread query: amp threads search output exceeded 64-byte limit", calls: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := injectAmpThreadsListProcess(t, test.behavior)
			statuses, err := exactThreadArchiveStatuses([]config.Row{row})
			if test.wantError != "" {
				if err == nil || err.Error() != test.wantError {
					t.Fatalf("exactThreadArchiveStatuses error = %v, want %q", err, test.wantError)
				}
			} else if err != nil || statuses[row.Thread] != test.want {
				t.Fatalf("exactThreadArchiveStatuses = %v, %v; want %s", statuses, err, test.want)
			}
			if *calls != test.calls {
				t.Fatalf("amp threads search calls = %d, want %d", *calls, test.calls)
			}
		})
	}
}

func TestWorkerDoctorInventoryFailureProducesOrderedIndependentOutcomes(t *testing.T) {
	dir := t.TempDir()
	rows := []config.Row{
		{Workspace: "alpha", Window: "b", Workdir: "/tmp/b", Thread: "T-b"},
		{Workspace: "alpha", Window: "a", Workdir: "/tmp/a", Thread: "T-a"},
	}
	writeWorkerRegistry(t, dir, rows[0].String()+"\n"+rows[1].String()+"\n")
	before, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil {
		t.Fatal(err)
	}
	installWorkerDoctorTmux(t, rows)
	calls := injectAmpThreadsListProcess(t, func([]string) (string, string) { return "nonzero", "" })

	got, err := executeWorkerJSONResult(t, "--json", "--config-dir", dir, "worker", "doctor", "--all")
	if err == nil || result.ErrorKindOf(err) != result.ErrorRuntime {
		t.Fatalf("worker doctor error = %v", err)
	}
	if *calls != 1 {
		t.Fatalf("amp threads list calls = %d, want one failed shared inventory", *calls)
	}
	if len(got.Successful) != 2 || got.Successful[0].Resource.Thread != "T-a" || got.Successful[1].Resource.Thread != "T-b" {
		t.Fatalf("ordered worker outcomes = %+v", got)
	}
	for _, out := range got.Successful {
		if !strings.Contains(out.Message, "remote=unknown") {
			t.Fatalf("worker local diagnostic = %+v", out)
		}
	}
	if len(got.Failed) != 1 || got.Failed[0].Resource.Kind != "command" || got.Failed[0].Error == nil || got.Failed[0].Error.Message != "active thread inventory: amp threads list failed with exit code 7" {
		t.Fatalf("shared inventory failure = %+v", got)
	}
	after, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("worker doctor changed registry: err=%v\nbefore=%s\nafter=%s", err, before, after)
	}
}

func TestAmpThreadsListDescendantIsBoundedAndStopped(t *testing.T) {
	oldTimeout := ampThreadsListTimeout
	oldWaitDelay := ampThreadsListWaitDelay
	t.Cleanup(func() {
		ampThreadsListTimeout = oldTimeout
		ampThreadsListWaitDelay = oldWaitDelay
	})
	ampThreadsListTimeout = 100 * time.Millisecond
	ampThreadsListWaitDelay = 50 * time.Millisecond
	marker := filepath.Join(t.TempDir(), "descendant-survived")
	calls := injectAmpThreadsListProcess(t, func([]string) (string, string) { return "descendant", marker })

	started := time.Now()
	_, err := threadArchiveStatuses([]config.Row{{Workspace: "alpha", Window: "worker", Workdir: "/tmp/worker", Thread: "T-worker"}})
	if err == nil || !strings.Contains(err.Error(), "amp threads list timed out after 100ms") {
		t.Fatalf("descendant inventory error = %v", err)
	}
	if *calls != 1 || time.Since(started) > time.Second {
		t.Fatalf("descendant inventory calls=%d duration=%s", *calls, time.Since(started))
	}
	time.Sleep(400 * time.Millisecond)
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("amp threads list descendant survived process-group cancellation: %v", statErr)
	}
}

func TestAmpThreadsListSyntheticProcess(t *testing.T) {
	if os.Getenv("AMUX_AMP_THREADS_LIST_TEST_PROCESS") != "1" {
		return
	}
	if os.Getenv("AMUX_AMP_THREADS_LIST_TEST_DESCENDANT") == "1" {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("AMUX_AMP_THREADS_LIST_TEST_OUTPUT"), []byte("survived"), 0o600)
		os.Exit(0)
	}
	switch os.Getenv("AMUX_AMP_THREADS_LIST_TEST_SCENARIO") {
	case "timeout":
		time.Sleep(10 * time.Second)
	case "nonzero":
		fmt.Fprint(os.Stderr, "synthetic-private-output")
		os.Exit(7)
	case "malformed":
		fmt.Fprint(os.Stdout, `{"title":"synthetic-private-output"`)
	case "overflow":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("synthetic-private-output", 20))
	case "stderr-overflow":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("synthetic-private-output", 20))
	case "overflow-stall":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("synthetic-private-output", 20))
		time.Sleep(10 * time.Second)
	case "output-stall":
		fmt.Fprint(os.Stdout, os.Getenv("AMUX_AMP_THREADS_LIST_TEST_OUTPUT"))
		time.Sleep(10 * time.Second)
	case "descendant":
		cmd := exec.Command(os.Args[0], "-test.run=^TestAmpThreadsListSyntheticProcess$")
		cmd.Env = append(os.Environ(), "AMUX_AMP_THREADS_LIST_TEST_DESCENDANT=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(8)
		}
	case "output":
		fmt.Fprint(os.Stdout, os.Getenv("AMUX_AMP_THREADS_LIST_TEST_OUTPUT"))
	default:
		os.Exit(9)
	}
	os.Exit(0)
}

func injectAmpThreadsListProcess(t *testing.T, behavior func([]string) (string, string)) *int {
	t.Helper()
	oldCommand := ampThreadsListCommand
	t.Cleanup(func() { ampThreadsListCommand = oldCommand })
	calls := 0
	ampThreadsListCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		calls++
		scenario, output := behavior(args)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAmpThreadsListSyntheticProcess$")
		cmd.Env = append(os.Environ(),
			"AMUX_AMP_THREADS_LIST_TEST_PROCESS=1",
			"AMUX_AMP_THREADS_LIST_TEST_SCENARIO="+scenario,
			"AMUX_AMP_THREADS_LIST_TEST_OUTPUT="+output,
		)
		return cmd
	}
	return &calls
}

func installWorkerDoctorTmux(t *testing.T, rows []config.Row) {
	t.Helper()
	var panes strings.Builder
	for i, row := range rows {
		identity := teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}
		fmt.Fprintf(&panes, "%s\\t@%d\\t%s\\n", row.Window, i+1, teardownExpectedStartCommand(identity, row))
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ncase \"$1\" in\n  has-session) exit 0 ;;\n  list-panes) printf %b "+shellSingleQuote(panes.String())+" ;;\n  *) exit 99 ;;\nesac\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
			for _, removed := range []string{"--idempotency-key", "--message-file", "--title-prefix"} {
				if completionLineContains(output, "spawn", removed) {
					t.Fatalf("%s spawn completion retains removed flag %q\n%s", shell, removed, output)
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

func executeWorkerJSONResult(t *testing.T, args ...string) (result.Envelope, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(args)
	var envelope result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode execute(%q): %v\nstdout: %s", args, decodeErr, stdout.String())
	}
	return envelope, err
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
