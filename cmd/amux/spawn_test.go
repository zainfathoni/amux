package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

const testPhysicalHost = "host-exact"

func TestGeneralizedSpawnAdmissionIsClosedBeforeMutation(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	creates := 0
	spawnCreateThread = func(string, string) (string, error) {
		creates++
		return "T-unexpected", nil
	}
	args := spawnArgs(dir, workdir, "prepare", "", "", "", false)
	args = removeFlag(args, "--native-capability", true)
	env, err := executeSpawnResult(args, "ordinary work")
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("error=%v envelope=%+v", err, env)
	}
	for _, want := range []string{config.SpawnCutoverGeneration, "authenticated native Amp thread creation", "exact intended Orb or live runner and workdir", "without amux adoption"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rejection lacks %q: %v", want, err)
		}
	}
	if creates != 0 {
		t.Fatalf("generalized rejection created %d threads", creates)
	}
	assertConfigDirEmpty(t, dir)
}

func TestProjectlessPhysicalHostExceptionBindsExactlyAndCreatesNoLifecycleState(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	creates := 0
	spawnCreateThread = func(gotWorkdir, mode string) (string, error) {
		creates++
		if gotWorkdir != workdir || mode != "high" {
			t.Fatalf("create workdir/mode=%q/%q", gotWorkdir, mode)
		}
		return "T-exact", nil
	}
	prompt := "exact private assignment\nsecond line"
	prepared := executeSpawnSuccess(t, spawnArgs(dir, workdir, "prepare", "", "", "", true), prompt)
	if len(prepared.Successful) != 1 || prepared.Successful[0].Resource.Thread != "T-exact" || prepared.Successful[0].Assignment == nil || prepared.Successful[0].Assignment.LocalOwnership != "absent" || prepared.Successful[0].Assignment.LocalPresentation != "absent" {
		t.Fatalf("prepare=%+v", prepared)
	}
	record := loadOnlySpawnAssignment(t, dir)
	if record.SchemaVersion != config.SpawnAssignmentProjectlessHostSchemaVersion || record.Admission != config.SpawnAssignmentProjectlessHostAdmission || record.PhysicalHost != testPhysicalHost || record.Workdir != workdir || record.Group != "" || record.Thread != "T-exact" || record.Phase != config.SpawnAssignmentPrepared {
		t.Fatalf("prepared record=%+v", record)
	}
	assertOnlySpawnAssignmentStore(t, dir)

	armed := executeSpawnSuccess(t, spawnArgs(dir, workdir, "arm", "T-exact", "", "", true), prompt)
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
	finalized := executeSpawnSuccess(t, spawnArgs(dir, workdir, "finalize", "T-exact", "authenticated_accepted", cursor, true), prompt)
	if len(finalized.Successful) != 1 || finalized.Successful[0].Assignment.Assignment != "authenticated_accepted" || finalized.Successful[0].Assignment.LocalOwnership != "absent" || finalized.Successful[0].Assignment.LocalPresentation != "absent" || finalized.Successful[0].Assignment.Receipt != "native_latest_cursor" {
		t.Fatalf("finalize=%+v", finalized)
	}
	if creates != 1 || messageCalls != 1 {
		t.Fatalf("creates=%d messages=%d", creates, messageCalls)
	}
	record = loadOnlySpawnAssignment(t, dir)
	if record.Phase != config.SpawnAssignmentFinalized || record.Outcome != config.SpawnAssignmentAuthenticatedAccepted || record.ReceiptCursor != cursor {
		t.Fatalf("finalized record=%+v", record)
	}
	if strings.Contains(mustRead(t, filepath.Join(dir, config.SpawnAssignmentsFile)), prompt) {
		t.Fatal("spawn assignment store leaked prompt")
	}
	assertOnlySpawnAssignmentStore(t, dir)
}

