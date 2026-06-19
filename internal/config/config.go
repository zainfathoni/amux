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

const DefaultRelativePath = ".config/amp-tmux/workspaces.tsv"

type Row struct {
	Workspace string
	Window    string
	Workdir   string
	Thread    string
}

func DefaultPath() string {
	if path := os.Getenv("AMP_TMUX_WORKSPACES"); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return DefaultRelativePath
	}
	return filepath.Join(home, DefaultRelativePath)
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

func Parse(r io.Reader) ([]Row, error) {
	scanner := bufio.NewScanner(r)
	var rows []Row
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
		row := Row{Workspace: fields[0], Window: fields[1], Workdir: fields[2], Thread: fields[3]}
		if err := row.Validate(); err != nil {
			return nil, fmt.Errorf("invalid row on line %d: %w", lineNo, err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func Store(path string, row Row) (bool, error) {
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

func (r Row) String() string {
	return strings.Join([]string{r.Workspace, r.Window, r.Workdir, r.Thread}, "\t")
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
	return validateField("thread", r.Thread)
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

const defaultConfig = `# workspace	window	workdir	thread-id-or-url
#
# Usage:
#   amux
#   amux launch mac
#
# Add/remove rows with amux store/store-current/remove/remove-current/spawn.
# Use either Amp thread IDs or full https://ampcode.com/threads/... URLs.
`
