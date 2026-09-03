package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/result"
)

func TestHelpAndCompletionsExposeOnlyRetainedHostCommands(t *testing.T) {
	removed := []string{"worker", "spawn", "group", "report", "callback", "shelve", "unshelve"}
	var help bytes.Buffer
	if err := (app{stdout: &help}).execute([]string{"help"}); err != nil {
		t.Fatal(err)
	}
	for _, retained := range []string{"runner", "workspace", "install", "launch", "park", "restart", "doctor", "reconcile", "migrate-config", "completion", "update"} {
		if !strings.Contains(help.String(), "  "+retained) {
			t.Errorf("root help is missing retained command %q:\n%s", retained, help.String())
		}
	}
	for _, command := range removed {
		if strings.Contains(help.String(), "  "+command+" ") {
			t.Errorf("root help advertises removed command %q:\n%s", command, help.String())
		}
	}
	var runnerHelp bytes.Buffer
	if err := (app{stdout: &runnerHelp}).execute([]string{"help", "runner"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runnerHelp.String(), "  teardown ") || strings.Contains(help.String(), "  teardown ") {
		t.Errorf("teardown must be runner-scoped only; root=%q runner=%q", help.String(), runnerHelp.String())
	}

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var completion bytes.Buffer
			if err := (app{stdout: &completion}).execute([]string{"completion", shell}); err != nil {
				t.Fatal(err)
			}
			output := completion.String()
			for _, command := range removed {
				if completionHasCommand(output, command) {
					t.Errorf("%s completion advertises removed command %q", shell, command)
				}
			}
			for _, retained := range []string{"runner", "workspace", "launch", "doctor"} {
				if !completionHasCommand(output, retained) {
					t.Errorf("%s completion is missing retained command %q", shell, retained)
				}
			}
			for _, retained := range []string{"maintenance", "install", "remove", "run", "teardown", "confirm-plan"} {
				if !strings.Contains(output, retained) {
					t.Errorf("%s completion is missing functional retained token %q", shell, retained)
				}
			}
			switch shell {
			case "bash":
				if !strings.Contains(output, "for ((i=1; i<COMP_CWORD; i++))") || !strings.Contains(output, `"$leaf" == maintenance`) || !strings.Contains(output, "--config-dir") || !strings.Contains(output, "--update-owner") {
					t.Error("bash completion does not parse global flags and nested runner maintenance commands")
				}
			case "zsh":
				if !strings.Contains(output, "for ((i=2; i<CURRENT; i++))") || !strings.Contains(output, "if [[ $word == --config-dir") || !strings.Contains(output, "case $command in") || !strings.Contains(output, "runner_commands") || !strings.Contains(output, "maintenance_commands") || !strings.Contains(output, "--update-owner") {
					t.Error("zsh completion does not implement nested command and flag completion")
				}
			case "fish":
				if !strings.Contains(output, "not __fish_seen_subcommand_from maintenance list pin unpin teardown launch park restart remove doctor reconcile") || strings.Contains(output, "not __fish_seen_subcommand_from 'maintenance list") || !strings.Contains(output, "__fish_seen_subcommand_from maintenance") || !strings.Contains(output, "__fish_seen_subcommand_from list launch park restart remove doctor reconcile pin; and not __fish_seen_subcommand_from maintenance") || !strings.Contains(output, "-l workdir -s d -r") || !strings.Contains(output, "-l workspace -s w -r") || !strings.Contains(output, "-l confirm-plan -r") || !strings.Contains(output, "-l config-dir -r") || !strings.Contains(output, "-l terminal-launcher -r") || !strings.Contains(output, "-l update-owner -r") {
					t.Error("fish completion does not scope nested maintenance and runner flags")
				}
			}
		})
	}
}

func completionHasCommand(output, command string) bool {
	return strings.Contains(output, " "+command+" ") ||
		strings.Contains(output, `"`+command+`:`) ||
		strings.Contains(output, "-a '"+command+"'")
}

