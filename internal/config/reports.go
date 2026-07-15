// Package config's report store is deliberately passive: callers provide times and
// hold the machine lock. The versioned file is replaced atomically with mode 0600.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ReportsSchemaVersion     = 1
	ReportIDMaxLength        = 256
	ReportHashMaxLength      = 256
	ReportIssueMaxLength     = 256
	ReportReferenceMaxLength = 256
	ReportURLMaxLength       = 2048
	ReportSummaryMaxLength   = 1024
	// ReportReadyDefinition is the coordinator-verification contract represented
	// by ready. Merge, post-merge verification, and finish stay separate.
	ReportReadyDefinition = "implementation, tests, one review, pull request, and normal CI complete"
)

type ReportStatus string

const (
	ReportReady   ReportStatus = "ready"
	ReportBlocked ReportStatus = "blocked"
	ReportMerged  ReportStatus = "merged"
)

type ReportSubmitOutcome string

const (
	ReportRecorded  ReportSubmitOutcome = "recorded"
	ReportDuplicate ReportSubmitOutcome = "duplicate"
)

type AcknowledgeOutcome string

const (
	ReportAcknowledged   AcknowledgeOutcome = "acknowledged"
	AcknowledgeDuplicate AcknowledgeOutcome = "duplicate"
)

type ReportEvent struct {
	Kind      string       `json:"kind"`
	Status    ReportStatus `json:"status,omitempty"`
	At        time.Time    `json:"at"`
	Thread    string       `json:"thread,omitempty"`
	Reference string       `json:"reference,omitempty"`
}
type ReportRecord struct {
	SchemaVersion          int           `json:"schema_version"`
	ReportID               string        `json:"report_id"`
	RequestHash            string        `json:"request_hash"`
	GroupID                string        `json:"group_id"`
	MemberThread           string        `json:"member_thread"`
	Issue                  string        `json:"issue,omitempty"`
	Reference              string        `json:"reference,omitempty"`
	PRURL                  string        `json:"pr_url,omitempty"`
	Summary                string        `json:"summary,omitempty"`
	Status                 ReportStatus  `json:"status"`
	CreatedAt              time.Time     `json:"created_at"`
	UpdatedAt              time.Time     `json:"updated_at"`
	AcknowledgedAt         time.Time     `json:"acknowledged_at,omitempty"`
	AuthorizedAt           time.Time     `json:"authorized_at,omitempty"`
	AuthorizingThread      string        `json:"authorizing_thread,omitempty"`
	AuthorizationReference string        `json:"authorization_reference,omitempty"`
	Events                 []ReportEvent `json:"events"`
}

type ReportCommitError struct{ Err error }

func (e *ReportCommitError) Error() string { return "commit reports store: " + e.Err.Error() }
func (e *ReportCommitError) Unwrap() error { return e.Err }

