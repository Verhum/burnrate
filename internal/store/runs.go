package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type Run = domain.Run

func (s *Store) CreateRun(taskID int64, worktreePath, branch, repoPath, windowID string, attempt int) (*Run, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO runs (task_id, worktree_path, branch, repo_path, window_id, attempt, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID, worktreePath, branch, repoPath, windowID, attempt, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	id, _ := res.LastInsertId()
	return s.getRun(id)
}

func (s *Store) getRun(id int64) (*Run, error) {
	r := &Run{}
	err := s.db.QueryRow(`
		SELECT id, task_id, session_id, worktree_path, branch, repo_path, pid, status,
		       attempt, cost_usd, num_turns, pr_url, error, window_id,
		       rate_limit_reset_at, started_at, ended_at, agent_repo, agent_worked_in,
		       result_text, model, lines_added, lines_removed
		FROM runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.TaskID, &r.SessionID, &r.WorktreePath, &r.Branch, &r.RepoPath,
		&r.PID, &r.Status, &r.Attempt, &r.CostUSD, &r.NumTurns, &r.PRURL,
		&r.Error, &r.WindowID, &r.RateLimitResetAt, &r.StartedAt, &r.EndedAt,
		&r.AgentRepo, &r.AgentWorkedIn, &r.ResultText,
		&r.Model, &r.LinesAdded, &r.LinesRemoved)
	if err != nil {
		return nil, fmt.Errorf("get run %d: %w", id, err)
	}
	return r, nil
}

func (s *Store) GetRun(id int64) (*Run, error) {
	return s.getRun(id)
}

func (s *Store) SetRunSessionID(id int64, sessionID string) error {
	_, err := s.db.Exec("UPDATE runs SET session_id=? WHERE id=?", sessionID, id)
	return err
}

func (s *Store) SetRunPID(id int64, pid int) error {
	_, err := s.db.Exec("UPDATE runs SET pid=? WHERE id=?", pid, id)
	return err
}

func (s *Store) SetRunRateLimitResetAt(id int64, resetAt string) error {
	_, err := s.db.Exec("UPDATE runs SET rate_limit_reset_at=? WHERE id=?", resetAt, id)
	return err
}

func (s *Store) SetRunStatus(id int64, status string) error {
	_, err := s.db.Exec("UPDATE runs SET status=? WHERE id=?", status, id)
	return err
}

func (s *Store) FinishRun(id int64, status string, costUSD float64, numTurns int, prURL, errMsg, resultText string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE runs SET status=?, cost_usd=?, num_turns=?, pr_url=?, error=?, result_text=?, ended_at=? WHERE id=?`,
		status, costUSD, numTurns, prURL, errMsg, resultText, now, id,
	)
	return err
}

func (s *Store) RunsByStatus(statuses ...string) ([]Run, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat(",?", len(statuses))[1:]
	query := "SELECT id, task_id, session_id, worktree_path, branch, repo_path, pid, status, attempt, cost_usd, num_turns, pr_url, error, window_id, rate_limit_reset_at, started_at, ended_at, agent_repo, agent_worked_in, result_text, model, lines_added, lines_removed FROM runs WHERE status IN (" +
		placeholders + ") ORDER BY id DESC"
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}

	return s.queryRuns(query, args...)
}

func (s *Store) ResumableRuns() ([]Run, error) {
	return s.queryRuns(`
		SELECT r.id, r.task_id, r.session_id, r.worktree_path, r.branch, r.repo_path,
		       r.pid, r.status, r.attempt, r.cost_usd, r.num_turns, r.pr_url,
		       r.error, r.window_id, r.rate_limit_reset_at, r.started_at, r.ended_at,
		       r.agent_repo, r.agent_worked_in, r.result_text, r.model, r.lines_added, r.lines_removed
		FROM runs r
		INNER JOIN tasks t ON t.id = r.task_id
		WHERE t.status = 'resumable' AND r.session_id != ''
		  AND r.id = (SELECT MAX(id) FROM runs WHERE task_id = r.task_id)
		ORDER BY t.sort_order ASC`)
}

func (s *Store) ListRuns(taskID int64, limit int) ([]Run, error) {
	if taskID > 0 {
		return s.queryRuns(
			`SELECT id, task_id, session_id, worktree_path, branch, repo_path, pid, status,
			        attempt, cost_usd, num_turns, pr_url, error, window_id,
			        rate_limit_reset_at, started_at, ended_at, agent_repo, agent_worked_in,
			        result_text, model, lines_added, lines_removed
			 FROM runs WHERE task_id = ? ORDER BY id DESC LIMIT ?`,
			taskID, limit,
		)
	}
	return s.queryRuns(
		`SELECT id, task_id, session_id, worktree_path, branch, repo_path, pid, status,
		        attempt, cost_usd, num_turns, pr_url, error, window_id,
		        rate_limit_reset_at, started_at, ended_at, agent_repo, agent_worked_in,
		        result_text, model, lines_added, lines_removed
		 FROM runs ORDER BY id DESC LIMIT ?`,
		limit,
	)
}

