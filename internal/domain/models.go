package domain

import (
	"encoding/json"
	"time"
)

type TaskStatus string

const (
	TaskStatusQueued        TaskStatus = "queued"
	TaskStatusRunning       TaskStatus = "running"
	TaskStatusResumable     TaskStatus = "resumable"
	TaskStatusDone          TaskStatus = "done"
	TaskStatusFailed        TaskStatus = "failed"
	TaskStatusPaused        TaskStatus = "paused"
	TaskStatusDismissed     TaskStatus = "dismissed"
	TaskStatusBacklog       TaskStatus = "backlog"
	TaskStatusPRCreated     TaskStatus = "pr_created"
	TaskStatusAwaitingHuman TaskStatus = "awaiting_human"
)

type RequestKind string

const (
	RequestKindQuestion        RequestKind = "question"
	RequestKindDemo            RequestKind = "demo"
	RequestKindCaptureApproval RequestKind = "capture_approval"
)

type RequestStatus string

const (
	RequestStatusPending  RequestStatus = "pending"
	RequestStatusAnswered RequestStatus = "answered"
	RequestStatusDenied   RequestStatus = "denied"
	RequestStatusExpired  RequestStatus = "expired"
	RequestStatusCanceled RequestStatus = "canceled"
)

type HumanRequest struct {
	ID                int64  `json:"id"`
	TaskID            int64  `json:"task_id"`
	RunID             int64  `json:"run_id"`
	Kind              string `json:"kind"`
	Title             string `json:"title"`
	Body              string `json:"body"`
	Status            string `json:"status"`
	Live              bool   `json:"live"`
	CreatedAt         string `json:"created_at"`
	AnsweredAt        string `json:"answered_at"`
	ResponseCommentID int64  `json:"response_comment_id,omitempty"`
}

type CaptureInitiator string

const (
	CaptureInitiatorHuman CaptureInitiator = "human"
	CaptureInitiatorAgent CaptureInitiator = "agent"
)

type CaptureMode string

const (
	CaptureScreenshot CaptureMode = "screenshot"
	CaptureVideo      CaptureMode = "video"
)

type CaptureStatus string

const (
	CaptureStatusProcessing CaptureStatus = "processing"
	CaptureStatusReady      CaptureStatus = "ready"
	CaptureStatusFailed     CaptureStatus = "failed"
)

type Capture struct {
	ID          int64    `json:"id"`
	TaskID      int64    `json:"task_id"`
	RequestID   int64    `json:"request_id,omitempty"`
	Initiator   string   `json:"initiator"`
	TargetDesc  string   `json:"target_desc"`
	Mode        string   `json:"mode"`
	Status      string   `json:"status"`
	VideoPath   string   `json:"video_path,omitempty"`
	Transcript  string   `json:"transcript,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	DurationSec float64  `json:"duration_sec"`
	CreatedAt   string   `json:"created_at"`
	Keyframes   []string `json:"keyframes,omitempty"`
}

type RunStatus string

const (
	RunStatusStarting    RunStatus = "starting"
	RunStatusRunning     RunStatus = "running"
	RunStatusResuming    RunStatus = "resuming"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusErrored     RunStatus = "errored"
	RunStatusRateLimited RunStatus = "rate_limited"
	RunStatusTimedOut    RunStatus = "timed_out"
	RunStatusAbandoned   RunStatus = "abandoned"
)

type Task struct {
	ID        int64   `json:"id"`
	DisplayID string  `json:"display_id"`
	Title     string  `json:"title"`
	Prompt    string  `json:"prompt"`
	RepoPath  string  `json:"repo_path"`
	Size      string  `json:"size"`
	Model     string  `json:"model"`
	SortOrder float64 `json:"sort_order"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`

	// AttemptResetRunID is the newest run id that no longer counts toward the
	// task's attempt cap, set when a user edits the task or re-queues it by
	// hand. See EffectiveAttempt and migration 012.
	AttemptResetRunID int64 `json:"attempt_reset_run_id"`

	Summary string `json:"summary,omitempty"`

	LatestRunStatus string `json:"latest_run_status,omitempty"`
	LatestRunPRURL  string `json:"latest_run_pr_url,omitempty"`
	HasSession      bool   `json:"has_session"`
	// PRs holds every PR the task produced, across every repo it touched. A
	// single task may span multiple repos, so LatestRunPRURL is only the head
	// of this list, kept for older clients.
	PRs []TaskPR `json:"prs,omitempty"`
}

// TaskPR is one repo's outcome for a task: the branch that was pushed and the
// PR opened for it.
type TaskPR struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	RunID     int64  `json:"run_id"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	PRURL     string `json:"pr_url"`
	WorkedIn  string `json:"worked_in"`
	CreatedAt string `json:"created_at"`
	// LinesAdded/LinesRemoved are the branch's cumulative diff against its
	// merge-base with the default branch, re-measured on every run that touches
	// it. See migration 011 for why the cumulative total lives here and the
	// per-run delta lives on the run.
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
	// PRState is gh's own vocabulary — OPEN, MERGED, CLOSED — plus the local
	// PRStateGone marker, or "" when the PR has never been probed. PRIsDraft is
	// orthogonal to it and only meaningful while the PR is OPEN.
	PRState     string `json:"pr_state"`
	PRIsDraft   bool   `json:"pr_is_draft"`
	PRCheckedAt string `json:"pr_checked_at"`
	// PRProbeFailures counts consecutive probes that failed without reaching a
	// verdict. Any answer resets it to 0. The prober backs off exponentially on
	// it, so a URL nobody can read costs a call a day rather than one per sweep.
	PRProbeFailures int `json:"pr_probe_failures"`
}

