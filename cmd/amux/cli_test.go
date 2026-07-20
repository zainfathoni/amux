package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/result"
)

func TestExecuteUsesConfigDirectoryFlagAndDoesNotCreateItForPath(t *testing.T) {
	home := t.TempDir()
	fromEnv := filepath.Join(home, "from-env")
	fromFlag := filepath.Join(home, "from-flag")
	t.Setenv("HOME", home)
	t.Setenv("AMUX_CONFIG_DIR", fromEnv)

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--config-dir", fromFlag, "path"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), fromFlag+"\n"; got != want {
		t.Fatalf("path output = %q, want %q", got, want)
	}
	for _, path := range []string{fromEnv, fromFlag} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("read-only path command created %s: %v", path, err)
		}
	}
}

func TestExecuteRejectsLegacyFileSelectionWithRemediation(t *testing.T) {
	t.Setenv("AMUX_CONFIG_DIR", "")
	t.Setenv("AMUX_WORKSPACES", "/tmp/legacy.tsv")
	err := (app{}).execute([]string{"path"})
	if err == nil || !strings.Contains(err.Error(), "AMUX_CONFIG_DIR") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("legacy environment error = %v, exit=%d", err, result.ExitCode(err))
	}

	err = (app{}).execute([]string{"--config", "/tmp/legacy.tsv", "path"})
	if err == nil || !strings.Contains(err.Error(), "--config-dir") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("legacy flag error = %v, exit=%d", err, result.ExitCode(err))
	}
}

func TestExecuteProvidesContextualHelpAndStableSelectorFlags(t *testing.T) {
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"help", "worker", "pin"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Usage: amux worker pin",
		"--workspace, -w",
		"--window, -W",
		"--workdir, -d",
		"--thread, -t",
		"--current",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("worker pin help missing %q\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	if err := (app{stdout: &stdout}).execute([]string{"runner", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Usage: amux runner <command>") || !strings.Contains(stdout.String(), "pin") {
		t.Fatalf("runner help is not contextual:\n%s", stdout.String())
	}
}

func TestCompletionRootSurfaceMatchesAuthoritativeHelp(t *testing.T) {
	var help bytes.Buffer
	if err := (app{stdout: &help}).execute([]string{"help"}); err != nil {
		t.Fatal(err)
	}
	commands, flags := rootHelpSurface(help.String())
	if len(commands) == 0 || len(flags) == 0 {
		t.Fatalf("failed to read root help surface:\n%s", help.String())
	}

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var completion bytes.Buffer
			if err := (app{stdout: &completion}).execute([]string{"completion", shell}); err != nil {
				t.Fatal(err)
			}
			output := completion.String()
			bashWords := map[string]bool{}
			zshCommands := map[string]bool{}
			if shell == "bash" {
				bashWords = bashRootCompletionWords(output)
			} else if shell == "zsh" {
				zshCommands = zshRootCompletionCommands(output)
			}
			for _, command := range commands {
				found := false
				switch shell {
				case "zsh":
					found = zshCommands[command]
				case "fish":
					found = completionLineContains(output, "__fish_use_subcommand", "-a '"+command+"'")
				default:
					found = bashWords[command]
				}
				if !found {
					t.Errorf("%s root completion is missing help command %q", shell, command)
				}
			}
			for _, flag := range flags {
				found := false
				if shell == "fish" {
					if strings.HasPrefix(flag, "--") {
						found = completionLineContains(output, "__fish_use_subcommand", "-l '"+strings.TrimPrefix(flag, "--")+"'")
					} else {
						found = completionLineContains(output, "__fish_use_subcommand", "-s '"+strings.TrimPrefix(flag, "-")+"'")
					}
				} else if shell == "bash" {
					found = bashWords[flag]
				} else {
					found = strings.Contains(output, "  '"+flag+"[")
				}
				if !found {
					t.Errorf("%s root completion is missing help global flag %q", shell, flag)
				}
			}
		})
	}
}

