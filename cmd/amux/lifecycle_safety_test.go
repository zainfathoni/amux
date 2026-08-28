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

func TestLifecycleExecutorPreflightUsesExactAncestry(t *testing.T) {
	for _, test := range []struct {
		name       string
		currentPID int
		targetPID  int
		parents    map[int]int
		wantError  string
	}{
		{name: "direct self target", currentPID: 90, targetPID: 90, parents: map[int]int{90: 80, 80: 1}, wantError: "would stop or replace"},
		{name: "target transport is ancestor", currentPID: 90, targetPID: 80, parents: map[int]int{90: 80, 80: 1}, wantError: "would stop or replace"},
		{name: "target transport is descendant", currentPID: 90, targetPID: 100, parents: map[int]int{100: 90, 90: 80, 80: 1}, wantError: "would stop or replace"},
		{name: "direct PID 1 target", currentPID: 1, targetPID: 1, parents: map[int]int{1: 0}, wantError: "would stop or replace"},
		{name: "PID 1 target ancestor", currentPID: 2, targetPID: 1, parents: map[int]int{2: 1, 1: 0}, wantError: "would stop or replace"},
		{name: "PID 1 current ancestor", currentPID: 1, targetPID: 2, parents: map[int]int{2: 1, 1: 0}, wantError: "would stop or replace"},
		{name: "independent executor", currentPID: 90, targetPID: 70, parents: map[int]int{90: 80, 80: 1, 70: 60, 60: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			installLifecycleSafetyFixture(t, test.currentPID, test.targetPID, func(pid int) (tmux.ProcessMetadata, error) {
				if pid == 1 {
					return tmux.ProcessMetadata{PID: 1, ParentPID: 0, Identity: "start-1"}, nil
				}
				parent, ok := test.parents[pid]
				if !ok {
					return tmux.ProcessMetadata{}, fmt.Errorf("unexpected pid %d", pid)
				}
				return tmux.ProcessMetadata{PID: pid, ParentPID: parent, Identity: fmt.Sprintf("start-%d", pid)}, nil
			})

			err := preflightLifecycleExecutor("worker park", []tmux.WindowPane{{Session: "alpha", Window: "worker", WindowID: "@1"}})
			if test.wantError == "" && err != nil {
				t.Fatalf("independent preflight error = %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError) || !strings.Contains(err.Error(), "verified independent executor")) {
				t.Fatalf("preflight error = %v, want %q and guidance", err, test.wantError)
			}
		})
	}
}

func TestLifecycleProcessAncestryStopsAtVerifiedPID1BoundaryAndUsesExactLimit(t *testing.T) {
	oldInspect := lifecycleProcessLink
	t.Cleanup(func() { lifecycleProcessLink = oldInspect })
	inspectedPID1 := false
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		if pid == 1 {
			inspectedPID1 = true
			return tmux.ProcessMetadata{}, errors.New("operation not permitted")
		}
		parent := pid - 1
		return tmux.ProcessMetadata{PID: pid, ParentPID: parent, Identity: fmt.Sprintf("start-%d", pid)}, nil
	}
	ancestry, err := lifecycleProcessAncestry(lifecycleAncestryLimit)
	if err != nil || len(ancestry) != lifecycleAncestryLimit-1 || ancestry[2].ParentPID != 1 {
		t.Fatalf("exact-limit ancestry = %d records, PID 2 = %+v, error = %v", len(ancestry), ancestry[2], err)
	}
	if inspectedPID1 {
		t.Fatal("ancestry queried PID 1 after verifying a parent-to-1 boundary")
	}
	if _, err := lifecycleProcessAncestry(lifecycleAncestryLimit + 1); err == nil || !strings.Contains(err.Error(), "exceeded safety limit") {
		t.Fatalf("over-limit ancestry error = %v", err)
	}

	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		if pid == 1 {
			return tmux.ProcessMetadata{}, errors.New("PID 1 unavailable")
		}
		return tmux.ProcessMetadata{PID: pid, ParentPID: 1, Identity: fmt.Sprintf("start-%d", pid)}, nil
	}
	if _, err := lifecycleProcessAncestry(1); err == nil || !strings.Contains(err.Error(), "PID 1 unavailable") {
		t.Fatalf("explicit unavailable PID 1 error = %v", err)
	}

	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		if pid == 1 {
			return tmux.ProcessMetadata{PID: 1, ParentPID: 1, Identity: "start-1"}, nil
		}
		return tmux.ProcessMetadata{PID: pid, ParentPID: 1, Identity: fmt.Sprintf("start-%d", pid)}, nil
	}
	if _, err := lifecycleProcessAncestry(1); err == nil || !strings.Contains(err.Error(), "PID 1 returned a nonzero parent") {
		t.Fatalf("malformed PID 1 parent error = %v", err)
	}
}

