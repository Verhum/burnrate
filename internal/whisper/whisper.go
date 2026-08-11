package whisper

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const defaultModel = "base"

type State string

const (
	StateUnknown     State = "unknown"
	StateChecking    State = "checking"
	StateReady       State = "ready"
	StateUnavailable State = "unavailable"
	StateInstalling  State = "installing"
	StateError       State = "error"
)

type Status struct {
	State   State  `json:"state"`
	Message string `json:"message,omitempty"`
}

type Service struct {
	mu      sync.Mutex
	model   string
	dataDir string
	uvPath  string
	state   State
	message string

	installCancel context.CancelFunc
	installDone   chan struct{}
}

func New(dataDir string) *Service {
	return &Service{
		model:   defaultModel,
		dataDir: dataDir,
		state:   StateUnknown,
	}
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{State: s.state, Message: s.message}
}

func (s *Service) setState(st State, msg string) {
	s.state = st
	s.message = msg
}

func (s *Service) resolveUV() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uvPath != "" {
		return s.uvPath, nil
	}
	for _, p := range []string{
		os.Getenv("HOME") + "/.local/bin/uv",
		"/opt/homebrew/bin/uv",
		"/usr/local/bin/uv",
	} {
		if _, err := os.Stat(p); err == nil {
			s.uvPath = p
			return p, nil
		}
	}
	if p, err := exec.LookPath("uv"); err == nil {
		s.uvPath = p
		return p, nil
	}
	return "", fmt.Errorf("uv not found; install it: https://docs.astral.sh/uv/")
}

func (s *Service) modelCachePath() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(cacheDir, "whisper", s.model+".pt")
}

func (s *Service) Init(ctx context.Context) {
	s.mu.Lock()
	s.setState(StateChecking, "")
	s.mu.Unlock()

	uv, err := s.resolveUV()
	if err != nil {
		s.mu.Lock()
		s.setState(StateUnavailable, err.Error())
		s.mu.Unlock()
		return
	}

	// Check if model weights already exist on disk.
	if _, err := os.Stat(s.modelCachePath()); err == nil {
		s.mu.Lock()
		s.setState(StateReady, fmt.Sprintf("uv=%s model=%s", uv, s.model))
		s.mu.Unlock()
		return
	}

	// uv is available but model not yet downloaded.
	s.mu.Lock()
	s.setState(StateUnavailable, "model not downloaded")
	s.mu.Unlock()
}

// Install downloads the model weights. It is safe to call concurrently; only
// one download runs at a time. The context passed to Init/Transcribe is not
// used here — Install creates its own cancellable context so Close can stop it.
func (s *Service) Install() error {
	s.mu.Lock()
	if s.state == StateInstalling {
		// Already in progress — wait for it.
		ch := s.installDone
		s.mu.Unlock()
		if ch != nil {
			<-ch
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.state == StateReady {
			return nil
		}
		return fmt.Errorf("install failed: %s", s.message)
	}
	if s.state == StateReady {
		s.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.installCancel = cancel
	s.installDone = done
	s.setState(StateInstalling, "downloading model weights")
	s.mu.Unlock()

	err := s.downloadModel(ctx)

	s.mu.Lock()
	s.installCancel = nil
	s.installDone = nil
	if err != nil {
		s.setState(StateError, err.Error())
	} else {
		s.setState(StateReady, fmt.Sprintf("model=%s", s.model))
	}
	s.mu.Unlock()

	close(done)
	cancel()
	return err
}

func (s *Service) downloadModel(ctx context.Context) error {
	uv, err := s.resolveUV()
	if err != nil {
		return err
	}

	script := fmt.Sprintf("import whisper; whisper.load_model('%s')", s.model)
	args := []string{
		"run", "--with", "openai-whisper", "--with", "setuptools",
		"python", "-c", script,
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, uv, args...)
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin")

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("install cancelled")
		}
		return fmt.Errorf("model download failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	cancel := s.installCancel
	done := s.installDone
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Service) Transcribe(ctx context.Context, audioPath string) (string, error) {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()

	if st != StateReady {
		return "", fmt.Errorf("whisper not ready (state=%s); install the model first", st)
	}

	uv, err := s.resolveUV()
	if err != nil {
		return "", err
	}

	outDir, err := os.MkdirTemp("", "whisper-out-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	args := []string{
		"run", "--with", "openai-whisper", "--with", "setuptools",
		"whisper",
		audioPath,
		"--model", s.model,
		"--output_format", "txt",
		"--output_dir", outDir,
		"--fp16", "False",
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, uv, args...)
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin")

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("whisper failed: %w\nstderr: %s", err, stderr.String())
	}

	matches, err := filepath.Glob(filepath.Join(outDir, "*.txt"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no transcription output found in %s", outDir)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return "", fmt.Errorf("reading whisper output: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}
