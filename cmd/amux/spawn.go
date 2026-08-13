package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

const maxSpawnPromptBytes = 1 << 20

var (
	spawnCreateThread    = createLocalAmpThread
	spawnStoreAssignment = config.StoreSpawnAssignment
	spawnPhysicalHost    = os.Hostname
)

func (a app) workerSpawn(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	s := in.Selectors
	if s.Workdir == "" || s.Workspace == "" || s.Window == "" || s.PromptFile == "" || s.AssignmentPhase == "" {
		return env, result.Request(errors.New("spawn requires --assignment-phase, --workdir, --workspace, --window, and --prompt-file <path|->"))
	}
	if len(in.Args) != 0 {
		return env, result.Request(errors.New("spawn accepts no positional prompt or identity arguments"))
	}
	mode := s.Mode
	if mode == "" {
		mode = "medium"
	}
	prompt, err := a.readSpawnPrompt(s.PromptFile)
	if err != nil {
		return env, result.Request(err)
	}
	workdir, err := config.CanonicalWorkdir(s.Workdir)
	if err != nil {
		return env, result.Preflight(err)
	}
	info, err := os.Stat(workdir)
	if err != nil || !info.IsDir() {
		return env, result.Preflight(fmt.Errorf("missing workdir: %s", workdir))
	}
	if !filepath.IsAbs(s.Workdir) || s.Workdir != workdir {
		return env, result.Preflight(fmt.Errorf("--workdir must be the canonical existing path %s", workdir))
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(prompt)))
	request := config.SpawnAssignmentRecord{
		Workspace: s.Workspace, Window: s.Window, Workdir: workdir, Thread: s.Thread,
		Mode: mode, Group: s.Group, PromptDigest: digest,
	}
	if err := validateSpawnPhaseRequest(s); err != nil {
		return env, result.Request(err)
	}
	if s.AssignmentPhase == "prepare" && (!s.OwnerAuthorizedProjectlessPhysicalHost || s.PhysicalHost == "") {
		return env, result.Preflight(fmt.Errorf("generalized amux spawn admission closed at %s; create ordinary work with authenticated native Amp thread creation on the exact intended Orb or live runner and workdir, without amux adoption; the sole exception requires --owner-authorized-projectless-physical-host and --physical-host <exact-local-host>", config.SpawnCutoverGeneration))
	}
	if s.NativeCapability != "existing-thread-message-v1" {
		failure := result.Preflight(errors.New("native existing-thread message capability must be confirmed before spawn mutation with --native-capability existing-thread-message-v1"))
		out := result.Outcome{Resource: result.CommandResource(), Action: "spawn-preflight", Message: "creation rejected before mutation because native assignment is unsupported", Assignment: spawnAssignmentDetails("rejected", "absent", "absent", "unsupported", digest, "")}
		out.Error = &result.Failure{Kind: result.ErrorPreflight, Message: failure.Error()}
		env.Failed = append(env.Failed, out)
		return env, failure
	}
	if s.AssignmentPhase == "prepare" {
		if s.Group != "" {
			return env, result.Preflight(errors.New("the projectless physical-host exception creates no Amux group or unrelated lifecycle state; omit --group"))
		}
		request.SchemaVersion = config.SpawnAssignmentProjectlessHostSchemaVersion
		request.Admission = config.SpawnAssignmentProjectlessHostAdmission
		request.PhysicalHost = s.PhysicalHost
	} else if s.OwnerAuthorizedProjectlessPhysicalHost != (s.PhysicalHost != "") {
		return env, result.Preflight(errors.New("--owner-authorized-projectless-physical-host and --physical-host must be supplied together"))
	}
	if !in.Options.DryRun {
		held, err := acquireMutationLock(in.Path)
		if err != nil {
			return env, result.Preflight(err)
		}
		defer held.Release()
	}

	switch s.AssignmentPhase {
	case "prepare":
		return a.prepareBoundedSpawn(in, dir, env, request)
	case "arm":
		return a.armExistingSpawn(in, dir, env, request)
	default:
		return a.finalizeExistingSpawn(in, dir, env, request)
	}
}

