package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/result"
)

var mutationLockWait = 2 * time.Second

type cliOptions struct {
	ConfigDir        string
	JSON             bool
	DryRun           bool
	Help             bool
	Version          bool
	AttachMode       attachMode
	TerminalLauncher string
}

type selectors struct {
	Workspace      string
	Window         string
	Workdir        string
	Thread         string
	Group          string
	Groups         []string
	Mode           string
	TitlePrefix    string
	Current        bool
	All            bool
	Shelf          string
	IdempotencyKey string
	ReportID       string
	Pane           string
	Status         string
	Issue          string
	Reference      string
	PRURL          string
	Summary        string
	Message        string
	MessageFile    string
	MessageStdin   bool
	Reconcile      bool
}

type commandSpec struct {
	Name           string
	Summary        string
	Usage          string
	Flags          []string
	Children       []*commandSpec
	NeedsConfig    bool
	Mutating       bool
	FoundationOnly bool
}

type invocation struct {
	Options          cliOptions
	Command          *commandSpec
	Path             []string
	Selectors        selectors
	Args             []string
	MaintenanceOwner string
	Scheduled        bool
}

var rootCommand = &commandSpec{
	Name:    "amux",
	Summary: "Manage local Amp workers and runners",
	Usage:   "amux [global flags] <command> [flags]",
	Children: []*commandSpec{
		lifecycleCommand("list", "List configured workers and runners", false, "--workspace, -w <name>", "--thread, -t <id>", "--workdir, -d <path>", "--current", "--all"),
		lifecycleCommand("launch", "Launch configured workers and runners", true, "--workspace, -w <name>", "--thread, -t <id>", "--workdir, -d <path>", "--current", "--all"),
		lifecycleCommand("park", "Park running workers and runners", true, "--workspace, -w <name>", "--thread, -t <id>", "--workdir, -d <path>", "--current", "--all"),
		lifecycleCommand("restart", "Restart running workers and runners", true, "--workspace, -w <name>", "--thread, -t <id>", "--workdir, -d <path>", "--current", "--all"),
		lifecycleCommand("remove", "Remove worker or runner configuration", true, "--workspace, -w <name>", "--thread, -t <id>", "--workdir, -d <path>", "--current", "--all"),
		lifecycleCommand("doctor", "Diagnose worker and runner state", false, "--workspace, -w <name>", "--thread, -t <id>", "--workdir, -d <path>", "--current", "--all"),
		lifecycleCommand("reconcile", "Explicitly repair worker and runner drift", true, "--workspace, -w <name>", "--thread, -t <id>", "--workdir, -d <path>", "--current", "--all"),
		workerCommand(),
		runnerCommand(),
		workspaceCommand(),
		groupCommand(),
		callbackCommand(),
		reportCommand(),
		installCommand(),
		lifecycleCommand("workspaces", "Exact alias for workspace list", false, "--mode, -m <worker|runner>"),
		lifecycleCommand("spawn", "Spawn an interactive worker", true, "--workspace, -w <name>", "--window, -W <name>", "--workdir, -d <path>", "--mode, -m <mode>", "--title-prefix <prefix>  An exact #<number> prefix owns issue identity; window must be an issue-unprefixed semantic slug", "--group <id>  Repeat to attach the authoritative worker thread to multiple groups", "--message <text>", "--message-file <path>", "--message-stdin", "--idempotency-key <key>", "--reconcile  Recover a verified exact provisioned-thread timeout without resubmitting"),
		lifecycleCommand("shelve", "Shelve workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		lifecycleCommand("unshelve", "Unshelve workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		lifecycleCommand("teardown", "Teardown workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		{Name: "migrate-config", Summary: "Explicitly migrate legacy configuration", Usage: "amux migrate-config", NeedsConfig: true, Mutating: true, FoundationOnly: true},
		{Name: "completion", Summary: "Print shell completion", Usage: "amux completion <bash|zsh|fish>", FoundationOnly: true},
		{Name: "update", Summary: "Update the amux executable", Usage: "amux update", Mutating: true, FoundationOnly: true},
		{Name: "version", Summary: "Print version and build metadata", Usage: "amux version", FoundationOnly: true},
		{Name: "path", Summary: "Print the selected config directory", Usage: "amux path", NeedsConfig: true, FoundationOnly: true},
	},
}

var removedCommands = map[string]string{
	"store":          "use `amux worker pin`",
	"store-current":  "use `amux worker pin --current`",
	"pin":            "pin requires a resource namespace; use `amux worker pin` or `amux runner pin`",
	"pin-current":    "use `amux worker pin --current`",
	"unpin":          "unpin requires a resource namespace; use `amux worker unpin` or `amux runner unpin`",
	"unpin-current":  "use `amux worker unpin --current`",
	"remove-current": "use `amux worker remove --current`",
	"park-current":   "use `amux worker park --current`",
	"shelve-current": "use `amux shelve --current`",
	"shelved":        "use `amux worker list` with the shelved intent filter",
	"prune-archived": "use explicit worker reconciliation",
	"self-update":    "use `amux update`",
}

func lifecycleCommand(name, summary string, mutating bool, flags ...string) *commandSpec {
	return &commandSpec{
		Name:        name,
		Summary:     summary,
		Usage:       "amux " + name + " [selectors]",
		Flags:       flags,
		NeedsConfig: true,
		Mutating:    mutating,
	}
}

func workerCommand() *commandSpec {
	worker := &commandSpec{Name: "worker", Summary: "Manage interactive thread-bound clients", Usage: "amux worker <command>"}
	worker.Children = []*commandSpec{
		workerLeaf("list", "List configured workers", false, "--workspace, -w <name>", "--thread, -t <id>", "--shelf <shelved|unshelved>", "--current", "--all"),
		workerLeaf("pin", "Pin a worker without launching it", true, "--workspace, -w <name>", "--window, -W <name>", "--workdir, -d <path>", "--thread, -t <id>", "--current"),
		workerLeaf("unpin", "Unpin a worker without stopping it", true, "--thread, -t <id>", "--current"),
		workerLeaf("launch", "Launch workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("park", "Park workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("restart", "Restart workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("remove", "Remove workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("spawn", "Spawn a worker", true, "--workspace, -w <name>", "--window, -W <name>", "--workdir, -d <path>", "--mode, -m <mode>", "--title-prefix <prefix>  An exact #<number> prefix owns issue identity; window must be an issue-unprefixed semantic slug", "--group <id>  Repeat to attach the authoritative worker thread to multiple groups", "--message <text>", "--message-file <path>", "--message-stdin", "--idempotency-key <key>", "--reconcile  Recover a verified exact provisioned-thread timeout without resubmitting"),
		workerLeaf("shelve", "Shelve workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("unshelve", "Unshelve workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("teardown", "Teardown workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("doctor", "Diagnose workers", false, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
		workerLeaf("reconcile", "Reconcile workers", true, "--workspace, -w <name>", "--thread, -t <id>", "--current", "--all"),
	}
	return worker
}

func workerLeaf(name, summary string, mutating bool, flags ...string) *commandSpec {
	return &commandSpec{Name: name, Summary: summary, Usage: "amux worker " + name + " [selectors]", Flags: flags, NeedsConfig: true, Mutating: mutating}
}

func runnerCommand() *commandSpec {
	runner := &commandSpec{Name: "runner", Summary: "Manage non-interactive workdir-bound clients", Usage: "amux runner <command>"}
	runner.Children = []*commandSpec{
		maintenanceCommand(),
		runnerLeaf("list", "List configured runners", false, "--workspace, -w <name>", "--workdir, -d <path>", "--current", "--all"),
		runnerLeaf("pin", "Pin a runner without launching it", true, "--workspace, -w <name>", "--workdir, -d <path>", "--current"),
		runnerLeaf("unpin", "Unpin a runner without stopping it", true, "--workdir, -d <path>", "--current"),
		runnerLeaf("launch", "Launch runners", true, "--workspace, -w <name>", "--workdir, -d <path>", "--current", "--all"),
		runnerLeaf("park", "Park runners", true, "--workspace, -w <name>", "--workdir, -d <path>", "--current", "--all"),
		runnerLeaf("restart", "Restart runners", true, "--workspace, -w <name>", "--workdir, -d <path>", "--current", "--all"),
		runnerLeaf("remove", "Remove runners", true, "--workspace, -w <name>", "--workdir, -d <path>", "--current", "--all"),
		runnerLeaf("doctor", "Diagnose runners", false, "--workspace, -w <name>", "--workdir, -d <path>", "--current", "--all"),
		runnerLeaf("reconcile", "Reconcile runners", true, "--workspace, -w <name>", "--workdir, -d <path>", "--current", "--all"),
	}
	return runner
}

func maintenanceCommand() *commandSpec {
	return &commandSpec{Name: "maintenance", Summary: "Manage scheduled runner maintenance", Usage: "amux runner maintenance <command>", Children: []*commandSpec{
		{Name: "install", Summary: "Install scheduled maintenance", Usage: "amux runner maintenance install --update-owner <self|external>", Flags: []string{"--update-owner <self|external>"}, NeedsConfig: true, Mutating: true},
		{Name: "remove", Summary: "Remove scheduled maintenance", Usage: "amux runner maintenance remove", NeedsConfig: true, Mutating: true},
		{Name: "run", Summary: "Run maintenance", Usage: "amux runner maintenance run", Flags: []string{"--scheduled"}, NeedsConfig: true, Mutating: true},
	}}
}

func runnerLeaf(name, summary string, mutating bool, flags ...string) *commandSpec {
	return &commandSpec{Name: name, Summary: summary, Usage: "amux runner " + name + " [selectors]", Flags: flags, NeedsConfig: true, Mutating: mutating}
}

func workspaceCommand() *commandSpec {
	return &commandSpec{
		Name:    "workspace",
		Summary: "Inspect configured workspaces",
		Usage:   "amux workspace <command>",
		Children: []*commandSpec{
			{Name: "list", Summary: "List worker and runner workspaces", Usage: "amux workspace list", Flags: []string{"--mode, -m <worker|runner>"}, NeedsConfig: true},
		},
	}
}

func groupCommand() *commandSpec {
	group := &commandSpec{Name: "group", Summary: "Manage durable Amp thread groups", Usage: "amux group <command>"}
	group.Children = []*commandSpec{
		groupLeaf("declare", "Declare a group with its coordinator", true, "--group <id>", "--thread, -t <id>"),
		groupLeaf("add", "Add explicit local membership and ensure its Amp label", true, "--group <id>", "--thread, -t <id>"),
		groupLeaf("remove", "Remove local membership without removing its Amp label", true, "--group <id>", "--thread, -t <id>"),
		groupLeaf("coordinator", "Designate a group's coordinator", true, "--group <id>", "--thread, -t <id>"),
		groupLeaf("list", "List durable group memberships locally", false, "--group <id>", "--thread, -t <id>", "--all"),
		groupLeaf("show", "Show one durable group locally", false, "--group <id>"),
		groupLeaf("reconcile", "Add-only ensure member labels; skip coordinators", true, "--group <id>", "--thread, -t <id>", "--all"),
	}
	return group
}

func groupLeaf(name, summary string, mutating bool, flags ...string) *commandSpec {
	return &commandSpec{Name: name, Summary: summary, Usage: "amux group " + name + " [selectors]", Flags: flags, NeedsConfig: true, Mutating: mutating}
}

func reportCommand() *commandSpec {
	report := &commandSpec{Name: "report", Summary: "Manage durable worker reports and finish authorization", Usage: "amux report <command>"}
	report.Children = []*commandSpec{
		reportLeaf("submit", "Submit or progress a durable worker report", true, "--report-id <id>", "--group <id>", "--thread, -t <id>", "--status <ready|blocked|merged>", "--issue <value>", "--reference <value>", "--pr <url>", "--summary <text>"),
		reportLeaf("pending", "List unacknowledged reports locally", false, "--group <id>", "--thread, -t <id>", "--all"),
		reportLeaf("history", "Show durable history for one report", false, "--report-id <id>"),
		reportLeaf("acknowledge", "Acknowledge a report without authorizing finish", true, "--report-id <id>"),
		reportLeaf("authorize-finish", "Explicitly authorize finish for a ready report", true, "--report-id <id>", "--thread, -t <coordinator-id>", "--reference <value>"),
	}
	return report
}

func callbackCommand() *commandSpec {
	callback := &commandSpec{Name: "callback", Summary: "Manage ephemeral coordinator callback leases", Usage: "amux callback <command>"}
	callback.Children = []*commandSpec{
		callbackLeaf("register", "Register the exact live coordinator pane", true, "--group <id>", "--thread, -t <id>", "--pane <id>"),
		callbackLeaf("clear", "Invalidate the current coordinator callback lease", true, "--group <id>"),
	}
	return callback
}

func callbackLeaf(name, summary string, mutating bool, flags ...string) *commandSpec {
	return &commandSpec{Name: name, Summary: summary, Usage: "amux callback " + name + " [selectors]", Flags: flags, NeedsConfig: true, Mutating: mutating}
}

func reportLeaf(name, summary string, mutating bool, flags ...string) *commandSpec {
	return &commandSpec{Name: name, Summary: summary, Usage: "amux report " + name + " [selectors]", Flags: flags, NeedsConfig: true, Mutating: mutating}
}

func installCommand() *commandSpec {
	return &commandSpec{
		Name:    "install",
		Summary: "Inspect the amux client installation",
		Usage:   "amux install <command>",
		Children: []*commandSpec{
			{Name: "doctor", Summary: "Diagnose executable targets, versions, and PATH drift", Usage: "amux install doctor", FoundationOnly: true},
		},
	}
}

func (a app) execute(args []string) error {
	if a.stdin == nil {
		a.stdin = os.Stdin
	}
	if a.stdout == nil {
		a.stdout = io.Discard
	}
	if a.stderr == nil {
		a.stderr = io.Discard
	}

	wantsJSON := globalFlagRequested(args, "--json", "-j")
	parsed, err := parseInvocation(args)
	if err != nil {
		err = result.Request(err)
		return a.finishInvocation(invocation{Options: cliOptions{JSON: wantsJSON, DryRun: globalFlagRequested(args, "--dry-run", "-n")}, Path: guessedCommandPath(args)}, nil, err)
	}
	envelope, err := a.dispatch(parsed)
	if err == nil {
		err = a.attachAfterAggregateLaunch(parsed)
	}
	return a.finishInvocation(parsed, envelope, err)
}

func (a app) finishInvocation(parsed invocation, envelope *result.Envelope, err error) error {
	if !parsed.Options.JSON {
		return err
	}
	if envelope == nil {
		created := result.NewEnvelope(strings.Join(parsed.Path, " "), parsed.Options.DryRun)
		envelope = &created
	}
	if err != nil && len(envelope.Failed) == 0 {
		failure := &result.Failure{
			Kind:    result.ErrorKindOf(err),
			Message: err.Error(),
		}
		var busy *lock.BusyError
		if errors.As(err, &busy) {
			failure.Lock = busy
		}
		envelope.Failed = append(envelope.Failed, result.Outcome{
			Resource: result.CommandResource(),
			Action:   strings.Join(parsed.Path, " "),
			Error:    failure,
		})
	}
	if writeErr := envelope.Write(a.stdout); writeErr != nil {
		return result.Runtime(writeErr)
	}
	return err
}

func parseInvocation(args []string) (invocation, error) {
	var parsed invocation
	opts, words, err := parseCLIOptions(args)
	if err != nil {
		return parsed, err
	}
	parsed.Options = opts

	if opts.Version {
		if len(words) != 0 {
			return parsed, errors.New("--version does not accept a command")
		}
		words = []string{"version"}
	}
	if len(words) == 0 {
		if opts.Help {
			parsed.Command = rootCommand
			parsed.Path = []string{"amux"}
			return parsed, nil
		}
		words = []string{"worker", "launch"}
	}

	if words[0] == "help" {
		spec, path, remaining, err := resolveCommand(words[1:])
		if err != nil {
			return parsed, err
		}
		if len(remaining) != 0 {
			return parsed, fmt.Errorf("help path does not accept arguments: %s", strings.Join(remaining, " "))
		}
		parsed.Command = spec
		parsed.Path = path
		parsed.Options.Help = true
		return parsed, nil
	}

	if remediation, removed := removedCommands[words[0]]; removed {
		return parsed, fmt.Errorf("command %q was removed; %s", words[0], remediation)
	}
	spec, path, commandArgs, err := resolveCommand(words)
	if err != nil {
		return parsed, err
	}
	parsed.Command = spec
	parsed.Path = path
	if opts.Help || len(spec.Children) > 0 {
		parsed.Options.Help = true
		if len(commandArgs) != 0 {
			return parsed, fmt.Errorf("%s does not accept arguments", strings.Join(path, " "))
		}
		return parsed, nil
	}
	if isMaintenancePath(path) {
		commandArgs, parsed.MaintenanceOwner, parsed.Scheduled, err = parseMaintenanceFlags(commandArgs)
		if err != nil {
			return parsed, err
		}
		leaf := path[len(path)-1]
		if parsed.Scheduled && leaf != "run" {
			return parsed, errors.New("--scheduled is only valid for runner maintenance run")
		}
		if parsed.MaintenanceOwner != "" && leaf != "install" {
			return parsed, errors.New("--update-owner is only valid for runner maintenance install")
		}
	}
	parsed.Selectors, parsed.Args, err = parseSelectors(commandArgs)
	if err != nil {
		return parsed, err
	}
	if err := validateCommandSelectors(spec, &parsed.Selectors); err != nil {
		return parsed, err
	}
	if !spec.FoundationOnly && len(parsed.Args) != 0 {
		return parsed, fmt.Errorf("positional selectors were removed from %s; use named selectors shown by `amux help %s`", strings.Join(path, " "), strings.Join(path, " "))
	}
	if !spec.FoundationOnly && spec.Mutating && spec.Name != "launch" && !isMaintenancePath(path) && !hasResourceScope(parsed.Selectors) {
		return parsed, fmt.Errorf("%s requires a resource scope; use an explicit selector or --all", strings.Join(path, " "))
	}
	if isAggregateLifecycle(path) && parsed.Selectors.Thread != "" && parsed.Selectors.Workdir != "" {
		return parsed, errors.New("--thread and --workdir select different resource kinds and cannot be combined")
	}
	if opts.AttachMode == attachAlways {
		if strings.Join(path, " ") != "launch" || parsed.Selectors.Workspace == "" {
			return parsed, errors.New("--attach requires top-level amux launch with an explicit --workspace")
		}
		if opts.JSON {
			return parsed, errors.New("--attach cannot be combined with --json")
		}
	}
	if opts.TerminalLauncher != "" && opts.AttachMode != attachAlways {
		return parsed, errors.New("--terminal-launcher requires --attach")
	}
	return parsed, nil
}

func parseMaintenanceFlags(args []string) ([]string, string, bool, error) {
	remaining := make([]string, 0, len(args))
	owner := ""
	scheduled := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--scheduled":
			if scheduled {
				return nil, "", false, errors.New("duplicate --scheduled")
			}
			scheduled = true
		case "--update-owner":
			if owner != "" {
				return nil, "", false, errors.New("duplicate --update-owner")
			}
			i++
			if i >= len(args) {
				return nil, "", false, errors.New("--update-owner requires self or external")
			}
			owner = args[i]
		default:
			if strings.HasPrefix(args[i], "--update-owner=") {
				if owner != "" {
					return nil, "", false, errors.New("duplicate --update-owner")
				}
				owner = strings.TrimPrefix(args[i], "--update-owner=")
			} else {
				remaining = append(remaining, args[i])
			}
		}
	}
	return remaining, owner, scheduled, nil
}

func isMaintenancePath(path []string) bool {
	return len(path) == 3 && path[0] == "runner" && path[1] == "maintenance"
}

func parseCLIOptions(args []string) (cliOptions, []string, error) {
	var opts cliOptions
	words := make([]string, 0, len(args))
	terminated := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if terminated {
			words = append(words, arg)
			continue
		}
		if arg == "--" {
			terminated = true
			words = append(words, arg)
			continue
		}
		if commandOptionRequiresValue(arg) {
			words = append(words, arg)
			if i+1 < len(args) {
				i++
				words = append(words, args[i])
			}
			continue
		}
		switch arg {
		case "--json", "-j":
			opts.JSON = true
		case "--dry-run", "-n":
			opts.DryRun = true
		case "--help", "-h":
			opts.Help = true
		case "--version":
			opts.Version = true
		case "--attach":
			if opts.AttachMode == attachNever {
				return opts, nil, errors.New("--attach and --no-attach are mutually exclusive")
			}
			opts.AttachMode = attachAlways
		case "--no-attach":
			if opts.AttachMode == attachAlways {
				return opts, nil, errors.New("--attach and --no-attach are mutually exclusive")
			}
			opts.AttachMode = attachNever
		case "--terminal-launcher":
			i++
			if i >= len(args) || args[i] == "" {
				return opts, nil, errors.New("--terminal-launcher requires a command")
			}
			opts.TerminalLauncher = args[i]
		case "--config":
			return opts, nil, errors.New("--config was removed; select a directory with --config-dir or -c")
		case "--config-dir", "-c":
			if opts.ConfigDir != "" {
				return opts, nil, errors.New("config directory may be selected only once")
			}
			i++
			if i >= len(args) || args[i] == "" {
				return opts, nil, fmt.Errorf("%s requires a directory", arg)
			}
			opts.ConfigDir = args[i]
		default:
			switch {
			case strings.HasPrefix(arg, "--config="):
				return opts, nil, errors.New("--config was removed; select a directory with --config-dir or -c")
			case strings.HasPrefix(arg, "--config-dir="):
				if opts.ConfigDir != "" {
					return opts, nil, errors.New("config directory may be selected only once")
				}
				opts.ConfigDir = strings.TrimPrefix(arg, "--config-dir=")
			case strings.HasPrefix(arg, "-c="):
				if opts.ConfigDir != "" {
					return opts, nil, errors.New("config directory may be selected only once")
				}
				opts.ConfigDir = strings.TrimPrefix(arg, "-c=")
			case strings.HasPrefix(arg, "--terminal-launcher="):
				opts.TerminalLauncher = strings.TrimPrefix(arg, "--terminal-launcher=")
				if opts.TerminalLauncher == "" {
					return opts, nil, errors.New("--terminal-launcher requires a command")
				}
			default:
				words = append(words, arg)
			}
			if (strings.HasPrefix(arg, "--config-dir=") || strings.HasPrefix(arg, "-c=")) && opts.ConfigDir == "" {
				return opts, nil, errors.New("--config-dir requires a directory")
			}
		}
	}
	return opts, words, nil
}

func commandOptionRequiresValue(arg string) bool {
	name, _, hasInline := splitFlag(arg)
	if hasInline {
		return false
	}
	switch name {
	case "--workspace", "-w", "--window", "-W", "--workdir", "-d", "--thread", "-t",
		"--group", "--mode", "-m", "--title-prefix", "--shelf", "--idempotency-key",
		"--report-id", "--pane", "--status", "--issue", "--reference", "--pr", "--summary",
		"--message", "--message-file", "--update-owner":
		return true
	default:
		return false
	}
}

func resolveCommand(words []string) (*commandSpec, []string, []string, error) {
	if len(words) == 0 {
		return rootCommand, []string{"amux"}, nil, nil
	}
	root := commandChild(rootCommand, words[0])
	if root == nil {
		return nil, nil, nil, fmt.Errorf("unknown command %q; run `amux help`", words[0])
	}
	path := []string{root.Name}
	remaining := words[1:]
	if len(root.Children) == 0 {
		return root, path, remaining, nil
	}
	if len(remaining) == 0 || strings.HasPrefix(remaining[0], "-") {
		return root, path, remaining, nil
	}
	current := root
	for len(current.Children) > 0 && len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
		child := commandChild(current, remaining[0])
		if child == nil {
			return nil, nil, nil, fmt.Errorf("unknown %s command %q; run `amux help %s`", current.Name, remaining[0], strings.Join(path, " "))
		}
		current = child
		path = append(path, child.Name)
		remaining = remaining[1:]
	}
	return current, path, remaining, nil
}

func commandChild(parent *commandSpec, name string) *commandSpec {
	for _, child := range parent.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func parseSelectors(args []string) (selectors, []string, error) {
	var parsed selectors
	remaining := make([]string, 0, len(args))
	terminated := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if terminated {
			remaining = append(remaining, arg)
			continue
		}
		if arg == "--" {
			terminated = true
			continue
		}
		name, inline, hasInline := splitFlag(arg)
		switch name {
		case "--current":
			if hasInline {
				return parsed, nil, errors.New("--current does not accept a value")
			}
			parsed.Current = true
		case "--all":
			if hasInline {
				return parsed, nil, errors.New("--all does not accept a value")
			}
			parsed.All = true
		case "--workspace", "-w":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.Workspace, value, "--workspace"); err != nil {
				return parsed, nil, err
			}
		case "--window", "-W":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.Window, value, "--window"); err != nil {
				return parsed, nil, err
			}
		case "--workdir", "-d":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.Workdir, value, "--workdir"); err != nil {
				return parsed, nil, err
			}
		case "--thread", "-t":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.Thread, value, "--thread"); err != nil {
				return parsed, nil, err
			}
		case "--group":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if parsed.Group == "" {
				parsed.Group = value
			}
			parsed.Groups = append(parsed.Groups, value)
		case "--mode", "-m":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.Mode, value, "--mode"); err != nil {
				return parsed, nil, err
			}
		case "--title-prefix":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.TitlePrefix, value, "--title-prefix"); err != nil {
				return parsed, nil, err
			}
		case "--shelf":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.Shelf, value, "--shelf"); err != nil {
				return parsed, nil, err
			}
		case "--idempotency-key":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			if err := setSelector(&parsed.IdempotencyKey, value, "--idempotency-key"); err != nil {
				return parsed, nil, err
			}
		case "--report-id", "--pane", "--status", "--issue", "--reference", "--pr", "--summary":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			target := map[string]*string{
				"--report-id": &parsed.ReportID,
				"--pane":      &parsed.Pane,
				"--status":    &parsed.Status,
				"--issue":     &parsed.Issue,
				"--reference": &parsed.Reference,
				"--pr":        &parsed.PRURL,
				"--summary":   &parsed.Summary,
			}[name]
			if err := setSelector(target, value, name); err != nil {
				return parsed, nil, err
			}
		case "--message", "--message-file":
			value, next, err := selectorValue(args, i, name, inline, hasInline)
			if err != nil {
				return parsed, nil, err
			}
			i = next
			target := &parsed.Message
			if name == "--message-file" {
				target = &parsed.MessageFile
			}
			if err := setSelector(target, value, name); err != nil {
				return parsed, nil, err
			}
		case "--message-stdin":
			if hasInline {
				return parsed, nil, errors.New("--message-stdin does not accept a value")
			}
			if parsed.MessageStdin {
				return parsed, nil, errors.New("--message-stdin may be specified only once")
			}
			parsed.MessageStdin = true
		case "--reconcile":
			if hasInline {
				return parsed, nil, errors.New("--reconcile does not accept a value")
			}
			if parsed.Reconcile {
				return parsed, nil, errors.New("--reconcile may be specified only once")
			}
			parsed.Reconcile = true
		default:
			if strings.HasPrefix(arg, "-") {
				return parsed, nil, fmt.Errorf("unknown option %s", arg)
			}
			remaining = append(remaining, arg)
		}
	}
	if parsed.All && (parsed.Current || parsed.Workspace != "" || parsed.Window != "" || parsed.Workdir != "" || parsed.Thread != "" || len(parsed.Groups) != 0) {
		return parsed, nil, errors.New("--all cannot be combined with --current or resource selectors")
	}
	if parsed.Current && (parsed.Workspace != "" || parsed.Window != "" || parsed.Workdir != "" || parsed.Thread != "" || len(parsed.Groups) != 0) {
		return parsed, nil, errors.New("--current cannot be combined with resource selectors")
	}
	return parsed, remaining, nil
}

