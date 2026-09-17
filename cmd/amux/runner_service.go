package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

const runnerServiceLabelPrefix = "com.zainfathoni.amux.runner."

var runnerServiceGOOS = runtime.GOOS
var runnerServiceHome = os.UserHomeDir
var runnerServiceUserConfigDir = os.UserConfigDir
var runnerServiceLookPath = exec.LookPath
var runnerServiceTimeout = 30 * time.Second
var runnerServiceExec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type runnerServiceMetadata struct {
	SchemaVersion     int               `json:"schema_version"`
	Platform          string            `json:"platform"`
	AmpPath           string            `json:"amp_path"`
	Profiles          []string          `json:"profiles"`
	Artifacts         map[string]string `json:"artifacts"`
	ActivationPending bool              `json:"activation_pending,omitempty"`
	PreviousArtifacts map[string]string `json:"previous_artifacts,omitempty"`
}

func (a app) executeRunnerService(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if len(in.Args) != 0 || !selectorsEmpty(in.Selectors) {
		return &env, result.Request(fmt.Errorf("usage: %s", in.Command.Usage))
	}
	if runnerServiceGOOS != "linux" && runnerServiceGOOS != "darwin" {
		return &env, result.Preflight(fmt.Errorf("runner services are unsupported on %s", runnerServiceGOOS))
	}
	switch in.Command.Name {
	case "install":
		return a.installRunnerServices(in, dir, &env)
	case "remove":
		return a.removeRunnerServices(in, dir, &env)
	case "doctor":
		return a.doctorRunnerServices(in, dir, &env)
	default:
		return &env, result.Request(errors.New("unknown runner service operation"))
	}
}

func nativeRunnerArgs(profile config.NativeRunnerProfile) []string {
	args := []string{"--no-tui", "--runner-id", profile.RunnerID}
	if profile.DiscoverDirectories {
		args = append(args, "--discover-dirs")
	}
	for _, directory := range profile.Directories {
		args = append(args, "--dir", directory)
	}
	if profile.RemoteControlTerminal {
		args = append(args, "--remote-control-terminal")
	}
	return args
}

func systemdRunnerServiceArtifact(ampPath string, profile config.NativeRunnerProfile) string {
	arguments := []string{systemdQuote(ampPath)}
	for _, argument := range nativeRunnerArgs(profile) {
		arguments = append(arguments, systemdQuote(argument))
	}
	return "[Unit]\nDescription=Amp native runner " + profile.Name + "\nWants=network-online.target\nAfter=network-online.target\n\n[Service]\nType=simple\nWorkingDirectory=" + systemdQuote(profile.StartupDirectory) + "\nExecStart=" + strings.Join(arguments, " ") + "\nRestart=always\nRestartSec=5s\n\n[Install]\nWantedBy=default.target\n"
}

func launchdRunnerServiceArtifact(ampPath string, profile config.NativeRunnerProfile) string {
	var arguments strings.Builder
	arguments.WriteString("<string>" + html.EscapeString(ampPath) + "</string>")
	for _, argument := range nativeRunnerArgs(profile) {
		arguments.WriteString("<string>" + html.EscapeString(argument) + "</string>")
	}
	label := runnerServiceLabelPrefix + profile.Name
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n" +
		`<plist version="1.0"><dict><key>Label</key><string>` + html.EscapeString(label) +
		`</string><key>ProgramArguments</key><array>` + arguments.String() +
		`</array><key>WorkingDirectory</key><string>` + html.EscapeString(profile.StartupDirectory) +
		`</string><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>5</integer><key>ProcessType</key><string>Background</string></dict></plist>` + "\n"
}

