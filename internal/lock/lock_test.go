package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireSerializesMutationAndReportsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	first, err := Acquire(context.Background(), path, Owner{
		PID:        123,
		Command:    "amux worker pin --thread T-one",
		Hostname:   "test-host",
		AcquiredAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Release() })

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = Acquire(ctx, path, Owner{Command: "amux runner pin --workdir /tmp/two"})
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("contending Acquire error = %v, want BusyError", err)
	}
	if busy.Owner.PID != 123 || busy.Owner.Command != "amux worker pin --thread T-one" || busy.Owner.Hostname != "test-host" {
		t.Fatalf("busy owner = %+v", busy.Owner)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(context.Background(), path, Owner{Command: "amux runner pin --workdir /tmp/two"})
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireModeAllowsSharedReadersAndRejectsWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.lock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := AcquireMode(context.Background(), path, Owner{}, Shared, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := AcquireMode(context.Background(), path, Owner{}, Shared, false)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := AcquireMode(ctx, path, Owner{}, Exclusive, false); err == nil {
		t.Fatal("exclusive lock acquired while shared readers held it")
	}
}

func TestAcquireModeReadOnlyDoesNotCreateOrFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.lock")
	if _, err := AcquireMode(context.Background(), missing, Owner{}, Shared, false); err == nil {
		t.Fatal("read-only lock unexpectedly created a missing file")
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only lock mutated path: %v", err)
	}
	target := filepath.Join(dir, "target.lock")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireMode(context.Background(), link, Owner{}, Shared, false); err == nil {
		t.Fatal("read-only lock followed a symlink")
	}
}
