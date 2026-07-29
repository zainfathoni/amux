//go:build !linux && !darwin

package tmux

import "fmt"

func ProcessWorkdir(pid int) (string, error) {
	return "", fmt.Errorf("process cwd inspection is unsupported on this platform for pid %d", pid)
}
