-- Human Loop M1: the request pipeline.
--
-- human_requests tracks questions an agent poses to the human. Each request
-- lives on a task and optionally on a run, and follows a simple lifecycle:
-- pending -> answered|denied|expired|canceled.
--
-- The tasks table gains an awaiting_human status so parked tasks are visible
-- but inert to the scheduler (which only picks up status='queued').

PRAGMA foreign_keys=OFF;

CREATE TABLE IF NOT EXISTS human_requests (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id             INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    run_id              INTEGER NOT NULL DEFAULT 0,
    kind                TEXT NOT NULL CHECK(kind IN ('question', 'demo', 'capture_approval')),
    title               TEXT NOT NULL DEFAULT '',
    body                TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'answered', 'denied', 'expired', 'canceled')),
    live                INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    answered_at         TEXT NOT NULL DEFAULT '',
    response_comment_id INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_human_requests_task ON human_requests(task_id);
CREATE INDEX IF NOT EXISTS idx_human_requests_status ON human_requests(status);

-- This migration runs outside a transaction (it toggles PRAGMA foreign_keys),
-- so a mid-way failure can leave tasks_new behind. Clear it before rebuilding.
DROP TABLE IF EXISTS tasks_new;

CREATE TABLE tasks_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    title           TEXT NOT NULL,
    prompt          TEXT NOT NULL,
    repo_path       TEXT NOT NULL DEFAULT '',
    size            TEXT NOT NULL DEFAULT 'medium' CHECK(size IN ('small', 'medium', 'large')),
    model           TEXT NOT NULL DEFAULT '',
    sort_order      REAL NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued', 'running', 'resumable', 'done', 'failed', 'paused', 'dismissed', 'backlog', 'pr_created', 'awaiting_human')),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    attempt_reset_run_id INTEGER NOT NULL DEFAULT 0,
    summary         TEXT NOT NULL DEFAULT ''
);

-- Columns are listed explicitly: the live tasks table has model/summary/
-- attempt_reset_run_id appended at the end by earlier ALTER TABLE migrations,
-- so its physical order does not match tasks_new and SELECT * would shift
-- values into the wrong columns.
INSERT INTO tasks_new (
    id, title, prompt, repo_path, size, model, sort_order, status,
    created_at, updated_at, attempt_reset_run_id, summary
)
SELECT
    id, title, prompt, repo_path, size, model, sort_order, status,
    created_at, updated_at, attempt_reset_run_id, summary
FROM tasks;
DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;
UPDATE sqlite_sequence SET name='tasks' WHERE name='tasks_new';

PRAGMA foreign_keys=ON;
