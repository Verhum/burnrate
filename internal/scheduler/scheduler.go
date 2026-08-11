package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/runner"
	"github.com/Verhum/burnrate/internal/scheduling"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

type StatusInfo struct {
	WindowState       string          `json:"window_state"`
	FiveHourUtil      float64         `json:"five_hour_util"`
	SevenDayUtil      float64         `json:"seven_day_util"`
	ResetsAt          time.Time       `json:"resets_at"`
	SevenDayResetsAt  *time.Time      `json:"seven_day_resets_at,omitempty"`
	CapturedAt        *time.Time      `json:"captured_at,omitempty"`
	RunningCount      int             `json:"running_count"`
	RunningRuns       []RunSummary    `json:"running_runs"`
	NextCandidate     string          `json:"next_candidate,omitempty"`
	BlockedReason     string          `json:"blocked_reason,omitempty"`
	BlockedReasonCode string          `json:"blocked_reason_code,omitempty"`
	BackoffUntil      *time.Time      `json:"backoff_until,omitempty"`
	RateLimitUntil    *time.Time      `json:"rate_limit_until,omitempty"`
	RateLimitAttempt  int             `json:"rate_limit_attempt,omitempty"`
	Forecast          []ForecastEntry `json:"forecast"`
	Batch             *BatchInfo      `json:"batch,omitempty"`
	Limits            []LimitInfo     `json:"limits,omitempty"`
	Account           string          `json:"account"`
	TokenFreshness    string          `json:"token_freshness,omitempty"`
}

type RunSummary struct {
	RunID     int64  `json:"run_id"`
	TaskID    int64  `json:"task_id"`
	Title     string `json:"title"`
	Attempt   int    `json:"attempt"`
	StartedAt string `json:"started_at"`
}

// ForecastEntry is one task's scheduling verdict, for display.
//
// It carries no ETA on purpose. burnrate models no task duration, so there is
// no honest way to predict when a busy worker frees up; a task waiting behind
// the pool says so ("queued — all 3 workers busy") rather than inventing a
// clock time.
type ForecastEntry struct {
	TaskID     int64  `json:"task_id"`
	Action     string `json:"action"`
	ReasonCode string `json:"reason_code"`
	Reason     string `json:"reason"`
}

type BatchInfo struct {
	ParallelN          int     `json:"parallel_n"`
	RunningCount       int     `json:"running_count"`
	UtilThreshold      float64 `json:"util_threshold"`
	CurrentUtil        float64 `json:"current_util"`
	LaunchedThisWindow int     `json:"launched_this_window"`
	WindowCost         float64 `json:"window_cost"`
	MaxAttempts        int     `json:"max_attempts"`
	RunTimeoutMin      int     `json:"run_timeout_min"`
	Model              string  `json:"model"`
}

type LimitInfo struct {
	Kind       string  `json:"kind"`
	Group      string  `json:"group"`
	Percent    float64 `json:"percent"`
	Severity   string  `json:"severity"`
	IsActive   bool    `json:"is_active"`
	ResetsAt   string  `json:"resets_at"`
	ScopeModel string  `json:"scope_model,omitempty"`
}

type candidate struct {
	task   store.Task
	resume *store.Run
}

type Scheduler struct {
	st     *store.Store
	cfg    config.Config
	client *usage.Client
	logger *log.Logger
	// resets spots the edge into a new session. Session *state* is a pure
	// function of the latest reading (scheduling.WindowStateFor), so this holds
	// only what edge detection needs.
	resets scheduling.ResetDetector

	OnBroadcast func(event string, payload any)

	mu sync.Mutex
	// inflight maps task ID to its run's cancel func. The cause distinguishes a
	// suspend (resume next session) from a plain cancel.
	inflight map[int64]context.CancelCauseFunc
	wg       sync.WaitGroup // tracks in-flight launch goroutines
	lastSnap *usage.Snapshot
	// lastFetchAt debounces RequestRefresh. Snapshot *freshness* is judged from
	// the reading's own CapturedAt inside scheduling, not from here.
	lastFetchAt time.Time
	backoff     *time.Time
	stopped     chan struct{}
	refreshNow  chan struct{}
	// cfgReloaded wakes the poll loop when SetConfig changed poll_interval_sec,
	// which is the one config value baked into a live object rather than read
	// per use.
	cfgReloaded chan struct{}

	rateLimitBackoffUntil time.Time     // skip API calls until this time
	rateLimitDelay        time.Duration // current backoff duration (doubles on each 429)
	rateLimitConsecutive  int           // consecutive 429 count for log escalation
	lastIdleFetchAt       time.Time     // last successful fetch while idle (for idle throttle)

	LaunchFunc func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error
}

