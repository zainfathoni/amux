package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const NativeRunnersSchemaVersion = 1

var runnerProfileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type NativeRunnerConfig struct {
	SchemaVersion int                   `json:"schema_version"`
	Runners       []NativeRunnerProfile `json:"runners"`
}

type NativeRunnerProfile struct {
	Name                  string   `json:"name"`
	RunnerID              string   `json:"runner_id"`
	StartupDirectory      string   `json:"startup_directory"`
	DiscoverDirectories   bool     `json:"discover_dirs,omitempty"`
	Directories           []string `json:"dirs,omitempty"`
	RemoteControlTerminal bool     `json:"remote_control_terminal,omitempty"`
}

func LoadNativeRunners(path string) (NativeRunnerConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return NativeRunnerConfig{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var document NativeRunnerConfig
	if err := decoder.Decode(&document); err != nil {
		return document, fmt.Errorf("decode native runner configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return document, errors.New("native runner configuration has trailing data")
	}
	if err := document.Validate(); err != nil {
		return document, err
	}
	return document, nil
}

func (c *NativeRunnerConfig) Validate() error {
	if c.SchemaVersion != NativeRunnersSchemaVersion {
		return fmt.Errorf("native runner configuration schema_version must be %d", NativeRunnersSchemaVersion)
	}
	seenNames := make(map[string]bool, len(c.Runners))
	seenIDs := make(map[string]bool, len(c.Runners))
	seenStartups := make(map[string]string, len(c.Runners))
	for i := range c.Runners {
		profile := &c.Runners[i]
		if !runnerProfileNamePattern.MatchString(profile.Name) {
			return fmt.Errorf("runner profile %d name must match %s", i+1, runnerProfileNamePattern)
		}
		if seenNames[profile.Name] {
			return fmt.Errorf("duplicate runner profile name %q", profile.Name)
		}
		seenNames[profile.Name] = true
		if err := validateRunnerID(profile.RunnerID); err != nil {
			return fmt.Errorf("runner profile %q: %w", profile.Name, err)
		}
		id := strings.ToLower(profile.RunnerID)
		if seenIDs[id] {
			return fmt.Errorf("duplicate runner ID %q", profile.RunnerID)
		}
		seenIDs[id] = true
		startup, err := canonicalExistingDirectory(profile.StartupDirectory)
		if err != nil {
			return fmt.Errorf("runner profile %q startup_directory: %w", profile.Name, err)
		}
		if previous, exists := seenStartups[startup]; exists {
			return fmt.Errorf("runner profiles %q and %q share startup_directory %s; use a dedicated startup directory per profile", previous, profile.Name, startup)
		}
		seenStartups[startup] = profile.Name
		profile.StartupDirectory = startup
		seenDirectories := map[string]bool{startup: true}
		for j, directory := range profile.Directories {
			canonical, err := canonicalExistingDirectory(directory)
			if err != nil {
				return fmt.Errorf("runner profile %q dirs[%d]: %w", profile.Name, j, err)
			}
			if seenDirectories[canonical] {
				return fmt.Errorf("runner profile %q repeats served directory %s", profile.Name, canonical)
			}
			seenDirectories[canonical] = true
			profile.Directories[j] = canonical
		}
		sort.Strings(profile.Directories)
	}
	sort.Slice(c.Runners, func(i, j int) bool { return c.Runners[i].Name < c.Runners[j].Name })
	return nil
}

func canonicalExistingDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("directory is required")
	}
	expanded := ExpandHome(path)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", canonical)
	}
	return filepath.Clean(canonical), nil
}

func validateRunnerID(value string) error {
	if len(value) == 0 || len(value) > 253 {
		return errors.New("runner_id must be a hostname of 1 to 253 characters")
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("runner_id %q is not a valid hostname", value)
		}
		for _, r := range label {
			if r != '-' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return fmt.Errorf("runner_id %q is not a valid hostname", value)
			}
		}
	}
	return nil
}
