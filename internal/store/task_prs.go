package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type TaskPR = domain.TaskPR

// UpsertTaskPR records one repo's outcome for a task. A task can produce work
// in several repos, so PRs are keyed by (task, repo, branch) rather than being
// a single column on the run: a resumed run that re-reports the same branch
// updates the existing row instead of duplicating it.
func (s *Store) UpsertTaskPR(taskID, runID int64, repo, branch, prURL, workedIn string) error {
	// Agents sometimes write prose into the PR trailer ("none (the API was
	// down)"). Anything that is not a URL is stored as no-PR rather than
	// becoming a broken link in the UI.
	if !strings.HasPrefix(prURL, "http") {
		prURL = ""
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO task_prs (task_id, run_id, repo, branch, pr_url, worked_in, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id, repo, branch) DO UPDATE SET
			run_id = excluded.run_id,
			pr_url = CASE WHEN excluded.pr_url != '' THEN excluded.pr_url ELSE task_prs.pr_url END,
			worked_in = CASE WHEN excluded.worked_in != '' THEN excluded.worked_in ELSE task_prs.worked_in END,
			-- A new URL on the same branch is a different PR, so the cached
			-- state describes something else and has to be dropped.
			pr_state = CASE WHEN excluded.pr_url NOT IN ('', task_prs.pr_url) THEN '' ELSE task_prs.pr_state END,
			pr_is_draft = CASE WHEN excluded.pr_url NOT IN ('', task_prs.pr_url) THEN 0 ELSE task_prs.pr_is_draft END,
			pr_checked_at = CASE WHEN excluded.pr_url NOT IN ('', task_prs.pr_url) THEN '' ELSE task_prs.pr_checked_at END,
			pr_probe_failures = CASE WHEN excluded.pr_url NOT IN ('', task_prs.pr_url) THEN 0 ELSE task_prs.pr_probe_failures END`,
		taskID, runID, repo, branch, prURL, workedIn, now,
	)
	if err != nil {
		return fmt.Errorf("upsert task pr: %w", err)
	}
	return nil
}

// RecordTaskPRLines stores a branch's cumulative line churn and returns how much
// of it is new since the last measurement.
//
// A branch is measured from scratch on every run that touches it (the diff
// against its merge-base), so a followup run sees the earlier run's lines too.
// Attributing the full total to every run would count the same lines two or
// three times in any per-day or per-model sum, so the cumulative figure is kept
// here and only the delta is attributed to the run. The delta is clamped at zero
// because a branch can shrink — a followup that deletes code must not hand the
// run a negative line count.
//
// The (task, repo, branch) row must already exist; UpsertTaskPR creates it.
func (s *Store) RecordTaskPRLines(taskID int64, repo, branch string, added, removed int) (deltaAdded, deltaRemoved int, err error) {
	var prevAdded, prevRemoved int
	err = s.db.QueryRow(
		"SELECT lines_added, lines_removed FROM task_prs WHERE task_id = ? AND repo = ? AND branch = ?",
		taskID, repo, branch,
	).Scan(&prevAdded, &prevRemoved)
	if err != nil {
		return 0, 0, fmt.Errorf("read prior lines for %s@%s: %w", repo, branch, err)
	}

	if _, err := s.db.Exec(
		"UPDATE task_prs SET lines_added = ?, lines_removed = ? WHERE task_id = ? AND repo = ? AND branch = ?",
		added, removed, taskID, repo, branch,
	); err != nil {
		return 0, 0, fmt.Errorf("record lines for %s@%s: %w", repo, branch, err)
	}

	return max(0, added-prevAdded), max(0, removed-prevRemoved), nil
}

// SetTaskPRState caches what gh last reported about a PR. Reaching an answer at
// all — including the local "GONE" verdict — ends any run of probe failures, so
// the backoff in prstatus starts from scratch next time.
func (s *Store) SetTaskPRState(id int64, state string, isDraft bool, checkedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE task_prs
		 SET pr_state = ?, pr_is_draft = ?, pr_checked_at = ?, pr_probe_failures = 0
		 WHERE id = ?`,
		state, isDraft, checkedAt.UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("set task pr state: %w", err)
	}
	return nil
}

// RecordTaskPRProbeFailure notes that a probe failed without saying anything
// about the PR — no auth, no network, a repo gh cannot resolve. The cached state
// is left alone (see prstatus) and only the counter and check time move, which is
// what lets the sweep back off a URL that keeps failing instead of paying for it
// on every tick.
func (s *Store) RecordTaskPRProbeFailure(id int64, checkedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE task_prs
		 SET pr_checked_at = ?, pr_probe_failures = pr_probe_failures + 1
		 WHERE id = ?`,
		checkedAt.UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("record task pr probe failure: %w", err)
	}
	return nil
}

func (s *Store) ListTaskPRs(taskID int64) ([]TaskPR, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, run_id, repo, branch, pr_url, worked_in, created_at,
		       lines_added, lines_removed, pr_state, pr_is_draft, pr_checked_at,
		       pr_probe_failures
		FROM task_prs WHERE task_id = ? ORDER BY id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prs []TaskPR
	for rows.Next() {
		var p TaskPR
		if err := rows.Scan(&p.ID, &p.TaskID, &p.RunID, &p.Repo, &p.Branch,
			&p.PRURL, &p.WorkedIn, &p.CreatedAt, &p.LinesAdded, &p.LinesRemoved,
			&p.PRState, &p.PRIsDraft, &p.PRCheckedAt, &p.PRProbeFailures); err != nil {
			return nil, err
		}
		prs = append(prs, p)
	}
	return prs, rows.Err()
}

// AllTaskPRs returns every recorded PR grouped by task id. ListTasks uses it to
// attach PRs in one query instead of one per task.
func (s *Store) AllTaskPRs() (map[int64][]TaskPR, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, run_id, repo, branch, pr_url, worked_in, created_at,
		       lines_added, lines_removed, pr_state, pr_is_draft, pr_checked_at,
		       pr_probe_failures
		FROM task_prs ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byTask := make(map[int64][]TaskPR)
	for rows.Next() {
		var p TaskPR
		if err := rows.Scan(&p.ID, &p.TaskID, &p.RunID, &p.Repo, &p.Branch,
			&p.PRURL, &p.WorkedIn, &p.CreatedAt, &p.LinesAdded, &p.LinesRemoved,
			&p.PRState, &p.PRIsDraft, &p.PRCheckedAt, &p.PRProbeFailures); err != nil {
			return nil, err
		}
		byTask[p.TaskID] = append(byTask[p.TaskID], p)
	}
	return byTask, rows.Err()
}
