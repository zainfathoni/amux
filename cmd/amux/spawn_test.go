package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

func TestNativeSpawnAcceptedFlowUsesOneExactCreateAndOneCoordinatorMessageWithoutTUIInput(t *testing.T) {
	dir, workdir, log := setupNativeSpawnTest(t, "")
	createCalls := 0
	spawnCreateThread = func(gotWorkdir, mode string) (string, error) {
		createCalls++
		if gotWorkdir != workdir || mode != "high" {
			t.Fatalf("create cwd/mode=%q/%q", gotWorkdir, mode)
		}
		return "T-exact", nil
	}
	setReadyNativeSpawnPane(t, workdir, "T-exact")
	prompt := "exact private assignment\nsecond line"

	prepared := executeNativeSpawn(t, dir, workdir, prompt, "prepare", "", "", "", true)
	if len(prepared.Successful) != 1 || prepared.Successful[0].Resource.Thread != "T-exact" || prepared.Successful[0].Assignment == nil || prepared.Successful[0].Assignment.Creation != "exact_thread_allocated" || prepared.Successful[0].Assignment.LocalPresentation != "absent" || prepared.Successful[0].Assignment.Assignment != "not_attempted" || prepared.Successful[0].Assignment.Execution != "unproven" {
		t.Fatalf("prepare=%+v", prepared)
	}
	armed := executeNativeSpawn(t, dir, workdir, prompt, "arm", "T-exact", "", "", true)
	if len(armed.Successful) != 1 || armed.Successful[0].Assignment.Assignment != "indeterminate" {
		t.Fatalf("arm=%+v", armed)
	}

	messageCalls := 0
	nativeMessage := func(thread, message string) string {
		messageCalls++
		if thread != "T-exact" || message != prompt {
			t.Fatalf("native target/message=%q/%q", thread, message)
		}
		return "cursor-after-acceptance"
	}
	cursor := nativeMessage("T-exact", prompt)
	finalized := executeNativeSpawn(t, dir, workdir, prompt, "finalize", "T-exact", "authenticated_accepted", cursor, true)
	if len(finalized.Successful) != 1 || finalized.Successful[0].Assignment == nil || finalized.Successful[0].Assignment.Assignment != "authenticated_accepted" || finalized.Successful[0].Assignment.Receipt != "native_latest_cursor" || finalized.Successful[0].Assignment.LocalPresentation != "exact_client_established" || finalized.Successful[0].Assignment.Execution != "unproven" {
		t.Fatalf("finalize=%+v", finalized)
	}
	if createCalls != 1 || messageCalls != 1 {
		t.Fatalf("creates=%d messages=%d", createCalls, messageCalls)
	}
	assertNativeSpawnTransport(t, log, 1)
	record := loadOnlySpawnAssignment(t, dir)
	if record.Thread != "T-exact" || record.Phase != config.SpawnAssignmentFinalized || record.Outcome != config.SpawnAssignmentAuthenticatedAccepted || record.ReceiptCursor != cursor || strings.Contains(mustRead(t, filepath.Join(dir, config.SpawnAssignmentsFile)), prompt) {
		t.Fatalf("record=%+v", record)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].AssignmentState != config.WorkerAssignmentAuthenticatedAccepted {
		t.Fatalf("workers=%+v err=%v", rows, err)
	}
}

