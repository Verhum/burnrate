package store

import (
	"fmt"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type Capture = domain.Capture

func (s *Store) CreateCapture(taskID, requestID int64, initiator, targetDesc, mode string) (*Capture, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO captures (task_id, request_id, initiator, target_desc, mode, status, created_at)
		 VALUES (?, ?, ?, ?, ?, 'processing', ?)`,
		taskID, requestID, initiator, targetDesc, mode, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create capture: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Capture{
		ID:         id,
		TaskID:     taskID,
		RequestID:  requestID,
		Initiator:  initiator,
		TargetDesc: targetDesc,
		Mode:       mode,
		Status:     "processing",
		CreatedAt:  now,
	}, nil
}

func (s *Store) GetCapture(id int64) (*Capture, error) {
	c := &Capture{}
	err := s.db.QueryRow(`
		SELECT id, task_id, request_id, initiator, target_desc, mode, status,
		       video_path, transcript, notes, duration_sec, created_at
		FROM captures WHERE id = ?`, id,
	).Scan(&c.ID, &c.TaskID, &c.RequestID, &c.Initiator, &c.TargetDesc, &c.Mode,
		&c.Status, &c.VideoPath, &c.Transcript, &c.Notes, &c.DurationSec, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get capture %d: %w", id, err)
	}
	return c, nil
}

func (s *Store) ListCaptures(taskID int64) ([]Capture, error) {
	query := `SELECT id, task_id, request_id, initiator, target_desc, mode, status,
		       video_path, transcript, notes, duration_sec, created_at
		FROM captures`
	var args []any
	if taskID > 0 {
		query += ` WHERE task_id = ?`
		args = append(args, taskID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []Capture
	for rows.Next() {
		var c Capture
		if err := rows.Scan(&c.ID, &c.TaskID, &c.RequestID, &c.Initiator, &c.TargetDesc,
			&c.Mode, &c.Status, &c.VideoPath, &c.Transcript, &c.Notes, &c.DurationSec, &c.CreatedAt); err != nil {
			return nil, err
		}
		captures = append(captures, c)
	}
	return captures, rows.Err()
}

func (s *Store) SetCaptureStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE captures SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) SetCaptureVideoPath(id int64, videoPath string) error {
	_, err := s.db.Exec(`UPDATE captures SET video_path = ? WHERE id = ?`, videoPath, id)
	return err
}

func (s *Store) SetCaptureTranscript(id int64, transcript string) error {
	_, err := s.db.Exec(`UPDATE captures SET transcript = ? WHERE id = ?`, transcript, id)
	return err
}

func (s *Store) SetCaptureNotes(id int64, notes string) error {
	_, err := s.db.Exec(`UPDATE captures SET notes = ? WHERE id = ?`, notes, id)
	return err
}

func (s *Store) SetCaptureDuration(id int64, durationSec float64) error {
	_, err := s.db.Exec(`UPDATE captures SET duration_sec = ? WHERE id = ?`, durationSec, id)
	return err
}

func (s *Store) FinishCapture(id int64, videoPath, transcript string, durationSec float64) error {
	_, err := s.db.Exec(
		`UPDATE captures SET status = 'ready', video_path = ?, transcript = ?, duration_sec = ? WHERE id = ?`,
		videoPath, transcript, durationSec, id)
	return err
}