func New(st *store.Store, cfg config.Config, client *usage.Client, logger *log.Logger) *Scheduler {
	return &Scheduler{
		st:          st,
		cfg:         cfg,
		client:      client,
		logger:      logger,
		inflight:    make(map[int64]context.CancelCauseFunc),
		stopped:     make(chan struct{}),
		refreshNow:  make(chan struct{}, 1),
		cfgReloaded: make(chan struct{}, 1),
	}
}

// RequestRefresh asks the scheduler loop to perform an immediate usage fetch
// (debounced to at most once per 3 seconds). Non-blocking; safe to call from
// any goroutine.
func (s *Scheduler) RequestRefresh() {
	select {
	case s.refreshNow <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	cfg := s.Config()
	s.logger.Infof("scheduler starting (poll=%ds, parallel=%d, threshold=%.0f%%)",
		cfg.PollIntervalSec, cfg.ParallelN, cfg.UtilThreshold)

	s.reconcile()

	s.tick(ctx, false)

	ticker := time.NewTicker(pollInterval(cfg))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Infof("scheduler stopping")
			s.cancelAll()
			s.wg.Wait() // let in-flight runs kill claude + persist final state before we signal done
			close(s.stopped)
			return
		case <-ticker.C:
			s.tick(ctx, false)
		case <-s.refreshNow:
			s.mu.Lock()
			recent := time.Since(s.lastFetchAt) < 3*time.Second
			s.mu.Unlock()
			if !recent {
				s.tick(ctx, true)
			}
		case <-s.cfgReloaded:
			ticker.Reset(pollInterval(s.Config()))
		}
	}
}

func (s *Scheduler) Wait() {
	<-s.stopped
}

func (s *Scheduler) Status() StatusInfo {
	// One plan serves the whole response: window state, the blocked reason, the
	// next candidate and every task's chip all come from the same decision the
	// daemon acts on, so the UI cannot contradict it.
	plan, byID := s.plan(nil)

	s.mu.Lock()
	account := "inherited environment"
	if s.cfg.ClaudeConfigDir != "" {
		suffix := usage.ConfigDirSuffix(s.cfg.ClaudeConfigDir)
		account = s.cfg.ClaudeConfigDir + " (keychain " + suffix + ")"
	}
	info := StatusInfo{
		WindowState:  plan.WindowState,
		RunningCount: len(s.inflight),
		BackoffUntil: s.backoff,
		Account:      account,
	}
	if !s.rateLimitBackoffUntil.IsZero() && time.Now().Before(s.rateLimitBackoffUntil) {
		t := s.rateLimitBackoffUntil
		info.RateLimitUntil = &t
		info.RateLimitAttempt = s.rateLimitConsecutive
	}
	if s.cfg.ClaudeConfigDir != "" {
		info.TokenFreshness = tokenFreshness(s.cfg)
	}
	if s.lastSnap != nil {
		info.FiveHourUtil = s.lastSnap.FiveHour.Utilization
		info.SevenDayUtil = s.lastSnap.SevenDay.Utilization
		info.ResetsAt = s.lastSnap.FiveHour.ResetsAt
		if !s.lastSnap.SevenDay.ResetsAt.IsZero() {
			sevenResets := s.lastSnap.SevenDay.ResetsAt
			info.SevenDayResetsAt = &sevenResets
		}
		captured := s.lastSnap.CapturedAt
		info.CapturedAt = &captured
		for _, l := range s.lastSnap.Limits {
			info.Limits = append(info.Limits, LimitInfo{
				Kind:       l.Kind,
				Group:      l.Group,
				Percent:    l.Percent,
				Severity:   l.Severity,
				IsActive:   l.IsActive,
				ResetsAt:   l.ResetsAt,
				ScopeModel: l.ScopeModel,
			})
		}
	}

	inflightTasks := make([]int64, 0, len(s.inflight))
	for taskID := range s.inflight {
		inflightTasks = append(inflightTasks, taskID)
	}
	s.mu.Unlock()

	for _, taskID := range inflightTasks {
		run, err := s.st.LatestRunForTask(taskID)
		if err != nil || run == nil {
			continue
		}
		task, err := s.st.GetTask(taskID)
		if err != nil {
			continue
		}
		info.RunningRuns = append(info.RunningRuns, RunSummary{
			RunID:     run.ID,
			TaskID:    taskID,
			Title:     task.Title,
			Attempt:   run.Attempt,
			StartedAt: run.StartedAt,
		})
	}

	// A blocking Global is worth surfacing; "ready" and "queue empty" are not
	// problems and would only add noise.
	if plan.Global.Blocking() {
		info.BlockedReason = plan.Global.Text
		info.BlockedReasonCode = string(plan.Global.Code)
	}
	if launches := plan.Launches(); len(launches) > 0 {
		if c, ok := byID[launches[0].TaskID]; ok {
			info.NextCandidate = c.task.Title
		}
	}
	info.Forecast = forecastFrom(plan)
	info.Batch = s.batchInfo()

	return info
}

