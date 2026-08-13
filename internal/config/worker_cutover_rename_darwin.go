//go:build darwin

package config

import "golang.org/x/sys/unix"

func workerCutoverRenameNoReplace(dirFD int, oldName, newName string) error {
	return unix.RenameatxNp(dirFD, oldName, dirFD, newName, unix.RENAME_EXCL)
}
