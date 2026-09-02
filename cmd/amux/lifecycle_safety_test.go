package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

func TestLifecycleExecutorPreflightUsesExactRunnerAncestry(t *testing.T) {
	for _, test := range []struct {
		name       string
		currentPID int
		targetPID  int
		parents    map[int]int
		wantError  bool
	}{
		{name: "self", currentPID: 90, targetPID: 90, parents: map[int]int{90: 80, 80: 1}, wantError: true},
		{name: "target ancestor", currentPID: 90, targetPID: 80, parents: map[int]int{90: 80, 80: 1}, wantError: true},
		{name: "target descendant", currentPID: 90, targetPID: 100, parents: map[int]int{100: 90, 90: 80, 80: 1}, wantError: true},
		{name: "independent", currentPID: 90, targetPID: 70, parents: map[int]int{90: 80, 80: 1, 70: 60, 60: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			installLifecycleSafetyFixture(t, test.currentPID, test.targetPID, func(pid int) (tmux.ProcessMetadata, error) {
				parent, ok := test.parents[pid]
				if !ok {
					return tmux.ProcessMetadata{}, fmt.Errorf("unexpected PID %d", pid)
				}
				return tmux.ProcessMetadata{PID: pid, ParentPID: parent, Identity: fmt.Sprintf("start-%d", pid)}, nil
			})
			err := preflightLifecycleExecutor("runner restart", []tmux.WindowPane{{Session: "alpha", Window: "runner", WindowID: "@1"}})
			if test.wantError && (err == nil || !strings.Contains(err.Error(), "would stop or replace")) {
				t.Fatalf("self-target error = %v", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("independent executor rejected: %v", err)
			}
		})
	}
}

func TestLifecycleProcessAncestryRejectsUnavailableIncompleteAndOverLimit(t *testing.T) {
	old := lifecycleProcessLink
	t.Cleanup(func() { lifecycleProcessLink = old })
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		if pid == 80 {
			return tmux.ProcessMetadata{}, errors.New("unavailable")
		}
		return tmux.ProcessMetadata{PID: pid, ParentPID: 80, Identity: "current"}, nil
	}
	if _, err := lifecycleProcessAncestry(90); err == nil {
		t.Fatal("unavailable intermediate ancestry was accepted")
	}
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		return tmux.ProcessMetadata{PID: pid, ParentPID: pid - 1, Identity: fmt.Sprintf("start-%d", pid)}, nil
	}
	if _, err := lifecycleProcessAncestry(lifecycleAncestryLimit + 1); err == nil || !strings.Contains(err.Error(), "exceeded safety limit") {
		t.Fatalf("over-limit ancestry error = %v", err)
	}
}

