package main

import "testing"

func TestRunnerDrainPreflightConfiguredLive(t *testing.T) {
	evidence := exactLiveRunnerDrainEvidence(true)
	got := classifyRunnerDrain(evidence)
	if got.Case != runnerDrainConfiguredLive || got.Disposition != runnerDrainStopExactKeepRow || got.StopTarget != evidence.Current || got.Blocker != "" {
		t.Fatalf("configured live decision = %+v", got)
	}

	for _, test := range []struct {
		name  string
		state runnerDrainEvidenceState
	}{
		{name: "live", state: runnerDrainEvidenceLive},
		{name: "conflicting", state: runnerDrainEvidenceConflicting},
		{name: "unreadable", state: runnerDrainEvidenceUnreadable},
		{name: "unproven", state: runnerDrainEvidenceUnproven},
	} {
		t.Run("catalog-"+test.name, func(t *testing.T) {
			candidate := evidence
			candidate.NativeCatalog = test.state
			got := classifyRunnerDrain(candidate)
			if got.Disposition != runnerDrainRetainRow || got.StopTarget.valid() || got.Blocker != runnerDrainEvidenceBlocker("native_catalog", test.state) {
				t.Fatalf("configured %s catalog decision = %+v", test.name, got)
			}
		})
	}

	evidence.Independence = runnerDrainEvidenceUnproven
	got = classifyRunnerDrain(evidence)
	if got.Disposition != runnerDrainRetainRow || got.StopTarget.valid() || got.Blocker != "executor_independence_unproven" {
		t.Fatalf("configured live without independent executor decision = %+v", got)
	}
}

func TestRunnerDrainPreflightConfiguredAbsent(t *testing.T) {
	evidence := absentRunnerDrainEvidence(true)
	got := classifyRunnerDrain(evidence)
	if got.Case != runnerDrainConfiguredAbsent || got.Disposition != runnerDrainRemoveProvenAbsentRow || got.StopTarget.valid() || got.Blocker != "" {
		t.Fatalf("configured absent decision = %+v", got)
	}

	evidence.NativeCatalog = runnerDrainEvidenceUnreadable
	got = classifyRunnerDrain(evidence)
	if got.Disposition != runnerDrainRetainRow || got.Blocker != "native_catalog_unreadable" {
		t.Fatalf("configured absence with unreadable catalog decision = %+v", got)
	}
}

func TestRunnerDrainPreflightRowAbsentLive(t *testing.T) {
	evidence := exactLiveRunnerDrainEvidence(false)
	got := classifyRunnerDrain(evidence)
	if got.Case != runnerDrainRowAbsentOrphan || got.Disposition != runnerDrainExternalOrphanOwnerStop || got.StopTarget.valid() || got.Blocker != "row_absent_immutable_prior_outcome_unproven" {
		t.Fatalf("unbound row-absent live decision = %+v", got)
	}

	evidence.Prior = exactRunnerDrainPrior(evidence.Current)
	got = classifyRunnerDrain(evidence)
	if got.Disposition != runnerDrainStopExactOrphan || got.StopTarget != evidence.Current || got.Blocker != "" {
		t.Fatalf("exact row-absent live decision = %+v", got)
	}

	evidence.Prior.Target.StartIdentity = "different-start"
	got = classifyRunnerDrain(evidence)
	if got.Disposition != runnerDrainExternalOrphanOwnerStop || got.StopTarget.valid() || got.Blocker != "row_absent_process_incarnation_changed" {
		t.Fatalf("changed row-absent live decision = %+v", got)
	}

	absent := absentRunnerDrainEvidence(false)
	got = classifyRunnerDrain(absent)
	if got.Disposition != runnerDrainRowAbsentVerified || got.StopTarget.valid() || got.Blocker != "" {
		t.Fatalf("row-absent owner-stop verification = %+v", got)
	}
}

func TestRunnerDrainPreflightConflicting(t *testing.T) {
	evidence := exactLiveRunnerDrainEvidence(true)
	evidence.Process = runnerDrainEvidenceConflicting
	got := classifyRunnerDrain(evidence)
	if got.Case != runnerDrainConflicting || got.Disposition != runnerDrainRetainRow || got.StopTarget.valid() || got.Blocker != "process_conflicting" {
		t.Fatalf("conflicting decision = %+v", got)
	}
}

