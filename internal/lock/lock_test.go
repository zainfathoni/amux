package lock

import (
	"context"
	"errors"
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
