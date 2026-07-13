package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	ConfigDirEnv   = "AMUX_CONFIG_DIR"
	WorkersFile    = "workers.tsv"
	RunnersFile    = "runners.tsv"
	ShelvesFile    = "shelves.tsv"
	OperationsFile = "operations.json"
)

// Directory is the complete on-disk configuration selected for one invocation.
type Directory struct {
	Path string
}

func ResolveDirectory(explicit string) (Directory, error) {
	path := explicit
	if path == "" {
		path = os.Getenv(ConfigDirEnv)
	}
	if path == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Directory{}, fmt.Errorf("resolve user config directory: %w", err)
		}
		path = filepath.Join(base, "amux")
	}
	abs, err := filepath.Abs(ExpandHome(path))
	if err != nil {
		return Directory{}, fmt.Errorf("resolve config directory %q: %w", path, err)
	}
	return Directory{Path: filepath.Clean(abs)}, nil
}

func (d Directory) WorkersPath() string {
	return filepath.Join(d.Path, WorkersFile)
}

func (d Directory) RunnersPath() string {
	return filepath.Join(d.Path, RunnersFile)
}

func (d Directory) ShelvesPath() string {
	return filepath.Join(d.Path, ShelvesFile)
}

func (d Directory) OperationsPath() string {
	return filepath.Join(d.Path, OperationsFile)
}
