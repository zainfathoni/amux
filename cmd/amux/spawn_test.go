package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

func TestSpawnRetainsExactOwnershipButReturnsIndeterminateWhenInputCommandsLackDeliveryAcknowledgement(t *testing.T) {
	dir, workdir, log, pasted := setupSpawnTest(t, "")
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "issue-297", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	groupCalls := installSupportedGroupAmp(t, nil)
	var createCalls int
	spawnCreateThread = func(gotWorkdir, mode string) (string, error) {
		createCalls++
		if gotWorkdir != workdir || mode != "medium" {
			t.Fatalf("create cwd/mode = %q/%q", gotWorkdir, mode)
		}
		return "T-new", nil
	}
	setReadySpawnPane(t, workdir, "T-new")
	prompt := "first line\nsecond line\n"
	var stdout bytes.Buffer
	err := (app{stdin: strings.NewReader(prompt), stdout: &stdout}).execute(spawnArgs(dir, workdir, "--group", "issue-297", "--prompt-file", "-"))
	for _, want := range []string{"spawn retained-indeterminate", "thread=T-new", "tmux=alpha/worker window=@1 pane=%1", "input-attempt=one-paste-one-enter-completed", "delivery-acknowledgement=unavailable", "completed-persistence-phases=persist-worker,persist-group,ensure-label", "assignment delivery and task execution are unproven", "automatic retry, repaste, submit, cleanup, archive, search, reconciliation, and alternate receivers are prohibited"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("retained-indeterminate error lacks %q: %v", want, err)
		}
	}
	if result.ExitCode(err) != result.ExitRuntimeFailure || createCalls != 1 || !strings.HasPrefix(stdout.String(), "T-new\n") || !strings.Contains(stdout.String(), "RETAINED-INDETERMINATE\tT-new\talpha/worker\t@1\t%1\tinput=one-paste-one-enter-completed\tcompleted=persist-worker,persist-group,ensure-label\tdelivery=unacknowledged") {
		t.Fatalf("create calls=%d stdout=%q", createCalls, stdout.String())
	}
	if countMutationCommands(*groupCalls) != 1 {
		t.Fatalf("group label mutations=%d calls=%+v", countMutationCommands(*groupCalls), *groupCalls)
	}
	gotLog := readSpawnTestFile(t, log)
	for _, pair := range []struct {
		needle string
		count  int
	}{{"new-session ", 1}, {"load-buffer ", 1}, {"paste-buffer ", 1}, {"send-keys -t %1 Enter", 1}} {
		if got := strings.Count(gotLog, pair.needle); got != pair.count {
			t.Fatalf("%q calls=%d, want %d\n%s", pair.needle, got, pair.count, gotLog)
		}
	}
	if got := readSpawnTestFile(t, pasted); got != prompt {
		t.Fatalf("pasted prompt = %q", got)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Thread != "T-new" || rows[0].Workdir != workdir {
		t.Fatalf("stored workers=%+v err=%v", rows, err)
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || membershipIndex(memberships, "issue-297", "T-new") < 0 {
		t.Fatalf("stored groups=%+v err=%v", memberships, err)
	}
	for _, forbidden := range []string{"capture-pane", "threads search", "threads list", "threads export", "threads raw", "kill-window", "archive"} {
		if strings.Contains(gotLog, forbidden) {
			t.Fatalf("spawn used forbidden recovery %q:\n%s", forbidden, gotLog)
		}
	}
}

func TestSpawnAliasesReturnExactRetainedIndeterminateJSONWithoutLeakingPrompt(t *testing.T) {
	for _, command := range [][]string{{"spawn"}, {"worker", "spawn"}} {
		t.Run(strings.Join(command, "-"), func(t *testing.T) {
			dir, workdir, log, _ := setupSpawnTest(t, "")
			createCalls := 0
			spawnCreateThread = func(string, string) (string, error) {
				createCalls++
				return "T-json", nil
			}
			setReadySpawnPane(t, workdir, "T-json")
			args := []string{"--json", "--config-dir", dir}
			args = append(args, command...)
			args = append(args, "--workdir", workdir, "--workspace", "alpha", "--window", "worker", "--prompt-file", "-")
			const prompt = "private assignment bytes"
			var stdout bytes.Buffer
			err := (app{stdin: strings.NewReader(prompt), stdout: &stdout}).execute(args)
			if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure || createCalls != 1 {
				t.Fatalf("error=%v exit=%d creates=%d", err, result.ExitCode(err), createCalls)
			}
			var env result.Envelope
			if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if len(env.Successful) != 2 || env.Successful[0].Action != "attempt-input" || env.Successful[1].Action != "persist-worker" || len(env.Failed) != 1 || env.Failed[0].Action != "acknowledge-delivery" {
				t.Fatalf("retained JSON phases=%+v", env)
			}
			failed := env.Failed[0]
			if failed.Resource.Thread != "T-json" || failed.Worker == nil || failed.Worker.Workspace != "alpha" || failed.Worker.Window != "worker" || failed.Worker.WindowID != "@1" || failed.Worker.PaneID != "%1" || failed.Worker.LocalState != "retained_indeterminate" || failed.Worker.AssignmentState != "retained_indeterminate" || failed.Error == nil || !strings.Contains(failed.Error.Message, "completed-persistence-phases=persist-worker") {
				t.Fatalf("retained JSON identity=%+v", failed)
			}
			if strings.Contains(stdout.String(), prompt) {
				t.Fatalf("JSON leaked prompt: %s", stdout.String())
			}
			gotLog := readSpawnTestFile(t, log)
			if strings.Count(gotLog, "new-session ") != 1 || strings.Count(gotLog, "paste-buffer ") != 1 || strings.Count(gotLog, "send-keys -t %1 Enter") != 1 || strings.Contains(gotLog, "capture-pane") || strings.Contains(gotLog, "kill-window") {
				t.Fatalf("alias retried, inspected, or cleaned up:\n%s", gotLog)
			}
		})
	}
}

func TestSpawnGroupRetainedIndeterminateJSONPreservesEveryPersistencePhase(t *testing.T) {
	dir, workdir, _, _ := setupSpawnTest(t, "")
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "group", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	spawnCreateThread = func(string, string) (string, error) { return "T-grouped", nil }
	setReadySpawnPane(t, workdir, "T-grouped")
	var stdout bytes.Buffer
	err := (app{stdin: strings.NewReader("secret group assignment"), stdout: &stdout}).execute(append([]string{"--json"}, spawnArgs(dir, workdir, "--group", "group", "--prompt-file", "-")...))
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("error=%v exit=%d", err, result.ExitCode(err))
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	wantActions := []string{"attempt-input", "persist-worker", "persist-group", "ensure-label"}
	if len(env.Successful) != len(wantActions) {
		t.Fatalf("successful phases=%+v", env.Successful)
	}
	for i, want := range wantActions {
		if env.Successful[i].Action != want {
			t.Fatalf("successful phase %d=%q, want %q", i, env.Successful[i].Action, want)
		}
	}
	if len(env.Failed) != 1 || env.Failed[0].Action != "acknowledge-delivery" || env.Failed[0].Error == nil || !strings.Contains(env.Failed[0].Error.Message, "completed-persistence-phases=persist-worker,persist-group,ensure-label") {
		t.Fatalf("delivery acknowledgement outcome=%+v", env.Failed)
	}
}

func TestSpawnPreservesExactModeAndMultilineFilePrompt(t *testing.T) {
	dir, workdir, _, pasted := setupSpawnTest(t, "")
	promptPath := filepath.Join(t.TempDir(), "prompt.txt")
	prompt := "one\ntwo\nthree"
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}
	spawnCreateThread = func(gotWorkdir, mode string) (string, error) {
		if gotWorkdir != workdir || mode != "ultra" {
			t.Fatalf("create cwd/mode = %q/%q", gotWorkdir, mode)
		}
		return "T-file", nil
	}
	setReadySpawnPane(t, workdir, "T-file")
	if err := (app{}).execute(spawnArgs(dir, workdir, "--mode", "ultra", "--prompt-file", promptPath)); err == nil || !strings.Contains(err.Error(), "retained-indeterminate") {
		t.Fatalf("spawn error=%v", err)
	}
	if got := readSpawnTestFile(t, pasted); got != prompt {
		t.Fatalf("pasted prompt = %q", got)
	}
}