func TestCompletionPreservesRunnerAndWorkspaceLeaves(t *testing.T) {
	tests := map[string][]string{
		"bash": {
			`compgen -W "maintenance list pin unpin launch park restart remove doctor reconcile"`,
			`pin) COMPREPLY=( $(compgen -W "--workspace --workdir --current -w -d"`,
			`unpin) COMPREPLY=( $(compgen -W "--workdir --current -d"`,
			`compgen -W "install remove run"`,
			`compgen -W "--update-owner"`,
			`compgen -W "--scheduled"`,
			`compgen -W "--mode -m"`,
		},
		"zsh": {
			"runner_commands=(", "runner_maintenance_commands=(", "workspace_commands=(",
			`pin) _arguments '--workspace[workspace]:workspace:' '--workdir[working directory]:directory:_directories' '--current[current runner]'`,
			`unpin) _arguments '--workdir[working directory]:directory:_directories' '--current[current runner]'`,
			`_arguments '--update-owner[update owner]:owner:(self external)'`,
			`_arguments '--scheduled[scheduled invocation]'`,
			`'--mode[client mode]:mode:(worker runner)'`,
		},
		"fish": {
			"test (__fish_amux_root_command) = runner; and test -z (__fish_amux_runner_leaf)' -a 'maintenance'",
			"test (__fish_amux_runner_leaf) = pin' -r -l 'workdir'",
			"test (__fish_amux_runner_leaf) = unpin' -r -l 'workdir'",
			"test -z (__fish_amux_runner_maintenance_command)' -a 'install'",
			"test (__fish_amux_runner_maintenance_command) = install' -r -l 'update-owner'",
			"test (__fish_amux_runner_maintenance_command) = run' -f -l 'scheduled'",
			"test (__fish_amux_root_command) = workspaces' -r -f -a 'worker runner' -l 'mode' -d 'Client mode'",
			"test (__fish_amux_workspace_leaf) = list' -r -f -a 'worker runner' -l 'mode' -d 'Client mode'",
		},
	}

	for shell, wants := range tests {
		t.Run(shell, func(t *testing.T) {
			var completion bytes.Buffer
			if err := (app{stdout: &completion}).execute([]string{"completion", shell}); err != nil {
				t.Fatal(err)
			}
			for _, want := range wants {
				if !strings.Contains(completion.String(), want) {
					t.Errorf("%s completion is missing runner/workspace contract %q", shell, want)
				}
			}
		})
	}
}

func rootHelpSurface(help string) (commands, flags []string) {
	section := ""
	for _, line := range strings.Split(help, "\n") {
		switch line {
		case "Global flags:":
			section = "flags"
			continue
		case "Commands:":
			section = "commands"
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		fields := strings.Fields(line)
		switch section {
		case "commands":
			commands = append(commands, fields[0])
		case "flags":
			for _, field := range fields {
				flag := strings.TrimSuffix(field, ",")
				if strings.HasPrefix(flag, "-") {
					flags = append(flags, flag)
					continue
				}
				break
			}
		}
	}
	return commands, flags
}

func bashRootCompletionWords(completion string) map[string]bool {
	words := map[string]bool{}
	for _, line := range strings.Split(completion, "\n") {
		if !strings.Contains(line, "COMPREPLY") || !strings.Contains(line, "--config-dir") {
			continue
		}
		start := strings.Index(line, `compgen -W "`)
		if start == -1 {
			continue
		}
		value := line[start+len(`compgen -W "`):]
		end := strings.Index(value, `"`)
		if end == -1 {
			continue
		}
		for _, word := range strings.Fields(value[:end]) {
			words[word] = true
		}
	}
	return words
}

func zshRootCompletionCommands(completion string) map[string]bool {
	commands := map[string]bool{}
	inCommands := false
	for _, line := range strings.Split(completion, "\n") {
		switch line {
		case "commands=(":
			inCommands = true
			continue
		case ")":
			if inCommands {
				return commands
			}
		}
		if !inCommands {
			continue
		}
		entry := strings.Trim(strings.TrimSpace(line), `"`)
		name, _, found := strings.Cut(entry, ":")
		if found {
			commands[name] = true
		}
	}
	return commands
}

func completionLineContains(completion string, values ...string) bool {
	for _, line := range strings.Split(completion, "\n") {
		found := true
		for _, value := range values {
			found = found && strings.Contains(line, value)
		}
		if found {
			return true
		}
	}
	return false
}

func TestWorkerSpawnHelpDocumentsIssueTitleOwnership(t *testing.T) {
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"help", "worker", "spawn"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--title-prefix <prefix>",
		"exact #<number> prefix owns issue identity",
		"issue-unprefixed semantic slug",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("worker spawn help missing %q\n%s", want, stdout.String())
		}
	}
}

