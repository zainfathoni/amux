package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/result"
)

func installReportFixture(t *testing.T) (string, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	memberships := []config.GroupMembership{
		{Group: "issue-133", Thread: "T-coordinator", Role: config.GroupCoordinator},
		{Group: "issue-133", Thread: "T-worker", Role: config.GroupMember},
	}
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), memberships); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	oldNow := reportNow
	reportNow = func() time.Time { return now }
	oldNotify := reportNotifyCallback
	reportNotifyCallback = func(dir config.Directory, group, reportID string) (result.Outcome, error) {
		return result.Outcome{Resource: result.ResourceID{Kind: "callback", Group: group, Path: reportID}, Action: "notified", Callback: &result.CallbackDetails{Notified: true}}, nil
	}
	t.Cleanup(func() {
		reportNow = oldNow
		reportNotifyCallback = oldNotify
	})
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	return dir, &now
}

func reportSubmitArgs(dir, status string) []string {
	return []string{"--config-dir", dir, "report", "submit", "--report-id", "report-133", "--group", "issue-133", "--thread", "T-worker", "--status", status, "--issue", "#133", "--pr", "https://github.com/zainfathoni/amux/pull/200", "--summary", "implementation-tests-review-pr-ci-complete"}
}

func TestReportCLIReplayConflictProgressAuthorizationAndSeparation(t *testing.T) {
	dir, now := installReportFixture(t)
	var stdout bytes.Buffer
	a := app{stdout: &stdout}
	if err := a.execute(reportSubmitArgs(dir, "ready")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := a.execute(reportSubmitArgs(dir, "ready")); err != nil || !strings.Contains(stdout.String(), "duplicate") {
		t.Fatalf("duplicate replay = %v, %q", err, stdout.String())
	}

	conflict := reportSubmitArgs(dir, "ready")
	conflict[len(conflict)-1] = "different-summary"
	stdout.Reset()
	conflict = append([]string{"--json"}, conflict...)
	err := a.execute(conflict)
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("conflict = %v, exit %d", err, result.ExitCode(err))
	}
	var rejected result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&rejected); decodeErr != nil || len(rejected.Failed) != 1 || rejected.Failed[0].Action != "rejected" {
		t.Fatalf("rejected JSON = %+v, %v", rejected, decodeErr)
	}

	*now = now.Add(time.Minute)
	err = a.execute(reportSubmitArgs(dir, "merged"))
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("merged before auth = %v", err)
	}
	*now = now.Add(time.Minute)
	stdout.Reset()
	if err := a.execute([]string{"--json", "--config-dir", dir, "report", "acknowledge", "--report-id", "report-133"}); err != nil {
		t.Fatal(err)
	}
	var acknowledged result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&acknowledged); err != nil || len(acknowledged.Successful) != 1 || acknowledged.Successful[0].Action != "acknowledged" || acknowledged.Successful[0].Report.AcknowledgedAt == "" || acknowledged.Successful[0].Report.Pending {
		t.Fatalf("acknowledged JSON = %+v, %v", acknowledged, err)
	}
	record, err := findReport(filepath.Join(dir, config.ReportsFile), "report-133")
	if err != nil || record.AcknowledgedAt.IsZero() || !record.AuthorizedAt.IsZero() {
		t.Fatalf("ack state = %+v, %v", record, err)
	}
	*now = now.Add(time.Minute)
	stdout.Reset()
	if err := a.execute([]string{"--json", "--config-dir", dir, "report", "authorize-finish", "--report-id", "report-133", "--thread", "T-coordinator", "--reference", "coordinator-verification"}); err != nil {
		t.Fatal(err)
	}
	var authorized result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&authorized); err != nil || len(authorized.Successful) != 1 || authorized.Successful[0].Action != "authorized" || authorized.Successful[0].Report.AuthorizedAt == "" || authorized.Successful[0].Report.AuthorizingThread != "T-coordinator" {
		t.Fatalf("authorized JSON = %+v, %v", authorized, err)
	}
	record, _ = findReport(filepath.Join(dir, config.ReportsFile), "report-133")
	if record.AuthorizedAt.IsZero() || record.AcknowledgedAt.IsZero() {
		t.Fatalf("authorization changed acknowledgement: %+v", record)
	}
	*now = now.Add(time.Minute)
	if err := a.execute(reportSubmitArgs(dir, "merged")); err != nil {
		t.Fatal(err)
	}
	record, _ = findReport(filepath.Join(dir, config.ReportsFile), "report-133")
	if record.Status != config.ReportMerged || !record.AcknowledgedAt.IsZero() || record.AuthorizedAt.IsZero() {
		t.Fatalf("merged record = %+v", record)
	}
	*now = now.Add(time.Minute)
	if err := a.execute(reportSubmitArgs(dir, "ready")); err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("merged regression = %v", err)
	}
}

func TestReportPendingHistoryAndGroupShowAreLocalAndPersistAfterWorkerRemoval(t *testing.T) {
	dir, _ := installReportFixture(t)
	if err := (app{}).execute(reportSubmitArgs(dir, "blocked")); err != nil {
		t.Fatal(err)
	}
	var pending bytes.Buffer
	if err := (app{stdout: &pending}).execute([]string{"--config-dir", dir, "report", "pending", "--group", "issue-133"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pending.String(), "report-133\tissue-133\tT-worker\tblocked") {
		t.Fatalf("pending = %q", pending.String())
	}
	var show bytes.Buffer
	if err := (app{stdout: &show}).execute([]string{"--config-dir", dir, "group", "show", "--group", "issue-133"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show.String(), "REPORT\treport-133\tT-worker\tblocked") {
		t.Fatalf("group show = %q", show.String())
	}
	var history bytes.Buffer
	if err := (app{stdout: &history}).execute([]string{"--config-dir", dir, "report", "history", "--report-id", "report-133"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(history.String(), "report-133\tstatus\tblocked") {
		t.Fatalf("history = %q", history.String())
	}
	if _, err := config.Remove(filepath.Join(dir, config.WorkersFile), "any", "worker"); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadPendingReports(filepath.Join(dir, config.ReportsFile))
	if err != nil || len(loaded) != 1 {
		t.Fatalf("reports after teardown-equivalent config removal = %+v, %v", loaded, err)
	}
}

func TestReportDryRunMalformedStoreAndLockTimeoutWriteNothing(t *testing.T) {
	dir, _ := installReportFixture(t)
	path := filepath.Join(dir, config.ReportsFile)
	if err := (app{}).execute(append([]string{"--dry-run"}, reportSubmitArgs(dir, "ready")...)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote reports: %v", err)
	}
	if err := os.WriteFile(path, []byte("{malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := (app{}).execute([]string{"--config-dir", dir, "report", "pending"})
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("malformed store = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	lockPath, err := lock.MachinePath()
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), lockPath, lock.Owner{PID: 133, Command: "coordinator"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })
	oldWait := mutationLockWait
	mutationLockWait = 20 * time.Millisecond
	t.Cleanup(func() { mutationLockWait = oldWait })
	err = (app{}).execute(reportSubmitArgs(dir, "ready"))
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("lock timeout = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock timeout wrote reports: %v", err)
	}
}