func TestSpawnDryRunValidatesEverythingWithoutMutation(t *testing.T) {
	dir, workdir, log, _ := setupSpawnTest(t, "")
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) {
		createCalls++
		return "T-unexpected", nil
	}
	var stdout bytes.Buffer
	args := append([]string{"--dry-run"}, spawnArgs(dir, workdir, "--prompt-file", "-")...)
	if err := (app{stdin: strings.NewReader("secret dry prompt"), stdout: &stdout}).execute(args); err != nil {
		t.Fatal(err)
	}
	if createCalls != 0 || strings.Contains(stdout.String(), "secret dry prompt") {
		t.Fatalf("dry-run create calls=%d output=%q", createCalls, stdout.String())
	}
	if !strings.Contains(stdout.String(), "return retained-indeterminate") || !strings.Contains(stdout.String(), "no supported delivery acknowledgement") {
		t.Fatalf("dry-run obscured real outcome: %q", stdout.String())
	}
	if got := readSpawnTestFile(t, log); strings.Contains(got, "new-session") || strings.Contains(got, "load-buffer") || strings.Contains(got, "send-keys") {
		t.Fatalf("dry-run mutated tmux:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote worker catalog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "amux", lock.FileName)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote mutation lock: %v", err)
	}
}

func TestSpawnGroupDryRunPreflightsCapabilityWithoutMutation(t *testing.T) {
	dir, workdir, log, _ := setupSpawnTest(t, "")
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	groupsPath := filepath.Join(dir, config.GroupsFile)
	if err := config.WriteGroups(groupsPath, []config.GroupMembership{{Group: "group", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	before := readSpawnTestFile(t, groupsPath)
	commands := installSupportedGroupAmp(t, nil)
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) {
		createCalls++
		return "T-unexpected", nil
	}
	var stdout bytes.Buffer
	args := append([]string{"--dry-run"}, spawnArgs(dir, workdir, "--group", "group", "--prompt-file", "-")...)
	if err := (app{stdin: strings.NewReader("bounded prompt"), stdout: &stdout}).execute(args); err != nil {
		t.Fatal(err)
	}
	if createCalls != 0 || countMutationCommands(*commands) != 0 || len(*commands) != 2 {
		t.Fatalf("dry-run creates=%d Amp calls=%+v", createCalls, *commands)
	}
	if !strings.Contains(stdout.String(), "add-only ensure its Amp label") {
		t.Fatalf("dry-run omitted label plan: %q", stdout.String())
	}
	if got := readSpawnTestFile(t, groupsPath); got != before {
		t.Fatalf("dry-run mutated groups: before=%q after=%q", before, got)
	}
	if got := readSpawnTestFile(t, log); strings.Contains(got, "new-session") || strings.Contains(got, "load-buffer") || strings.Contains(got, "send-keys") {
		t.Fatalf("dry-run mutated tmux:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote worker catalog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "amux", lock.FileName)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote mutation lock: %v", err)
	}
}

func TestSpawnRejectsOwnershipConflictsBeforeCreate(t *testing.T) {
	dir, workdir, _, _ := setupSpawnTest(t, "")
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) {
		createCalls++
		return "T-unexpected", nil
	}
	writeWorkerRegistry(t, dir, "alpha\tworker\t"+t.TempDir()+"\tT-existing\n")
	err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
	if err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("catalog conflict error=%v", err)
	}
	if createCalls != 0 {
		t.Fatalf("catalog conflict created %d threads", createCalls)
	}
}

