package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

func installCallbackFixture(t *testing.T) (config.Directory, tmux.WindowPane, tmux.ProcessMetadata) {
	t.Helper()
	dir := config.Directory{Path: t.TempDir()}
	workdir := t.TempDir()
	if err := config.WriteGroups(dir.GroupsPath(), []config.GroupMembership{
		{Group: "issue-134", Thread: "T-coordinator", Role: config.GroupCoordinator},
		{Group: "issue-134", Thread: "T-worker", Role: config.GroupMember},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Store(dir.WorkersPath(), config.Row{Workspace: "amux", Window: "coordinator", Workdir: workdir, Thread: "T-coordinator"}); err != nil {
		t.Fatal(err)
	}
	pane := tmux.WindowPane{Session: "amux", Window: "coordinator", WindowID: "@105", PaneID: "%16", Path: workdir, Command: "amp", StartCommand: tmux.ContinueCommandWithEnv(workdir, "T-coordinator", map[string]string{"AMUX_WORKSPACE": "amux", "AMUX_SESSION": "amux", "AMUX_WINDOW": "coordinator", "AMUX_THREAD_ID": "T-coordinator", "AMUX_WORKDIR": workdir}), PID: 4242, StartTime: 1773550000}
	process := tmux.ProcessMetadata{PID: 4242, Name: "amp", Command: "amp threads continue T-coordinator", Identity: "Wed Jul 15 06:20:00 2026"}
	oldPane, oldProcess, oldSend, oldNow := callbackPaneByID, callbackInspectProcess, callbackSend, callbackNow
	callbackPaneByID = func(string) (tmux.WindowPane, error) { return pane, nil }
	callbackInspectProcess = func(int) (tmux.ProcessMetadata, error) { return process, nil }
	callbackSend = func(string, string) error { return nil }
	callbackNow = func() time.Time { return time.Date(2026, 7, 15, 6, 30, 0, 0, time.UTC) }
	t.Cleanup(func() {
		callbackPaneByID, callbackInspectProcess, callbackSend, callbackNow = oldPane, oldProcess, oldSend, oldNow
	})
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	return dir, pane, process
}

func registerCallbackArgs(dir config.Directory) []string {
	return []string{"--config-dir", dir.Path, "callback", "register", "--group", "issue-134", "--thread", "T-coordinator", "--pane", "%16"}
}

func TestCallbackRegistrationClearGenerationAndRuntimeStorage(t *testing.T) {
	dir, _, _ := installCallbackFixture(t)
	a := app{}
	if err := a.execute(registerCallbackArgs(dir)); err != nil {
		t.Fatal(err)
	}
	if err := a.execute(registerCallbackArgs(dir)); err != nil {
		t.Fatal(err)
	}
	path, _ := callbackRuntimePath()
	store, err := loadCallbackStore(path)
	if err != nil || len(store.Slots) != 1 || store.Slots[0].Generation != 2 || store.Slots[0].Lease == nil {
		t.Fatalf("registered store = %+v, %v", store, err)
	}
	if strings.HasPrefix(path, dir.Path) || filepath.Base(path) != "callback-leases.json" {
		t.Fatalf("lease stored in portable config: %s", path)
	}
	groupsBefore, _ := os.ReadFile(dir.GroupsPath())
	if err := a.execute([]string{"--config-dir", dir.Path, "callback", "clear", "--group", "issue-134"}); err != nil {
		t.Fatal(err)
	}
	store, _ = loadCallbackStore(path)
	if store.Slots[0].Generation != 3 || store.Slots[0].Lease != nil {
		t.Fatalf("cleared store = %+v", store)
	}
	groupsAfter, _ := os.ReadFile(dir.GroupsPath())
	if !bytes.Equal(groupsBefore, groupsAfter) {
		t.Fatal("callback mutation changed portable groups registry")
	}
}

func TestCallbackDryRunMutatesNeitherRuntimeNorTmux(t *testing.T) {
	dir, _, _ := installCallbackFixture(t)
	sends := 0
	callbackSend = func(string, string) error { sends++; return nil }
	if err := (app{}).execute(append([]string{"--dry-run"}, registerCallbackArgs(dir)...)); err != nil {
		t.Fatal(err)
	}
	path, _ := callbackRuntimePath()
	if _, err := os.Stat(path); !os.IsNotExist(err) || sends != 0 {
		t.Fatalf("dry-run runtime=%v sends=%d", err, sends)
	}
	if err := (app{}).execute([]string{"--dry-run", "--config-dir", dir.Path, "callback", "clear", "--group", "issue-134"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run clear wrote runtime: %v", err)
	}
}

func TestCallbackMutationUsesMachineLock(t *testing.T) {
	dir, _, _ := installCallbackFixture(t)
	path, err := lock.MachinePath()
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), path, lock.Owner{PID: 134, Command: "coordinator"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })
	oldWait := mutationLockWait
	mutationLockWait = 20 * time.Millisecond
	t.Cleanup(func() { mutationLockWait = oldWait })
	err = (app{}).execute(registerCallbackArgs(dir))
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("lock contention = %v", err)
	}
	runtimePath, _ := callbackRuntimePath()
	if _, err := os.Stat(runtimePath); !os.IsNotExist(err) {
		t.Fatalf("lock contention wrote lease: %v", err)
	}
}

