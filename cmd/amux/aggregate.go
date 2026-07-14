package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

func isAggregateLifecycle(path []string) bool {
	if len(path) != 1 {
		return false
	}
	switch path[0] {
	case "list", "launch", "park", "restart", "remove", "doctor", "reconcile":
		return true
	}
	return false
}

func isWorkspaceList(path []string) bool {
	return len(path) == 1 && path[0] == "workspaces" || len(path) == 2 && path[0] == "workspace" && path[1] == "list"
}

func (a app) executeAggregate(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	workerIn, runnerIn, useWorker, useRunner, err := aggregateInvocations(in, dir)
	if err != nil {
		return &env, result.Preflight(err)
	}
	if !useWorker && !useRunner {
		switch {
		case in.Command.Name == "list":
			return &env, nil
		case in.Selectors.All && in.Command.Name != "launch":
			env.Skipped = append(env.Skipped, result.Outcome{Resource: result.CommandResource(), Action: in.Command.Name, Message: "already in desired state"})
			return &env, nil
		default:
			return &env, result.Preflight(errors.New("no configured worker or runner matches the selector"))
		}
	}

	// Read-only commands need no mutation barrier. Mutating commands first run
	// both complete mode plans with dry-run semantics while the operation lock is
	// held, so neither mode can mutate before the other mode accepts its scope.
	var workerPlan *result.Envelope
	if in.Command.Mutating {
		preflightApp := a
		preflightApp.stdout = io.Discard
		for _, mode := range []struct {
			use    bool
			worker bool
			in     invocation
		}{{useRunner, false, runnerIn}, {useWorker, true, workerIn}} {
			if !mode.use {
				continue
			}
			planned := mode.in
			planned.Options.DryRun = true
			var plan *result.Envelope
			var preflightErr error
			if mode.worker {
				plan, preflightErr = preflightApp.executeWorker(planned, dir)
				workerPlan = plan
			} else {
				plan, preflightErr = preflightApp.executeRunner(planned, dir)
			}
			if preflightErr != nil {
				return &env, result.Preflight(errors.New(preflightErr.Error()))
			}
		}
	}

	if in.Options.DryRun {
		if useRunner {
			modeEnv, modeErr := a.executeRunner(runnerIn, dir)
			mergeEnvelope(&env, modeEnv)
			if modeErr != nil {
				return &env, modeErr
			}
		}
		if useWorker {
			modeEnv, modeErr := a.executeWorker(workerIn, dir)
			mergeEnvelope(&env, modeEnv)
			if modeErr != nil {
				return &env, modeErr
			}
		}
		if in.Options.AttachMode == attachAlways {
			env.Planned = append(env.Planned, result.Outcome{Resource: result.CommandResource(), Action: "attach", Message: "attach workspace " + in.Selectors.Workspace})
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "Would attach workspace %s after launch\n", in.Selectors.Workspace)
			}
		}
		return &env, nil
	}

	var aggregateErr error
	aggregateStarted := false
	if useRunner {
		modeEnv, modeErr := a.executeRunner(runnerIn, dir)
		mergeEnvelope(&env, modeEnv)
		aggregateStarted = len(modeEnv.Successful) > 0 || len(modeEnv.Failed) > 0
		aggregateErr = modeErr
	}
	// Runtime failures are resource-local, so workers still run. A new request
	// or preflight rejection means the selected plan is no longer valid.
	if useWorker && (aggregateErr == nil || result.ErrorKindOf(aggregateErr) == result.ErrorRuntime) {
		modeEnv, modeErr := a.executeWorker(workerIn, dir)
		mergeEnvelope(&env, modeEnv)
		if modeErr != nil && in.Command.Mutating && aggregateStarted && result.ErrorKindOf(modeErr) != result.ErrorRuntime {
			appendLatePreflightFailure(&env, workerPlan, modeErr)
			modeErr = result.Runtime(errors.New("worker precondition changed after aggregate mutation began: " + modeErr.Error()))
		}
		if modeErr != nil && (aggregateErr == nil || result.ErrorKindOf(modeErr) == result.ErrorRuntime) {
			aggregateErr = modeErr
		}
	}
	if aggregateErr != nil {
		return &env, aggregateErr
	}
	return &env, nil
}

