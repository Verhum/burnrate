-- Attempts chain across runs (runner.Run computes a resume's attempt as
-- prior.Attempt + 1) and count interruptions, not only failures, so a
-- long-running task walks toward max_attempts and is eventually failed. When a
-- user edits the task or moves it back into the queue by hand, that history is
-- no longer about the work being attempted now — the cap should start over.
--
-- attempt_reset_run_id is the id of the newest run at the moment of the reset.
-- Every run at or below it is discounted, so the next launch is attempt 1 again.
-- A run id is used rather than a timestamp because ids are monotonic and exact:
-- no clock skew, and no ambiguity for two runs recorded in the same second.
-- Run rows keep the attempt number they actually ran at; nothing is rewritten.

ALTER TABLE tasks ADD COLUMN attempt_reset_run_id INTEGER NOT NULL DEFAULT 0;