func TestLifecycleProcessAncestryRejectsUnavailableOrIncompleteIntermediateNode(t *testing.T) {
	for _, test := range []struct {
		name    string
		process tmux.ProcessMetadata
		err     error
	}{
		{name: "unavailable", err: errors.New("operation not permitted")},
		{name: "incomplete", process: tmux.ProcessMetadata{PID: 80, ParentPID: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldInspect := lifecycleProcessLink
			t.Cleanup(func() { lifecycleProcessLink = oldInspect })
			lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
				if pid == 90 {
					return tmux.ProcessMetadata{PID: 90, ParentPID: 80, Identity: "start-90"}, nil
				}
				if pid == 80 {
					return test.process, test.err
				}
				return tmux.ProcessMetadata{}, fmt.Errorf("unexpected PID %d", pid)
			}
			if _, err := lifecycleProcessAncestry(90); err == nil {
				t.Fatal("incomplete intermediate ancestry was accepted")
			}
		})
	}
}

func TestLifecycleExecutorPreflightFailsClosedOnUnavailableOrReusedIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		inspect func(int) (tmux.ProcessMetadata, error)
	}{
		{name: "unavailable", inspect: func(pid int) (tmux.ProcessMetadata, error) {
			if pid == 70 {
				return tmux.ProcessMetadata{}, errors.New("identity unavailable")
			}
			return lifecycleFixtureProcess(pid), nil
		}},
		{name: "pid reused during preflight", inspect: func() func(int) (tmux.ProcessMetadata, error) {
			calls := map[int]int{}
			return func(pid int) (tmux.ProcessMetadata, error) {
				calls[pid]++
				process := lifecycleFixtureProcess(pid)
				if pid == 70 && calls[pid] > 1 {
					process.Identity = "replacement"
				}
				return process, nil
			}
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			installLifecycleSafetyFixture(t, 90, 70, test.inspect)
			err := preflightLifecycleExecutor("runner restart", []tmux.WindowPane{{Session: "alpha", Window: "runner", WindowID: "@1"}})
			if err == nil || !strings.Contains(err.Error(), "cannot prove runner restart is independent") || !strings.Contains(err.Error(), "verified independent executor") {
				t.Fatalf("ambiguous evidence error = %v", err)
			}
		})
	}
}

func TestLifecycleExecutorConflictRequiresStableIntersectingIdentity(t *testing.T) {
	calls := map[int]int{}
	installLifecycleSafetyFixture(t, 90, 80, func(pid int) (tmux.ProcessMetadata, error) {
		calls[pid]++
		parents := map[int]int{90: 80, 80: 1}
		identity := fmt.Sprintf("start-%d", pid)
		if pid == 80 && calls[pid] > 1 {
			identity = "reused-80"
		}
		return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: identity}, nil
	})

	err := preflightLifecycleExecutor("worker teardown", []tmux.WindowPane{{Session: "alpha", Window: "worker", WindowID: "@1"}})
	if err == nil || !strings.Contains(err.Error(), "cannot prove worker teardown is independent") || strings.Contains(err.Error(), "would stop or replace") {
		t.Fatalf("drifting conflict-path identity error = %v", err)
	}
}