func splitFlag(arg string) (name, value string, hasValue bool) {
	if index := strings.IndexByte(arg, '='); index >= 0 {
		return arg[:index], arg[index+1:], true
	}
	return arg, "", false
}

func selectorValue(args []string, index int, name, inline string, hasInline bool) (string, int, error) {
	if hasInline {
		if inline == "" {
			return "", index, fmt.Errorf("%s requires a value", name)
		}
		return inline, index, nil
	}
	next := index + 1
	if next >= len(args) || args[next] == "" {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[next], next, nil
}

func setSelector(target *string, value, name string) error {
	if *target != "" {
		return fmt.Errorf("%s may be specified only once", name)
	}
	*target = value
	return nil
}

func validateCommandSelectors(command *commandSpec, parsed *selectors) error {
	tests := []struct {
		name  string
		value string
	}{
		{"--workspace", parsed.Workspace},
		{"--window", parsed.Window},
		{"--workdir", parsed.Workdir},
		{"--thread", parsed.Thread},
		{"--group", parsed.Group},
		{"--mode", parsed.Mode},
		{"--title-prefix", parsed.TitlePrefix},
		{"--shelf", parsed.Shelf},
		{"--idempotency-key", parsed.IdempotencyKey},
		{"--report-id", parsed.ReportID},
		{"--pane", parsed.Pane},
		{"--status", parsed.Status},
		{"--issue", parsed.Issue},
		{"--reference", parsed.Reference},
		{"--pr", parsed.PRURL},
		{"--summary", parsed.Summary},
		{"--message", parsed.Message},
		{"--message-file", parsed.MessageFile},
	}
	for _, test := range tests {
		if test.value != "" && !commandAcceptsFlag(command, test.name) {
			return fmt.Errorf("%s does not accept %s; run `amux help %s`", command.UsageName(), test.name, command.UsageName())
		}
	}
	if parsed.Current && !commandAcceptsFlag(command, "--current") {
		return fmt.Errorf("%s does not accept --current; run `amux help %s`", command.UsageName(), command.UsageName())
	}
	if parsed.All && !commandAcceptsFlag(command, "--all") {
		return fmt.Errorf("%s does not accept --all; run `amux help %s`", command.UsageName(), command.UsageName())
	}
	if parsed.Workspace != "" {
		if err := config.ValidateField("workspace", parsed.Workspace); err != nil {
			return err
		}
	}
	if parsed.Window != "" {
		if err := config.ValidateField("window", parsed.Window); err != nil {
			return err
		}
	}
	if parsed.Workdir != "" {
		workdir, err := config.CanonicalWorkdir(parsed.Workdir)
		if err != nil {
			return err
		}
		parsed.Workdir = workdir
	}
	if parsed.Thread != "" {
		thread, err := config.CanonicalThreadID(parsed.Thread)
		if err != nil {
			return err
		}
		parsed.Thread = thread
	}
	if parsed.Group != "" {
		for _, group := range parsed.Groups {
			if err := config.ValidateGroupID(group); err != nil {
				return err
			}
		}
		if command.Name == "spawn" {
			sort.Strings(parsed.Groups)
			parsed.Groups = compactStrings(parsed.Groups)
			parsed.Group = ""
		} else if len(parsed.Groups) != 1 {
			return errors.New("--group may be repeated only for worker spawn")
		}
	}
	if parsed.Mode != "" {
		if err := config.ValidateField("mode", parsed.Mode); err != nil {
			return err
		}
	}
	if parsed.TitlePrefix != "" {
		if err := config.ValidateField("title-prefix", parsed.TitlePrefix); err != nil {
			return err
		}
		if strings.TrimSpace(parsed.TitlePrefix) == "" {
			return errors.New("title-prefix must not be blank")
		}
	}
	if parsed.Shelf != "" && parsed.Shelf != "shelved" && parsed.Shelf != "unshelved" {
		return errors.New("--shelf must be shelved or unshelved")
	}
	if parsed.IdempotencyKey != "" {
		if err := config.ValidateField("idempotency key", parsed.IdempotencyKey); err != nil {
			return err
		}
	}
	if parsed.MessageStdin && !commandAcceptsFlag(command, "--message-stdin") {
		return fmt.Errorf("%s does not accept --message-stdin; run `amux help %s`", command.UsageName(), command.UsageName())
	}
	if parsed.Reconcile && !commandAcceptsFlag(command, "--reconcile") {
		return fmt.Errorf("%s does not accept --reconcile; run `amux help %s`", command.UsageName(), command.UsageName())
	}
	messageInputs := 0
	if parsed.Message != "" {
		messageInputs++
	}
	if parsed.MessageFile != "" {
		messageInputs++
	}
	if parsed.MessageStdin {
		messageInputs++
	}
	if messageInputs > 1 {
		return errors.New("--message, --message-file, and --message-stdin are mutually exclusive")
	}
	return nil
}

func commandAcceptsFlag(command *commandSpec, name string) bool {
	for _, flag := range command.Flags {
		if strings.HasPrefix(flag, name) {
			return true
		}
	}
	return false
}

func (c *commandSpec) UsageName() string {
	return strings.TrimSuffix(strings.TrimPrefix(c.Usage, "amux "), " [selectors]")
}

func hasResourceScope(parsed selectors) bool {
	return parsed.All || parsed.Current || parsed.Workspace != "" || parsed.Window != "" || parsed.Workdir != "" || parsed.Thread != "" || len(parsed.Groups) != 0 || parsed.ReportID != ""
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	compacted := values[:1]
	for _, value := range values[1:] {
		if value != compacted[len(compacted)-1] {
			compacted = append(compacted, value)
		}
	}
	return compacted
}

func selectorsEmpty(parsed selectors) bool {
	return parsed.Workspace == "" && parsed.Window == "" && parsed.Workdir == "" && parsed.Thread == "" && parsed.Group == "" && len(parsed.Groups) == 0 && parsed.Mode == "" && parsed.TitlePrefix == "" && !parsed.Current && !parsed.All && parsed.Shelf == "" && parsed.IdempotencyKey == "" && parsed.ReportID == "" && parsed.Pane == "" && parsed.Status == "" && parsed.Issue == "" && parsed.Reference == "" && parsed.PRURL == "" && parsed.Summary == "" && parsed.Message == "" && parsed.MessageFile == "" && !parsed.MessageStdin && !parsed.Reconcile
}

func isGroupPath(path []string) bool {
	return len(path) == 2 && path[0] == "group"
}

func isReportPath(path []string) bool {
	return len(path) == 2 && path[0] == "report"
}

func isCallbackPath(path []string) bool {
	return len(path) == 2 && path[0] == "callback"
}

func (a app) dispatch(parsed invocation) (*result.Envelope, error) {
	if parsed.Options.Help {
		if parsed.Options.JSON {
			return nil, result.Request(errors.New("--json is not supported with help"))
		}
		a.printCommandHelp(parsed.Command)
		return nil, nil
	}
	if parsed.Options.JSON && parsed.Command.FoundationOnly && parsed.Command.Name != "migrate-config" && strings.Join(parsed.Path, " ") != "install doctor" {
		return nil, result.Request(fmt.Errorf("--json is not supported with %s", strings.Join(parsed.Path, " ")))
	}

	var dir config.Directory
	var err error
	if parsed.Command.NeedsConfig {
		dir, err = resolveInvocationDirectory(parsed.Options.ConfigDir)
		if err != nil {
			return nil, result.Request(err)
		}
	}

	if parsed.Command.NeedsConfig && parsed.Command.Name != "migrate-config" && parsed.Command.Name != "path" {
		required, err := config.MigrationRequired(dir)
		if err != nil {
			return nil, result.Preflight(err)
		}
		if required {
			return nil, result.Preflight(fmt.Errorf("configuration migration required for %s; run `amux migrate-config --config-dir %s`", dir.Path, dir.Path))
		}
	}
	if isMaintenancePath(parsed.Path) {
		if parsed.Command.Name == "run" && parsed.Scheduled && maintenanceGOOS == "darwin" && !parsed.Options.DryRun {
			maintenanceSleep(time.Duration(maintenanceRandom(int64(maintenanceJitter))))
		}
		var held *lock.Lock
		if parsed.Command.Mutating {
			held, err = acquireMutationLock(parsed.Path)
			if err != nil {
				return nil, result.Preflight(err)
			}
			defer held.Release()
		}
		return a.executeMaintenance(parsed, dir)
	}

	if !parsed.Command.FoundationOnly && !isAggregateLifecycle(parsed.Path) && !isWorkspaceList(parsed.Path) && !isGroupPath(parsed.Path) && !isReportPath(parsed.Path) && !isCallbackPath(parsed.Path) && (len(parsed.Path) != 2 || parsed.Path[0] != "worker" && parsed.Path[0] != "runner") && !isWorkerConvenience(parsed.Path) {
		return nil, result.Preflight(fmt.Errorf("%s is reserved for its lifecycle implementation phase and is not available in the CLI foundations", strings.Join(parsed.Path, " ")))
	}

	var held *lock.Lock
	if parsed.Command.Mutating {
		held, err = acquireMutationLock(parsed.Path)
		if err != nil {
			return nil, result.Preflight(err)
		}
		defer held.Release()
	}
	if isGroupPath(parsed.Path) {
		return a.executeGroup(parsed, dir)
	}
	if isReportPath(parsed.Path) {
		return a.executeReport(parsed, dir)
	}
	if isCallbackPath(parsed.Path) {
		return a.executeCallback(parsed, dir)
	}

	switch parsed.Command.Name {
	case "list", "pin", "unpin", "launch", "park", "restart", "remove", "spawn", "shelve", "unshelve", "teardown", "doctor", "reconcile":
		if strings.Join(parsed.Path, " ") == "install doctor" {
			if len(parsed.Args) != 0 || !selectorsEmpty(parsed.Selectors) {
				return nil, result.Request(errors.New("usage: amux install doctor"))
			}
			return a.installDoctor(parsed)
		}
		if !parsed.Command.FoundationOnly {
			if isWorkspaceList(parsed.Path) {
				return a.executeWorkspaceList(parsed, dir)
			}
			if isAggregateLifecycle(parsed.Path) {
				return a.executeAggregate(parsed, dir)
			}
			if len(parsed.Path) == 2 && parsed.Path[0] == "runner" {
				return a.executeRunner(parsed, dir)
			}
			return a.executeWorker(parsed, dir)
		}
		return nil, result.Preflight(fmt.Errorf("%s is reserved for its lifecycle implementation phase", strings.Join(parsed.Path, " ")))
	case "workspaces":
		return a.executeWorkspaceList(parsed, dir)
	case "migrate-config":
		if len(parsed.Args) != 0 || !selectorsEmpty(parsed.Selectors) {
			return nil, result.Request(errors.New("usage: amux migrate-config"))
		}
		return a.executeMigration(parsed, dir)
	case "path":
		if len(parsed.Args) != 0 || !selectorsEmpty(parsed.Selectors) {
			return nil, result.Request(errors.New("usage: amux path"))
		}
		fmt.Fprintln(a.stdout, dir.Path)
		return nil, nil
	case "version":
		if len(parsed.Args) != 0 || !selectorsEmpty(parsed.Selectors) {
			return nil, result.Request(errors.New("usage: amux version"))
		}
		fmt.Fprintln(a.stdout, versionString())
		return nil, nil
	case "completion":
		return nil, result.Request(a.completion(parsed.Args))
	case "update":
		if len(parsed.Args) != 0 || !selectorsEmpty(parsed.Selectors) {
			return nil, result.Request(errors.New("usage: amux update"))
		}
		if err := a.selfUpdate(options{dryRun: parsed.Options.DryRun}, nil); err != nil {
			return nil, result.Runtime(err)
		}
		return nil, nil
	default:
		return nil, result.Request(fmt.Errorf("unsupported foundation command %s", parsed.Command.Name))
	}
}

func resolveInvocationDirectory(explicit string) (config.Directory, error) {
	if explicit == "" && os.Getenv(config.ConfigDirEnv) == "" {
		if legacy := os.Getenv(config.WorkspacesEnv); legacy != "" {
			return config.Directory{}, fmt.Errorf("%s is no longer supported; select %s with AMUX_CONFIG_DIR or --config-dir, then run amux migrate-config", config.WorkspacesEnv, filepath.Dir(legacy))
		}
		if legacy := os.Getenv(config.LegacyWorkspacesEnv); legacy != "" {
			return config.Directory{}, fmt.Errorf("%s is no longer supported; select %s with AMUX_CONFIG_DIR or --config-dir, then run amux migrate-config", config.LegacyWorkspacesEnv, filepath.Dir(legacy))
		}
	}
	return config.ResolveDirectory(explicit)
}

func acquireMutationLock(path []string) (*lock.Lock, error) {
	lockPath, err := lock.MachinePath()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), mutationLockWait)
	defer cancel()
	return lock.Acquire(ctx, lockPath, lock.Owner{Command: "amux " + strings.Join(path, " ")})
}

