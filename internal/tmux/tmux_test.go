package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSessionExistsDistinguishesAbsenceFromInspectionFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		script     string
		wantExists bool
		wantError  string
	}{
		{name: "exists", script: "#!/bin/sh\nexit 0\n", wantExists: true},
		{name: "absent", script: "#!/bin/sh\nexit 1\n"},
		{name: "missing server", script: "#!/bin/sh\necho 'no server running on /tmp/tmux.sock' >&2\nexit 1\n"},
		{name: "missing server socket", script: "#!/bin/sh\necho 'error connecting to /tmp/tmux-1000/default (No such file or directory)' >&2\nexit 1\n"},
		{name: "inspection failure", script: "#!/bin/sh\necho 'permission denied' >&2\nexit 1\n", wantError: "permission denied"},
		{name: "unexpected exit", script: "#!/bin/sh\nexit 2\n", wantError: "tmux has-session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			writeExecutable(t, filepath.Join(bin, "tmux"), test.script)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			exists, err := (Runner{}).SessionExists("alpha")
			if exists != test.wantExists {
				t.Fatalf("SessionExists() exists = %t, want %t", exists, test.wantExists)
			}
			if test.wantError == "" && err != nil {
				t.Fatalf("SessionExists() error = %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("SessionExists() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestInspectProcessUsesLinuxAndMacOSCompatiblePSFields(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "ps.log")
	writeExecutable(t, filepath.Join(tmp, "ps"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
case "$*" in
  "-p 4242 -o comm=") printf '/opt/amp/bin/amp\n' ;;
  "-p 4242 -o lstart=") printf 'Wed Jul 15 06:20:00 2026\n' ;;
  "-p 4242 -o command=") printf 'amp threads continue T-coordinator\n' ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", tmp)
	metadata, err := InspectProcess(4242)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.PID != 4242 || metadata.Name != "amp" || metadata.Command != "amp threads continue T-coordinator" || metadata.Identity != "Wed Jul 15 06:20:00 2026" {
		t.Fatalf("metadata = %+v", metadata)
	}
	log, _ := os.ReadFile(logPath)
	if got := string(log); !strings.Contains(got, "-o comm=") || !strings.Contains(got, "-o lstart=") || !strings.Contains(got, "-o command=") {
		t.Fatalf("ps calls = %q", got)
	}
}

func TestProcessArgsReturnsExactCurrentArgv(t *testing.T) {
	args, err := ProcessArgs(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, os.Args) {
		t.Fatalf("ProcessArgs(%d) = %#v, want %#v", os.Getpid(), args, os.Args)
	}
}

func TestProcessIdentityReturnsStableNativeStartToken(t *testing.T) {
	first, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("ProcessIdentity(%d) = %q then %q", os.Getpid(), first, second)
	}
}

func TestProcessNameReturnsStableNativeName(t *testing.T) {
	first, err := ProcessName(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProcessName(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("ProcessName(%d) = %q then %q", os.Getpid(), first, second)
	}
}

func TestInspectChildProcessesPreservesWhitespaceAndRejectsMalformedRows(t *testing.T) {
	for _, test := range []struct {
		name        string
		output      string
		processName string
		wantError   bool
	}{
		{name: "leading whitespace", output: "5252 4242\n", processName: " amp"},
		{name: "trailing whitespace", output: "5252 4242\n", processName: "amp "},
		{name: "repeated whitespace", output: "5252 4242\n", processName: "amp  helper"},
		{name: "tab whitespace", output: "5252 4242\n", processName: "amp\thelper"},
		{name: "malformed pid", output: "not-a-pid 4242\n", wantError: true},
		{name: "missing parent pid", output: "5252\n", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			writeExecutable(t, filepath.Join(bin, "ps"), "#!/bin/sh\ncat <<'EOF'\n"+test.output+"EOF\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			oldIdentity := inspectProcessIdentity
			inspectProcessIdentity = func(pid int) (string, error) { return fmt.Sprintf("start-%d", pid), nil }
			oldName := inspectProcessName
			inspectProcessName = func(int) (string, error) { return test.processName, nil }
			t.Cleanup(func() { inspectProcessIdentity, inspectProcessName = oldIdentity, oldName })

			children, err := InspectChildProcesses(4242)
			if test.wantError {
				if err == nil {
					t.Fatalf("InspectChildProcesses accepted malformed row: %+v", children)
				}
				return
			}
			if err != nil || len(children) != 1 || children[0].Name != test.processName {
				t.Fatalf("InspectChildProcesses = %+v, %v", children, err)
			}
		})
	}
}

func TestParseRestartPanesRejectsMalformedRequiredNumericMetadata(t *testing.T) {
	base := "amux\tworker\t@1\t%%1\t/tmp\tamp\tstart\t0\t%s\t%s\n"
	for _, row := range []string{
		fmt.Sprintf(base, "not-a-pid", "123"),
		fmt.Sprintf(base, "42", "not-a-time"),
	} {
		if _, err := parseRestartPanes([]byte(row)); err == nil {
			t.Fatalf("parseRestartPanes accepted %q", row)
		}
	}
	panes, err := parseRestartPanes([]byte(fmt.Sprintf(base, "42", "")))
	if err != nil || len(panes) != 1 || panes[0].PID != 42 || panes[0].StartTime != 0 {
		t.Fatalf("optional unavailable pane creation time = %+v, %v", panes, err)
	}
}

func TestDryRunWritesPlannedCommandToConfiguredOutput(t *testing.T) {
	var output strings.Builder
	runner := Runner{DryRun: true, Output: &output}

	if err := runner.NewSession("amux", "worker", "amp threads continue T-one"); err != nil {
		t.Fatal(err)
	}

	if got := output.String(); !strings.Contains(got, "tmux 'new-session' '-d' '-s' 'amux' '-n' 'worker'") {
		t.Fatalf("dry-run output = %q", got)
	}
}

func TestNewWindowUsesEmptyWindowComponentInExactSessionTarget(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" != new-window ]; then exit 2; fi
if [ "$2" = -t ] && [ "$3" = '=odd session $x:' ] && [ "$4" = -n ] && [ "$5" = worker ] && [ "$6" = 'amp --no-tui' ] && [ "$#" = 6 ]; then
  exit 0
fi
if [ "$2" = -P ] && [ "$3" = -F ] && [ "$4" = '#{window_id}' ] && [ "$5" = -t ] && [ "$6" = '=odd session $x:' ] && [ "$7" = -n ] && [ "$8" = thread ] && [ "$9" = 'amp threads continue T-one' ] && [ "$#" = 9 ]; then
  printf '@1\n'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := Runner{}
	if err := runner.NewWindow("odd session $x", "worker", "amp --no-tui"); err != nil {
		t.Fatal(err)
	}
	if windowID, err := runner.NewWindowID("odd session $x", "thread", "amp threads continue T-one"); err != nil {
		t.Fatal(err)
	} else if windowID != "@1" {
		t.Fatalf("NewWindowID() = %q, want @1", windowID)
	}
}

func TestContinueCommandQuotesShellArgs(t *testing.T) {
	got := ContinueCommand("/tmp/with space/that's", "T-'thread'")
	want := "cd '/tmp/with space/that'\\''s' && exec amp threads continue 'T-'\\''thread'\\'''"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestContinueCommandWithEnvInjectsAmuxIdentity(t *testing.T) {
	got := ContinueCommandWithEnv("/tmp/work dir", "T-thread", map[string]string{
		"AMUX_WORKSPACE": "mac",
		"AMUX_SESSION":   "Amp",
		"AMUX_WINDOW":    "worker one",
		"AMUX_THREAD_ID": "T-thread",
		"AMUX_WORKDIR":   "/tmp/work dir",
	})
	want := "cd '/tmp/work dir' && AMUX_WORKSPACE='mac' AMUX_SESSION='Amp' AMUX_WINDOW='worker one' AMUX_THREAD_ID='T-thread' AMUX_WORKDIR='/tmp/work dir' exec amp threads continue 'T-thread'"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTmuxErrorsIncludeCommandOutput(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
echo 'open terminal failed: not a terminal' >&2
exit 1
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := (Runner{}).WindowNames("Amp")
	if err == nil {
		t.Fatal("WindowNames succeeded, want tmux error")
	}
	if !strings.Contains(err.Error(), "open terminal failed: not a terminal") {
		t.Fatalf("tmux error did not include stderr: %v", err)
	}
}

func TestTmuxOutputIgnoresStderrOnSuccess(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
echo 'warning on stderr' >&2
printf 'one\ntwo\n'
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	windows, err := (Runner{}).WindowNames("Amp")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(windows, ","), "one,two"; got != want {
		t.Fatalf("got windows %q, want %q", got, want)
	}
}

func TestSendEnterUsesTmuxEnterKeyName(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = send-keys ]; then
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (Runner{}).SendEnter("@1"); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logBytes)), "send-keys -t @1 Enter"; got != want {
		t.Fatalf("SendEnter sent %q, want %q", got, want)
	}
}

func TestNotifySendsTokenAndEnterInOneTmuxInvocation(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
test "$1" = send-keys
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := (Runner{}).Notify("%16", "AMUX_REPORT group=issue-134 report=report-134"); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(log)), "send-keys -t %16 AMUX_REPORT group=issue-134 report=report-134 Enter"; got != want {
		t.Fatalf("Notify invocation = %q, want %q", got, want)
	}
}

