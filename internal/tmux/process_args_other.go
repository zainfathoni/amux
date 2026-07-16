//go:build !darwin && !linux

package tmux

import "fmt"

func ProcessArgs(pid int) ([]string, error) {
	return nil, fmt.Errorf("exact process argv inspection is unsupported on this platform")
}

func ProcessIdentity(pid int) (string, error) {
	return "", fmt.Errorf("exact process identity inspection is unsupported on this platform")
}
