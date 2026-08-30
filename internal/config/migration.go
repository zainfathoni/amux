package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type MigrationStatus string

const (
	MigrationPlanned    MigrationStatus = "planned"
	MigrationSuccessful MigrationStatus = "successful"
	MigrationSkipped    MigrationStatus = "skipped"
)

type MigrationAction struct {
	Registry    string
	Source      string
	Destination string
	Status      MigrationStatus
	contents    []byte
}

type MigrationPlan struct {
	Directory Directory
	Actions   []MigrationAction
}

type MigrationResult struct {
	Registry    string
	Source      string
	Destination string
	Status      MigrationStatus
}

func PlanMigration(dir Directory) (MigrationPlan, error) {
	plan := MigrationPlan{Directory: dir}
	sources, err := migrationSources(dir)
	if err != nil {
		return plan, err
	}
	if sources.runners == "" {
		return plan, nil
	}

	var runners []byte
	if !fileExists(dir.RunnersPath()) {
		runners, err = migratedRunners(sources.runners)
		if err != nil {
			return plan, fmt.Errorf("prepare runners migration: %w", err)
		}
	}

	plan.Actions = []MigrationAction{migrationAction("runners", sources.runners, dir.RunnersPath(), runners)}
	return plan, nil
}

func (p MigrationPlan) Apply() ([]MigrationResult, error) {
	results := make([]MigrationResult, 0, len(p.Actions))
	for _, action := range p.Actions {
		result := MigrationResult{
			Registry:    action.Registry,
			Source:      action.Source,
			Destination: action.Destination,
			Status:      action.Status,
		}
		if action.Status == MigrationSkipped {
			results = append(results, result)
			continue
		}
		if err := writeMigrationFile(action.Destination, action.contents); err != nil {
			if errors.Is(err, os.ErrExist) {
				result.Status = MigrationSkipped
				results = append(results, result)
				continue
			}
			return results, fmt.Errorf("write migrated %s registry: %w", action.Registry, err)
		}
		result.Status = MigrationSuccessful
		results = append(results, result)
	}
	return results, nil
}

func MigrationRequired(dir Directory) (bool, error) {
	plan, err := PlanMigration(dir)
	if err != nil {
		return false, err
	}
	for _, action := range plan.Actions {
		if action.Status == MigrationPlanned {
			return true, nil
		}
	}
	return false, nil
}

type legacySources struct {
	runners string
}

func migrationSources(dir Directory) (legacySources, error) {
	var sources legacySources
	defaultDir, err := ResolveDirectory("")
	if err != nil {
		return sources, err
	}
	if filepath.Clean(defaultDir.Path) != filepath.Clean(dir.Path) {
		return sources, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return sources, err
	}
	legacyDir := filepath.Join(home, filepath.Dir(LegacyDefaultRelativePath))
	if path := filepath.Join(legacyDir, RunnersFile); fileExists(path) {
		sources.runners = path
	}
	return sources, nil
}

func migrationAction(registry, source, destination string, contents []byte) MigrationAction {
	status := MigrationPlanned
	if fileExists(destination) {
		status = MigrationSkipped
	}
	return MigrationAction{
		Registry:    registry,
		Source:      source,
		Destination: destination,
		Status:      status,
		contents:    contents,
	}
}

func migratedRunners(path string) ([]byte, error) {
	rows, err := parseOptionalRunners(path)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("# amux-schema: runners/v1\n")
	seenWorkdirs := make(map[string]string)
	for _, row := range rows {
		workdir, err := CanonicalWorkdir(row.Workdir)
		if err != nil {
			return nil, err
		}
		if previous, exists := seenWorkdirs[workdir]; exists {
			return nil, fmt.Errorf("runner workdir %s is already configured as %s", workdir, previous)
		}
		seenWorkdirs[workdir] = row.Workspace
		row.Workdir = workdir
		out.WriteString(row.String())
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func parseOptionalRunners(path string) ([]RunnerRow, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ParseRunners(file)
}

func writeMigrationFile(path string, contents []byte) error {
	return writeMigrationFileFrom(path, bytes.NewReader(contents))
}

func writeMigrationFileFrom(path string, contents io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := io.Copy(file, contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Link(tmp, path)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