func TestSpawnRejectsNoncanonicalWorkdirAndExistingGroupOrTmuxConflicts(t *testing.T) {
	t.Run("noncanonical workdir", func(t *testing.T) {
		dir, workdir, _, _ := setupSpawnTest(t, "")
		err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir+string(os.PathSeparator)+".", "--prompt-file", "-"))
		if err == nil || !strings.Contains(err.Error(), "must be the canonical existing path") {
			t.Fatalf("noncanonical error=%v", err)
		}
	})
	t.Run("missing group", func(t *testing.T) {
		dir, workdir, _, _ := setupSpawnTest(t, "")
		err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--group", "missing-group", "--prompt-file", "-"))
		if err == nil || !strings.Contains(err.Error(), "group missing-group does not exist") {
			t.Fatalf("group error=%v", err)
		}
	})
	t.Run("tmux window", func(t *testing.T) {
		dir, workdir, _, _ := setupSpawnTest(t, "window")
		err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
		if err == nil || !strings.Contains(err.Error(), "tmux window alpha/worker already exists") {
			t.Fatalf("tmux error=%v", err)
		}
	})
}

func TestSpawnUnparseableCreationResultsAreIndeterminateWithoutRawOutput(t *testing.T) {
	for _, resultCase := range []struct {
		name   string
		thread string
		err    error
	}{
		{name: "nonzero without parseable ID", thread: "private stdout from amp", err: errors.New("private stderr from amp")},
		{name: "zero with invalid ID", thread: "private invalid output from amp"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			name := resultCase.name + "/human"
			if jsonOutput {
				name = resultCase.name + "/json"
			}
			t.Run(name, func(t *testing.T) {
				dir, workdir, log, _ := setupSpawnTest(t, "")
				createCalls := 0
				spawnCreateThread = func(string, string) (string, error) {
					createCalls++
					return resultCase.thread, resultCase.err
				}
				args := spawnArgs(dir, workdir, "--prompt-file", "-")
				if jsonOutput {
					args = append([]string{"--json"}, args...)
				}
				var stdout bytes.Buffer
				err := (app{stdin: strings.NewReader("prompt"), stdout: &stdout}).execute(args)
				if err == nil || createCalls != 1 {
					t.Fatalf("creation error=%v calls=%d", err, createCalls)
				}
				want := "thread=creation-indeterminate tmux=not-created requested=alpha/worker"
				if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "preserved without retry or cleanup") || strings.Contains(err.Error(), "private") {
					t.Fatalf("indeterminate error=%v", err)
				}
				if jsonOutput {
					var env result.Envelope
					if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					if len(env.Failed) != 1 || env.Failed[0].Error == nil || !strings.Contains(env.Failed[0].Error.Message, want) || strings.Contains(env.Failed[0].Error.Message, "private") {
						t.Fatalf("indeterminate JSON=%+v", env)
					}
				} else if stdout.Len() != 0 {
					t.Fatalf("human indeterminate stdout=%q", stdout.String())
				}
				if got := readSpawnTestFile(t, log); strings.Contains(got, "new-session") || strings.Contains(got, "new-window") {
					t.Fatalf("indeterminate creation created tmux state:\n%s", got)
				}
			})
		}
	}
}

