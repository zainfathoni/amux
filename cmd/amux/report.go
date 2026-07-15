package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

var (
	reportNow            = func() time.Time { return time.Now().UTC() }
	reportNotifyCallback = notifyReportCallback
)

func (a app) executeReport(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if err := validateReportInvocation(in); err != nil {
		return &env, result.Request(err)
	}
	switch in.Command.Name {
	case "submit":
		return a.submitReport(in, dir, &env)
	case "pending":
		return a.listPendingReports(in, dir, &env)
	case "history":
		return a.showReportHistory(in, dir, &env)
	case "acknowledge":
		return a.acknowledgeReport(in, dir, &env)
	case "authorize-finish":
		return a.authorizeReportFinish(in, dir, &env)
	default:
		return &env, result.Request(fmt.Errorf("unsupported report command %s", in.Command.Name))
	}
}

func validateReportInvocation(in invocation) error {
	s := in.Selectors
	switch in.Command.Name {
	case "submit":
		if s.ReportID == "" || s.Group == "" || s.Thread == "" || s.Status == "" || s.Summary == "" || s.All || s.Issue == "" && s.Reference == "" {
			return errors.New("report submit requires --report-id, --group, --thread, --status, --summary, and at least one of --issue or --reference")
		}
		if s.Status != string(config.ReportReady) && s.Status != string(config.ReportBlocked) && s.Status != string(config.ReportMerged) {
			return errors.New("report status must be ready, blocked, or merged")
		}
		if s.Status != string(config.ReportBlocked) && (s.PRURL == "" || s.PRURL == "none") {
			return fmt.Errorf("%s report requires --pr because ready includes a pull request and normal CI", s.Status)
		}
	case "pending":
		// Unscoped pending is intentionally a machine-local read of every report.
	case "history", "acknowledge":
		if s.ReportID == "" {
			return fmt.Errorf("report %s requires --report-id", in.Command.Name)
		}
	case "authorize-finish":
		if s.ReportID == "" || s.Thread == "" {
			return errors.New("report authorize-finish requires --report-id and --thread for the authorizing coordinator")
		}
	}
	return nil
}

func (a app) submitReport(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	if err := requireGroupMembership(dir, in.Selectors.Group, in.Selectors.Thread, false); err != nil {
		return rejectReport(env, in.Selectors.ReportID, in.Selectors.Group, in.Selectors.Thread, "submit", err)
	}
	prURL := in.Selectors.PRURL
	if prURL == "none" {
		prURL = ""
	}
	now := reportNow()
	record := config.ReportRecord{
		ReportID:     in.Selectors.ReportID,
		RequestHash:  config.ReportRequestHash(in.Selectors.Group, in.Selectors.Thread, in.Selectors.Issue, in.Selectors.Reference),
		GroupID:      in.Selectors.Group,
		MemberThread: in.Selectors.Thread,
		Issue:        in.Selectors.Issue,
		Reference:    in.Selectors.Reference,
		PRURL:        prURL,
		Summary:      in.Selectors.Summary,
		Status:       config.ReportStatus(in.Selectors.Status),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	var outcome config.ReportSubmitOutcome
	var err error
	if in.Options.DryRun {
		outcome, err = config.PlanReportSubmission(dir.ReportsPath(), record)
	} else {
		outcome, err = config.SubmitReport(dir.ReportsPath(), record)
	}
	if err != nil {
		return rejectReport(env, record.ReportID, record.GroupID, record.MemberThread, "submit", err)
	}
	if !in.Options.DryRun {
		record, err = findReport(dir.ReportsPath(), record.ReportID)
		if err != nil {
			return rejectReport(env, record.ReportID, record.GroupID, record.MemberThread, "submit", err)
		}
	}
	out := reportOutcome(record, "submit")
	if outcome == config.ReportDuplicate {
		out.Action = "duplicate"
		out.Message = "duplicate report replay; durable state unchanged"
		env.Skipped = append(env.Skipped, out)
	} else if in.Options.DryRun {
		out.Action = "record"
		out.Message = "report would be recorded"
		env.Planned = append(env.Planned, out)
	} else {
		out.Action = "recorded"
		out.Message = "report recorded"
		env.Successful = append(env.Successful, out)
	}
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", record.ReportID, record.Status, outcomeLabel(outcome, in.Options.DryRun), record.MemberThread)
	}
	if in.Options.DryRun {
		callback := result.Outcome{Resource: result.ResourceID{Kind: "callback", Group: record.GroupID, Path: record.ReportID}, Action: "notify", Message: "wake-up notification would be attempted only after durable report persistence", Callback: &result.CallbackDetails{ConfigDir: dir.Path}}
		env.Planned = append(env.Planned, callback)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "CALLBACK\t%s\t%s\tplanned\n", record.GroupID, record.ReportID)
		}
		return env, nil
	}
	callback, notifyErr := reportNotifyCallback(dir, record.GroupID, record.ReportID)
	if notifyErr != nil {
		callback.Action = "notify"
		callback.Message = "durable report persisted; callback failed separately and report remains pending"
		callback.Error = &result.Failure{Kind: result.ErrorRuntime, Message: notifyErr.Error()}
		env.Failed = append(env.Failed, callback)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "CALLBACK\t%s\t%s\tfailed\n", record.GroupID, record.ReportID)
		}
		return env, result.Runtime(notifyErr)
	}
	env.Successful = append(env.Successful, callback)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "CALLBACK\t%s\t%s\tnotified\n", record.GroupID, record.ReportID)
	}
	return env, nil
}