func TestRunnerDrainPreflightInterruptedStop(t *testing.T) {
	evidence := exactLiveRunnerDrainEvidence(true)
	evidence.Attempt = runnerDrainAttemptInterrupted
	evidence.Prior = exactRunnerDrainPrior(evidence.Current)
	got := classifyRunnerDrain(evidence)
	if got.Case != runnerDrainInterruptedStop || got.Disposition != runnerDrainRetainRow || got.StopTarget.valid() || got.Blocker != "interrupted_stop_requires_proven_absence_or_exact_replay" {
		t.Fatalf("interrupted live decision = %+v", got)
	}

	absent := absentRunnerDrainEvidence(true)
	absent.Attempt = runnerDrainAttemptInterrupted
	absent.Prior = evidence.Prior
	absent.AbsenceTarget = evidence.Prior.Target
	got = classifyRunnerDrain(absent)
	if got.Case != runnerDrainInterruptedStop || got.Disposition != runnerDrainRemoveProvenAbsentRow || got.StopTarget.valid() || got.Blocker != "" {
		t.Fatalf("interrupted absence decision = %+v", got)
	}

	absent.AbsenceTarget.StartIdentity = "different-start"
	got = classifyRunnerDrain(absent)
	if got.Disposition != runnerDrainRetainRow || got.Blocker != "original_process_absence_unproven" {
		t.Fatalf("interrupted wrong-target absence decision = %+v", got)
	}
}

func TestRunnerDrainPreflightExactReplay(t *testing.T) {
	evidence := exactLiveRunnerDrainEvidence(true)
	evidence.Attempt = runnerDrainAttemptReplay
	evidence.Prior = exactRunnerDrainPrior(evidence.Current)
	got := classifyRunnerDrain(evidence)
	if got.Case != runnerDrainExactReplay || got.Disposition != runnerDrainStopExactKeepRow || got.StopTarget != evidence.Current || got.Blocker != "" {
		t.Fatalf("exact replay decision = %+v", got)
	}

	evidence.Current.StartIdentity = "reused-pid-start"
	got = classifyRunnerDrain(evidence)
	if got.Disposition != runnerDrainRetainRow || got.StopTarget.valid() || got.Blocker != "replay_process_incarnation_changed" {
		t.Fatalf("changed replay decision = %+v", got)
	}

	absent := absentRunnerDrainEvidence(true)
	absent.Attempt = runnerDrainAttemptReplay
	absent.Prior = exactRunnerDrainPrior(runnerDrainProcessIncarnation{PID: 4242, StartIdentity: "start-4242"})
	absent.AbsenceTarget = absent.Prior.Target
	got = classifyRunnerDrain(absent)
	if got.Disposition != runnerDrainRemoveProvenAbsentRow || got.StopTarget.valid() || got.Blocker != "" {
		t.Fatalf("already absent replay decision = %+v", got)
	}
}

func exactLiveRunnerDrainEvidence(rowPresent bool) runnerDrainEvidence {
	return runnerDrainEvidence{
		RowPresent:       rowPresent,
		CanonicalWorkdir: runnerDrainEvidenceExact,
		Pane:             runnerDrainEvidenceExact,
		Process:          runnerDrainEvidenceExact,
		Executable:       runnerDrainEvidenceExact,
		Argv:             runnerDrainEvidenceExact,
		NativeCatalog:    runnerDrainEvidenceExact,
		Independence:     runnerDrainEvidenceExact,
		Current:          runnerDrainProcessIncarnation{PID: 4242, StartIdentity: "start-4242"},
	}
}

func absentRunnerDrainEvidence(rowPresent bool) runnerDrainEvidence {
	return runnerDrainEvidence{
		RowPresent:       rowPresent,
		CanonicalWorkdir: runnerDrainEvidenceExact,
		Pane:             runnerDrainEvidenceAbsent,
		Process:          runnerDrainEvidenceAbsent,
		NativeCatalog:    runnerDrainEvidenceAbsent,
	}
}

func exactRunnerDrainPrior(target runnerDrainProcessIncarnation) runnerDrainPriorOutcome {
	return runnerDrainPriorOutcome{Evidence: runnerDrainEvidenceExact, Target: target}
}
