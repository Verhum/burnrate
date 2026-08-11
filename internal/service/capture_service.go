package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Verhum/burnrate/internal/domain"
)

type CaptureService struct {
	tasks       domain.TaskRepository
	captures    domain.CaptureRepository
	requests    domain.HumanRequestRepository
	attachments domain.AttachmentRepository
	settings    domain.SettingsRepository
	dataDir     string
}

func NewCaptureService(
	tasks domain.TaskRepository,
	captures domain.CaptureRepository,
	requests domain.HumanRequestRepository,
	attachments domain.AttachmentRepository,
	settings domain.SettingsRepository,
	dataDir string,
) *CaptureService {
	return &CaptureService{
		tasks:       tasks,
		captures:    captures,
		requests:    requests,
		attachments: attachments,
		settings:    settings,
		dataDir:     dataDir,
	}
}

func (s *CaptureService) Create(ctx context.Context, taskID, requestID int64, initiator, targetDesc, mode string) (*domain.Capture, error) {
	if initiator != "human" && initiator != "agent" {
		return nil, &ValidationError{Field: "initiator", Message: "initiator must be human or agent"}
	}
	if mode != "screenshot" && mode != "video" {
		return nil, &ValidationError{Field: "mode", Message: "mode must be screenshot or video"}
	}
	if _, err := s.tasks.GetTask(taskID); err != nil {
		return nil, &NotFoundError{Entity: "task", ID: taskID}
	}

	dir := s.captureDir(taskID)
	os.MkdirAll(dir, 0o755)

	return s.captures.CreateCapture(taskID, requestID, initiator, targetDesc, mode)
}

func (s *CaptureService) Get(ctx context.Context, id int64) (*domain.Capture, error) {
	cap, err := s.captures.GetCapture(id)
	if err != nil {
		return nil, &NotFoundError{Entity: "capture", ID: id}
	}
	cap.Keyframes = s.findKeyframes(cap.TaskID, cap.ID)
	return cap, nil
}

func (s *CaptureService) List(ctx context.Context, taskID int64) ([]domain.Capture, error) {
	caps, err := s.captures.ListCaptures(taskID)
	if err != nil {
		return nil, err
	}
	for i := range caps {
		caps[i].Keyframes = s.findKeyframes(caps[i].TaskID, caps[i].ID)
	}
	return caps, nil
}

func (s *CaptureService) Finish(ctx context.Context, id int64, videoPath, transcript string, durationSec float64) error {
	if _, err := s.captures.GetCapture(id); err != nil {
		return &NotFoundError{Entity: "capture", ID: id}
	}
	return s.captures.FinishCapture(id, videoPath, transcript, durationSec)
}

func (s *CaptureService) SetNotes(ctx context.Context, id int64, notes string) error {
	if _, err := s.captures.GetCapture(id); err != nil {
		return &NotFoundError{Entity: "capture", ID: id}
	}
	return s.captures.SetCaptureNotes(id, notes)
}

func (s *CaptureService) Fail(ctx context.Context, id int64) error {
	return s.captures.SetCaptureStatus(id, "failed")
}

func (s *CaptureService) AutoApproveEnabled() bool {
	if v, ok := s.settings.GetSetting("agent_capture_auto_approve"); ok {
		return v == "true" || v == "1"
	}
	return false
}

func (s *CaptureService) captureDir(taskID int64) string {
	return filepath.Join(s.dataDir, "captures", fmt.Sprintf("task-%d", taskID))
}

func (s *CaptureService) findKeyframes(taskID, captureID int64) []string {
	dir := filepath.Join(s.captureDir(taskID), fmt.Sprintf("capture-%d", captureID))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var frames []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
			frames = append(frames, filepath.Join(dir, e.Name()))
		}
	}
	return frames
}
