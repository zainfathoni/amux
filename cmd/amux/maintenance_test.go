package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/result"
)

// TestMaintenanceFailurePreservesAppliedFingerprint covers failures which occur
// before Amp can safely be invoked.  These are particularly important because
// the outcome file is both a diagnostic record and the durable restart
// checkpoint for the next invocation.
func TestMaintenanceFailurePreservesAppliedFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owner  string
		target func(string) string
	}{
		{"package managed self install", "self", func(string) string { return "/home/test/.local/share/mise/installs/amp/bin/amp" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			dir := config.Directory{Path: tmp}
			amp := filepath.Join(tmp, "amp")
			if tc.owner == "self" {
				amp = filepath.Join(tmp, ".local", "share", "mise", "installs", "amp", "bin", "amp")
			}
			if err := os.MkdirAll(filepath.Dir(amp), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(amp, []byte("amp"), 0o700); err != nil {
				t.Fatal(err)
			}
			target := tc.target(amp)
			if tc.owner == "self" {
				target = amp
			}
			meta := maintenanceMetadata{SchemaVersion: 1, Owner: tc.owner, Platform: "linux", Schedule: "6h", Path: "/opt/homebrew/bin:/usr/bin", AmuxPath: "/amux", AmpPath: amp, AmpTarget: target, Artifacts: map[string]string{"/artifact": "digest"}}
			if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
				t.Fatal(err)
			}
			prior := maintenanceOutcome{SchemaVersion: 1, Status: "successful", AmpPath: amp, AmpVersion: "amp 9", ObservedFingerprint: "observed-old", AppliedFingerprint: "applied-old", AppliedVersion: "amp applied", Runners: []maintenanceRunnerOutcome{{Workdir: "/runner", Status: "successful"}}}
			if err := atomicJSON(dir.MaintenanceResultPath(), prior); err != nil {
				t.Fatal(err)
			}

			oldExec := maintenanceExec
			t.Cleanup(func() { maintenanceExec = oldExec })
			maintenanceExec = func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("preflight failure invoked a command")
				return nil, nil
			}
			in := invocation{Command: maintenanceCommand().Children[2], Path: []string{"runner", "maintenance", "run"}}
			env := result.NewEnvelope("runner maintenance run", false)
			if _, err := (app{}).runMaintenance(in, dir, &env); err == nil {
				t.Fatal("run unexpectedly succeeded")
			}
			got, err := loadMaintenanceOutcome(dir.MaintenanceResultPath())
			if err != nil {
				t.Fatal(err)
			}
			if got.AppliedFingerprint != prior.AppliedFingerprint || got.AppliedVersion != prior.AppliedVersion || got.ObservedFingerprint != prior.ObservedFingerprint || got.AmpVersion != prior.AmpVersion {
				t.Fatalf("failure destroyed prior checkpoint: got=%+v prior=%+v", got, prior)
			}
			if len(got.Runners) != 1 || got.Runners[0] != prior.Runners[0] {
				t.Fatalf("runner outcomes not retained: %+v", got.Runners)
			}
		})
	}
}

func TestMaintenanceDoctorHumanAndJSONWithNoRunners(t *testing.T) {
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	artifact := filepath.Join(tmp, "timer")
	if err := os.WriteFile(artifact, []byte("expected"), 0o600); err != nil {
		t.Fatal(err)
	}
	amp := filepath.Join(tmp, "amp")
	if err := os.WriteFile(amp, []byte("amp"), 0o700); err != nil {
		t.Fatal(err)
	}
	meta := maintenanceMetadata{SchemaVersion: 1, Owner: "external", Platform: "linux", Schedule: "6h", Path: "/opt/homebrew/bin:/usr/bin", AmuxPath: "/usr/bin/amux", AmpPath: amp, AmpTarget: amp, Artifacts: map[string]string{artifact: digest([]byte("expected"))}}
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(dir.MaintenanceResultPath(), maintenanceOutcome{SchemaVersion: 1, Status: "successful", AmpVersion: "amp 2", AppliedFingerprint: "new", ObservedFingerprint: "new", AppliedVersion: "amp 2"}); err != nil {
		t.Fatal(err)
	}
	oldExec := maintenanceExec
	t.Cleanup(func() { maintenanceExec = oldExec })
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "systemctl" {
			t.Fatalf("unexpected command %s %q", name, args)
		}
		if args[len(args)-1] == maintenanceLabel+".timer" {
			return []byte(map[bool]string{true: "enabled", false: "active"}[args[1] == "is-enabled"]), nil
		}
		return nil, errors.New("unexpected systemctl arguments")
	}

	var human bytes.Buffer
	in := invocation{Command: &commandSpec{Name: "doctor"}, Path: []string{"runner", "doctor"}, Selectors: selectors{All: true}}
	if _, err := (app{stdout: &human}).executeRunner(in, dir); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"owner=external", "schedule=6h", "version=amp 2", artifact} {
		if !strings.Contains(human.String(), want) {
			t.Errorf("human doctor missing %q: %s", want, human.String())
		}
	}

	// Drift must be a failed outcome and a non-zero operation, not successful
	// output carrying a hidden MaintenanceDetails.Error.
	if err := os.WriteFile(artifact, []byte("drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	jsonIn := in
	jsonIn.Options.JSON = true
	env, err := (app{}).executeRunner(jsonIn, dir)
	if err == nil || result.ExitCode(err) == 0 {
		t.Fatalf("drifted doctor err=%v", err)
	}
	if result.ErrorKindOf(err) != result.ErrorRuntime || len(env.Failed) != 1 || len(env.Successful) != 0 || env.Failed[0].Error == nil || env.Failed[0].Error.Kind != result.ErrorRuntime || env.Failed[0].Maintenance == nil || !strings.Contains(env.Failed[0].Maintenance.Error, "drift") {
		t.Fatalf("drift was not classified once as failed: %+v", env)
	}
	encoded, err := json.Marshal(env)
	if err != nil || !bytes.Contains(encoded, []byte(`"maintenance"`)) || !bytes.Contains(encoded, []byte(`"applied_version":"amp 2"`)) {
		t.Fatalf("JSON maintenance missing: %s, %v", encoded, err)
	}
}

func TestRunnerDoctorFailsForIncompleteMaintenanceCheckpoint(t *testing.T) {
	oldExec := maintenanceExec
	t.Cleanup(func() { maintenanceExec = oldExec })
	maintenanceExec = healthyMaintenanceScheduler

	for _, tc := range []struct {
		name    string
		outcome *maintenanceOutcome
		want    string
	}{
		{name: "failed pending runner without top-level error", outcome: &maintenanceOutcome{SchemaVersion: 1, Status: "failed", Runners: []maintenanceRunnerOutcome{{Workdir: "/runner", Status: "failed", Phase: "pending_launch"}}}, want: "pending_launch"},
		{name: "installed metadata without outcome", want: "has no outcome"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := maintenanceDoctorFixture(t, tc.outcome)
			in := invocation{Command: &commandSpec{Name: "doctor"}, Path: []string{"runner", "doctor"}, Selectors: selectors{All: true}, Options: cliOptions{JSON: true}}
			env, err := (app{}).executeRunner(in, dir)
			if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure || result.ErrorKindOf(err) != result.ErrorRuntime {
				t.Fatalf("runner doctor err=%v exit=%d", err, result.ExitCode(err))
			}
			if len(env.Failed) != 1 || env.Failed[0].Maintenance == nil || !strings.Contains(env.Failed[0].Maintenance.Error, tc.want) || !strings.Contains(env.Failed[0].Maintenance.Error, "amux runner maintenance run") {
				t.Fatalf("runner doctor diagnostic = %+v", env)
			}
		})
	}
}

