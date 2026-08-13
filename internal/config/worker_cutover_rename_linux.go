//go:build linux

package config

import "golang.org/x/sys/unix"

func workerCutoverRenameNoReplace(dirFD int, oldName, newName string) error {
	return unix.Renameat2(dirFD, oldName, dirFD, newName, unix.RENAME_NOREPLACE)
}
