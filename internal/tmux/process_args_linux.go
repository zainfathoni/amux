//go:build linux

package tmux

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// ProcessArgs returns the kernel's exact argv vector for pid.
func ProcessArgs(pid int) ([]string, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("process PID is unavailable")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, fmt.Errorf("inspect process %d argv: %w", pid, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("process %d returned empty argv", pid)
	}
	fields := bytes.Split(data, []byte{0})
	if len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	args := make([]string, len(fields))
	for i, field := range fields {
		args[i] = string(field)
	}
	return args, nil
}

// ProcessIdentity returns Linux's per-incarnation process start ticks.
func ProcessIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("process PID is unavailable")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", fmt.Errorf("inspect process %d identity: %w", pid, err)
	}
	endName := strings.LastIndexByte(string(data), ')')
	if endName < 0 {
		return "", fmt.Errorf("process %d returned malformed stat identity", pid)
	}
	fields := strings.Fields(string(data[endName+1:]))
	const startTimeIndex = 19 // Field 22 after fields 1 (pid) and 2 (comm).
	if len(fields) <= startTimeIndex || fields[startTimeIndex] == "" {
		return "", fmt.Errorf("process %d returned incomplete stat identity", pid)
	}
	return fields[startTimeIndex], nil
}

// ProcessName returns Linux's native comm value without normalizing whitespace.
func ProcessName(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("process PID is unavailable")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return "", fmt.Errorf("inspect process %d name: %w", pid, err)
	}
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if len(data) == 0 {
		return "", fmt.Errorf("process %d returned empty name", pid)
	}
	return string(data), nil
}