func TestProjectlessPhysicalHostExceptionFailsClosedBeforeCreate(t *testing.T) {
	for _, test := range []struct {
		name       string
		mutateArgs func([]string) []string
		want       string
	}{
		{name: "authorization absent", mutateArgs: func(args []string) []string {
			return removeFlag(args, "--owner-authorized-projectless-physical-host", false)
		}, want: "generalized amux spawn admission closed"},
		{name: "host absent", mutateArgs: func(args []string) []string { return removeFlag(args, "--physical-host", true) }, want: "generalized amux spawn admission closed"},
		{name: "host mismatch", mutateArgs: func(args []string) []string { return replaceFlagValue(args, "--physical-host", "other-host") }, want: "exact physical host mismatch"},
		{name: "group forbidden", mutateArgs: func(args []string) []string { return append(args, "--group", "existing") }, want: "creates no Amux group"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, workdir := setupSpawnCutoverTest(t)
			creates := 0
			spawnCreateThread = func(string, string) (string, error) { creates++; return "T-no", nil }
			_, err := executeSpawnResult(test.mutateArgs(spawnArgs(dir, workdir, "prepare", "", "", "", true)), "prompt")
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) || creates != 0 {
				t.Fatalf("error=%v creates=%d", err, creates)
			}
			assertConfigDirEmpty(t, dir)
		})
	}
}

func TestProjectlessPhysicalHostPrepareFailsClosedWhenLocalHostIsUnavailable(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	spawnPhysicalHost = func() (string, error) { return "", errors.New("hostname unavailable") }
	creates := 0
	spawnCreateThread = func(string, string) (string, error) { creates++; return "T-no", nil }
	_, err := executeSpawnResult(spawnArgs(dir, workdir, "prepare", "", "", "", true), "prompt")
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "determine exact local physical host") || creates != 0 {
		t.Fatalf("error=%v creates=%d", err, creates)
	}
	assertConfigDirEmpty(t, dir)
}

func TestProjectlessPhysicalHostCreationIndeterminacyIsPreservedAndNeverRetried(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	creates := 0
	spawnCreateThread = func(string, string) (string, error) {
		creates++
		return "unparseable", errors.New("connection lost")
	}
	args := spawnArgs(dir, workdir, "prepare", "", "", "", true)
	_, firstErr := executeSpawnResult(args, "prompt")
	if firstErr == nil || result.ExitCode(firstErr) != result.ExitRuntimeFailure || !strings.Contains(firstErr.Error(), "indeterminate") {
		t.Fatalf("first error=%v", firstErr)
	}
	record := loadOnlySpawnAssignment(t, dir)
	if record.Phase != config.SpawnAssignmentCreationArmed || record.PhysicalHost != testPhysicalHost || record.Workdir != workdir {
		t.Fatalf("record=%+v", record)
	}
	_, replayErr := executeSpawnResult(args, "prompt")
	if replayErr == nil || result.ExitCode(replayErr) != result.ExitRejected || !strings.Contains(replayErr.Error(), "will not be retried") || creates != 1 {
		t.Fatalf("replay error=%v creates=%d", replayErr, creates)
	}
}

func TestProjectlessPhysicalHostContinuationRequiresSameAuthorizedHostAndBoundary(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	spawnCreateThread = func(string, string) (string, error) { return "T-bound", nil }
	prompt := "prompt"
	executeSpawnSuccess(t, spawnArgs(dir, workdir, "prepare", "", "", "", true), prompt)

	for _, test := range []struct {
		name   string
		args   []string
		prompt string
		want   string
	}{
		{name: "authorization omitted", args: spawnArgs(dir, workdir, "arm", "T-bound", "", "", false), prompt: prompt, want: "renewed explicit owner authorization"},
		{name: "host changed", args: replaceFlagValue(spawnArgs(dir, workdir, "arm", "T-bound", "", "", true), "--physical-host", "other-host"), prompt: prompt, want: "host binding does not exactly match"},
		{name: "thread changed", args: spawnArgs(dir, workdir, "arm", "T-other", "", "", true), prompt: prompt, want: "does not exactly match"},
		{name: "prompt changed", args: spawnArgs(dir, workdir, "arm", "T-bound", "", "", true), prompt: "changed", want: "does not exactly match"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := executeSpawnResult(test.args, test.prompt)
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v", err)
			}
			if record := loadOnlySpawnAssignment(t, dir); record.Phase != config.SpawnAssignmentPrepared {
				t.Fatalf("record mutated on rejected continuation: %+v", record)
			}
		})
	}
}