func validateSpawnPhaseRequest(s selectors) error {
	switch s.AssignmentPhase {
	case "prepare":
		if s.Thread != "" || s.AssignmentOutcome != "" || s.LatestCursor != "" {
			return errors.New("spawn prepare does not accept --thread, --assignment-outcome, or --latest-cursor")
		}
	case "arm":
		if s.Thread == "" || s.AssignmentOutcome != "" || s.LatestCursor != "" {
			return errors.New("spawn arm requires --thread and does not accept --assignment-outcome or --latest-cursor")
		}
	case "finalize":
		if s.Thread == "" || s.AssignmentOutcome == "" {
			return errors.New("spawn finalize requires --thread and --assignment-outcome")
		}
		if s.AssignmentOutcome == string(config.SpawnAssignmentAuthenticatedAccepted) && s.LatestCursor == "" {
			return errors.New("authenticated_accepted finalization requires --latest-cursor from native tool success")
		}
		if s.AssignmentOutcome != string(config.SpawnAssignmentAuthenticatedAccepted) && s.LatestCursor != "" {
			return errors.New("--latest-cursor is valid only with authenticated_accepted")
		}
	default:
		return errors.New("--assignment-phase must be prepare, arm, or finalize")
	}
	return nil
}

func (a app) prepareBoundedSpawn(in invocation, dir config.Directory, env *result.Envelope, request config.SpawnAssignmentRecord) (*result.Envelope, error) {
	if err := verifySpawnPhysicalHost(request.PhysicalHost); err != nil {
		return env, result.Preflight(err)
	}
	if err := preflightSpawnOwnership(dir, request.Workdir); err != nil {
		return env, result.Preflight(err)
	}
	records, err := config.LoadSpawnAssignments(dir.SpawnAssignmentsPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, existing := range records {
		if existing.Workspace == request.Workspace && existing.Window == request.Window {
			return env, result.Preflight(fmt.Errorf("spawn assignment %s/%s already exists at phase %s; creation and messaging will not be retried", request.Workspace, request.Window, existing.Phase))
		}
		if existing.Workdir == request.Workdir {
			return env, result.Preflight(fmt.Errorf("workdir %s is already bound to spawn assignment %s/%s at phase %s; the exception will not overlap, adopt, rebind, or replace it", request.Workdir, existing.Workspace, existing.Window, existing.Phase))
		}
	}
	if in.Options.DryRun {
		out := spawnAssignmentOutcome(request, "spawn-prepare", "would durably bind the owner-authorized projectless physical-host exception before one exact local create", "not_attempted", "")
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}

	request.Phase = config.SpawnAssignmentCreationArmed
	request.Outcome = config.SpawnAssignmentNotAttempted
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), request); err != nil {
		return env, result.Runtime(fmt.Errorf("durably bind exact physical host and workdir before thread creation: %w", err))
	}
	thread, createErr := spawnCreateThread(request.Workdir, request.Mode)
	thread = strings.TrimSpace(thread)
	exact, identityErr := config.CanonicalThreadID(thread)
	if createErr != nil || identityErr != nil {
		resource := result.CommandResource()
		identity := "no exact thread identity was returned"
		if identityErr == nil {
			resource.Thread = exact
			identity = "exact returned identity=" + exact
		}
		failure := result.Runtime(fmt.Errorf("projectless physical-host thread creation is indeterminate for %s/%s on host %s workdir %s (%s); the durable creation arm prohibits retry, fallback, reroute, rebind, adoption, search, cleanup, or alternate creation", request.Workspace, request.Window, request.PhysicalHost, request.Workdir, identity))
		out := spawnAssignmentOutcome(request, "spawn-prepare", failure.Error(), "not_attempted", "")
		out.Resource = resource
		out.Assignment.Creation = "indeterminate"
		out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
		env.Failed = append(env.Failed, out)
		return env, failure
	}
	request.Thread = exact
	request.Phase = config.SpawnAssignmentPrepared
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), request); err != nil {
		return env, result.Runtime(fmt.Errorf("exact thread %s was allocated on host %s workdir %s but prepare finalization failed; creation is indeterminate and must not be retried, rerouted, rebound, or adopted: %w", exact, request.PhysicalHost, request.Workdir, err))
	}
	out := spawnAssignmentOutcome(request, "spawn-prepare", "exact projectless physical-host thread created once; assignment not attempted; no Amux worker, group, pane, or adoption was created", "not_attempted", "")
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "SPAWN-PREPARED\tthread=%s\thost=%s\tworkdir=%s\tcreation=exact_thread_allocated\tlocal-ownership=absent\tlocal-presentation=absent\tassignment=not_attempted\texecution=unproven\tprompt-digest=%s\n", exact, request.PhysicalHost, request.Workdir, request.PromptDigest)
	}
	return env, nil
}

