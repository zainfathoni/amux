package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if sources.workers == "" && sources.runners == "" && sources.shelves == "" {
		return plan, nil
	}

	workers, err := migratedWorkers(sources.workers)
	if err != nil {
		return plan, fmt.Errorf("prepare workers migration: %w", err)
	}
	runners, err := migratedRunners(sources.runners)
	if err != nil {
		return plan, fmt.Errorf("prepare runners migration: %w", err)
	}
	shelves, err := migratedShelves(sources.shelves)
	if err != nil {
		return plan, fmt.Errorf("prepare shelves migration: %w", err)
	}

	plan.Actions = []MigrationAction{
		migrationAction("workers", sources.workers, dir.WorkersPath(), workers),
		migrationAction("runners", sources.runners, dir.RunnersPath(), runners),
		migrationAction("shelves", sources.shelves, dir.ShelvesPath(), shelves),
	}
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
	workers string
	runners string
	shelves string
}

func migrationSources(dir Directory) (legacySources, error) {
	var sources legacySources
	localWorkers := filepath.Join(dir.Path, "workspaces.tsv")
	if fileExists(localWorkers) {
		sources.workers = localWorkers
	}

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
	if sources.workers == "" {
		path := filepath.Join(legacyDir, "workspaces.tsv")
		if fileExists(path) {
			sources.workers = path
		}
	}
	if path := filepath.Join(legacyDir, RunnersFile); fileExists(path) {
		sources.runners = path
	}
	if path := filepath.Join(legacyDir, ShelvesFile); fileExists(path) {
		sources.shelves = path
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

func migratedWorkers(path string) ([]byte, error) {
	rows, err := parseOptionalWorkers(path)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("# amux-schema: workers/v1\n")
	seenThreads := make(map[string]string)
	seenWindows := make(map[string]bool)
	for _, row := range rows {
		thread, err := CanonicalThreadID(row.Thread)
		if err != nil {
			return nil, err
		}
		key := row.Workspace + "\x00" + row.Window
		if seenWindows[key] {
			return nil, fmt.Errorf("duplicate worker row %s/%s", row.Workspace, row.Window)
		}
		if previous, exists := seenThreads[thread]; exists {
			return nil, fmt.Errorf("worker thread %s is already configured as %s", thread, previous)
		}
		seenWindows[key] = true
		seenThreads[thread] = row.Workspace + "/" + row.Window
		row.Thread = thread
		out.WriteString(row.String())
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func migratedRunners(path string) ([]byte, error) {
	rows, err := parseOptionalRunners(path)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("# amux-schema: runners/v1\n")
	seenWorkdirs := make(map[string]string)
	seenWindows := make(map[string]bool)
	for _, row := range rows {
		workdir, err := CanonicalWorkdir(row.Workdir)
		if err != nil {
			return nil, err
		}
		key := row.Workspace + "\x00" + row.Window
		if seenWindows[key] {
			return nil, fmt.Errorf("duplicate runner row %s/%s", row.Workspace, row.Window)
		}
		if previous, exists := seenWorkdirs[workdir]; exists {
			return nil, fmt.Errorf("runner workdir %s is already configured as %s", workdir, previous)
		}
		seenWindows[key] = true
		seenWorkdirs[workdir] = row.Workspace + "/" + row.Window
		row.Workdir = workdir
		out.WriteString(row.String())
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func migratedShelves(path string) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("# amux-schema: shelves/v1\n")
	if path == "" {
		return out.Bytes(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for lineNo, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		thread, err := CanonicalThreadID(line)
		if err != nil {
			return nil, fmt.Errorf("invalid shelf on line %d: %w", lineNo+1, err)
		}
		if seen[thread] {
			return nil, fmt.Errorf("duplicate shelf intent for worker %s", thread)
		}
		seen[thread] = true
		out.WriteString(thread)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func parseOptionalWorkers(path string) ([]Row, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Parse(file)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
