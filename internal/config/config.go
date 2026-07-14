package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultRelativePath       = DefaultDirectoryRelativePath + "/workers.tsv"
	LegacyDefaultRelativePath = ".config/amp-tmux/workspaces.tsv"
	WorkspacesEnv             = "AMUX_WORKSPACES"
	LegacyWorkspacesEnv       = "AMP_TMUX_WORKSPACES"
)

type Row struct {
	Workspace string
	Window    string
	Workdir   string
	Thread    string
}

type RunnerRow struct {
	Workspace string
	Window    string
	Workdir   string
}

func DefaultPath() string {
	dir, err := ResolveDirectory("")
	if err != nil {
		return DefaultRelativePath
	}
	return dir.WorkersPath()
}

func DefaultPathReadOnly() string {
	return DefaultPath()
}

func RunnerPath(workspacesPath string) string {
	return filepath.Join(filepath.Dir(workspacesPath), "runners.tsv")
}

func MigrateDefaultDir() (bool, error) {
	dir, err := ResolveDirectory("")
	if err != nil {
		return false, err
	}
	plan, err := PlanMigration(dir)
	if err != nil {
		return false, err
	}
	results, err := plan.Apply()
	if err != nil {
		return false, err
	}
	for _, result := range results {
		if result.Status == MigrationSuccessful {
			return true, nil
		}
	}
	return false, nil
}

func Ensure(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfig), 0o644)
}

func Load(path string) ([]Row, error) {
	if err := Ensure(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Parse(file)
}

func LoadReadOnly(path string) ([]Row, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Parse(file)
}

func EnsureRunners(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(defaultRunnerConfig), 0o644)
}