func TestClearLineUsesTmuxControlU(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = send-keys ]; then
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (Runner{}).ClearLine("%1"); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logBytes)), "send-keys -t %1 C-u"; got != want {
		t.Fatalf("ClearLine sent %q, want %q", got, want)
	}
}

func TestCapturePaneJoinsWrappedLines(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = capture-pane ]; then
  printf 'pane text\n'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	contents, err := (Runner{}).CapturePane("%1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := contents, "pane text"; got != want {
		t.Fatalf("CapturePane returned %q, want %q", got, want)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logBytes)), "capture-pane -J -p -t %1"; got != want {
		t.Fatalf("CapturePane sent %q, want %q", got, want)
	}
}

func TestWindowPanesWithPaneIDRequiresExactReturnedSessionAndWindow(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'alpha-prefix\tworker\t@1\t%%1\texec amp threads continue T-provisioned\n'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := (Runner{}).WindowPanesWithPaneID("alpha", "worker"); err == nil || !strings.Contains(err.Error(), "exact session/window") {
		t.Fatalf("mismatched returned session error = %v", err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "list-panes -t =alpha:=worker -F #{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}\t#{pane_start_command}"
	if got := strings.TrimSpace(string(logBytes)); got != want {
		t.Fatalf("WindowPanesWithPaneID sent %q, want %q", got, want)
	}
}

func TestWindowPanesWithPaneIDSelectsOneExactWindowInMultiWindowSession(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1 $2" = "list-panes -t" ] && [ "$3" = '=alpha:=worker' ]; then
  printf 'alpha\tworker\t@1\t%%1\texec amp threads continue T-provisioned\n'
  exit 0
fi
if [ "$1" = list-panes ]; then
  printf 'alpha\tworker\t@1\t%%1\texec amp threads continue T-provisioned\n'
  printf 'alpha\trunner\t@2\t%%2\texec amp --no-tui\n'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	panes, err := (Runner{}).WindowPanesWithPaneID("alpha", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 || panes[0].Session != "alpha" || panes[0].Window != "worker" || panes[0].PaneID != "%1" {
		t.Fatalf("exact worker panes = %+v", panes)
	}
}

func TestSelectAndAttachInvokesAttachAfterSelectingWindow(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ] || [ "$1" = attach ]; then
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "select-window -t Amp:1") || !strings.Contains(log, "attach -t Amp") {
		t.Fatalf("SelectAndAttach did not select and attach\nlog:\n%s", log)
	}
}

func TestSelectAndAttachSwitchesClientWhenAlreadyInsideTmux(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ] || [ "$1" = switch-client ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), `#!/bin/sh
printf 'uwsm-app %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"tmux select-window -t Amp:1",
		"tmux switch-client -t Amp",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q\nlog:\n%s", want, log)
		}
	}
	if strings.Contains(log, "attach") || strings.Contains(log, "uwsm-app") {
		t.Fatalf("inside-tmux attach used attach/external terminal instead of switch-client\nlog:\n%s", log)
	}
}

func TestSelectAndAttachOpensOmarchyTerminalWhenTmuxCannotAttachWithoutTerminal(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ]; then
  exit 0
fi
if [ "$1" = attach ]; then
  echo 'open terminal failed: not a terminal' >&2
  exit 1
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), `#!/bin/sh
printf 'uwsm-app %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), `#!/bin/sh
printf 'xdg-terminal-exec %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "alacritty"), `#!/bin/sh
printf 'alacritty %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	log := waitForLogContaining(t, logPath, "uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp")
	for _, want := range []string{
		"tmux select-window -t Amp:1",
		"tmux attach -t Amp",
		"uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q\nlog:\n%s", want, log)
		}
	}
	if strings.Contains(log, "alacritty") {
		t.Fatalf("used alacritty despite Omarchy terminal launcher being available\nlog:\n%s", log)
	}
}

