-- Cost-efficiency analytics needs two facts a run did not previously record:
-- which model produced the work, and how many lines that work changed.
--
-- Lines live in two places on purpose. task_prs holds the *cumulative* diff of a
-- branch (measured against its merge-base with the default branch), because a
-- followup run re-measures the whole branch. runs holds only that run's *delta*
-- against the branch total already recorded, so summing runs never double-counts
-- a branch that several runs contributed to.

ALTER TABLE runs ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN lines_added INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN lines_removed INTEGER NOT NULL DEFAULT 0;

ALTER TABLE task_prs ADD COLUMN lines_added INTEGER NOT NULL DEFAULT 0;
ALTER TABLE task_prs ADD COLUMN lines_removed INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_runs_started_model ON runs(started_at, model);
