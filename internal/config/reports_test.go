package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testReport(at time.Time) ReportRecord {
	return ReportRecord{ReportID: "r-1", RequestHash: ReportRequestHash("group", "T-one", "133", "ref"), GroupID: "group", MemberThread: "T-one", Issue: "133", Reference: "ref", PRURL: "https://example.test/pr/1", Summary: "done", Status: ReportReady, CreatedAt: at, UpdatedAt: at}
}

func TestReportReplayProgressAuthorizationAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), ReportsFile)
	at := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	r := testReport(at)
	if got, err := SubmitReport(path, r); err != nil || got != ReportRecorded {
		t.Fatalf("submit = %q, %v", got, err)
	}
	before, _ := os.ReadFile(path)
	if got, err := SubmitReport(path, r); err != nil || got != ReportDuplicate {
		t.Fatalf("replay = %q, %v", got, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("duplicate rewrote store")
	}
	if _, err := AcknowledgeReport(path, r.ReportID, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthorizeReportFinish(path, r.ReportID, at.Add(2*time.Minute), "T-coordinator", "approval"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadReports(path)
	if loaded[0].AcknowledgedAt.IsZero() || loaded[0].AuthorizedAt.IsZero() {
		t.Fatal("acknowledgement and authorization were not independently recorded")
	}
	r.Status, r.UpdatedAt = ReportMerged, at.Add(3*time.Minute)
	if _, err := SubmitReport(path, r); err != nil {
		t.Fatal(err)
	}
	loaded, _ = LoadReports(path)
	if !loaded[0].AcknowledgedAt.IsZero() || loaded[0].AuthorizedAt.IsZero() || len(loaded[0].Events) != 4 {
		t.Fatalf("merged state = %+v", loaded[0])
	}
	r.Status, r.UpdatedAt = ReportReady, at.Add(4*time.Minute)
	if _, err := SubmitReport(path, r); err == nil {
		t.Fatal("merged report regressed")
	}
}

func TestReportsFailClosedAndMissingReadDoesNotCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	path := filepath.Join(dir, ReportsFile)
	if got, err := LoadReports(path); err != nil || len(got) != 0 {
		t.Fatalf("missing load = %v, %v", got, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("read created directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReports(path); err == nil {
		t.Fatal("unsupported store accepted")
	}
}

func TestReportsFailClosedOnImpossiblePersistedTransition(t *testing.T) {
	path := filepath.Join(t.TempDir(), ReportsFile)
	at := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	r := testReport(at)
	r.SchemaVersion = ReportsSchemaVersion
	r.Status = ReportMerged
	r.Events = []ReportEvent{{Kind: "status", Status: ReportMerged, At: at}}
	data, err := json.Marshal(reportsFile{SchemaVersion: ReportsSchemaVersion, Reports: []ReportRecord{r}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReports(path); err == nil || !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("merged-without-auth store error = %v", err)
	}
}

func TestReportsFailClosedOnAuthorizationWithoutCoordinatorThread(t *testing.T) {
	path := filepath.Join(t.TempDir(), ReportsFile)
	at := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	r := testReport(at)
	r.SchemaVersion = ReportsSchemaVersion
	r.UpdatedAt = at.Add(time.Minute)
	r.AuthorizedAt = r.UpdatedAt
	r.AuthorizationReference = "approval"
	r.Events = []ReportEvent{
		{Kind: "status", Status: ReportReady, At: at},
		{Kind: "authorized", At: r.UpdatedAt, Reference: "approval"},
	}
	data, err := json.Marshal(reportsFile{SchemaVersion: ReportsSchemaVersion, Reports: []ReportRecord{r}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReports(path); err == nil || !strings.Contains(err.Error(), "thread is required") {
		t.Fatalf("authorization without thread error = %v", err)
	}
}

func TestBlockedReportCanProgressPayloadToReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), ReportsFile)
	at := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	r := testReport(at)
	r.Status, r.PRURL, r.Summary = ReportBlocked, "", "waiting for upstream"
	if _, err := SubmitReport(path, r); err != nil {
		t.Fatal(err)
	}
	r.Status, r.PRURL, r.Summary = ReportReady, "https://example.test/pr/1", "implementation and CI complete"
	r.CreatedAt, r.UpdatedAt = at.Add(time.Minute), at.Add(time.Minute)
	if _, err := SubmitReport(path, r); err != nil {
		t.Fatalf("blocked-to-ready progress: %v", err)
	}
	loaded, err := LoadReports(path)
	if err != nil || loaded[0].Status != ReportReady || loaded[0].Summary != r.Summary {
		t.Fatalf("progressed report = %+v, %v", loaded, err)
	}
}

func TestRejectedReportMutationPreservesAtomicStoreAndRetryIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ReportsFile)
	at := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	r := testReport(at)
	if _, err := SubmitReport(path, r); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	conflict := r
	conflict.RequestHash = "different"
	conflict.UpdatedAt = at.Add(time.Minute)
	if _, err := SubmitReport(path, conflict); err == nil {
		t.Fatal("conflicting reuse succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("rejected mutation changed store: %v", err)
	}
	temps, err := filepath.Glob(path + ".tmp.*")
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary report files = %v, %v", temps, err)
	}
}

func TestLargeTimerGenerationMatchesCoordinatorAssignment(t *testing.T) {
	start := time.Date(2026, 7, 15, 5, 39, 17, 0, time.UTC)
	record, err := NewDeadline("issue-133", "T-worker", TimerLarge, start, "large issue implementation")
	if err != nil {
		t.Fatal(err)
	}
	generation := record.Generations[0]
	if generation.Generation != 1 || generation.BudgetSeconds != int64((2*time.Hour)/time.Second) || !generation.Deadline.Equal(time.Date(2026, 7, 15, 7, 39, 17, 0, time.UTC)) {
		t.Fatalf("large generation = %+v", generation)
	}
}

func TestNonDefaultTimerRequiresImmutableReason(t *testing.T) {
	start := time.Date(2026, 7, 15, 5, 39, 17, 0, time.UTC)
	if _, err := NewDeadline("issue-133", "T-worker", TimerLarge, start, ""); err == nil {
		t.Fatal("large timer without scope reason succeeded")
	}
	if _, err := NewDeadline("issue-133", "T-worker", TimerMedium, start, "unexpected"); err == nil {
		t.Fatal("default medium timer with override reason succeeded")
	}
}

func TestDeadlineExtensionPausesDiagnosticsAndNearest(t *testing.T) {
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	r, err := NewDeadline("group", "T-one", TimerSmall, start, "small focused change")
	if err != nil {
		t.Fatal(err)
	}
	if err := ExtendDeadline(&r, 15*time.Minute, start.Add(10*time.Minute), "approved"); err != nil {
		t.Fatal(err)
	}
	if err := ExtendDeadline(&r, time.Minute, start.Add(11*time.Minute), "again"); err == nil {
		t.Fatal("second extension accepted")
	}
	r.ExternalWaits = []ExternalWaitEvidence{{Kind: "ci", Reference: "build-1", StartedAt: start.Add(20 * time.Minute), EndedAt: start.Add(50 * time.Minute), Demonstrated: true}}
	r.OracleReview, r.Finish = DeadlinePhase{StartedAt: start}, DeadlinePhase{StartedAt: start}
	events, err := DeriveDiagnostics(&r, start.Add(76*time.Minute), 2)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[DiagnosticKind]bool{}
	for _, event := range events {
		seen[event.Kind] = true
	}
	for _, kind := range []DiagnosticKind{DiagnosticStale, DiagnosticOverdue, DiagnosticBlocker, DiagnosticOracleWarning, DiagnosticExternalWaitAlert, DiagnosticFinishAlert} {
		if !seen[kind] {
			t.Errorf("missing %s", kind)
		}
	}
	before := len(r.Diagnostics)
	if got, err := DeriveDiagnostics(&r, start.Add(80*time.Minute), 1); err != nil || len(got) != 0 || len(r.Diagnostics) != before {
		t.Fatalf("superseded diagnostics = %v, %v", got, err)
	}
	r2, _ := NewDeadline("another", "T-two", TimerMedium, start, "")
	next, ok, err := NearestDiagnosticDeadline([]DeadlineRecord{r2}, start)
	if err != nil || !ok || next.Kind != DiagnosticStale || !next.At.Equal(start.Add(15*time.Minute)) {
		t.Fatalf("nearest = %+v, %v, %v", next, ok, err)
	}
}

func TestDeadlinePhaseCompletionQueueBoundariesAndRepeatedEpisodes(t *testing.T) {
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	r, err := NewDeadline("group", "T-one", TimerMedium, start, "")
	if err != nil {
		t.Fatal(err)
	}
	r.OracleReview = DeadlinePhase{StartedAt: start, EndedAt: start.Add(5 * time.Minute)}
	r.Finish = DeadlinePhase{StartedAt: start, EndedAt: start.Add(11 * time.Minute)}
	events, err := DeriveDiagnostics(&r, start.Add(15*time.Minute), 1)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[DiagnosticKind]bool{}
	for _, event := range events {
		seen[event.Kind] = true
	}
	if seen[DiagnosticOracleWarning] || !seen[DiagnosticFinishAlert] || !seen[DiagnosticStale] {
		t.Fatalf("phase boundary diagnostics = %+v", events)
	}
	r.ProgressAt = start.Add(20 * time.Minute)
	events, err = DeriveDiagnostics(&r, start.Add(35*time.Minute), 1)
	if err != nil {
		t.Fatal(err)
	}
	staleCount := 0
	for _, event := range r.Diagnostics {
		if event.Kind == DiagnosticStale {
			staleCount++
		}
	}
	if staleCount != 2 {
		t.Fatalf("repeated stale episodes = %+v", r.Diagnostics)
	}
	for _, event := range events {
		if event.Kind == DiagnosticOracleWarning {
			t.Fatal("closed short Oracle phase later warned")
		}
	}
}

func TestNearestDeadlineMatchesWaitAfterThresholdAndPausesBeforeThreshold(t *testing.T) {
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	after, _ := NewDeadline("after", "T-one", TimerMedium, start, "")
	after.ExternalWaits = []ExternalWaitEvidence{{Kind: "ci", Reference: "after", StartedAt: start.Add(20 * time.Minute), Demonstrated: true}}
	next, ok, err := NearestDiagnosticDeadline([]DeadlineRecord{after}, start.Add(25*time.Minute))
	if err != nil || !ok || next.Kind != DiagnosticStale || !next.At.Equal(start.Add(25*time.Minute)) {
		t.Fatalf("wait after stale threshold = %+v, %v, %v", next, ok, err)
	}
	before, _ := NewDeadline("before", "T-two", TimerMedium, start, "")
	before.ExternalWaits = []ExternalWaitEvidence{{Kind: "ci", Reference: "before", StartedAt: start.Add(10 * time.Minute), Demonstrated: true}}
	next, ok, err = NearestDiagnosticDeadline([]DeadlineRecord{before}, start.Add(10*time.Minute))
	if err != nil || !ok || next.Kind != DiagnosticExternalWaitAlert || !next.At.Equal(start.Add(30*time.Minute)) {
		t.Fatalf("wait before stale threshold = %+v, %v, %v", next, ok, err)
	}
}

func TestDerivedDeadlineEventsPersistWithoutChangingReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), ReportsFile)
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	report := testReport(start)
	if _, err := SubmitReport(path, report); err != nil {
		t.Fatal(err)
	}
	deadline, _ := NewDeadline("group", "T-one", TimerMedium, start, "")
	if err := StoreDeadline(path, deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := DeriveDiagnostics(&deadline, start.Add(time.Hour), 1); err != nil {
		t.Fatal(err)
	}
	if err := StoreDeadline(path, deadline); err != nil {
		t.Fatal(err)
	}
	deadlines, err := LoadDeadlines(path)
	if err != nil || len(deadlines) != 1 || len(deadlines[0].Diagnostics) < 3 {
		t.Fatalf("durable diagnostics = %+v, %v", deadlines, err)
	}
	reports, err := LoadReports(path)
	if err != nil || len(reports) != 1 || reports[0].Status != ReportReady || !reports[0].AuthorizedAt.IsZero() {
		t.Fatalf("diagnostics altered report state = %+v, %v", reports, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("reports mode = %v, %v", info.Mode().Perm(), err)
	}
}