func (a app) listPendingReports(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	reports, err := config.LoadPendingReports(dir.ReportsPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, record := range reports {
		if !reportSelected(record, in.Selectors) {
			continue
		}
		out := reportOutcome(record, "pending")
		out.Report.Pending = true
		env.Successful = append(env.Successful, out)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\t%s\n", record.ReportID, record.GroupID, record.MemberThread, record.Status, record.Summary)
		}
	}
	return env, nil
}

func (a app) showReportHistory(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	record, err := findReport(dir.ReportsPath(), in.Selectors.ReportID)
	if err != nil {
		return rejectReport(env, in.Selectors.ReportID, "", "", "history", err)
	}
	for _, event := range record.Events {
		out := reportOutcome(record, event.Kind)
		out.Report.Status = string(event.Status)
		out.Report.UpdatedAt = event.At.Format(time.RFC3339Nano)
		out.Report.AuthorizingThread = event.Thread
		out.Message = event.Reference
		env.Successful = append(env.Successful, out)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", record.ReportID, event.Kind, event.Status, event.At.Format(time.RFC3339Nano))
		}
	}
	return env, nil
}

func (a app) acknowledgeReport(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	record, err := findReport(dir.ReportsPath(), in.Selectors.ReportID)
	if err != nil {
		return rejectReport(env, in.Selectors.ReportID, "", "", "acknowledge", err)
	}
	var outcome config.AcknowledgeOutcome
	if in.Options.DryRun {
		outcome, err = config.PlanReportAcknowledgement(dir.ReportsPath(), record.ReportID, reportNow())
	} else {
		outcome, err = config.AcknowledgeReport(dir.ReportsPath(), record.ReportID, reportNow())
	}
	if err != nil {
		return rejectReport(env, record.ReportID, record.GroupID, record.MemberThread, "acknowledge", err)
	}
	if !in.Options.DryRun {
		record, err = findReport(dir.ReportsPath(), record.ReportID)
		if err != nil {
			return rejectReport(env, record.ReportID, record.GroupID, record.MemberThread, "acknowledge", err)
		}
	}
	out := reportOutcome(record, "acknowledge")
	if outcome == config.AcknowledgeDuplicate {
		out.Action = "duplicate"
		out.Message = "report already acknowledged; authorization unchanged"
		env.Skipped = append(env.Skipped, out)
	} else if in.Options.DryRun {
		out.Action = "acknowledge"
		out.Message = "report would be acknowledged; authorization unchanged"
		env.Planned = append(env.Planned, out)
	} else {
		out.Action = "acknowledged"
		out.Message = "report acknowledged; authorization unchanged"
		env.Successful = append(env.Successful, out)
	}
	if !in.Options.JSON {
		label := "acknowledged"
		if outcome == config.AcknowledgeDuplicate {
			label = "duplicate"
		} else if in.Options.DryRun {
			label = "planned"
		}
		fmt.Fprintf(a.stdout, "%s\t%s\n", record.ReportID, label)
	}
	return env, nil
}

