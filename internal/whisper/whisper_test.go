package whisper

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewDefaultState(t *testing.T) {
	svc := New(t.TempDir())
	st := svc.Status()
	if st.State != StateUnknown {
		t.Fatalf("expected state=unknown, got %s", st.State)
	}
}

func TestInitWithoutUV(t *testing.T) {
	svc := New(t.TempDir())
	// Clear PATH so uv can't be found.
	orig := os.Getenv("PATH")
	os.Setenv("PATH", "")
	os.Setenv("HOME", t.TempDir())
	defer os.Setenv("PATH", orig)

	svc.Init(context.Background())
	st := svc.Status()
	if st.State != StateUnavailable {
		t.Fatalf("expected state=unavailable without uv, got %s", st.State)
	}
}

func TestInitWithModelPresent(t *testing.T) {
	home := t.TempDir()
	cacheDir := filepath.Join(home, ".cache", "whisper")
	os.MkdirAll(cacheDir, 0o755)
	os.WriteFile(filepath.Join(cacheDir, "base.pt"), []byte("fake"), 0o644)
	os.Setenv("HOME", home)

	// Plant a fake uv binary.
	binDir := filepath.Join(home, "bin")
	os.MkdirAll(binDir, 0o755)
	fakeUV := filepath.Join(binDir, "uv")
	os.WriteFile(fakeUV, []byte("#!/bin/sh\nexit 0\n"), 0o755)

	orig := os.Getenv("PATH")
	os.Setenv("PATH", binDir)
	defer os.Setenv("PATH", orig)

	svc := New(t.TempDir())
	svc.Init(context.Background())
	st := svc.Status()
	if st.State != StateReady {
		t.Fatalf("expected state=ready with model present, got %s (%s)", st.State, st.Message)
	}
}

func TestInitWithoutModel(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)

	binDir := filepath.Join(home, "bin")
	os.MkdirAll(binDir, 0o755)
	fakeUV := filepath.Join(binDir, "uv")
	os.WriteFile(fakeUV, []byte("#!/bin/sh\nexit 0\n"), 0o755)

	orig := os.Getenv("PATH")
	os.Setenv("PATH", binDir)
	defer os.Setenv("PATH", orig)

	svc := New(t.TempDir())
	svc.Init(context.Background())
	st := svc.Status()
	if st.State != StateUnavailable {
		t.Fatalf("expected state=unavailable without model, got %s (%s)", st.State, st.Message)
	}
	if st.Message != "model not downloaded" {
		t.Fatalf("expected message about model, got %q", st.Message)
	}
}

func TestTranscribeRejectsWhenNotReady(t *testing.T) {
	svc := New(t.TempDir())
	_, err := svc.Transcribe(context.Background(), "/fake/audio.webm")
	if err == nil {
		t.Fatal("expected error when not ready")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	svc := New(t.TempDir())
	svc.Close()
	svc.Close()
}

func TestInstallWhenAlreadyReady(t *testing.T) {
	svc := New(t.TempDir())
	svc.mu.Lock()
	svc.setState(StateReady, "")
	svc.mu.Unlock()

	if err := svc.Install(); err != nil {
		t.Fatalf("Install on ready service should be no-op, got: %v", err)
	}
}

func TestModelCachePath(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	os.Unsetenv("XDG_CACHE_HOME")

	svc := New(t.TempDir())
	got := svc.modelCachePath()
	want := filepath.Join(home, ".cache", "whisper", "base.pt")
	if got != want {
		t.Fatalf("modelCachePath() = %q, want %q", got, want)
	}
}

func TestModelCachePathXDG(t *testing.T) {
	xdg := t.TempDir()
	os.Setenv("XDG_CACHE_HOME", xdg)
	defer os.Unsetenv("XDG_CACHE_HOME")

	svc := New(t.TempDir())
	got := svc.modelCachePath()
	want := filepath.Join(xdg, "whisper", "base.pt")
	if got != want {
		t.Fatalf("modelCachePath() = %q, want %q", got, want)
	}
}