func TestSpawnCreateErrorWithExactThreadPreservesIdentityWithoutTmux(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		name := "human"
		if jsonOutput {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			dir, workdir, log, _ := setupSpawnTest(t, "")
			spawnCreateThread = func(string, string) (string, error) { return "T-created", errors.New("private late create error") }
			args := spawnArgs(dir, workdir, "--prompt-file", "-")
			if jsonOutput {
				args = append([]string{"--json"}, args...)
			}
			var stdout bytes.Buffer
			err := (app{stdin: strings.NewReader("prompt"), stdout: &stdout}).execute(args)
			if err == nil || !strings.Contains(err.Error(), "thread=T-created") || !strings.Contains(err.Error(), "tmux=not-created requested=alpha/worker") || !strings.Contains(err.Error(), "preserved without retry or cleanup") || strings.Contains(err.Error(), "private") {
				t.Fatalf("late create error=%v", err)
			}
			if jsonOutput {
				var env result.Envelope
				if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if len(env.Failed) != 1 || env.Failed[0].Error == nil || !strings.Contains(env.Failed[0].Error.Message, "thread=T-created") || strings.Contains(env.Failed[0].Error.Message, "private") {
					t.Fatalf("late create JSON=%+v", env)
				}
			}
			if got := readSpawnTestFile(t, log); strings.Contains(got, "new-session") || strings.Contains(got, "new-window") {
				t.Fatalf("late create error created tmux state:\n%s", got)
			}
		})
	}
}