// ActiveConfigDir returns the pinned Claude Code config dir ("" = inherited).
func (s *Scheduler) ActiveConfigDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.ClaudeConfigDir
}

// SetAccount switches the active Claude Code account at runtime. It updates the
// account used for future run launches (env passed to spawned claude) and the
// usage client's token source, clears stale usage/backoff state from the
// previous account, and requests an immediate refresh so the UI picks up the
// new account's utilization.
func (s *Scheduler) SetAccount(configDir, sandboxKeychain, sandboxKeychainPasswordFile string) {
	s.mu.Lock()
	s.cfg.ClaudeConfigDir = configDir
	s.cfg.SandboxKeychain = sandboxKeychain
	s.cfg.SandboxKeychainPasswordFile = sandboxKeychainPasswordFile
	s.lastSnap = nil
	s.lastFetchAt = time.Time{}
	s.lastIdleFetchAt = time.Time{}
	s.rateLimitDelay = 0
	s.rateLimitBackoffUntil = time.Time{}
	s.rateLimitConsecutive = 0
	s.mu.Unlock()
	if s.client != nil {
		s.client.SetAccount(configDir, sandboxKeychain, sandboxKeychainPasswordFile)
	}
	s.RequestRefresh()
}

// Config returns the scheduler's current config snapshot. Every launch reads it
// through here, so a settings change reaches the next run without a restart.
func (s *Scheduler) Config() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// SetConfig replaces the config the scheduler launches runs with. The three
// account fields are excluded: they are owned by SetAccount, whose selection can
// be newer than any reload of the settings table.
func (s *Scheduler) SetConfig(cfg config.Config) {
	s.mu.Lock()
	cfg.ClaudeConfigDir = s.cfg.ClaudeConfigDir
	cfg.SandboxKeychain = s.cfg.SandboxKeychain
	cfg.SandboxKeychainPasswordFile = s.cfg.SandboxKeychainPasswordFile
	pollChanged := pollInterval(cfg) != pollInterval(s.cfg)
	s.cfg = cfg
	s.mu.Unlock()

	if !pollChanged {
		return
	}
	select {
	case s.cfgReloaded <- struct{}{}:
	default:
	}
}

// pollInterval clamps poll_interval_sec: time.NewTicker panics on a
// non-positive duration, and the value now comes from a live HTTP write.
func pollInterval(cfg config.Config) time.Duration {
	if cfg.PollIntervalSec < 1 {
		return time.Second
	}
	return time.Duration(cfg.PollIntervalSec) * time.Second
}

