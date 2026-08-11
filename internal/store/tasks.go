package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type Task = domain.Task

func (s *Store) CreateTask(title, prompt, repoPath, size, model, status string) (*Task, error) {
	if status == "" {
		status = "queued"
	}
	if status != "queued" && status != "backlog" {
		return nil, fmt.Errorf("create task: status must be 'queued' or 'backlog', got %q", status)
	}

	var maxOrder sql.NullFloat64
	_ = s.db.QueryRow("SELECT MAX(sort_order) FROM tasks").Scan(&maxOrder)
	nextOrder := 10.0
	if maxOrder.Valid {
		nextOrder = maxOrder.Float64 + 10.0
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO tasks (title, prompt, repo_path, size, model, sort_order, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		title, prompt, repoPath, size, model, nextOrder, status, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	id, _ := res.LastInsertId()
	return s.GetTask(id)
}

func (s *Store) GetTask(id int64) (*Task, error) {
	t := &Task{}
	var latestStatus, latestPR, latestSession sql.NullString
	err := s.db.QueryRow(`
		SELECT t.id, t.title, t.prompt, t.repo_path, t.size, t.model, t.sort_order, t.status,
		       t.created_at, t.updated_at, t.attempt_reset_run_id, t.summary,
		       r.status, r.pr_url, r.session_id
		FROM tasks t
		LEFT JOIN runs r ON r.id = (SELECT id FROM runs WHERE task_id = t.id ORDER BY id DESC LIMIT 1)
		WHERE t.id = ?`, id,
	).Scan(&t.ID, &t.Title, &t.Prompt, &t.RepoPath, &t.Size, &t.Model, &t.SortOrder, &t.Status,
		&t.CreatedAt, &t.UpdatedAt, &t.AttemptResetRunID, &t.Summary,
		&latestStatus, &latestPR, &latestSession,
	)
	if err != nil {
		return nil, fmt.Errorf("get task %d: %w", id, err)
	}
	t.DisplayID = fmt.Sprintf("BR%d", t.ID)
	if latestStatus.Valid {
		t.LatestRunStatus = latestStatus.String
	}
	if latestPR.Valid {
		t.LatestRunPRURL = latestPR.String
	}
	if latestSession.Valid && latestSession.String != "" {
		t.HasSession = true
	}
	t.PRs, _ = s.ListTaskPRs(t.ID)
	return t, nil
}

func (s *Store) ListTasks() ([]Task, error) {
	// Fetched before the task cursor is opened: the pool allows a single
	// connection, so querying while rows are still open would deadlock.
	prsByTask, _ := s.AllTaskPRs()

	rows, err := s.db.Query(`
		SELECT t.id, t.title, t.prompt, t.repo_path, t.size, t.model, t.sort_order, t.status,
		       t.created_at, t.updated_at, t.attempt_reset_run_id, t.summary,
		       r.status, r.pr_url, r.session_id
		FROM tasks t
		LEFT JOIN runs r ON r.id = (SELECT id FROM runs WHERE task_id = t.id ORDER BY id DESC LIMIT 1)
		ORDER BY t.sort_order ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var latestStatus, latestPR, latestSession sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Prompt, &t.RepoPath, &t.Size, &t.Model, &t.SortOrder, &t.Status,
			&t.CreatedAt, &t.UpdatedAt, &t.AttemptResetRunID, &t.Summary,
			&latestStatus, &latestPR, &latestSession,
		); err != nil {
			return nil, err
		}
		t.DisplayID = fmt.Sprintf("BR%d", t.ID)
		if latestStatus.Valid {
			t.LatestRunStatus = latestStatus.String
		}
		if latestPR.Valid {
			t.LatestRunPRURL = latestPR.String
		}
		if latestSession.Valid && latestSession.String != "" {
			t.HasSession = true
		}
		t.PRs = prsByTask[t.ID]
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) UpdateTask(id int64, title, prompt, repoPath, size, model string) (*Task, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE tasks SET title=?, prompt=?, repo_path=?, size=?, model=?, updated_at=? WHERE id=?`,
		title, prompt, repoPath, size, model, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update task %d: %w", id, err)
	}
	return s.GetTask(id)
}

func (s *Store) SetTaskSummary(id int64, summary string) {
	s.db.Exec("UPDATE tasks SET summary=? WHERE id=?", summary, id)
}

func (s *Store) DeleteTask(id int64) error {
	_, err := s.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	return err
}

func (s *Store) ReorderTasks(orderedIDs []int64) error {
	if len(orderedIDs) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	placeholders := strings.Repeat(",?", len(orderedIDs))[1:]
	args := make([]any, len(orderedIDs))
	for i, id := range orderedIDs {
		args[i] = id
	}

	rows, err := tx.Query(
		"SELECT sort_order FROM tasks WHERE id IN ("+placeholders+") ORDER BY sort_order ASC",
		args...,
	)
	if err != nil {
		return fmt.Errorf("reorder: %w", err)
	}
	var orders []float64
	for rows.Next() {
		var o float64
		if err := rows.Scan(&o); err != nil {
			rows.Close()
			return fmt.Errorf("reorder: %w", err)
		}
		orders = append(orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reorder: %w", err)
	}

	if len(orders) != len(orderedIDs) {
		return fmt.Errorf("reorder: some task ids not found")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for i, id := range orderedIDs {
		if _, err := tx.Exec("UPDATE tasks SET sort_order=?, updated_at=? WHERE id=?", orders[i], now, id); err != nil {
			return fmt.Errorf("reorder: %w", err)
		}
	}
	return tx.Commit()
}

// ResetTaskAttempts discounts everything the task has already tried, so its next
// launch is attempt 1 again and it gets a full max_attempts to work with. It
// records the newest run id rather than clearing a counter, because the counter
// lives on the run rows and those stay as they ran (see migration 012).
//
// Called whenever a user touches the task by hand — an edit, or a move back into
// a status the scheduler acts on. Idempotent, and a no-op in effect for a task
// with no runs yet. Returns the id it reset to.
func (s *Store) ResetTaskAttempts(id int64) (int64, error) {
	var newest sql.NullInt64
	if err := s.db.QueryRow("SELECT MAX(id) FROM runs WHERE task_id = ?", id).Scan(&newest); err != nil {
		return 0, fmt.Errorf("reset attempts for task %d: %w", id, err)
	}
	// A reset never moves backwards: a later edit while the newest run is
	// older than an earlier reset point must not un-discount runs.
	if _, err := s.db.Exec(
		"UPDATE tasks SET attempt_reset_run_id = MAX(attempt_reset_run_id, ?) WHERE id = ?",
		newest.Int64, id,
	); err != nil {
		return 0, fmt.Errorf("reset attempts for task %d: %w", id, err)
	}
	return newest.Int64, nil
}

func (s *Store) SetTaskStatus(id int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec("UPDATE tasks SET status=?, updated_at=? WHERE id=?", status, now, id)
	return err
}

func (s *Store) QueuedTasksByOrder() ([]Task, error) {
	rows, err := s.db.Query(`
		SELECT id, title, prompt, repo_path, size, model, sort_order, status, created_at, updated_at,
		       attempt_reset_run_id
		FROM tasks WHERE status = 'queued' ORDER BY sort_order ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Prompt, &t.RepoPath, &t.Size, &t.Model, &t.SortOrder, &t.Status,
			&t.CreatedAt, &t.UpdatedAt, &t.AttemptResetRunID); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) TaskCountsByStatus() (map[string]int, error) {
	rows, err := s.db.Query("SELECT status, COUNT(*) FROM tasks GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

// ValidateStatusTransition checks whether a status transition is allowed.
// Returns nil if the transition is valid, or an error describing the problem.
// Note: resumable requires an additional session check in the handler.
func ValidateStatusTransition(from, to string) error {
	if from == "running" {
		return fmt.Errorf("cannot change status of a running task")
	}
	if to == "pr_created" || to == "awaiting_human" {
		return fmt.Errorf("%s is set by the runner, not by users", to)
	}
	if to == "running" {
		return fmt.Errorf("running is set by the scheduler, not by users")
	}
	validTargets := map[string]bool{
		"queued": true, "backlog": true, "paused": true,
		"done": true, "dismissed": true, "failed": true, "resumable": true,
	}
	if !validTargets[to] {
		return fmt.Errorf("unknown target status %q", to)
	}
	return nil
}

func (s *Store) TasksByStatus(status string) ([]Task, error) {
	rows, err := s.db.Query(`
		SELECT id, title, prompt, repo_path, size, model, sort_order, status, created_at, updated_at,
		       attempt_reset_run_id
		FROM tasks WHERE status = ? ORDER BY sort_order ASC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Prompt, &t.RepoPath, &t.Size, &t.Model, &t.SortOrder, &t.Status,
			&t.CreatedAt, &t.UpdatedAt, &t.AttemptResetRunID); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