func runnerServiceArtifacts(ampPath string, profiles []config.NativeRunnerProfile) (map[string][]byte, error) {
	artifacts := make(map[string][]byte, len(profiles))
	if runnerServiceGOOS == "linux" {
		root, err := runnerServiceUserConfigDir()
		if err != nil || root == "" {
			home, homeErr := runnerServiceHome()
			if homeErr != nil {
				return nil, homeErr
			}
			root = filepath.Join(home, ".config")
		}
		for _, profile := range profiles {
			path := filepath.Join(root, "systemd", "user", runnerServiceLabelPrefix+profile.Name+".service")
			artifacts[path] = []byte(systemdRunnerServiceArtifact(ampPath, profile))
		}
		return artifacts, nil
	}
	home, err := runnerServiceHome()
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		path := filepath.Join(home, "Library", "LaunchAgents", runnerServiceLabelPrefix+profile.Name+".plist")
		artifacts[path] = []byte(launchdRunnerServiceArtifact(ampPath, profile))
	}
	return artifacts, nil
}

func loadRunnerServiceMetadata(path string) (runnerServiceMetadata, error) {
	var metadata runnerServiceMetadata
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return metadata, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return metadata, errors.New("runner service metadata has trailing data")
	}
	if metadata.SchemaVersion != 1 || (metadata.Platform != "linux" && metadata.Platform != "darwin") || !filepath.IsAbs(metadata.AmpPath) || metadata.Artifacts == nil {
		return metadata, errors.New("malformed runner service metadata")
	}
	if !sort.StringsAreSorted(metadata.Profiles) {
		return metadata, errors.New("runner service metadata profiles are not sorted")
	}
	seenProfiles := make(map[string]struct{}, len(metadata.Profiles))
	for _, profile := range metadata.Profiles {
		if _, duplicate := seenProfiles[profile]; duplicate || profile == "" {
			return metadata, errors.New("runner service metadata contains invalid profiles")
		}
		seenProfiles[profile] = struct{}{}
	}
	if len(metadata.Artifacts) != len(metadata.Profiles) || (!metadata.ActivationPending && len(metadata.PreviousArtifacts) != 0) {
		return metadata, errors.New("runner service metadata has inconsistent artifacts")
	}
	for path, artifactDigest := range metadata.Artifacts {
		if !filepath.IsAbs(path) || len(artifactDigest) != 64 {
			return metadata, errors.New("runner service metadata contains invalid artifacts")
		}
	}
	for path, artifactDigest := range metadata.PreviousArtifacts {
		if !filepath.IsAbs(path) || len(artifactDigest) != 64 {
			return metadata, errors.New("runner service metadata contains invalid previous artifacts")
		}
	}
	for _, profile := range metadata.Profiles {
		suffix := runnerServiceLabelPrefix + profile
		if metadata.Platform == "linux" {
			suffix += ".service"
		} else {
			suffix += ".plist"
		}
		found := false
		for path := range metadata.Artifacts {
			if filepath.Base(path) == suffix {
				found = true
				break
			}
		}
		if !found {
			return metadata, errors.New("runner service metadata profile does not match its artifact")
		}
	}
	return metadata, nil
}

func runnerServiceCall(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runnerServiceTimeout)
	defer cancel()
	output, err := runnerServiceExec(ctx, name, args...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output, fmt.Errorf("command timed out after %s: %w", runnerServiceTimeout, ctx.Err())
	}
	return output, err
}

func verifyOwnedRunnerServiceArtifacts(metadata runnerServiceMetadata) error {
	paths := make(map[string]struct{}, len(metadata.Artifacts)+len(metadata.PreviousArtifacts))
	for path := range metadata.PreviousArtifacts {
		paths[path] = struct{}{}
	}
	for path := range metadata.Artifacts {
		paths[path] = struct{}{}
	}
	for path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		actual := digest(data)
		currentDigest, current := metadata.Artifacts[path]
		previousDigest, previous := metadata.PreviousArtifacts[path]
		if (!current || actual != currentDigest) && (!metadata.ActivationPending || !previous || actual != previousDigest) {
			return fmt.Errorf("refusing to modify unrecognized runner service artifact %s", path)
		}
	}
	return nil
}