// Forecast reports what the scheduler would do with each candidate right now,
// with the reason attached.
func (s *Scheduler) Forecast() []ForecastEntry {
	plan, _ := s.plan(nil)
	return forecastFrom(plan)
}

func (s *Scheduler) batchInfo() *BatchInfo {
	s.mu.Lock()
	if s.lastSnap == nil {
		s.mu.Unlock()
		return nil
	}
	snap := *s.lastSnap
	running := len(s.inflight)
	s.mu.Unlock()

	windowID := ""
	if !snap.FiveHour.ResetsAt.IsZero() {
		windowID = snap.FiveHour.ResetsAt.Format(time.RFC3339)
	}
	var launched int
	var cost float64
	if windowID != "" {
		agg, err := s.st.WindowAggregate(windowID)
		if err == nil {
			launched = agg.Count
			cost = agg.CostSum
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return &BatchInfo{
		ParallelN:          s.cfg.ParallelN,
		RunningCount:       running,
		UtilThreshold:      s.cfg.UtilThreshold,
		CurrentUtil:        snap.FiveHour.Utilization,
		LaunchedThisWindow: launched,
		WindowCost:         cost,
		MaxAttempts:        s.cfg.MaxAttempts,
		RunTimeoutMin:      int(s.runTimeoutLocked().Minutes()),
		Model:              s.cfg.Model,
	}
}

func (s *Scheduler) tick(ctx context.Context, forceRefresh bool) {
	var snap usage.Snapshot
	var fetched bool

	s.mu.Lock()
	inBackoff := time.Now().Before(s.rateLimitBackoffUntil)
	idle := len(s.inflight) == 0
	idleThrottle := !forceRefresh && idle && !s.lastIdleFetchAt.IsZero() && time.Since(s.lastIdleFetchAt) < idlePollInterval
	s.mu.Unlock()

	if idleThrottle && !inBackoff {
		candidates := s.fetchCandidates()
		if len(candidates) == 0 {
			s.mu.Lock()
			have := s.lastSnap != nil
			if have {
				snap = *s.lastSnap
			}
			s.mu.Unlock()
			// Never broadcast a zero snapshot as if it were a reading — it
			// would replace whatever the UI already fetched with all-zero
			// utilization and empty reset times.
			if have && s.OnBroadcast != nil {
				s.OnBroadcast("usage", usageSnapshotJSON(snap))
			}
			return
		}
	}

	if !inBackoff {
		var err error
		snap, err = s.client.Fetch(ctx)
		if err != nil {
			if errors.Is(err, usage.ErrRateLimited429) {
				retryAfter := usage.RetryAfterFrom(err)
				s.mu.Lock()
				s.rateLimitDelay = nextRateLimitDelay(s.rateLimitDelay, retryAfter)
				s.rateLimitBackoffUntil = time.Now().Add(s.rateLimitDelay)
				s.rateLimitConsecutive++
				n := s.rateLimitConsecutive
				delay := s.rateLimitDelay
				s.mu.Unlock()
				if n >= 10 {
					s.logger.Errorf("usage API rate limited (429), attempt %d, backing off %s", n, delay.Round(time.Second))
				} else {
					s.logger.Warnf("usage API rate limited (429), attempt %d, backing off %s", n, delay.Round(time.Second))
				}
			} else {
				s.logger.Warnf("usage fetch failed: %v", err)
			}
		} else {
			fetched = true
			s.mu.Lock()
			s.lastSnap = &snap
			s.lastFetchAt = time.Now()
			s.rateLimitDelay = 0
			s.rateLimitBackoffUntil = time.Time{}
			s.rateLimitConsecutive = 0
			if idle {
				s.lastIdleFetchAt = time.Now()
			} else {
				s.lastIdleFetchAt = time.Time{}
			}
			s.mu.Unlock()

			s.storeSnapshot(snap)
			s.trimOldSnapshots()
		}
	}

	// A missing reading is no longer a reason to skip the tick. Staleness is a
	// scheduling gate (ReasonStaleUsageData), so reconciliation still runs and
	// the status endpoint still explains itself while the usage API is
	// rate-limiting us.
	haveSnapshot := fetched
	if !fetched {
		s.mu.Lock()
		if s.lastSnap != nil {
			snap = *s.lastSnap
			haveSnapshot = true
		}
		s.mu.Unlock()
	}

	if haveSnapshot {
		if s.OnBroadcast != nil {
			s.OnBroadcast("usage", usageSnapshotJSON(snap))
		}
		s.mu.Lock()
		reset := s.resets.Observe(*toPolicySnapshot(&snap))
		if reset {
			s.logger.Infof("new session detected")
			s.backoff = nil
		}
		s.mu.Unlock()
	}

	s.reconcile()
	s.evaluate(ctx)
}

const (
	rateLimitInitialDelay = 30 * time.Second
	rateLimitMaxDelay     = 10 * time.Minute
	idlePollInterval      = 60 * time.Second
)

var jitterFn = defaultJitter

func defaultJitter(d time.Duration) time.Duration {
	jitter := time.Duration(float64(d) * 0.2)
	half := jitter / 2
	n := time.Duration(time.Now().UnixNano() % int64(jitter+1))
	return d - half + n
}

func nextRateLimitDelay(current, retryAfter time.Duration) time.Duration {
	var base time.Duration
	if current == 0 {
		base = rateLimitInitialDelay
	} else {
		base = current * 2
	}
	if base > rateLimitMaxDelay {
		base = rateLimitMaxDelay
	}
	if retryAfter > base {
		base = retryAfter
		if base > rateLimitMaxDelay {
			base = rateLimitMaxDelay
		}
	}
	return jitterFn(base)
}

// usageSnapshotJSON is the wire shape of a usage reading. The SSE "usage" event
// and GET /api/usage must serialize the *same* type: usage.Snapshot carries no
// JSON tags, so broadcasting it directly published PascalCase keys that
// overwrote the snake_case ones the UI had just fetched, blanking 7D RESETS IN a
// few seconds after every load.
func usageSnapshotJSON(snap usage.Snapshot) store.UsageSnapshot {
	fiveResets := ""
	if !snap.FiveHour.ResetsAt.IsZero() {
		fiveResets = snap.FiveHour.ResetsAt.Format(time.RFC3339)
	}
	sevenResets := ""
	if !snap.SevenDay.ResetsAt.IsZero() {
		sevenResets = snap.SevenDay.ResetsAt.Format(time.RFC3339)
	}
	var opusUtil *float64
	if snap.SevenDayOpus != nil {
		v := snap.SevenDayOpus.Utilization
		opusUtil = &v
	}
	var scoped []store.ScopedWeekly
	for _, sw := range snap.ScopedWeekly {
		scoped = append(scoped, store.ScopedWeekly{Model: sw.Model, Percent: sw.Percent})
	}
	return store.UsageSnapshot{
		CapturedAt:       snap.CapturedAt.Format(time.RFC3339),
		FiveHourUtil:     snap.FiveHour.Utilization,
		FiveHourResetsAt: fiveResets,
		SevenDayUtil:     snap.SevenDay.Utilization,
		SevenDayResetsAt: sevenResets,
		SevenDayOpusUtil: opusUtil,
		ScopedWeekly:     scoped,
		RawJSON:          string(snap.Raw),
	}
}

func (s *Scheduler) storeSnapshot(snap usage.Snapshot) {
	s.st.InsertUsageSnapshot(usageSnapshotJSON(snap))
}

func (s *Scheduler) trimOldSnapshots() {
	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	s.st.TrimUsageSnapshots(cutoff)
}

// reconcile adopts or buries runs the previous daemon left behind.
//
// Broadcasts are deliberately deferred until after the lock is released.
// OnBroadcast reaches back into Status(), which calls plan(), which takes
// s.mu — and sync.Mutex is not reentrant, so broadcasting from inside the
// locked section deadlocked the scheduler against itself and parked every
// /api/status handler behind it permanently. Every other broadcast site in
// this package already broadcasts unlocked; this one is no different.
func (s *Scheduler) reconcile() {
	runs, err := s.st.RunsByStatus("starting", "running", "resuming")
	if err != nil {
		s.logger.Errorf("reconcile: list runs: %v", err)
		return
	}

	for _, taskID := range s.reconcileLocked(runs) {
		if s.OnBroadcast != nil {
			s.OnBroadcast("run_update", taskID)
		}
	}
}

// reconcileLocked does the state changes under s.mu and returns the tasks whose
// listeners need telling, leaving the notifying to the caller.
func (s *Scheduler) reconcileLocked(runs []store.Run) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	var touched []int64

	for _, r := range runs {
		if _, tracked := s.inflight[r.TaskID]; tracked {
			continue
		}

		if r.PID > 0 && processAlive(r.PID) {
			if !ProcessIsClaude(r.PID) {
				s.logger.Warnf("reconcile: run %d (task %d) pid %d is alive but not claude (recycled pid), marking errored",
					r.ID, r.TaskID, r.PID)
				s.st.FinishRun(r.ID, "errored", r.CostUSD, r.NumTurns, "", "daemon lost track (pid recycled)", "")
				if r.SessionID != "" {
					s.st.SetTaskStatus(r.TaskID, "resumable")
				} else {
					s.st.SetTaskStatus(r.TaskID, "failed")
				}
				touched = append(touched, r.TaskID)
				continue
			}

			s.logger.Warnf("reconcile: run %d (task %d) has orphaned claude pid %d — killing",
				r.ID, r.TaskID, r.PID)
			s.mu.Unlock()
			killOrphan(r.PID)
			s.mu.Lock()

			errText := "orphaned claude from previous daemon killed; will resume"
			s.st.FinishRun(r.ID, "errored", r.CostUSD, r.NumTurns, "", errText, "")
			s.st.SetTaskStatus(r.TaskID, scheduling.TaskStatusAfterInterruption(r.SessionID != ""))
			touched = append(touched, r.TaskID)
			continue
		}

		s.logger.Infof("reconcile: run %d (task %d) is dead, marking recoverable", r.ID, r.TaskID)
		s.st.FinishRun(r.ID, "errored", r.CostUSD, r.NumTurns, "", "process died", "")
		s.st.SetTaskStatus(r.TaskID, scheduling.TaskStatusAfterInterruption(r.SessionID != ""))
		touched = append(touched, r.TaskID)
	}

	return touched
}

// evaluate runs one scheduling pass: ask the policy what to do, then do it.
//
// It holds no scheduling logic of its own — every branch below is a Plan the
// policy produced.
func (s *Scheduler) evaluate(ctx context.Context) {
	s.recordWeeklyBackoff()

	plan, byID := s.plan(nil)

	// Suspends first: freeing workers a spent session cannot feed is the point.
	for _, d := range plan.Suspend {
		s.suspend(d)
	}
	for _, d := range plan.Failures() {
		if c, ok := byID[d.TaskID]; ok {
			s.fail(c, d)
		}
	}
	for _, d := range plan.Launches() {
		if c, ok := byID[d.TaskID]; ok {
			s.launch(ctx, c, d)
		}
	}
}

// recordWeeklyBackoff latches a backoff until the weekly reset once the 7-day
// limit is spent, and clears it once that time passes. The latch matters because
// the weekly reading can dip back under the threshold without the week actually
// having rolled over.
func (s *Scheduler) recordWeeklyBackoff() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastSnap == nil {
		return
	}
	if s.lastSnap.SevenDay.Utilization >= s.cfg.SevenDayThreshold {
		if s.backoff == nil {
			backoff := s.lastSnap.SevenDay.ResetsAt
			s.backoff = &backoff
			s.logger.Warnf("7-day utilization %.0f%% >= %.0f%%, backing off until %s",
				s.lastSnap.SevenDay.Utilization, s.cfg.SevenDayThreshold, backoff.Format(time.RFC3339))
		}
		return
	}
	if s.backoff != nil && !time.Now().Before(*s.backoff) {
		s.backoff = nil
	}
}

