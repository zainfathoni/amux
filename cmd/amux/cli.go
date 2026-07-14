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
	ConfigDir string
	JSON      bool
	DryRun    bool
	Help      bool
	Version   bool
}

type selectors struct {
	Workspace      string
	Window         string
	Workdir        string
	Thread         string
	Mode           string
	TitlePrefix    string
	Current        bool
	All            bool
	Shelf          string
	IdempotencyKey string
	Message        string
	MessageFile    string
	MessageStdin   bool
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
	Options   cliOptions
	Command   *commandSpec
	Path      []string
	Selectors selectors
	Args      []string
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
		installCommand(),
		lifecycleCommand("workspaces", "Exact alias for workspace list", false),
		lifecycleCommand("spawn", "Spawn an interactive worker", true, "--workspace, -w <name>", "--window, -W <name>", "--workdir, -d <path>", "--mode, -m <mode>", "--title-prefix <prefix>  An exact #<number> prefix owns issue identity; window must be an issue-unprefixed semantic slug", "--message <text>", "--message-file <path>", "--message-stdin", "--idempotency-key <key>"),
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
		workerLeaf("spawn", "Spawn a worker", true, "--workspace, -w <name>", "--window, -W <name>", "--workdir, -d <path>", "--mode, -m <mode>", "--title-prefix <prefix>  An exact #<number> prefix owns issue identity; window must be an issue-unprefixed semantic slug", "--message <text>", "--message-file <path>", "--message-stdin", "--idempotency-key <key>"),
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

func runnerLeaf(name, summary string, mutating bool, flags ...string) *commandSpec {
	return &commandSpec{Name: name, Summary: summary, Usage: "amux runner " + name + " [selectors]", Flags: flags, NeedsConfig: true, Mutating: mutating}
}

func workspaceCommand() *commandSpec {
	return &commandSpec{
		Name:    "workspace",
		Summary: "Inspect configured workspaces",
		Usage:   "amux workspace <command>",
		Children: []*commandSpec{
			{Name: "list", Summary: "List worker and runner workspaces", Usage: "amux workspace list", NeedsConfig: true},
		},
	}
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

	wantsJSON := jsonRequested(args)
	parsed, err := parseInvocation(args)
	if err != nil {
		err = result.Request(err)
		return a.finishInvocation(invocation{Options: cliOptions{JSON: wantsJSON}, Path: guessedCommandPath(args)}, nil, err)
	}
	envelope, err := a.dispatch(parsed)
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
	if !spec.FoundationOnly && spec.Mutating && spec.Name != "launch" && !hasResourceScope(parsed.Selectors) {
		return parsed, fmt.Errorf("%s requires a resource scope; use an explicit selector or --all", strings.Join(path, " "))
	}
	return parsed, nil
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
		switch arg {
		case "--json", "-j":
			opts.JSON = true
		case "--dry-run", "-n":
			opts.DryRun = true
		case "--help", "-h":
			opts.Help = true
		case "--version":
			opts.Version = true
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
	child := commandChild(root, remaining[0])
	if child == nil {
		return nil, nil, nil, fmt.Errorf("unknown %s command %q; run `amux help %s`", root.Name, remaining[0], root.Name)
	}
	path = append(path, child.Name)
	return child, path, remaining[1:], nil
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
		default:
			if strings.HasPrefix(arg, "-") {
				return parsed, nil, fmt.Errorf("unknown option %s", arg)
			}
			remaining = append(remaining, arg)
		}
	}
	if parsed.All && (parsed.Current || parsed.Workspace != "" || parsed.Window != "" || parsed.Workdir != "" || parsed.Thread != "") {
		return parsed, nil, errors.New("--all cannot be combined with --current or resource selectors")
	}
	if parsed.Current && (parsed.Workspace != "" || parsed.Window != "" || parsed.Workdir != "" || parsed.Thread != "") {
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
		{"--mode", parsed.Mode},
		{"--title-prefix", parsed.TitlePrefix},
		{"--shelf", parsed.Shelf},
		{"--idempotency-key", parsed.IdempotencyKey},
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
	return parsed.All || parsed.Current || parsed.Workspace != "" || parsed.Window != "" || parsed.Workdir != "" || parsed.Thread != ""
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

	if !parsed.Command.FoundationOnly && (len(parsed.Path) != 2 || parsed.Path[0] != "worker") && !isWorkerConvenience(parsed.Path) {
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

	switch parsed.Command.Name {
	case "list", "pin", "unpin", "launch", "park", "restart", "remove", "spawn", "shelve", "unshelve", "teardown", "doctor", "reconcile":
		if strings.Join(parsed.Path, " ") == "install doctor" {
			if len(parsed.Args) != 0 || parsed.Selectors != (selectors{}) {
				return nil, result.Request(errors.New("usage: amux install doctor"))
			}
			return a.installDoctor(parsed)
		}
		if !parsed.Command.FoundationOnly {
			return a.executeWorker(parsed, dir)
		}
		return nil, result.Preflight(fmt.Errorf("%s is reserved for its lifecycle implementation phase", strings.Join(parsed.Path, " ")))
	case "migrate-config":
		if len(parsed.Args) != 0 || parsed.Selectors != (selectors{}) {
			return nil, result.Request(errors.New("usage: amux migrate-config"))
		}
		return a.executeMigration(parsed, dir)
	case "path":
		if len(parsed.Args) != 0 || parsed.Selectors != (selectors{}) {
			return nil, result.Request(errors.New("usage: amux path"))
		}
		fmt.Fprintln(a.stdout, dir.Path)
		return nil, nil
	case "version":
		if len(parsed.Args) != 0 || parsed.Selectors != (selectors{}) {
			return nil, result.Request(errors.New("usage: amux version"))
		}
		fmt.Fprintln(a.stdout, versionString())
		return nil, nil
	case "completion":
		return nil, result.Request(a.completion(parsed.Args))
	case "update":
		if len(parsed.Args) != 0 || parsed.Selectors != (selectors{}) {
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

func jsonRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || arg == "-j" {
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