func TestLifecycleExecutorRejectsReusedAndDriftingIdentity(t *testing.T) {
	for _, test := range []struct {
		name      string
		drift     int
		afterCall int
		change    func(*tmux.ProcessMetadata)
		want      string
	}{
		{name: "target incarnation", drift: 70, afterCall: 2, change: func(process *tmux.ProcessMetadata) { process.Identity = "reused" }, want: "incarnation changed"},
		{name: "current ancestry", drift: 90, afterCall: 1, change: func(process *tmux.ProcessMetadata) { process.Identity = "reused" }, want: "ancestry identity changed"},
		{name: "parent ancestry", drift: 60, afterCall: 1, change: func(process *tmux.ProcessMetadata) { process.ParentPID = 55 }, want: "ancestry identity changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := map[int]int{}
			installLifecycleSafetyFixture(t, 90, 70, func(pid int) (tmux.ProcessMetadata, error) {
				calls[pid]++
				process := lifecycleFixtureProcess(pid)
				if pid == test.drift && calls[pid] > test.afterCall {
					test.change(&process)
				}
				return process, nil
			})
			err := preflightLifecycleExecutor("runner restart", []tmux.WindowPane{{Session: "alpha", Window: "runner", WindowID: "@1"}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("drift error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLifecycleExecutorRejectsPaneIdentityDrift(t *testing.T) {
	oldCurrent, oldInspect, oldPane := lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess
	lifecycleCurrentPID = func() int { return 90 }
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) { return lifecycleFixtureProcess(pid), nil }
	calls := 0
	lifecyclePaneProcess = func(pane tmux.WindowPane) (tmux.WindowPane, error) {
		calls++
		pane.PID = 70
		pane.PaneID = "%1"
		if calls > 1 {
			pane.PaneID = "%2"
		}
		return pane, nil
	}
	t.Cleanup(func() {
		lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess = oldCurrent, oldInspect, oldPane
	})
	err := preflightLifecycleExecutor("runner park", []tmux.WindowPane{{Session: "alpha", Window: "runner", WindowID: "@1"}})
	if err == nil || !strings.Contains(err.Error(), "pane process identity changed") {
		t.Fatalf("pane drift error = %v", err)
	}
}

func TestResolveLifecyclePaneProcessRequiresExactLiveIncarnation(t *testing.T) {
	for _, test := range []struct {
		name      string
		row       string
		wantError bool
	}{
		{name: "exact", row: "alpha\trunner\t@1\t%1\t/tmp\tamp\tstart\t0\t42\t123\n"},
		{name: "reused pane", row: "alpha\trunner\t@1\t%2\t/tmp\tamp\tstart\t0\t42\t123\n", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nprintf %s "+shellSingleQuote(test.row)+"\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			pane, err := resolveLifecyclePaneProcess(tmux.WindowPane{Session: "alpha", Window: "runner", WindowID: "@1", PaneID: "%1"})
			if test.wantError && err == nil {
				t.Fatalf("stale pane accepted: %+v", pane)
			}
			if !test.wantError && (err != nil || pane.PID != 42) {
				t.Fatalf("exact pane = %+v, %v", pane, err)
			}
		})
	}
}

func TestStopCapableRunnerRoutesRequireLifecycleGuard(t *testing.T) {
	want := map[string]bool{"park": true, "restart": true}
	got := map[string]bool{}
	for _, command := range runnerCommand().Children {
		if lifecycleCommandStopsRunner(command.Name) {
			got[command.Name] = true
			if !want[command.Name] || !runnerCommandNeedsTmux(command.Name) {
				t.Errorf("runner route %q bypasses guarded route table", command.Name)
			}
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("stop-capable route drift: got=%v want=%v", got, want)
	}
	for _, command := range rootCommand.Children {
		if want[command.Name] && !lifecycleCommandStopsRunner(command.Name) {
			t.Errorf("top-level runner alias %q bypasses lifecycle guard", command.Name)
		}
	}
	maintenance := maintenanceCommand().Children[2]
	if maintenance.Name != "run" || !maintenance.Mutating {
		t.Fatal("runner maintenance run is no longer identified as guarded mutation")
	}
}

func TestRunnerStopCommandsRejectSelfTargetBeforeMutation(t *testing.T) {
	for _, command := range []string{"park", "restart"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry-run=%t", command, dryRun), func(t *testing.T) {
				dir, workdir := t.TempDir(), t.TempDir()
				window := config.RunnerWindow(workdir)
				writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
				before, err := os.ReadFile(filepath.Join(dir, config.RunnersFile))
				if err != nil {
					t.Fatal(err)
				}
				bin := t.TempDir()
				logPath := filepath.Join(bin, "tmux.log")
				writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "$*" >> `+shellSingleQuote(logPath)+`
case "$1" in
  has-session) exit 0 ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(runnerStartCommand(workdir))+` ;;
  *) exit 2 ;;
esac
`)
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
				installLifecycleSafetyFixture(t, 90, 80, func(pid int) (tmux.ProcessMetadata, error) {
					parents := map[int]int{90: 80, 80: 1}
					return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: fmt.Sprintf("start-%d", pid)}, nil
				})
				args := []string{"--json", "--config-dir", dir, "runner", command, "--workdir", workdir}
				if dryRun {
					args = append([]string{"--dry-run"}, args...)
				}
				err = executeRunnerJSONError(t, args...)
				if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "would stop or replace") {
					t.Fatalf("self-target error = %v", err)
				}
				after, readErr := os.ReadFile(filepath.Join(dir, config.RunnersFile))
				if readErr != nil || !bytes.Equal(before, after) {
					t.Fatalf("runner registry changed: %v", readErr)
				}
				log, _ := os.ReadFile(logPath)
				if strings.Contains(string(log), "kill-window") || strings.Contains(string(log), "new-window") || strings.Contains(string(log), "new-session") {
					t.Fatalf("self-target invoked mutation:\n%s", log)
				}
			})
		}
	}
}

