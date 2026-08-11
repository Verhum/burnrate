-- Per-task model override. Empty string means "use the global config model".
-- When set, the runner uses this model instead of the config-level default,
-- so different tasks can target different Claude models.

ALTER TABLE tasks ADD COLUMN model TEXT NOT NULL DEFAULT '';