func TestLifecycleExecutorConfirmRejectsPaneProcessIncarnationDrift(t *testing.T) {
	calls := map[int]int{}
	installLifecycleSafetyFixture(t, 90, 70, func(pid int) (tmux.ProcessMetadata, error) {
		calls[pid]++
		process := lifecycleFixtureProcess(pid)
		if pid == 70 && calls[pid] == 3 {
			process.Identity = "reused-at-confirm"
		}
		return process, nil
	})

	err := preflightLifecycleExecutor("runner restart", []tmux.WindowPane{{Session: "alpha", Window: "runner", WindowID: "@1"}})
	if err == nil || !strings.Contains(err.Error(), "target pane process incarnation changed") {
		t.Fatalf("confirm-step incarnation drift error = %v", err)
	}
}

func TestLifecycleExecutorRevalidatesCurrentAncestryAfterPaneConfirmation(t *testing.T) {
	calls := map[int]int{}
	installLifecycleSafetyFixture(t, 90, 70, func(pid int) (tmux.ProcessMetadata, error) {
		calls[pid]++
		process := lifecycleFixtureProcess(pid)
		if pid == 90 && calls[pid] > 1 {
			process.Identity = "reused-current"
		}
		return process, nil
	})

	err := preflightLifecycleExecutor("runner restart", []tmux.WindowPane{{Session: "alpha", Window: "runner", WindowID: "@1"}})
	if err == nil || !strings.Contains(err.Error(), "process 90 ancestry identity changed") {
		t.Fatalf("late current ancestry drift error = %v", err)
	}
}

func TestLifecycleExecutorRejectsPID1BoundaryParentDrift(t *testing.T) {
	calls := map[int]int{}
	installLifecycleSafetyFixture(t, 90, 70, func(pid int) (tmux.ProcessMetadata, error) {
		calls[pid]++
		process := lifecycleFixtureProcess(pid)
		if pid == 60 && calls[pid] > 1 {
			process.ParentPID = 55
		}
		return process, nil
	})

	err := preflightLifecycleExecutor("runner restart", []tmux.WindowPane{{Session: "alpha", Window: "runner", WindowID: "@1"}})
	if err == nil || !strings.Contains(err.Error(), "process 60 ancestry identity changed") {
		t.Fatalf("PID 1 boundary parent drift error = %v", err)
	}
}

func TestResolveLifecyclePaneProcessRequiresExactLiveIncarnation(t *testing.T) {
	for _, test := range []struct {
		name      string
		row       string
		wantError bool
	}{
		{name: "tmux 3.4 empty pane created", row: "alpha\tworker\t@1\t%1\t/tmp\tamp\tstart\t0\t42\t\n"},
		{name: "reused pane", row: "alpha\tworker\t@1\t%2\t/tmp\tamp\tstart\t0\t42\t123\n", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nprintf %s "+shellSingleQuote(test.row)+"\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			pane, err := resolveLifecyclePaneProcess(tmux.WindowPane{Session: "alpha", Window: "worker", WindowID: "@1", PaneID: "%1"})
			if test.wantError && err == nil {
				t.Fatalf("resolve accepted stale pane: %+v", pane)
			}
			if !test.wantError && (err != nil || pane.PID != 42) {
				t.Fatalf("resolve exact pane = %+v, %v", pane, err)
			}
		})
	}
}

func TestLifecycleExecutorAllowsIndependentTmux34PaneWithoutCreatedField(t *testing.T) {
	bin := t.TempDir()
	row := "alpha\tworker\t@1\t%1\t/tmp\tamp\tstart\t0\t70\t\n"
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nprintf %s "+shellSingleQuote(row)+"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldCurrent, oldInspect, oldPane := lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess
	lifecycleCurrentPID = func() int { return 90 }
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) { return lifecycleFixtureProcess(pid), nil }
	lifecyclePaneProcess = resolveLifecyclePaneProcess
	t.Cleanup(func() {
		lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess = oldCurrent, oldInspect, oldPane
	})

	if err := preflightLifecycleExecutor("worker park", []tmux.WindowPane{{Session: "alpha", Window: "worker", WindowID: "@1"}}); err != nil {
		t.Fatalf("independent tmux 3.4 target rejected: %v", err)
	}
}