func TestParseSelectorsSupportsFixedShorthandsAndExplicitScopes(t *testing.T) {
	selectors, remaining, err := parseSelectors([]string{
		"-w", "workspace", "-W=window", "-d", "/tmp/project", "-t=T-worker", "-m", "high", "--title-prefix", "#119",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining selectors = %q", remaining)
	}
	if selectors.Workspace != "workspace" || selectors.Window != "window" || selectors.Workdir != "/tmp/project" || selectors.Thread != "T-worker" || selectors.Mode != "high" || selectors.TitlePrefix != "#119" {
		t.Fatalf("selectors = %+v", selectors)
	}
	current, _, err := parseSelectors([]string{"--current"})
	if err != nil || !current.Current {
		t.Fatalf("current selector = %+v, error = %v", current, err)
	}

	_, _, err = parseSelectors([]string{"--all", "--workspace", "workspace"})
	if err == nil || !strings.Contains(err.Error(), "--all cannot be combined") {
		t.Fatalf("conflicting --all error = %v", err)
	}
	_, _, err = parseSelectors([]string{"--current", "--thread", "T-worker"})
	if err == nil || !strings.Contains(err.Error(), "--current cannot be combined") {
		t.Fatalf("conflicting --current error = %v", err)
	}
}

func TestParseInvocationConsumesGlobalFlagTokensAsOptionValues(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantJSON   bool
		wantDryRun bool
		want       func(selectors) string
		wantValue  string
	}{
		{
			name:       "reported message value",
			args:       []string{"--dry-run", "worker", "spawn", "--workspace", "demo", "--window", "test", "--workdir", "/tmp", "--message", "--json", "--idempotency-key", "demo-key"},
			wantDryRun: true,
			want:       func(got selectors) string { return got.Message },
			wantValue:  "--json",
		},
		{
			name:      "selector value before true trailing global",
			args:      []string{"worker", "list", "--workspace", "--dry-run", "--json"},
			wantJSON:  true,
			want:      func(got selectors) string { return got.Workspace },
			wantValue: "--dry-run",
		},
		{
			name:      "inline value before true trailing global",
			args:      []string{"worker", "list", "--workspace=--dry-run", "--json"},
			wantJSON:  true,
			want:      func(got selectors) string { return got.Workspace },
			wantValue: "--dry-run",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseInvocation(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if got.Options.JSON != test.wantJSON || got.Options.DryRun != test.wantDryRun {
				t.Fatalf("global options = %+v, want JSON=%t DryRun=%t", got.Options, test.wantJSON, test.wantDryRun)
			}
			if value := test.want(got.Selectors); value != test.wantValue {
				t.Fatalf("option value = %q, want %q", value, test.wantValue)
			}
		})
	}
}

func TestParseCLIOptionsPreservesGlobalTokensForEveryCommandOptionValue(t *testing.T) {
	for _, option := range []string{
		"--workspace", "-w", "--window", "-W", "--workdir", "-d", "--thread", "-t",
		"--group", "--mode", "-m", "--title-prefix", "--shelf", "--idempotency-key",
		"--report-id", "--pane", "--status", "--issue", "--reference", "--pr", "--summary",
		"--message", "--message-file", "--update-owner",
	} {
		t.Run(option, func(t *testing.T) {
			opts, words, err := parseCLIOptions([]string{option, "--json", "--dry-run"})
			if err != nil {
				t.Fatal(err)
			}
			if opts.JSON || !opts.DryRun {
				t.Fatalf("global options = %+v, want JSON=false DryRun=true", opts)
			}
			if got, want := strings.Join(words, " "), option+" --json"; got != want {
				t.Fatalf("remaining words = %q, want %q", got, want)
			}
		})
	}
}

func TestParseInvocationRejectsContextualFlagsImplicitBulkAndPositionals(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"runner", "pin", "--window", "hidden-runtime-name"}, want: "runner pin does not accept --window"},
		{args: []string{"worker", "park"}, want: "use an explicit selector or --all"},
		{args: []string{"launch", "workspace", "session"}, want: "positional selectors were removed"},
	} {
		_, err := parseInvocation(test.args)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("parseInvocation(%q) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestExecuteRejectsRemovedAndUnknownCommandsBeforeSideEffects(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), "#!/bin/sh\necho tmux >> "+logPath+"\n")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"store", "mac", "worker", "/tmp", "T-worker"}, want: "amux worker pin"},
		{args: []string{"self-update"}, want: "amux update"},
		{args: []string{"mac", "Amp"}, want: "unknown command"},
	} {
		err := (app{}).execute(test.args)
		if err == nil || !strings.Contains(err.Error(), test.want) || result.ExitCode(err) != result.ExitRejected {
			t.Fatalf("execute(%q) error = %v, exit=%d", test.args, err, result.ExitCode(err))
		}
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("rejected command invoked tmux: %v", err)
	}
}

func TestExecuteRequiresExplicitMigrationWithoutWritingConfig(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "workspaces.tsv")
	legacy := "mac\tworker\t/tmp/project\tT-worker\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	err := (app{}).execute([]string{"--config-dir", dir, "worker", "list"})
	if err == nil || !strings.Contains(err.Error(), "amux migrate-config") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("migration-required error = %v, exit=%d", err, result.ExitCode(err))
	}
	if _, err := os.Stat(filepath.Join(dir, "workers.tsv")); !os.IsNotExist(err) {
		t.Fatalf("ordinary command migrated config: %v", err)
	}
	got, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != legacy {
		t.Fatalf("ordinary command changed legacy config: %q", got)
	}
}