func TestNativeSpawnHumanOutputSeparatesCreationOwnershipPresentationAssignmentAndExecution(t *testing.T) {
	dir, workdir, _ := setupNativeSpawnTest(t, "")
	spawnCreateThread = func(string, string) (string, error) { return "T-human", nil }
	setReadyNativeSpawnPane(t, workdir, "T-human")
	prompt := "private human prompt"
	run := func(phase, thread, outcome, cursor string) string {
		args := nativeSpawnArgs(dir, workdir, phase, thread, outcome, cursor)
		args = args[1:] // remove --json
		var stdout bytes.Buffer
		if err := (app{stdin: strings.NewReader(prompt), stdout: &stdout}).execute(args); err != nil {
			t.Fatalf("phase %s: %v", phase, err)
		}
		if strings.Contains(stdout.String(), prompt) {
			t.Fatalf("phase %s leaked prompt: %s", phase, stdout.String())
		}
		return stdout.String()
	}
	prepared := run("prepare", "", "", "")
	if !strings.Contains(prepared, "creation=exact_thread_allocated") || !strings.Contains(prepared, "local-ownership=retained") || !strings.Contains(prepared, "local-presentation=absent") || !strings.Contains(prepared, "assignment=not_attempted") || !strings.Contains(prepared, "execution=unproven") {
		t.Fatalf("prepare output=%q", prepared)
	}
	run("arm", "T-human", "", "")
	finalized := run("finalize", "T-human", "authenticated_accepted", "cursor")
	if !strings.Contains(finalized, "local-presentation=exact_client_established") || !strings.Contains(finalized, "assignment=authenticated_accepted") || !strings.Contains(finalized, "execution=unproven") {
		t.Fatalf("finalize output=%q", finalized)
	}
}

func TestNativeSpawnCapabilityUnavailableRejectsBeforeCreateOrTmuxMutation(t *testing.T) {
	dir, workdir, log := setupNativeSpawnTest(t, "")
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) { createCalls++; return "T-no", nil }
	args := nativeSpawnArgs(dir, workdir, "prepare", "", "", "")
	for i := 0; i < len(args); i++ {
		if args[i] == "--native-capability" {
			args = append(args[:i], args[i+2:]...)
			break
		}
	}
	err := (app{stdin: strings.NewReader("prompt")}).execute(args)
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "capability") || createCalls != 0 {
		t.Fatalf("err=%v creates=%d", err, createCalls)
	}
	if data, readErr := os.ReadFile(log); readErr == nil && len(data) != 0 || readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("capability rejection touched tmux: %q err=%v", data, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, config.SpawnAssignmentsFile)); !os.IsNotExist(statErr) {
		t.Fatalf("capability rejection persisted state: %v", statErr)
	}
}