func (a app) armExistingSpawn(in invocation, dir config.Directory, env *result.Envelope, request config.SpawnAssignmentRecord) (*result.Envelope, error) {
	record, err := exactSpawnAssignment(dir.SpawnAssignmentsPath(), request)
	if err != nil {
		return env, result.Preflight(err)
	}
	if err := validateSpawnContinuationAdmission(record, in.Selectors); err != nil {
		return env, result.Preflight(err)
	}
	if record.Phase != config.SpawnAssignmentPrepared {
		return env, result.Preflight(fmt.Errorf("spawn assignment for exact thread %s is phase %s; native messaging will not be attempted", request.Thread, record.Phase))
	}
	if in.Options.DryRun {
		out := spawnAssignmentOutcome(record, "spawn-arm", "would arm the exact existing assignment without creating or adopting Amux lifecycle state", "indeterminate", "")
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}
	record.Phase = config.SpawnAssignmentArmed
	record.Outcome = config.SpawnAssignmentIndeterminate
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), record); err != nil {
		return env, result.Runtime(fmt.Errorf("arm native assignment for exact thread %s: %w", request.Thread, err))
	}
	out := spawnAssignmentOutcome(record, "spawn-arm", "native assignment armed for the exact existing thread; one coordinator message may now be attempted; interruption is indeterminate and never retryable", "indeterminate", "")
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "SPAWN-ARMED\tthread=%s\tcreation=exact_thread_allocated\tlocal-ownership=%s\tlocal-presentation=absent\tassignment=indeterminate\texecution=unproven\n", request.Thread, spawnLocalOwnership(record))
	}
	return env, nil
}

func (a app) finalizeExistingSpawn(in invocation, dir config.Directory, env *result.Envelope, request config.SpawnAssignmentRecord) (*result.Envelope, error) {
	record, err := exactSpawnAssignment(dir.SpawnAssignmentsPath(), request)
	if err != nil {
		return env, result.Preflight(err)
	}
	if err := validateSpawnContinuationAdmission(record, in.Selectors); err != nil {
		return env, result.Preflight(err)
	}
	if record.Phase != config.SpawnAssignmentArmed {
		return env, result.Preflight(fmt.Errorf("spawn assignment for exact thread %s is phase %s; finalization cannot change it", request.Thread, record.Phase))
	}
	outcome := config.SpawnAssignmentOutcome(in.Selectors.AssignmentOutcome)
	switch outcome {
	case config.SpawnAssignmentRejected, config.SpawnAssignmentIndeterminate, config.SpawnAssignmentAuthenticatedAccepted:
	default:
		return env, result.Request(errors.New("--assignment-outcome must be rejected, indeterminate, or authenticated_accepted"))
	}
	if in.Options.DryRun {
		out := spawnAssignmentOutcome(record, "spawn-finalize", "would finalize the exact armed assignment without resending or writing any Amux lifecycle store", string(outcome), spawnReceipt(outcome))
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}
	record.Phase = config.SpawnAssignmentFinalized
	record.Outcome = outcome
	record.ReceiptCursor = in.Selectors.LatestCursor
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), record); err != nil {
		failure := result.Runtime(fmt.Errorf("native result for exact thread %s could not be durably finalized; assignment remains indeterminate and must not be resent: %w", request.Thread, err))
		out := spawnAssignmentOutcome(record, "spawn-finalize", failure.Error(), "indeterminate", "")
		out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
		env.Failed = append(env.Failed, out)
		return env, failure
	}
	out := spawnAssignmentOutcome(record, "spawn-finalize", fmt.Sprintf("assignment=%s finalized for the exact existing thread without resend, adoption, presentation, or lifecycle dual-write; execution remains unproven", outcome), string(outcome), spawnReceipt(outcome))
	if outcome == config.SpawnAssignmentAuthenticatedAccepted {
		env.Successful = append(env.Successful, out)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "SPAWN-FINALIZED\tthread=%s\tcreation=exact_thread_allocated\tlocal-ownership=%s\tlocal-presentation=absent\tassignment=%s\texecution=unproven\treceipt=native_latest_cursor\n", request.Thread, spawnLocalOwnership(record), outcome)
		}
		return env, nil
	}
	failure := result.Runtime(fmt.Errorf("assignment=%s for exact thread %s; durable truth is preserved, execution remains unproven, and no message retry or fallback is permitted", outcome, request.Thread))
	out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
	env.Failed = append(env.Failed, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "SPAWN-FINALIZED\tthread=%s\tcreation=exact_thread_allocated\tlocal-ownership=%s\tlocal-presentation=absent\tassignment=%s\texecution=unproven\n", request.Thread, spawnLocalOwnership(record), outcome)
	}
	return env, failure
}