func appendLatePreflightFailure(env *result.Envelope, plan *result.Envelope, err error) {
	message := "precondition changed after aggregate mutation began: " + err.Error()
	if plan != nil {
		for _, out := range plan.Planned {
			out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: message}
			env.Failed = append(env.Failed, out)
		}
		env.Skipped = append(env.Skipped, plan.Skipped...)
	}
	if len(env.Failed) == 0 {
		env.Failed = append(env.Failed, result.Outcome{
			Resource: result.CommandResource(),
			Action:   "aggregate",
			Error:    &result.Failure{Kind: result.ErrorRuntime, Message: message},
		})
	}
}

// attachAfterAggregateLaunch runs after dispatch returns so the mutation lock
// is released before an interactive tmux attachment can block.
func (a app) attachAfterAggregateLaunch(in invocation) error {
	if !isAggregateLifecycle(in.Path) || in.Command.Name != "launch" || in.Options.AttachMode != attachAlways || in.Options.DryRun {
		return nil
	}
	if err := (tmux.Runner{TerminalLauncher: in.Options.TerminalLauncher}).SelectAndAttach(in.Selectors.Workspace, false); err != nil {
		return result.Runtime(fmt.Errorf("attach workspace %s after launch: %w", in.Selectors.Workspace, err))
	}
	return nil
}

func aggregateInvocations(in invocation, dir config.Directory) (invocation, invocation, bool, bool, error) {
	workerIn, runnerIn := in, in
	if in.Selectors.Current {
		if aggregateCurrentIsWorker() {
			return workerIn, runnerIn, true, false, nil
		}
		return workerIn, runnerIn, false, true, nil
	}
	if in.Selectors.Thread != "" {
		return workerIn, runnerIn, true, false, nil
	}
	if in.Selectors.Workdir != "" {
		return workerIn, runnerIn, false, true, nil
	}
	workers, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return workerIn, runnerIn, false, false, err
	}
	runners, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return workerIn, runnerIn, false, false, err
	}
	useWorker := len(selectWorkerRows(workers, in.Selectors)) > 0
	useRunner := len(selectRunnerRows(runners, in.Selectors)) > 0
	if in.Command.Name == "remove" && in.Selectors.All && !useWorker {
		shelves, shelfErr := config.LoadShelvesReadOnly(dir.ShelvesPath())
		if shelfErr != nil {
			return workerIn, runnerIn, false, false, shelfErr
		}
		useWorker = len(shelves) > 0
	}
	return workerIn, runnerIn, useWorker, useRunner, nil
}

func aggregateCurrentIsWorker() bool {
	for _, name := range []string{"AMUX_WORKSPACE", "AMUX_SESSION", "AMUX_WINDOW", "AMUX_THREAD_ID", "AMUX_WORKDIR"} {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

func mergeEnvelope(target *result.Envelope, source *result.Envelope) {
	if source == nil {
		return
	}
	target.Planned = append(target.Planned, source.Planned...)
	target.Successful = append(target.Successful, source.Successful...)
	target.Skipped = append(target.Skipped, source.Skipped...)
	target.Failed = append(target.Failed, source.Failed...)
}

func (a app) executeWorkspaceList(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if in.Selectors.Mode != "" && in.Selectors.Mode != "worker" && in.Selectors.Mode != "runner" {
		return &env, result.Request(errors.New("--mode must be worker or runner"))
	}
	workspaces := map[string]bool{}
	if in.Selectors.Mode != "runner" {
		workers, err := config.LoadReadOnly(dir.WorkersPath())
		if err != nil {
			return &env, result.Preflight(err)
		}
		for _, row := range workers {
			workspaces[row.Workspace] = true
		}
	}
	if in.Selectors.Mode != "worker" {
		runners, err := config.LoadRunnersReadOnly(dir.RunnersPath())
		if err != nil {
			return &env, result.Preflight(err)
		}
		for _, row := range runners {
			workspaces[row.Workspace] = true
		}
	}
	names := make([]string, 0, len(workspaces))
	for name := range workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env.Successful = append(env.Successful, result.Outcome{Resource: result.WorkspaceResource(name), Action: "list"})
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, name)
		}
	}
	return &env, nil
}