func TestSpawnTmuxCreateFailureReportsIndeterminateIdentityWithoutRetry(t *testing.T) {
	dir, workdir, log, _ := setupSpawnTest(t, "create")
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) {
		createCalls++
		return "T-created", nil
	}
	err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
	if err == nil || !strings.Contains(err.Error(), "thread=T-created") || !strings.Contains(err.Error(), "tmux=creation-indeterminate requested=alpha/worker") {
		t.Fatalf("tmux create error=%v", err)
	}
	if got := readSpawnTestFile(t, log); createCalls != 1 || strings.Count(got, "new-session ") != 1 || strings.Contains(got, "load-buffer") || strings.Contains(got, "kill-window") {
		t.Fatalf("tmux failure create=%d log:\n%s", createCalls, got)
	}
}

func TestCreateLocalAmpThreadUsesExactModeAndCwdOnce(t *testing.T) {
	workdir := t.TempDir()
	bin := t.TempDir()
	log := filepath.Join(bin, "amp.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
printf '%s\n%s\n' "$(pwd)" "$*" >> `+shellSingleQuote(log)+`
printf 'T-exact\n'
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	thread, err := createLocalAmpThread(workdir, "high")
	if err != nil || thread != "T-exact" {
		t.Fatalf("thread=%q err=%v", thread, err)
	}
	if got, want := readSpawnTestFile(t, log), workdir+"\nthreads new --mode high\n"; got != want {
		t.Fatalf("amp invocation=%q, want %q", got, want)
	}
}

func TestSpawnPromptIsBounded(t *testing.T) {
	_, err := (app{stdin: strings.NewReader(strings.Repeat("x", maxSpawnPromptBytes+1))}).readSpawnPrompt("-")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized prompt error=%v", err)
	}
}

func TestSpawnPostCreateFailuresPreserveIdentityWithoutRetryOrCleanup(t *testing.T) {
	for _, phase := range []string{"paste", "enter"} {
		t.Run(phase, func(t *testing.T) {
			dir, workdir, log, _ := setupSpawnTest(t, phase)
			if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "group", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
				t.Fatal(err)
			}
			createCalls := 0
			spawnCreateThread = func(string, string) (string, error) {
				createCalls++
				return "T-preserved", nil
			}
			setReadySpawnPane(t, workdir, "T-preserved")
			err := (app{stdin: strings.NewReader("sensitive prompt")}).execute(spawnArgs(dir, workdir, "--group", "group", "--prompt-file", "-"))
			wantInput := "paste-completed-enter-failed-indeterminate"
			if phase == "paste" {
				wantInput = "paste-failed-enter-not-attempted"
			}
			if err == nil || !strings.Contains(err.Error(), "spawn retained-indeterminate") || !strings.Contains(err.Error(), "thread=T-preserved") || !strings.Contains(err.Error(), "tmux=alpha/worker window=@1 pane=%1") || !strings.Contains(err.Error(), "input-attempt="+wantInput) || !strings.Contains(err.Error(), "completed-persistence-phases=persist-worker,persist-group,ensure-label") {
				t.Fatalf("post-create error=%v", err)
			}
			got := readSpawnTestFile(t, log)
			if createCalls != 1 || strings.Count(got, "new-session ") != 1 || strings.Count(got, "load-buffer ") != 1 || strings.Contains(got, "kill-window") || strings.Contains(got, "archive") {
				t.Fatalf("phase=%s create=%d log:\n%s", phase, createCalls, got)
			}
			wantEnter := 1
			if phase == "paste" {
				wantEnter = 0
			}
			if strings.Count(got, "paste-buffer ") != 1 || strings.Count(got, "send-keys -t %1 Enter") != wantEnter {
				t.Fatalf("phase=%s duplicate input log:\n%s", phase, got)
			}
			if phase == "paste" && strings.Count(got, "delete-buffer ") != 1 {
				t.Fatalf("failed paste retained sensitive buffer:\n%s", got)
			}
			rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
			if loadErr != nil || len(rows) != 1 || rows[0].Thread != "T-preserved" || rows[0].AssignmentState != config.WorkerAssignmentRetainedIndeterminate {
				t.Fatalf("retained worker=%+v err=%v", rows, loadErr)
			}
			memberships, groupErr := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
			if groupErr != nil || membershipIndex(memberships, "group", "T-preserved") < 0 {
				t.Fatalf("retained group=%+v err=%v", memberships, groupErr)
			}
		})
	}
}

