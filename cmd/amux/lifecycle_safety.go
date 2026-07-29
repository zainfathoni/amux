package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/zainfathoni/amux/internal/tmux"
)

const lifecycleAncestryLimit = 256

var (
	lifecycleCurrentPID  = os.Getpid
	lifecycleProcessLink = func(pid int) (tmux.ProcessMetadata, error) {
		return tmux.InspectProcessLink(pid)
	}
	lifecyclePaneProcess = resolveLifecyclePaneProcess
)

func lifecycleCommandStopsWorker(name string) bool {
	switch name {
	case "shelve", "park", "restart", "remove", "teardown":
		return true
	}
	return false
}

func lifecycleCommandStopsRunner(name string) bool {
	switch name {
	case "park", "restart", "remove":
		return true
	}
	return false
}

func preflightLifecycleExecutor(command string, panes []tmux.WindowPane) error {
	_, err := preflightLifecycleExecutorEvidence(command, panes)
	return err
}

func preflightLifecycleExecutorEvidence(command string, panes []tmux.WindowPane) (map[string]tmux.ProcessMetadata, error) {
	if len(panes) == 0 {
		return map[string]tmux.ProcessMetadata{}, nil
	}
	currentPID := lifecycleCurrentPID()
	current, err := lifecycleProcessAncestry(currentPID)
	if err != nil {
		return nil, lifecycleExecutorEvidenceError(command, err)
	}
	type targetEvidence struct {
		pane     tmux.WindowPane
		ancestry map[int]tmux.ProcessMetadata
		conflict bool
	}
	targets := make([]targetEvidence, 0, len(panes))
	for _, pane := range panes {
		target, resolveErr := lifecyclePaneProcess(pane)
		if resolveErr != nil {
			return nil, lifecycleExecutorEvidenceError(command, resolveErr)
		}
		targetAncestry, ancestryErr := lifecycleProcessAncestry(target.PID)
		if ancestryErr != nil {
			return nil, lifecycleExecutorEvidenceError(command, ancestryErr)
		}
		targetProcess, targetInCurrentAncestry := current[target.PID]
		currentProcess, currentInTargetAncestry := targetAncestry[currentPID]
		targetIsAncestor := targetInCurrentAncestry || target.PID == 1
		targetIsDescendant := currentInTargetAncestry || currentPID == 1
		if targetInCurrentAncestry && !sameLifecycleProcess(targetProcess, targetAncestry[target.PID]) {
			return nil, lifecycleExecutorEvidenceError(command, errors.New("intersecting target process identity differs between ancestry snapshots"))
		}
		if currentInTargetAncestry && !sameLifecycleProcess(currentProcess, current[currentPID]) {
			return nil, lifecycleExecutorEvidenceError(command, errors.New("intersecting current process identity differs between ancestry snapshots"))
		}
		targets = append(targets, targetEvidence{pane: target, ancestry: targetAncestry, conflict: targetIsAncestor || targetIsDescendant})
	}
	conflict := false
	confirmedProcesses := make(map[string]tmux.ProcessMetadata, len(targets))
	for _, target := range targets {
		if err := revalidateLifecycleAncestry(target.ancestry); err != nil {
			return nil, lifecycleExecutorEvidenceError(command, err)
		}
		confirmed, confirmErr := lifecyclePaneProcess(target.pane)
		if confirmErr != nil {
			return nil, lifecycleExecutorEvidenceError(command, confirmErr)
		}
		if confirmed.PID != target.pane.PID || confirmed.PaneID != target.pane.PaneID || confirmed.WindowID != target.pane.WindowID {
			return nil, lifecycleExecutorEvidenceError(command, errors.New("target pane process identity changed during preflight"))
		}
		confirmedProcess, confirmErr := lifecycleProcessLink(confirmed.PID)
		if confirmErr != nil || !sameLifecycleProcess(confirmedProcess, target.ancestry[confirmed.PID]) {
			if confirmErr == nil {
				confirmErr = errors.New("target pane process incarnation changed during preflight")
			}
			return nil, lifecycleExecutorEvidenceError(command, confirmErr)
		}
		confirmedProcesses[lifecyclePaneProcessKey(confirmed)] = confirmedProcess
		conflict = conflict || target.conflict
	}
	if err := revalidateLifecycleAncestry(current); err != nil {
		return nil, lifecycleExecutorEvidenceError(command, err)
	}
	if conflict {
		return nil, lifecycleExecutorConflict(command)
	}
	return confirmedProcesses, nil
}

