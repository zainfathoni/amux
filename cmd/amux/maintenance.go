package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

const maintenanceJitter = 30 * time.Minute
const maintenanceLabel = "com.zainfathoni.amux.runner-maintenance"

var maintenanceGOOS = runtime.GOOS
var maintenanceHome = os.UserHomeDir
var maintenanceUserConfigDir = os.UserConfigDir
var maintenanceNow = time.Now
var maintenanceSleep = time.Sleep
var maintenanceTimeout = 30 * time.Second
var maintenanceLookPath = exec.LookPath
var maintenanceExec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
var maintenanceRandom = func(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return rand.Int64N(n)
}

type maintenanceMetadata struct {
	SchemaVersion     int               `json:"schema_version"`
	ActivationPending bool              `json:"activation_pending"`
	Owner             string            `json:"update_owner"`
	Platform          string            `json:"platform"`
	Schedule          string            `json:"schedule"`
	Path              string            `json:"path"`
	AmuxPath          string            `json:"amux_path"`
	AmpPath           string            `json:"amp_path"`
	AmpTarget         string            `json:"amp_target"`
	Artifacts         map[string]string `json:"artifacts"`
	PreviousArtifacts map[string]string `json:"previous_artifacts,omitempty"`
}
type maintenanceRunnerOutcome struct {
	Workdir string `json:"workdir"`
	Status  string `json:"status"`
	Phase   string `json:"phase,omitempty"`
	Error   string `json:"error,omitempty"`
}
type maintenanceOutcome struct {
	SchemaVersion       int                        `json:"schema_version"`
	Status              string                     `json:"status"`
	Time                time.Time                  `json:"time"`
	AmpPath             string                     `json:"amp_path,omitempty"`
	AmpVersion          string                     `json:"amp_version,omitempty"`
	ObservedFingerprint string                     `json:"observed_fingerprint,omitempty"`
	AppliedFingerprint  string                     `json:"applied_fingerprint,omitempty"`
	AppliedVersion      string                     `json:"applied_version,omitempty"`
	Changed             bool                       `json:"changed"`
	Error               string                     `json:"error,omitempty"`
	Runners             []maintenanceRunnerOutcome `json:"runners"`
}

func systemdQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '%':
			b.WriteString("%%")
		case '$':
			b.WriteString("$$")
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
func systemdMaintenanceArtifacts(exe, dir, path string) (string, string) {
	// Environment= does not perform the command-line $ expansion used by
	// ExecStart=, so preserve dollars while still escaping systemd specifiers.
	pathAssignment := strings.ReplaceAll(systemdQuote("PATH="+path), "$$", "$")
	service := "[Unit]\nDescription=amux runner maintenance\n[Service]\nType=oneshot\nEnvironment=" + pathAssignment + "\nExecStart=" + systemdQuote(exe) + " --config-dir " + systemdQuote(dir) + " runner maintenance run --scheduled\n"
	timer := "[Unit]\nDescription=Run amux runner maintenance every six hours\n[Timer]\nOnCalendar=*-*-* 00/6:00:00\nPersistent=true\nRandomizedDelaySec=30m\n[Install]\nWantedBy=timers.target\n"
	return service, timer
}
func escapedLaunchAgentMaintenanceArtifact(exe, dir, path string) string {
	e, d, p := html.EscapeString(exe), html.EscapeString(dir), html.EscapeString(path)
	var intervals strings.Builder
	for _, h := range []int{0, 6, 12, 18} {
		fmt.Fprintf(&intervals, "<dict><key>Hour</key><integer>%d</integer><key>Minute</key><integer>0</integer></dict>", h)
	}
	return `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + maintenanceLabel + `</string><key>EnvironmentVariables</key><dict><key>PATH</key><string>` + p + `</string></dict><key>ProgramArguments</key><array><string>` + e + `</string><string>--config-dir</string><string>` + d + `</string><string>runner</string><string>maintenance</string><string>run</string><string>--scheduled</string></array><key>RunAtLoad</key><true/><key>StartCalendarInterval</key><array>` + intervals.String() + `</array></dict></plist>`
}

func launchAgentMaintenanceArtifact(exe, dir, path string) string {
	return strings.ReplaceAll(escapedLaunchAgentMaintenanceArtifact(exe, dir, path), `\"`, `"`)
}

func command(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), maintenanceTimeout)
	defer cancel()
	b, err := maintenanceExec(ctx, name, args...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return b, fmt.Errorf("command timed out after %s: %w", maintenanceTimeout, ctx.Err())
	}
	return b, err
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (a app) executeMaintenance(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if len(in.Args) != 0 || !selectorsEmpty(in.Selectors) {
		return &env, result.Request(errors.New("maintenance command does not accept selectors or arguments"))
	}
	if maintenanceGOOS != "linux" && maintenanceGOOS != "darwin" {
		return &env, result.Preflight(fmt.Errorf("runner maintenance is unsupported on %s", maintenanceGOOS))
	}
	switch in.Command.Name {
	case "install":
		return a.installMaintenance(in, dir, &env)
	case "remove":
		return a.removeMaintenance(in, dir, &env)
	case "run":
		return a.runMaintenance(in, dir, &env)
	default:
		return &env, result.Request(errors.New("unknown maintenance operation"))
	}
}
func maintenancePaths() (string, string, error) {
	if maintenanceGOOS == "linux" {
		configDir, configErr := maintenanceUserConfigDir()
		if configErr != nil || configDir == "" {
			home, err := maintenanceHome()
			if err != nil {
				return "", "", err
			}
			configDir = filepath.Join(home, ".config")
		}
		base := filepath.Join(configDir, "systemd", "user")
		return filepath.Join(base, maintenanceLabel+".service"), filepath.Join(base, maintenanceLabel+".timer"), nil
	}
	home, err := maintenanceHome()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", maintenanceLabel+".plist"), "", nil
}
func atomicJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0o600)
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".amux-maintenance-*")
	if err != nil {
		return err
	}
	n := f.Name()
	defer os.Remove(n)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(n, path)
}
func canonicalExecutable(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("executable target is not an executable file: %s", abs)
	}
	return abs, nil
}
func (a app) installMaintenance(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	if in.MaintenanceOwner != "self" && in.MaintenanceOwner != "external" {
		return env, result.Request(errors.New("--update-owner must be self or external"))
	}
	amuxPath, err := canonicalSelfUpdatePath()
	if err != nil {
		return env, result.Preflight(err)
	}
	amuxPath, err = canonicalExecutable(amuxPath)
	if err != nil {
		return env, result.Preflight(fmt.Errorf("canonical amux target: %w", err))
	}
	if _, err = validateCanonicalSelfUpdateTarget(amuxPath); err != nil {
		return env, result.Preflight(err)
	}
	ampPath, err := maintenanceLookPath("amp")
	if err != nil {
		return env, result.Preflight(fmt.Errorf("resolve Amp: %w", err))
	}
	ampPath, err = canonicalExecutable(ampPath)
	if err != nil {
		return env, result.Preflight(err)
	}
	ampTarget := resolvePathForComparison(ampPath)
	installPath := os.Getenv("PATH")
	if installPath == "" {
		return env, result.Preflight(errors.New("PATH is empty; cannot install maintenance"))
	}
	if in.MaintenanceOwner == "self" && managedInstallPath(ampTarget) {
		return env, result.Preflight(fmt.Errorf("self-owned maintenance refused for package/toolchain-managed Amp executable %s", ampTarget))
	}
	p1, p2, err := maintenancePaths()
	if err != nil {
		return env, result.Preflight(err)
	}
	artifacts := map[string][]byte{}
	if maintenanceGOOS == "linux" {
		s, t := systemdMaintenanceArtifacts(amuxPath, dir.Path, installPath)
		artifacts[p1], artifacts[p2] = []byte(s), []byte(t)
	} else {
		artifacts[p1] = []byte(launchAgentMaintenanceArtifact(amuxPath, dir.Path, installPath))
	}
	var previous *maintenanceMetadata
	installed, loadErr := loadMaintenance(dir.MaintenancePath())
	if loadErr == nil {
		previous = &installed
		if installed.Platform != maintenanceGOOS {
			return env, result.Preflight(fmt.Errorf("existing maintenance installation is for %s; remove it before installing for %s", installed.Platform, maintenanceGOOS))
		}
		if len(installed.Artifacts) != len(artifacts) {
			return env, result.Preflight(errors.New("existing maintenance artifact paths differ; remove the installation before reinstalling"))
		}
		for p := range installed.Artifacts {
			if _, ok := artifacts[p]; !ok {
				return env, result.Preflight(errors.New("existing maintenance artifact paths differ; remove the installation before reinstalling"))
			}
		}
	} else if !os.IsNotExist(loadErr) {
		return env, result.Preflight(fmt.Errorf("load maintenance installation: %w", loadErr))
	}
	for p, expected := range artifacts {
		if b, e := os.ReadFile(p); e == nil {
			got := digest(b)
			owned := previous != nil && (got == previous.Artifacts[p] || previous.ActivationPending && got == previous.PreviousArtifacts[p])
			if got != digest(expected) && !owned {
				return env, result.Preflight(fmt.Errorf("refusing to overwrite unrecognized maintenance artifact %s", p))
			}
		} else if e != nil && !os.IsNotExist(e) {
			return env, result.Preflight(e)
		}
	}
	// The outcome is the durable checkpoint used to decide whether runners
	// still need to be restarted.  An install may update metadata and scheduler
	// artifacts, but must never reset an existing checkpoint.
	resultAbsent := false
	if _, err := loadMaintenanceOutcome(dir.MaintenanceResultPath()); err != nil {
		if os.IsNotExist(err) {
			resultAbsent = true
		} else {
			return env, result.Preflight(fmt.Errorf("load maintenance outcome: %w", err))
		}
	}
	details := maintenanceResultDetails(maintenanceMetadata{SchemaVersion: 1, Owner: in.MaintenanceOwner, Platform: maintenanceGOOS, Schedule: "6h", Path: installPath, AmuxPath: amuxPath, AmpPath: ampPath, AmpTarget: ampTarget, Artifacts: map[string]string{}})
	for p := range artifacts {
		details.ArtifactPaths = append(details.ArtifactPaths, p)
	}
	sort.Strings(details.ArtifactPaths)
	out := result.Outcome{Resource: result.ConfigResource(dir.MaintenancePath()), Action: "install-maintenance", Maintenance: details, Message: maintenancePlanMessage("install", details)}
	if in.Options.DryRun {
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON && a.stdout != nil {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}
	var baseline maintenanceOutcome
	if resultAbsent {
		// Establish the complete applied identity before installing any scheduler
		// artifacts. In particular, launchd's RunAtLoad may execute immediately.
		baselineFingerprint, err := fingerprint(ampPath)
		if err != nil {
			return env, result.Preflight(fmt.Errorf("fingerprint Amp baseline: %w", err))
		}
		versionOut, err := command(ampPath, "version")
		if err != nil {
			return env, result.Preflight(fmt.Errorf("amp version baseline: %s: %w", strings.TrimSpace(string(versionOut)), err))
		}
		baselineVersion := strings.TrimSpace(string(versionOut))
		baseline = maintenanceOutcome{SchemaVersion: 1, Status: "skipped", Time: maintenanceNow().UTC(), AmpPath: ampPath, AmpVersion: baselineVersion, ObservedFingerprint: baselineFingerprint, AppliedFingerprint: baselineFingerprint, AppliedVersion: baselineVersion}
	}
	digests := map[string]string{}
	artifactsChanged := previous == nil
	activationWasPending := previous != nil && previous.ActivationPending
	for p, b := range artifacts {
		digests[p] = digest(b)
		if previous != nil && previous.Artifacts[p] != digests[p] {
			artifactsChanged = true
		}
	}
	previousDigests := map[string]string{}
	for p := range artifacts {
		if b, readErr := os.ReadFile(p); readErr == nil {
			// Preflight above proved these exact bytes are owned. Recording the
			// concrete digest makes a new transaction safe even when it supersedes
			// an interrupted transaction whose artifacts are a mixed generation.
			previousDigests[p] = digest(b)
		} else if !os.IsNotExist(readErr) {
			return env, result.Runtime(readErr)
		}
	}
	meta := maintenanceMetadata{SchemaVersion: 1, ActivationPending: true, Owner: in.MaintenanceOwner, Platform: maintenanceGOOS, Schedule: "6h", Path: installPath, AmuxPath: amuxPath, AmpPath: ampPath, AmpTarget: ampTarget, Artifacts: digests, PreviousArtifacts: previousDigests}
	if resultAbsent {
		if err := atomicJSON(dir.MaintenanceResultPath(), baseline); err != nil {
			return env, result.Runtime(err)
		}
	}
	// Publish ownership of both generations before replacing the first
	// artifact. This marker deliberately survives artifact and activation
	// failures so retry and remove can recognize any partially applied state.
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		return env, result.Runtime(err)
	}
	for p, b := range artifacts {
		if err := atomicWrite(p, b, 0o600); err != nil {
			return env, result.Runtime(err)
		}
	}
	activationFailure := func(err error) (*result.Envelope, error) { return env, result.Runtime(err) }
	if maintenanceGOOS == "linux" {
		if b, e := command("systemctl", "--user", "daemon-reload"); e != nil {
			return activationFailure(fmt.Errorf("systemctl daemon-reload: %s: %w", strings.TrimSpace(string(b)), e))
		}
		if b, e := command("systemctl", "--user", "enable", "--now", maintenanceLabel+".timer"); e != nil {
			return activationFailure(fmt.Errorf("systemctl enable: %s: %w", strings.TrimSpace(string(b)), e))
		}
	} else {
		domain := "gui/" + fmt.Sprint(os.Getuid())
		printOut, printErr := command("launchctl", "print", domain+"/"+maintenanceLabel)
		loaded := printErr == nil
		if printErr != nil {
			if !benignNotLoaded(printOut) && !benignNotLoaded([]byte(printErr.Error())) {
				return activationFailure(fmt.Errorf("verify launchd maintenance state: %s: %w", strings.TrimSpace(string(printOut)), printErr))
			}
		}
		if loaded && (artifactsChanged || activationWasPending) {
			if b, e := command("launchctl", "bootout", domain, p1); e != nil {
				return activationFailure(fmt.Errorf("launchctl bootout: %s: %w", strings.TrimSpace(string(b)), e))
			}
			if b, e := command("launchctl", "print", domain+"/"+maintenanceLabel); e == nil || !benignNotLoaded(b) && !benignNotLoaded([]byte(e.Error())) {
				return activationFailure(fmt.Errorf("verify launchd maintenance bootout: %s: %v", strings.TrimSpace(string(b)), e))
			}
			loaded = false
		}
		if !loaded {
			if b, e := command("launchctl", "bootstrap", domain, p1); e != nil {
				return activationFailure(fmt.Errorf("launchctl bootstrap: %s: %w", strings.TrimSpace(string(b)), e))
			}
			if b, e := command("launchctl", "print", domain+"/"+maintenanceLabel); e != nil {
				return activationFailure(fmt.Errorf("verify launchd maintenance bootstrap: %s: %w", strings.TrimSpace(string(b)), e))
			}
		}
	}
	meta.ActivationPending = false
	meta.PreviousArtifacts = nil
	if err := atomicJSON(dir.MaintenancePath(), meta); err != nil {
		return env, result.Runtime(fmt.Errorf("persist completed maintenance activation: %w", err))
	}
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON && a.stdout != nil {
		fmt.Fprintln(a.stdout, out.Message)
	}
	return env, nil
}
func loadMaintenance(path string) (maintenanceMetadata, error) {
	var m maintenanceMetadata
	if err := decodeStrict(path, &m); err != nil {
		return m, err
	}
	if m.SchemaVersion != 1 || (m.Owner != "self" && m.Owner != "external") || (m.Platform != "linux" && m.Platform != "darwin") || m.Path == "" || m.AmuxPath == "" || m.AmpPath == "" || m.AmpTarget == "" || len(m.Artifacts) == 0 {
		return m, errors.New("malformed runner maintenance metadata")
	}
	return m, nil
}
func decodeStrict(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err = d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing maintenance JSON data")
	}
	return nil
}

func benignNotLoaded(output []byte) bool {
	s := strings.ToLower(string(output))
	return strings.Contains(s, "not loaded") || strings.Contains(s, "not found") || strings.Contains(s, "could not find")
}
func systemdUnitAbsent(output []byte) bool {
	s := strings.ToLower(string(output))
	return strings.Contains(s, "unit") && (strings.Contains(s, "does not exist") || strings.Contains(s, "not found") || strings.Contains(s, "could not be found") || strings.Contains(s, "not loaded"))
}

func launchAgentArtifact(m maintenanceMetadata) (string, error) {
	if len(m.Artifacts) != 1 {
		return "", fmt.Errorf("darwin maintenance metadata must contain exactly one launch agent artifact, found %d", len(m.Artifacts))
	}
	for path := range m.Artifacts {
		if !strings.EqualFold(filepath.Ext(path), ".plist") {
			return "", fmt.Errorf("darwin maintenance artifact is not a plist: %s", path)
		}
		return path, nil
	}
	panic("unreachable")
}

func (a app) removeMaintenance(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	m, err := loadMaintenance(dir.MaintenancePath())
	if os.IsNotExist(err) {
		if _, resultErr := os.Stat(dir.MaintenanceResultPath()); os.IsNotExist(resultErr) {
			out := result.Outcome{Resource: result.ConfigResource(dir.MaintenancePath()), Action: "remove-maintenance", Message: "already absent"}
			env.Skipped = append(env.Skipped, out)
			return env, nil
		} else if resultErr != nil {
			return env, result.Preflight(fmt.Errorf("inspect maintenance result: %w", resultErr))
		}
		out := result.Outcome{Resource: result.ConfigResource(dir.MaintenanceResultPath()), Action: "remove-maintenance", Message: "remove orphaned maintenance result " + dir.MaintenanceResultPath()}
		if in.Options.DryRun {
			env.Planned = append(env.Planned, out)
			if !in.Options.JSON && a.stdout != nil {
				fmt.Fprintln(a.stdout, out.Message)
			}
			return env, nil
		}
		if removeErr := os.Remove(dir.MaintenanceResultPath()); removeErr != nil {
			return env, result.Runtime(fmt.Errorf("remove maintenance result: %w", removeErr))
		}
		env.Successful = append(env.Successful, out)
		if !in.Options.JSON && a.stdout != nil {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}
	if err != nil {
		return env, result.Preflight(fmt.Errorf("load maintenance installation: %w", err))
	}
	launchAgentPath := ""
	if m.Platform == "darwin" {
		launchAgentPath, err = launchAgentArtifact(m)
		if err != nil {
			return env, result.Preflight(err)
		}
	}
	for p, want := range m.Artifacts {
		b, e := os.ReadFile(p)
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return env, result.Preflight(e)
		}
		got := digest(b)
		if got != want && (!m.ActivationPending || got != m.PreviousArtifacts[p]) {
			return env, result.Preflight(fmt.Errorf("refusing to remove unrecognized maintenance artifact %s", p))
		}
	}
	details := maintenanceResultDetails(m)
	out := result.Outcome{Resource: result.ConfigResource(dir.MaintenancePath()), Action: "remove-maintenance", Maintenance: details, Message: maintenancePlanMessage("remove", details)}
	if in.Options.DryRun {
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON && a.stdout != nil {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}
	var b []byte
	if m.Platform == "linux" {
		b, err = command("systemctl", "--user", "stop", maintenanceLabel+".timer")
		if err != nil && !systemdUnitAbsent(append(b, []byte(err.Error())...)) {
			return env, result.Runtime(fmt.Errorf("stop maintenance: %s: %w", strings.TrimSpace(string(b)), err))
		}
		b, err = command("systemctl", "--user", "disable", maintenanceLabel+".timer")
		if err != nil && !systemdUnitAbsent(append(b, []byte(err.Error())...)) {
			return env, result.Runtime(fmt.Errorf("disable maintenance: %s: %w", strings.TrimSpace(string(b)), err))
		}
		err = nil
	} else {
		domain := "gui/" + fmt.Sprint(os.Getuid())
		printOut, printErr := command("launchctl", "print", domain+"/"+maintenanceLabel)
		if printErr == nil {
			b, err = command("launchctl", "bootout", domain, launchAgentPath)
			if err == nil {
				verifyOut, verifyErr := command("launchctl", "print", domain+"/"+maintenanceLabel)
				if verifyErr == nil {
					err = errors.New("launchd job remains loaded after bootout")
				} else if !benignNotLoaded(verifyOut) && !benignNotLoaded([]byte(verifyErr.Error())) {
					err = fmt.Errorf("uncertain launchd state after bootout: %s: %w", strings.TrimSpace(string(verifyOut)), verifyErr)
				}
			}
		} else if !benignNotLoaded(printOut) && !benignNotLoaded([]byte(printErr.Error())) {
			return env, result.Runtime(fmt.Errorf("verify launchd maintenance state: %w", printErr))
		}
	}
	if err != nil && !benignNotLoaded(b) {
		return env, result.Runtime(fmt.Errorf("deactivate maintenance: %s: %w", strings.TrimSpace(string(b)), err))
	}
	for p := range m.Artifacts {
		if e := os.Remove(p); e != nil && !os.IsNotExist(e) {
			return env, result.Runtime(e)
		}
	}
	if m.Platform == "linux" {
		if b, e := command("systemctl", "--user", "daemon-reload"); e != nil {
			return env, result.Runtime(fmt.Errorf("systemctl daemon-reload: %s: %w", strings.TrimSpace(string(b)), e))
		}
	}
	if err := os.Remove(dir.MaintenanceResultPath()); err != nil && !os.IsNotExist(err) {
		return env, result.Runtime(fmt.Errorf("remove maintenance result: %w", err))
	}
	if err := os.Remove(dir.MaintenancePath()); err != nil && !os.IsNotExist(err) {
		return env, result.Runtime(err)
	}
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON && a.stdout != nil {
		fmt.Fprintln(a.stdout, out.Message)
	}
	return env, nil
}
func fingerprint(path string) (string, error) {
	target := resolvePathForComparison(path)
	b, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(append([]byte(target+"\x00"), b...))
	return hex.EncodeToString(h[:]), nil
}
func loadMaintenanceOutcome(path string) (maintenanceOutcome, error) {
	var o maintenanceOutcome
	if err := decodeOutcome(path, &o); err != nil {
		return o, err
	}
	if o.SchemaVersion != 1 || (o.Status != "successful" && o.Status != "skipped" && o.Status != "failed") {
		return o, errors.New("malformed runner maintenance outcome")
	}
	for _, r := range o.Runners {
		if r.Workdir == "" || (r.Status != "successful" && r.Status != "skipped" && r.Status != "failed") || (r.Phase != "" && r.Phase != "restart_required" && r.Phase != "pending_launch") {
			return o, errors.New("malformed runner maintenance runner outcome")
		}
	}
	return o, nil
}
func decodeOutcome(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing maintenance outcome JSON data")
	}
	return nil
}
func executableResult(path, version string, changed bool) result.Outcome {
	return result.Outcome{Resource: result.ExecutableResource(path), Action: "maintenance", Executable: &result.ExecutableDetails{Target: path, Version: version}, Message: map[bool]string{true: "Amp executable change applied", false: "Amp executable unchanged"}[changed]}
}
func pendingMaintenanceRunners(runners []maintenanceRunnerOutcome) []maintenanceRunnerOutcome {
	pending := make([]maintenanceRunnerOutcome, 0, len(runners))
	for _, runner := range runners {
		if runner.Phase == "restart_required" || runner.Phase == "pending_launch" {
			pending = append(pending, runner)
		}
	}
	return pending
}
func (a app) runMaintenance(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	m, err := loadMaintenance(dir.MaintenancePath())
	if err != nil {
		return env, result.Preflight(fmt.Errorf("load maintenance installation: %w", err))
	}
	prior := maintenanceOutcome{}
	if !in.Options.DryRun {
		var priorErr error
		prior, priorErr = loadMaintenanceOutcome(dir.MaintenanceResultPath())
		if priorErr != nil && !os.IsNotExist(priorErr) {
			return env, result.Preflight(fmt.Errorf("load prior maintenance result: %w", priorErr))
		}
	}
	failureBase := maintenanceOutcome{
		SchemaVersion:       1,
		AmpPath:             m.AmpPath,
		AmpVersion:          prior.AmpVersion,
		ObservedFingerprint: prior.ObservedFingerprint,
		AppliedFingerprint:  prior.AppliedFingerprint,
		AppliedVersion:      prior.AppliedVersion,
		Changed:             prior.Changed,
		Runners:             prior.Runners,
	}
	ampPath, e := canonicalExecutable(m.AmpPath)
	currentTarget := resolvePathForComparison(ampPath)
	if e != nil || (m.Owner == "self" && currentTarget != m.AmpTarget) {
		if e == nil {
			e = fmt.Errorf("persisted Amp target changed from %s to %s", m.AmpTarget, currentTarget)
		}
		cause := fmt.Errorf("validate persisted Amp executable: %w", e)
		if in.Options.DryRun {
			return env, result.Preflight(cause)
		}
		return env, a.persistMaintenanceFailure(dir, env, failureBase, cause)
	}
	if m.Owner == "self" && managedInstallPath(resolvePathForComparison(ampPath)) {
		cause := fmt.Errorf("self-owned maintenance refused for package/toolchain-managed Amp executable %s", resolvePathForComparison(ampPath))
		if in.Options.DryRun {
			return env, result.Preflight(cause)
		}
		return env, a.persistMaintenanceFailure(dir, env, failureBase, cause)
	}
	planDetails := maintenanceResultDetails(m)
	if in.Options.DryRun {
		out := result.Outcome{Resource: result.ExecutableResource(m.AmpPath), Action: "run-maintenance", Maintenance: planDetails, Message: maintenancePlanMessage("run", planDetails)}
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON && a.stdout != nil {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}
	base := maintenanceOutcome{SchemaVersion: 1, Status: "failed", Time: maintenanceNow().UTC(), AmpPath: m.AmpPath, AppliedFingerprint: prior.AppliedFingerprint, AppliedVersion: prior.AppliedVersion, Runners: prior.Runners}
	observed, e := fingerprint(m.AmpPath)
	if e != nil {
		return env, a.persistMaintenanceFailure(dir, env, base, e)
	}
	base.ObservedFingerprint = observed
	if m.Owner == "self" {
		if base.AppliedFingerprint == "" {
			base.AppliedFingerprint = observed
			if e := atomicJSON(dir.MaintenanceResultPath(), base); e != nil {
				return env, result.Runtime(fmt.Errorf("persist maintenance baseline: %w", e))
			}
		}
		if b, e := command(m.AmpPath, "update"); e != nil {
			return env, a.persistMaintenanceFailure(dir, env, base, fmt.Errorf("amp update: %s: %w", strings.TrimSpace(string(b)), e))
		}
		observed, e = fingerprint(m.AmpPath)
		if e != nil {
			return env, a.persistMaintenanceFailure(dir, env, base, e)
		}
		base.ObservedFingerprint = observed
	}
	// A stopped runner remains our responsibility across executable identities,
	// but completion checkpoints only apply to the identity that produced them.
	if prior.ObservedFingerprint != observed {
		base.Runners = pendingMaintenanceRunners(base.Runners)
	}
	vout, e := command(m.AmpPath, "version")
	if e != nil {
		return env, a.persistMaintenanceFailure(dir, env, base, fmt.Errorf("amp version: %s: %w", strings.TrimSpace(string(vout)), e))
	}
	base.AmpVersion = strings.TrimSpace(string(vout))
	if prior.ObservedFingerprint != observed || prior.AmpVersion != base.AmpVersion {
		base.Runners = pendingMaintenanceRunners(base.Runners)
	}
	if m.Owner == "external" && currentTarget != m.AmpTarget {
		m.AmpTarget = currentTarget
		if e := atomicJSON(dir.MaintenancePath(), m); e != nil {
			return env, a.persistMaintenanceFailure(dir, env, base, fmt.Errorf("persist observed Amp target: %w", e))
		}
	}
	if base.AppliedFingerprint == "" && m.Owner == "external" {
		base.AppliedFingerprint = observed
		base.AppliedVersion = base.AmpVersion
		base.Status = "skipped"
		if e := atomicJSON(dir.MaintenanceResultPath(), base); e != nil {
			return env, result.Runtime(fmt.Errorf("persist maintenance baseline: %w", e))
		}
		env.Skipped = append(env.Skipped, executableResult(m.AmpPath, base.AmpVersion, false))
		return env, nil
	}
	changed := observed != base.AppliedFingerprint || base.AmpVersion != base.AppliedVersion
	base.Changed = changed
	hasPendingLaunch := false
	for _, runner := range base.Runners {
		if runner.Phase == "restart_required" || runner.Phase == "pending_launch" {
			hasPendingLaunch = true
			break
		}
	}
	if !changed && !hasPendingLaunch {
		base.Status = "skipped"
		base.AppliedVersion = base.AmpVersion
		if e := atomicJSON(dir.MaintenanceResultPath(), base); e != nil {
			return env, result.Runtime(fmt.Errorf("persist unchanged maintenance result: %w", e))
		}
		env.Skipped = append(env.Skipped, executableResult(m.AmpPath, base.AmpVersion, false))
		return env, nil
	}
	rows, e := config.LoadRunnersReadOnly(dir.RunnersPath())
	if e != nil {
		return env, a.persistMaintenanceFailure(dir, env, base, e)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Workdir < rows[j].Workdir })
	completed := map[string]bool{}
	pendingPhase := map[string]string{}
	sameObservedIdentity := prior.ObservedFingerprint == observed && prior.AmpVersion == base.AmpVersion
	for _, runner := range base.Runners {
		pendingPhase[runner.Workdir] = runner.Phase
		if prior.Status == "failed" && sameObservedIdentity {
			completed[runner.Workdir] = runner.Status != "failed"
		}
	}
	base.Runners = nil
	for _, row := range rows {
		if phase := pendingPhase[row.Workdir]; phase != "" {
			base.Runners = append(base.Runners, maintenanceRunnerOutcome{Workdir: row.Workdir, Status: "failed", Phase: phase})
		}
	}
	setProgressInMemory := func(ro maintenanceRunnerOutcome) {
		for i := range base.Runners {
			if base.Runners[i].Workdir == ro.Workdir {
				base.Runners[i] = ro
				return
			}
		}
		base.Runners = append(base.Runners, ro)
	}
	setProgress := func(ro maintenanceRunnerOutcome) error {
		setProgressInMemory(ro)
		return atomicJSON(dir.MaintenanceResultPath(), base)
	}
	failed := false
	for _, row := range rows {
		out := runnerOutcome(row, "maintenance-restart", "")
		ro := maintenanceRunnerOutcome{Workdir: row.Workdir}
		if completed[row.Workdir] {
			ro.Status = "skipped"
			out.Message = "restart already completed for this executable"
			env.Skipped = append(env.Skipped, out)
			base.Runners = append(base.Runners, ro)
			continue
		}
		inspection, ie := inspectRunner(row)
		if ie != nil || inspection.state == runnerPaneConflict || inspection.state == runnerPaneAmbiguous {
			if ie == nil {
				ie = fmt.Errorf("runner identity is %s", inspection.state)
			}
			ro.Status, ro.Error = "failed", ie.Error()
			if pendingPhase[row.Workdir] != "" {
				ro.Phase = pendingPhase[row.Workdir]
			}
			out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: ie.Error()}
			env.Failed = append(env.Failed, out)
			failed = true
		} else if pendingPhase[row.Workdir] == "pending_launch" && inspection.state == runnerPaneExact {
			ro.Status = "skipped"
			out.Message = "pending runner launch already recovered"
			env.Skipped = append(env.Skipped, out)
		} else if inspection.state == runnerPaneAbsent && pendingPhase[row.Workdir] == "" {
			ro.Status = "skipped"
			out.Message = "stopped runner preserved"
			env.Skipped = append(env.Skipped, out)
		} else {
			e = requireLockedWorktree(row.Workdir)
			stopped := pendingPhase[row.Workdir] == "pending_launch" || inspection.state == runnerPaneAbsent
			if e == nil && !stopped {
				ro.Status, ro.Phase = "failed", "restart_required"
				if e = setProgress(ro); e != nil {
					return env, result.Runtime(fmt.Errorf("persist runner restart checkpoint: %w", e))
				}
				e = stopRunner(row, inspection)
				stopped = e == nil
				if stopped {
					ro.Phase = "pending_launch"
					if e = setProgress(ro); e != nil {
						return env, result.Runtime(fmt.Errorf("persist pending runner launch: %w", e))
					}
				}
			}
			if e == nil && stopped && ro.Phase != "pending_launch" {
				ro.Status, ro.Phase = "failed", "pending_launch"
				if e = setProgress(ro); e != nil {
					return env, result.Runtime(fmt.Errorf("persist pending runner launch: %w", e))
				}
			}
			if e == nil {
				row.Window = config.RunnerWindow(row.Workdir)
				row.LegacyWindow = false
				_, e = launchRunner(row)
			}
			if e != nil {
				ro.Status, ro.Error = "failed", e.Error()
				if stopped {
					ro.Phase = "pending_launch"
				}
				out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: e.Error()}
				env.Failed = append(env.Failed, out)
				failed = true
			} else {
				ro.Status = "successful"
				env.Successful = append(env.Successful, out)
			}
		}
		setProgressInMemory(ro)
	}
	if failed {
		base.Status = "failed"
		base.Error = "one or more runner restarts failed"
	} else {
		base.Status = "successful"
		base.AppliedFingerprint = observed
		base.AppliedVersion = base.AmpVersion
	}
	if e := atomicJSON(dir.MaintenanceResultPath(), base); e != nil {
		return env, result.Runtime(fmt.Errorf("persist maintenance result after runner processing: %w", e))
	}
	execOut := executableResult(m.AmpPath, base.AmpVersion, !failed)
	if failed {
		execOut.Error = &result.Failure{Kind: result.ErrorRuntime, Message: base.Error}
		env.Failed = append(env.Failed, execOut)
		return env, result.Runtime(errors.New(base.Error))
	}
	env.Successful = append(env.Successful, execOut)
	return env, nil
}
func (a app) persistMaintenanceFailure(dir config.Directory, env *result.Envelope, o maintenanceOutcome, cause error) error {
	o.Status = "failed"
	o.Time = maintenanceNow().UTC()
	o.Error = cause.Error()
	if err := atomicJSON(dir.MaintenanceResultPath(), o); err != nil {
		return result.Runtime(fmt.Errorf("%v; additionally failed to persist maintenance result: %w", cause, err))
	}
	out := executableResult(o.AmpPath, o.AmpVersion, false)
	out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: cause.Error()}
	env.Failed = append(env.Failed, out)
	return result.Runtime(cause)
}
func maintenanceDoctorDetails(dir config.Directory) (*result.MaintenanceDetails, error) {
	m, mErr := loadMaintenance(dir.MaintenancePath())
	o, oErr := loadMaintenanceOutcome(dir.MaintenanceResultPath())
	if os.IsNotExist(mErr) && os.IsNotExist(oErr) {
		return nil, nil
	}
	if os.IsNotExist(mErr) && oErr == nil {
		d := &result.MaintenanceDetails{
			AmpPath: o.AmpPath, AmpVersion: o.AmpVersion, Status: o.Status,
			Changed: o.Changed, ObservedFingerprint: o.ObservedFingerprint,
			AppliedFingerprint: o.AppliedFingerprint, AppliedVersion: o.AppliedVersion,
			Error: "maintenance result exists without installation metadata; run `amux runner maintenance remove`",
		}
		if !o.Time.IsZero() {
			d.Time = o.Time.Format(time.RFC3339)
		}
		for _, r := range o.Runners {
			d.RunnerOutcomes = append(d.RunnerOutcomes, result.MaintenanceRunnerDetails{Workdir: r.Workdir, Status: r.Status, Phase: r.Phase, Error: r.Error})
		}
		return d, nil
	}
	if mErr != nil {
		return nil, fmt.Errorf("maintenance metadata: %w; reinstall with `amux runner maintenance install --update-owner <self|external>`", mErr)
	}
	if oErr != nil && !os.IsNotExist(oErr) {
		return nil, fmt.Errorf("maintenance outcome: %w; run `amux runner maintenance run`", oErr)
	}
	d := maintenanceResultDetails(m)
	if os.IsNotExist(oErr) {
		d.Error = "installed maintenance metadata has no outcome"
	} else {
		d.Status, d.Error, d.AmpVersion, d.Changed, d.ObservedFingerprint, d.AppliedFingerprint, d.AppliedVersion = o.Status, o.Error, o.AmpVersion, o.Changed, o.ObservedFingerprint, o.AppliedFingerprint, o.AppliedVersion
		if o.Status == "failed" && o.Error == "" {
			d.Error = "latest maintenance outcome failed without an error"
		}
	}
	if m.ActivationPending {
		d.Error = appendMaintenanceDoctorError(d.Error, "maintenance scheduler activation pending; reinstall maintenance")
	}
	for p, want := range m.Artifacts {
		b, err := os.ReadFile(p)
		if err != nil {
			d.Error = appendMaintenanceDoctorError(d.Error, fmt.Sprintf("maintenance artifact %s: %v; reinstall maintenance", p, err))
			continue
		}
		if digest(b) != want {
			d.Error = appendMaintenanceDoctorError(d.Error, fmt.Sprintf("maintenance artifact drift at %s; reinstall maintenance", p))
		}
	}
	if m.Platform == "linux" {
		enabled, ee := command("systemctl", "--user", "is-enabled", maintenanceLabel+".timer")
		active, ae := command("systemctl", "--user", "is-active", maintenanceLabel+".timer")
		d.SchedulerState = strings.TrimSpace(string(enabled)) + "/" + strings.TrimSpace(string(active))
		if ee != nil || ae != nil {
			d.Error = appendMaintenanceDoctorError(d.Error, fmt.Sprintf("systemd timer unhealthy (%s): enabled=%v active=%v; reinstall maintenance", d.SchedulerState, ee, ae))
		}
	} else {
		b, err := command("launchctl", "print", "gui/"+fmt.Sprint(os.Getuid())+"/"+maintenanceLabel)
		if err != nil {
			d.Error = appendMaintenanceDoctorError(d.Error, fmt.Sprintf("launchd job unhealthy: %s: %v; reinstall maintenance", strings.TrimSpace(string(b)), err))
		} else {
			d.SchedulerState = "loaded"
		}
	}
	if !o.Time.IsZero() {
		d.Time = o.Time.Format(time.RFC3339)
	}
	for _, r := range o.Runners {
		d.RunnerOutcomes = append(d.RunnerOutcomes, result.MaintenanceRunnerDetails{Workdir: r.Workdir, Status: r.Status, Phase: r.Phase, Error: r.Error})
		if r.Phase == "restart_required" || r.Phase == "pending_launch" {
			problem := fmt.Sprintf("runner %s is in maintenance phase %s", r.Workdir, r.Phase)
			if r.Error != "" {
				problem += ": " + r.Error
			}
			d.Error = appendMaintenanceDoctorError(d.Error, problem)
		}
	}
	if d.Error != "" {
		d.Error = appendMaintenanceDoctorError(d.Error, "run `amux runner maintenance run`")
	}
	return d, nil
}

func appendMaintenanceDoctorError(existing, problem string) string {
	if existing == "" {
		return problem
	}
	if problem == "" || strings.Contains(existing, problem) {
		return existing
	}
	return existing + "; " + problem
}

func maintenanceResultDetails(m maintenanceMetadata) *result.MaintenanceDetails {
	d := &result.MaintenanceDetails{Owner: m.Owner, Schedule: m.Schedule, Platform: m.Platform, Path: m.Path, AmuxPath: m.AmuxPath, AmpPath: m.AmpPath, AmpTarget: m.AmpTarget}
	for p := range m.Artifacts {
		d.ArtifactPaths = append(d.ArtifactPaths, p)
	}
	sort.Strings(d.ArtifactPaths)
	return d
}

func maintenancePlanMessage(action string, d *result.MaintenanceDetails) string {
	return fmt.Sprintf("%s maintenance: amux=%s amp=%s target=%s owner=%s schedule=%s artifacts=%s", action, d.AmuxPath, d.AmpPath, d.AmpTarget, d.Owner, d.Schedule, strings.Join(d.ArtifactPaths, ","))
}
