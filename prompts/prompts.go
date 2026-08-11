package prompts

import _ "embed"

//go:embed worker-new.md
var WorkerNew string

//go:embed worker-resume.md
var WorkerResume string

//go:embed worker-followup.md
var WorkerFollowup string

//go:embed worker-agent.md
var WorkerAgent string

// WorkerContinue is the prompt used to nudge a session that stopped because a
// tool call was auto-denied ("wait for the user" with no user present).
//
//go:embed worker-continue.md
var WorkerContinue string

// DenialPolicy is appended to every worker prompt: unattended runs must never
// treat an auto-denied tool call as a reason to stop.
//
//go:embed denial-policy.md
var DenialPolicy string

// EffortLevels describes the four levels of effort — how thoroughly a worker
// carries a task, not how hard the model thinks. The runner appends the level
// resolved for the run underneath it.
//
//go:embed effort-levels.md
var EffortLevels string