func TestSpawnLoadBufferFailureScrubsExactBufferAndDoesNotPasteOrEnter(t *testing.T) {
	dir, workdir, log, pasted := setupSpawnTest(t, "load")
	spawnCreateThread = func(string, string) (string, error) { return "T-load-fail", nil }
	setReadySpawnPane(t, workdir, "T-load-fail")
	prompt := "sensitive prompt"
	err := (app{stdin: strings.NewReader(prompt)}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
	if err == nil || !strings.Contains(err.Error(), "input-attempt=paste-failed-enter-not-attempted") {
		t.Fatalf("load failure=%v", err)
	}
	if got := readSpawnTestFile(t, pasted); got != prompt {
		t.Fatalf("load did not consume complete stdin: %q", got)
	}
	got := readSpawnTestFile(t, log)
	if strings.Count(got, "load-buffer ") != 1 || strings.Count(got, "delete-buffer ") != 1 || strings.Contains(got, "paste-buffer ") || strings.Contains(got, "send-keys -t %1 Enter") {
		t.Fatalf("load failure transport log:\n%s", got)
	}
	var loadName, deleteName string
	for _, line := range strings.Split(got, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "load-buffer" && fields[1] == "-b" {
			loadName = fields[2]
		}
		if len(fields) >= 3 && fields[0] == "delete-buffer" && fields[1] == "-b" {
			deleteName = fields[2]
		}
	}
	if loadName == "" || loadName != deleteName || !strings.HasPrefix(loadName, "amux-spawn-") {
		t.Fatalf("load buffer=%q delete buffer=%q log:\n%s", loadName, deleteName, got)
	}
}

func TestSpawnReadsPromptBeforeLockAndHoldsLockThroughCreate(t *testing.T) {
	dir, workdir, _, _ := setupSpawnTest(t, "")
	lockPath, err := lock.MachinePath()
	if err != nil {
		t.Fatal(err)
	}
	external, err := lock.Acquire(context.Background(), lockPath, lock.Owner{Command: "test prompt gate"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = external.Release() })
	oldWait := mutationLockWait
	mutationLockWait = time.Second
	t.Cleanup(func() { mutationLockWait = oldWait })

	createHeld := make(chan bool, 1)
	spawnCreateThread = func(string, string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		contender, acquireErr := lock.Acquire(ctx, lockPath, lock.Owner{Command: "create contender"})
		if acquireErr == nil {
			_ = contender.Release()
			createHeld <- false
		} else {
			var busy *lock.BusyError
			createHeld <- errors.As(acquireErr, &busy)
		}
		return "T-lock-order", nil
	}
	setReadySpawnPane(t, workdir, "T-lock-order")
	reader, writer := io.Pipe()
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- (app{stdin: reader}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
	}()
	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(writer, "prompt before lock")
		closeErr := writer.Close()
		if writeErr != nil {
			writeDone <- writeErr
			return
		}
		writeDone <- closeErr
	}()
	select {
	case writeErr := <-writeDone:
		if writeErr != nil {
			t.Fatal(writeErr)
		}
	case <-time.After(500 * time.Millisecond):
		_ = external.Release()
		_ = writer.CloseWithError(errors.New("prompt read remained blocked behind mutation lock"))
		t.Fatal("spawn did not read prompt before attempting the mutation lock")
	}
	if err := external.Release(); err != nil {
		t.Fatal(err)
	}
	if err := <-resultCh; err == nil || !strings.Contains(err.Error(), "retained-indeterminate") {
		t.Fatalf("spawn error=%v", err)
	}
	if held := <-createHeld; !held {
		t.Fatal("spawnCreateThread ran without holding the machine mutation lock")
	}
}