func TestBashCompletionReturnsNestedRunnerCandidates(t *testing.T) {
	var completion bytes.Buffer
	if err := (app{stdout: &completion}).execute([]string{"completion", "bash"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		words     string
		index     int
		candidate string
		excluded  string
	}{
		{name: "root", words: `amux ""`, index: 1, candidate: "runner"},
		{name: "runner after global flag", words: `amux --json runner ""`, index: 3, candidate: "maintenance"},
		{name: "runner after help flag", words: `amux --help runner ""`, index: 3, candidate: "maintenance"},
		{name: "maintenance", words: `amux runner maintenance ""`, index: 3, candidate: "install"},
		{name: "maintenance install flags", words: `amux runner maintenance install ""`, index: 4, candidate: "--update-owner"},
		{name: "maintenance remove has no runner flags", words: `amux runner maintenance remove ""`, index: 4, excluded: "--all"},
		{name: "teardown exact flags", words: `amux runner teardown ""`, index: 3, candidate: "--confirm-plan", excluded: "--all"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := completion.String() + fmt.Sprintf("\nCOMP_WORDS=(%s); COMP_CWORD=%d; _amux_complete; printf '%%s\\n' \"${COMPREPLY[@]}\"\n", test.words, test.index)
			output, err := exec.Command("bash", "-c", script).CombinedOutput()
			if err != nil {
				t.Fatalf("execute completion: %v\n%s", err, output)
			}
			if test.candidate != "" && !strings.Contains("\n"+string(output)+"\n", "\n"+test.candidate+"\n") {
				t.Fatalf("candidates do not contain %q: %s", test.candidate, output)
			}
			if test.excluded != "" && strings.Contains("\n"+string(output)+"\n", "\n"+test.excluded+"\n") {
				t.Fatalf("candidates unexpectedly contain %q: %s", test.excluded, output)
			}
		})
	}
}

func TestZshCompletionReturnsNestedRunnerCandidates(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh unavailable")
	}
	var completion bytes.Buffer
	if err := (app{stdout: &completion}).execute([]string{"completion", "zsh"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		words     string
		current   int
		candidate string
		excluded  string
	}{
		{name: "runner after help flag", words: `amux --help runner ''`, current: 4, candidate: "maintenance"},
		{name: "maintenance after valued global flag", words: `amux --config-dir /tmp runner maintenance ''`, current: 6, candidate: "install"},
		{name: "maintenance install flags", words: `amux runner maintenance install ''`, current: 5, candidate: "--update-owner"},
		{name: "maintenance remove has no runner flags", words: `amux runner maintenance remove ''`, current: 5, excluded: "--all"},
		{name: "runner pin excludes all", words: `amux runner pin ''`, current: 4, excluded: "--all"},
		{name: "runner teardown flags", words: `amux runner teardown ''`, current: 4, candidate: "--confirm-plan", excluded: "--all"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := `
function _arguments {
  if [[ "$*" == *--update-owner* ]]; then
    print -r -- --update-owner
  elif [[ -n "$state" ]]; then
    print -r -- "$*"
  else
    state=args
  fi
}
function _describe {
  local array_name=${argv[-1]}
  print -l -- ${(P)array_name}
}
function _values { shift; print -l -- "$@" }
function _amux_test {
` + completion.String() + "\n}\nwords=(" + test.words + fmt.Sprintf(")\nCURRENT=%d\n_amux_test\n", test.current)
			output, err := exec.Command(zsh, "-f", "-c", script).CombinedOutput()
			if err != nil {
				t.Fatalf("execute completion: %v\n%s", err, output)
			}
			if test.candidate != "" && !strings.Contains(string(output), test.candidate) {
				t.Fatalf("candidates do not contain %q: %s", test.candidate, output)
			}
			if test.excluded != "" && strings.Contains(string(output), test.excluded) {
				t.Fatalf("candidates unexpectedly contain %q: %s", test.excluded, output)
			}
		})
	}
}

func TestFishCompletionDoesNotLeakRunnerFlagsIntoMaintenance(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish unavailable")
	}
	var completion bytes.Buffer
	if err := (app{stdout: &completion}).execute([]string{"completion", "fish"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		command   string
		candidate string
		excluded  string
	}{
		{command: "amux runner maintenance install --", candidate: "--update-owner"},
		{command: "amux runner maintenance remove --", excluded: "--all"},
		{command: "amux runner pin --", excluded: "--all"},
	} {
		script := completion.String() + "\ncomplete -C " + shellSingleQuote(test.command) + "\n"
		output, err := exec.Command(fish, "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("execute completion: %v\n%s", err, output)
		}
		if test.candidate != "" && !strings.Contains(string(output), test.candidate) {
			t.Fatalf("candidates do not contain %q: %s", test.candidate, output)
		}
		if test.excluded != "" && strings.Contains(string(output), test.excluded) {
			t.Fatalf("candidates unexpectedly contain %q: %s", test.excluded, output)
		}
	}
}

