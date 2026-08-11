package scheduler

import (
	"context"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/runner"
	"github.com/Verhum/burnrate/internal/scheduling"
	"github.com/Verhum/burnrate/internal/usage"
)

const (
	// maxSnapshotAge is how stale a usage reading may be and still be scheduled
	// against. The usage API rate-limits aggressively, so readings do go missing
	// for minutes at a time; beyond this we stop guessing.
	maxSnapshotAge = 15 * time.Minute
	// fallbackRunTimeout applies only if config carries no estimate at all.
	fallbackRunTimeout = 75 * time.Minute
)

// This file is the whole adapter between the daemon and internal/scheduling.
// The daemon owns I/O — the store, the usage API, spawning claude — and the
// policy owns every decision. Nothing here decides anything: it gathers the
// world into an Input, and turns the resulting Plan into side effects.

// runTimeoutLocked is the flat wall-clock budget for a run. It is deliberately
// independent of how much of the session remains: burnrate models no task
// duration, so there is nothing to compare the remaining window against, and a
// run is allowed to straddle a session reset.
//
// s.mu must be held.
func (s *Scheduler) runTimeoutLocked() time.Duration {
	if e, ok := s.cfg.SizeEstimates["medium"]; ok && e.MaxTimeout > 0 {
		return e.MaxTimeout
	}
	return fallbackRunTimeout
}

// policyLocked projects config onto the scheduling policy. s.mu must be held.
func (s *Scheduler) policyLocked() scheduling.Policy {
	return scheduling.Policy{
		Workers:          s.cfg.ParallelN,
		SessionThreshold: s.cfg.UtilThreshold,
		WeeklyThreshold:  s.cfg.SevenDayThreshold,
		MaxAttempts:      s.cfg.MaxAttempts,
		RunTimeout:       s.runTimeoutLocked(),
		MaxSnapshotAge:   maxSnapshotAge,
	}
}

// toPolicySnapshot narrows a usage reading to the fields policy consumes.
func toPolicySnapshot(snap *usage.Snapshot) *scheduling.Snapshot {
	if snap == nil {
		return nil
	}
	return &scheduling.Snapshot{
		CapturedAt:      snap.CapturedAt,
		SessionUtil:     snap.FiveHour.Utilization,
		SessionResetsAt: snap.FiveHour.ResetsAt,
		WeeklyUtil:      snap.SevenDay.Utilization,
		WeeklyResetsAt:  snap.SevenDay.ResetsAt,
	}
}

// plan asks the policy what to do right now. It returns the Plan alongside the
// store rows behind each candidate, so callers can act on a decision without
// re-querying.
//
// Every consumer goes through here — launching, the status endpoint, the
// per-task forecast — which is what stops the UI from disagreeing with the
// daemon. force names tasks launched by hand (see RunNow).
func (s *Scheduler) plan(force map[int64]bool) (scheduling.Plan, map[int64]candidate) {
	// Queried before taking the lock: this hits the database, and s.mu also
	// guards the launch path.
	candidates := s.fetchCandidates()

	byID := make(map[int64]candidate, len(candidates))
	policyCandidates := make([]scheduling.Candidate, 0, len(candidates))
	for _, c := range candidates {
		byID[c.task.ID] = c
		pc := scheduling.Candidate{TaskID: c.task.ID, Title: c.task.Title}
		if c.resume != nil {
			pc.Resume = true
			pc.Attempt = domain.EffectiveAttempt(c.task.AttemptResetRunID, c.resume)
		}
		policyCandidates = append(policyCandidates, pc)
	}

	s.mu.Lock()
	input := scheduling.Input{
		Now:        time.Now(),
		Snapshot:   toPolicySnapshot(s.lastSnap),
		Policy:     s.policyLocked(),
		Candidates: policyCandidates,
		Running:    s.runningLocked(),
		Force:      force,
	}
	if s.backoff != nil {
		input.WeeklyBackoffUntil = *s.backoff
	}
	s.mu.Unlock()

	return scheduling.Decide(input), byID
}

// runningLocked lists the task IDs holding a worker. s.mu must be held.
func (s *Scheduler) runningLocked() []int64 {
	ids := make([]int64, 0, len(s.inflight))
	for taskID := range s.inflight {
		ids = append(ids, taskID)
	}
	return ids
}

