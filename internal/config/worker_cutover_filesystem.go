package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var workerCutoverFilesystemHook func(string)

var workerCutoverDirectorySync = func(path string, directory *os.File) error {
	return directory.Sync()
}

type workerCutoverDirectory struct {
	path          string
	requestedPath string
	file          *os.File
	info          os.FileInfo
	present       bool
}

func openWorkerCutoverDirectory(path string, create bool) (*workerCutoverDirectory, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve worker cutover config directory: %w", err)
	}
	abs = filepath.Clean(abs)
	canonical, err := canonicalWorkerCutoverDirectoryPath(abs)
	if err != nil {
		return nil, err
	}
	rootFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root for worker cutover: %w", err)
	}
	current := os.NewFile(uintptr(rootFD), string(filepath.Separator))
	if current == nil {
		_ = unix.Close(rootFD)
		return nil, errors.New("open filesystem root for worker cutover")
	}
	currentPath := string(filepath.Separator)
	relative := strings.TrimPrefix(canonical, string(filepath.Separator))
	components := make([]string, 0)
	if relative != "" && relative != "." {
		components = strings.Split(relative, string(filepath.Separator))
	}
	for _, component := range components {
		nextPath := filepath.Join(currentPath, component)
		fd, openErr := unix.Openat(int(current.Fd()), component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			mkdirErr := unix.Mkdirat(int(current.Fd()), component, 0o700)
			if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = current.Close()
				return nil, fmt.Errorf("create worker cutover config directory %s: %w", nextPath, mkdirErr)
			}
			if mkdirErr == nil || errors.Is(mkdirErr, unix.EEXIST) {
				if mkdirErr == nil && workerCutoverFilesystemHook != nil {
					workerCutoverFilesystemHook("directory-created:" + nextPath)
				}
				if syncErr := workerCutoverDirectorySync(currentPath, current); syncErr != nil {
					_ = current.Close()
					return nil, fmt.Errorf("durably create worker cutover config directory %s: sync parent %s: %w", nextPath, currentPath, syncErr)
				}
			}
			fd, openErr = unix.Openat(int(current.Fd()), component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if errors.Is(openErr, unix.ENOENT) && !create {
			_ = current.Close()
			return &workerCutoverDirectory{path: canonical, requestedPath: abs}, nil
		}
		if openErr != nil {
			_ = current.Close()
			return nil, fmt.Errorf("open pinned worker cutover config directory %s without following links: %w", nextPath, openErr)
		}
		next := os.NewFile(uintptr(fd), nextPath)
		if next == nil {
			_ = unix.Close(fd)
			_ = current.Close()
			return nil, fmt.Errorf("open pinned worker cutover config directory %s", nextPath)
		}
		_ = current.Close()
		current = next
		currentPath = nextPath
	}
	info, err := current.Stat()
	if err != nil {
		_ = current.Close()
		return nil, fmt.Errorf("fstat pinned worker cutover config directory %s: %w", canonical, err)
	}
	if !info.IsDir() {
		_ = current.Close()
		return nil, fmt.Errorf("worker cutover config path %s must be a directory", canonical)
	}
	return &workerCutoverDirectory{path: canonical, requestedPath: abs, file: current, info: info, present: true}, nil
}

func canonicalWorkerCutoverDirectoryPath(path string) (string, error) {
	existing := path
	missing := make([]string, 0)
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			components := append([]string{resolved}, missing...)
			return filepath.Clean(filepath.Join(components...)), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve worker cutover config directory %s without an unstable symlink traversal: %w", path, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("resolve existing ancestor of worker cutover config directory %s: %w", path, err)
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
}

func (d *workerCutoverDirectory) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	return d.file.Close()
}

func (d *workerCutoverDirectory) verifyPathIdentity() error {
	if d == nil {
		return errors.New("worker cutover config directory is unavailable")
	}
	reopened, err := openWorkerCutoverDirectory(d.requestedPath, false)
	if err != nil {
		return err
	}
	defer reopened.Close()
	if reopened.present != d.present || d.present && !os.SameFile(d.info, reopened.info) || !d.present && reopened.path != d.path {
		return errors.New("worker cutover config directory path changed after it was pinned")
	}
	return nil
}

func (d *workerCutoverDirectory) fd() int {
	return int(d.file.Fd())
}

type workerCutoverFileSnapshot struct {
	name    string
	data    []byte
	info    os.FileInfo
	mode    os.FileMode
	present bool
}