// PRStateGone is the one PRState value gh never reports: the daemon writes it
// when GitHub says the URL is not a PR at all. Its whole job is to be
// distinguishable from "" (never probed) — without it a dead URL reads as
// unprobed and costs a gh call on every sweep, forever. The web UI needs no case
// for it; it falls through to the same "unknown" chip as an unprobed PR.
const PRStateGone = "GONE"

// Terminal reports whether GitHub has settled the PR's fate, so the prober can
// stop spending a gh call on it. PRStateGone is deliberately not terminal here —
// it is the daemon's own conclusion rather than GitHub's, so an explicit refresh
// is still allowed to challenge it. See prstatus.shouldProbe.
func (p TaskPR) Terminal() bool {
	return p.PRState == "MERGED" || p.PRState == "CLOSED"
}

type Run struct {
	ID               int64   `json:"id"`
	TaskID           int64   `json:"task_id"`
	SessionID        string  `json:"session_id"`
	WorktreePath     string  `json:"worktree_path"`
	Branch           string  `json:"branch"`
	RepoPath         string  `json:"repo_path"`
	PID              int     `json:"pid"`
	Status           string  `json:"status"`
	Attempt          int     `json:"attempt"`
	CostUSD          float64 `json:"cost_usd"`
	NumTurns         int     `json:"num_turns"`
	PRURL            string  `json:"pr_url"`
	Error            string  `json:"error"`
	WindowID         string  `json:"window_id"`
	RateLimitResetAt string  `json:"rate_limit_reset_at"`
	StartedAt        string  `json:"started_at"`
	EndedAt          string  `json:"ended_at"`
	AgentRepo        string  `json:"agent_repo"`
	AgentWorkedIn    string  `json:"agent_worked_in"`
	ResultText       string  `json:"result_text,omitempty"`
	// Model is the Claude model that produced the run, read from the stream's
	// system/init event and falling back to the configured model. Empty for runs
	// that finished before migration 011.
	Model string `json:"model"`
	// LinesAdded/LinesRemoved are this run's *contribution* to its branches, not
	// the branch totals: a followup run measuring the same branch again records
	// only the growth since the last measurement, so summing runs is safe.
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

// LinesChanged is the churn a run produced, the denominator of cost per line.
func (r Run) LinesChanged() int { return r.LinesAdded + r.LinesRemoved }

// EffectiveAttempt reports how many attempts prior counts for against a task's
// max_attempts cap. It is prior.Attempt, except that a run recorded at or before
// the task's last manual reset counts for nothing — the user edited the task or
// put it back in the queue, so the next launch starts the count over at 1.
//
// This is the single definition of "how far through its attempts is this task",
// shared by the scheduler's launch gate (scheduler.plan) and by the two places
// the runner numbers a new run (runner.Run for a resume, runner.preflightError
// for a run that never got off the ground). Run rows keep the attempt number
// they actually ran at, so history stays readable; only the count forward moves.
func EffectiveAttempt(attemptResetRunID int64, prior *Run) int {
	if prior == nil || prior.ID <= attemptResetRunID {
		return 0
	}
	return prior.Attempt
}

type Comment struct {
	ID            int64  `json:"id"`
	TaskID        int64  `json:"task_id"`
	Author        string `json:"author"`
	Body          string `json:"body"`
	CreatedAt     string `json:"created_at"`
	ConsumedByRun int64  `json:"consumed_by_run"`
}

type Attachment struct {
	ID          int64  `json:"id"`
	TaskID      int64  `json:"task_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

type ScopedWeekly struct {
	Model   string  `json:"model"`
	Percent float64 `json:"percent"`
}

type UsageSnapshot struct {
	CapturedAt       string         `json:"captured_at"`
	FiveHourUtil     float64        `json:"five_hour_util"`
	FiveHourResetsAt string         `json:"five_hour_resets_at"`
	SevenDayUtil     float64        `json:"seven_day_util"`
	SevenDayResetsAt string         `json:"seven_day_resets_at"`
	SevenDayOpusUtil *float64       `json:"seven_day_opus_util,omitempty"`
	ScopedWeekly     []ScopedWeekly `json:"scoped_weekly,omitempty"`
	RawJSON          string         `json:"raw_json"`
}

type WindowAggregate struct {
	Count   int     `json:"count"`
	CostSum float64 `json:"cost_sum"`
}

type SizeEstimate struct {
	BudgetUSD  int           `json:"budget_usd"`
	MaxTimeout time.Duration `json:"max_timeout"`
}

type Config struct {
	ParallelN         int
	UtilThreshold     float64
	SevenDayThreshold float64
	Model             string
	PollIntervalSec   int
	MaxAttempts       int
	MaxAutoContinue   int
	BaseCodeDir       string
	WorktreeRoot      string
	Port              int
	UsageURL          string
	DryRun            bool
	DataDir           string
	SizeEstimates     map[string]SizeEstimate

	ClaudeConfigDir             string
	SandboxKeychain             string
	SandboxKeychainPasswordFile string
}

type StatusInfo struct {
	WindowState       string          `json:"window_state"`
	FiveHourUtil      float64         `json:"five_hour_util"`
	SevenDayUtil      float64         `json:"seven_day_util"`
	ResetsAt          time.Time       `json:"resets_at"`
	CapturedAt        *time.Time      `json:"captured_at,omitempty"`
	RunningCount      int             `json:"running_count"`
	RunningRuns       []RunSummary    `json:"running_runs"`
	NextCandidate     string          `json:"next_candidate,omitempty"`
	BlockedReason     string          `json:"blocked_reason,omitempty"`
	BlockedReasonCode string          `json:"blocked_reason_code,omitempty"`
	BackoffUntil      *time.Time      `json:"backoff_until,omitempty"`
	Forecast          []ForecastEntry `json:"forecast"`
	Batch             *BatchInfo      `json:"batch,omitempty"`
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

// ForecastEntry mirrors scheduler.ForecastEntry: a task's scheduling verdict and
// the reason behind it. There is no ETA — no task duration is modelled, so a
// predicted start time would be invented.
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

// UsageWindow mirrors usage.Window for domain-level usage data.
type UsageWindow struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// UsageLimit mirrors usage.Limit for domain-level rate limit info.
type UsageLimit struct {
	Kind       string  `json:"kind"`
	Group      string  `json:"group"`
	Percent    float64 `json:"percent"`
	Severity   string  `json:"severity"`
	IsActive   bool    `json:"is_active"`
	ResetsAt   string  `json:"resets_at"`
	ScopeModel string  `json:"scope_model,omitempty"`
}

type FastestBurnEntry struct {
	Rank      int     `json:"rank"`
	StartedAt string  `json:"started_at"`
	ReachedAt string  `json:"reached_at"`
	DurationS int     `json:"duration_s"`
	StartUtil float64 `json:"start_util"`
	PeakUtil  float64 `json:"peak_util"`
	IsToday   bool    `json:"is_today,omitempty"`
}

type HighestDailyEntry struct {
	Rank      int     `json:"rank"`
	Date      string  `json:"date"`
	PeakSpend float64 `json:"peak_spend"`
	IsToday   bool    `json:"is_today,omitempty"`
}

type MaxBurnRateEntry struct {
	Rank     int     `json:"rank"`
	Date     string  `json:"date"`
	TaskID   int64   `json:"task_id"`
	CostUSD  float64 `json:"cost_usd"`
	RatePerH float64 `json:"rate_per_h"`
	IsToday  bool    `json:"is_today,omitempty"`
}

type MostTasksDailyEntry struct {
	Rank    int    `json:"rank"`
	Date    string `json:"date"`
	Count   int    `json:"count"`
	IsToday bool   `json:"is_today,omitempty"`
}

type LeaderboardData struct {
	FastestBurns      []FastestBurnEntry    `json:"fastest_burns"`
	HighestDailySpend []HighestDailyEntry   `json:"highest_daily_spend"`
	MaxBurnRates      []MaxBurnRateEntry    `json:"max_burn_rates"`
	MostTasksDaily    []MostTasksDailyEntry `json:"most_tasks_daily"`
	TodayFastestBurn  *FastestBurnEntry     `json:"today_fastest_burn,omitempty"`
	TodayDailySpend   *HighestDailyEntry    `json:"today_daily_spend,omitempty"`
	TodayMaxBurnRate  *MaxBurnRateEntry     `json:"today_max_burn_rate,omitempty"`
	TodayMostTasks    *MostTasksDailyEntry  `json:"today_most_tasks,omitempty"`
}

// StreakData is the activity summary for the Usage page: consecutive-day run
// streaks plus lifetime totals over every recorded run.
//
// A streak counts UTC days on which at least one run started — a failed
// attempt is still activity. The current streak is still alive when its last
// active day is yesterday, so a quiet morning doesn't zero a streak the user
// can extend later today.
type StreakData struct {
	CurrentStreak int     `json:"current_streak"`
	LongestStreak int     `json:"longest_streak"`
	LongestStart  string  `json:"longest_start,omitempty"` // YYYY-MM-DD, UTC
	LongestEnd    string  `json:"longest_end,omitempty"`   // YYYY-MM-DD, UTC
	ActiveDays    int     `json:"active_days"`
	FirstDay      string  `json:"first_day,omitempty"` // YYYY-MM-DD, UTC
	TotalRuns     int     `json:"total_runs"`
	TotalTasks    int     `json:"total_tasks"` // distinct tasks ever run
	TotalCostUSD  float64 `json:"total_cost_usd"`
	LinesAdded    int     `json:"lines_added"`
	LinesRemoved  int     `json:"lines_removed"`
}

type Achievement struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Icon         string  `json:"icon"`
	Unlocked     bool    `json:"unlocked"`
	UnlockedAt   string  `json:"unlocked_at,omitempty"`
	Progress     float64 `json:"progress,omitempty"`
	ProgressText string  `json:"progress_text,omitempty"`
}

type AchievementsData struct {
	Achievements []Achievement `json:"achievements"`
	Unlocked     int           `json:"unlocked"`
	Total        int           `json:"total"`
}

// CostEfficiencyPoint is one (day, model) bucket of delivered work: what was
// spent, how many tasks and lines it bought, and the two derived unit costs.
//
// CostPerTask and CostPerLine are ratios over the whole bucket, not averages of
// per-run ratios, so a run that spent money without landing lines (a rate-limited
// attempt, say) correctly raises the bucket's unit cost rather than being dropped.
// Both are zero when their denominator is zero, which the UI renders as a gap in
// the line rather than as a data point at zero.
type CostEfficiencyPoint struct {
	Date         string  `json:"date"`  // YYYY-MM-DD, UTC
	Model        string  `json:"model"` // UnknownModel for runs recorded before models were tracked
	Runs         int     `json:"runs"`
	Tasks        int     `json:"tasks"` // distinct tasks worked that day on that model
	CostUSD      float64 `json:"cost_usd"`
	LinesAdded   int     `json:"lines_added"`
	LinesRemoved int     `json:"lines_removed"`
	LinesChanged int     `json:"lines_changed"` // added + removed
	CostPerTask  float64 `json:"cost_per_task"`
	CostPerLine  float64 `json:"cost_per_line"`
}

// CostEfficiency is the cost-per-task / cost-per-line series for the Usage page.
//
// Models is the chart's series order and is deliberately computed over *all*
// history rather than over the requested range: the frontend assigns a palette
// slot by index, and narrowing the range must not repaint the models that
// survive the filter.
type CostEfficiency struct {
	Days   int                   `json:"days"`
	Models []string              `json:"models"`
	Points []CostEfficiencyPoint `json:"points"`
	Totals []CostEfficiencyPoint `json:"totals"` // one per model, over the range; Date is ""
}

// TaskStats aggregates a task's runs into the numbers a user actually wants:
// what it cost, how much code it produced, and how long it took.
type TaskStats struct {
	TaskID       int64   `json:"task_id"`
	Runs         int     `json:"runs"`
	CostUSD      float64 `json:"cost_usd"`
	LinesAdded   int     `json:"lines_added"`
	LinesRemoved int     `json:"lines_removed"`
	DurationSec  int     `json:"duration_sec"`
	Model        string  `json:"model"`
}

// UnknownModel labels work whose model was not recorded — every run that
// finished before migration 011 added the column.
const UnknownModel = "unknown"

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var AvailableModels = []ModelInfo{
	{ID: "claude-opus-5", Name: "Opus 5"},
	{ID: "claude-opus-4-6", Name: "Opus 4.6"},
	{ID: "claude-sonnet-5", Name: "Sonnet 5"},
	{ID: "claude-sonnet-4-6", Name: "Sonnet 4.6"},
	{ID: "claude-fable-5", Name: "Fable 5"},
	{ID: "claude-haiku-4-5-20251001", Name: "Haiku 4.5"},
}

// LiveUsageSnapshot is the in-memory snapshot used by the scheduler, distinct
// from the persisted UsageSnapshot above.
type LiveUsageSnapshot struct {
	FiveHour     UsageWindow
	SevenDay     UsageWindow
	SevenDayOpus *UsageWindow
	ScopedWeekly []ScopedWeekly
	Limits       []UsageLimit
	Raw          json.RawMessage
	CapturedAt   time.Time
}