func TestExecuteRunnerMutationIsAvailableAndUsesLock(t *testing.T) {
	configDir := t.TempDir()
	runtimeDir := filepath.Join(t.TempDir(), "missing-runtime")
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

	err := (app{}).execute([]string{"--config-dir", configDir, "runner", "park", "--all"})
	if err != nil {
		t.Fatalf("runner lifecycle error = %v", err)
	}
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Fatalf("runner mutation did not use operation lock directory: %v", err)
	}
}

func TestExecuteMigrationDryRunJSONIsOneDocumentAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "workspaces.tsv")
	if err := os.WriteFile(legacyPath, []byte("mac\tworker\t/tmp/project\tT-worker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"-c", dir, "-j", "-n", "migrate-config"})
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&stdout)
	var document struct {
		SchemaVersion int              `json:"schema_version"`
		Planned       []result.Outcome `json:"planned"`
		Successful    []result.Outcome `json:"successful"`
		Failed        []result.Outcome `json:"failed"`
	}
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON stdout contains more than one document: %v", err)
	}
	if document.SchemaVersion != result.SchemaVersion || len(document.Planned) != 3 || len(document.Successful) != 0 || len(document.Failed) != 0 {
		t.Fatalf("migration JSON = %+v", document)
	}
	for _, target := range []string{"workers.tsv", "runners.tsv", "shelves.tsv"} {
		if _, err := os.Stat(filepath.Join(dir, target)); !os.IsNotExist(err) {
			t.Fatalf("dry-run migration wrote %s: %v", target, err)
		}
	}
}

func TestExecuteJSONRejectionStillWritesExactlyOneDocument(t *testing.T) {
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--config-dir", "/tmp/amux-test-config", "--json", "unknown-command"})
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("JSON rejection error = %v, exit=%d", err, result.ExitCode(err))
	}
	decoder := json.NewDecoder(&stdout)
	var envelope result.Envelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON stdout contains more than one document: %v", err)
	}
	if len(envelope.Failed) != 1 || envelope.Failed[0].Error.Kind != result.ErrorRequest {
		t.Fatalf("JSON rejection envelope = %+v", envelope)
	}
	if envelope.Command != "unknown-command" {
		t.Fatalf("JSON rejection command = %q, want %q", envelope.Command, "unknown-command")
	}
}

func TestExecuteErrorOutputDistinguishesJSONFlagsFromOptionValues(t *testing.T) {
	t.Run("true trailing global emits JSON", func(t *testing.T) {
		var stdout bytes.Buffer
		err := (app{stdout: &stdout}).execute([]string{"worker", "list", "--attach", "--no-attach", "--json"})
		if err == nil || result.ExitCode(err) != result.ExitRejected {
			t.Fatalf("error = %v, exit=%d", err, result.ExitCode(err))
		}
		var envelope result.Envelope
		decoder := json.NewDecoder(&stdout)
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			t.Fatalf("JSON stdout contains more than one document: %v", err)
		}
		if len(envelope.Failed) != 1 || envelope.Failed[0].Error.Kind != result.ErrorRequest {
			t.Fatalf("JSON rejection envelope = %+v", envelope)
		}
	})

	t.Run("message value does not emit JSON", func(t *testing.T) {
		var stdout bytes.Buffer
		err := (app{stdout: &stdout}).execute([]string{"worker", "spawn", "--message", "--json", "--attach", "--no-attach"})
		if err == nil || result.ExitCode(err) != result.ExitRejected {
			t.Fatalf("error = %v, exit=%d", err, result.ExitCode(err))
		}
		if stdout.Len() != 0 {
			t.Fatalf("non-global --json produced output %q", stdout.String())
		}
	})
}

func TestExecuteMutationLockRejectsConcurrentMigrationWithOwner(t *testing.T) {
	runtimeDir := t.TempDir()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	if err := os.WriteFile(filepath.Join(dir, "workspaces.tsv"), []byte("mac\tworker\t/tmp/project\tT-worker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := lock.MachinePath()
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), path, lock.Owner{PID: 456, Command: "other amux mutation", Hostname: "test-host"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })
	oldWait := mutationLockWait
	mutationLockWait = 30 * time.Millisecond
	t.Cleanup(func() { mutationLockWait = oldWait })

	var stdout bytes.Buffer
	err = (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "migrate-config"})
	var busy *lock.BusyError
	if !errors.As(err, &busy) || busy.Owner.PID != 456 || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("lock contention error = %v, owner=%+v, exit=%d", err, busy, result.ExitCode(err))
	}
	var envelope result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(envelope.Failed) != 1 || envelope.Failed[0].Error.Lock == nil || envelope.Failed[0].Error.Lock.Owner.PID != 456 {
		t.Fatalf("lock contention JSON = %+v", envelope)
	}
	if _, err := os.Stat(filepath.Join(dir, "workers.tsv")); !os.IsNotExist(err) {
		t.Fatalf("contending migration wrote config: %v", err)
	}
}