func TestSpawnGroupCapabilityRejectsBeforeThreadCreation(t *testing.T) {
	dir, workdir, _, _ := setupSpawnTest(t, "")
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "group", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	installGroupAmp(t, "old\n", supportedGroupHelp, nil)
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) {
		createCalls++
		return "T-unexpected", nil
	}
	err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--group", "group", "--prompt-file", "-"))
	if err == nil || result.ExitCode(err) != result.ExitRejected || createCalls != 0 {
		t.Fatalf("capability preflight error=%v exit=%d creates=%d", err, result.ExitCode(err), createCalls)
	}
}

func TestSpawnReportsWorkerAndGroupBeforeLabelFailureWithoutRollback(t *testing.T) {
	dir, workdir, log, _ := setupSpawnTest(t, "")
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "group", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	commands := installSupportedGroupAmp(t, func(args []string) ([]byte, error) {
		if reflect.DeepEqual(args, []string{"threads", "label", "T-group-fail", "group"}) {
			return []byte("label rejected"), errors.New("exit status 1")
		}
		return nil, nil
	})
	spawnCreateThread = func(string, string) (string, error) { return "T-group-fail", nil }
	setReadySpawnPane(t, workdir, "T-group-fail")
	var stdout bytes.Buffer
	err := (app{stdin: strings.NewReader("prompt"), stdout: &stdout}).execute(append([]string{"--json"}, spawnArgs(dir, workdir, "--group", "group", "--prompt-file", "-")...))
	if err == nil || !strings.Contains(err.Error(), "thread=T-group-fail") || !strings.Contains(err.Error(), "tmux=alpha/worker window=@1 pane=%1") || !strings.Contains(err.Error(), "completed-persistence-phases=persist-worker,persist-group") {
		t.Fatalf("group label failure=%v", err)
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(env.Successful) != 3 || env.Successful[0].Action != "attempt-input" || env.Successful[1].Action != "persist-worker" || env.Successful[2].Action != "persist-group" || len(env.Failed) != 1 || env.Failed[0].Action != "ensure-label" || env.Failed[0].Error == nil || !strings.Contains(env.Failed[0].Error.Message, "thread=T-group-fail") {
		t.Fatalf("partial group envelope=%+v", env)
	}
	if countMutationCommands(*commands) != 1 {
		t.Fatalf("label attempts=%d calls=%+v", countMutationCommands(*commands), *commands)
	}
	if got := readSpawnTestFile(t, log); strings.Count(got, "new-session ") != 1 || strings.Count(got, "paste-buffer ") != 1 || strings.Count(got, "send-keys -t %1 Enter") != 1 || strings.Contains(got, "kill-window") {
		t.Fatalf("group failure retried or cleaned state:\n%s", got)
	}
	rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	memberships, groupErr := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if loadErr != nil || len(rows) != 1 || groupErr != nil || membershipIndex(memberships, "group", "T-group-fail") < 0 {
		t.Fatalf("preserved worker=%+v workerErr=%v groups=%+v groupErr=%v", rows, loadErr, memberships, groupErr)
	}
}

func TestSpawnReportsPersistedWorkerWhenGroupStoreFails(t *testing.T) {
	dir, workdir, _, _ := setupSpawnTest(t, "")
	groupsPath := filepath.Join(dir, config.GroupsFile)
	if err := config.WriteGroups(groupsPath, []config.GroupMembership{{Group: "group", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	spawnCreateThread = func(string, string) (string, error) {
		if err := os.Remove(groupsPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(groupsPath, 0o700); err != nil {
			t.Fatal(err)
		}
		return "T-group-store-fail", nil
	}
	setReadySpawnPane(t, workdir, "T-group-store-fail")
	var stdout bytes.Buffer
	err := (app{stdin: strings.NewReader("prompt"), stdout: &stdout}).execute(append([]string{"--json"}, spawnArgs(dir, workdir, "--group", "group", "--prompt-file", "-")...))
	if err == nil || !strings.Contains(err.Error(), "persist exact group member") || !strings.Contains(err.Error(), "thread=T-group-store-fail") || !strings.Contains(err.Error(), "completed-persistence-phases=persist-worker") {
		t.Fatalf("group store failure=%v", err)
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(env.Successful) != 2 || env.Successful[0].Action != "attempt-input" || env.Successful[1].Action != "persist-worker" || len(env.Failed) != 1 || env.Failed[0].Action != "persist-group" {
		t.Fatalf("group store envelope=%+v", env)
	}
	rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if loadErr != nil || len(rows) != 1 || rows[0].Thread != "T-group-store-fail" {
		t.Fatalf("preserved worker=%+v err=%v", rows, loadErr)
	}
}

func setupSpawnTest(t *testing.T, fail string) (dir, workdir, log, pasted string) {
	t.Helper()
	dir, workdir = t.TempDir(), t.TempDir()
	bin := t.TempDir()
	log, pasted = filepath.Join(bin, "tmux.log"), filepath.Join(bin, "prompt")
	script := `#!/bin/sh
printf '%s\n' "$*" >> ` + shellSingleQuote(log) + `
case "$1" in
  has-session) test "` + fail + `" = window; exit $? ;;
  new-session) test "` + fail + `" = create && exit 1; printf 'alpha\tworker\t@1\t%%1\n'; exit 0 ;;
  load-buffer) cat > ` + shellSingleQuote(pasted) + `; test "` + fail + `" != load; exit $? ;;
  paste-buffer) test "` + fail + `" != paste; exit $? ;;
  delete-buffer) exit 0 ;;
  send-keys) test "` + fail + `" != enter; exit $? ;;
  list-windows) printf 'worker\n'; exit 0 ;;
esac
exit 77
`
	writeExecutable(t, filepath.Join(bin, "tmux"), script)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	installSupportedGroupAmp(t, nil)
	oldCreate := spawnCreateThread
	oldInspect, oldLimit, oldPoll, oldSettle := spawnInspectPaneByID, spawnReadinessLimit, spawnReadinessPoll, spawnReadinessSettle
	spawnCreateThread = func(string, string) (string, error) {
		t.Fatal("preflight test unexpectedly reached Amp thread creation")
		return "", errors.New("unreachable")
	}
	spawnReadinessLimit, spawnReadinessPoll, spawnReadinessSettle = time.Millisecond, time.Microsecond, 0
	t.Cleanup(func() {
		spawnCreateThread = oldCreate
		spawnInspectPaneByID, spawnReadinessLimit, spawnReadinessPoll, spawnReadinessSettle = oldInspect, oldLimit, oldPoll, oldSettle
	})
	return dir, workdir, log, pasted
}

func setReadySpawnPane(t *testing.T, workdir, thread string) {
	t.Helper()
	command := tmux.ContinueCommandWithEnv(workdir, thread, map[string]string{
		"AMUX_WORKSPACE": "alpha", "AMUX_SESSION": "alpha", "AMUX_WINDOW": "worker", "AMUX_THREAD_ID": thread, "AMUX_WORKDIR": workdir,
	})
	spawnInspectPaneByID = func(string) (tmux.WindowPane, error) {
		return tmux.WindowPane{Session: "alpha", Window: "worker", WindowID: "@1", PaneID: "%1", Path: workdir, Command: "amp", StartCommand: command}, nil
	}
}

func spawnArgs(dir, workdir string, tail ...string) []string {
	args := []string{"--config-dir", dir, "spawn", "--workdir", workdir, "--workspace", "alpha", "--window", "worker"}
	return append(args, tail...)
}

func readSpawnTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