func exactSpawnAssignment(path string, request config.SpawnAssignmentRecord) (config.SpawnAssignmentRecord, error) {
	records, err := config.LoadSpawnAssignments(path)
	if err != nil {
		return config.SpawnAssignmentRecord{}, err
	}
	for _, record := range records {
		if record.Workspace != request.Workspace || record.Window != request.Window {
			continue
		}
		if record.Thread != request.Thread || record.Workdir != request.Workdir || record.Mode != request.Mode || record.Group != request.Group || record.PromptDigest != request.PromptDigest {
			return config.SpawnAssignmentRecord{}, errors.New("spawn assignment boundary does not exactly match the prepared thread, cwd, mode, group, and prompt digest")
		}
		return record, nil
	}
	return config.SpawnAssignmentRecord{}, fmt.Errorf("no prepared spawn assignment exists for %s/%s", request.Workspace, request.Window)
}

func validateSpawnContinuationAdmission(record config.SpawnAssignmentRecord, s selectors) error {
	switch record.SchemaVersion {
	case config.SpawnAssignmentSchemaVersion:
		if s.OwnerAuthorizedProjectlessPhysicalHost || s.PhysicalHost != "" {
			return errors.New("pre-cutover spawn drain must use its unchanged legacy boundary without post-cutover exception flags")
		}
		return nil
	case config.SpawnAssignmentProjectlessHostSchemaVersion:
		if !s.OwnerAuthorizedProjectlessPhysicalHost || s.PhysicalHost == "" {
			return errors.New("the projectless physical-host assignment requires renewed explicit owner authorization and its exact --physical-host for every transition")
		}
		if record.Admission != config.SpawnAssignmentProjectlessHostAdmission || record.PhysicalHost != s.PhysicalHost {
			return errors.New("projectless physical-host assignment admission or host binding does not exactly match; reroute and rebind are forbidden")
		}
		return verifySpawnPhysicalHost(s.PhysicalHost)
	default:
		return fmt.Errorf("spawn assignment schema version %d cannot prove pre-cutover admission or the bounded exception; transition fails closed", record.SchemaVersion)
	}
}

func verifySpawnPhysicalHost(requested string) error {
	actual, err := spawnPhysicalHost()
	if err != nil {
		return fmt.Errorf("determine exact local physical host: %w", err)
	}
	if actual == "" || requested != actual {
		return fmt.Errorf("exact physical host mismatch: requested %q, local %q; fallback, reroute, and rebind are forbidden", requested, actual)
	}
	return nil
}