func TestStopCapableCommandRoutesRequireTmuxAndLifecycleGuard(t *testing.T) {
	workerStops := map[string]bool{"shelve": true, "park": true, "restart": true, "remove": true, "teardown": true}
	runnerStops := map[string]bool{"park": true, "restart": true}
	workerFound, runnerFound := map[string]bool{}, map[string]bool{}
	for _, command := range workerCommand().Children {
		if lifecycleCommandStopsWorker(command.Name) {
			workerFound[command.Name] = true
			if !workerStops[command.Name] || !workerCommandNeedsTmux(command.Name) {
				t.Errorf("worker route %q can stop tmux without matching the guarded route list", command.Name)
			}
		}
	}
	for _, command := range runnerCommand().Children {
		if lifecycleCommandStopsRunner(command.Name) {
			runnerFound[command.Name] = true
			if !runnerStops[command.Name] || !runnerCommandNeedsTmux(command.Name) {
				t.Errorf("runner route %q can stop tmux without matching the guarded route list", command.Name)
			}
		}
	}
	if fmt.Sprint(workerFound) != fmt.Sprint(workerStops) || fmt.Sprint(runnerFound) != fmt.Sprint(runnerStops) {
		t.Fatalf("stop-capable command table drift: worker=%v want=%v runner=%v want=%v", workerFound, workerStops, runnerFound, runnerStops)
	}
	for _, command := range rootCommand.Children {
		if workerStops[command.Name] && !lifecycleCommandStopsWorker(command.Name) {
			t.Errorf("aggregate worker route %q does not require the lifecycle guard", command.Name)
		}
		if runnerStops[command.Name] && !lifecycleCommandStopsRunner(command.Name) {
			t.Errorf("aggregate runner route %q does not require the lifecycle guard", command.Name)
		}
	}
	if !maintenanceCommand().Children[2].Mutating || maintenanceCommand().Children[2].Name != "run" {
		t.Fatal("runner maintenance run route no longer identifies the guarded mutating command")
	}
}

func TestWorkerStopCommandsRejectSelfTargetBeforeAnyMutation(t *testing.T) {
	for _, test := range []struct {
		command string
		dryRun  bool
	}{
		{command: "shelve"},
		{command: "park"},
		{command: "park", dryRun: true},
		{command: "restart"},
		{command: "remove"},
		{command: "teardown"},
	} {
		t.Run(fmt.Sprintf("%s/dry-run=%t", test.command, test.dryRun), func(t *testing.T) {
			dir := t.TempDir()
			row := config.Row{Workspace: "alpha", Window: "worker", Workdir: t.TempDir(), Thread: "T-worker"}
			writeWorkerRegistry(t, dir, row.String()+"\n")
			files := map[string][]byte{
				config.ShelvesFile: []byte("# amux-schema: shelves/v1\n"),
				config.GroupsFile:  []byte("# amux-schema: groups/v1\nissue-278\tT-worker\tmember\n"),
				"callbacks.json":   []byte("{\"version\":1,\"slots\":[]}\n"),
				"reports.json":     []byte("{\"version\":1,\"reports\":[]}\n"),
			}
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workersBefore, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
			if err != nil {
				t.Fatal(err)
			}
			start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
			bin := t.TempDir()
			logPath := filepath.Join(bin, "calls.log")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> "+shellSingleQuote(logPath)+"\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"tmux $*\" >> "+shellSingleQuote(logPath)+"\nif [ \"$1\" = has-session ]; then exit 0; fi\nif [ \"$1\" = list-panes ]; then printf '%s\\n' "+shellSingleQuote("worker\t@1\t"+start)+"; exit 0; fi\nexit 2\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			installLifecycleSafetyFixture(t, 90, 80, func(pid int) (tmux.ProcessMetadata, error) {
				parents := map[int]int{90: 80, 80: 1}
				return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: fmt.Sprintf("start-%d", pid)}, nil
			})

			args := []string{"--json", "--config-dir", dir, "worker", test.command, "--thread", row.Thread}
			if test.dryRun {
				args = append([]string{"--dry-run"}, args...)
			}
			err = executeWorkerJSONError(t, args...)
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "would stop or replace") {
				t.Fatalf("self-target error = %v, exit=%d", err, result.ExitCode(err))
			}
			workersAfter, readErr := os.ReadFile(filepath.Join(dir, config.WorkersFile))
			if readErr != nil || !bytes.Equal(workersBefore, workersAfter) {
				t.Fatalf("worker registry changed: err=%v before=%q after=%q", readErr, workersBefore, workersAfter)
			}
			for name, before := range files {
				after, readErr := os.ReadFile(filepath.Join(dir, name))
				if readErr != nil || !bytes.Equal(before, after) {
					t.Fatalf("%s changed: err=%v before=%q after=%q", name, readErr, before, after)
				}
			}
			log, _ := os.ReadFile(logPath)
			if strings.Contains(string(log), "amp ") || strings.Contains(string(log), "kill-window") {
				t.Fatalf("self-target invoked mutation:\n%s", log)
			}
		})
	}
}