func (a app) installRunnerServices(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	configuration, err := config.LoadNativeRunners(dir.NativeRunnersPath())
	if err != nil {
		return env, result.Preflight(fmt.Errorf("load %s: %w", dir.NativeRunnersPath(), err))
	}
	if len(configuration.Runners) == 0 {
		return env, result.Preflight(errors.New("native-runners.json must contain at least one runner profile"))
	}
	ampPath, err := runnerServiceLookPath("amp")
	if err != nil {
		return env, result.Preflight(fmt.Errorf("resolve Amp: %w", err))
	}
	ampPath, err = canonicalExecutable(ampPath)
	if err != nil {
		return env, result.Preflight(err)
	}
	artifacts, err := runnerServiceArtifacts(ampPath, configuration.Runners)
	if err != nil {
		return env, result.Preflight(err)
	}
	prior, priorErr := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if priorErr == nil {
		if prior.Platform != runnerServiceGOOS {
			return env, result.Preflight(fmt.Errorf("installed runner services target %s; remove them before installing for %s", prior.Platform, runnerServiceGOOS))
		}
		if err := verifyOwnedRunnerServiceArtifacts(prior); err != nil {
			return env, result.Preflight(err)
		}
	} else if !os.IsNotExist(priorErr) {
		return env, result.Preflight(fmt.Errorf("load runner service metadata: %w", priorErr))
	}
	for path, expected := range artifacts {
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			_, previouslyOwned := prior.Artifacts[path]
			if !previouslyOwned && digest(data) != digest(expected) {
				return env, result.Preflight(fmt.Errorf("refusing to overwrite unrecognized runner service artifact %s", path))
			}
		} else if !os.IsNotExist(readErr) {
			return env, result.Preflight(readErr)
		}
	}
	paths := sortedArtifactPaths(artifacts)
	outcomes := make([]result.Outcome, 0, len(paths))
	for _, path := range paths {
		out := result.Outcome{Resource: result.ConfigResource(path), Action: "install-runner-service", Message: "install native Amp runner service " + path}
		if in.Options.DryRun {
			env.Planned = append(env.Planned, out)
			if !in.Options.JSON {
				fmt.Fprintln(a.stdout, out.Message)
			}
		} else {
			outcomes = append(outcomes, out)
		}
	}
	if in.Options.DryRun {
		return env, nil
	}
	profiles := make([]string, 0, len(configuration.Runners))
	digests := make(map[string]string, len(artifacts))
	previous := make(map[string]string)
	for _, profile := range configuration.Runners {
		profiles = append(profiles, profile.Name)
	}
	sort.Strings(profiles)
	for path, data := range artifacts {
		digests[path] = digest(data)
	}
	if priorErr == nil {
		for path := range prior.Artifacts {
			if data, readErr := os.ReadFile(path); readErr == nil {
				previous[path] = digest(data)
			}
		}
		if err := deactivateRunnerServices(prior); err != nil {
			return env, result.Runtime(err)
		}
	}
	metadata := runnerServiceMetadata{SchemaVersion: 1, Platform: runnerServiceGOOS, AmpPath: ampPath, Profiles: profiles, Artifacts: digests, ActivationPending: true, PreviousArtifacts: previous}
	if err := atomicJSON(dir.RunnerServicesPath(), metadata); err != nil {
		return env, result.Runtime(err)
	}
	oldArtifacts := make(map[string]struct{}, len(prior.Artifacts)+len(prior.PreviousArtifacts))
	for path := range prior.Artifacts {
		oldArtifacts[path] = struct{}{}
	}
	for path := range prior.PreviousArtifacts {
		oldArtifacts[path] = struct{}{}
	}
	for path := range oldArtifacts {
		if _, retained := artifacts[path]; !retained {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return env, result.Runtime(err)
			}
		}
	}
	for path, data := range artifacts {
		if err := atomicWrite(path, data, 0o600); err != nil {
			return env, result.Runtime(err)
		}
	}
	if err := activateRunnerServices(metadata); err != nil {
		return env, result.Runtime(err)
	}
	metadata.ActivationPending = false
	metadata.PreviousArtifacts = nil
	if err := atomicJSON(dir.RunnerServicesPath(), metadata); err != nil {
		return env, result.Runtime(err)
	}
	env.Successful = append(env.Successful, outcomes...)
	if !in.Options.JSON {
		for _, out := range outcomes {
			fmt.Fprintln(a.stdout, out.Message)
		}
	}
	return env, nil
}

