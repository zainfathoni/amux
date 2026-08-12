package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

func isRetirementPath(path []string) bool {
	return len(path) == 2 && path[0] == "retirement"
}

func (a app) executeRetirement(parsed invocation, dir config.Directory) (*result.Envelope, error) {
	envelope := result.NewEnvelope("retirement inspect", false)
	if parsed.Command.Name != "inspect" || parsed.Selectors.Record == "" || len(parsed.Args) != 0 {
		return &envelope, result.Request(errors.New("usage: amux retirement inspect --record <ret-id>"))
	}
	inspection, err := config.DefaultRetirementStore().Inspect(context.Background(), dir, parsed.Selectors.Record)
	details := retirementDetails(inspection, parsed.Selectors.Record)
	resource := result.RetirementResource(parsed.Selectors.Record)
	if err != nil {
		code := config.RetirementRecordInvalid
		var retirementErr *config.RetirementError
		if errors.As(err, &retirementErr) {
			code = retirementErr.Code
		}
		envelope.Failed = append(envelope.Failed, result.Outcome{
			Resource:   resource,
			Action:     "inspect",
			Retirement: details,
			Error:      &result.Failure{Kind: result.ErrorPreflight, Code: code, Message: privacySafeRetirementMessage(code)},
		})
		return &envelope, result.Preflight(errors.New(privacySafeRetirementMessage(code)))
	}
	envelope.Successful = append(envelope.Successful, result.Outcome{Resource: resource, Action: "inspect", Retirement: details})
	if !parsed.Options.JSON {
		fmt.Fprintf(a.stdout, "Retirement record %s: integrity=%s events=%d last_sequence=%d recoverable_tail=%t recovery_required=%t\n", inspection.RecordID, inspection.IntegrityStatus, inspection.VerifiedEventCount, inspection.LastSequence, inspection.RecoverableTail, inspection.RecoveryRequired)
		fmt.Fprintf(a.stdout, "Subject commitments: thread=%s worker_binding=%s workspace=%s initial_worktree=%s physical_owner=%s created_by=%s\n", inspection.Subject.ThreadCommitment, inspection.Subject.WorkerBindingCommitment, inspection.Subject.WorkspaceCommitment, inspection.Subject.InitialWorktreeCommitment, inspection.Subject.PhysicalOwnerCommitment, inspection.Subject.CreatedBy)
		if inspection.LatestOperation != nil {
			fmt.Fprintf(a.stdout, "Latest operation: digest=%s scope=%s attachments=%d evidence=%d authority=%d\n", inspection.LatestOperation.OperationDigest, inspection.LatestOperation.Scope, len(inspection.LatestOperation.AttachmentCommitments), len(inspection.LatestOperation.EvidenceCommitments), len(inspection.LatestOperation.AuthorityCommitments))
		}
	}
	return &envelope, nil
}

func retirementDetails(inspection config.RetirementInspection, requestedID string) *result.RetirementDetails {
	recordID := inspection.RecordID
	if recordID == "" {
		recordID = requestedID
	}
	if inspection.SchemaVersion == 0 {
		inspection.SchemaVersion = config.RetirementSchemaVersion
	}
	details := &result.RetirementDetails{
		SchemaVersion:      inspection.SchemaVersion,
		RecordID:           recordID,
		VerifiedEventCount: inspection.VerifiedEventCount,
		LastSequence:       inspection.LastSequence,
		LastEventDigest:    inspection.LastEventDigest,
		IntegrityStatus:    inspection.IntegrityStatus,
		RecoverableTail:    inspection.RecoverableTail,
		RecoveryRequired:   inspection.RecoveryRequired,
		Subject: result.RetirementSubjectDetails{
			ThreadCommitment:          inspection.Subject.ThreadCommitment,
			WorkerBindingCommitment:   inspection.Subject.WorkerBindingCommitment,
			CreatedBy:                 inspection.Subject.CreatedBy,
			WorkspaceCommitment:       inspection.Subject.WorkspaceCommitment,
			InitialWorktreeCommitment: inspection.Subject.InitialWorktreeCommitment,
			PhysicalOwnerCommitment:   inspection.Subject.PhysicalOwnerCommitment,
		},
	}
	if inspection.LatestOperation != nil {
		details.LatestOperation = &result.RetirementOperationDetails{
			RecordID:              inspection.LatestOperation.RecordID,
			RecordCommitment:      inspection.LatestOperation.RecordCommitment,
			OperationID:           inspection.LatestOperation.OperationID,
			SubjectCommitment:     inspection.LatestOperation.SubjectCommitment,
			OperationDigest:       inspection.LatestOperation.OperationDigest,
			Scope:                 inspection.LatestOperation.Scope,
			AttachmentCommitments: inspection.LatestOperation.AttachmentCommitments,
			EvidenceCommitments:   inspection.LatestOperation.EvidenceCommitments,
			AuthorityCommitments:  inspection.LatestOperation.AuthorityCommitments,
			SupersedesOperationID: inspection.LatestOperation.SupersedesOperationID,
		}
	}
	return details
}

func privacySafeRetirementMessage(code string) string {
	switch code {
	case config.RetirementRecordNotFound:
		return "retirement record not found"
	case config.RetirementRecordRecovery:
		return "retirement record requires explicit recovery"
	case config.RetirementRecordLockBusy:
		return "retirement record lock is busy"
	case config.RetirementRecordCorrupt:
		return "retirement record failed integrity verification"
	default:
		return "retirement record inspection rejected"
	}
}