func TestStopRunnerRejectsPostPreflightProcessIncarnationDrift(t *testing.T) {
	oldInspect, oldKill, oldProcess := stopRunnerInspect, stopRunnerKill, lifecycleProcessLink
	t.Cleanup(func() { stopRunnerInspect, stopRunnerKill, lifecycleProcessLink = oldInspect, oldKill, oldProcess })
	row := config.RunnerRow{Workspace: "alpha", Window: "runner", Workdir: t.TempDir()}
	before := runnerInspection{state: runnerPaneExact, pane: tmux.WindowPane{WindowID: "@1", PaneID: "%1", PID: 70}}
	stopRunnerInspect = func(config.RunnerRow) (runnerInspection, error) { return before, nil }
	lifecycleProcessLink = func(int) (tmux.ProcessMetadata, error) {
		return tmux.ProcessMetadata{PID: 70, ParentPID: 60, Identity: "replacement"}, nil
	}
	killed := false
	stopRunnerKill = func(string) error { killed = true; return nil }
	expected := tmux.ProcessMetadata{PID: 70, ParentPID: 60, Identity: "original"}
	err := stopRunner(row, before, expected)
	if err == nil || !strings.Contains(err.Error(), "process incarnation changed") {
		t.Fatalf("post-preflight drift error = %v", err)
	}
	if killed {
		t.Fatal("post-preflight process drift killed the replacement window")
	}
}

func TestRunnerMaintenanceRejectsSelfTargetBeforeMutation(t *testing.T) {
	fixture := newMaintenanceLifecycleFixture(t, "external", 1)
	fingerprint := lifecycleFingerprint(t, fixture.amp)
	seedLifecyclePrior(t, fixture, fingerprint)
	writeLifecycleState(t, fixture, map[string]string{"ws0": "exact"})
	before, err := os.ReadFile(fixture.dir.MaintenanceResultPath())
	if err != nil {
		t.Fatal(err)
	}
	beforeLog := lifecycleLog(t, fixture)
	installLifecycleSafetyFixture(t, 90, 80, func(pid int) (tmux.ProcessMetadata, error) {
		parents := map[int]int{90: 80, 80: 1}
		return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: fmt.Sprintf("start-%d", pid)}, nil
	})
	envelope := result.NewEnvelope("runner maintenance run", false)
	_, err = (app{}).runMaintenance(invocation{Command: maintenanceCommand().Children[2], Path: []string{"runner", "maintenance", "run"}}, fixture.dir, &envelope)
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "would stop or replace") {
		t.Fatalf("maintenance self-target error = %v", err)
	}
	after, readErr := os.ReadFile(fixture.dir.MaintenanceResultPath())
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("maintenance result changed: %v", readErr)
	}
	if delta := strings.TrimPrefix(lifecycleLog(t, fixture), beforeLog); strings.Contains(delta, "amp update") || strings.Contains(delta, "kill-window") || strings.Contains(delta, "new-window") || strings.Contains(delta, "new-session") {
		t.Fatalf("maintenance self-target invoked mutation:\n%s", delta)
	}
}

func installLifecycleSafetyFixture(t *testing.T, currentPID, targetPID int, inspect func(int) (tmux.ProcessMetadata, error)) {
	t.Helper()
	oldCurrent, oldInspect, oldPane := lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess
	lifecycleCurrentPID = func() int { return currentPID }
	lifecycleProcessLink = inspect
	lifecyclePaneProcess = func(pane tmux.WindowPane) (tmux.WindowPane, error) {
		pane.PID = targetPID
		pane.StartTime = 123
		return pane, nil
	}
	t.Cleanup(func() {
		lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess = oldCurrent, oldInspect, oldPane
	})
}

func lifecycleFixtureProcess(pid int) tmux.ProcessMetadata {
	parents := map[int]int{90: 80, 80: 1, 70: 60, 60: 1}
	return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: fmt.Sprintf("start-%d", pid)}
}
