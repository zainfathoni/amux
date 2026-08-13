package main

// runnerDrainEvidenceState is the bounded result of independently inspecting
// one runner identity source. Exact means the source was revalidated against
// the same candidate incarnation; proven_absent is positive absence evidence,
// never missing or inaccessible evidence. The zero value is unproven.
type runnerDrainEvidenceState string

const (
	runnerDrainEvidenceUnproven    runnerDrainEvidenceState = ""
	runnerDrainEvidenceExact       runnerDrainEvidenceState = "exact"
	runnerDrainEvidenceAbsent      runnerDrainEvidenceState = "proven_absent"
	runnerDrainEvidenceLive        runnerDrainEvidenceState = "live_unbound"
	runnerDrainEvidenceConflicting runnerDrainEvidenceState = "conflicting"
	runnerDrainEvidenceUnreadable  runnerDrainEvidenceState = "unreadable"
)

type runnerDrainAttempt string

const (
	runnerDrainAttemptInitial     runnerDrainAttempt = ""
	runnerDrainAttemptInterrupted runnerDrainAttempt = "interrupted_stop"
	runnerDrainAttemptReplay      runnerDrainAttempt = "exact_replay"
)

type runnerDrainMigrationCase string

const (
	runnerDrainConfiguredLive   runnerDrainMigrationCase = "configured_live"
	runnerDrainConfiguredAbsent runnerDrainMigrationCase = "configured_absent"
	runnerDrainRowAbsentOrphan  runnerDrainMigrationCase = "row_absent_orphan"
	runnerDrainConflicting      runnerDrainMigrationCase = "conflicting"
	runnerDrainInterruptedStop  runnerDrainMigrationCase = "interrupted_stop"
	runnerDrainExactReplay      runnerDrainMigrationCase = "exact_replay"
)

// runnerDrainDisposition describes authority established by the preflight. It
// is not an executor: classification never stops a process or changes a row.
type runnerDrainDisposition string

const (
	runnerDrainRetainRow               runnerDrainDisposition = "retain_row"
	runnerDrainStopExactKeepRow        runnerDrainDisposition = "stop_exact_keep_row"
	runnerDrainRemoveProvenAbsentRow   runnerDrainDisposition = "remove_proven_absent_row"
	runnerDrainStopExactOrphan         runnerDrainDisposition = "stop_exact_orphan_without_adoption"
	runnerDrainExternalOrphanOwnerStop runnerDrainDisposition = "external_orphan_requires_native_or_os_owner_stop"
	runnerDrainRowAbsentVerified       runnerDrainDisposition = "row_absent_and_runtime_absence_verified"
)

type runnerDrainProcessIncarnation struct {
	PID           int
	StartIdentity string
}

func (p runnerDrainProcessIncarnation) valid() bool {
	return p.PID > 0 && p.StartIdentity != ""
}

type runnerDrainPriorOutcome struct {
	// Exact means an immutable prior Amux outcome independently binds Target.
	Evidence runnerDrainEvidenceState
	Target   runnerDrainProcessIncarnation
}

type runnerDrainEvidence struct {
	RowPresent bool
	Attempt    runnerDrainAttempt

	CanonicalWorkdir runnerDrainEvidenceState
	Pane             runnerDrainEvidenceState
	Process          runnerDrainEvidenceState
	Executable       runnerDrainEvidenceState
	Argv             runnerDrainEvidenceState
	NativeCatalog    runnerDrainEvidenceState
	Independence     runnerDrainEvidenceState

	Current       runnerDrainProcessIncarnation
	AbsenceTarget runnerDrainProcessIncarnation
	Prior         runnerDrainPriorOutcome
}

type runnerDrainDecision struct {
	Case        runnerDrainMigrationCase
	Disposition runnerDrainDisposition
	Blocker     string
	StopTarget  runnerDrainProcessIncarnation
}