func (a app) authorizeReportFinish(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	record, err := findReport(dir.ReportsPath(), in.Selectors.ReportID)
	if err != nil {
		return rejectReport(env, in.Selectors.ReportID, "", "", "authorize-finish", err)
	}
	if err := requireGroupMembership(dir, record.GroupID, in.Selectors.Thread, true); err != nil {
		return rejectReport(env, record.ReportID, record.GroupID, record.MemberThread, "authorize-finish", err)
	}
	var outcome config.ReportSubmitOutcome
	if in.Options.DryRun {
		outcome, err = config.PlanReportFinishAuthorization(dir.ReportsPath(), record.ReportID, reportNow(), in.Selectors.Thread, in.Selectors.Reference)
	} else {
		outcome, err = config.AuthorizeReportFinish(dir.ReportsPath(), record.ReportID, reportNow(), in.Selectors.Thread, in.Selectors.Reference)
	}
	if err != nil {
		return rejectReport(env, record.ReportID, record.GroupID, record.MemberThread, "authorize-finish", err)
	}
	if !in.Options.DryRun {
		record, err = findReport(dir.ReportsPath(), record.ReportID)
		if err != nil {
			return rejectReport(env, record.ReportID, record.GroupID, record.MemberThread, "authorize-finish", err)
		}
	}
	out := reportOutcome(record, "authorize-finish")
	if outcome == config.ReportDuplicate {
		out.Action = "duplicate"
		out.Message = "identical finish authorization already recorded; acknowledgement unchanged"
		env.Skipped = append(env.Skipped, out)
	} else if in.Options.DryRun {
		out.Action = "authorize"
		out.Message = "finish authorization would be recorded; acknowledgement unchanged"
		env.Planned = append(env.Planned, out)
	} else {
		out.Action = "authorized"
		out.Message = "finish authorized; acknowledgement unchanged"
		env.Successful = append(env.Successful, out)
	}
	if !in.Options.JSON {
		label := "authorized"
		if outcome == config.ReportDuplicate {
			label = "duplicate"
		} else if in.Options.DryRun {
			label = "planned"
		}
		fmt.Fprintf(a.stdout, "%s\t%s\n", record.ReportID, label)
	}
	return env, nil
}

func requireGroupMembership(dir config.Directory, group, thread string, coordinator bool) error {
	memberships, err := config.LoadGroupsReadOnly(dir.GroupsPath())
	if err != nil {
		return err
	}
	for _, membership := range memberships {
		if membership.Group == group && membership.Thread == thread {
			if coordinator && membership.Role != config.GroupCoordinator {
				return fmt.Errorf("thread %s is not the coordinator for group %s", thread, group)
			}
			return nil
		}
	}
	return fmt.Errorf("thread %s is not a member of group %s", thread, group)
}

func findReport(path, id string) (config.ReportRecord, error) {
	reports, err := config.LoadReports(path)
	if err != nil {
		return config.ReportRecord{}, err
	}
	for _, record := range reports {
		if record.ReportID == id {
			return record, nil
		}
	}
	return config.ReportRecord{}, fmt.Errorf("report %q was not found", id)
}

func reportSelected(record config.ReportRecord, selectors selectors) bool {
	return (selectors.Group == "" || selectors.Group == record.GroupID) && (selectors.Thread == "" || selectors.Thread == record.MemberThread)
}

func reportOutcome(record config.ReportRecord, action string) result.Outcome {
	resource, _ := result.ReportResource(record.ReportID, record.GroupID, record.MemberThread)
	details := &result.ReportDetails{
		ReportID:    record.ReportID,
		RequestHash: record.RequestHash,
		Status:      string(record.Status),
		Issue:       record.Issue,
		Reference:   record.Reference,
		PRURL:       record.PRURL,
		Summary:     record.Summary,
		CreatedAt:   formatReportTime(record.CreatedAt),
		UpdatedAt:   formatReportTime(record.UpdatedAt),
		Pending:     record.AcknowledgedAt.IsZero(),
	}
	details.AcknowledgedAt = formatReportTime(record.AcknowledgedAt)
	details.AuthorizedAt = formatReportTime(record.AuthorizedAt)
	details.AuthorizingThread = record.AuthorizingThread
	return result.Outcome{Resource: resource, Action: action, Report: details}
}

func rejectReport(env *result.Envelope, id, group, thread, action string, err error) (*result.Envelope, error) {
	kind := result.ErrorPreflight
	wrapped := result.Preflight(err)
	var commit *config.ReportCommitError
	if errors.As(err, &commit) {
		kind = result.ErrorRuntime
		wrapped = result.Runtime(err)
	}
	resource := result.ResourceID{Kind: "report", Path: id, Group: group, Thread: thread}
	env.Failed = append(env.Failed, result.Outcome{Resource: resource, Action: "rejected", Message: action + " rejected", Report: &result.ReportDetails{ReportID: id}, Error: &result.Failure{Kind: kind, Message: err.Error()}})
	return env, wrapped
}

func formatReportTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func outcomeLabel(outcome config.ReportSubmitOutcome, dryRun bool) string {
	if outcome == config.ReportDuplicate {
		return "duplicate"
	}
	if dryRun {
		return "planned"
	}
	return string(outcome)
}