func maintenanceDoctorFixture(t *testing.T, outcome *maintenanceOutcome) config.Directory {
	t.Helper()
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	artifact := filepath.Join(tmp, "timer")
	contents := []byte("expected")
	if err := os.WriteFile(artifact, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	meta := maintenanceMetadata{SchemaVersion: 1, Owner: "external", Platform: "linux", Schedule: "6h", Path: "/usr/bin", AmuxPath: "/usr/bin/amux", AmpPath: "/usr/bin/amp", AmpTarget: "/usr/bin/amp", Artifacts: map[string]string{artifact: digest(contents)}}
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		t.Fatal(err)
	}
	if outcome != nil {
		if err := atomicJSON(dir.MaintenanceResultPath(), *outcome); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func healthyMaintenanceScheduler(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "systemctl" || len(args) < 2 {
		return nil, fmt.Errorf("unexpected scheduler command %s %q", name, args)
	}
	if args[1] == "is-enabled" {
		return []byte("enabled"), nil
	}
	return []byte("active"), nil
}

func TestSystemdMaintenanceArtifacts(t *testing.T) {
	path := "/opt/homebrew/bin:/tmp/a path/$cash/%spec"
	service, timer := systemdMaintenanceArtifacts("/opt/amux bin/amux", "/tmp/config dir", path)
	for _, want := range []string{"Environment=\"PATH=/opt/homebrew/bin:/tmp/a path/$cash/%%spec\"", "ExecStart=\"/opt/amux bin/amux\" --config-dir \"/tmp/config dir\" runner maintenance run --scheduled", "OnCalendar=*-*-* 00/6:00:00", "Persistent=true", "RandomizedDelaySec=30m"} {
		if !strings.Contains(service+timer, want) {
			t.Fatalf("artifact missing %q:\n%s%s", want, service, timer)
		}
	}
}

func TestMaintenancePathsHonorsXDGConfigHomeAndFallsBackToHome(t *testing.T) {
	oldGOOS, oldHome, oldUserConfigDir := maintenanceGOOS, maintenanceHome, maintenanceUserConfigDir
	t.Cleanup(func() {
		maintenanceGOOS, maintenanceHome, maintenanceUserConfigDir = oldGOOS, oldHome, oldUserConfigDir
	})
	maintenanceGOOS = "linux"
	maintenanceHome = func() (string, error) { return "/fallback-home", nil }
	xdgConfig := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	maintenanceUserConfigDir = func() (string, error) { return os.Getenv("XDG_CONFIG_HOME"), nil }
	service, timer, err := maintenancePaths()
	wantXDG := filepath.Join(xdgConfig, "systemd", "user")
	if err != nil || filepath.Dir(service) != wantXDG || filepath.Dir(timer) != wantXDG {
		t.Fatalf("XDG paths = %q, %q, err=%v", service, timer, err)
	}
	maintenanceUserConfigDir = func() (string, error) { return "", errors.New("unavailable") }
	service, timer, err = maintenancePaths()
	if err != nil || filepath.Dir(service) != "/fallback-home/.config/systemd/user" || filepath.Dir(timer) != "/fallback-home/.config/systemd/user" {
		t.Fatalf("fallback paths = %q, %q, err=%v", service, timer, err)
	}
}

func TestMaintenanceRandomBoundsAndInjectableSeam(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := maintenanceRandom(7); got < 0 || got >= 7 {
			t.Fatalf("maintenanceRandom(7) = %d", got)
		}
	}
	if got := maintenanceRandom(0); got != 0 {
		t.Fatalf("maintenanceRandom(0) = %d", got)
	}
	oldRandom := maintenanceRandom
	t.Cleanup(func() { maintenanceRandom = oldRandom })
	maintenanceRandom = func(n int64) int64 {
		if n != 9 {
			t.Fatalf("seam bound = %d", n)
		}
		return 4
	}
	if got := maintenanceRandom(9); got != 4 {
		t.Fatalf("seam result = %d", got)
	}
}

func TestSystemdQuoteEscapesCStyleAndSpecifiers(t *testing.T) {
	got := systemdQuote("/tmp/a'post % $ \\ \"\n")
	if got != "\"/tmp/a'post %% $$ \\\\ \\\"\\n\"" {
		t.Fatalf("systemdQuote() = %q", got)
	}
}

func TestLaunchAgentMaintenanceArtifact(t *testing.T) {
	plist := launchAgentMaintenanceArtifact("/Applications/amux & tools/amux", "/tmp/a<b", "/opt/homebrew/bin:/tmp/a & b/$cash/%spec")
	decoder := xml.NewDecoder(strings.NewReader(plist))
	for {
		if _, err := decoder.Token(); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("generated LaunchAgent is not valid XML: %v\n%s", err, plist)
		}
	}
	for _, want := range []string{"<integer>0</integer>", "<integer>6</integer>", "<integer>12</integer>", "<integer>18</integer>", "<string>--scheduled</string>", "amux &amp; tools", "a&lt;b", "<key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/homebrew/bin:/tmp/a &amp; b/$cash/%spec</string>"} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing %q:\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "KeepAlive") {
		t.Fatal("LaunchAgent must not KeepAlive")
	}
	if !strings.Contains(plist, "<key>RunAtLoad</key><true/>") {
		t.Fatal("LaunchAgent must catch up at login")
	}
}

func TestMaintenanceCLIContract(t *testing.T) {
	parsed, err := parseInvocation([]string{"runner", "maintenance", "install", "--update-owner", "external"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.MaintenanceOwner != "external" || strings.Join(parsed.Path, " ") != "runner maintenance install" {
		t.Fatalf("unexpected parse: %#v", parsed)
	}
	if _, err := parseInvocation([]string{"runner", "refresh"}); err == nil {
		t.Fatal("refresh alias unexpectedly accepted")
	}
	for _, args := range [][]string{
		{"runner", "maintenance", "remove", "--scheduled"},
		{"runner", "maintenance", "run", "--update-owner", "self"},
		{"runner", "maintenance", "run", "--scheduled", "--scheduled"},
		{"runner", "list", "--scheduled"},
	} {
		if _, err := parseInvocation(args); err == nil {
			t.Errorf("parseInvocation(%q) unexpectedly succeeded", args)
		}
	}
}

func TestExternalMaintenanceBaselineAndFailurePreserveAppliedFingerprint(t *testing.T) {
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	amp := filepath.Join(tmp, "amp")
	if err := os.WriteFile(amp, []byte("first"), 0o700); err != nil {
		t.Fatal(err)
	}
	meta := maintenanceMetadata{SchemaVersion: 1, Owner: "external", Platform: "linux", Schedule: "6h", Path: "/opt/homebrew/bin:/usr/bin", AmuxPath: "/amux", AmpPath: amp, AmpTarget: amp, Artifacts: map[string]string{"/artifact": "digest"}}
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		t.Fatal(err)
	}
	oldExec := maintenanceExec
	t.Cleanup(func() { maintenanceExec = oldExec })
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != amp || len(args) != 1 || args[0] != "version" {
			t.Fatalf("unexpected command: %s %q", name, args)
		}
		return []byte("amp 1\n"), nil
	}
	in := invocation{Command: maintenanceCommand().Children[2], Path: []string{"runner", "maintenance", "run"}}
	env := result.NewEnvelope("runner maintenance run", false)
	if _, err := (app{}).runMaintenance(in, dir, &env); err != nil {
		t.Fatal(err)
	}
	baseline, err := loadMaintenanceOutcome(dir.MaintenanceResultPath())
	if err != nil || baseline.Status != "skipped" || baseline.AppliedFingerprint == "" || baseline.ObservedFingerprint != baseline.AppliedFingerprint {
		t.Fatalf("baseline = %+v, err=%v", baseline, err)
	}
	if err := os.WriteFile(amp, []byte("second"), 0o700); err != nil {
		t.Fatal(err)
	}
	maintenanceExec = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("broken"), errors.New("exit 1")
	}
	env = result.NewEnvelope("runner maintenance run", false)
	if _, err := (app{}).runMaintenance(in, dir, &env); err == nil {
		t.Fatal("version failure unexpectedly succeeded")
	}
	failed, err := loadMaintenanceOutcome(dir.MaintenanceResultPath())
	if err != nil || failed.Status != "failed" || failed.AppliedFingerprint != baseline.AppliedFingerprint || failed.ObservedFingerprint == baseline.ObservedFingerprint {
		t.Fatalf("failed outcome lost durable baseline: %+v, err=%v", failed, err)
	}
}

func TestMaintenanceRunDryRunHasNoCommandOrWrite(t *testing.T) {
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	amp := filepath.Join(tmp, "amp")
	if err := os.WriteFile(amp, []byte("amp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(dir.MaintenancePath(), maintenanceMetadata{SchemaVersion: 1, Owner: "external", Platform: "linux", Schedule: "6h", Path: "/usr/bin", AmuxPath: "/amux", AmpPath: amp, AmpTarget: amp, Artifacts: map[string]string{"/a": "b"}}); err != nil {
		t.Fatal(err)
	}
	oldExec := maintenanceExec
	t.Cleanup(func() { maintenanceExec = oldExec })
	maintenanceExec = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("dry-run executed command")
		return nil, nil
	}
	in := invocation{Options: cliOptions{DryRun: true}, Command: maintenanceCommand().Children[2], Path: []string{"runner", "maintenance", "run"}}
	env := result.NewEnvelope("runner maintenance run", true)
	if _, err := (app{}).runMaintenance(in, dir, &env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir.MaintenanceResultPath()); !os.IsNotExist(err) {
		t.Fatalf("dry-run result stat = %v", err)
	}
}

func setMaintenanceIntegrationSeams(t *testing.T, goos, home, amux, amp string) {
	t.Helper()
	oldGOOS, oldHome, oldUserConfigDir, oldLookPath, oldExec := maintenanceGOOS, maintenanceHome, maintenanceUserConfigDir, maintenanceLookPath, maintenanceExec
	oldExecutable, oldCanonical := executablePath, canonicalSelfUpdatePath
	maintenanceGOOS = goos
	maintenanceHome = func() (string, error) { return home, nil }
	maintenanceUserConfigDir = func() (string, error) { return filepath.Join(home, ".config"), nil }
	maintenanceLookPath = func(name string) (string, error) {
		if name != "amp" {
			return "", fmt.Errorf("unexpected lookup %q", name)
		}
		return amp, nil
	}
	executablePath = func() (string, error) { return amux, nil }
	canonicalSelfUpdatePath = func() (string, error) { return amux, nil }
	t.Cleanup(func() {
		maintenanceGOOS, maintenanceHome, maintenanceUserConfigDir, maintenanceLookPath, maintenanceExec = oldGOOS, oldHome, oldUserConfigDir, oldLookPath, oldExec
		executablePath, canonicalSelfUpdatePath = oldExecutable, oldCanonical
	})
}

func executableFixture(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test executable\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeMaintenanceJSON(t *testing.T, dir string, args ...string) (result.Envelope, error) {
	t.Helper()
	var stdout bytes.Buffer
	all := append([]string{"--json", "--config-dir", dir}, args...)
	err := (app{stdout: &stdout}).execute(all)
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatalf("decode JSON %q: %v (output %q, operation error %v)", all, decodeErr, stdout.String(), err)
	}
	return env, err
}

