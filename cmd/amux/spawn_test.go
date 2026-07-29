package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/tmux"
)

func TestSpawnCreatesOncePastesOnceSubmitsOnceAndPersists(t *testing.T) {
	dir, workdir, log, pasted := setupSpawnTest(t, "")
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "issue-297", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 || strings.TrimSpace(stdout.String()) != "T-new" {
		t.Fatalf("create calls=%d stdout=%q", createCalls, stdout.String())
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
	if err := (app{}).execute(spawnArgs(dir, workdir, "--mode", "ultra", "--prompt-file", promptPath)); err != nil {
		t.Fatal(err)
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

func TestSpawnRejectsRunnerWorkdirAndOwnershipConflictsBeforeCreate(t *testing.T) {
	dir, workdir, _, _ := setupSpawnTest(t, "")
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) {
		createCalls++
		return "T-unexpected", nil
	}
	spawnProcessWorkdir = func(int) (string, error) { return t.TempDir(), nil }
	err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
	if err == nil || !strings.Contains(err.Error(), "does not own canonical workdir") {
		t.Fatalf("runner mismatch error=%v", err)
	}
	if createCalls != 0 {
		t.Fatalf("runner mismatch created %d threads", createCalls)
	}

	spawnProcessWorkdir = func(int) (string, error) { return workdir, nil }
	writeWorkerRegistry(t, dir, "alpha\tworker\t"+t.TempDir()+"\tT-existing\n")
	err = (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
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

func TestSpawnNativeRejectionCreatesNothing(t *testing.T) {
	dir, workdir, log, _ := setupSpawnTest(t, "")
	createCalls := 0
	spawnCreateThread = func(string, string) (string, error) {
		createCalls++
		return "", errors.New("native rejection")
	}
	err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
	if err == nil || !strings.Contains(err.Error(), "rejected before returning a thread") || createCalls != 1 {
		t.Fatalf("rejection error=%v calls=%d", err, createCalls)
	}
	if got := readSpawnTestFile(t, log); strings.Contains(got, "new-session") || strings.Contains(got, "new-window") {
		t.Fatalf("native rejection created tmux state:\n%s", got)
	}
}

func TestSpawnCreateErrorWithExactThreadPreservesIdentityWithoutTmux(t *testing.T) {
	dir, workdir, log, _ := setupSpawnTest(t, "")
	spawnCreateThread = func(string, string) (string, error) { return "T-created", errors.New("late create error") }
	err := (app{stdin: strings.NewReader("prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
	if err == nil || !strings.Contains(err.Error(), "thread=T-created") || !strings.Contains(err.Error(), "tmux=not-created requested=alpha/worker") || !strings.Contains(err.Error(), "preserved without retry or cleanup") {
		t.Fatalf("late create error=%v", err)
	}
	if got := readSpawnTestFile(t, log); strings.Contains(got, "new-session") || strings.Contains(got, "new-window") {
		t.Fatalf("late create error created tmux state:\n%s", got)
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
			createCalls := 0
			spawnCreateThread = func(string, string) (string, error) {
				createCalls++
				return "T-preserved", nil
			}
			setReadySpawnPane(t, workdir, "T-preserved")
			err := (app{stdin: strings.NewReader("sensitive prompt")}).execute(spawnArgs(dir, workdir, "--prompt-file", "-"))
			if err == nil || !strings.Contains(err.Error(), "thread=T-preserved") || !strings.Contains(err.Error(), "tmux=alpha/worker window=@1 pane=%1") || !strings.Contains(err.Error(), "preserved without retry or cleanup") {
				t.Fatalf("post-create error=%v", err)
			}
			got := readSpawnTestFile(t, log)
			if createCalls != 1 || strings.Count(got, "new-session ") != 1 || strings.Count(got, "load-buffer ") != 1 || strings.Contains(got, "kill-window") || strings.Contains(got, "archive") {
				t.Fatalf("phase=%s create=%d log:\n%s", phase, createCalls, got)
			}
			if strings.Count(got, "paste-buffer ") != 1 || strings.Count(got, "send-keys -t %1 Enter") > 1 {
				t.Fatalf("phase=%s duplicate input log:\n%s", phase, got)
			}
			if phase == "paste" && strings.Count(got, "delete-buffer ") != 1 {
				t.Fatalf("failed paste retained sensitive buffer:\n%s", got)
			}
			if _, err := os.Stat(filepath.Join(dir, config.WorkersFile)); !os.IsNotExist(err) {
				t.Fatalf("failed spawn persisted worker: %v", err)
			}
		})
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
  new-session) printf 'alpha\tworker\t@1\t%%1\n'; exit 0 ;;
  load-buffer) cat > ` + shellSingleQuote(pasted) + `; exit 0 ;;
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
	oldIDs, oldArgs, oldIdentity, oldWorkdir, oldCreate := spawnProcessIDs, spawnProcessArgs, spawnProcessIdentity, spawnProcessWorkdir, spawnCreateThread
	oldInspect, oldLimit, oldPoll, oldSettle := spawnInspectPaneByID, spawnReadinessLimit, spawnReadinessPoll, spawnReadinessSettle
	spawnProcessIDs = func() ([]int, error) { return []int{4242}, nil }
	spawnProcessArgs = func(int) ([]string, error) { return []string{"amp", "--no-tui", "--runner-id", "physical-1"}, nil }
	spawnProcessIdentity = func(int) (string, error) { return "start-4242", nil }
	spawnProcessWorkdir = func(int) (string, error) { return workdir, nil }
	spawnCreateThread = func(string, string) (string, error) {
		t.Fatal("preflight test unexpectedly reached Amp thread creation")
		return "", errors.New("unreachable")
	}
	spawnReadinessLimit, spawnReadinessPoll, spawnReadinessSettle = time.Millisecond, time.Microsecond, 0
	t.Cleanup(func() {
		spawnProcessIDs, spawnProcessArgs, spawnProcessIdentity, spawnProcessWorkdir, spawnCreateThread = oldIDs, oldArgs, oldIdentity, oldWorkdir, oldCreate
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
	args := []string{"--config-dir", dir, "spawn", "--runner-id", "physical-1", "--workdir", workdir, "--workspace", "alpha", "--window", "worker"}
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