func TestSelectAndAttachFallsBackToAlacrittyWithoutOmarchyTerminalLauncher(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ]; then
  exit 0
fi
if [ "$1" = attach ]; then
  echo 'open terminal failed: not a terminal' >&2
  exit 1
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "alacritty"), `#!/bin/sh
printf 'alacritty %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp)
	t.Setenv("TMUX", "")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	log := waitForLogContaining(t, logPath, "alacritty -e tmux attach -t Amp")
	if !strings.Contains(log, "alacritty -e tmux attach -t Amp") {
		t.Fatalf("log missing alacritty fallback\nlog:\n%s", log)
	}
}

func TestSelectAndAttachUsesConfiguredTerminalLauncherBeforeDefaults(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ]; then
  exit 0
fi
if [ "$1" = attach ]; then
  echo 'open terminal failed: not a terminal' >&2
  exit 1
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "custom-terminal"), `#!/bin/sh
printf 'custom-terminal %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), `#!/bin/sh
printf 'uwsm-app %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), `#!/bin/sh
printf 'xdg-terminal-exec %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "alacritty"), `#!/bin/sh
printf 'alacritty %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := (Runner{TerminalLauncher: "custom-terminal --new-window -e"}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	log := waitForLogContaining(t, logPath, "custom-terminal --new-window -e tmux attach -t Amp")
	if !strings.Contains(log, "custom-terminal --new-window -e tmux attach -t Amp") {
		t.Fatalf("log missing configured terminal launcher\nlog:\n%s", log)
	}
	if strings.Contains(log, "uwsm-app") || strings.Contains(log, "alacritty") {
		t.Fatalf("used default fallback after configured terminal launcher started\nlog:\n%s", log)
	}
}