// fetchCandidates builds the ordered list of potential launch candidates
// (resumable first, then queued).
func (s *Scheduler) fetchCandidates() []candidate {
	resumable, err := s.st.ResumableRuns()
	if err != nil {
		s.logger.Errorf("fetchCandidates: resumable runs: %v", err)
	}
	queued, err := s.st.QueuedTasksByOrder()
	if err != nil {
		s.logger.Errorf("fetchCandidates: queued tasks: %v", err)
	}

	var candidates []candidate
	for _, r := range resumable {
		t, err := s.st.GetTask(r.TaskID)
		if err != nil {
			continue
		}
		rCopy := r
		candidates = append(candidates, candidate{task: *t, resume: &rCopy})
	}
	for _, t := range queued {
		candidates = append(candidates, candidate{task: t})
	}
	return candidates
}

// RunNow launches a task by hand, past the worker cap and the quota gates. It
// refuses only what a manual launch cannot fix: a task that is not
// queued/resumable, a missing claude binary, a task already running, or one
// that has burned its attempts.
//
// It goes through the same Decide call as scheduled work — with the task marked
// forced — so the run gets the same timeout and the same bookkeeping rather than
// a second, drifting implementation.
func (s *Scheduler) RunNow(_ context.Context, taskID int64) error {
	task, err := s.st.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if task.Status != "queued" && task.Status != "resumable" {
		return fmt.Errorf("task status is %s, must be queued or resumable", task.Status)
	}

	if _, err := exec.LookPath("claude"); err != nil {
		if !s.Config().DryRun {
			return fmt.Errorf("claude binary not found")
		}
	}

	if s.IsInflight(taskID) {
		return fmt.Errorf("task %d is already running", taskID)
	}

	plan, byID := s.plan(map[int64]bool{taskID: true})

	d, ok := plan.For(taskID)
	if !ok {
		// Not a candidate: the queue query disagrees with the task's status,
		// e.g. a resumable task whose last run recorded no session.
		return fmt.Errorf("task %d is not launchable", taskID)
	}
	switch d.Action {
	case scheduling.ActionLaunch:
	case scheduling.ActionFail:
		return fmt.Errorf("task %d %s", taskID, d.Reason.Text)
	default:
		return fmt.Errorf("task %d cannot launch: %s", taskID, d.Reason.Text)
	}

	c, ok := byID[taskID]
	if !ok {
		return fmt.Errorf("task %d is not launchable", taskID)
	}

	// The launch context is rooted at Background, never the caller's request
	// context: an HTTP request context is cancelled the moment the handler
	// returns, which would kill the run instantly. Shutdown still reaches it via
	// cancelAll()+wg.Wait(), and CancelTask via the inflight registry.
	s.launch(context.Background(), c, d)
	return nil
}