func ReportRequestHash(group, thread, issue, reference string) string {
	data, _ := json.Marshal([]string{group, thread, issue, reference})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

type TimerSize string

const (
	TimerSmall  TimerSize = "Small"
	TimerMedium TimerSize = "Medium"
	TimerLarge  TimerSize = "Large"
)

func StandardTimerBudget(size TimerSize) (time.Duration, error) {
	switch size {
	case TimerSmall:
		return 30 * time.Minute, nil
	case TimerMedium:
		return time.Hour, nil
	case TimerLarge:
		return 2 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid timer size %q", size)
}

type TimerGeneration struct {
	Generation    int       `json:"generation"`
	Size          TimerSize `json:"size"`
	BudgetSeconds int64     `json:"budget_seconds"`
	StartedAt     time.Time `json:"started_at"`
	Deadline      time.Time `json:"deadline"`
	AssignedAt    time.Time `json:"assigned_at"`
	Reason        string    `json:"reason,omitempty"`
}
type ExternalWaitEvidence struct {
	Kind         string    `json:"kind"`
	Reference    string    `json:"reference"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
	Demonstrated bool      `json:"demonstrated"`
}
type DiagnosticKind string

const (
	DiagnosticStale             DiagnosticKind = "stale"
	DiagnosticOverdue           DiagnosticKind = "overdue"
	DiagnosticBlocker           DiagnosticKind = "blocker"
	DiagnosticOracleWarning     DiagnosticKind = "oracle_warning"
	DiagnosticExternalWaitAlert DiagnosticKind = "external_wait_alert"
	DiagnosticFinishAlert       DiagnosticKind = "finish_alert"
)

type DiagnosticEvent struct {
	Generation int            `json:"generation"`
	Kind       DiagnosticKind `json:"kind"`
	Source     string         `json:"source"`
	At         time.Time      `json:"at"`
	Reason     string         `json:"reason"`
}
type DeadlinePhase struct {
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}
type DeadlineRecord struct {
	GroupID       string                 `json:"group_id"`
	MemberThread  string                 `json:"member_thread"`
	Generations   []TimerGeneration      `json:"generations"`
	ExternalWaits []ExternalWaitEvidence `json:"external_waits,omitempty"`
	ProgressAt    time.Time              `json:"progress_at"`
	OracleReview  DeadlinePhase          `json:"oracle_review,omitempty"`
	Finish        DeadlinePhase          `json:"finish,omitempty"`
	Diagnostics   []DiagnosticEvent      `json:"diagnostics,omitempty"`
}
type reportsFile struct {
	SchemaVersion int              `json:"schema_version"`
	Reports       []ReportRecord   `json:"reports"`
	Deadlines     []DeadlineRecord `json:"deadlines"`
}

func LoadReports(path string) ([]ReportRecord, error) {
	f, e := loadReportsFile(path)
	return f.Reports, e
}
func LoadPendingReports(path string) ([]ReportRecord, error) {
	rs, e := LoadReports(path)
	if e != nil {
		return nil, e
	}
	out := []ReportRecord{}
	for _, r := range rs {
		if r.AcknowledgedAt.IsZero() {
			out = append(out, r)
		}
	}
	return out, nil
}
func ReportHistory(path, id string) ([]ReportEvent, error) {
	rs, e := LoadReports(path)
	if e != nil {
		return nil, e
	}
	for _, r := range rs {
		if r.ReportID == id {
			return append([]ReportEvent(nil), r.Events...), nil
		}
	}
	return nil, fmt.Errorf("report %q was not found", id)
}

func SubmitReport(path string, candidate ReportRecord) (ReportSubmitOutcome, error) {
	return submitReport(path, candidate, true)
}

func PlanReportSubmission(path string, candidate ReportRecord) (ReportSubmitOutcome, error) {
	return submitReport(path, candidate, false)
}

func submitReport(path string, candidate ReportRecord, persist bool) (ReportSubmitOutcome, error) {
	if candidate.SchemaVersion == 0 {
		candidate.SchemaVersion = ReportsSchemaVersion
	}
	if err := validateReportSubmission(candidate); err != nil {
		return "", err
	}
	f, err := loadReportsFile(path)
	if err != nil {
		return "", err
	}
	for i, old := range f.Reports {
		if old.ReportID != candidate.ReportID {
			continue
		}
		if !sameReportBinding(old, candidate) {
			return "", fmt.Errorf("report %q is already bound to a different request", candidate.ReportID)
		}
		if old.Status == candidate.Status {
			if old.PRURL == candidate.PRURL && old.Summary == candidate.Summary {
				return ReportDuplicate, nil
			}
			return "", fmt.Errorf("report %q is already bound to different payload for status %s", candidate.ReportID, candidate.Status)
		}
		if old.Status == ReportMerged {
			return "", fmt.Errorf("merged report cannot transition to %s", candidate.Status)
		}
		if !old.AuthorizedAt.IsZero() && candidate.Status != ReportMerged {
			return "", errors.New("finish-authorized report may only progress to merged")
		}
		if !old.AuthorizedAt.IsZero() && (old.PRURL != candidate.PRURL || old.Summary != candidate.Summary) {
			return "", errors.New("finish-authorized report payload is immutable")
		}
		if candidate.Status == ReportMerged && old.AuthorizedAt.IsZero() {
			return "", errors.New("merged report requires finish authorization")
		}
		candidate.CreatedAt = old.CreatedAt
		candidate.AuthorizedAt = old.AuthorizedAt
		candidate.AuthorizingThread = old.AuthorizingThread
		candidate.AuthorizationReference = old.AuthorizationReference
		candidate.AcknowledgedAt = time.Time{}
		candidate.Events = append(append([]ReportEvent(nil), old.Events...), ReportEvent{Kind: "status", Status: candidate.Status, At: candidate.UpdatedAt})
		if err := validateReportRecord(candidate); err != nil {
			return "", err
		}
		f.Reports[i] = candidate
		if !persist {
			return ReportRecorded, nil
		}
		return ReportRecorded, commitReportsFile(path, f)
	}
	if candidate.Status == ReportMerged {
		return "", errors.New("merged report requires existing finish authorization")
	}
	candidate.Events = []ReportEvent{{Kind: "status", Status: candidate.Status, At: candidate.CreatedAt}}
	if err := validateReportRecord(candidate); err != nil {
		return "", err
	}
	f.Reports = append(f.Reports, candidate)
	if !persist {
		return ReportRecorded, nil
	}
	return ReportRecorded, commitReportsFile(path, f)
}
func AcknowledgeReport(path, id string, at time.Time) (AcknowledgeOutcome, error) {
	return acknowledgeReport(path, id, at, true)
}
func PlanReportAcknowledgement(path, id string, at time.Time) (AcknowledgeOutcome, error) {
	return acknowledgeReport(path, id, at, false)
}
func acknowledgeReport(path, id string, at time.Time, persist bool) (AcknowledgeOutcome, error) {
	if at.IsZero() {
		return "", errors.New("acknowledgement time is required")
	}
	f, e := loadReportsFile(path)
	if e != nil {
		return "", e
	}
	for i, r := range f.Reports {
		if r.ReportID == id {
			if !r.AcknowledgedAt.IsZero() {
				return AcknowledgeDuplicate, nil
			}
			if at.Before(r.UpdatedAt) {
				return "", errors.New("acknowledgement precedes report update")
			}
			r.AcknowledgedAt = at
			r.UpdatedAt = at
			r.Events = append(r.Events, ReportEvent{Kind: "acknowledged", At: at})
			f.Reports[i] = r
			if !persist {
				return ReportAcknowledged, nil
			}
			return ReportAcknowledged, commitReportsFile(path, f)
		}
	}
	return "", fmt.Errorf("report %q was not found", id)
}
func AuthorizeReportFinish(path, id string, at time.Time, thread, ref string) (ReportSubmitOutcome, error) {
	return authorizeReportFinish(path, id, at, thread, ref, true)
}
func PlanReportFinishAuthorization(path, id string, at time.Time, thread, ref string) (ReportSubmitOutcome, error) {
	return authorizeReportFinish(path, id, at, thread, ref, false)
}
func authorizeReportFinish(path, id string, at time.Time, thread, ref string, persist bool) (ReportSubmitOutcome, error) {
	if at.IsZero() {
		return "", errors.New("authorization time is required")
	}
	if err := textField("authorization reference", ref, ReportReferenceMaxLength, false); err != nil {
		return "", err
	}
	if thread == "" {
		return "", errors.New("authorizing thread is required")
	}
	c, e := CanonicalThreadID(thread)
	if e != nil || c != thread {
		return "", errors.New("authorizing thread must be canonical")
	}
	f, e := loadReportsFile(path)
	if e != nil {
		return "", e
	}
	for i, r := range f.Reports {
		if r.ReportID != id {
			continue
		}
		if !r.AuthorizedAt.IsZero() {
			if r.AuthorizingThread == thread && r.AuthorizationReference == ref {
				return ReportDuplicate, nil
			}
			return "", errors.New("report has conflicting finish authorization")
		}
		if r.Status != ReportReady {
			return "", errors.New("finish authorization requires ready status")
		}
		if at.Before(r.UpdatedAt) {
			return "", errors.New("authorization precedes report update")
		}
		r.AuthorizedAt = at
		r.AuthorizingThread = thread
		r.AuthorizationReference = ref
		r.UpdatedAt = at
		r.Events = append(r.Events, ReportEvent{Kind: "authorized", At: at, Thread: thread, Reference: ref})
		f.Reports[i] = r
		if !persist {
			return ReportRecorded, nil
		}
		return ReportRecorded, commitReportsFile(path, f)
	}
	return "", fmt.Errorf("report %q was not found", id)
}

func LoadDeadlines(path string) ([]DeadlineRecord, error) {
	f, e := loadReportsFile(path)
	return f.Deadlines, e
}
func StoreDeadline(path string, r DeadlineRecord) error {
	f, e := loadReportsFile(path)
	if e != nil {
		return e
	}
	if e = validateDeadline(r); e != nil {
		return e
	}
	key := deadlineKey(r)
	for i, x := range f.Deadlines {
		if deadlineKey(x) == key {
			if len(r.Generations) < len(x.Generations) || r.Generations[0] != x.Generations[0] {
				return errors.New("initial timer generation is immutable")
			}
			if len(x.Generations) == 2 && r.Generations[1] != x.Generations[1] {
				return errors.New("timer extension is immutable")
			}
			if !externalWaitProgressAllowed(x.ExternalWaits, r.ExternalWaits) || !deadlinePrefix(x.Diagnostics, r.Diagnostics) {
				return errors.New("deadline evidence and diagnostics are append-only")
			}
			if r.ProgressAt.Before(x.ProgressAt) {
				return errors.New("deadline progress cannot regress")
			}
			if !phaseProgressAllowed(x.OracleReview, r.OracleReview) || !phaseProgressAllowed(x.Finish, r.Finish) {
				return errors.New("deadline phase evidence may only start once and then close")
			}
			f.Deadlines[i] = r
			return commitReportsFile(path, f)
		}
	}
	f.Deadlines = append(f.Deadlines, r)
	return commitReportsFile(path, f)
}
func NewDeadline(group, thread string, size TimerSize, start time.Time, reason string) (DeadlineRecord, error) {
	b, e := StandardTimerBudget(size)
	r := DeadlineRecord{GroupID: group, MemberThread: thread, Generations: []TimerGeneration{{Generation: 1, Size: size, BudgetSeconds: int64(b / time.Second), StartedAt: start, Deadline: start.Add(b), AssignedAt: start, Reason: reason}}, ProgressAt: start}
	return r, func() error {
		if e != nil {
			return e
		}
		return validateDeadline(r)
	}()
}
func ExtendDeadline(r *DeadlineRecord, extra time.Duration, at time.Time, reason string) error {
	if len(r.Generations) != 1 {
		return errors.New("deadline may be extended at most once")
	}
	g := r.Generations[0]
	if strings.TrimSpace(reason) == "" {
		return errors.New("extension reason is required")
	}
	if extra <= 0 || extra > time.Duration(g.BudgetSeconds)*time.Second/2 {
		return errors.New("extension must be positive and at most half the original budget")
	}
	if at.IsZero() || at.Before(g.StartedAt) {
		return errors.New("valid extension time is required")
	}
	r.Generations = append(r.Generations, TimerGeneration{Generation: 2, Size: g.Size, BudgetSeconds: g.BudgetSeconds + int64(extra/time.Second), StartedAt: g.StartedAt, Deadline: g.Deadline.Add(extra), AssignedAt: at, Reason: reason})
	return validateDeadline(*r)
}

func DeriveDiagnostics(r *DeadlineRecord, now time.Time, generation int) ([]DiagnosticEvent, error) {
	if err := validateDeadline(*r); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, errors.New("now is required")
	}
	current := r.Generations[len(r.Generations)-1]
	if generation != current.Generation {
		return nil, nil
	}
	before := len(r.Diagnostics)
	add := func(k DiagnosticKind, source, reason string) {
		for _, d := range r.Diagnostics {
			if d.Generation == generation && d.Kind == k && d.Source == source {
				return
			}
		}
		r.Diagnostics = append(r.Diagnostics, DiagnosticEvent{Generation: generation, Kind: k, Source: source, At: now, Reason: reason})
	}
	if activeElapsedSince(*r, r.ProgressAt, now) >= 15*time.Minute {
		add(DiagnosticStale, timeSource(r.ProgressAt), "no progress for 15 minutes")
	}
	if activeElapsed(*r, now) >= time.Duration(current.BudgetSeconds)*time.Second {
		source := fmt.Sprintf("generation:%d", generation)
		add(DiagnosticOverdue, source, "active budget exceeded")
		add(DiagnosticBlocker, source, "soft deadline crossed; coordinator action required")
	}
	if phaseElapsed(r.OracleReview, now) >= 10*time.Minute {
		add(DiagnosticOracleWarning, timeSource(r.OracleReview.StartedAt), "oracle running over 10 minutes")
	}
	if phaseElapsed(r.Finish, now) >= 10*time.Minute {
		add(DiagnosticFinishAlert, timeSource(r.Finish.StartedAt), "finish running over 10 minutes")
	}
	for _, w := range r.ExternalWaits {
		end := w.EndedAt
		if end.IsZero() {
			end = now
		}
		if end.Sub(w.StartedAt) >= 20*time.Minute {
			add(DiagnosticExternalWaitAlert, waitSource(w), "external wait over 20 minutes")
		}
	}
	return append([]DiagnosticEvent(nil), r.Diagnostics[before:]...), nil
}

type NextDiagnostic struct {
	At           time.Time
	GroupID      string
	MemberThread string
	Generation   int
	Kind         DiagnosticKind
	Source       string
}

func NearestDiagnosticDeadline(records []DeadlineRecord, now time.Time) (NextDiagnostic, bool, error) {
	var best NextDiagnostic
	found := false
	for _, r := range records {
		if e := validateDeadline(r); e != nil {
			return best, false, e
		}
		g := r.Generations[len(r.Generations)-1]
		cs := []NextDiagnostic{}
		if at, known := activeThreshold(r, r.ProgressAt, 15*time.Minute, now); known {
			cs = append(cs, NextDiagnostic{at, r.GroupID, r.MemberThread, g.Generation, DiagnosticStale, timeSource(r.ProgressAt)})
		}
		if at, known := activeThreshold(r, g.StartedAt, time.Duration(g.BudgetSeconds)*time.Second, now); known {
			cs = append(cs, NextDiagnostic{at, r.GroupID, r.MemberThread, g.Generation, DiagnosticOverdue, fmt.Sprintf("generation:%d", g.Generation)})
		}
		if phaseNeedsDiagnostic(r.OracleReview, 10*time.Minute) {
			cs = append(cs, NextDiagnostic{r.OracleReview.StartedAt.Add(10 * time.Minute), r.GroupID, r.MemberThread, g.Generation, DiagnosticOracleWarning, timeSource(r.OracleReview.StartedAt)})
		}
		if phaseNeedsDiagnostic(r.Finish, 10*time.Minute) {
			cs = append(cs, NextDiagnostic{r.Finish.StartedAt.Add(10 * time.Minute), r.GroupID, r.MemberThread, g.Generation, DiagnosticFinishAlert, timeSource(r.Finish.StartedAt)})
		}
		for _, w := range r.ExternalWaits {
			if w.EndedAt.IsZero() || w.EndedAt.Sub(w.StartedAt) >= 20*time.Minute {
				cs = append(cs, NextDiagnostic{w.StartedAt.Add(20 * time.Minute), r.GroupID, r.MemberThread, g.Generation, DiagnosticExternalWaitAlert, waitSource(w)})
			}
		}
		for _, c := range cs {
			if c.At.Before(now) {
				c.At = now
			}
			if diagnosticExists(r, c.Kind, c.Generation, c.Source) {
				continue
			}
			if !found || nextLess(c, best) {
				best = c
				found = true
			}
		}
	}
	return best, found, nil
}

func validateReportSubmission(r ReportRecord) error {
	if !r.AcknowledgedAt.IsZero() || !r.AuthorizedAt.IsZero() || r.AuthorizingThread != "" || r.AuthorizationReference != "" || len(r.Events) != 0 {
		return errors.New("report submission cannot set acknowledgement, authorization, or history")
	}
	return validateReportFields(r)
}

func validateReportFields(r ReportRecord) error {
	if r.SchemaVersion != ReportsSchemaVersion {
		return fmt.Errorf("unsupported report schema version %d", r.SchemaVersion)
	}
	for _, x := range []struct {
		n, v string
		m    int
		req  bool
	}{{"report ID", r.ReportID, ReportIDMaxLength, true}, {"request hash", r.RequestHash, ReportHashMaxLength, true}, {"issue", r.Issue, ReportIssueMaxLength, false}, {"reference", r.Reference, ReportReferenceMaxLength, false}, {"summary", r.Summary, ReportSummaryMaxLength, false}} {
		if e := textField(x.n, x.v, x.m, x.req); e != nil {
			return e
		}
	}
	if e := ValidateGroupID(r.GroupID); e != nil {
		return e
	}
	c, e := CanonicalThreadID(r.MemberThread)
	if e != nil || c != r.MemberThread {
		return errors.New("member thread must be canonical")
	}
	if r.PRURL != "" {
		if e := textField("PR URL", r.PRURL, ReportURLMaxLength, false); e != nil {
			return e
		}
		u, e := url.Parse(r.PRURL)
		if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
			return errors.New("PR URL must be an absolute HTTP(S) URL")
		}
	}
	if r.Status != ReportReady && r.Status != ReportBlocked && r.Status != ReportMerged {
		return fmt.Errorf("invalid report status %q", r.Status)
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return errors.New("valid report timestamps are required")
	}
	wantHash := ReportRequestHash(r.GroupID, r.MemberThread, r.Issue, r.Reference)
	if r.RequestHash != wantHash {
		return errors.New("request hash does not match immutable report binding")
	}
	return nil
}

func validateReportRecord(r ReportRecord) error {
	if err := validateReportFields(r); err != nil {
		return err
	}
	if len(r.Events) == 0 || r.Events[0].Kind != "status" || r.Events[0].Status == "" || !r.Events[0].At.Equal(r.CreatedAt) {
		return errors.New("report history must begin with its created status")
	}
	status := ReportStatus("")
	var acknowledged, authorized time.Time
	var previousEventAt time.Time
	for _, event := range r.Events {
		if event.At.IsZero() || event.At.Before(r.CreatedAt) || !previousEventAt.IsZero() && event.At.Before(previousEventAt) {
			return errors.New("invalid report event time")
		}
		previousEventAt = event.At
		switch event.Kind {
		case "status":
			if event.Status != ReportReady && event.Status != ReportBlocked && event.Status != ReportMerged {
				return errors.New("invalid status event")
			}
			if status == ReportMerged && event.Status != ReportMerged {
				return errors.New("merged report history regresses")
			}
			if event.Status == ReportMerged && authorized.IsZero() {
				return errors.New("merged report history lacks prior authorization")
			}
			if !authorized.IsZero() && event.Status != ReportMerged {
				return errors.New("finish-authorized report history may only progress to merged")
			}
			status = event.Status
			acknowledged = time.Time{}
		case "acknowledged":
			acknowledged = event.At
		case "authorized":
			if status != ReportReady || !authorized.IsZero() {
				return errors.New("finish authorization history requires ready status exactly once")
			}
			if event.Thread == "" {
				return errors.New("authorization event thread is required")
			}
			thread, err := CanonicalThreadID(event.Thread)
			if err != nil || thread != event.Thread {
				return errors.New("authorization event thread must be canonical")
			}
			if err := textField("authorization event reference", event.Reference, ReportReferenceMaxLength, false); err != nil {
				return err
			}
			authorized = event.At
		default:
			return errors.New("invalid report event")
		}
	}
	if status != r.Status || !acknowledged.Equal(r.AcknowledgedAt) || !authorized.Equal(r.AuthorizedAt) {
		return errors.New("report state does not match its history")
	}
	if !authorized.IsZero() && r.AuthorizingThread == "" || authorized.IsZero() && (r.AuthorizingThread != "" || r.AuthorizationReference != "") {
		return errors.New("report authorization fields are inconsistent")
	}
	if r.AuthorizingThread != "" {
		thread, err := CanonicalThreadID(r.AuthorizingThread)
		if err != nil || thread != r.AuthorizingThread {
			return errors.New("report authorizing thread must be canonical")
		}
	}
	if !authorized.IsZero() {
		lastAuthorization := ReportEvent{}
		for _, event := range r.Events {
			if event.Kind == "authorized" {
				lastAuthorization = event
			}
		}
		if lastAuthorization.Thread != r.AuthorizingThread || lastAuthorization.Reference != r.AuthorizationReference {
			return errors.New("report authorization does not match its history")
		}
	}
	if len(r.Events) > 0 && r.UpdatedAt.Before(r.Events[len(r.Events)-1].At) {
		return errors.New("report updated_at precedes its history")
	}
	return nil
}
func sameReportBinding(a, b ReportRecord) bool {
	return a.RequestHash == b.RequestHash && a.GroupID == b.GroupID && a.MemberThread == b.MemberThread && a.Issue == b.Issue && a.Reference == b.Reference
}
func textField(n, v string, max int, required bool) error {
	if required && v == "" {
		return fmt.Errorf("%s is required", n)
	}
	if len(v) > max {
		return fmt.Errorf("%s exceeds %d bytes", n, max)
	}
	if !utf8.ValidString(v) {
		return fmt.Errorf("%s is not UTF-8", n)
	}
	for _, c := range v {
		if c < 32 || c == 127 {
			return fmt.Errorf("%s contains control characters", n)
		}
	}
	return nil
}
func validateDeadline(r DeadlineRecord) error {
	if e := ValidateGroupID(r.GroupID); e != nil {
		return e
	}
	c, e := CanonicalThreadID(r.MemberThread)
	if e != nil || c != r.MemberThread {
		return errors.New("deadline thread must be canonical")
	}
	if len(r.Generations) < 1 || len(r.Generations) > 2 {
		return errors.New("deadline requires one or two generations")
	}
	base := r.Generations[0]
	standard, e := StandardTimerBudget(base.Size)
	if e != nil {
		return e
	}
	if base.Generation != 1 || base.BudgetSeconds != int64(standard/time.Second) || base.StartedAt.IsZero() || !base.Deadline.Equal(base.StartedAt.Add(standard)) || !base.AssignedAt.Equal(base.StartedAt) || base.Size == TimerMedium && base.Reason != "" || base.Size != TimerMedium && strings.TrimSpace(base.Reason) == "" {
		return errors.New("invalid initial timer generation")
	}
	if len(r.Generations) == 2 {
		g := r.Generations[1]
		extra := g.BudgetSeconds - base.BudgetSeconds
		if g.Generation != 2 || g.Size != base.Size || !g.StartedAt.Equal(base.StartedAt) || g.AssignedAt.Before(base.StartedAt) || strings.TrimSpace(g.Reason) == "" || extra <= 0 || extra > base.BudgetSeconds/2 || !g.Deadline.Equal(g.StartedAt.Add(time.Duration(g.BudgetSeconds)*time.Second)) {
			return errors.New("invalid timer extension")
		}
	}
	if r.ProgressAt.IsZero() {
		return errors.New("progress timestamp is required")
	}
	if r.ProgressAt.Before(base.StartedAt) {
		return errors.New("deadline activity timestamps cannot precede the timer")
	}
	if err := validatePhase("oracle review", r.OracleReview, base.StartedAt); err != nil {
		return err
	}
	if err := validatePhase("finish", r.Finish, base.StartedAt); err != nil {
		return err
	}
	for _, w := range r.ExternalWaits {
		if !w.Demonstrated || w.Kind == "" || w.Reference == "" || w.StartedAt.Before(base.StartedAt) || !w.EndedAt.IsZero() && !w.EndedAt.After(w.StartedAt) {
			return errors.New("invalid external wait evidence")
		}
		if e := textField("external wait kind", w.Kind, ReportReferenceMaxLength, true); e != nil {
			return e
		}
		if e := textField("external wait reference", w.Reference, ReportReferenceMaxLength, true); e != nil {
			return e
		}
	}
	for i, a := range r.ExternalWaits {
		ae := a.EndedAt
		if ae.IsZero() {
			ae = time.Unix(1<<62, 0)
		}
		for _, b := range r.ExternalWaits[i+1:] {
			be := b.EndedAt
			if be.IsZero() {
				be = time.Unix(1<<62, 0)
			}
			if a.StartedAt.Before(be) && b.StartedAt.Before(ae) {
				return errors.New("external waits overlap")
			}
		}
	}
	seenDiagnostics := make(map[string]bool)
	for _, d := range r.Diagnostics {
		if d.Generation < 1 || d.Generation > len(r.Generations) || d.Source == "" || d.At.Before(base.StartedAt) || d.Reason == "" || !validDiagnostic(d.Kind) {
			return errors.New("invalid diagnostic event")
		}
		key := fmt.Sprintf("%d\x00%s\x00%s", d.Generation, d.Kind, d.Source)
		if seenDiagnostics[key] {
			return errors.New("duplicate diagnostic event")
		}
		seenDiagnostics[key] = true
		if e := textField("diagnostic reason", d.Reason, ReportSummaryMaxLength, true); e != nil {
			return e
		}
	}
	return nil
}
func activeElapsed(r DeadlineRecord, now time.Time) time.Duration {
	start := r.Generations[0].StartedAt
	return activeElapsedSince(r, start, now)
}
func activeElapsedSince(r DeadlineRecord, start, now time.Time) time.Duration {
	elapsed := now.Sub(start)
	for _, w := range r.ExternalWaits {
		a := w.StartedAt
		if a.Before(start) {
			a = start
		}
		b := w.EndedAt
		if b.IsZero() || b.After(now) {
			b = now
		}
		if b.After(a) {
			elapsed -= b.Sub(a)
		}
	}
	if elapsed < 0 {
		return 0
	}
	return elapsed
}
func activeThreshold(r DeadlineRecord, start time.Time, budget time.Duration, now time.Time) (time.Time, bool) {
	d := start.Add(budget)
	waits := append([]ExternalWaitEvidence(nil), r.ExternalWaits...)
	sort.Slice(waits, func(i, j int) bool { return waits[i].StartedAt.Before(waits[j].StartedAt) })
	for _, w := range waits {
		if !w.StartedAt.Before(d) {
			continue
		}
		if w.EndedAt.IsZero() {
			if !w.StartedAt.After(now) {
				return time.Time{}, false
			}
			continue
		}
		a := w.StartedAt
		if a.Before(start) {
			a = start
		}
		if w.EndedAt.After(a) {
			d = d.Add(w.EndedAt.Sub(a))
		}
	}
	return d, true
}
func validDiagnostic(k DiagnosticKind) bool {
	switch k {
	case DiagnosticStale, DiagnosticOverdue, DiagnosticBlocker, DiagnosticOracleWarning, DiagnosticExternalWaitAlert, DiagnosticFinishAlert:
		return true
	}
	return false
}
func diagnosticExists(r DeadlineRecord, k DiagnosticKind, g int, source string) bool {
	for _, d := range r.Diagnostics {
		if d.Kind == k && d.Generation == g && d.Source == source {
			return true
		}
	}
	return false
}
func nextLess(a, b NextDiagnostic) bool {
	if !a.At.Equal(b.At) {
		return a.At.Before(b.At)
	}
	ak := a.GroupID + "\x00" + a.MemberThread + "\x00" + string(a.Kind) + "\x00" + a.Source
	bk := b.GroupID + "\x00" + b.MemberThread + "\x00" + string(b.Kind) + "\x00" + b.Source
	return ak < bk
}
func deadlineKey(r DeadlineRecord) string { return r.GroupID + "\x00" + r.MemberThread }

func deadlinePrefix[T comparable](old, next []T) bool {
	if len(next) < len(old) {
		return false
	}
	for i := range old {
		if old[i] != next[i] {
			return false
		}
	}
	return true
}

func externalWaitProgressAllowed(old, next []ExternalWaitEvidence) bool {
	if len(next) < len(old) {
		return false
	}
	for i := range old {
		if old[i] == next[i] {
			continue
		}
		closed := old[i]
		closed.EndedAt = next[i].EndedAt
		if !old[i].EndedAt.IsZero() || next[i].EndedAt.IsZero() || closed != next[i] {
			return false
		}
	}
	return true
}

func phaseProgressAllowed(old, next DeadlinePhase) bool {
	if old == next {
		return true
	}
	if old.StartedAt.IsZero() {
		return !next.StartedAt.IsZero()
	}
	return old.EndedAt.IsZero() && old.StartedAt.Equal(next.StartedAt) && !next.EndedAt.IsZero()
}

func validatePhase(name string, phase DeadlinePhase, timerStart time.Time) error {
	if phase.StartedAt.IsZero() {
		if !phase.EndedAt.IsZero() {
			return fmt.Errorf("%s cannot end before it starts", name)
		}
		return nil
	}
	if phase.StartedAt.Before(timerStart) || !phase.EndedAt.IsZero() && !phase.EndedAt.After(phase.StartedAt) {
		return fmt.Errorf("invalid %s evidence", name)
	}
	return nil
}

func phaseOpen(phase DeadlinePhase) bool {
	return !phase.StartedAt.IsZero() && phase.EndedAt.IsZero()
}

func phaseNeedsDiagnostic(phase DeadlinePhase, threshold time.Duration) bool {
	return phaseOpen(phase) || !phase.StartedAt.IsZero() && phase.EndedAt.Sub(phase.StartedAt) >= threshold
}

func phaseElapsed(phase DeadlinePhase, now time.Time) time.Duration {
	if phase.StartedAt.IsZero() {
		return 0
	}
	end := phase.EndedAt
	if end.IsZero() || end.After(now) {
		end = now
	}
	if end.Before(phase.StartedAt) {
		return 0
	}
	return end.Sub(phase.StartedAt)
}

func timeSource(value time.Time) string { return value.Format(time.RFC3339Nano) }
func waitSource(wait ExternalWaitEvidence) string {
	return wait.Kind + ":" + wait.Reference + ":" + timeSource(wait.StartedAt)
}

func loadReportsFile(path string) (reportsFile, error) {
	data, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return reportsFile{SchemaVersion: ReportsSchemaVersion}, nil
	}
	if e != nil {
		return reportsFile{}, e
	}
	var f reportsFile
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e = d.Decode(&f); e != nil {
		return f, fmt.Errorf("parse reports: %w", e)
	}
	if e = d.Decode(&struct{}{}); e != io.EOF {
		return f, errors.New("parse reports: trailing JSON data")
	}
	if f.SchemaVersion != ReportsSchemaVersion {
		return f, fmt.Errorf("unsupported reports file schema version %d", f.SchemaVersion)
	}
	seen := map[string]bool{}
	for i, r := range f.Reports {
		if e := validateReportRecord(r); e != nil {
			return f, fmt.Errorf("invalid report %d: %w", i+1, e)
		}
		if seen[r.ReportID] {
			return f, fmt.Errorf("duplicate report ID %q", r.ReportID)
		}
		seen[r.ReportID] = true
	}
	seen = map[string]bool{}
	for _, r := range f.Deadlines {
		if e := validateDeadline(r); e != nil {
			return f, e
		}
		k := deadlineKey(r)
		if seen[k] {
			return f, errors.New("duplicate deadline")
		}
		seen[k] = true
	}
	sort.Slice(f.Reports, func(i, j int) bool { return f.Reports[i].ReportID < f.Reports[j].ReportID })
	sort.Slice(f.Deadlines, func(i, j int) bool { return deadlineKey(f.Deadlines[i]) < deadlineKey(f.Deadlines[j]) })
	return f, nil
}
func writeReportsFile(path string, f reportsFile) error {
	sort.Slice(f.Reports, func(i, j int) bool { return f.Reports[i].ReportID < f.Reports[j].ReportID })
	sort.Slice(f.Deadlines, func(i, j int) bool { return deadlineKey(f.Deadlines[i]) < deadlineKey(f.Deadlines[j]) })
	for _, r := range f.Reports {
		if e := validateReportRecord(r); e != nil {
			return e
		}
	}
	for _, r := range f.Deadlines {
		if e := validateDeadline(r); e != nil {
			return e
		}
	}
	f.SchemaVersion = ReportsSchemaVersion
	data, e := json.Marshal(f)
	if e != nil {
		return e
	}
	data = append(data, '\n')
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	file, e := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if e != nil {
		return e
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if e = file.Chmod(0600); e == nil {
		_, e = file.Write(data)
	}
	if e == nil {
		e = file.Sync()
	}
	ce := file.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e := os.Rename(tmp, path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func commitReportsFile(path string, f reportsFile) error {
	if err := writeReportsFile(path, f); err != nil {
		return &ReportCommitError{Err: err}
	}
	return nil
}