func TestProjectlessPhysicalHostContinuationFailsClosedWhenCurrentHostCannotProveBinding(t *testing.T) {
	for _, test := range []struct {
		name string
		host func() (string, error)
		want string
	}{
		{name: "host changed", host: func() (string, error) { return "different-host", nil }, want: "exact physical host mismatch"},
		{name: "host unavailable", host: func() (string, error) { return "", errors.New("hostname unavailable") }, want: "determine exact local physical host"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, workdir := setupSpawnCutoverTest(t)
			spawnCreateThread = func(string, string) (string, error) { return "T-host-bound", nil }
			prompt := "prompt"
			executeSpawnSuccess(t, spawnArgs(dir, workdir, "prepare", "", "", "", true), prompt)
			spawnPhysicalHost = test.host

			_, err := executeSpawnResult(spawnArgs(dir, workdir, "arm", "T-host-bound", "", "", true), prompt)
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v", err)
			}
			if record := loadOnlySpawnAssignment(t, dir); record.Phase != config.SpawnAssignmentPrepared {
				t.Fatalf("record mutated on unprovable host: %+v", record)
			}
		})
	}
}

func TestProjectlessPhysicalHostExceptionRejectsExistingOwnershipAndAssignmentOverlap(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, dir, workdir string)
		want  string
	}{
		{name: "worker", setup: func(t *testing.T, dir, workdir string) {
			if _, err := config.Store(filepath.Join(dir, config.WorkersFile), config.Row{Workspace: "owned", Window: "worker", Workdir: workdir, Thread: "T-owner"}); err != nil {
				t.Fatal(err)
			}
		}, want: "already owned by worker"},
		{name: "worker with unprovable relative workdir", setup: func(t *testing.T, dir, _ string) {
			if err := os.WriteFile(filepath.Join(dir, config.WorkersFile), []byte("owned\tworker\trelative\tT-owner\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "unprovable non-canonical workdir ownership"},
		{name: "runner", setup: func(t *testing.T, dir, workdir string) {
			if _, err := config.StoreRunner(filepath.Join(dir, config.RunnersFile), config.RunnerRow{Workspace: "owned", Workdir: workdir}); err != nil {
				t.Fatal(err)
			}
		}, want: "already owned by Amux Runner"},
		{name: "runner with unprovable relative workdir", setup: func(t *testing.T, dir, _ string) {
			if err := os.WriteFile(filepath.Join(dir, config.RunnersFile), []byte("owned\trelative\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "unprovable non-canonical workdir ownership"},
		{name: "assignment", setup: func(t *testing.T, dir, workdir string) {
			record := config.SpawnAssignmentRecord{
				SchemaVersion: config.SpawnAssignmentSchemaVersion,
				Workspace:     "owned",
				Window:        "assignment",
				Workdir:       workdir,
				Thread:        "T-owner",
				Mode:          "high",
				PromptDigest:  spawnPromptDigest("existing"),
				Phase:         config.SpawnAssignmentPrepared,
				Outcome:       config.SpawnAssignmentNotAttempted,
			}
			if err := config.StoreSpawnAssignment(filepath.Join(dir, config.SpawnAssignmentsFile), record); err != nil {
				t.Fatal(err)
			}
		}, want: "already bound to spawn assignment"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, workdir := setupSpawnCutoverTest(t)
			test.setup(t, dir, workdir)
			creates := 0
			spawnCreateThread = func(string, string) (string, error) { creates++; return "T-no", nil }
			_, err := executeSpawnResult(spawnArgs(dir, workdir, "prepare", "", "", "", true), "prompt")
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) || creates != 0 {
				t.Fatalf("error=%v creates=%d", err, creates)
			}
		})
	}
}

func TestProjectlessPhysicalHostExactCreatePersistenceFailureStaysArmedAndNeverRetries(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	creates, stores := 0, 0
	spawnCreateThread = func(string, string) (string, error) {
		creates++
		return "T-created", nil
	}
	realStore := config.StoreSpawnAssignment
	spawnStoreAssignment = func(path string, record config.SpawnAssignmentRecord) error {
		stores++
		if stores == 2 {
			return errors.New("persistence interrupted")
		}
		return realStore(path, record)
	}
	args := spawnArgs(dir, workdir, "prepare", "", "", "", true)
	_, firstErr := executeSpawnResult(args, "prompt")
	if firstErr == nil || result.ExitCode(firstErr) != result.ExitRuntimeFailure || !strings.Contains(firstErr.Error(), "creation is indeterminate") {
		t.Fatalf("first error=%v", firstErr)
	}
	spawnStoreAssignment = realStore
	if record := loadOnlySpawnAssignment(t, dir); record.Phase != config.SpawnAssignmentCreationArmed || record.Thread != "" {
		t.Fatalf("record=%+v", record)
	}
	_, replayErr := executeSpawnResult(args, "prompt")
	if replayErr == nil || result.ExitCode(replayErr) != result.ExitRejected || !strings.Contains(replayErr.Error(), "will not be retried") || creates != 1 {
		t.Fatalf("replay error=%v creates=%d", replayErr, creates)
	}
}

func TestPreCutoverSpawnDrainsInItsExistingStoreWithoutResendOrDualWrite(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	prompt := "legacy exact prompt"
	record := config.SpawnAssignmentRecord{
		SchemaVersion: config.SpawnAssignmentSchemaVersion,
		Workspace:     "alpha",
		Window:        "worker",
		Workdir:       workdir,
		Thread:        "T-legacy",
		Mode:          "high",
		Group:         "legacy-group",
		PromptDigest:  spawnPromptDigest(prompt),
		Phase:         config.SpawnAssignmentPrepared,
		Outcome:       config.SpawnAssignmentNotAttempted,
	}
	if err := config.StoreSpawnAssignment(filepath.Join(dir, config.SpawnAssignmentsFile), record); err != nil {
		t.Fatal(err)
	}
	otherStores := map[string]string{
		config.WorkersFile:    "alpha\tworker\t" + workdir + "\tT-legacy\n",
		config.GroupsFile:     "legacy-group\tT-legacy\tmember\n",
		config.OperationsFile: "{\"sentinel\":true}\n",
	}
	for name, contents := range otherStores {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	armArgs := spawnArgs(dir, workdir, "arm", "T-legacy", "", "", false)
	armArgs = append(armArgs, "--group", "legacy-group")
	executeSpawnSuccess(t, armArgs, prompt)
	if record := loadOnlySpawnAssignment(t, dir); record.SchemaVersion != config.SpawnAssignmentSchemaVersion || record.Phase != config.SpawnAssignmentArmed || record.Outcome != config.SpawnAssignmentIndeterminate {
		t.Fatalf("armed legacy record=%+v", record)
	}

	finalizeArgs := spawnArgs(dir, workdir, "finalize", "T-legacy", "authenticated_accepted", "cursor", false)
	finalizeArgs = append(finalizeArgs, "--group", "legacy-group")
	executeSpawnSuccess(t, finalizeArgs, prompt)
	if record := loadOnlySpawnAssignment(t, dir); record.Phase != config.SpawnAssignmentFinalized || record.Outcome != config.SpawnAssignmentAuthenticatedAccepted || record.ReceiptCursor != "cursor" {
		t.Fatalf("finalized legacy record=%+v", record)
	}
	for name, want := range otherStores {
		if got := mustRead(t, filepath.Join(dir, name)); got != want {
			t.Errorf("%s changed during drain\ngot:  %q\nwant: %q", name, got, want)
		}
	}
}

func TestAlreadyArmedPreCutoverSpawnFinalizesWithoutCreateArmOrLifecycleWrite(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	prompt := "already sent legacy prompt"
	record := config.SpawnAssignmentRecord{
		SchemaVersion: config.SpawnAssignmentSchemaVersion,
		Workspace:     "alpha",
		Window:        "worker",
		Workdir:       workdir,
		Thread:        "T-armed-legacy",
		Mode:          "high",
		PromptDigest:  spawnPromptDigest(prompt),
		Phase:         config.SpawnAssignmentArmed,
		Outcome:       config.SpawnAssignmentIndeterminate,
	}
	if err := config.StoreSpawnAssignment(filepath.Join(dir, config.SpawnAssignmentsFile), record); err != nil {
		t.Fatal(err)
	}
	spawnCreateThread = func(string, string) (string, error) {
		t.Fatal("already-armed drain attempted replacement creation")
		return "", nil
	}
	finalizeArgs := spawnArgs(dir, workdir, "finalize", record.Thread, "authenticated_accepted", "existing-message-cursor", false)
	executeSpawnSuccess(t, finalizeArgs, prompt)

	finalized := loadOnlySpawnAssignment(t, dir)
	if finalized.Phase != config.SpawnAssignmentFinalized || finalized.Outcome != config.SpawnAssignmentAuthenticatedAccepted || finalized.ReceiptCursor != "existing-message-cursor" {
		t.Fatalf("finalized record=%+v", finalized)
	}
	assertOnlySpawnAssignmentStore(t, dir)
}

func TestPreCutoverDrainRejectsPostCutoverFlagsAndUnprovableSchema(t *testing.T) {
	legacy := config.SpawnAssignmentRecord{SchemaVersion: config.SpawnAssignmentSchemaVersion}
	if err := validateSpawnContinuationAdmission(legacy, selectors{PhysicalHost: testPhysicalHost, OwnerAuthorizedProjectlessPhysicalHost: true}); err == nil || !strings.Contains(err.Error(), "unchanged legacy boundary") {
		t.Fatalf("legacy flags error=%v", err)
	}
	if err := validateSpawnContinuationAdmission(config.SpawnAssignmentRecord{}, selectors{}); err == nil || !strings.Contains(err.Error(), "cannot prove pre-cutover") {
		t.Fatalf("unprovable schema error=%v", err)
	}
}

func TestSpawnDryRunAppliesAdmissionAndExactStateChecksWithoutWriting(t *testing.T) {
	dir, workdir := setupSpawnCutoverTest(t)
	spawnCreateThread = func(string, string) (string, error) { t.Fatal("dry-run created a thread"); return "", nil }
	args := append(spawnArgs(dir, workdir, "prepare", "", "", "", true), "--dry-run")
	env := executeSpawnSuccess(t, args, "prompt")
	if len(env.Planned) != 1 || env.Planned[0].Assignment.LocalOwnership != "absent" {
		t.Fatalf("dry-run=%+v", env)
	}
	assertConfigDirEmpty(t, dir)
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

func setupSpawnCutoverTest(t *testing.T) (dir, workdir string) {
	t.Helper()
	dir, workdir = t.TempDir(), t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldCreate, oldStore, oldHost := spawnCreateThread, spawnStoreAssignment, spawnPhysicalHost
	spawnCreateThread = func(string, string) (string, error) { t.Fatal("unexpected create"); return "", nil }
	spawnStoreAssignment = config.StoreSpawnAssignment
	spawnPhysicalHost = func() (string, error) { return testPhysicalHost, nil }
	t.Cleanup(func() {
		spawnCreateThread, spawnStoreAssignment, spawnPhysicalHost = oldCreate, oldStore, oldHost
	})
	return dir, workdir
}

func spawnArgs(dir, workdir, phase, thread, outcome, cursor string, exception bool) []string {
	args := []string{"--json", "--config-dir", dir, "spawn", "--assignment-phase", phase, "--native-capability", "existing-thread-message-v1", "--workdir", workdir, "--workspace", "alpha", "--window", "worker", "--mode", "high", "--prompt-file", "-"}
	if exception {
		args = append(args, "--owner-authorized-projectless-physical-host", "--physical-host", testPhysicalHost)
	}
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

func executeSpawnSuccess(t *testing.T, args []string, prompt string) result.Envelope {
	t.Helper()
	env, err := executeSpawnResult(args, prompt)
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return env
}

func executeSpawnResult(args []string, prompt string) (result.Envelope, error) {
	var stdout bytes.Buffer
	err := (app{stdin: strings.NewReader(prompt), stdout: &stdout}).execute(args)
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		return env, fmt.Errorf("decode result %q: %w", stdout.String(), decodeErr)
	}
	return env, err
}

func loadOnlySpawnAssignment(t *testing.T, dir string) config.SpawnAssignmentRecord {
	t.Helper()
	records, err := config.LoadSpawnAssignments(filepath.Join(dir, config.SpawnAssignmentsFile))
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	return records[0]
}

func assertOnlySpawnAssignmentStore(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != config.SpawnAssignmentsFile {
		t.Fatalf("exception created unrelated lifecycle state: %+v", entries)
	}
}

func assertConfigDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected/dry-run spawn wrote config: %+v", entries)
	}
}

func spawnPromptDigest(prompt string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(prompt)))
}

func removeFlag(args []string, name string, hasValue bool) []string {
	for i, arg := range args {
		if arg != name {
			continue
		}
		end := i + 1
		if hasValue {
			end++
		}
		return append(append([]string{}, args[:i]...), args[end:]...)
	}
	return args
}

func replaceFlagValue(args []string, name, value string) []string {
	args = append([]string{}, args...)
	for i := range args {
		if args[i] == name {
			args[i+1] = value
			return args
		}
	}
	return args
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
