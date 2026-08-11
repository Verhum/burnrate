CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    prompt TEXT NOT NULL,
    repo_path TEXT NOT NULL DEFAULT '',
    size TEXT NOT NULL DEFAULT 'medium' CHECK(size IN ('small', 'medium', 'large')),
    sort_order REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued', 'running', 'resumable', 'done', 'failed', 'paused', 'dismissed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    session_id TEXT NOT NULL DEFAULT '',
    worktree_path TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    repo_path TEXT NOT NULL DEFAULT '',
    pid INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'starting' CHECK(status IN ('starting', 'running', 'succeeded', 'rate_limited', 'timed_out', 'errored', 'resuming', 'abandoned')),
    attempt INTEGER NOT NULL DEFAULT 1,
    cost_usd REAL NOT NULL DEFAULT 0,
    num_turns INTEGER NOT NULL DEFAULT 0,
    pr_url TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    window_id TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    ended_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS usage_snapshots (
    captured_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    five_hour_util REAL NOT NULL DEFAULT 0,
    five_hour_resets_at TEXT NOT NULL DEFAULT '',
    seven_day_util REAL NOT NULL DEFAULT 0,
    seven_day_resets_at TEXT NOT NULL DEFAULT '',
    seven_day_opus_util REAL,
    raw_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
