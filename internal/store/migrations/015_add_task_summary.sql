-- Stores the parsed summary from the worker's structured output, so task cards
-- can show what was done without reading the full comment body.
ALTER TABLE tasks ADD COLUMN summary TEXT NOT NULL DEFAULT '';
