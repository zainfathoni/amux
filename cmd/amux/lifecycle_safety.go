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
	if len(panes) == 0 {
		return nil
	}
	currentPID := lifecycleCurrentPID()
	current, err := lifecycleProcessAncestry(currentPID)
	if err != nil {
		return lifecycleExecutorEvidenceError(command, err)
	}
	for _, pane := range panes {
		target, resolveErr := lifecyclePaneProcess(pane)
		if resolveErr != nil {
			return lifecycleExecutorEvidenceError(command, resolveErr)
		}
		targetAncestry, ancestryErr := lifecycleProcessAncestry(target.PID)
		if ancestryErr != nil {
			return lifecycleExecutorEvidenceError(command, ancestryErr)
		}
		targetProcess, targetIsAncestor := current[target.PID]
		currentProcess, targetIsDescendant := targetAncestry[currentPID]
		if targetIsAncestor && !sameLifecycleProcess(targetProcess, targetAncestry[target.PID]) {
			return lifecycleExecutorEvidenceError(command, errors.New("intersecting target process identity differs between ancestry snapshots"))
		}
		if targetIsDescendant && !sameLifecycleProcess(currentProcess, current[currentPID]) {
			return lifecycleExecutorEvidenceError(command, errors.New("intersecting current process identity differs between ancestry snapshots"))
		}
		if err := revalidateLifecycleAncestry(current); err != nil {
			return lifecycleExecutorEvidenceError(command, err)
		}
		if err := revalidateLifecycleAncestry(targetAncestry); err != nil {
			return lifecycleExecutorEvidenceError(command, err)
		}
		confirmed, confirmErr := lifecyclePaneProcess(target)
		if confirmErr != nil {
			return lifecycleExecutorEvidenceError(command, confirmErr)
		}
		if confirmed.PID != target.PID || confirmed.StartTime != target.StartTime || confirmed.PaneID != target.PaneID || confirmed.WindowID != target.WindowID {
			return lifecycleExecutorEvidenceError(command, errors.New("target pane process identity changed during preflight"))
		}
		if targetIsAncestor || targetIsDescendant {
			return lifecycleExecutorConflict(command)
		}
	}
	return nil
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
	if len(matches) != 1 || matches[0].Dead || matches[0].PID <= 0 || matches[0].StartTime <= 0 {
		return tmux.WindowPane{}, errors.New("target pane process identity is unavailable or changed")
	}
	return matches[0], nil
}

func lifecycleProcessAncestry(pid int) (map[int]tmux.ProcessMetadata, error) {
	if pid <= 0 {
		return nil, errors.New("process PID is unavailable")
	}
	ancestry := make(map[int]tmux.ProcessMetadata)
	for len(ancestry) < lifecycleAncestryLimit {
		if pid <= 1 {
			return ancestry, nil
		}
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