func LoadRunners(path string) ([]RunnerRow, error) {
	if err := EnsureRunners(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ParseRunners(file)
}

func LoadRunnersReadOnly(path string) ([]RunnerRow, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ParseRunners(file)
}

func Parse(r io.Reader) ([]Row, error) {
	scanner := bufio.NewScanner(r)
	var rows []Row
	seenThreads := make(map[string]string)
	seenWindows := make(map[string]bool)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid row on line %d: expected 4 tab-separated fields", lineNo)
		}
		thread, err := CanonicalThreadID(fields[3])
		if err != nil {
			return nil, fmt.Errorf("invalid row on line %d: %w", lineNo, err)
		}
		row := Row{Workspace: fields[0], Window: fields[1], Workdir: fields[2], Thread: thread}
		if err := row.Validate(); err != nil {
			return nil, fmt.Errorf("invalid row on line %d: %w", lineNo, err)
		}
		windowKey := row.Workspace + "\x00" + row.Window
		if seenWindows[windowKey] {
			return nil, fmt.Errorf("duplicate worker row %s/%s", row.Workspace, row.Window)
		}
		if previous, exists := seenThreads[row.Thread]; exists {
			return nil, fmt.Errorf("worker thread %s is already configured as %s", row.Thread, previous)
		}
		seenWindows[windowKey] = true
		seenThreads[row.Thread] = row.Workspace + "/" + row.Window
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func ParseRunners(r io.Reader) ([]RunnerRow, error) {
	scanner := bufio.NewScanner(r)
	var rows []RunnerRow
	seenWorkdirs := make(map[string]string)
	seenWindows := make(map[string]bool)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid runner row on line %d: expected 3 tab-separated fields", lineNo)
		}
		row := RunnerRow{Workspace: fields[0], Window: fields[1], Workdir: fields[2]}
		if err := row.Validate(); err != nil {
			return nil, fmt.Errorf("invalid runner row on line %d: %w", lineNo, err)
		}
		windowKey := row.Workspace + "\x00" + row.Window
		if seenWindows[windowKey] {
			return nil, fmt.Errorf("duplicate runner row %s/%s", row.Workspace, row.Window)
		}
		workdir, err := CanonicalWorkdir(row.Workdir)
		if err != nil {
			return nil, fmt.Errorf("invalid runner row on line %d: %w", lineNo, err)
		}
		if previous, exists := seenWorkdirs[workdir]; exists {
			return nil, fmt.Errorf("runner workdir %s is already configured as %s", workdir, previous)
		}
		seenWindows[windowKey] = true
		seenWorkdirs[workdir] = row.Workspace + "/" + row.Window
		row.Workdir = workdir
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func Store(path string, row Row) (bool, error) {
	thread, err := CanonicalThreadID(row.Thread)
	if err != nil {
		return false, err
	}
	row.Thread = thread
	if err := row.Validate(); err != nil {
		return false, err
	}
	if err := Ensure(path); err != nil {
		return false, err
	}
	lines, err := readLines(path)
	if err != nil {
		return false, err
	}
	replaced := false
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 && fields[0] == row.Workspace && fields[1] == row.Window {
			lines[i] = row.String()
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, row.String())
	}
	if _, err := Parse(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil {
		return false, err
	}
	return replaced, writeLinesAtomic(path, lines)
}

func StoreRunner(path string, row RunnerRow) (bool, error) {
	workdir, err := CanonicalWorkdir(row.Workdir)
	if err != nil {
		return false, err
	}
	row.Workdir = workdir
	if err := row.Validate(); err != nil {
		return false, err
	}
	if err := EnsureRunners(path); err != nil {
		return false, err
	}
	lines, err := readLines(path)
	if err != nil {
		return false, err
	}
	replaced := false
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 && fields[0] == row.Workspace && fields[1] == row.Window {
			lines[i] = row.String()
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, row.String())
	}
	if _, err := ParseRunners(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil {
		return false, err
	}
	return replaced, writeLinesAtomic(path, lines)
}

func Remove(path, workspace, window string) (bool, error) {
	if err := validateField("workspace", workspace); err != nil {
		return false, err
	}
	if err := validateField("window", window); err != nil {
		return false, err
	}
	if err := Ensure(path); err != nil {
		return false, err
	}
	lines, err := readLines(path)
	if err != nil {
		return false, err
	}
	kept := lines[:0]
	removed := false
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			kept = append(kept, line)
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 && fields[0] == workspace && fields[1] == window {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	return removed, writeLinesAtomic(path, kept)
}

func RemoveRunner(path, workspace, window string) (bool, error) {
	if err := validateField("workspace", workspace); err != nil {
		return false, err
	}
	if err := validateField("window", window); err != nil {
		return false, err
	}
	if err := EnsureRunners(path); err != nil {
		return false, err
	}
	lines, err := readLines(path)
	if err != nil {
		return false, err
	}
	kept := lines[:0]
	removed := false
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			kept = append(kept, line)
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 && fields[0] == workspace && fields[1] == window {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	return removed, writeLinesAtomic(path, kept)
}

func RemoveRows(path string, shouldRemove func(Row) bool) (int, error) {
	if err := Ensure(path); err != nil {
		return 0, err
	}
	lines, err := readLines(path)
	if err != nil {
		return 0, err
	}
	kept := lines[:0]
	removed := 0
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			kept = append(kept, line)
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return 0, fmt.Errorf("invalid row: expected 4 tab-separated fields")
		}
		row := Row{Workspace: fields[0], Window: fields[1], Workdir: fields[2], Thread: fields[3]}
		if err := row.Validate(); err != nil {
			return 0, err
		}
		thread, err := CanonicalThreadID(row.Thread)
		if err != nil {
			return 0, err
		}
		row.Thread = thread
		if shouldRemove(row) {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	return removed, writeLinesAtomic(path, kept)
}

func (r Row) String() string {
	return strings.Join([]string{r.Workspace, r.Window, r.Workdir, r.Thread}, "\t")
}

func (r RunnerRow) String() string {
	return strings.Join([]string{r.Workspace, r.Window, r.Workdir}, "\t")
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeLinesAtomic(path string, lines []string) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmp := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := file.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (r Row) Validate() error {
	if err := validateField("workspace", r.Workspace); err != nil {
		return err
	}
	if err := validateField("window", r.Window); err != nil {
		return err
	}
	if err := validateField("workdir", r.Workdir); err != nil {
		return err
	}
	if err := validateField("thread", r.Thread); err != nil {
		return err
	}
	_, err := CanonicalThreadID(r.Thread)
	return err
}

func (r RunnerRow) Validate() error {
	if err := validateField("workspace", r.Workspace); err != nil {
		return err
	}
	if err := validateField("window", r.Window); err != nil {
		return err
	}
	return validateField("workdir", r.Workdir)
}

func ValidateField(name, value string) error {
	return validateField(name, value)
}

func validateField(name, value string) error {
	if value == "" {
		return fmt.Errorf("missing %s", name)
	}
	if strings.ContainsAny(value, "\t\n\r") {
		return fmt.Errorf("%s must not contain tabs or newlines", name)
	}
	return nil
}

func ExpandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

const defaultConfig = `# amux-schema: workers/v1
# workspace	window	workdir	thread-id
#
# Worker identity is the canonical Amp thread ID.
`

const defaultRunnerConfig = `# amux-schema: runners/v1
# workspace	window	workdir
#
# Runner identity is the canonical workdir. The window column is retained until
# the runner lifecycle migration removes it.
`
