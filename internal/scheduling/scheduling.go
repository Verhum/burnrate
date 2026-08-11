// Package scheduling holds every scheduling decision burnrate makes.
//
// It is deliberately pure: no database, no network, no clock, no logging. All
// state arrives in an Input and all conclusions leave in a Plan. That is what
// makes the policy testable as a table of cases rather than as an orchestration
// of fakes, and it is why this package must not grow an import of internal/store,
// internal/usage or internal/runner.
//
// The scheduler daemon is the only caller. It gathers an Input, calls Decide,
// and performs the side effects the Plan asks for. Everything that reports on
// scheduling — the status endpoint, the per-task forecast chips, the logs —
// renders the same Plan, so the UI cannot disagree with what the daemon does.
//
// Model:
//
//   - There are Workers slots. Tasks launch until the slots are full.
//   - A run gets a flat RunTimeout. Task duration is not modelled or estimated,
//     so the remaining time in a session never shortens a timeout and never
//     blocks a launch; a run may straddle a session reset.
//   - When the session limit is spent, running workers are suspended and resumed
//     in the next session. When the weekly limit is spent, nothing launches.
package scheduling

import (
	"fmt"
	"time"
)

// Action is what the daemon should do about one task.
type Action string

const (
	// ActionLaunch starts a fresh run, or resumes one from its session.
	ActionLaunch Action = "launch"
	// ActionWait means do nothing; the Decision's Reason says why.
	ActionWait Action = "wait"
	// ActionSuspend stops an in-flight run so it can resume in the next
	// session. The run ends as rate_limited and its task becomes resumable.
	ActionSuspend Action = "suspend"
	// ActionFail gives up on a task that has burned its attempts.
	ActionFail Action = "fail"
)

// ReasonCode is the stable, machine-readable half of a Reason. Clients switch
// on the code; humans read the Text. Never render a code directly.
type ReasonCode string

const (
	// ReasonReady means this task can launch, or (as Plan.Global) that
	// scheduling is unblocked.
	ReasonReady ReasonCode = "ready"
	// ReasonRunning means the task already has a worker.
	ReasonRunning ReasonCode = "running"
	// ReasonWorkersBusy means every worker slot is taken. This is the honest
	// answer to "why is my queued task not running" and must never be
	// reported as a session-reset wait.
	ReasonWorkersBusy ReasonCode = "workers_busy"
	// ReasonSessionExhausted means the 5h session limit is spent. New work
	// waits and running work is suspended until the session resets.
	ReasonSessionExhausted ReasonCode = "session_exhausted"
	// ReasonWeeklyExhausted means the 7-day limit is spent.
	ReasonWeeklyExhausted ReasonCode = "weekly_exhausted"
	// ReasonWeeklyBackoff means we are holding off until a recorded weekly
	// reset, even if the latest reading looks survivable.
	ReasonWeeklyBackoff ReasonCode = "weekly_backoff"
	// ReasonAttemptCap means the task has failed MaxAttempts times.
	ReasonAttemptCap ReasonCode = "attempt_cap"
	// ReasonNoUsageData means no usage snapshot has been read yet.
	ReasonNoUsageData ReasonCode = "no_usage_data"
	// ReasonStaleUsageData means the newest snapshot is too old to schedule
	// against — usually the usage API rate-limiting us.
	ReasonStaleUsageData ReasonCode = "stale_usage_data"
	// ReasonQueueEmpty means there is genuinely nothing to schedule. It must
	// only ever be reported when Candidates is empty.
	ReasonQueueEmpty ReasonCode = "queue_empty"
)

// Reason is a scheduling verdict: a stable code plus text meant for a human.
// Rendering happens here, once, so the daemon log, the status endpoint and the
// task chips all say the same words.
type Reason struct {
	Code ReasonCode `json:"code"`
	Text string     `json:"text"`
}

func reasonf(code ReasonCode, format string, args ...any) Reason {
	return Reason{Code: code, Text: fmt.Sprintf(format, args...)}
}

// Blocking reports whether this reason explains why work is not progressing,
// as opposed to describing healthy state.
func (r Reason) Blocking() bool {
	switch r.Code {
	case ReasonReady, ReasonRunning, ReasonQueueEmpty:
		return false
	default:
		return true
	}
}

// Window states. These describe the session quota, not scheduling behaviour —
// there is no DRAINING state because the end of a session no longer changes
// what the scheduler does.
const (
	WindowIdle      = "IDLE"
	WindowOpen      = "OPEN"
	WindowSaturated = "SATURATED"
)

// Snapshot is the usage reading the decision is made against. It is a narrowed
// view of usage.Snapshot: only the fields policy actually consumes.
type Snapshot struct {
	CapturedAt      time.Time
	SessionUtil     float64
	SessionResetsAt time.Time
	WeeklyUtil      float64
	WeeklyResetsAt  time.Time
}