func preflightSpawnOwnership(dir config.Directory, workdir string) error {
	rows, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return err
	}
	for _, existing := range rows {
		existingWorkdir, canonicalErr := config.CanonicalWorkdir(existing.Workdir)
		if canonicalErr != nil {
			return fmt.Errorf("existing worker %s/%s has unprovable workdir ownership: %w", existing.Workspace, existing.Window, canonicalErr)
		}
		if !filepath.IsAbs(existing.Workdir) || existing.Workdir != existingWorkdir {
			return fmt.Errorf("existing worker %s/%s has unprovable non-canonical workdir ownership %q; the exception fails closed", existing.Workspace, existing.Window, existing.Workdir)
		}
		if existingWorkdir == workdir {
			return fmt.Errorf("workdir %s is already owned by worker %s/%s; the exception will not adopt, rebind, or overlap it", workdir, existing.Workspace, existing.Window)
		}
	}
	runnerData, err := os.ReadFile(dir.RunnersPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, line := range strings.Split(string(runnerData), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 && len(fields) != 3 {
			continue // The authoritative loader below reports malformed rows.
		}
		storedWorkdir := fields[len(fields)-1]
		canonical, canonicalErr := config.CanonicalWorkdir(storedWorkdir)
		if canonicalErr == nil && (!filepath.IsAbs(storedWorkdir) || storedWorkdir != canonical) {
			return fmt.Errorf("existing runner %s has unprovable non-canonical workdir ownership %q; the exception fails closed", fields[0], storedWorkdir)
		}
	}
	runners, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return err
	}
	for _, existing := range runners {
		existingWorkdir, canonicalErr := config.CanonicalWorkdir(existing.Workdir)
		if canonicalErr != nil {
			return fmt.Errorf("existing runner %s has unprovable workdir ownership: %w", existing.Workspace, canonicalErr)
		}
		if !filepath.IsAbs(existing.Workdir) || existing.Workdir != existingWorkdir {
			return fmt.Errorf("existing runner %s has unprovable non-canonical workdir ownership %q; the exception fails closed", existing.Workspace, existing.Workdir)
		}
		if existingWorkdir == workdir {
			return fmt.Errorf("workdir %s is already owned by Amux Runner workspace %s; the exception will not reroute through or overlap it", workdir, existing.Workspace)
		}
	}
	return nil
}

func spawnAssignmentOutcome(record config.SpawnAssignmentRecord, action, message, assignment, receipt string) result.Outcome {
	resource := result.CommandResource()
	resource.Thread = record.Thread
	return result.Outcome{
		Resource: resource,
		Action:   action,
		Message:  message,
		Assignment: spawnAssignmentDetails(
			"exact_thread_allocated", spawnLocalOwnership(record), "absent", assignment, record.PromptDigest, receipt,
		),
	}
}

func spawnLocalOwnership(record config.SpawnAssignmentRecord) string {
	if record.SchemaVersion == config.SpawnAssignmentSchemaVersion {
		return "preserved"
	}
	return "absent"
}

func spawnReceipt(outcome config.SpawnAssignmentOutcome) string {
	if outcome == config.SpawnAssignmentAuthenticatedAccepted {
		return "native_latest_cursor"
	}
	return ""
}

func spawnAssignmentDetails(creation, ownership, presentation, assignment, digest, receipt string) *result.SpawnAssignmentDetails {
	return &result.SpawnAssignmentDetails{Creation: creation, LocalOwnership: ownership, LocalPresentation: presentation, Assignment: assignment, Execution: "unproven", PromptDigest: digest, Receipt: receipt}
}

func (a app) readSpawnPrompt(path string) (string, error) {
	var reader io.Reader
	if path == "-" {
		reader = a.stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("read --prompt-file: %w", err)
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxSpawnPromptBytes+1))
	if err != nil {
		return "", fmt.Errorf("read --prompt-file: %w", err)
	}
	if len(data) == 0 {
		return "", errors.New("spawn prompt must not be empty")
	}
	if len(data) > maxSpawnPromptBytes {
		return "", fmt.Errorf("spawn prompt exceeds %d bytes", maxSpawnPromptBytes)
	}
	return string(data), nil
}

func createLocalAmpThread(workdir, mode string) (string, error) {
	cmd := exec.Command("amp", "threads", "new", "--mode", mode)
	cmd.Dir = workdir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", err, message)
		}
		return strings.TrimSpace(string(out)), err
	}
	return strings.TrimSpace(string(out)), nil
}
