//go:build linux

package tmux

import (
	"bytes"
	"fmt"
	"os"
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