func TestRunnerStopCommandsRejectSelfTargetBeforeAnyMutation(t *testing.T) {
	for _, command := range []string{"park", "restart"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			window := config.RunnerWindow(workdir)
			writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
			registryBefore, err := os.ReadFile(filepath.Join(dir, config.RunnersFile))
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

			err = executeRunnerJSONError(t, "--json", "--dry-run", "--config-dir", dir, "runner", command, "--workdir", workdir)
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "would stop or replace") {
				t.Fatalf("self-target error = %v, exit=%d", err, result.ExitCode(err))
			}
			registryAfter, readErr := os.ReadFile(filepath.Join(dir, config.RunnersFile))
			if readErr != nil || !bytes.Equal(registryBefore, registryAfter) {
				t.Fatalf("runner registry changed: err=%v before=%q after=%q", readErr, registryBefore, registryAfter)
			}
			log, _ := os.ReadFile(logPath)
			if strings.Contains(string(log), "kill-window") || strings.Contains(string(log), "new-window") || strings.Contains(string(log), "new-session") {
				t.Fatalf("self-target invoked mutation:\n%s", log)
			}
		})
	}
}

func TestRunnerMaintenanceRejectsSelfTargetBeforeUpdateOrCheckpoint(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			fixture := newMaintenanceLifecycleFixture(t, "external", 1)
			fingerprint := lifecycleFingerprint(t, fixture.amp)
			seedLifecyclePrior(t, fixture, fingerprint)
			writeLifecycleState(t, fixture, map[string]string{"ws0": "exact"})
			beforeResult, err := os.ReadFile(fixture.dir.MaintenanceResultPath())
			if err != nil {
				t.Fatal(err)
			}
			beforeLog := lifecycleLog(t, fixture)
			installLifecycleSafetyFixture(t, 90, 80, func(pid int) (tmux.ProcessMetadata, error) {
				parents := map[int]int{90: 80, 80: 1}
				return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: fmt.Sprintf("start-%d", pid)}, nil
			})

			env := result.NewEnvelope("runner maintenance run", dryRun)
			_, err = (app{}).runMaintenance(invocation{Command: maintenanceCommand().Children[2], Path: []string{"runner", "maintenance", "run"}, Options: cliOptions{DryRun: dryRun}}, fixture.dir, &env)
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "would stop or replace") {
				t.Fatalf("maintenance self-target error = %v, exit=%d", err, result.ExitCode(err))
			}
			afterResult, readErr := os.ReadFile(fixture.dir.MaintenanceResultPath())
			if readErr != nil || !bytes.Equal(beforeResult, afterResult) {
				t.Fatalf("maintenance checkpoint changed: err=%v before=%q after=%q", readErr, beforeResult, afterResult)
			}
			if delta := strings.TrimPrefix(lifecycleLog(t, fixture), beforeLog); strings.Contains(delta, "amp update") || strings.Contains(delta, "kill-window") || strings.Contains(delta, "new-window") || strings.Contains(delta, "new-session") {
				t.Fatalf("maintenance self-target invoked mutation:\n%s", delta)
			}
		})
	}
}