// Policy is the tunable half of the decision, derived from config.
type Policy struct {
	// Workers is the maximum number of concurrent runs.
	Workers int
	// SessionThreshold is the 5h utilization percentage at which the session
	// counts as spent.
	SessionThreshold float64
	// WeeklyThreshold is the 7-day utilization percentage at which the week
	// counts as spent.
	WeeklyThreshold float64
	// MaxAttempts is how many times a task may be tried before it fails.
	MaxAttempts int
	// RunTimeout is the flat wall-clock budget for a single run. It does not
	// depend on how much of the session remains.
	RunTimeout time.Duration
	// MaxSnapshotAge is how old a usage reading may be and still be scheduled
	// against. Zero disables the check.
	MaxSnapshotAge time.Duration
}

// Candidate is a task that could run: either queued, or resumable from a
// previous run's session. Callers pass them in priority order.
type Candidate struct {
	TaskID int64
	Title  string
	// Resume is true when this task continues an earlier run via its session.
	Resume bool
	// Attempt is how many times the task has already been tried. Only
	// meaningful when Resume is true.
	Attempt int
}

// Input is everything Decide is allowed to look at.
type Input struct {
	Now      time.Time
	Snapshot *Snapshot
	Policy   Policy
	// Candidates are launchable tasks in priority order (resumable first,
	// then queued by sort order).
	Candidates []Candidate
	// Running are the task IDs that currently hold a worker.
	Running []int64
	// WeeklyBackoffUntil holds launches back until this time, regardless of
	// the current weekly reading. Zero means no backoff.
	WeeklyBackoffUntil time.Time
	// Force names tasks the operator launched by hand. They bypass every gate
	// except "already running" and the attempt cap, and they do not consume a
	// worker slot in the accounting — a manual launch is allowed to exceed
	// Workers.
	Force map[int64]bool
}

// Decision is the verdict for one task.
type Decision struct {
	TaskID int64  `json:"task_id"`
	Action Action `json:"action"`
	// Timeout is the run's wall-clock budget. Only set when Action is
	// ActionLaunch.
	Timeout time.Duration `json:"timeout"`
	Reason  Reason        `json:"reason"`
}

// Plan is the complete result of one scheduling pass.
type Plan struct {
	WindowState string `json:"window_state"`
	// Global answers "why is nothing happening?" in one line. It is
	// ReasonReady when work is launching, ReasonQueueEmpty only when there
	// are no candidates at all, and otherwise the reason that is holding the
	// highest-priority candidate back.
	Global Reason `json:"global"`
	// Decisions has one entry per candidate, in input order.
	Decisions []Decision `json:"decisions"`
	// Suspend has one entry per running task that should be stopped and
	// resumed in the next session.
	Suspend []Decision `json:"suspend"`
}

// Launches returns the decisions the daemon should act on by starting a run.
func (p Plan) Launches() []Decision {
	var out []Decision
	for _, d := range p.Decisions {
		if d.Action == ActionLaunch {
			out = append(out, d)
		}
	}
	return out
}

// Failures returns the decisions for tasks that have exhausted their attempts.
func (p Plan) Failures() []Decision {
	var out []Decision
	for _, d := range p.Decisions {
		if d.Action == ActionFail {
			out = append(out, d)
		}
	}
	return out
}

// For returns the decision for a task, if the task was a candidate.
func (p Plan) For(taskID int64) (Decision, bool) {
	for _, d := range p.Decisions {
		if d.TaskID == taskID {
			return d, true
		}
	}
	return Decision{}, false
}