func TestRemovedCoordinationCommandsAreTombstonesBeforeSideEffects(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	writeExecutable(t, filepath.Join(dir, "tmux"), "#!/bin/sh\necho tmux >>"+shellSingleQuote(logPath)+"\n")
	writeExecutable(t, filepath.Join(dir, "amp"), "#!/bin/sh\necho amp >>"+shellSingleQuote(logPath)+"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tests := []struct {
		command string
		want    string
	}{
		{"worker", "native Amp threads"},
		{"spawn", "native Amp thread creation"},
		{"group", "native Amp parent/reply routing"},
		{"report", "/amux-tycho uses its separate receipt bridge"},
		{"callback", "callback leases were removed"},
		{"shelve", "native Amp archive state"},
		{"unshelve", "native Amp archive state"},
		{"teardown", "worker teardown was removed"},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			err := (app{}).execute([]string{"--config-dir", dir, test.command})
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("%s error = %v, exit=%d", test.command, err, result.ExitCode(err))
			}
		})
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("removed command invoked a process: %v", err)
	}
}

func TestRemovedCommandsDoNotMutateLegacyStores(t *testing.T) {
	dir := t.TempDir()
	stores := map[string][]byte{
		"workers.tsv":            []byte("# amux-schema: workers/v1\nlegacy\tworker\t/tmp/work\tT-old\n"),
		"shelves.tsv":            []byte("# amux-schema: shelves/v1\nT-old\n"),
		"groups.tsv":             []byte("# amux-schema: groups/v1\nlegacy\tT-old\tmember\n"),
		"reports.json":           []byte("{\"schema_version\":1,\"reports\":[],\"deadlines\":[]}\n"),
		"operations.json":        []byte("{\"schema_version\":1,\"operations\":[]}\n"),
		"spawn-assignments.json": []byte("{\"schema_version\":1,\"assignments\":[]}\n"),
	}
	for name, contents := range stores {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []string{"worker", "spawn", "group", "report", "callback", "shelve", "teardown"} {
		_ = (app{}).execute([]string{"--config-dir", dir, command})
	}
	for name, before := range stores {
		after, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s changed: err=%v before=%q after=%q", name, err, before, after)
		}
	}
}

func TestBareAmuxAndTopLevelLifecycleRouteToRunners(t *testing.T) {
	bare, err := parseInvocation(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(bare.Path, " "); got != "launch" || !bare.Selectors.All {
		t.Fatalf("bare amux = path %q all=%t, want launch --all", got, bare.Selectors.All)
	}
	for _, command := range []string{"list", "launch", "park", "restart", "remove", "doctor", "reconcile"} {
		parsed, err := parseInvocation([]string{command, "--all"})
		if err != nil {
			t.Fatalf("parse %s: %v", command, err)
		}
		if !isAggregateLifecycle(parsed.Path) {
			t.Fatalf("%s is not a retained top-level runner alias", command)
		}
	}
}

func TestWorkspaceListIgnoresInertWorkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "workers.tsv"), []byte("worker-only\tw\t/tmp/w\tT-old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runners.tsv"), []byte("runner-only\tr\t/tmp/r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"--config-dir", dir, "workspace", "list"}); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "runner-only\trunner") || strings.Contains(got, "worker-only") {
		t.Fatalf("workspace list = %q", got)
	}
}

func TestPathIsReadOnlyAndJSONRejectionIsOneDocument(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"--config-dir", dir, "path"}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != dir+"\n" {
		t.Fatalf("path output = %q", stdout.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("path created config directory: %v", err)
	}

	stdout.Reset()
	err := (app{stdout: &stdout}).execute([]string{"--json", "report"})
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("report rejection = %v", err)
	}
	decoder := json.NewDecoder(&stdout)
	var env result.Envelope
	if err := decoder.Decode(&env); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON output has more than one document: %v", err)
	}
	if len(env.Failed) != 1 || !strings.Contains(env.Failed[0].Error.Message, "/amux-tycho") {
		t.Fatalf("report tombstone JSON = %+v", env)
	}
}
