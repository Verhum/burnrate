package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()

	lock, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Verify pid was written
	data, err := os.ReadFile(dir + "/daemon.lock")
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if !strings.Contains(string(data), "pid") == false {
		// just check it's non-empty and a number
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		t.Fatal("lock file should contain pid")
	}

	// Second acquire must fail
	_, err = Acquire(dir)
	if err == nil {
		t.Fatal("second acquire should fail while lock is held")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected 'already running' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "pid") {
		t.Fatalf("expected holder pid in error, got: %v", err)
	}

	// Release and re-acquire should work
	lock.Release()

	lock2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
	lock2.Release()
}

func TestAcquireTwoDirectories(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	lock1, err := Acquire(dir1)
	if err != nil {
		t.Fatalf("acquire dir1 failed: %v", err)
	}
	defer lock1.Release()

	lock2, err := Acquire(dir2)
	if err != nil {
		t.Fatalf("acquire dir2 should succeed (different data dir): %v", err)
	}
	defer lock2.Release()
}
