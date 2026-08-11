CREATE TABLE task_prs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    run_id INTEGER NOT NULL DEFAULT 0,
    repo TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    worked_in TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE UNIQUE INDEX idx_task_prs_target ON task_prs(task_id, repo, branch);

CREATE INDEX idx_task_prs_task ON task_prs(task_id);

INSERT INTO task_prs (task_id, run_id, repo, branch, pr_url, worked_in, created_at)
SELECT task_id, id, agent_repo, branch, pr_url, agent_worked_in,
       CASE WHEN ended_at != '' THEN ended_at ELSE started_at END
FROM runs
WHERE pr_url LIKE 'http%'
ORDER BY id ASC
ON CONFLICT(task_id, repo, branch) DO UPDATE SET
    run_id = excluded.run_id,
    pr_url = excluded.pr_url,
    worked_in = excluded.worked_in,
    created_at = excluded.created_at;