// Decide is the single scheduling decision in burnrate. Every consumer —
// launching, the status endpoint, per-task forecasting — derives from its Plan.
func Decide(in Input) Plan {
	plan := Plan{
		WindowState: WindowStateFor(in.Snapshot, in.Policy.SessionThreshold),
		Decisions:   make([]Decision, 0, len(in.Candidates)),
	}

	gate := globalGate(in)
	plan.Global = gate

	// A spent session pauses work in progress. Everything else that blocks
	// launching leaves running workers alone: a run that is already burning
	// quota should finish, and the weekly limit is not something a running
	// run can make worse in a way suspending it would fix.
	if gate.Code == ReasonSessionExhausted {
		for _, id := range in.Running {
			plan.Suspend = append(plan.Suspend, Decision{
				TaskID: id,
				Action: ActionSuspend,
				Reason: gate,
			})
		}
	}

	free := in.Policy.Workers - len(in.Running)
	var firstBlocked Reason

	for _, c := range in.Candidates {
		d := Decision{TaskID: c.TaskID, Action: ActionWait}

		switch {
		case contains(in.Running, c.TaskID):
			d.Reason = reasonf(ReasonRunning, "running")

		case c.Resume && c.Attempt >= in.Policy.MaxAttempts:
			d.Action = ActionFail
			d.Reason = reasonf(ReasonAttemptCap, "gave up after %d attempts", in.Policy.MaxAttempts)

		case in.Force[c.TaskID]:
			d.Action = ActionLaunch
			d.Timeout = in.Policy.RunTimeout
			d.Reason = reasonf(ReasonReady, "launched by hand")

		case gate.Blocking():
			d.Reason = gate

		case free <= 0:
			d.Reason = reasonf(ReasonWorkersBusy, "queued — all %d workers busy", in.Policy.Workers)

		default:
			d.Action = ActionLaunch
			d.Timeout = in.Policy.RunTimeout
			d.Reason = reasonf(ReasonReady, "ready")
			free--
		}

		// Only a task that is still waiting *and* held up by something counts as
		// an explanation for the queue. A candidate that is already running is
		// not blocked, and one being failed is being resolved rather than held —
		// letting either win here would mask whatever is actually stopping the
		// rest of the queue.
		if d.Action == ActionWait && d.Reason.Blocking() && firstBlocked.Code == "" {
			firstBlocked = d.Reason
		}
		plan.Decisions = append(plan.Decisions, d)
	}

	// Global must explain the observable situation. If no gate is tripped but
	// nothing is launching anyway, report what is actually holding the queue —
	// never "queue empty", which is only true with no candidates at all.
	if !plan.Global.Blocking() && len(plan.Launches()) == 0 {
		switch {
		case len(in.Candidates) == 0:
			plan.Global = reasonf(ReasonQueueEmpty, "queue empty")
		case firstBlocked.Code != "":
			plan.Global = firstBlocked
		}
	}

	return plan
}

// globalGate reports the one thing stopping every launch, or ReasonReady.
// Worker saturation is deliberately absent: it is a per-candidate condition,
// because a full worker pool still lets a forced launch through and still needs
// to read as "queued", not as a quota problem.
func globalGate(in Input) Reason {
	if in.Snapshot == nil {
		return reasonf(ReasonNoUsageData, "waiting for first usage reading")
	}

	if in.Policy.MaxSnapshotAge > 0 {
		if age := in.Now.Sub(in.Snapshot.CapturedAt); age > in.Policy.MaxSnapshotAge {
			return reasonf(ReasonStaleUsageData, "usage reading %s old", age.Round(time.Minute))
		}
	}

	if in.Snapshot.SessionUtil >= in.Policy.SessionThreshold {
		// Expressed as a duration, not a clock time: this package has no
		// timezone and formatting one here would make the policy's output
		// depend on the host's locale.
		if resets := in.Snapshot.SessionResetsAt; !resets.IsZero() && resets.After(in.Now) {
			return reasonf(ReasonSessionExhausted, "session spent (%.0f%%) — new session in %s",
				in.Snapshot.SessionUtil, resets.Sub(in.Now).Round(time.Minute))
		}
		return reasonf(ReasonSessionExhausted, "session spent (%.0f%%)", in.Snapshot.SessionUtil)
	}

	if in.Snapshot.WeeklyUtil >= in.Policy.WeeklyThreshold {
		return reasonf(ReasonWeeklyExhausted, "weekly limit spent (%.0f%%)", in.Snapshot.WeeklyUtil)
	}

	if !in.WeeklyBackoffUntil.IsZero() && in.Now.Before(in.WeeklyBackoffUntil) {
		return reasonf(ReasonWeeklyBackoff, "weekly backoff — %s remaining",
			in.WeeklyBackoffUntil.Sub(in.Now).Round(time.Minute))
	}

	return reasonf(ReasonReady, "ready")
}

// WindowStateFor describes the session quota. It is a pure function of the
// reading, so it cannot drift from what the gates decide.
func WindowStateFor(snap *Snapshot, sessionThreshold float64) string {
	if snap == nil {
		return WindowIdle
	}
	if snap.SessionUtil >= sessionThreshold {
		return WindowSaturated
	}
	if snap.SessionUtil > 0 || !snap.SessionResetsAt.IsZero() {
		return WindowOpen
	}
	return WindowIdle
}

// TaskStatusAfterInterruption is the status a task takes when a run stops
// before finishing — suspended for a spent session, timed out, killed, or
// orphaned by a daemon restart. A run that recorded a session can be resumed
// from it; one that never got that far has nothing to resume, so the task
// fails.
//
// This is the single definition of "is this recoverable?". The runner's
// classification of finished runs and the daemon's reconciliation of orphans
// both call it, so the two cannot disagree.
func TaskStatusAfterInterruption(hasSession bool) string {
	if hasSession {
		return "resumable"
	}
	return "failed"
}

func contains(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