func TestCallbackCompleteMetadataVerificationFailsClosed(t *testing.T) {
	dir, pane, process := installCallbackFixture(t)
	if err := (app{}).execute(registerCallbackArgs(dir)); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*tmux.WindowPane, *tmux.ProcessMetadata)
	}{
		{"missing", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) { p.PaneID = "" }},
		{"dead", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) { p.Dead = true }},
		{"renamed", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) { p.Window = "renamed" }},
		{"wrong-window-id", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) { p.WindowID = "@reused" }},
		{"wrong-thread-start-command", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) {
			p.StartCommand = tmux.ContinueCommand(p.Path, "T-other")
		}},
		{"wrong-workdir", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) { p.Path = t.TempDir() }},
		{"wrong-current-process", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) { p.Command = "bash" }},
		{"wrong-process-name", func(_ *tmux.WindowPane, m *tmux.ProcessMetadata) { m.Name = "bash" }},
		{"wrong-process-command", func(_ *tmux.WindowPane, m *tmux.ProcessMetadata) { m.Command = "amp --no-tui" }},
		{"missing-process-identity", func(_ *tmux.WindowPane, m *tmux.ProcessMetadata) { m.Identity = "" }},
		{"pid-reuse", func(p *tmux.WindowPane, m *tmux.ProcessMetadata) { p.PID = 5252; m.PID = 5252 }},
		{"process-identity-reuse", func(_ *tmux.WindowPane, m *tmux.ProcessMetadata) { m.Identity = "Wed Jul 15 06:31:00 2026" }},
		{"pane-reuse", func(p *tmux.WindowPane, _ *tmux.ProcessMetadata) { p.StartTime++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			freshPane, freshProcess := pane, process
			test.edit(&freshPane, &freshProcess)
			callbackPaneByID = func(string) (tmux.WindowPane, error) { return freshPane, nil }
			callbackInspectProcess = func(int) (tmux.ProcessMetadata, error) { return freshProcess, nil }
			sends := 0
			callbackSend = func(string, string) error { sends++; return nil }
			if _, err := notifyReportCallback(dir, "issue-134", "report-134"); err == nil || sends != 0 {
				t.Fatalf("notify err=%v sends=%d", err, sends)
			}
		})
	}
}