func TestMaintenanceInstallSystemdDryRunJSONAndNoEffects(t *testing.T) {
	root, configDir, home := t.TempDir(), t.TempDir(), t.TempDir()
	amux := executableFixture(t, filepath.Join(root, "amux"))
	amp := executableFixture(t, filepath.Join(root, "amp"))
	setMaintenanceIntegrationSeams(t, "linux", home, amux, amp)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	maintenanceExec = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("dry-run executed a command")
		return nil, nil
	}

	env, err := executeMaintenanceJSON(t, configDir, "--dry-run", "runner", "maintenance", "install", "--update-owner", "external")
	if err != nil || len(env.Planned) != 1 || len(env.Successful) != 0 || len(env.Failed) != 0 {
		t.Fatalf("dry-run env=%+v err=%v", env, err)
	}
	service, timer := systemdMaintenanceArtifacts(amux, configDir, os.Getenv("PATH"))
	paths := []string{filepath.Join(home, ".config/systemd/user", maintenanceLabel+".service"), filepath.Join(home, ".config/systemd/user", maintenanceLabel+".timer")}
	if !reflect.DeepEqual(env.Planned[0].Maintenance.ArtifactPaths, paths) {
		t.Fatalf("artifact paths=%q want=%q", env.Planned[0].Maintenance.ArtifactPaths, paths)
	}
	if env.Planned[0].Maintenance.Owner != "external" || env.Planned[0].Maintenance.AmuxPath != amux || env.Planned[0].Maintenance.AmpPath != amp {
		t.Fatalf("details=%+v", env.Planned[0].Maintenance)
	}
	for _, p := range append(paths, filepath.Join(configDir, config.MaintenanceFile)) {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Fatalf("dry-run created %s: %v", p, statErr)
		}
	}
	if service == "" || timer == "" {
		t.Fatal("expected exact nonempty systemd artifacts")
	}
}

func TestMaintenanceInstallSystemdCommandsAndIdempotentRetry(t *testing.T) {
	root, configDir, home := t.TempDir(), t.TempDir(), t.TempDir()
	amux := executableFixture(t, filepath.Join(root, "amux"))
	amp := executableFixture(t, filepath.Join(root, "amp"))
	setMaintenanceIntegrationSeams(t, "linux", home, amux, amp)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var commands []string
	fail := true
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == amp {
			return []byte("amp 1\n"), nil
		}
		if fail {
			fail = false
			return []byte("reload failed"), errors.New("exit 1")
		}
		return nil, nil
	}
	_, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external")
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("first install err=%v exit=%d", err, result.ExitCode(err))
	}
	for i := 0; i < 2; i++ {
		env, retryErr := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external")
		if retryErr != nil || len(env.Successful) != 1 {
			t.Fatalf("install %d env=%+v err=%v", i, env, retryErr)
		}
	}
	wantCommands := []string{amp + " version", "systemctl --user daemon-reload", "systemctl --user daemon-reload", "systemctl --user enable --now " + maintenanceLabel + ".timer", "systemctl --user daemon-reload", "systemctl --user enable --now " + maintenanceLabel + ".timer"}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands=%q want=%q", commands, wantCommands)
	}
	s, timer := systemdMaintenanceArtifacts(amux, configDir, os.Getenv("PATH"))
	servicePath := filepath.Join(home, ".config/systemd/user", maintenanceLabel+".service")
	timerPath := filepath.Join(home, ".config/systemd/user", maintenanceLabel+".timer")
	for path, want := range map[string]string{servicePath: s, timerPath: timer} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("artifact %s=%q err=%v want=%q", path, got, readErr, want)
		}
	}
	m, loadErr := loadMaintenance(filepath.Join(configDir, config.MaintenanceFile))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if m.Owner != "external" || m.Artifacts[servicePath] != digest([]byte(s)) || m.Artifacts[timerPath] != digest([]byte(timer)) {
		t.Fatalf("metadata=%+v", m)
	}
}

func TestMaintenanceReinstallReplacesOwnedArtifactAfterPATHChange(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			root, configDir, home := t.TempDir(), t.TempDir(), t.TempDir()
			amux := executableFixture(t, filepath.Join(root, "amux"))
			amp := executableFixture(t, filepath.Join(root, "amp"))
			setMaintenanceIntegrationSeams(t, goos, home, amux, amp)
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			loaded := false
			var launchCommands []string
			maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == amp {
					return []byte("amp 1\n"), nil
				}
				if name == "launchctl" {
					launchCommands = append(launchCommands, args[0])
				}
				if name == "launchctl" && args[0] == "print" {
					if loaded {
						return []byte("loaded"), nil
					}
					return []byte("not loaded"), errors.New("exit 113")
				}
				if name == "launchctl" && args[0] == "bootout" {
					loaded = false
				}
				if name == "launchctl" && args[0] == "bootstrap" {
					loaded = true
				}
				return nil, nil
			}

			t.Setenv("PATH", "/first/bin:/usr/bin")
			if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external"); err != nil {
				t.Fatal(err)
			}
			dir := config.Directory{Path: configDir}
			prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", AppliedFingerprint: "keep", Runners: []maintenanceRunnerOutcome{{Workdir: "/runner", Status: "failed", Phase: "pending_launch"}}}
			if err := atomicJSON(dir.MaintenanceResultPath(), prior); err != nil {
				t.Fatal(err)
			}

			t.Setenv("PATH", "/second/bin:/usr/bin")
			if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "self"); err != nil {
				t.Fatal(err)
			}
			meta, err := loadMaintenance(dir.MaintenancePath())
			if err != nil || meta.Path != "/second/bin:/usr/bin" || meta.Owner != "self" {
				t.Fatalf("metadata=%+v err=%v", meta, err)
			}
			if goos == "darwin" {
				want := []string{"print", "bootstrap", "print", "print", "bootout", "print", "bootstrap", "print"}
				if !reflect.DeepEqual(launchCommands, want) {
					t.Fatalf("changed PATH launchctl commands=%q want=%q", launchCommands, want)
				}
			}
			for path, wantDigest := range meta.Artifacts {
				contents, readErr := os.ReadFile(path)
				if readErr != nil || digest(contents) != wantDigest || !strings.Contains(string(contents), "/second/bin:/usr/bin") && strings.HasSuffix(path, ".service") {
					t.Fatalf("artifact %s was not updated: %q err=%v", path, contents, readErr)
				}
			}
			preserved, err := loadMaintenanceOutcome(dir.MaintenanceResultPath())
			if err != nil || !reflect.DeepEqual(preserved, prior) {
				t.Fatalf("outcome changed: got=%+v want=%+v err=%v", preserved, prior, err)
			}

			for path := range meta.Artifacts {
				if strings.HasSuffix(path, ".timer") {
					continue
				}
				if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
				break
			}
			t.Setenv("PATH", "/third/bin:/usr/bin")
			if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external"); err == nil || result.ExitCode(err) != result.ExitRejected {
				t.Fatalf("tampered reinstall err=%v", err)
			}
		})
	}
}

func TestMaintenanceRemoveSystemdRetryAfterReloadFailureConverges(t *testing.T) {
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	service, timer := filepath.Join(tmp, "maintenance.service"), filepath.Join(tmp, "maintenance.timer")
	artifacts := map[string]string{}
	for _, path := range []string{service, timer} {
		contents := []byte(path)
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		artifacts[path] = digest(contents)
	}
	meta := maintenanceMetadata{SchemaVersion: 1, Owner: "external", Platform: "linux", Schedule: "6h", Path: "/usr/bin", AmuxPath: "/amux", AmpPath: "/amp", AmpTarget: "/amp", Artifacts: artifacts}
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(dir.MaintenanceResultPath(), maintenanceOutcome{SchemaVersion: 1, Status: "skipped"}); err != nil {
		t.Fatal(err)
	}
	var commands []string
	reloadFailed := false
	oldExec := maintenanceExec
	t.Cleanup(func() { maintenanceExec = oldExec })
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		commands = append(commands, command)
		if args[1] == "daemon-reload" && !reloadFailed {
			reloadFailed = true
			return []byte("reload failed"), errors.New("exit 1")
		}
		if reloadFailed && (args[1] == "stop" || args[1] == "disable") {
			return []byte("Unit maintenance.timer does not exist"), errors.New("exit 5")
		}
		return nil, nil
	}
	in := invocation{Command: maintenanceCommand().Children[1], Path: []string{"runner", "maintenance", "remove"}}
	if _, err := (app{}).removeMaintenance(in, dir, ptrEnvelope(result.NewEnvelope("runner maintenance remove", false))); err == nil {
		t.Fatal("first removal unexpectedly succeeded")
	}
	if _, err := (app{}).removeMaintenance(in, dir, ptrEnvelope(result.NewEnvelope("runner maintenance remove", false))); err != nil {
		t.Fatalf("retry removal: %v (commands %q)", err, commands)
	}
	for _, path := range []string{service, timer, dir.MaintenancePath(), dir.MaintenanceResultPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", path, err)
		}
	}
}

func TestMaintenanceRemoveOutcomeWithoutMetadata(t *testing.T) {
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	if err := atomicJSON(dir.MaintenanceResultPath(), maintenanceOutcome{SchemaVersion: 1, Status: "successful", AmpPath: "/amp"}); err != nil {
		t.Fatal(err)
	}
	in := invocation{Command: maintenanceCommand().Children[1], Path: []string{"runner", "maintenance", "remove"}}
	dryRun := in
	dryRun.Options.DryRun = true
	dryEnv := result.NewEnvelope("runner maintenance remove", true)
	if _, err := (app{}).removeMaintenance(dryRun, dir, &dryEnv); err != nil {
		t.Fatal(err)
	}
	if len(dryEnv.Planned) != 1 || dryEnv.Planned[0].Resource.Path != dir.MaintenanceResultPath() {
		t.Fatalf("dry-run did not plan orphan cleanup: %+v", dryEnv)
	}
	if _, err := os.Stat(dir.MaintenanceResultPath()); err != nil {
		t.Fatalf("dry-run removed result: %v", err)
	}

	env := result.NewEnvelope("runner maintenance remove", false)
	if _, err := (app{}).removeMaintenance(in, dir, &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Successful) != 1 || env.Successful[0].Resource.Path != dir.MaintenanceResultPath() {
		t.Fatalf("orphan cleanup was not successful: %+v", env)
	}
	if _, err := os.Stat(dir.MaintenanceResultPath()); !os.IsNotExist(err) {
		t.Fatalf("result remains after cleanup: %v", err)
	}
	repeat := result.NewEnvelope("runner maintenance remove", false)
	if _, err := (app{}).removeMaintenance(in, dir, &repeat); err != nil || len(repeat.Skipped) != 1 {
		t.Fatalf("repeated remove did not skip: env=%+v err=%v", repeat, err)
	}
}