// launch starts the run a decision calls for.
//
// parent must be a context whose lifetime matches the run, not the request that
// triggered it: for scheduled work that is the daemon's context, and for a
// manual launch it must be context.Background(), since an HTTP request context
// is cancelled the moment the handler returns and would kill the run instantly.
func (s *Scheduler) launch(parent context.Context, c candidate, d scheduling.Decision) {
	s.mu.Lock()
	if _, already := s.inflight[c.task.ID]; already {
		s.mu.Unlock()
		return
	}
	launchCtx, cancel := context.WithCancelCause(parent)
	s.inflight[c.task.ID] = cancel

	// Snapshotted under the lock so the run uses the account selected at this
	// moment; SetAccount mutates these fields under the same lock.
	cfgCopy := s.cfg
	windowID := ""
	if s.lastSnap != nil && !s.lastSnap.FiveHour.ResetsAt.IsZero() {
		windowID = s.lastSnap.FiveHour.ResetsAt.Format(time.RFC3339)
	}
	s.mu.Unlock()

	params := runner.Params{
		Timeout:  d.Timeout,
		WindowID: windowID,
		OnRunMutation: func(taskID int64) {
			if s.OnBroadcast != nil {
				s.OnBroadcast("run_update", taskID)
			}
			s.RequestRefresh()
		},
	}

	s.logger.Infof("launching task %d (%s), timeout=%s, resume=%v — %s",
		c.task.ID, c.task.Title, d.Timeout, c.resume != nil, d.Reason.Text)

	launchFn := s.LaunchFunc
	if launchFn == nil {
		launchFn = runner.Run
	}

	taskCopy := c.task
	resumeCopy := c.resume

	s.wg.Add(1)
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inflight, taskCopy.ID)
			s.mu.Unlock()
			cancel(nil)
			if s.OnBroadcast != nil {
				s.OnBroadcast("run_complete", taskCopy.ID)
			}
			s.RequestRefresh()
			s.wg.Done()
		}()
		if err := launchFn(launchCtx, s.st, cfgCopy, taskCopy, resumeCopy, params, s.logger); err != nil {
			s.logger.Warnf("task %d finished with error: %v", taskCopy.ID, err)
		}
	}()
}

// suspend pauses a running worker so it picks up again in the next session.
//
// The run is cancelled with an ErrSuspended cause, which the runner records as
// rate_limited with the reset time and leaves the task resumable — so the
// ordinary candidate queue relaunches it once the session opens. Suspending is
// not reported as an error.
//
// It does consume an attempt: runner.Run computes a resume's attempt as
// prior.Attempt + 1 regardless of why the prior run stopped. That is intended —
// MaxAttempts is sized for interruptions rather than failures (default 20) —
// but it does mean a task cannot span more than MaxAttempts sessions.
func (s *Scheduler) suspend(d scheduling.Decision) {
	s.mu.Lock()
	cancel, running := s.inflight[d.TaskID]
	var resetAt time.Time
	if s.lastSnap != nil {
		resetAt = s.lastSnap.FiveHour.ResetsAt
	}
	s.mu.Unlock()

	if !running {
		return
	}
	s.logger.Infof("suspending task %d: %s", d.TaskID, d.Reason.Text)
	cancel(&runner.ErrSuspended{ResetAt: resetAt})
}

// fail gives up on a task that has burned its attempts, tearing down whatever
// workspace the last run left behind.
func (s *Scheduler) fail(c candidate, d scheduling.Decision) {
	s.logger.Warnf("task %d (%s): %s, marking failed", c.task.ID, c.task.Title, d.Reason.Text)
	s.st.SetTaskStatus(c.task.ID, "failed")
	if s.OnBroadcast != nil {
		s.OnBroadcast("run_update", c.task.ID)
	}
	if c.resume == nil || c.resume.WorktreePath == "" {
		return
	}
	go func(wtPath, repoPath string, runID int64) {
		cpCtx, cpCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cpCancel()
		runner.CheckpointAndRemove(cpCtx, wtPath, repoPath, runID, s.logger)
	}(c.resume.WorktreePath, c.resume.RepoPath, c.resume.ID)
}

// forecastFrom renders a plan for display. One entry per candidate, carrying
// the same reason the daemon acted on.
func forecastFrom(plan scheduling.Plan) []ForecastEntry {
	entries := make([]ForecastEntry, 0, len(plan.Decisions))
	for _, d := range plan.Decisions {
		entries = append(entries, ForecastEntry{
			TaskID:     d.TaskID,
			Action:     string(d.Action),
			ReasonCode: string(d.Reason.Code),
			Reason:     d.Reason.Text,
		})
	}
	return entries
}
