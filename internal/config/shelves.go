package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadShelvesReadOnly(path string) ([]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var threads []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		thread, err := CanonicalThreadID(line)
		if err != nil {
			return nil, fmt.Errorf("invalid shelf on line %d: %w", lineNo, err)
		}
		if seen[thread] {
			return nil, fmt.Errorf("duplicate shelf intent for worker %s", thread)
		}
		seen[thread] = true
		threads = append(threads, thread)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return threads, nil
}

func StoreShelf(path, value string) (bool, error) {
	thread, err := CanonicalThreadID(value)
	if err != nil {
		return false, err
	}
	if err := ensureShelves(path); err != nil {
		return false, err
	}
	threads, err := LoadShelvesReadOnly(path)
	if err != nil {
		return false, err
	}
	for _, existing := range threads {
		if existing == thread {
			return false, nil
		}
	}
	lines, err := readLines(path)
	if err != nil {
		return false, err
	}
	lines = append(lines, thread)
	return true, writeLinesAtomic(path, lines)
}

func RemoveShelf(path, value string) (bool, error) {
	thread, err := CanonicalThreadID(value)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
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
		canonical, err := CanonicalThreadID(line)
		if err != nil {
			return false, err
		}
		if canonical == thread {
			removed = true
			continue
		}
		kept = append(kept, canonical)
	}
	if !removed {
		return false, nil
	}
	return true, writeLinesAtomic(path, kept)
}

func ensureShelves(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte("# amux-schema: shelves/v1\n"), 0o600)
}