func TestRunnerMaintenanceSelfTargetPrecedesExecutableValidationFailure(t *testing.T) {
	fixture := newMaintenanceLifecycleFixture(t, "self", 1)
	fingerprint := lifecycleFingerprint(t, fixture.amp)
	seedLifecyclePrior(t, fixture, fingerprint)
	writeLifecycleState(t, fixture, map[string]string{"ws0": "exact"})
	metadata, err := loadMaintenance(fixture.dir.MaintenancePath())
	if err != nil {
		t.Fatal(err)
	}
	metadata.AmpTarget = filepath.Join(t.TempDir(), "stale-target")
	if err := atomicJSON(fixture.dir.MaintenancePath(), metadata); err != nil {
		t.Fatal(err)
	}
	beforeResult, err := os.ReadFile(fixture.dir.MaintenanceResultPath())
	if err != nil {
		t.Fatal(err)
	}
	installLifecycleSafetyFixture(t, 90, 80, func(pid int) (tmux.ProcessMetadata, error) {
		parents := map[int]int{90: 80, 80: 1}
		return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: fmt.Sprintf("start-%d", pid)}, nil
	})

	_, err = runLifecycle(t, fixture)
	if err == nil || !strings.Contains(err.Error(), "would stop or replace") || strings.Contains(err.Error(), "persisted Amp target changed") {
		t.Fatalf("early self-target preflight error = %v", err)
	}
	afterResult, readErr := os.ReadFile(fixture.dir.MaintenanceResultPath())
	if readErr != nil || !bytes.Equal(beforeResult, afterResult) {
		t.Fatalf("early validation path changed result: err=%v before=%q after=%q", readErr, beforeResult, afterResult)
	}
}

func TestIndependentLifecycleCallersDoNotInspectPID1AfterVerifiedBoundary(t *testing.T) {
	dir := t.TempDir()
	workerWorkdir, runnerWorkdir := t.TempDir(), t.TempDir()
	worker := config.Row{Workspace: "workers", Window: "worker", Workdir: workerWorkdir, Thread: "T-worker"}
	writeWorkerRegistry(t, dir, worker.String()+"\n")
	writeRunnerRegistry(t, dir, "runners\t"+runnerWorkdir+"\n")
	runnerWindow := config.RunnerWindow(runnerWorkdir)
	workerStart := teardownExpectedStartCommand(teardownIdentity{Workspace: worker.Workspace, Session: worker.Workspace, Window: worker.Window, Thread: worker.Thread}, worker)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-panes)
    case "$*" in
      *runners*) printf 'runners\t`+runnerWindow+`\t@7\t%%7\t`+runnerWorkdir+`\tzsh\t%s\t0\t70\t123\n' `+shellSingleQuote(runnerStartCommand(runnerWorkdir))+` ;;
      *workers*) printf 'worker\t@8\t%s\n' `+shellSingleQuote(workerStart)+` ;;
      *) exit 2 ;;
    esac ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	pid1Queries := 0
	oldCurrent, oldInspect, oldPane := lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess
	lifecycleCurrentPID = func() int { return 90 }
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		if pid == 1 {
			pid1Queries++
			return tmux.ProcessMetadata{}, errors.New("operation not permitted")
		}
		parents := map[int]int{90: 80, 80: 1, 70: 60, 60: 1}
		parent, ok := parents[pid]
		if !ok {
			return tmux.ProcessMetadata{}, fmt.Errorf("unexpected PID %d", pid)
		}
		return tmux.ProcessMetadata{PID: pid, ParentPID: parent, Identity: fmt.Sprintf("start-%d", pid)}, nil
	}
	lifecyclePaneProcess = func(pane tmux.WindowPane) (tmux.WindowPane, error) {
		pane.PID = 70
		pane.StartTime = 123
		return pane, nil
	}
	t.Cleanup(func() {
		lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess = oldCurrent, oldInspect, oldPane
	})

	workerResult, err := executeWorkerJSONResult(t, "--json", "--dry-run", "--config-dir", dir, "worker", "teardown", "--thread", worker.Thread)
	if err != nil || len(workerResult.Planned) != 1 {
		t.Fatalf("independent worker teardown = %+v, %v", workerResult, err)
	}
	runnerResult := executeRunnerJSON(t, "--json", "--dry-run", "--config-dir", dir, "runner", "restart", "--workdir", runnerWorkdir)
	if len(runnerResult.Planned) != 1 {
		t.Fatalf("independent runner restart = %+v", runnerResult)
	}
	aggregateResult := executeAggregateJSON(t, "--json", "--dry-run", "--config-dir", dir, "restart", "--all")
	if keys := aggregateResourceKeys(aggregateResult.Planned); strings.Join(keys, ",") != "runner:"+runnerWorkdir+",worker:"+worker.Thread {
		t.Fatalf("independent aggregate restart --all = %+v", aggregateResult)
	}
	if pid1Queries != 0 {
		t.Fatalf("independent lifecycle callers queried PID 1 %d times", pid1Queries)
	}
}

