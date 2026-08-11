PRAGMA foreign_keys=OFF;
CREATE TABLE tasks_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    prompt TEXT NOT NULL,
    repo_path TEXT NOT NULL DEFAULT '',
    size TEXT NOT NULL DEFAULT 'medium' CHECK(size IN ('small', 'medium', 'large')),
    sort_order REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued', 'running', 'resumable', 'done', 'failed', 'paused', 'dismissed', 'backlog')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
INSERT INTO tasks_new SELECT * FROM tasks;
DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;
UPDATE sqlite_sequence SET name='tasks' WHERE name='tasks_new';
PRAGMA foreign_keys=ON;