func TestMaintenanceRemoveOutcomeDeletionFailureIsRuntime(t *testing.T) {
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	if err := os.MkdirAll(filepath.Join(dir.MaintenanceResultPath(), "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	in := invocation{Command: maintenanceCommand().Children[1], Path: []string{"runner", "maintenance", "remove"}}
	_, err := (app{}).removeMaintenance(in, dir, ptrEnvelope(result.NewEnvelope("runner maintenance remove", false)))
	if err == nil || result.ErrorKindOf(err) != result.ErrorRuntime {
		t.Fatalf("result deletion failure = %v kind=%s", err, result.ErrorKindOf(err))
	}
}

func TestMaintenanceRemoveOwnsPartialFirstInstallWhileActivationPending(t *testing.T) {
	tmp := t.TempDir()
	dir := config.Directory{Path: tmp}
	service := filepath.Join(tmp, "maintenance.service")
	timer := filepath.Join(tmp, "maintenance.timer")
	desired := []byte("new service")
	if err := os.WriteFile(service, desired, 0o600); err != nil {
		t.Fatal(err)
	}
	meta := maintenanceMetadata{SchemaVersion: 1, ActivationPending: true, Owner: "external", Platform: "linux", Schedule: "6h", Path: "/usr/bin", AmuxPath: "/amux", AmpPath: "/amp", AmpTarget: "/amp", Artifacts: map[string]string{service: digest(desired), timer: digest([]byte("new timer"))}}
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		t.Fatal(err)
	}
	oldExec := maintenanceExec
	t.Cleanup(func() { maintenanceExec = oldExec })
	maintenanceExec = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	in := invocation{Command: maintenanceCommand().Children[1], Path: []string{"runner", "maintenance", "remove"}}
	if _, err := (app{}).removeMaintenance(in, dir, ptrEnvelope(result.NewEnvelope("runner maintenance remove", false))); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{service, timer, dir.MaintenancePath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("partial install path %s remains: %v", path, err)
		}
	}
}

func TestMaintenancePendingMixedSystemdArtifactsAcceptAnotherPATHChange(t *testing.T) {
	root, configDir, home := t.TempDir(), t.TempDir(), t.TempDir()
	amux := executableFixture(t, filepath.Join(root, "amux"))
	amp := executableFixture(t, filepath.Join(root, "amp"))
	setMaintenanceIntegrationSeams(t, "linux", home, amux, amp)
	dir := config.Directory{Path: configDir}
	servicePath, timerPath, err := maintenancePaths()
	if err != nil {
		t.Fatal(err)
	}
	oldService, oldTimer := systemdMaintenanceArtifacts(amux, configDir, "/old/bin")
	pendingService, pendingTimer := systemdMaintenanceArtifacts(amux, configDir, "/pending/bin")
	if err := atomicWrite(servicePath, []byte(pendingService), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(timerPath, []byte(oldTimer), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := maintenanceMetadata{SchemaVersion: 1, ActivationPending: true, Owner: "external", Platform: "linux", Schedule: "6h", Path: "/pending/bin", AmuxPath: amux, AmpPath: amp, AmpTarget: amp, Artifacts: map[string]string{servicePath: digest([]byte(pendingService)), timerPath: digest([]byte(pendingTimer))}, PreviousArtifacts: map[string]string{servicePath: digest([]byte(oldService)), timerPath: digest([]byte(oldTimer))}}
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(dir.MaintenanceResultPath(), maintenanceOutcome{SchemaVersion: 1, Status: "skipped"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/newest/bin")
	maintenanceExec = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external"); err != nil {
		t.Fatal(err)
	}
	got, err := loadMaintenance(dir.MaintenancePath())
	if err != nil || got.ActivationPending || len(got.PreviousArtifacts) != 0 || got.Path != "/newest/bin" {
		t.Fatalf("completed metadata=%+v err=%v", got, err)
	}
}

func ptrEnvelope(env result.Envelope) *result.Envelope { return &env }

func TestMaintenanceInstallRejectsSelfManagedSymlinkBeforeEffectsAndExternalAllows(t *testing.T) {
	root, configDir, home := t.TempDir(), t.TempDir(), t.TempDir()
	amux := executableFixture(t, filepath.Join(root, "amux"))
	target := executableFixture(t, filepath.Join(root, ".local/share/mise/installs/amp/1/bin/amp"))
	link := filepath.Join(root, "bin/amp")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	setMaintenanceIntegrationSeams(t, "linux", home, amux, link)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var commands int
	maintenanceExec = func(context.Context, string, ...string) ([]byte, error) { commands++; return nil, nil }
	env, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "self")
	if err == nil || result.ExitCode(err) != result.ExitRejected || len(env.Failed) != 1 || !strings.Contains(env.Failed[0].Error.Message, "package/toolchain-managed") {
		t.Fatalf("self env=%+v err=%v", env, err)
	}
	if commands != 0 {
		t.Fatalf("rejected install ran %d commands", commands)
	}
	if _, statErr := os.Stat(filepath.Join(configDir, config.MaintenanceFile)); !os.IsNotExist(statErr) {
		t.Fatalf("rejected install wrote metadata: %v", statErr)
	}
	env, err = executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external")
	if err != nil || len(env.Successful) != 1 || commands != 3 {
		t.Fatalf("external env=%+v err=%v commands=%d", env, err, commands)
	}
	m, err := loadMaintenance(filepath.Join(configDir, config.MaintenanceFile))
	if err != nil || m.AmpPath != link || m.AmpTarget != resolvePathForComparison(target) {
		t.Fatalf("metadata=%+v err=%v", m, err)
	}
}

func TestMaintenanceLaunchAgentInstallIdempotentAndRemoveFailureRetainsState(t *testing.T) {
	root, configDir, home := t.TempDir(), t.TempDir(), t.TempDir()
	amux := executableFixture(t, filepath.Join(root, "amux"))
	amp := executableFixture(t, filepath.Join(root, "amp"))
	setMaintenanceIntegrationSeams(t, "darwin", home, amux, amp)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	loaded := false
	failBootout := false
	var commands []string
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == amp {
			return []byte("amp 1\n"), nil
		}
		if name != "launchctl" {
			return nil, fmt.Errorf("unexpected command %s", name)
		}
		switch args[0] {
		case "print":
			if loaded {
				return []byte("loaded"), nil
			}
			return []byte("not loaded"), errors.New("exit 113")
		case "bootstrap":
			loaded = true
			return nil, nil
		case "bootout":
			if failBootout {
				return []byte("permission denied"), errors.New("exit 1")
			}
			loaded = false
			return nil, nil
		}
		return nil, errors.New("unexpected launchctl arguments")
	}
	for i := 0; i < 2; i++ {
		env, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external")
		if err != nil || len(env.Successful) != 1 {
			t.Fatalf("install %d env=%+v err=%v", i, env, err)
		}
	}
	plist := filepath.Join(home, "Library/LaunchAgents", maintenanceLabel+".plist")
	want := launchAgentMaintenanceArtifact(amux, configDir, os.Getenv("PATH"))
	got, err := os.ReadFile(plist)
	if err != nil || string(got) != want {
		t.Fatalf("plist=%q err=%v want=%q", got, err, want)
	}
	if len(commands) != 5 || !strings.Contains(commands[1], " print ") || !strings.Contains(commands[2], " bootstrap ") || !strings.Contains(commands[3], " print ") || !strings.Contains(commands[4], " print ") {
		t.Fatalf("commands=%q", commands)
	}
	failBootout = true
	env, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "remove")
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure || len(env.Failed) != 1 {
		t.Fatalf("remove env=%+v err=%v", env, err)
	}
	if b, readErr := os.ReadFile(plist); readErr != nil || string(b) != want {
		t.Fatalf("failed remove changed plist: %q %v", b, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(configDir, config.MaintenanceFile)); statErr != nil {
		t.Fatalf("failed remove lost metadata: %v", statErr)
	}
}

func TestMaintenanceLaunchAgentRemoveUsesInstalledArtifactAfterHomeChanges(t *testing.T) {
	root, configDir, oldHome := t.TempDir(), t.TempDir(), t.TempDir()
	amux := executableFixture(t, filepath.Join(root, "amux"))
	amp := executableFixture(t, filepath.Join(root, "amp"))
	setMaintenanceIntegrationSeams(t, "darwin", oldHome, amux, amp)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	loaded := false
	var bootoutPath string
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == amp {
			return []byte("amp 1\n"), nil
		}
		switch args[0] {
		case "print":
			if loaded {
				return []byte("loaded"), nil
			}
			return []byte("not loaded"), errors.New("exit 113")
		case "bootstrap":
			loaded = true
			return nil, nil
		case "bootout":
			bootoutPath = args[2]
			loaded = false
			return nil, nil
		}
		return nil, errors.New("unexpected launchctl arguments")
	}
	if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external"); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(oldHome, "Library/LaunchAgents", maintenanceLabel+".plist")
	newHome := t.TempDir()
	maintenanceHome = func() (string, error) { return newHome, nil }
	env, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "remove")
	if err != nil || len(env.Successful) != 1 {
		t.Fatalf("remove env=%+v err=%v", env, err)
	}
	if bootoutPath != installedPath {
		t.Fatalf("bootout path=%q want installed artifact %q", bootoutPath, installedPath)
	}
	for _, path := range []string{installedPath, filepath.Join(configDir, config.MaintenanceFile), filepath.Join(configDir, config.MaintenanceResultFile)} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("remove did not converge; %s stat error=%v", path, statErr)
		}
	}
}