func TestNativeSpawnBindsExactTargetPromptModeAndProjectlessCanonicalCWD(t *testing.T) {
	dir, workdir, log := setupNativeSpawnTest(t, "")
	spawnCreateThread = func(string, string) (string, error) { return "T-bound", nil }
	executeNativeSpawn(t, dir, workdir, "prompt", "prepare", "", "", "", true)

	for _, test := range []struct {
		name   string
		thread string
		prompt string
	}{
		{name: "thread", thread: "T-other", prompt: "prompt"},
		{name: "prompt", thread: "T-bound", prompt: "changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := executeNativeSpawnError(dir, workdir, test.prompt, "arm", test.thread, "", "")
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "does not exactly match") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if got := mustRead(t, log); strings.Contains(got, "new-session") || strings.Contains(got, "new-window") {
		t.Fatalf("prepare presented a client before assignment: %s", got)
	}
}

func TestNativeSpawnRejectedAndIndeterminateOutcomesStayOrthogonal(t *testing.T) {
	for _, outcome := range []string{"rejected", "indeterminate"} {
		t.Run(outcome, func(t *testing.T) {
			dir, workdir, log := setupNativeSpawnTest(t, "")
			spawnCreateThread = func(string, string) (string, error) { return "T-" + outcome, nil }
			setReadyNativeSpawnPane(t, workdir, "T-"+outcome)
			executeNativeSpawn(t, dir, workdir, "prompt", "prepare", "", "", "", true)
			executeNativeSpawn(t, dir, workdir, "prompt", "arm", "T-"+outcome, "", "", true)
			env, err := executeNativeSpawnResult(dir, workdir, "prompt", "finalize", "T-"+outcome, outcome, "")
			if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure || len(env.Failed) != 1 || env.Failed[0].Assignment == nil || env.Failed[0].Assignment.Assignment != outcome || env.Failed[0].Assignment.LocalPresentation != "exact_client_established" || env.Failed[0].Assignment.Execution != "unproven" {
				t.Fatalf("env=%+v err=%v", env, err)
			}
			assertNativeSpawnTransport(t, log, 1)
			record := loadOnlySpawnAssignment(t, dir)
			if string(record.Outcome) != outcome || record.ReceiptCursor != "" {
				t.Fatalf("record=%+v", record)
			}
		})
	}
}

func TestNativeSpawnInterruptionBoundariesNeverRetryCreateOrArmedMessage(t *testing.T) {
	t.Run("before creation arm persistence", func(t *testing.T) {
		dir, workdir, _ := setupNativeSpawnTest(t, "")
		creates := 0
		spawnCreateThread = func(string, string) (string, error) { creates++; return "T-no", nil }
		spawnStoreAssignment = func(string, config.SpawnAssignmentRecord) error { return errors.New("disk") }
		err := executeNativeSpawnError(dir, workdir, "prompt", "prepare", "", "", "")
		if err == nil || creates != 0 {
			t.Fatalf("err=%v creates=%d", err, creates)
		}
	})

	t.Run("creation armed", func(t *testing.T) {
		dir, workdir, _ := setupNativeSpawnTest(t, "")
		creates := 0
		spawnCreateThread = func(string, string) (string, error) { creates++; return "unparseable", errors.New("interrupted") }
		if err := executeNativeSpawnError(dir, workdir, "prompt", "prepare", "", "", ""); err == nil {
			t.Fatal("missing creation-indeterminate error")
		}
		err := executeNativeSpawnError(dir, workdir, "prompt", "prepare", "", "", "")
		if err == nil || result.ExitCode(err) != result.ExitRejected || creates != 1 {
			t.Fatalf("replay err=%v creates=%d", err, creates)
		}
	})

	t.Run("exact create before prepared persistence", func(t *testing.T) {
		dir, workdir, _ := setupNativeSpawnTest(t, "")
		creates, stores := 0, 0
		spawnCreateThread = func(string, string) (string, error) { creates++; return "T-late", nil }
		realStore := config.StoreSpawnAssignment
		spawnStoreAssignment = func(path string, record config.SpawnAssignmentRecord) error {
			stores++
			if stores == 2 {
				return errors.New("interrupted")
			}
			return realStore(path, record)
		}
		if err := executeNativeSpawnError(dir, workdir, "prompt", "prepare", "", "", ""); err == nil || !strings.Contains(err.Error(), "T-late") {
			t.Fatalf("error=%v", err)
		}
		spawnStoreAssignment = realStore
		if err := executeNativeSpawnError(dir, workdir, "prompt", "prepare", "", "", ""); err == nil || creates != 1 {
			t.Fatalf("replay err=%v creates=%d", err, creates)
		}
	})

	t.Run("armed is indeterminate and cannot be armed twice", func(t *testing.T) {
		dir, workdir, _ := setupNativeSpawnTest(t, "")
		spawnCreateThread = func(string, string) (string, error) { return "T-armed", nil }
		executeNativeSpawn(t, dir, workdir, "prompt", "prepare", "", "", "", true)
		executeNativeSpawn(t, dir, workdir, "prompt", "arm", "T-armed", "", "", true)
		err := executeNativeSpawnError(dir, workdir, "prompt", "arm", "T-armed", "", "")
		if err == nil || result.ExitCode(err) != result.ExitRejected || loadOnlySpawnAssignment(t, dir).Outcome != config.SpawnAssignmentIndeterminate {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestNativeSpawnAcceptedThenFinalizationOrPresentationFailurePreservesTruthWithoutResend(t *testing.T) {
	t.Run("finalization persistence failure remains indeterminate", func(t *testing.T) {
		dir, workdir, _ := setupNativeSpawnTest(t, "")
		spawnCreateThread = func(string, string) (string, error) { return "T-finalize", nil }
		executeNativeSpawn(t, dir, workdir, "prompt", "prepare", "", "", "", true)
		executeNativeSpawn(t, dir, workdir, "prompt", "arm", "T-finalize", "", "", true)
		messageCalls := 1 // one native tool success happened outside core
		spawnStoreAssignment = func(string, config.SpawnAssignmentRecord) error { return errors.New("disk full") }
		err := executeNativeSpawnError(dir, workdir, "prompt", "finalize", "T-finalize", "authenticated_accepted", "cursor")
		if err == nil || !strings.Contains(err.Error(), "remains indeterminate") || messageCalls != 1 {
			t.Fatalf("err=%v messages=%d", err, messageCalls)
		}
		spawnStoreAssignment = config.StoreSpawnAssignment
		if record := loadOnlySpawnAssignment(t, dir); record.Phase != config.SpawnAssignmentArmed || record.Outcome != config.SpawnAssignmentIndeterminate {
			t.Fatalf("record=%+v", record)
		}
	})

	t.Run("accepted before presentation failure", func(t *testing.T) {
		dir, workdir, log := setupNativeSpawnTest(t, "create")
		spawnCreateThread = func(string, string) (string, error) { return "T-present", nil }
		executeNativeSpawn(t, dir, workdir, "prompt", "prepare", "", "", "", true)
		executeNativeSpawn(t, dir, workdir, "prompt", "arm", "T-present", "", "", true)
		err := executeNativeSpawnError(dir, workdir, "prompt", "finalize", "T-present", "authenticated_accepted", "cursor")
		if err == nil || !strings.Contains(err.Error(), "truth is preserved") || !strings.Contains(err.Error(), "must not be resent") {
			t.Fatalf("error=%v", err)
		}
		record := loadOnlySpawnAssignment(t, dir)
		if record.Outcome != config.SpawnAssignmentAuthenticatedAccepted || record.ReceiptCursor != "cursor" {
			t.Fatalf("record=%+v", record)
		}
		rows, _ := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
		if len(rows) != 1 || rows[0].AssignmentState != config.WorkerAssignmentAuthenticatedAccepted {
			t.Fatalf("rows=%+v", rows)
		}
		assertNativeSpawnTransport(t, log, 1)
	})
}

func TestCreateLocalAmpThreadUsesCanonicalCWDAndExactModeOnce(t *testing.T) {
	workdir := t.TempDir()
	bin := t.TempDir()
	log := filepath.Join(bin, "amp.log")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\nprintf '%s\\n%s\\n' \"$(pwd)\" \"$*\" >> "+shellSingleQuote(log)+"\nprintf 'T-exact\\n'\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	thread, err := createLocalAmpThread(workdir, "team-architect")
	if err != nil || thread != "T-exact" || mustRead(t, log) != workdir+"\nthreads new --mode team-architect\n" {
		t.Fatalf("thread=%q err=%v log=%q", thread, err, mustRead(t, log))
	}
}

func TestSpawnPromptIsBounded(t *testing.T) {
	_, err := (app{stdin: strings.NewReader(strings.Repeat("x", maxSpawnPromptBytes+1))}).readSpawnPrompt("-")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error=%v", err)
	}
}

func setupNativeSpawnTest(t *testing.T, fail string) (dir, workdir, log string) {
	t.Helper()
	dir, workdir = t.TempDir(), t.TempDir()
	bin := t.TempDir()
	log = filepath.Join(bin, "tmux.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> ` + shellSingleQuote(log) + `
case "$1" in
  has-session) exit 1 ;;
  new-session) test "` + fail + `" = create && exit 1; printf 'alpha\tworker\t@1\t%%1\n'; exit 0 ;;
  list-windows) exit 0 ;;
esac
exit 77
`
	writeExecutable(t, filepath.Join(bin, "tmux"), script)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldCreate, oldStore := spawnCreateThread, spawnStoreAssignment
	oldInspect, oldSettle := spawnInspectPaneByID, spawnReadinessSettle
	spawnCreateThread = func(string, string) (string, error) { t.Fatal("unexpected create"); return "", nil }
	spawnStoreAssignment = config.StoreSpawnAssignment
	spawnReadinessSettle = 0
	t.Cleanup(func() {
		spawnCreateThread, spawnStoreAssignment = oldCreate, oldStore
		spawnInspectPaneByID, spawnReadinessSettle = oldInspect, oldSettle
	})
	return dir, workdir, log
}

func setReadyNativeSpawnPane(t *testing.T, workdir, thread string) {
	t.Helper()
	command := tmux.ContinueCommandWithEnv(workdir, thread, map[string]string{
		"AMUX_WORKSPACE": "alpha", "AMUX_SESSION": "alpha", "AMUX_WINDOW": "worker", "AMUX_THREAD_ID": thread, "AMUX_WORKDIR": workdir,
	})
	spawnInspectPaneByID = func(string) (tmux.WindowPane, error) {
		return tmux.WindowPane{Session: "alpha", Window: "worker", WindowID: "@1", PaneID: "%1", Path: workdir, Command: "amp", StartCommand: command}, nil
	}
}

func nativeSpawnArgs(dir, workdir, phase, thread, outcome, cursor string) []string {
	args := []string{"--json", "--config-dir", dir, "spawn", "--assignment-phase", phase, "--native-capability", "existing-thread-message-v1", "--workdir", workdir, "--workspace", "alpha", "--window", "worker", "--mode", "high", "--prompt-file", "-"}
	if thread != "" {
		args = append(args, "--thread", thread)
	}
	if outcome != "" {
		args = append(args, "--assignment-outcome", outcome)
	}
	if cursor != "" {
		args = append(args, "--latest-cursor", cursor)
	}
	return args
}

func executeNativeSpawn(t *testing.T, dir, workdir, prompt, phase, thread, outcome, cursor string, wantSuccess bool) result.Envelope {
	t.Helper()
	env, err := executeNativeSpawnResult(dir, workdir, prompt, phase, thread, outcome, cursor)
	if wantSuccess && err != nil {
		t.Fatalf("phase %s: %v", phase, err)
	}
	return env
}

func executeNativeSpawnResult(dir, workdir, prompt, phase, thread, outcome, cursor string) (result.Envelope, error) {
	var stdout bytes.Buffer
	err := (app{stdin: strings.NewReader(prompt), stdout: &stdout}).execute(nativeSpawnArgs(dir, workdir, phase, thread, outcome, cursor))
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		return env, decodeErr
	}
	return env, err
}

func executeNativeSpawnError(dir, workdir, prompt, phase, thread, outcome, cursor string) error {
	_, err := executeNativeSpawnResult(dir, workdir, prompt, phase, thread, outcome, cursor)
	return err
}

func loadOnlySpawnAssignment(t *testing.T, dir string) config.SpawnAssignmentRecord {
	t.Helper()
	records, err := config.LoadSpawnAssignments(filepath.Join(dir, config.SpawnAssignmentsFile))
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	return records[0]
}

func assertNativeSpawnTransport(t *testing.T, log string, wantCreates int) {
	t.Helper()
	got := mustRead(t, log)
	if strings.Count(got, "new-session ") != wantCreates {
		t.Fatalf("presentation creates=%d want=%d log=%s", strings.Count(got, "new-session "), wantCreates, got)
	}
	for _, forbidden := range []string{"load-buffer", "paste-buffer", "send-keys", "capture-pane", "threads search", "threads export", "kill-window", "archive"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("native route used forbidden %q: %s", forbidden, got)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