func lifecyclePaneProcessKey(pane tmux.WindowPane) string {
	return pane.WindowID + "\x00" + pane.PaneID
}

func sameLifecycleProcess(left, right tmux.ProcessMetadata) bool {
	return left.PID == right.PID && left.ParentPID == right.ParentPID && left.Identity != "" && left.Identity == right.Identity
}

func resolveLifecyclePaneProcess(before tmux.WindowPane) (tmux.WindowPane, error) {
	if before.Session == "" || before.Window == "" || before.WindowID == "" {
		return tmux.WindowPane{}, errors.New("target pane identity is incomplete")
	}
	panes, err := (tmux.Runner{}).RestartWindowPanes(before.Session, before.Window)
	if err != nil {
		return tmux.WindowPane{}, fmt.Errorf("reinspect target pane process: %w", err)
	}
	var matches []tmux.WindowPane
	for _, pane := range panes {
		if pane.WindowID == before.WindowID && (before.PaneID == "" || pane.PaneID == before.PaneID) {
			matches = append(matches, pane)
		}
	}
	if len(matches) != 1 || matches[0].Dead || matches[0].PID <= 0 {
		return tmux.WindowPane{}, errors.New("target pane process identity is unavailable or changed")
	}
	return matches[0], nil
}

func lifecycleProcessAncestry(pid int) (map[int]tmux.ProcessMetadata, error) {
	if pid <= 0 {
		return nil, errors.New("process PID is unavailable")
	}
	ancestry := make(map[int]tmux.ProcessMetadata)
	for steps := 0; steps < lifecycleAncestryLimit; steps++ {
		if _, seen := ancestry[pid]; seen {
			return nil, errors.New("process ancestry contains a cycle")
		}
		process, err := lifecycleProcessLink(pid)
		if err != nil {
			return nil, err
		}
		if process.PID != pid || process.ParentPID < 0 || process.Identity == "" {
			return nil, fmt.Errorf("process %d returned incomplete ancestry identity", pid)
		}
		ancestry[pid] = process
		if pid == 1 {
			if process.ParentPID != 0 {
				return nil, errors.New("PID 1 returned a nonzero parent PID")
			}
			return ancestry, nil
		}
		if process.ParentPID == 1 {
			if steps+2 > lifecycleAncestryLimit {
				return nil, errors.New("process ancestry exceeded safety limit")
			}
			return ancestry, nil
		}
		if process.ParentPID <= 0 {
			return nil, fmt.Errorf("process %d ancestry terminated before PID 1", pid)
		}
		pid = process.ParentPID
	}
	return nil, errors.New("process ancestry exceeded safety limit")
}

func revalidateLifecycleAncestry(before map[int]tmux.ProcessMetadata) error {
	for pid, expected := range before {
		actual, err := lifecycleProcessLink(pid)
		if err != nil {
			return err
		}
		if actual.PID != expected.PID || actual.ParentPID != expected.ParentPID || actual.Identity != expected.Identity {
			return fmt.Errorf("process %d ancestry identity changed during preflight", pid)
		}
	}
	return nil
}

func lifecycleExecutorConflict(command string) error {
	return fmt.Errorf("%s would stop or replace the Amp executor transport running this command; run the lifecycle action from a verified independent executor", command)
}

func lifecycleExecutorEvidenceError(command string, err error) error {
	return fmt.Errorf("cannot prove %s is independent of the Amp executor transport running this command: %w; run the lifecycle action from a verified independent executor", command, err)
}