func TestMaintenanceLaunchAgentChangedPlistReloadIsVerifiedAndRetrySafe(t *testing.T) {
	for _, failAt := range []string{"bootout", "bootstrap", "verify"} {
		t.Run(failAt, func(t *testing.T) {
			root, configDir, home := t.TempDir(), t.TempDir(), t.TempDir()
			amux := executableFixture(t, filepath.Join(root, "amux"))
			amp := executableFixture(t, filepath.Join(root, "amp"))
			setMaintenanceIntegrationSeams(t, "darwin", home, amux, amp)
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			loaded, armed, failed := false, false, false
			var commands []string
			maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == amp {
					return []byte("amp 1\n"), nil
				}
				commands = append(commands, strings.Join(args, " "))
				switch args[0] {
				case "print":
					if armed && failAt == "verify" && !failed && loaded {
						failed = true
						return []byte("verification failed"), errors.New("exit 1")
					}
					if loaded {
						return []byte("loaded"), nil
					}
					return []byte("not loaded"), errors.New("exit 113")
				case "bootout":
					if armed && failAt == "bootout" && !failed {
						failed = true
						return []byte("bootout failed"), errors.New("exit 1")
					}
					loaded = false
					return nil, nil
				case "bootstrap":
					if armed && failAt == "bootstrap" && !failed {
						failed = true
						return []byte("bootstrap failed"), errors.New("exit 1")
					}
					loaded = true
					return nil, nil
				}
				return nil, errors.New("unexpected launchctl arguments")
			}

			t.Setenv("PATH", "/first/bin:/usr/bin")
			if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external"); err != nil {
				t.Fatal(err)
			}
			dir := config.Directory{Path: configDir}
			oldMeta, err := loadMaintenance(dir.MaintenancePath())
			if err != nil {
				t.Fatal(err)
			}
			priorOutcome, err := os.ReadFile(dir.MaintenanceResultPath())
			if err != nil {
				t.Fatal(err)
			}

			commands, armed = nil, true
			t.Setenv("PATH", "/second/bin:/usr/bin")
			if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external"); err == nil {
				t.Fatal("changed install unexpectedly succeeded")
			}
			plist := filepath.Join(home, "Library/LaunchAgents", maintenanceLabel+".plist")
			newBytes, err := os.ReadFile(plist)
			if err != nil || digest(newBytes) == oldMeta.Artifacts[plist] || !strings.Contains(string(newBytes), "/second/bin:/usr/bin") {
				t.Fatalf("new retry artifact=%q err=%v", newBytes, err)
			}
			pending, err := loadMaintenance(dir.MaintenancePath())
			if err != nil || !pending.ActivationPending || pending.Path != "/second/bin:/usr/bin" || pending.Artifacts[plist] != digest(newBytes) {
				t.Fatalf("metadata after failure=%+v err=%v", pending, err)
			}
			if got, err := os.ReadFile(dir.MaintenanceResultPath()); err != nil || !reflect.DeepEqual(got, priorOutcome) {
				t.Fatalf("outcome changed after failure: %q err=%v", got, err)
			}

			commands, armed = nil, false
			if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "install", "--update-owner", "external"); err != nil {
				t.Fatalf("retry: %v (commands %q)", err, commands)
			}
			meta, err := loadMaintenance(dir.MaintenancePath())
			if err != nil || meta.ActivationPending || meta.Artifacts[plist] != digest(newBytes) {
				t.Fatalf("retry metadata=%+v err=%v", meta, err)
			}
			if got, err := os.ReadFile(dir.MaintenanceResultPath()); err != nil || !reflect.DeepEqual(got, priorOutcome) {
				t.Fatalf("outcome changed after retry: %q err=%v", got, err)
			}
		})
	}
}

func TestMaintenanceScheduledJitterBeforeLockAndDryRunNoJitter(t *testing.T) {
	root, configDir := t.TempDir(), t.TempDir()
	amp := executableFixture(t, filepath.Join(root, "amp"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	setMaintenanceIntegrationSeams(t, "darwin", t.TempDir(), executableFixture(t, filepath.Join(root, "amux")), amp)
	if err := atomicJSON(filepath.Join(configDir, config.MaintenanceFile), maintenanceMetadata{SchemaVersion: 1, Owner: "external", Platform: "darwin", Schedule: "6h", Path: "/usr/bin", AmuxPath: filepath.Join(root, "amux"), AmpPath: amp, AmpTarget: amp, Artifacts: map[string]string{"/artifact": "digest"}}); err != nil {
		t.Fatal(err)
	}
	oldSleep, oldRandom := maintenanceSleep, maintenanceRandom
	t.Cleanup(func() { maintenanceSleep, maintenanceRandom = oldSleep, oldRandom })
	maintenanceRandom = func(n int64) int64 {
		if n != int64(maintenanceJitter) {
			t.Fatalf("random bound=%d", n)
		}
		return int64(17 * time.Second)
	}
	sleeps := 0
	maintenanceSleep = func(d time.Duration) {
		sleeps++
		if d != 17*time.Second {
			t.Fatalf("sleep=%s", d)
		}
		p, err := lock.MachinePath()
		if err != nil {
			t.Fatal(err)
		}
		held, err := lock.Acquire(context.Background(), p, lock.Owner{Command: "jitter probe"})
		if err != nil {
			t.Fatalf("lock held during jitter: %v", err)
		}
		if err := held.Release(); err != nil {
			t.Fatal(err)
		}
	}
	commands := 0
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands++
		if name != amp || !reflect.DeepEqual(args, []string{"version"}) {
			t.Fatalf("unexpected command %s %q", name, args)
		}
		return []byte("amp 1\n"), nil
	}
	if _, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "run", "--scheduled"); err != nil {
		t.Fatal(err)
	}
	if sleeps != 1 {
		t.Fatalf("scheduled sleeps=%d", sleeps)
	}
	if _, err := executeMaintenanceJSON(t, configDir, "--dry-run", "runner", "maintenance", "run", "--scheduled"); err != nil {
		t.Fatal(err)
	}
	if sleeps != 1 {
		t.Fatalf("dry-run jittered; sleeps=%d", sleeps)
	}
	if commands != 1 {
		t.Fatalf("dry-run executed command; total commands=%d", commands)
	}
}

func TestMaintenanceOperationLockJSONExit2(t *testing.T) {
	runtimeDir, configDir := t.TempDir(), t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	oldWait := mutationLockWait
	mutationLockWait = 20 * time.Millisecond
	t.Cleanup(func() { mutationLockWait = oldWait })
	p, err := lock.MachinePath()
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), p, lock.Owner{PID: 102, Command: "other maintenance", Hostname: "test-host"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })
	env, err := executeMaintenanceJSON(t, configDir, "runner", "maintenance", "remove")
	if err == nil || result.ExitCode(err) != 2 {
		t.Fatalf("err=%v exit=%d", err, result.ExitCode(err))
	}
	if len(env.Failed) != 1 || env.Failed[0].Error == nil || env.Failed[0].Error.Lock == nil || env.Failed[0].Error.Lock.Owner.PID != 102 {
		t.Fatalf("lock JSON=%+v", env)
	}
	if _, statErr := os.Stat(filepath.Join(configDir, config.MaintenanceFile)); !os.IsNotExist(statErr) {
		t.Fatalf("contention had effects: %v", statErr)
	}
}

// maintenanceLifecycleFixture drives the real runner inspection/stop/launch
// code through a stateful fake tmux.  State rows are workspace|mode, where mode
// is exact, conflict, or absent.
type maintenanceLifecycleFixture struct {
	dir        config.Directory
	amp, log   string
	state      string
	workdirs   map[string]string
	windows    map[string]string
	updateAmp  bool
	version    string
	versionErr error
	versions   int
}

