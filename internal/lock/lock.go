package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const FileName = "operation.lock"

type Owner struct {
	PID        int       `json:"pid"`
	Command    string    `json:"command"`
	Hostname   string    `json:"hostname"`
	AcquiredAt time.Time `json:"acquired_at"`
}

type BusyError struct {
	Path  string `json:"path"`
	Owner Owner  `json:"owner"`
}

func (e *BusyError) Error() string {
	if e.Owner.PID == 0 && e.Owner.Command == "" {
		return fmt.Sprintf("amux mutation lock %s is held by another process", e.Path)
	}
	return fmt.Sprintf("amux mutation lock %s is held by pid %d on %s: %s", e.Path, e.Owner.PID, e.Owner.Hostname, e.Owner.Command)
}

type Lock struct {
	file     *os.File
	mu       sync.Mutex
	released bool
}

type Mode int

const (
	Exclusive Mode = iota
	Shared
)

func MachinePath() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve machine lock directory: %w", err)
		}
	}
	return filepath.Join(base, "amux", FileName), nil
}

func Acquire(ctx context.Context, path string, owner Owner) (*Lock, error) {
	return AcquireMode(ctx, path, owner, Exclusive, true)
}

// AcquireMode obtains an advisory lock. create=false is used by read-only
// inspection so observing state never creates a lock artifact.
func AcquireMode(ctx context.Context, path string, owner Owner, mode Mode, create bool) (*Lock, error) {
	if mode != Exclusive && mode != Shared {
		return nil, errors.New("invalid lock mode")
	}
	if !create {
		file, err := openNoFollow(path, os.O_RDONLY, 0)
		if err != nil {
			return nil, fmt.Errorf("open mutation lock: %w", err)
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			_ = file.Close()
			return nil, errors.New("open mutation lock: lock must be a private regular file")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			_ = file.Close()
			return nil, errors.New("open mutation lock: lock must have exactly one link")
		}
		held, err := acquireFile(ctx, file, path, owner, mode, false)
		if err != nil {
			return nil, err
		}
		openInfo, openErr := file.Stat()
		pathInfo, pathErr := os.Lstat(path)
		if openErr != nil || pathErr != nil || !os.SameFile(openInfo, pathInfo) {
			_ = held.Release()
			return nil, errors.New("open mutation lock: lock path identity changed")
		}
		return held, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create mutation lock directory: %w", err)
	}
	file, err := openNoFollow(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open mutation lock: %w", err)
	}
	return acquireFile(ctx, file, path, owner, mode, mode == Exclusive)
}

func openNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func acquireFile(ctx context.Context, file *os.File, path string, owner Owner, mode Mode, recordOwner bool) (*Lock, error) {
	operation := syscall.LOCK_EX
	if mode == Shared {
		operation = syscall.LOCK_SH
	}
	for {
		err := syscall.Flock(int(file.Fd()), operation|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("acquire mutation lock: %w", err)
		}
		select {
		case <-ctx.Done():
			holder := readOwner(file)
			_ = file.Close()
			return nil, &BusyError{Path: path, Owner: holder}
		case <-time.After(10 * time.Millisecond):
		}
	}

	if recordOwner {
		owner = completeOwner(owner)
		if err := writeOwner(file, owner); err != nil {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			_ = file.Close()
			return nil, fmt.Errorf("record mutation lock owner: %w", err)
		}
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil
	}
	l.released = true
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func completeOwner(owner Owner) Owner {
	if owner.PID == 0 {
		owner.PID = os.Getpid()
	}
	if owner.Command == "" {
		owner.Command = strings.Join(os.Args, " ")
	}
	if owner.Hostname == "" {
		owner.Hostname, _ = os.Hostname()
	}
	if owner.AcquiredAt.IsZero() {
		owner.AcquiredAt = time.Now().UTC()
	}
	return owner
}

func writeOwner(file *os.File, owner Owner) error {
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func readOwner(file *os.File) Owner {
	var owner Owner
	data, err := io.ReadAll(io.NewSectionReader(file, 0, 64<<10))
	if err != nil {
		return owner
	}
	_ = json.Unmarshal(data, &owner)
	return owner
}