func (d *workerCutoverDirectory) readRegularFile(name, description string, allowMissing bool, requiredMode *os.FileMode) (workerCutoverFileSnapshot, error) {
	if d == nil || !d.present {
		if allowMissing {
			return workerCutoverFileSnapshot{name: name}, nil
		}
		return workerCutoverFileSnapshot{}, &os.PathError{Op: "open", Path: filepath.Join(d.path, name), Err: os.ErrNotExist}
	}
	fd, err := unix.Openat(d.fd(), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) && allowMissing {
		return workerCutoverFileSnapshot{name: name}, nil
	}
	if err != nil {
		return workerCutoverFileSnapshot{}, fmt.Errorf("open pinned %s %s without following links: %w", description, filepath.Join(d.path, name), err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(d.path, name))
	if file == nil {
		_ = unix.Close(fd)
		return workerCutoverFileSnapshot{}, fmt.Errorf("open pinned %s %s", description, filepath.Join(d.path, name))
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return workerCutoverFileSnapshot{}, fmt.Errorf("fstat pinned %s %s: %w", description, file.Name(), err)
	}
	if !before.Mode().IsRegular() {
		return workerCutoverFileSnapshot{}, fmt.Errorf("%s %s must be a regular file", description, file.Name())
	}
	if requiredMode != nil && before.Mode().Perm() != requiredMode.Perm() {
		return workerCutoverFileSnapshot{}, fmt.Errorf("%s %s must have mode %04o, found %04o", description, file.Name(), requiredMode.Perm(), before.Mode().Perm())
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return workerCutoverFileSnapshot{}, fmt.Errorf("read pinned %s %s: %w", description, file.Name(), err)
	}
	after, err := file.Stat()
	if err != nil {
		return workerCutoverFileSnapshot{}, fmt.Errorf("fstat pinned %s %s after read: %w", description, file.Name(), err)
	}
	if !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return workerCutoverFileSnapshot{}, fmt.Errorf("%s %s changed while its pinned descriptor was read", description, file.Name())
	}
	return workerCutoverFileSnapshot{name: name, data: data, info: after, mode: after.Mode().Perm(), present: true}, nil
}

func (d *workerCutoverDirectory) verifyFileSnapshot(expected workerCutoverFileSnapshot, description string, requiredMode *os.FileMode) error {
	actual, err := d.readRegularFile(expected.name, description, true, requiredMode)
	if err != nil {
		return err
	}
	if actual.present != expected.present {
		return fmt.Errorf("%s pathname presence changed after its descriptor was read", description)
	}
	if !expected.present {
		return nil
	}
	if !os.SameFile(expected.info, actual.info) {
		return fmt.Errorf("%s pathname identity changed after its descriptor was read", description)
	}
	if actual.mode.Perm() != expected.mode.Perm() || !bytes.Equal(actual.data, expected.data) {
		return fmt.Errorf("%s content or mode changed after its descriptor was read", description)
	}
	return nil
}

func (d *workerCutoverDirectory) sync() error {
	return workerCutoverDirectorySync(d.path, d.file)
}

func (d *workerCutoverDirectory) createTemp(prefix string, data []byte, mode os.FileMode) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("generate worker cutover temporary name: %w", err)
		}
		name := prefix + hex.EncodeToString(random[:])
		fd, err := unix.Openat(d.fd(), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create pinned worker cutover temporary file %s: %w", filepath.Join(d.path, name), err)
		}
		file := os.NewFile(uintptr(fd), filepath.Join(d.path, name))
		if file == nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(d.fd(), name, 0)
			return "", fmt.Errorf("create pinned worker cutover temporary file %s", filepath.Join(d.path, name))
		}
		failed := func(err error) (string, error) {
			_ = file.Close()
			_ = unix.Unlinkat(d.fd(), name, 0)
			return "", err
		}
		if err := file.Chmod(mode.Perm()); err != nil {
			return failed(err)
		}
		if _, err := file.Write(data); err != nil {
			return failed(err)
		}
		if err := file.Sync(); err != nil {
			return failed(err)
		}
		if err := file.Close(); err != nil {
			_ = unix.Unlinkat(d.fd(), name, 0)
			return "", err
		}
		return name, nil
	}
	return "", errors.New("allocate unique worker cutover temporary file")
}

func (d *workerCutoverDirectory) unlink(name string) error {
	if err := unix.Unlinkat(d.fd(), name, 0); err != nil {
		return fmt.Errorf("remove pinned worker cutover file %s: %w", filepath.Join(d.path, name), err)
	}
	return nil
}

func (d *workerCutoverDirectory) renameNoReplace(oldName, newName string) error {
	if err := workerCutoverRenameNoReplace(d.fd(), oldName, newName); err != nil {
		return fmt.Errorf("rename pinned worker cutover file %s to %s without replacement: %w", filepath.Join(d.path, oldName), filepath.Join(d.path, newName), err)
	}
	return nil
}