func newMaintenanceLifecycleFixture(t *testing.T, owner string, runners int) *maintenanceLifecycleFixture {
	t.Helper()
	root := t.TempDir()
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &maintenanceLifecycleFixture{dir: dir, amp: filepath.Join(root, "amp"), log: filepath.Join(root, "commands.log"), state: filepath.Join(root, "state"), workdirs: map[string]string{}, windows: map[string]string{}, version: "amp test"}
	if err := os.WriteFile(f.amp, []byte("old amp\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	rows := ""
	for i := 0; i < runners; i++ {
		ws := fmt.Sprintf("ws%d", i)
		wd := t.TempDir()
		f.workdirs[ws], f.windows[ws] = wd, config.RunnerWindow(wd)
		rows += ws + "\t" + wd + "\n"
	}
	writeRunnerRegistry(t, dir.Path, rows)
	writeLifecycleState(t, f, map[string]string{})
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	// Python keeps argument handling readable and faithfully logs every tmux call.
	script := `#!/usr/bin/env python3
import json, os, sys
a=sys.argv[1:]
with open(os.environ['AMUX_TMUX_LOG'],'a') as x: x.write(' '.join(a)+'\n')
with open(os.environ['AMUX_TMUX_STATE']) as x: s=json.load(x)
def save():
  with open(os.environ['AMUX_TMUX_STATE'],'w') as x: json.dump(s,x)
def row(ws, pane=None):
  v=s[ws]; mode=v['mode']; pane=pane or v.get('pane','%'+str(v['id']))
  if mode=='absent': return
  cmd='amp' if mode=='exact' else 'bash'
  start=v['start'] if mode=='exact' else 'foreign command'
  print('%s\t%s\t@%s\t%s\t%s\t%s\t%s\t0\t1' % (ws,v['window'],v['id'],pane,v['workdir'],cmd,start))
if a[0]=='has-session':
  ws=a[a.index('-t')+1].lstrip('='); sys.exit(0 if ws in s else 1)
if a[0]=='list-panes':
  target=a[a.index('-t')+1] if '-t' in a else ''
  if target.startswith('%'):
    for ws in s:
      if s[ws].get('pane','%'+str(s[ws]['id']))==target: row(ws,target)
  else:
    ws=target.lstrip('=').split(':')[0]
    if ws in s: row(ws)
  sys.exit(0)
if a[0]=='kill-window':
  wid=a[a.index('-t')+1]
  for ws in s:
    if '@'+str(s[ws]['id'])==wid: s[ws]['mode']='absent'
  save(); sys.exit(0)
if a[0] in ('new-window','new-session'):
  ws=(a[a.index('-t')+1].lstrip('=').rstrip(':') if '-t' in a else a[a.index('-s')+1])
  if os.path.exists(os.path.join(os.environ['AMUX_TMUX_FAIL'],ws)): sys.exit(2)
  v=s[ws]; v['mode']='exact'; v['id']+=100; v['pane']='%'+str(v['id']); save()
  print('%s\t%s\t@%s\t%s' % (ws,v['window'],v['id'],v['pane'])); sys.exit(0)
if a[0]=='capture-pane': sys.exit(0)
sys.exit(2)
`
	writeExecutable(t, filepath.Join(bin, "tmux"), script)
	failDir := filepath.Join(root, "fail")
	if err := os.Mkdir(failDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_TMUX_LOG", f.log)
	t.Setenv("AMUX_TMUX_STATE", f.state)
	t.Setenv("AMUX_TMUX_FAIL", failDir)
	oldExec, oldTimeout, oldPoll := maintenanceExec, runnerStartupTimeout, runnerPollInterval
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != f.amp {
			return nil, fmt.Errorf("unexpected maintenance command %s", name)
		}
		if len(args) == 1 && args[0] == "update" {
			if f.updateAmp {
				if err := os.WriteFile(f.amp, []byte("new amp\n"), 0o700); err != nil {
					return nil, err
				}
			}
			return []byte("updated"), nil
		}
		if len(args) == 1 && args[0] == "version" {
			f.versions++
			if f.versionErr != nil {
				return nil, f.versionErr
			}
			return []byte(f.version + "\n"), nil
		}
		return nil, fmt.Errorf("unexpected amp args %v", args)
	}
	runnerStartupTimeout, runnerPollInterval = 5*time.Millisecond, time.Millisecond
	t.Cleanup(func() { maintenanceExec = oldExec; runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })
	target, err := canonicalExecutable(f.amp)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(dir.MaintenancePath(), maintenanceMetadata{SchemaVersion: 1, Owner: owner, Platform: "linux", Schedule: "6h", Path: os.Getenv("PATH"), AmuxPath: "/amux", AmpPath: f.amp, AmpTarget: resolvePathForComparison(target), Artifacts: map[string]string{"/artifact": "digest"}}); err != nil {
		t.Fatal(err)
	}
	return f
}

func writeLifecycleState(t *testing.T, f *maintenanceLifecycleFixture, modes map[string]string) {
	t.Helper()
	state := map[string]map[string]any{}
	for ws, wd := range f.workdirs {
		mode := modes[ws]
		if mode == "" {
			mode = "absent"
		}
		state[ws] = map[string]any{"mode": mode, "workdir": wd, "window": f.windows[ws], "start": runnerStartCommand(wd), "id": len(state) + 1}
	}
	b, _ := json.Marshal(state)
	if err := os.WriteFile(f.state, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
func runLifecycle(t *testing.T, f *maintenanceLifecycleFixture) (result.Envelope, error) {
	t.Helper()
	env := result.NewEnvelope("runner maintenance run", false)
	_, err := (app{}).runMaintenance(invocation{Command: maintenanceCommand().Children[2], Path: []string{"runner", "maintenance", "run"}}, f.dir, &env)
	return env, err
}
func lifecycleFingerprint(t *testing.T, path string) string {
	t.Helper()
	got, err := fingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func seedLifecyclePrior(t *testing.T, f *maintenanceLifecycleFixture, hash string) {
	t.Helper()
	if err := atomicJSON(f.dir.MaintenanceResultPath(), maintenanceOutcome{SchemaVersion: 1, Status: "successful", AmpVersion: f.version, AppliedFingerprint: hash, ObservedFingerprint: hash, AppliedVersion: f.version}); err != nil {
		t.Fatal(err)
	}
}

func seedLifecyclePriorVersion(t *testing.T, f *maintenanceLifecycleFixture, hash, version string) {
	t.Helper()
	if err := atomicJSON(f.dir.MaintenanceResultPath(), maintenanceOutcome{SchemaVersion: 1, Status: "successful", AmpVersion: version, AppliedFingerprint: hash, ObservedFingerprint: hash, AppliedVersion: version}); err != nil {
		t.Fatal(err)
	}
}
func lifecycleLog(t *testing.T, f *maintenanceLifecycleFixture) string {
	t.Helper()
	b, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func lifecycleRunner(t *testing.T, o maintenanceOutcome, workdir string) maintenanceRunnerOutcome {
	t.Helper()
	for _, runner := range o.Runners {
		if runner.Workdir == workdir {
			return runner
		}
	}
	t.Fatalf("no persisted outcome for %s: %+v", workdir, o.Runners)
	return maintenanceRunnerOutcome{}
}

func reinstallLifecycle(t *testing.T, f *maintenanceLifecycleFixture, owner string) {
	t.Helper()
	amux := executableFixture(t, filepath.Join(filepath.Dir(f.amp), "amux"))
	home := t.TempDir()
	oldGOOS, oldHome, oldConfigDir := maintenanceGOOS, maintenanceHome, maintenanceUserConfigDir
	oldLookPath, oldCanonical, runExec := maintenanceLookPath, canonicalSelfUpdatePath, maintenanceExec
	t.Cleanup(func() {
		maintenanceGOOS, maintenanceHome, maintenanceUserConfigDir = oldGOOS, oldHome, oldConfigDir
		maintenanceLookPath, canonicalSelfUpdatePath, maintenanceExec = oldLookPath, oldCanonical, runExec
	})
	maintenanceGOOS = "linux"
	maintenanceHome = func() (string, error) { return home, nil }
	maintenanceUserConfigDir = func() (string, error) { return filepath.Join(home, ".config"), nil }
	maintenanceLookPath = func(name string) (string, error) { return f.amp, nil }
	canonicalSelfUpdatePath = func() (string, error) { return amux, nil }
	servicePath, timerPath, err := maintenancePaths()
	if err != nil {
		t.Fatal(err)
	}
	service, timer := systemdMaintenanceArtifacts(amux, f.dir.Path, os.Getenv("PATH"))
	artifacts := map[string]string{}
	for path, contents := range map[string]string{servicePath: service, timerPath: timer} {
		if err := atomicWrite(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		artifacts[path] = digest([]byte(contents))
	}
	installed, err := loadMaintenance(f.dir.MaintenancePath())
	if err != nil {
		t.Fatal(err)
	}
	installed.Platform, installed.Path, installed.AmuxPath = "linux", os.Getenv("PATH"), amux
	installed.Artifacts = artifacts
	if err := atomicJSON(f.dir.MaintenancePath(), installed); err != nil {
		t.Fatal(err)
	}
	maintenanceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" {
			return nil, nil
		}
		return runExec(context.Background(), name, args...)
	}
	in := invocation{Command: maintenanceCommand().Children[0], Path: []string{"runner", "maintenance", "install"}, MaintenanceOwner: owner}
	env := result.NewEnvelope("runner maintenance install", false)
	if _, err := (app{}).installMaintenance(in, f.dir, &env); err != nil {
		t.Fatal(err)
	}
	maintenanceExec = runExec
}

func TestMaintenanceReinstallPreservesChangedExecutableCheckpoint(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	old := lifecycleFingerprint(t, f.amp)
	seedLifecyclePrior(t, f, old)
	writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
	if err := os.WriteFile(f.amp, []byte("changed outside maintenance\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	reinstallLifecycle(t, f, "self") // Also exercises an update-owner reinstall.
	preserved, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || preserved.AppliedFingerprint != old {
		t.Fatalf("checkpoint after reinstall=%+v err=%v", preserved, err)
	}
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	if log := lifecycleLog(t, f); strings.Count(log, "kill-window") != 1 {
		t.Fatalf("changed executable was not restarted after reinstall:\n%s", log)
	}
}

func TestMaintenanceReinstallPreservesPendingLaunch(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	hash := lifecycleFingerprint(t, f.amp)
	prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", Time: time.Unix(123, 0).UTC(), AmpPath: f.amp, AmpVersion: "amp prior", ObservedFingerprint: hash, AppliedFingerprint: hash, AppliedVersion: "amp prior", Error: "launch failed", Runners: []maintenanceRunnerOutcome{{Workdir: f.workdirs["ws0"], Status: "failed", Phase: "pending_launch", Error: "absent"}}}
	if err := atomicJSON(f.dir.MaintenanceResultPath(), prior); err != nil {
		t.Fatal(err)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "absent"})
	reinstallLifecycle(t, f, "external")
	preserved, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || !reflect.DeepEqual(preserved, prior) {
		t.Fatalf("pending outcome changed: got=%+v want=%+v err=%v", preserved, prior, err)
	}
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	if log := lifecycleLog(t, f); strings.Count(log, "new-window")+strings.Count(log, "new-session") != 1 {
		t.Fatalf("pending absent runner was not launched:\n%s", log)
	}
}

func TestMaintenanceSelfFirstRunNoopAndChangedUpdate(t *testing.T) {
	t.Run("no-op update", func(t *testing.T) {
		f := newMaintenanceLifecycleFixture(t, "self", 1)
		seedLifecyclePrior(t, f, lifecycleFingerprint(t, f.amp))
		writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
		env, err := runLifecycle(t, f)
		if err != nil || len(env.Skipped) != 1 {
			t.Fatalf("env=%+v err=%v", env, err)
		}
		if strings.Contains(lifecycleLog(t, f), "kill-window") {
			t.Fatal("unchanged update restarted tmux")
		}
	})
	t.Run("changed executable", func(t *testing.T) {
		f := newMaintenanceLifecycleFixture(t, "self", 1)
		seedLifecyclePrior(t, f, lifecycleFingerprint(t, f.amp))
		writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
		f.updateAmp = true
		env, err := runLifecycle(t, f)
		if err != nil || len(env.Successful) != 2 {
			t.Fatalf("env=%+v err=%v", env, err)
		}
		log := lifecycleLog(t, f)
		if strings.Count(log, "kill-window") != 1 || strings.Count(log, "new-window")+strings.Count(log, "new-session") != 1 {
			t.Fatalf("commands:\n%s", log)
		}
	})
	t.Run("version-only update", func(t *testing.T) {
		f := newMaintenanceLifecycleFixture(t, "self", 1)
		seedLifecyclePrior(t, f, lifecycleFingerprint(t, f.amp))
		writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
		f.version = "amp new"
		env, err := runLifecycle(t, f)
		if err != nil || len(env.Successful) != 2 || f.versions != 1 {
			t.Fatalf("env=%+v err=%v versions=%d", env, err, f.versions)
		}
		if log := lifecycleLog(t, f); strings.Count(log, "kill-window") != 1 || strings.Count(log, "new-window")+strings.Count(log, "new-session") != 1 {
			t.Fatalf("version-only commands:\n%s", log)
		}
	})
}

func TestMaintenanceExternalChangedRestartsExactOnlyAndPreservesStopped(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 2)
	old := lifecycleFingerprint(t, f.amp)
	seedLifecyclePrior(t, f, old)
	writeLifecycleState(t, f, map[string]string{"ws0": "exact", "ws1": "absent"})
	before, _ := os.ReadFile(f.dir.RunnersPath())
	os.WriteFile(f.amp, []byte("changed\n"), 0o700)
	env, err := runLifecycle(t, f)
	if err != nil || len(env.Successful) != 2 || len(env.Skipped) != 1 {
		t.Fatalf("env=%+v err=%v", env, err)
	}
	log := lifecycleLog(t, f)
	if strings.Count(log, "kill-window") != 1 || strings.Contains(log, "amp threads") {
		t.Fatalf("commands:\n%s", log)
	}
	after, _ := os.ReadFile(f.dir.RunnersPath())
	if !bytes.Equal(before, after) {
		t.Fatal("runner registry changed")
	}
}

func TestMaintenanceExternalSymlinkRetargetRestartsAndUpdatesTarget(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	target1 := executableFixture(t, filepath.Join(filepath.Dir(f.amp), "amp-1"))
	target2 := executableFixture(t, filepath.Join(filepath.Dir(f.amp), "amp-2"))
	contents, err := os.ReadFile(target1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target2, contents, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.amp); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target1, f.amp); err != nil {
		t.Fatal(err)
	}
	m, err := loadMaintenance(f.dir.MaintenancePath())
	if err != nil {
		t.Fatal(err)
	}
	m.AmpTarget = resolvePathForComparison(f.amp)
	if err := atomicJSON(f.dir.MaintenancePath(), m); err != nil {
		t.Fatal(err)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	baseline, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || baseline.AppliedFingerprint == "" {
		t.Fatalf("baseline=%+v err=%v", baseline, err)
	}
	if err := os.Remove(f.amp); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target2, f.amp); err != nil {
		t.Fatal(err)
	}
	before := lifecycleLog(t, f)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	delta := strings.TrimPrefix(lifecycleLog(t, f), before)
	if strings.Count(delta, "kill-window") != 1 || strings.Count(delta, "new-window")+strings.Count(delta, "new-session") != 1 {
		t.Fatalf("retarget commands:\n%s", delta)
	}
	m, err = loadMaintenance(f.dir.MaintenancePath())
	if err != nil || m.AmpTarget != resolvePathForComparison(target2) {
		t.Fatalf("updated metadata=%+v err=%v", m, err)
	}
}

func TestMaintenanceStableExecutableVersionChangeRestartsOnce(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	hash := lifecycleFingerprint(t, f.amp)
	seedLifecyclePriorVersion(t, f, hash, "amp old")
	writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
	f.version = "amp new"

	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	if f.versions != 1 {
		t.Fatalf("version invoked %d times", f.versions)
	}
	if log := lifecycleLog(t, f); strings.Count(log, "kill-window") != 1 || strings.Count(log, "new-window")+strings.Count(log, "new-session") != 1 {
		t.Fatalf("version-change commands:\n%s", log)
	}
	got, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || got.AppliedVersion != "amp new" {
		t.Fatalf("outcome=%+v err=%v", got, err)
	}
}

func TestMaintenancePendingLaunchRetriesWithoutLaunchingNormallyStopped(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 2)
	old := lifecycleFingerprint(t, f.amp)
	seedLifecyclePrior(t, f, old)
	writeLifecycleState(t, f, map[string]string{"ws0": "exact", "ws1": "absent"})
	os.WriteFile(f.amp, []byte("changed\n"), 0o700)
	fail := filepath.Join(os.Getenv("AMUX_TMUX_FAIL"), "ws0")
	os.WriteFile(fail, []byte("x"), 0o600)
	if _, err := runLifecycle(t, f); err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("first err=%v", err)
	}
	got, _ := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if got.AppliedFingerprint != old || lifecycleRunner(t, got, f.workdirs["ws0"]).Phase != "pending_launch" {
		t.Fatalf("first outcome=%+v", got)
	}
	os.Remove(fail)
	before := lifecycleLog(t, f)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	delta := strings.TrimPrefix(lifecycleLog(t, f), before)
	if strings.Contains(delta, "kill-window") || strings.Count(delta, "new-window")+strings.Count(delta, "new-session") != 1 {
		t.Fatalf("retry commands:\n%s", delta)
	}
	got, _ = loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if got.AppliedFingerprint != got.ObservedFingerprint {
		t.Fatalf("retry outcome=%+v", got)
	}
}

func TestMaintenancePendingLaunchSurvivesVersionFailureAndRetriesUnchangedExecutable(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	applied := lifecycleFingerprint(t, f.amp)
	seedLifecyclePrior(t, f, applied)
	writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
	if err := os.WriteFile(f.amp, []byte("changed\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	fail := filepath.Join(os.Getenv("AMUX_TMUX_FAIL"), "ws0")
	if err := os.WriteFile(fail, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runLifecycle(t, f); err == nil {
		t.Fatal("failed launch unexpectedly succeeded")
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}

	f.versionErr = errors.New("version unavailable")
	if _, err := runLifecycle(t, f); err == nil {
		t.Fatal("version failure unexpectedly succeeded")
	}
	afterVersionFailure, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || lifecycleRunner(t, afterVersionFailure, f.workdirs["ws0"]).Phase != "pending_launch" {
		t.Fatalf("version failure lost pending launch: outcome=%+v err=%v", afterVersionFailure, err)
	}

	f.versionErr = nil
	writeLifecycleState(t, f, map[string]string{"ws0": "absent"})
	before := lifecycleLog(t, f)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	delta := strings.TrimPrefix(lifecycleLog(t, f), before)
	if launches := strings.Count(delta, "new-window") + strings.Count(delta, "new-session"); launches != 1 || strings.Contains(delta, "kill-window") {
		t.Fatalf("retry commands:\n%s", delta)
	}
	got, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || got.Status != "successful" || got.AppliedFingerprint != got.ObservedFingerprint || got.AppliedFingerprint == applied {
		t.Fatalf("retry did not advance applied outcome: outcome=%+v err=%v", got, err)
	}
}

func TestMaintenanceFingerprintChangeVersionFailureDiscardsCompletedRunnerCheckpoint(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	old := lifecycleFingerprint(t, f.amp)
	prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", AmpVersion: f.version, ObservedFingerprint: old, AppliedFingerprint: old, AppliedVersion: f.version, Runners: []maintenanceRunnerOutcome{{Workdir: f.workdirs["ws0"], Status: "successful"}}}
	if err := atomicJSON(f.dir.MaintenanceResultPath(), prior); err != nil {
		t.Fatal(err)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
	if err := os.WriteFile(f.amp, []byte("changed identity\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.versionErr = errors.New("version unavailable")
	if _, err := runLifecycle(t, f); err == nil {
		t.Fatal("version failure unexpectedly succeeded")
	}
	failed, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || len(failed.Runners) != 0 {
		t.Fatalf("stale runner completion survived identity change: outcome=%+v err=%v", failed, err)
	}
	f.versionErr = nil
	before := lifecycleLog(t, f)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	delta := strings.TrimPrefix(lifecycleLog(t, f), before)
	if strings.Count(delta, "kill-window") != 1 || strings.Count(delta, "new-window")+strings.Count(delta, "new-session") != 1 {
		t.Fatalf("retry reused stale completion checkpoint:\n%s", delta)
	}
}

func TestMaintenanceVersionOnlyIdentityChangeDiscardsCompletedRunnerCheckpoint(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	hash := lifecycleFingerprint(t, f.amp)
	prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", AmpVersion: "amp old", ObservedFingerprint: hash, AppliedFingerprint: hash, AppliedVersion: "amp old", Runners: []maintenanceRunnerOutcome{{Workdir: f.workdirs["ws0"], Status: "successful"}}}
	if err := atomicJSON(f.dir.MaintenanceResultPath(), prior); err != nil {
		t.Fatal(err)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
	f.version = "amp new"
	before := lifecycleLog(t, f)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	delta := strings.TrimPrefix(lifecycleLog(t, f), before)
	if strings.Count(delta, "kill-window") != 1 || strings.Count(delta, "new-window")+strings.Count(delta, "new-session") != 1 {
		t.Fatalf("version change reused stale completion checkpoint:\n%s", delta)
	}
}

func TestMaintenancePendingLaunchCarriesAcrossFingerprintAndRecoversExact(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		wantLaunch  int
	}{
		{name: "newer fingerprint absent relaunches", state: "absent", wantLaunch: 1},
		{name: "exact is recovered without duplicate", state: "exact", wantLaunch: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaintenanceLifecycleFixture(t, "external", 1)
			applied := lifecycleFingerprint(t, f.amp)
			if err := os.WriteFile(f.amp, []byte("newest amp\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", ObservedFingerprint: "intermediate-fingerprint", AppliedFingerprint: applied, Runners: []maintenanceRunnerOutcome{{Workdir: f.workdirs["ws0"], Status: "failed", Phase: "pending_launch"}}}
			if err := atomicJSON(f.dir.MaintenanceResultPath(), prior); err != nil {
				t.Fatal(err)
			}
			writeLifecycleState(t, f, map[string]string{"ws0": tc.state})
			before := lifecycleLog(t, f)
			if _, err := runLifecycle(t, f); err != nil {
				t.Fatal(err)
			}
			delta := strings.TrimPrefix(lifecycleLog(t, f), before)
			launches := strings.Count(delta, "new-window") + strings.Count(delta, "new-session")
			if launches != tc.wantLaunch || strings.Contains(delta, "kill-window") {
				t.Fatalf("commands:\n%s", delta)
			}
			got, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
			if err != nil || got.Status != "successful" || got.AppliedFingerprint != got.ObservedFingerprint {
				t.Fatalf("outcome=%+v err=%v", got, err)
			}
		})
	}
}

func TestMaintenanceRestartRequiredRetryConverges(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     string
		wantStops int
	}{
		{name: "exact is stopped before relaunch", state: "exact", wantStops: 1},
		{name: "absent is launched", state: "absent", wantStops: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaintenanceLifecycleFixture(t, "external", 1)
			applied := lifecycleFingerprint(t, f.amp)
			if err := os.WriteFile(f.amp, []byte("changed\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", ObservedFingerprint: "changed", AppliedFingerprint: applied, Runners: []maintenanceRunnerOutcome{{Workdir: f.workdirs["ws0"], Status: "failed", Phase: "restart_required"}}}
			if err := atomicJSON(f.dir.MaintenanceResultPath(), prior); err != nil {
				t.Fatal(err)
			}
			writeLifecycleState(t, f, map[string]string{"ws0": tc.state})
			before := lifecycleLog(t, f)
			if _, err := runLifecycle(t, f); err != nil {
				t.Fatal(err)
			}
			delta := strings.TrimPrefix(lifecycleLog(t, f), before)
			if stops := strings.Count(delta, "kill-window"); stops != tc.wantStops {
				t.Fatalf("stops=%d want=%d commands:\n%s", stops, tc.wantStops, delta)
			}
			if launches := strings.Count(delta, "new-window") + strings.Count(delta, "new-session"); launches != 1 {
				t.Fatalf("launches=%d commands:\n%s", launches, delta)
			}
		})
	}
}

func TestMaintenanceRestartRequiredAbsentLaunchFailurePersistsPendingLaunch(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	applied := lifecycleFingerprint(t, f.amp)
	if err := os.WriteFile(f.amp, []byte("changed\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", ObservedFingerprint: "changed", AppliedFingerprint: applied, Runners: []maintenanceRunnerOutcome{{Workdir: f.workdirs["ws0"], Status: "failed", Phase: "restart_required"}}}
	if err := atomicJSON(f.dir.MaintenanceResultPath(), prior); err != nil {
		t.Fatal(err)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "absent"})
	if err := os.WriteFile(filepath.Join(os.Getenv("AMUX_TMUX_FAIL"), "ws0"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runLifecycle(t, f); err == nil {
		t.Fatal("launch failure unexpectedly succeeded")
	}
	got, err := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if err != nil || lifecycleRunner(t, got, f.workdirs["ws0"]).Phase != "pending_launch" {
		t.Fatalf("launch checkpoint=%+v err=%v", got, err)
	}
}

func TestMaintenancePendingLaunchSurvivesConflictThenAbsent(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	applied := lifecycleFingerprint(t, f.amp)
	os.WriteFile(f.amp, []byte("changed\n"), 0o700)
	prior := maintenanceOutcome{SchemaVersion: 1, Status: "failed", ObservedFingerprint: "intermediate", AppliedFingerprint: applied, AppliedVersion: "amp old", Runners: []maintenanceRunnerOutcome{{Workdir: f.workdirs["ws0"], Status: "failed", Phase: "pending_launch"}}}
	if err := atomicJSON(f.dir.MaintenanceResultPath(), prior); err != nil {
		t.Fatal(err)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "conflict"})
	if _, err := runLifecycle(t, f); err == nil {
		t.Fatal("conflict unexpectedly succeeded")
	}
	conflicted, _ := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if conflicted.AppliedFingerprint != applied || lifecycleRunner(t, conflicted, f.workdirs["ws0"]).Phase != "pending_launch" {
		t.Fatalf("conflict lost checkpoint: %+v", conflicted)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "absent"})
	before := lifecycleLog(t, f)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	delta := strings.TrimPrefix(lifecycleLog(t, f), before)
	if strings.Count(delta, "new-window")+strings.Count(delta, "new-session") != 1 || strings.Contains(delta, "kill-window") {
		t.Fatalf("retry commands:\n%s", delta)
	}
}

func TestMaintenancePartialVersionChangeFailurePreservesAppliedVersion(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	hash := lifecycleFingerprint(t, f.amp)
	seedLifecyclePriorVersion(t, f, hash, "amp old")
	writeLifecycleState(t, f, map[string]string{"ws0": "exact"})
	f.version = "amp new"
	fail := filepath.Join(os.Getenv("AMUX_TMUX_FAIL"), "ws0")
	os.WriteFile(fail, []byte("x"), 0o600)
	if _, err := runLifecycle(t, f); err == nil {
		t.Fatal("partial failure unexpectedly succeeded")
	}
	failed, _ := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if failed.AppliedVersion != "amp old" || failed.AmpVersion != "amp new" {
		t.Fatalf("partial failure advanced applied version: %+v", failed)
	}
	os.Remove(fail)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	got, _ := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if got.AppliedVersion != "amp new" || f.versions != 2 {
		t.Fatalf("retry outcome=%+v version calls=%d", got, f.versions)
	}
}

func TestMaintenancePartialConflictPersistsAndRetries(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 2)
	old := lifecycleFingerprint(t, f.amp)
	seedLifecyclePrior(t, f, old)
	writeLifecycleState(t, f, map[string]string{"ws0": "exact", "ws1": "conflict"})
	os.WriteFile(f.amp, []byte("changed\n"), 0o700)
	if _, err := runLifecycle(t, f); err == nil {
		t.Fatal("partial conflict succeeded")
	}
	got, _ := loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if got.AppliedFingerprint != old || got.Status != "failed" || lifecycleRunner(t, got, f.workdirs["ws0"]).Status != "successful" || lifecycleRunner(t, got, f.workdirs["ws1"]).Status != "failed" {
		t.Fatalf("outcome=%+v", got)
	}
	writeLifecycleState(t, f, map[string]string{"ws0": "absent", "ws1": "exact"})
	before := lifecycleLog(t, f)
	if _, err := runLifecycle(t, f); err != nil {
		t.Fatal(err)
	}
	delta := strings.TrimPrefix(lifecycleLog(t, f), before)
	if strings.Count(delta, "kill-window") != 1 {
		t.Fatalf("retry commands:\n%s", delta)
	}
	got, _ = loadMaintenanceOutcome(f.dir.MaintenanceResultPath())
	if got.AppliedFingerprint != got.ObservedFingerprint {
		t.Fatalf("retry outcome=%+v", got)
	}
}

func TestMaintenanceDoesNotTouchRunnerConfigOrAmpPIDFiles(t *testing.T) {
	f := newMaintenanceLifecycleFixture(t, "external", 1)
	old := lifecycleFingerprint(t, f.amp)
	seedLifecyclePrior(t, f, old)
	writeLifecycleState(t, f, map[string]string{"ws0": "conflict"})
	registry, _ := os.ReadFile(f.dir.RunnersPath())
	cache := t.TempDir()
	marker := filepath.Join(cache, "marker")
	os.WriteFile(marker, []byte("owned\n"), 0o600)
	oldCache := runnerCacheDir
	runnerCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { runnerCacheDir = oldCache })
	os.WriteFile(f.amp, []byte("changed\n"), 0o700)
	_, _ = runLifecycle(t, f)
	after, _ := os.ReadFile(f.dir.RunnersPath())
	mark, _ := os.ReadFile(marker)
	if !bytes.Equal(registry, after) || string(mark) != "owned\n" {
		t.Fatal("maintenance modified runner config or PID marker")
	}
	if strings.Contains(lifecycleLog(t, f), "amp threads") {
		t.Fatal("maintenance used remote thread command")
	}
}