func (a app) executeMigration(parsed invocation, dir config.Directory) (*result.Envelope, error) {
	envelope := result.NewEnvelope(strings.Join(parsed.Path, " "), parsed.Options.DryRun)
	plan, err := config.PlanMigration(dir)
	if err != nil {
		return &envelope, result.Preflight(err)
	}
	if len(plan.Actions) == 0 {
		envelope.Skipped = append(envelope.Skipped, result.Outcome{Resource: result.ConfigResource(dir.Path), Action: "migrate", Message: "no legacy configuration found"})
		if !parsed.Options.JSON {
			fmt.Fprintln(a.stdout, "No config migration needed.")
		}
		return &envelope, nil
	}
	if parsed.Options.DryRun {
		for _, action := range plan.Actions {
			outcome := migrationOutcome(action.Registry, action.Source, action.Destination, "migrate")
			if action.Status == config.MigrationSkipped {
				envelope.Skipped = append(envelope.Skipped, outcome)
				if !parsed.Options.JSON {
					fmt.Fprintf(a.stdout, "Would keep existing %s registry at %s\n", action.Registry, action.Destination)
				}
				continue
			}
			envelope.Planned = append(envelope.Planned, outcome)
			if !parsed.Options.JSON {
				fmt.Fprintf(a.stdout, "Would migrate %s registry to %s\n", action.Registry, action.Destination)
			}
		}
		return &envelope, nil
	}

	results, applyErr := plan.Apply()
	for _, migration := range results {
		outcome := migrationOutcome(migration.Registry, migration.Source, migration.Destination, "migrate")
		if migration.Status == config.MigrationSuccessful {
			envelope.Successful = append(envelope.Successful, outcome)
			if !parsed.Options.JSON {
				fmt.Fprintf(a.stdout, "Migrated %s registry to %s\n", migration.Registry, migration.Destination)
			}
		} else {
			envelope.Skipped = append(envelope.Skipped, outcome)
			if !parsed.Options.JSON {
				fmt.Fprintf(a.stdout, "Kept existing %s registry at %s\n", migration.Registry, migration.Destination)
			}
		}
	}
	if applyErr != nil {
		envelope.Failed = append(envelope.Failed, result.Outcome{
			Resource: result.ConfigResource(dir.Path),
			Action:   "migrate",
			Error:    &result.Failure{Kind: result.ErrorRuntime, Message: applyErr.Error()},
		})
		return &envelope, result.Runtime(applyErr)
	}
	if !parsed.Options.JSON {
		fmt.Fprintln(a.stdout, "Legacy config files were left in place for rollback.")
	}
	return &envelope, nil
}

