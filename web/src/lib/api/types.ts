// Task statuses observed in the frontend
export type TaskStatus =
  | "queued"
  | "running"
  | "resumable"
  | "paused"
  | "done"
  | "dismissed"
  | "failed"
  | "backlog"
  | "pr_created"
  | "awaiting_human";

// Run statuses observed in the frontend
export type RunStatus =
  | "starting"
  | "running"
  | "resuming"
  | "succeeded"
  | "failed"
  | "errored"
  | "rate_limited"
  | "timed_out"
  | "abandoned";

// --- Domain models ---

// One repo's outcome for a task. A task can span several repos, each with its
// own branch and PR.
export interface TaskPR {
  id: number;
  task_id: number;
  run_id: number;
  repo: string;
  branch: string;
  pr_url: string;
  worked_in: string;
  created_at: string;
  lines_added: number;
  lines_removed: number;
  /** gh's PR state: "OPEN" | "MERGED" | "CLOSED", or "" before the first probe. */
  pr_state: string;
  pr_is_draft: boolean;
  pr_checked_at: string;
}

// One repo's outcome from POST /api/tasks/{id}/checkout.
export interface CheckoutResult {
  repo: string;
  branch: string;
  path: string;
  status: "checked_out" | "already" | "skipped" | "error";
  detail: string;
}