func TestTerminalAttachCommandsPutConfiguredLauncherBeforeOmarchyAndAlacritty(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", tmp)

	commands, err := terminalAttachCommands("Amp", "custom-terminal --class 'Amp Window' -e")
	if err != nil {
		t.Fatal(err)
	}
	got := commandStrings(commands)
	want := []string{
		"custom-terminal --class Amp Window -e tmux attach -t Amp",
		"uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp",
		"alacritty -e tmux attach -t Amp",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n got %q\nwant %q", got, want)
	}
}

func TestTerminalAttachCommandsPreserveDefaultFallbackOrder(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", tmp)

	commands, err := terminalAttachCommands("Amp", "")
	if err != nil {
		t.Fatal(err)
	}
	got := commandStrings(commands)
	want := []string{
		"uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp",
		"alacritty -e tmux attach -t Amp",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n got %q\nwant %q", got, want)
	}
}

func commandStrings(commands [][]string) []string {
	formatted := make([]string, 0, len(commands))
	for _, command := range commands {
		formatted = append(formatted, strings.Join(command, " "))
	}
	return formatted
}

func waitForLogContaining(t *testing.T, path, want string) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		logBytes, err := os.ReadFile(path)
		if err == nil {
			log := string(logBytes)
			if strings.Contains(log, want) {
				return log
			}
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				t.Fatal(err)
			}
			logBytes, _ := os.ReadFile(path)
			return string(logBytes)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
