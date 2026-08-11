package store

import (
	"fmt"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type Comment = domain.Comment

func (s *Store) AddComment(taskID int64, body, author string) (*Comment, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO task_comments (task_id, body, author, created_at) VALUES (?, ?, ?, ?)`,
		taskID, body, author, now,
	)
	if err != nil {
		return nil, fmt.Errorf("add comment: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Comment{ID: id, TaskID: taskID, Author: author, Body: body, CreatedAt: now, ConsumedByRun: 0}, nil
}

func (s *Store) CommentsForTask(taskID int64) ([]Comment, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, author, body, created_at, consumed_by_run
		 FROM task_comments WHERE task_id = ? ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.CreatedAt, &c.ConsumedByRun); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *Store) UnconsumedComments(taskID int64) ([]Comment, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, author, body, created_at, consumed_by_run
		 FROM task_comments WHERE task_id = ? AND consumed_by_run = 0
		 ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.CreatedAt, &c.ConsumedByRun); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *Store) GetComment(id int64) (*Comment, error) {
	c := &Comment{}
	err := s.db.QueryRow(
		`SELECT id, task_id, author, body, created_at, consumed_by_run
		 FROM task_comments WHERE id = ?`, id,
	).Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.CreatedAt, &c.ConsumedByRun)
	if err != nil {
		return nil, fmt.Errorf("get comment %d: %w", id, err)
	}
	return c, nil
}

func (s *Store) MarkCommentsConsumed(taskID, runID int64) error {
	_, err := s.db.Exec(
		`UPDATE task_comments SET consumed_by_run = ? WHERE task_id = ? AND consumed_by_run = 0`,
		runID, taskID,
	)
	return err
}

// MarkCommentConsumed retires exactly one comment, rather than sweeping the
// whole task the way MarkCommentsConsumed does. A reply handed to a live
// long-polling agent in-band has already been delivered, so it must not also
// ride the next run's "## Follow-up Instructions" injection — but the other
// unconsumed comments on that task still have to.
//
// runID 0 means "unconsumed" in this column, so callers with no run to
// attribute the delivery to must pass a non-zero sentinel (-1).
func (s *Store) MarkCommentConsumed(commentID, runID int64) error {
	if runID == 0 {
		runID = -1
	}
	_, err := s.db.Exec(
		`UPDATE task_comments SET consumed_by_run = ? WHERE id = ?`,
		runID, commentID,
	)
	return err
}
