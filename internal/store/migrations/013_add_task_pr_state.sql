-- A PR outlives the run that opened it: it gets reviewed, merged, or closed
-- long after burnrate stops watching. These columns cache what `gh pr view`
-- last reported so the UI can color a PR chip by its real state instead of
-- showing every PR as if it were still awaiting review.
--
-- pr_state is gh's own vocabulary (OPEN / MERGED / CLOSED) rather than a local
-- enum, so a value that arrives unrecognised renders as "unknown" instead of
-- being silently mapped onto the wrong color. Empty means never probed.
-- pr_checked_at is what makes MERGED/CLOSED terminal: the prober skips a PR it
-- has already seen reach an end state, and re-probes the rest on an interval.

ALTER TABLE task_prs ADD COLUMN pr_state TEXT NOT NULL DEFAULT '';
ALTER TABLE task_prs ADD COLUMN pr_is_draft INTEGER NOT NULL DEFAULT 0;
ALTER TABLE task_prs ADD COLUMN pr_checked_at TEXT NOT NULL DEFAULT '';
