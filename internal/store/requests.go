package store

import (
	"fmt"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type HumanRequest = domain.HumanRequest

func (s *Store) CreateHumanRequest(taskID, runID int64, kind, title, body string) (*HumanRequest, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO human_requests (task_id, run_id, kind, title, body, status, live, created_at)
		 VALUES (?, ?, ?, ?, ?, 'pending', 0, ?)`,
		taskID, runID, kind, title, body, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create human request: %w", err)
	}
	id, _ := res.LastInsertId()
	return &HumanRequest{
		ID:        id,
		TaskID:    taskID,
		RunID:     runID,
		Kind:      kind,
		Title:     title,
		Body:      body,
		Status:    "pending",
		CreatedAt: now,
	}, nil
}

func (s *Store) GetHumanRequest(id int64) (*HumanRequest, error) {
	r := &HumanRequest{}
	var live int
	err := s.db.QueryRow(`
		SELECT id, task_id, run_id, kind, title, body, status, live, created_at, answered_at, response_comment_id
		FROM human_requests WHERE id = ?`, id,
	).Scan(&r.ID, &r.TaskID, &r.RunID, &r.Kind, &r.Title, &r.Body, &r.Status, &live, &r.CreatedAt, &r.AnsweredAt, &r.ResponseCommentID)
	if err != nil {
		return nil, fmt.Errorf("get human request %d: %w", id, err)
	}
	r.Live = live != 0
	return r, nil
}

func (s *Store) ListHumanRequests(status string) ([]HumanRequest, error) {
	query := `SELECT id, task_id, run_id, kind, title, body, status, live, created_at, answered_at, response_comment_id
		FROM human_requests`
	var args []any
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	// FIFO + live-first: requests with a live long-poll sort above parked ones.
	query += ` ORDER BY live DESC, created_at ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []HumanRequest
	for rows.Next() {
		var r HumanRequest
		var live int
		if err := rows.Scan(&r.ID, &r.TaskID, &r.RunID, &r.Kind, &r.Title, &r.Body, &r.Status, &live, &r.CreatedAt, &r.AnsweredAt, &r.ResponseCommentID); err != nil {
			return nil, err
		}
		r.Live = live != 0
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

func (s *Store) SetHumanRequestStatus(id int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE human_requests SET status = ?, answered_at = ? WHERE id = ?`, status, now, id)
	return err
}

func (s *Store) SetHumanRequestLive(id int64, live bool) error {
	v := 0
	if live {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE human_requests SET live = ? WHERE id = ?`, v, id)
	return err
}

// ClearHumanRequestLive resets every live flag and reports how many rows it
// touched.
//
// `live` means "an agent is long-polling this request right now" — a property
// of a running process, not of the row. Nothing clears it when a daemon dies
// mid-poll, so the flag survives the restart: the request keeps sorting first
// (ListHumanRequests orders live DESC) and keeps wearing a LIVE badge, which
// points the human at the one request nobody is waiting on. Called once at
// daemon startup, where every long poll is known to be gone.
func (s *Store) ClearHumanRequestLive() (int64, error) {
	res, err := s.db.Exec(`UPDATE human_requests SET live = 0 WHERE live != 0`)
	if err != nil {
		return 0, fmt.Errorf("clear human request live flags: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Store) SetHumanRequestResponse(id int64, commentID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE human_requests SET status = 'answered', answered_at = ?, response_comment_id = ? WHERE id = ?`,
		now, commentID, id)
	return err
}

func (s *Store) PendingRequestCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM human_requests WHERE status = 'pending'`).Scan(&count)
	return count, err
}

// RequestCountsForRun reports how many human requests a run raised and how many
// of those are still pending. The two numbers together distinguish "the agent
// asked and is still waiting" (pending > 0) from "the human already replied in
// the park window" (total > 0, pending == 0) from "the agent parked without
// ever using the MCP tools" (total == 0).
func (s *Store) RequestCountsForRun(runID int64) (total, pending int, err error) {
	err = s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0)
		FROM human_requests WHERE run_id = ?`, runID,
	).Scan(&total, &pending)
	if err != nil {
		return 0, 0, fmt.Errorf("request counts for run %d: %w", runID, err)
	}
	return total, pending, nil
}

func (s *Store) CancelTaskRequests(taskID int64) error {
	_, err := s.db.Exec(`UPDATE human_requests SET status = 'canceled' WHERE task_id = ? AND status = 'pending'`, taskID)
	return err
}