func TestCallbackCoordinatorIntentChangeAndConfigDirectoryCollision(t *testing.T) {
	dir, pane, process := installCallbackFixture(t)
	if err := (app{}).execute(registerCallbackArgs(dir)); err != nil {
		t.Fatal(err)
	}
	other := config.Directory{Path: t.TempDir()}
	if err := config.WriteGroups(other.GroupsPath(), []config.GroupMembership{{Group: "issue-134", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Store(other.WorkersPath(), config.Row{Workspace: "amux", Window: "coordinator", Workdir: pane.Path, Thread: "T-coordinator"}); err != nil {
		t.Fatal(err)
	}
	callbackPaneByID = func(string) (tmux.WindowPane, error) { return pane, nil }
	callbackInspectProcess = func(int) (tmux.ProcessMetadata, error) { return process, nil }
	if _, err := notifyReportCallback(other, "issue-134", "other-report"); err == nil || !strings.Contains(err.Error(), "no live callback lease") {
		t.Fatalf("config collision notify = %v", err)
	}
	if err := config.WriteGroups(dir.GroupsPath(), []config.GroupMembership{{Group: "issue-134", Thread: "T-other", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	if _, err := notifyReportCallback(dir, "issue-134", "report-134"); err == nil || !strings.Contains(err.Error(), "coordinator intent changed") {
		t.Fatalf("changed coordinator notify = %v", err)
	}
}

func TestCoordinatorRestartRequiresExplicitReregistration(t *testing.T) {
	dir, pane, process := installCallbackFixture(t)
	if err := (app{}).execute(registerCallbackArgs(dir)); err != nil {
		t.Fatal(err)
	}
	pane.PID = 5252
	process.PID = 5252
	process.Identity = "Wed Jul 15 06:35:00 2026"
	callbackPaneByID = func(string) (tmux.WindowPane, error) { return pane, nil }
	callbackInspectProcess = func(int) (tmux.ProcessMetadata, error) { return process, nil }
	if _, err := notifyReportCallback(dir, "issue-134", "before-reregister"); err == nil {
		t.Fatal("restarted coordinator matched old lease")
	}
	if err := (app{}).execute(registerCallbackArgs(dir)); err != nil {
		t.Fatal(err)
	}
	path, _ := callbackRuntimePath()
	store, _ := loadCallbackStore(path)
	if store.Slots[0].Generation != 2 || store.Slots[0].Lease.PID != 5252 {
		t.Fatalf("reregistered slot = %+v", store.Slots[0])
	}
	if _, err := notifyReportCallback(dir, "issue-134", "after-reregister"); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersistsBeforeNotifyAndRetryDoesNotDuplicateOrAcknowledge(t *testing.T) {
	dirPath, _ := installReportFixture(t)
	attempts := 0
	reportNotifyCallback = func(dir config.Directory, group, reportID string) (result.Outcome, error) {
		attempts++
		record, err := findReport(dir.ReportsPath(), reportID)
		if err != nil || record.Status != config.ReportReady {
			t.Fatalf("notification preceded persistence: %+v, %v", record, err)
		}
		out := result.Outcome{Resource: result.ResourceID{Kind: "callback", Group: group, Path: reportID}, Action: "notified", Callback: &result.CallbackDetails{Notified: true}}
		if attempts == 1 {
			return out, errors.New("coordinator composer busy")
		}
		return out, nil
	}
	args := reportSubmitArgs(dirPath, "ready")
	if err := (app{}).execute(args); err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("first submit = %v", err)
	}
	if err := (app{}).execute(args); err != nil {
		t.Fatal(err)
	}
	record, err := findReport(filepath.Join(dirPath, config.ReportsFile), "report-133")
	if err != nil || len(record.Events) != 1 || !record.AcknowledgedAt.IsZero() || !record.AuthorizedAt.IsZero() || attempts != 2 {
		t.Fatalf("retry state = %+v attempts=%d err=%v", record, attempts, err)
	}
}

func TestCallbackFailureHasSeparateJSONOutcomeAndRuntimeExit(t *testing.T) {
	dirPath, _ := installReportFixture(t)
	reportNotifyCallback = func(dir config.Directory, group, reportID string) (result.Outcome, error) {
		return result.Outcome{Resource: result.ResourceID{Kind: "callback", Group: group, Path: reportID}, Action: "notify"}, errors.New("stale lease")
	}
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(append([]string{"--json"}, reportSubmitArgs(dirPath, "ready")...))
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("submit = %v", err)
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil || len(env.Successful) != 1 || env.Successful[0].Resource.Kind != "report" || len(env.Failed) != 1 || env.Failed[0].Resource.Kind != "callback" || env.Failed[0].Error.Kind != result.ErrorRuntime {
		t.Fatalf("callback JSON = %+v decode=%v", env, decodeErr)
	}
}

func TestCallbackFailureHasSeparateHumanOutcomeAndDryRunDoesNotNotify(t *testing.T) {
	dirPath, _ := installReportFixture(t)
	attempts := 0
	reportNotifyCallback = func(dir config.Directory, group, reportID string) (result.Outcome, error) {
		attempts++
		return result.Outcome{Resource: result.ResourceID{Kind: "callback", Group: group, Path: reportID}}, errors.New("missing lease")
	}
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(reportSubmitArgs(dirPath, "ready"))
	if err == nil || !strings.Contains(stdout.String(), "report-133\tready\trecorded\tT-worker") || !strings.Contains(stdout.String(), "CALLBACK\tissue-133\treport-133\tfailed") {
		t.Fatalf("human outcome = %q, %v", stdout.String(), err)
	}
	stdout.Reset()
	if err := (app{stdout: &stdout}).execute(append([]string{"--dry-run"}, reportSubmitArgs(dirPath, "ready")...)); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || !strings.Contains(stdout.String(), "CALLBACK\tissue-133\treport-133\tplanned") {
		t.Fatalf("dry-run attempts=%d output=%q", attempts, stdout.String())
	}
}

func TestWakeupsAreTokensOnlyAndDoNotMutateDurableReports(t *testing.T) {
	dir, pane, process := installCallbackFixture(t)
	if err := (app{}).execute(registerCallbackArgs(dir)); err != nil {
		t.Fatal(err)
	}
	callbackPaneByID = func(string) (tmux.WindowPane, error) { return pane, nil }
	callbackInspectProcess = func(int) (tmux.ProcessMetadata, error) { return process, nil }
	var tokens []string
	callbackSend = func(id, token string) error { tokens = append(tokens, token); return nil }
	for _, id := range []string{"late", "duplicate", "duplicate", "earlier"} {
		if _, err := notifyReportCallback(dir, "issue-134", id); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(tokens, "|"); got != "AMUX_REPORT group=issue-134 report=late|AMUX_REPORT group=issue-134 report=duplicate|AMUX_REPORT group=issue-134 report=duplicate|AMUX_REPORT group=issue-134 report=earlier" {
		t.Fatalf("tokens = %q", got)
	}
	if _, err := os.Stat(dir.ReportsPath()); !os.IsNotExist(err) {
		t.Fatalf("wakeups mutated report state: %v", err)
	}
}
