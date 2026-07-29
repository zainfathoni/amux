//go:build linux

package tmux

import (
	"fmt"
	"os"
)

func ProcessWorkdir(pid int) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
}