func (s *Store) queryRuns(query string, args ...any) ([]Run, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.TaskID, &r.SessionID, &r.WorktreePath, &r.Branch,
			&r.RepoPath, &r.PID, &r.Status, &r.Attempt, &r.CostUSD, &r.NumTurns,
			&r.PRURL, &r.Error, &r.WindowID, &r.RateLimitResetAt, &r.StartedAt, &r.EndedAt,
			&r.AgentRepo, &r.AgentWorkedIn, &r.ResultText,
			&r.Model, &r.LinesAdded, &r.LinesRemoved); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func scanRun(row interface{ Scan(...any) error }) (*Run, error) {
	r := &Run{}
	err := row.Scan(&r.ID, &r.TaskID, &r.SessionID, &r.WorktreePath, &r.Branch,
		&r.RepoPath, &r.PID, &r.Status, &r.Attempt, &r.CostUSD, &r.NumTurns,
		&r.PRURL, &r.Error, &r.WindowID, &r.RateLimitResetAt, &r.StartedAt, &r.EndedAt,
		&r.AgentRepo, &r.AgentWorkedIn, &r.ResultText,
		&r.Model, &r.LinesAdded, &r.LinesRemoved)
	if err != nil {
		return nil, err
	}
	return r, nil
}

type WindowAggregate = domain.WindowAggregate

func (s *Store) WindowAggregate(windowID string) (WindowAggregate, error) {
	var agg WindowAggregate
	err := s.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(cost_usd), 0) FROM runs WHERE window_id = ?",
		windowID,
	).Scan(&agg.Count, &agg.CostSum)
	return agg, err
}

// LatestRunForTask returns the most recent run for the given task, or nil if none.
func (s *Store) LatestRunForTask(taskID int64) (*Run, error) {
	r, err := scanRun(s.db.QueryRow(`
		SELECT id, task_id, session_id, worktree_path, branch, repo_path, pid, status,
		       attempt, cost_usd, num_turns, pr_url, error, window_id,
		       rate_limit_reset_at, started_at, ended_at, agent_repo, agent_worked_in,
		       result_text, model, lines_added, lines_removed
		FROM runs WHERE task_id = ? ORDER BY id DESC LIMIT 1`, taskID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (s *Store) SetRunBranch(id int64, branch string) error {
	_, err := s.db.Exec("UPDATE runs SET branch=? WHERE id=?", branch, id)
	return err
}

func (s *Store) SetRunAgentRepo(id int64, repo string) error {
	_, err := s.db.Exec("UPDATE runs SET agent_repo=? WHERE id=?", repo, id)
	return err
}

func (s *Store) SetRunAgentWorkedIn(id int64, workedIn string) error {
	_, err := s.db.Exec("UPDATE runs SET agent_worked_in=? WHERE id=?", workedIn, id)
	return err
}

// SetRunModel records which model produced a run, the grouping key of the
// cost-efficiency analytics.
func (s *Store) SetRunModel(id int64, model string) error {
	_, err := s.db.Exec("UPDATE runs SET model=? WHERE id=?", model, id)
	return err
}

// TaskStatsMap returns aggregate run statistics keyed by task id.
func (s *Store) TaskStatsMap() (map[int64]domain.TaskStats, error) {
	rows, err := s.db.Query(`
		SELECT
			task_id,
			COUNT(*) AS runs,
			COALESCE(SUM(cost_usd), 0) AS cost_usd,
			COALESCE(SUM(lines_added), 0) AS lines_added,
			COALESCE(SUM(lines_removed), 0) AS lines_removed,
			COALESCE(SUM(
				CASE WHEN started_at != '' AND ended_at != ''
				THEN CAST((julianday(ended_at) - julianday(started_at)) * 86400 AS INTEGER)
				ELSE 0 END
			), 0) AS duration_sec
		FROM runs
		GROUP BY task_id`)
	if err != nil {
		return nil, fmt.Errorf("task stats map: %w", err)
	}
	defer rows.Close()

	m := make(map[int64]domain.TaskStats)
	for rows.Next() {
		var ts domain.TaskStats
		if err := rows.Scan(&ts.TaskID, &ts.Runs, &ts.CostUSD,
			&ts.LinesAdded, &ts.LinesRemoved, &ts.DurationSec); err != nil {
			return nil, err
		}
		m[ts.TaskID] = ts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fill in the latest model per task.
	modelRows, err := s.db.Query(`
		SELECT task_id, model FROM runs
		WHERE id IN (SELECT MAX(id) FROM runs WHERE model != '' GROUP BY task_id)`)
	if err != nil {
		return m, nil
	}
	defer modelRows.Close()
	for modelRows.Next() {
		var taskID int64
		var model string
		if err := modelRows.Scan(&taskID, &model); err != nil {
			continue
		}
		if ts, ok := m[taskID]; ok {
			ts.Model = model
			m[taskID] = ts
		}
	}

	return m, nil
}

// SetRunLines records the line churn this run contributed. See migration 011:
// the value is a delta against the branch totals on task_prs, never a branch
// total, so summing runs does not double-count a branch two runs worked on.
func (s *Store) SetRunLines(id int64, added, removed int) error {
	_, err := s.db.Exec("UPDATE runs SET lines_added=?, lines_removed=? WHERE id=?", added, removed, id)
	return err
}
