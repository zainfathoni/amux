package main

import (
	"fmt"
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

// executeAggregate preserves the top-level lifecycle aliases for the retained
// runner host. Legacy worker participation was removed; existing worker state
// is inert and is never read or mutated here.
func (a app) executeAggregate(in invocation, dir config.Directory) (*result.Envelope, error) {
	in.Path = []string{"runner", in.Command.Name}
	env, err := a.executeRunner(in, dir)
	if env != nil {
		env.Command = in.Command.Name
	}
	return env, err
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

func (a app) executeWorkspaceList(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	rows, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return &env, result.Preflight(err)
	}
	workspaces := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		workspaces[row.Workspace] = struct{}{}
	}
	names := make([]string, 0, len(workspaces))
	for name := range workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env.Successful = append(env.Successful, result.Outcome{
			Resource: result.WorkspaceResource(name),
			Action:   "list",
			Message:  "runner",
		})
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "%s\trunner\n", name)
		}
	}
	return &env, nil
}
