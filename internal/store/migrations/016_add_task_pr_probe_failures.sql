-- Stop unreadable PR URLs from costing a gh call on every sweep, forever.
--
-- The old probe-failure path wrote pr_state = '' and stamped pr_checked_at. But
-- '' already means "never probed", so the row became a candidate again as soon
-- as it aged past MinAge: every URL that could not be read was re-probed on
-- every sweep, and they all arrived as a burst of warnings at every startup.
--
-- The prober now separates the two failures it can tell apart. A GitHub 404 is a
-- verdict and is recorded as pr_state = 'GONE', never probed again unless a user
-- asks. Everything else (no auth, no network, rate limit, an unresolvable repo)
-- says nothing about the PR, so the cached state survives and only this counter
-- moves. The sweep then waits MinAge doubled per consecutive failure, capped at a
-- day. Any answer resets the counter to 0, so it only accumulates while a URL is
-- genuinely unreadable.
--
-- The backfill retires the rows already in that state. A row with a check
-- timestamp but no state can only have come from the old failure path: the
-- upsert clears both columns together when a branch URL changes, and a
-- successful probe never yields an empty state (git.ProbePR errors on one).
-- Twelve failures puts them straight at the cap, so instead of a burst at the
-- next startup they get one quiet probe a day, enough to mark the truly dead
-- ones GONE and to recover any that were only failing transiently. Nothing here
-- asserts a verdict the daemon has not actually obtained.
--
-- What put ~50 of these in the author DB: the burnrate repo was renamed to
-- burnrate-legacy and a new repo took over the old name, so every stored
-- github.com/Verhum/burnrate/pull/N above N=24 now points into a repo where that
-- number never existed. GitHub cannot redirect a name that has been re-used.

ALTER TABLE task_prs ADD COLUMN pr_probe_failures INTEGER NOT NULL DEFAULT 0;

UPDATE task_prs
SET pr_probe_failures = 12
WHERE pr_state = '' AND pr_checked_at != '';
