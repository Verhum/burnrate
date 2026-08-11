package store

import (
	"fmt"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type Attachment = domain.Attachment

func (s *Store) AddAttachment(taskID int64, filename, contentType string, sizeBytes int64) (*Attachment, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO task_attachments (task_id, filename, content_type, size_bytes, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		taskID, filename, contentType, sizeBytes, now,
	)
	if err != nil {
		return nil, fmt.Errorf("add attachment: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Attachment{
		ID:          id,
		TaskID:      taskID,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   sizeBytes,
		CreatedAt:   now,
	}, nil
}

func (s *Store) ListAttachments(taskID int64) ([]Attachment, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, filename, content_type, size_bytes, created_at
		FROM task_attachments WHERE task_id = ? ORDER BY id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.TaskID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, rows.Err()
}

func (s *Store) GetAttachment(id int64) (*Attachment, error) {
	a := &Attachment{}
	err := s.db.QueryRow(`
		SELECT id, task_id, filename, content_type, size_bytes, created_at
		FROM task_attachments WHERE id = ?`, id,
	).Scan(&a.ID, &a.TaskID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get attachment %d: %w", id, err)
	}
	return a, nil
}

func (s *Store) DeleteAttachment(id int64) error {
	_, err := s.db.Exec("DELETE FROM task_attachments WHERE id = ?", id)
	return err
}