func migrationOutcome(registry, source, destination, action string) result.Outcome {
	message := "create " + registry + " registry"
	if source != "" {
		message = "migrate from " + source
	}
	return result.Outcome{Resource: result.ConfigResource(destination), Action: action, Message: message}
}

func (a app) printCommandHelp(command *commandSpec) {
	fmt.Fprintf(a.stdout, "Usage: %s\n\n%s.\n", command.Usage, command.Summary)
	if command == rootCommand {
		fmt.Fprintln(a.stdout, "\nGlobal flags:")
		for _, flag := range []string{
			"--config-dir, -c <path>  Select the directory containing all config registries",
			"--json, -j               Emit one versioned JSON document",
			"--dry-run, -n            Plan without mutation",
			"--attach                  Attach after a complete explicit-workspace aggregate launch",
			"--no-attach               Never attach after launch",
			"--terminal-launcher <cmd> Terminal launcher used with --attach",
			"--help, -h               Show contextual help",
		} {
			fmt.Fprintf(a.stdout, "  %s\n", flag)
		}
	}
	if len(command.Flags) > 0 {
		fmt.Fprintln(a.stdout, "\nFlags:")
		for _, flag := range command.Flags {
			fmt.Fprintf(a.stdout, "  %s\n", flag)
		}
	}
	if len(command.Children) > 0 {
		children := append([]*commandSpec(nil), command.Children...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
		fmt.Fprintln(a.stdout, "\nCommands:")
		for _, child := range children {
			fmt.Fprintf(a.stdout, "  %-16s %s\n", child.Name, child.Summary)
		}
	}
}

func globalFlagRequested(args []string, long, short string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}
		if commandOptionRequiresValue(arg) || arg == "--config-dir" || arg == "-c" || arg == "--terminal-launcher" {
			i++
			continue
		}
		if arg == long || arg == short {
			return true
		}
	}
	return false
}

func guessedCommandPath(args []string) []string {
	_, words, err := parseCLIOptions(args)
	if err == nil {
		if len(words) > 0 && words[0] == "help" {
			words = words[1:]
		}
		if len(words) == 0 {
			return []string{"amux"}
		}
		path := []string{words[0]}
		if parent := commandChild(rootCommand, words[0]); parent != nil && len(parent.Children) > 0 && len(words) > 1 && !strings.HasPrefix(words[1], "-") {
			path = append(path, words[1])
		}
		return path
	}

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if arg == "--config-dir" || arg == "-c" {
			index++
			continue
		}
		if arg == "help" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return []string{arg}
		}
	}
	return []string{"amux"}
}
