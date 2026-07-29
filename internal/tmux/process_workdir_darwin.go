//go:build darwin

package tmux

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func ProcessWorkdir(pid int) (string, error) {
	cmd := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("inspect process %d cwd: %w: %s", pid, err, strings.TrimSpace(stderr.String()))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") && len(line) > 1 {
			return line[1:], nil
		}
	}
	return "", fmt.Errorf("inspect process %d cwd: lsof returned no cwd", pid)
}