func (s *Scheduler) IsInflight(taskID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.inflight[taskID]
	return ok
}

// CancelTask signals the in-flight launch for the given task (if this daemon
// is running it) to stop. It cancels the launch context with an ErrCancelled
// cause, which makes the claude wrapper SIGTERM its own child process group
// and lets the runner transition the task to paused (not auto-retried).
// Returns true if a tracked in-flight launch was found and signalled.
//
// Prefer this over killing run.PID directly: CancelTask goes through the
// runner's cleanup flow (graceful shutdown, state transitions, worktree
// cleanup), whereas a raw kill leaves cleanup to the next reconcile.
func (s *Scheduler) CancelTask(taskID int64) bool {
	s.mu.Lock()
	cancel, ok := s.inflight[taskID]
	s.mu.Unlock()
	if ok {
		cancel(&runner.ErrCancelled{})
	}
	return ok
}

func (s *Scheduler) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.inflight {
		cancel(nil)
	}
}

func tokenFreshness(cfg config.Config) string {
	b, err := usage.BundleForAccount(cfg.ClaudeConfigDir, cfg.SandboxKeychain, cfg.SandboxKeychainPasswordFile)
	if err != nil {
		return "error"
	}
	exp := usage.ExpiresTime(b)
	if exp.IsZero() {
		return "valid (no expiry info)"
	}
	remaining := time.Until(exp)
	if remaining <= 0 {
		return "expired (will refresh on next use)"
	}
	m := int(remaining.Minutes())
	if m >= 60 {
		return fmt.Sprintf("valid %dh%dm", m/60, m%60)
	}
	return fmt.Sprintf("valid %dm", m)
}