export interface Task {
  id: number;
  display_id: string;
  title: string;
  prompt: string;
  repo_path: string;
  model: string;
  summary: string;
  status: TaskStatus;
  has_session: boolean;
  latest_run_status: RunStatus | "";
  latest_run_pr_url: string;
  prs?: TaskPR[];
  /**
   * Newest run id that no longer counts toward the task's attempt cap, moved
   * forward whenever a user edits, re-queues, or comments on the task.
   */
  attempt_reset_run_id: number;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export interface Run {
  id: number;
  task_id: number;
  session_id: string;
  worktree_path: string;
  status: RunStatus;
  attempt: number;
  cost_usd: number;
  num_turns: number;
  started_at: string;
  ended_at: string;
  pr_url: string;
  branch: string;
  agent_repo: string;
  model: string;
  lines_added: number;
  lines_removed: number;
}

/** GET /api/runs/{id}/resume. `command` is empty when the run has no session. */
export interface RunResume {
  run_id: number;
  session_id: string;
  worktree_path: string;
  claude_config_dir: string;
  command: string;
}

export interface Comment {
  id: number;
  task_id: number;
  author: string;
  body: string;
  consumed_by_run: number;
  created_at: string;
}

export interface Attachment {
  id: number;
  task_id: number;
  filename: string;
  content_type: string;
  created_at: string;
}

// --- Usage / Status ---

export interface ScopedWeeklyEntry {
  model: string;
  percent: number;
}

/**
 * Mirrors Go `domain.UsageSnapshot`. Both `GET /api/usage` and the SSE `usage`
 * event carry exactly this shape — the store replaces the object wholesale, so
 * a second shape on either path silently blanks whatever it omits.
 */
export interface UsageSnapshot {
  five_hour_util: number;
  seven_day_util: number;
  five_hour_resets_at: string;
  seven_day_resets_at: string;
  captured_at?: string;
  scoped_weekly?: ScopedWeeklyEntry[];
}

export interface RunningRun {
  run_id: number;
  task_id: number;
  title: string;
  attempt: number;
  started_at: string;
}

/** What the scheduler will do about a task. Mirrors scheduling.Action. */
export type ForecastAction = "launch" | "wait" | "suspend" | "fail";

/**
 * Stable reason codes from the scheduler. Mirrors scheduling.ReasonCode — switch
 * on these, and display `reason` (rendered server-side) rather than the code.
 */
export type ReasonCode =
  | "ready"
  | "running"
  | "workers_busy"
  | "session_exhausted"
  | "weekly_exhausted"
  | "weekly_backoff"
  | "attempt_cap"
  | "no_usage_data"
  | "stale_usage_data"
  | "queue_empty";

/**
 * One task's scheduling verdict. There is no ETA: burnrate models no task
 * duration, so a predicted start time would be invented.
 */
export interface ForecastEntry {
  task_id: number;
  action: ForecastAction;
  reason_code: ReasonCode;
  reason: string;
}

export interface BatchInfo {
  parallel_n: number;
  running_count: number;
  util_threshold: number;
  current_util: number;
  launched_this_window: number;
  window_cost: number;
  max_attempts: number;
  run_timeout_min: number;
  model: string;
}

export interface UsageLimit {
  kind: string;
  group: string;
  percent: number;
  severity: string;
  is_active: boolean;
  resets_at: string;
  scope_model: string;
}

export interface StatusInfo {
  window_state: string;
  running_count: number;
  resets_at: string;
  seven_day_resets_at?: string;
  blocked_reason: string;
  blocked_reason_code?: ReasonCode;
  next_candidate: string;
  running_runs: RunningRun[];
  forecast: ForecastEntry[];
  // Omitted by the server until the first usage fetch succeeds.
  batch?: BatchInfo;
  limits?: UsageLimit[];
  rate_limit_until?: string;
  rate_limit_attempt?: number;
  /**
   * Pending human requests across all tasks. Present on `GET /api/status`;
   * optional because the SSE `status` event is serialized from the scheduler's
   * own struct and does not always carry it — treat a missing value as
   * "unknown", not as zero.
   */
  pending_request_count?: number;
}

export interface CaffeinateStatus {
  active: boolean;
  uptime: string;
  reason: string;
  manual: boolean;
}

export interface Account {
  config_dir: string;
  label: string;
  keychain_suffix: string;
  has_sandbox_keychain: boolean;
  has_credentials_file: boolean;
  active: boolean;
}

export interface AccountsResponse {
  accounts: Account[];
}

export type Config = Record<string, string | number | boolean>;

// --- Request types ---

// repo_path is legacy: new tasks are agent-directed and name their repos in the
// prompt. Omitting it on update leaves an existing task's value untouched.
export interface CreateTaskRequest {
  title: string;
  prompt: string;
  repo_path?: string;
  model?: string;
  status?: TaskStatus;
}

export interface UpdateTaskRequest {
  title: string;
  prompt: string;
  repo_path?: string;
  model?: string;
}

export interface ModelInfo {
  id: string;
  name: string;
}

export interface ChangeStatusRequest {
  status: TaskStatus;
}

export interface ReorderTasksRequest {
  ordered_ids: number[];
}

export interface CreateCommentRequest {
  body: string;
}

export interface SelectAccountRequest {
  config_dir: string;
}

// --- Log events ---

export type LogEventType =
  | "init"
  | "assistant_text"
  | "tool_use"
  | "tool_result"
  | "result"
  | "rate_limit";

export interface LogEvent {
  type: LogEventType;
  // init
  session_id?: string;
  model?: string;
  // assistant_text
  text?: string;
  // tool_use
  tool_name?: string;
  input_summary?: string;
  input_full?: unknown;
  // tool_result
  output?: string;
  // result
  cost_usd?: number;
  num_turns?: number;
  duration?: number;
  is_error?: boolean;
  // rate_limit
  message?: string;
  // fallback
  raw?: string;
}

// --- Leaderboard ---

export interface FastestBurnEntry {
  rank: number;
  started_at: string;
  reached_at: string;
  duration_s: number;
  start_util: number;
  peak_util: number;
  is_today?: boolean;
}

export interface HighestDailyEntry {
  rank: number;
  date: string;
  peak_spend: number;
  is_today?: boolean;
}

export interface MaxBurnRateEntry {
  rank: number;
  date: string;
  task_id: number;
  cost_usd: number;
  rate_per_h: number;
  is_today?: boolean;
}

export interface MostTasksDailyEntry {
  rank: number;
  date: string;
  count: number;
  is_today?: boolean;
}

export interface LeaderboardData {
  fastest_burns: FastestBurnEntry[];
  highest_daily_spend: HighestDailyEntry[];
  max_burn_rates: MaxBurnRateEntry[];
  most_tasks_daily: MostTasksDailyEntry[];
  today_fastest_burn?: FastestBurnEntry;
  today_daily_spend?: HighestDailyEntry;
  today_max_burn_rate?: MaxBurnRateEntry;
  today_most_tasks?: MostTasksDailyEntry;
}

/**
 * Activity streaks plus lifetime totals over every recorded run. A streak
 * counts UTC days with at least one run started; the current streak is still
 * alive when its last active day is yesterday.
 */
export interface StreakData {
  current_streak: number;
  longest_streak: number;
  longest_start?: string; // YYYY-MM-DD, UTC
  longest_end?: string; // YYYY-MM-DD, UTC
  active_days: number;
  first_day?: string; // YYYY-MM-DD, UTC
  total_runs: number;
  total_tasks: number;
  total_cost_usd: number;
  lines_added: number;
  lines_removed: number;
}

/**
 * One (day, model) bucket of delivered work. `cost_per_task` and `cost_per_line`
 * are ratios over the whole bucket, and are 0 when their denominator is 0 —
 * which the chart draws as a gap rather than as a point on the axis.
 */
export interface CostEfficiencyPoint {
  date: string; // YYYY-MM-DD, UTC; "" on a totals row
  model: string;
  runs: number;
  tasks: number;
  cost_usd: number;
  lines_added: number;
  lines_removed: number;
  lines_changed: number;
  cost_per_task: number;
  cost_per_line: number;
}

export interface CostEfficiency {
  days: number;
  /**
   * Series order, computed server-side over all history rather than over the
   * requested range, so the palette slot a model gets does not change when the
   * range filter does.
   */
  models: string[];
  points: CostEfficiencyPoint[];
  totals: CostEfficiencyPoint[];
}

// --- Task stats ---

export interface TaskStats {
  task_id: number;
  runs: number;
  cost_usd: number;
  lines_added: number;
  lines_removed: number;
  duration_sec: number;
  model: string;
}

// --- Human request types ---

export type RequestKind = "question" | "demo" | "capture_approval";
export type RequestStatus = "pending" | "answered" | "denied" | "expired" | "canceled";
/** Verdict a human attaches when responding to a `demo` request. */
export type RequestResult = "pass" | "fail" | "blocked";

/**
 * A request for human input. `body` is markdown, except for `demo`, where it is
 * a JSON brief — parse it with `lib/human-requests.parseDemoBody`.
 *
 * Fields Go emits with `omitempty` are optional here: they arrive `undefined`,
 * not zero-valued.
 */
export interface HumanRequest {
  id: number;
  task_id: number;
  run_id: number;
  kind: RequestKind;
  title: string;
  body: string;
  status: RequestStatus;
  /** A long poll is currently attached — answering unblocks an agent right now. */
  live: boolean;
  created_at: string;
  answered_at: string;
  response_comment_id?: number;
}

// --- Capture types ---

export type CaptureInitiator = "human" | "agent";
export type CaptureMode = "screenshot" | "video";
export type CaptureStatus = "processing" | "ready" | "failed";

/**
 * Fields Go emits with `omitempty` are optional here. `keyframes` in particular
 * is absent until processing finishes — code that maps over it must guard.
 */
export interface Capture {
  id: number;
  task_id: number;
  request_id?: number;
  initiator: CaptureInitiator;
  target_desc: string;
  mode: CaptureMode;
  status: CaptureStatus;
  video_path?: string;
  transcript?: string;
  notes?: string;
  duration_sec: number;
  created_at: string;
  keyframes?: string[];
}

// --- Achievements ---

export interface Achievement {
  id: string;
  name: string;
  description: string;
  icon: string;
  unlocked: boolean;
  unlocked_at?: string;
  progress?: number;
  progress_text?: string;
}

export interface AchievementsData {
  achievements: Achievement[];
  unlocked: number;
  total: number;
}

// --- SSE event types ---

export type SSEEventType = "usage" | "status" | "caffeinate" | "task" | "run" | "request";