func TestAggregateSelfTargetRejectsBeforeIndependentRunnerMutation(t *testing.T) {
	dir := t.TempDir()
	workerWorkdir, runnerWorkdir := t.TempDir(), t.TempDir()
	worker := config.Row{Workspace: "workers", Window: "worker", Workdir: workerWorkdir, Thread: "T-worker"}
	writeWorkerRegistry(t, dir, worker.String()+"\n")
	writeRunnerRegistry(t, dir, "runners\t"+runnerWorkdir+"\n")
	runnerWindow := config.RunnerWindow(runnerWorkdir)
	workerStart := teardownExpectedStartCommand(teardownIdentity{Workspace: worker.Workspace, Session: worker.Workspace, Window: worker.Window, Thread: worker.Thread}, worker)
	bin := t.TempDir()
	logPath := filepath.Join(bin, "tmux.log")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "$*" >> `+shellSingleQuote(logPath)+`
case "$1" in
  has-session) exit 0 ;;
  list-panes)
    case "$*" in
      *runners*) printf 'runners\t`+runnerWindow+`\t@7\t%%7\t`+runnerWorkdir+`\tzsh\t%s\t0\t7000\t123\n' `+shellSingleQuote(runnerStartCommand(runnerWorkdir))+` ;;
      *workers*) printf 'worker\t@8\t%s\n' `+shellSingleQuote(workerStart)+` ;;
      *) exit 2 ;;
    esac ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldCurrent, oldInspect, oldPane := lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess
	lifecycleCurrentPID = func() int { return 90 }
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		parents := map[int]int{90: 80, 80: 1, 70: 60, 60: 1}
		return tmux.ProcessMetadata{PID: pid, ParentPID: parents[pid], Identity: fmt.Sprintf("start-%d", pid)}, nil
	}
	lifecyclePaneProcess = func(pane tmux.WindowPane) (tmux.WindowPane, error) {
		if pane.Window == worker.Window {
			pane.PID = 80
		} else {
			pane.PID = 70
		}
		pane.StartTime = 123
		return pane, nil
	}
	t.Cleanup(func() {
		lifecycleCurrentPID, lifecycleProcessLink, lifecyclePaneProcess = oldCurrent, oldInspect, oldPane
	})

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "restart", "--all"})
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "would stop or replace") {
		t.Fatalf("aggregate self-target error = %v, exit=%d", err, result.ExitCode(err))
	}
	log, _ := os.ReadFile(logPath)
	if strings.Contains(string(log), "kill-window") || strings.Contains(string(log), "new-window") || strings.Contains(string(log), "new-session") {
		t.Fatalf("aggregate preflight invoked mutation:\n%s", log)
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