func classifyRunnerDrain(e runnerDrainEvidence) runnerDrainDecision {
	caseName := runnerDrainCase(e)
	blocked := func(blocker string) runnerDrainDecision {
		disposition := runnerDrainRetainRow
		if !e.RowPresent {
			disposition = runnerDrainExternalOrphanOwnerStop
		}
		return runnerDrainDecision{Case: caseName, Disposition: disposition, Blocker: blocker}
	}

	if e.CanonicalWorkdir != runnerDrainEvidenceExact {
		return blocked(runnerDrainEvidenceBlocker("canonical_workdir", e.CanonicalWorkdir))
	}
	if e.Attempt != runnerDrainAttemptInitial && e.Attempt != runnerDrainAttemptInterrupted && e.Attempt != runnerDrainAttemptReplay {
		return blocked("attempt_invalid")
	}
	if e.Attempt == runnerDrainAttemptInterrupted || e.Attempt == runnerDrainAttemptReplay {
		if e.Prior.Evidence != runnerDrainEvidenceExact {
			return blocked(runnerDrainEvidenceBlocker("prior_outcome", e.Prior.Evidence))
		}
		if !e.Prior.Target.valid() {
			return blocked("prior_process_incarnation_incomplete")
		}
	}

	if runnerDrainAbsenceProven(e) {
		if (e.Attempt == runnerDrainAttemptInterrupted || e.Attempt == runnerDrainAttemptReplay) && e.AbsenceTarget != e.Prior.Target {
			return blocked("original_process_absence_unproven")
		}
		if e.RowPresent {
			return runnerDrainDecision{Case: caseName, Disposition: runnerDrainRemoveProvenAbsentRow}
		}
		return runnerDrainDecision{Case: caseName, Disposition: runnerDrainRowAbsentVerified}
	}

	if source, state, ok := runnerDrainFirstNonExactLiveEvidence(e); ok {
		return blocked(runnerDrainEvidenceBlocker(source, state))
	}
	if !e.Current.valid() {
		return blocked("current_process_incarnation_incomplete")
	}

	if e.Attempt == runnerDrainAttemptInterrupted {
		return blocked("interrupted_stop_requires_proven_absence_or_exact_replay")
	}
	if e.Attempt == runnerDrainAttemptReplay && e.Current != e.Prior.Target {
		return blocked("replay_process_incarnation_changed")
	}
	if !e.RowPresent {
		if e.Prior.Evidence != runnerDrainEvidenceExact || !e.Prior.Target.valid() {
			return blocked("row_absent_immutable_prior_outcome_unproven")
		}
		if e.Current != e.Prior.Target {
			return blocked("row_absent_process_incarnation_changed")
		}
		return runnerDrainDecision{Case: caseName, Disposition: runnerDrainStopExactOrphan, StopTarget: e.Current}
	}
	return runnerDrainDecision{Case: caseName, Disposition: runnerDrainStopExactKeepRow, StopTarget: e.Current}
}

func runnerDrainCase(e runnerDrainEvidence) runnerDrainMigrationCase {
	switch e.Attempt {
	case runnerDrainAttemptInterrupted:
		return runnerDrainInterruptedStop
	case runnerDrainAttemptReplay:
		return runnerDrainExactReplay
	}
	if !e.RowPresent {
		return runnerDrainRowAbsentOrphan
	}
	if runnerDrainAbsenceProven(e) {
		return runnerDrainConfiguredAbsent
	}
	if e.Pane == runnerDrainEvidenceConflicting || e.Process == runnerDrainEvidenceConflicting || e.NativeCatalog == runnerDrainEvidenceConflicting ||
		e.Pane == runnerDrainEvidenceLive || e.Process == runnerDrainEvidenceLive || e.NativeCatalog == runnerDrainEvidenceLive {
		return runnerDrainConflicting
	}
	return runnerDrainConfiguredLive
}

func runnerDrainAbsenceProven(e runnerDrainEvidence) bool {
	return e.Pane == runnerDrainEvidenceAbsent &&
		e.Process == runnerDrainEvidenceAbsent &&
		e.NativeCatalog == runnerDrainEvidenceAbsent
}

func runnerDrainFirstNonExactLiveEvidence(e runnerDrainEvidence) (string, runnerDrainEvidenceState, bool) {
	dynamic := []struct {
		name  string
		state runnerDrainEvidenceState
	}{
		{name: "pane", state: e.Pane},
		{name: "process", state: e.Process},
		{name: "native_catalog", state: e.NativeCatalog},
	}
	hasAbsent := false
	for _, evidence := range dynamic {
		hasAbsent = hasAbsent || evidence.state == runnerDrainEvidenceAbsent
	}
	if hasAbsent {
		for _, evidence := range dynamic {
			switch evidence.state {
			case runnerDrainEvidenceUnproven, runnerDrainEvidenceLive, runnerDrainEvidenceConflicting, runnerDrainEvidenceUnreadable:
				return evidence.name, evidence.state, true
			}
		}
	}
	for _, evidence := range []struct {
		name  string
		state runnerDrainEvidenceState
	}{
		dynamic[0],
		dynamic[1],
		{name: "executable", state: e.Executable},
		{name: "argv", state: e.Argv},
		dynamic[2],
		{name: "executor_independence", state: e.Independence},
	} {
		if evidence.state != runnerDrainEvidenceExact {
			return evidence.name, evidence.state, true
		}
	}
	return "", runnerDrainEvidenceUnproven, false
}

func runnerDrainEvidenceBlocker(source string, state runnerDrainEvidenceState) string {
	if state == runnerDrainEvidenceUnproven {
		return source + "_unproven"
	}
	return source + "_" + string(state)
}