func sortedArtifactPaths(artifacts map[string][]byte) []string {
	paths := make([]string, 0, len(artifacts))
	for path := range artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func activateRunnerServices(metadata runnerServiceMetadata) error {
	paths := make([]string, 0, len(metadata.Artifacts))
	for path := range metadata.Artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if metadata.Platform == "linux" {
		if output, err := runnerServiceCall("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %s: %w", strings.TrimSpace(string(output)), err)
		}
		for _, path := range paths {
			if output, err := runnerServiceCall("systemctl", "--user", "enable", "--now", filepath.Base(path)); err != nil {
				return fmt.Errorf("systemctl enable %s: %s: %w", filepath.Base(path), strings.TrimSpace(string(output)), err)
			}
		}
		return nil
	}
	domain := "gui/" + fmt.Sprint(os.Getuid())
	for _, path := range paths {
		if output, err := runnerServiceCall("launchctl", "bootstrap", domain, path); err != nil {
			return fmt.Errorf("launchctl bootstrap %s: %s: %w", path, strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

func deactivateRunnerServices(metadata runnerServiceMetadata) error {
	paths := make([]string, 0, len(metadata.Artifacts))
	for path := range metadata.Artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if metadata.Platform == "linux" {
		for _, path := range paths {
			output, err := runnerServiceCall("systemctl", "--user", "disable", "--now", filepath.Base(path))
			if err != nil && !systemdUnitAbsent(append(output, []byte(err.Error())...)) {
				return fmt.Errorf("systemctl disable %s: %s: %w", filepath.Base(path), strings.TrimSpace(string(output)), err)
			}
		}
		return nil
	}
	domain := "gui/" + fmt.Sprint(os.Getuid())
	for _, path := range paths {
		output, err := runnerServiceCall("launchctl", "bootout", domain, path)
		if err != nil && !benignNotLoaded(output) && !benignNotLoaded([]byte(err.Error())) {
			return fmt.Errorf("launchctl bootout %s: %s: %w", path, strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

func (a app) removeRunnerServices(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	metadata, err := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if os.IsNotExist(err) {
		env.Skipped = append(env.Skipped, result.Outcome{Resource: result.ConfigResource(dir.RunnerServicesPath()), Action: "remove-runner-services", Message: "runner services already absent"})
		return env, nil
	}
	if err != nil {
		return env, result.Preflight(fmt.Errorf("load runner service metadata: %w", err))
	}
	if metadata.ActivationPending {
		return env, result.Preflight(errors.New("runner service installation was interrupted; run amux runner service install again before removing services"))
	}
	if err := verifyOwnedRunnerServiceArtifacts(metadata); err != nil {
		return env, result.Preflight(err)
	}
	paths := make([]string, 0, len(metadata.Artifacts))
	for path := range metadata.Artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	outcomes := make([]result.Outcome, 0, len(paths))
	for _, path := range paths {
		out := result.Outcome{Resource: result.ConfigResource(path), Action: "remove-runner-service", Message: "remove native Amp runner service " + path}
		if in.Options.DryRun {
			env.Planned = append(env.Planned, out)
			if !in.Options.JSON {
				fmt.Fprintln(a.stdout, out.Message)
			}
		} else {
			outcomes = append(outcomes, out)
		}
	}
	if in.Options.DryRun {
		return env, nil
	}
	if err := deactivateRunnerServices(metadata); err != nil {
		return env, result.Runtime(err)
	}
	for path := range metadata.Artifacts {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return env, result.Runtime(err)
		}
	}
	if metadata.Platform == "linux" {
		if output, err := runnerServiceCall("systemctl", "--user", "daemon-reload"); err != nil {
			return env, result.Runtime(fmt.Errorf("systemctl daemon-reload: %s: %w", strings.TrimSpace(string(output)), err))
		}
	}
	if err := os.Remove(dir.RunnerServicesPath()); err != nil && !os.IsNotExist(err) {
		return env, result.Runtime(err)
	}
	env.Successful = append(env.Successful, outcomes...)
	if !in.Options.JSON {
		for _, out := range outcomes {
			fmt.Fprintln(a.stdout, out.Message)
		}
	}
	return env, nil
}

func (a app) doctorRunnerServices(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	configuration, configErr := config.LoadNativeRunners(dir.NativeRunnersPath())
	metadata, metadataErr := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if configErr != nil {
		return env, result.Preflight(fmt.Errorf("load %s: %w", dir.NativeRunnersPath(), configErr))
	}
	if metadataErr != nil {
		return env, result.Preflight(fmt.Errorf("load runner service metadata: %w", metadataErr))
	}
	if metadata.Platform != runnerServiceGOOS {
		return env, result.Preflight(fmt.Errorf("runner services target %s, not %s", metadata.Platform, runnerServiceGOOS))
	}
	if metadata.ActivationPending {
		return env, result.Preflight(errors.New("runner service installation was interrupted; run amux runner service install again"))
	}
	if err := verifyOwnedRunnerServiceArtifacts(metadata); err != nil {
		return env, result.Preflight(err)
	}
	profiles := make([]string, 0, len(configuration.Runners))
	for _, profile := range configuration.Runners {
		profiles = append(profiles, profile.Name)
	}
	sort.Strings(profiles)
	if !slices.Equal(profiles, metadata.Profiles) {
		return env, result.Preflight(errors.New("runner service installation does not match native-runners.json; reinstall services"))
	}
	expected, err := runnerServiceArtifacts(metadata.AmpPath, configuration.Runners)
	if err != nil {
		return env, result.Preflight(err)
	}
	expectedDigests := make(map[string]string, len(expected))
	for path, data := range expected {
		expectedDigests[path] = digest(data)
	}
	if !maps.Equal(expectedDigests, metadata.Artifacts) {
		return env, result.Preflight(errors.New("runner service installation does not match native-runners.json; reinstall services"))
	}
	if err := checkRunnerServicesActive(metadata); err != nil {
		return env, result.Preflight(err)
	}
	for _, profile := range configuration.Runners {
		out := result.Outcome{Resource: result.ConfigResource(dir.NativeRunnersPath()), Action: "doctor-runner-service", Message: fmt.Sprintf("runner profile %s serves startup=%s discover=%t explicit-dirs=%d", profile.Name, profile.StartupDirectory, profile.DiscoverDirectories, len(profile.Directories))}
		env.Successful = append(env.Successful, out)
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
	}
	return env, nil
}

func checkRunnerServicesActive(metadata runnerServiceMetadata) error {
	paths := make([]string, 0, len(metadata.Artifacts))
	for path := range metadata.Artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if metadata.Platform == "linux" {
		for _, path := range paths {
			unit := filepath.Base(path)
			if output, err := runnerServiceCall("systemctl", "--user", "is-enabled", unit); err != nil {
				return fmt.Errorf("runner service %s is not enabled: %s: %w", unit, strings.TrimSpace(string(output)), err)
			}
			if output, err := runnerServiceCall("systemctl", "--user", "is-active", unit); err != nil {
				return fmt.Errorf("runner service %s is not active: %s: %w", unit, strings.TrimSpace(string(output)), err)
			}
		}
		return nil
	}
	domain := "gui/" + fmt.Sprint(os.Getuid())
	for _, profile := range metadata.Profiles {
		label := runnerServiceLabelPrefix + profile
		if output, err := runnerServiceCall("launchctl", "print", domain+"/"+label); err != nil {
			return fmt.Errorf("runner service %s is not loaded: %s: %w", label, strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}
