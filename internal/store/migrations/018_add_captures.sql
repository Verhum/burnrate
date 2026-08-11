-- Migration 018: Add captures table for M2-M4 (agent screenshots, human demos,
-- self-capture tickets). A capture is one recording or screenshot session.

CREATE TABLE IF NOT EXISTS captures (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id         INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    request_id      INTEGER NOT NULL DEFAULT 0,
    initiator       TEXT NOT NULL CHECK(initiator IN ('human', 'agent')),
    target_desc     TEXT NOT NULL DEFAULT '',
    mode            TEXT NOT NULL DEFAULT 'screenshot' CHECK(mode IN ('screenshot', 'video')),
    status          TEXT NOT NULL DEFAULT 'processing' CHECK(status IN ('processing', 'ready', 'failed')),
    video_path      TEXT NOT NULL DEFAULT '',
    transcript      TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    duration_sec    REAL NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX idx_captures_task ON captures(task_id);
CREATE INDEX idx_captures_status ON captures(status);
