# burnrate — codebase map

## Route Table

```
GET    /health                        handleHealth            -> {status:"ok"}
GET    /                              web.Handler()           -> embedded SPA

GET    /api/tasks                     handleListTasks         -> []Task
POST   /api/tasks                     handleCreateTask        <- {title,prompt,repo_path,size,model,status} -> Task (201)
GET    /api/tasks/stats               handleTaskStats         -> map[task_id]TaskStats (aggregated run data per task)
POST   /api/tasks/reorder             handleReorderTasks      <- {ordered_ids:[]int64} -> {status:"reordered"}
PUT    /api/tasks/{id}                handleUpdateTask        <- {title,prompt,repo_path,size,model} -> Task
DELETE /api/tasks/{id}                handleDeleteTask        -> {status:"deleted"}
POST   /api/tasks/{id}/pause          handlePauseTask         -> {status:"paused"}
POST   /api/tasks/{id}/resume         handleResumeTask        -> {status:<newStatus>}
POST   /api/tasks/{id}/complete       handleCompleteTask      -> {status:"done"}
POST   /api/tasks/{id}/dismiss        handleDismissTask       -> {status:"dismissed"}
POST   /api/tasks/{id}/status         handleSetTaskStatus     <- {status:string} -> {status:<finalStatus>}
POST   /api/tasks/{id}/run-now        handleRunNow            -> {status:"launched"}
POST   /api/tasks/{id}/checkout        handleCheckoutTask      -> []checkout.Result (per repo; partial failure still 200)
POST   /api/tasks/{id}/prs/refresh     handleRefreshTaskPRs    -> []TaskPR (probes gh for each PR's state)

GET    /api/tasks/{id}/comments       handleListComments      -> []Comment
POST   /api/tasks/{id}/comments       handleAddComment        <- {body:string} -> Comment (201)

GET    /api/tasks/{id}/attachments    handleListAttachments   -> []Attachment
POST   /api/tasks/{id}/attachments    handleUploadAttachment  <- multipart/form-data (max 10MB, images) -> Attachment (201)
GET    /api/attachments/{id}/data     handleServeAttachment   -> image binary
DELETE /api/attachments/{id}          handleDeleteAttachment  -> {status:"deleted"}

GET    /api/runs                      handleListRuns          ?task_id=&limit= -> []Run
GET    /api/runs/{id}/log             handleRunLog            -> text/plain (last 512KB)
GET    /api/runs/{id}/events          handleRunEvents         -> []logEvent
GET    /api/runs/{id}/resume          handleRunResume         -> service.RunResume {run_id,session_id,
                                                                 worktree_path,claude_config_dir,command};
                                                                 command "" when no session
POST   /api/runs/{id}/cancel          handleCancelRun         -> {status:"cancelling"|"cancelled"}

GET    /api/accounts                  handleListAccounts      -> {active_config_dir,accounts:[]}
POST   /api/accounts/select           handleSelectAccount     <- {config_dir:string} -> {status:"selected",config_dir}

GET    /api/models                    handleListModels        -> []ModelInfo {id,name}

GET    /api/usage                     handleUsage             -> UsageSnapshot
GET    /api/usage/history             handleUsageHistory      ?hours= (default 5, max 168) -> []UsageSnapshot
GET    /api/usage/leaderboard         handleLeaderboard       -> LeaderboardData (top-5 + today entry per category)
GET    /api/usage/cost-efficiency     handleCostEfficiency    ?days= (default 30, max 365) -> CostEfficiency
GET    /api/usage/streak              handleStreak            -> StreakData (day streaks + lifetime totals)
GET    /api/usage/achievements        handleAchievements      -> AchievementsData (unlockable badges computed from runs)
GET    /api/status                    handleStatus            -> StatusInfo + pending_request_count
                                                              (server.statusPayload — shared with the SSE `status` event)
GET    /api/config                    handleGetConfig         -> map[string]any
PUT    /api/config                    handlePutConfig         <- map[string]string -> {status:"updated"}

GET    /api/voice/status              handleVoiceStatus       -> {state:string, message?:string}; states: unknown|checking|ready|unavailable|installing|error
POST   /api/voice/install            handleVoiceInstall      -> 202 {state,message}; starts model download in background
POST   /api/voice/transcribe         handleTranscribe        <- multipart/form-data {audio} -> {text:string}; requires state=ready
POST   /api/voice/task               handleVoiceTask         <- {text:string} -> Task (201); uses whisper + claude -p

GET    /api/caffeinate                handleGetCaffeinate     -> CaffeinateStatus
POST   /api/caffeinate                handleToggleCaffeinate  -> {active:bool,status:CaffeinateStatus}

POST   /api/requests                  handleCreateRequest     <- {task_id,run_id,kind,title,body} -> HumanRequest (201)
GET    /api/requests                  handleListRequests      ?status= -> []HumanRequest (FIFO + live-first)
GET    /api/requests/{id}             handleGetRequest        -> HumanRequest
GET    /api/requests/{id}/await       handleAwaitRequest      ?timeout_sec= (default 55, max 600) -> HumanRequest (long-poll)
POST   /api/requests/{id}/respond     handleRespondRequest    <- {body,result?} -> Comment (bypasses 409 guard)
POST   /api/requests/{id}/approve     handleApproveRequest    <- {scope:"once"|"run"} -> {status:"approved"}
POST   /api/requests/{id}/deny        handleDenyRequest       -> {status:"denied"}
POST   /api/captures                  handleCreateCapture     <- {task_id,request_id?,initiator,target_desc,mode} -> Capture (201)
GET    /api/captures                  handleListCaptures      ?task_id= -> []Capture
GET    /api/captures/{id}             handleGetCapture        -> Capture (with keyframes[] populated)
POST   /api/captures/{id}/finish      handleFinishCapture     <- {video_path?,transcript?,duration_sec?} -> {status:"ready"}
POST   /api/captures/{id}/notes       handleSetCaptureNotes   <- {notes:string} -> {status:"updated"}

GET /mcp  POST /mcp                   mcp.Server              MCP streamable-HTTP ?task={id}&run={id}
                                                              (origin-guarded — see Key Invariants)

GET    /api/events                    handleEvents            -> SSE stream
```

## SSE Event Types

```
usage        UsageSnapshot           after each scheduler tick
status       statusInfo              after usage fetch, run complete/update, account change, caffeinate
                                     toggle, config write, any request mutation
task         []Task                  after any task mutation
run          []Run                   after run list changes
caffeinate   CaffeinateStatus        after run complete/update (auto), toggle
request      []HumanRequest          after request create/respond/approve/deny/cancel (pending only)
notification notify.Notification     macOS notification relay (PR ready, human request opened)
```

30s heartbeat. 64-event buffered channel per client.

Two wire contracts other apps build against:

- **`status`** is `server.statusInfo` — `scheduler.StatusInfo` plus `pending_request_count`.
  `GET /api/status` and every SSE `status` frame serialise the *same* builder
  (`Server.statusPayload`). They must never diverge: the tray and the web UI replace the
  object wholesale, so a frame missing the count blanks whatever the last poll set.
- **`notification`** is `{title, body, task_id?, request_id?}` (`notify.Notification`; the ids
  are `omitempty`, so a notification with nothing to link to is the plain `{title, body}` the
  desktop app always understood). Producers: `notify.RequestCreated` (a request opened, carries
  both ids), `notify.HumanRequest` (a run parked itself, task only), `notify.CaptureApproval`
  (unused today — capture is not implemented), `notify.Review` (PR ready, no ids: only the
  display id "BR42" reaches that call site). Note for consumers: `tauri-plugin-notification`
  v2 has **no desktop click-through**, so the ids are routing data for in-app surfaces —
  do not assume clicking a native notification can deep-link.

## Task State Machine

```
States: queued | running | resumable | pr_created | done | failed | paused | dismissed | backlog | awaiting_human

System-only targets: running (scheduler), pr_created (runner), awaiting_human (runner)

User-settable targets: queued, backlog, paused, done, dismissed, failed, resumable

Cannot transition FROM running (user-initiated)

Specific transitions:
  pause:    queued|resumable -> paused
  resume:   paused -> queued (or resumable if session exists + run was rate_limited|timed_out|errored)
  complete: !running -> done (cleans worktree)
  dismiss:  !running -> dismissed (cleans worktree)
  backlog->queued: auto-upgrades to resumable if session exists
  ->resumable: requires latest run with session_id

Runner outcomes (cleanup = tear the worktree down, safe because the work is in Git; an
agent workdir holding files no branch backs is kept instead — see Key Invariants):
  success           -> task=pr_created  run=succeeded     (cleanup)
  user_cancel       -> task=paused      run=errored       (cleanup)
  rate_limit+session -> task=resumable  run=rate_limited  (cleanup)
  rate_limit-session -> task=failed     run=rate_limited  (cleanup)
  timeout+session   -> task=resumable   run=timed_out     (cleanup)
  timeout-session   -> task=failed      run=timed_out     (cleanup)
  error+session     -> task=resumable   run=errored       (cleanup)
  error-session     -> task=failed      run=errored       (cleanup)
  waiting_human     -> task=awaiting_human run=succeeded  (cleanup; no attempt burned)
  waiting_human, but the run's requests are all already answered/denied/expired
                    -> task=queued|resumable run=succeeded  (cleanup; attempts reset, no park)
                       The human replied between the long-poll expiring and classify();
                       parking there would strand the task forever — see Key Invariants
  On resume, worktrees are recreated from the branch.
  A resume whose session the CLI cannot find ("No conversation found with session ID",
  claude.ErrSessionNotFound — usually the account/CLAUDE_CONFIG_DIR changed since the
  session was recorded) does NOT fail the run: runner clears the dead session_id and
  re-invokes once with no --resume. The prompt already carries prior-run context.
  A cancelled run's task must be manually resumed (paused, not resumable).
```

## DB Schema

```sql
-- tasks
id              INTEGER PRIMARY KEY AUTOINCREMENT
title           TEXT NOT NULL
prompt          TEXT NOT NULL
repo_path       TEXT NOT NULL DEFAULT ''
size            TEXT NOT NULL DEFAULT 'medium'  CHECK(IN small,medium,large)
model           TEXT NOT NULL DEFAULT ''       -- per-task model override; '' = use global config
sort_order      REAL NOT NULL DEFAULT 0
status          TEXT NOT NULL DEFAULT 'queued'  CHECK(IN queued,running,resumable,done,failed,paused,dismissed,backlog,pr_created,awaiting_human)
summary         TEXT NOT NULL DEFAULT ''       -- parsed from worker output's ## Summary section on run success
created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))

-- runs
id                  INTEGER PRIMARY KEY AUTOINCREMENT
task_id             INTEGER NOT NULL  FK->tasks(id)
session_id          TEXT NOT NULL DEFAULT ''
worktree_path       TEXT NOT NULL DEFAULT ''
branch              TEXT NOT NULL DEFAULT ''
repo_path           TEXT NOT NULL DEFAULT ''
pid                 INTEGER NOT NULL DEFAULT 0
status              TEXT NOT NULL DEFAULT 'starting'  CHECK(IN starting,running,succeeded,rate_limited,timed_out,errored,resuming,abandoned)
attempt             INTEGER NOT NULL DEFAULT 1
cost_usd            REAL NOT NULL DEFAULT 0
num_turns           INTEGER NOT NULL DEFAULT 0
pr_url              TEXT NOT NULL DEFAULT ''
error               TEXT NOT NULL DEFAULT ''
window_id           TEXT NOT NULL DEFAULT ''
rate_limit_reset_at TEXT NOT NULL DEFAULT ''
started_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
ended_at            TEXT NOT NULL DEFAULT ''
agent_repo          TEXT NOT NULL DEFAULT ''
agent_worked_in     TEXT NOT NULL DEFAULT ''

-- usage_snapshots
captured_at         TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
five_hour_util      REAL DEFAULT 0
five_hour_resets_at TEXT DEFAULT ''
seven_day_util      REAL DEFAULT 0
seven_day_resets_at TEXT DEFAULT ''
seven_day_opus_util REAL
raw_json            TEXT DEFAULT '{}'

-- settings
key                 TEXT PRIMARY KEY
value               TEXT DEFAULT ''

-- task_comments
id                  INTEGER PRIMARY KEY AUTOINCREMENT
task_id             INTEGER FK->tasks(id) ON DELETE CASCADE
body                TEXT NOT NULL
author              TEXT DEFAULT 'user'   -- 'user' | 'agent'
created_at          TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
consumed_by_run     INTEGER DEFAULT 0

-- task_attachments
id                  INTEGER PRIMARY KEY AUTOINCREMENT
task_id             INTEGER FK->tasks(id) ON DELETE CASCADE
filename            TEXT NOT NULL
content_type        TEXT DEFAULT ''
size_bytes          INTEGER DEFAULT 0
created_at          TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))

-- task_prs  (010, 011, 013)
id                  INTEGER PRIMARY KEY AUTOINCREMENT
task_id             INTEGER FK->tasks(id) ON DELETE CASCADE
run_id              INTEGER
repo                TEXT NOT NULL DEFAULT ''
branch              TEXT NOT NULL DEFAULT ''
pr_url              TEXT NOT NULL DEFAULT ''
worked_in           TEXT NOT NULL DEFAULT ''
created_at          TEXT
lines_added         INTEGER NOT NULL DEFAULT 0   -- branch cumulative; runs holds the delta
lines_removed       INTEGER NOT NULL DEFAULT 0
pr_state            TEXT NOT NULL DEFAULT ''     -- gh vocabulary: OPEN|MERGED|CLOSED, plus local GONE (404), '' = unprobed
pr_is_draft         INTEGER NOT NULL DEFAULT 0
pr_checked_at       TEXT NOT NULL DEFAULT ''
pr_probe_failures   INTEGER NOT NULL DEFAULT 0   -- consecutive unclassifiable probe failures; drives backoff, 0 on any answer
UNIQUE(task_id, repo, branch)

-- human_requests  (017)
id                  INTEGER PRIMARY KEY AUTOINCREMENT
task_id             INTEGER NOT NULL  FK->tasks(id) ON DELETE CASCADE
run_id              INTEGER NOT NULL DEFAULT 0
kind                TEXT NOT NULL  CHECK(IN question,demo,capture_approval)
title               TEXT NOT NULL DEFAULT ''
body                TEXT NOT NULL DEFAULT ''
status              TEXT NOT NULL DEFAULT 'pending'  CHECK(IN pending,answered,denied,expired,canceled)
                    -- all five reachable: answered (respond/approve), denied (explicit deny),
                    -- expired (RequestService.Expire — a spent approval wait budget),
                    -- canceled (task completed/dismissed)
live                INTEGER NOT NULL DEFAULT 0
created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
answered_at         TEXT NOT NULL DEFAULT ''
response_comment_id INTEGER NOT NULL DEFAULT 0

-- captures  (018)
id                  INTEGER PRIMARY KEY AUTOINCREMENT
task_id             INTEGER NOT NULL  FK->tasks(id) ON DELETE CASCADE
request_id          INTEGER NOT NULL DEFAULT 0
initiator           TEXT NOT NULL  CHECK(IN human,agent)
target_desc         TEXT NOT NULL DEFAULT ''
mode                TEXT NOT NULL DEFAULT 'screenshot'  CHECK(IN screenshot,video)
status              TEXT NOT NULL DEFAULT 'processing'  CHECK(IN processing,ready,failed)
video_path          TEXT NOT NULL DEFAULT ''
transcript          TEXT NOT NULL DEFAULT ''
notes               TEXT NOT NULL DEFAULT ''
duration_sec        REAL NOT NULL DEFAULT 0
created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))

-- schema_migrations
version             INTEGER PRIMARY KEY
```

Migrations: 001_initial -> 002_add_rate_limit_reset -> 003_add_dismissed_status -> 004_add_backlog_status -> 005_create_task_comments -> 006_add_agent_columns -> 007_add_pr_created_status -> 008_create_task_attachments -> 009_add_comment_author_and_run_result -> 010_create_task_prs -> 011_add_run_model_and_lines -> 012_add_task_attempt_reset -> 013_add_task_pr_state -> 014_add_task_model -> 015_add_task_summary -> 016_add_task_pr_probe_failures -> 017_add_human_requests -> 018_add_captures

## Config Keys

```
parallel_n              int       3               BURNRATE_PARALLEL_N           max concurrent runs
util_threshold          float64   100             BURNRATE_UTIL_THRESHOLD       5h util % cap (100=disabled)
sevenday_threshold      float64   95              BURNRATE_SEVENDAY_THRESHOLD   7-day util % backoff
model                   string    claude-opus-4-6 BURNRATE_MODEL                claude model
poll_interval_sec       int       5               BURNRATE_POLL_INTERVAL_SEC    scheduler poll interval
max_attempts            int       20              BURNRATE_MAX_ATTEMPTS         max run attempts per task (counts interruptions, not only failures)
max_auto_continue       int       3               BURNRATE_MAX_AUTO_CONTINUE    in-run continuations after an auto-denied tool call (0 disables)
base_code_dir           string    ~/code            BURNRATE_BASE_CODE_DIR      base code folder holding repos [ro]
data_dir                string    ~/.burnrate     BURNRATE_DATA_DIR             DB, logs, worktrees,
                                                                                agentwork, attachments
worktree_root           string    {dataDir}/worktrees BURNRATE_WORKTREE_ROOT    worktree directory
port                    int       9112            BURNRATE_PORT                 HTTP port [ro]
usage_url               string    anthropic-api   BURNRATE_USAGE_URL            usage API endpoint [ro]
dry_run                 bool      false           BURNRATE_DRYRUN               stub claude
claude_config_dir       string    ""              BURNRATE_CLAUDE_CONFIG_DIR    pinned account (DB wins over env)
sandbox_keychain        string    ""              BURNRATE_SANDBOX_KEYCHAIN     sandbox keychain path
sandbox_keychain_password_file string "" BURNRATE_SANDBOX_KEYCHAIN_PASSWORD_FILE keychain password

human_request_wait_sec  int       600             —                             wait budget for ask_human /
                                                                                request_demo / await_request. LIVE:
                                                                                read from settings on every tool call
                                                                                (service.RequestService). Agent-supplied
                                                                                wait_sec is clamped by service.ClampWait
                                                                                — floor 5s, ceiling = this value. There
                                                                                is no hardcoded 600 cap: configure 1200
                                                                                and you get 1200
agent_capture_auto_approve bool   false           —                             auto-approve agent screenshots (inert:
                                                                                capture is not implemented)
capture_approval_wait_sec int     120             —                             approval wait budget
                                                                                (RequestService.AwaitApproval). LIVE,
                                                                                clamped the same way. On expiry the
                                                                                request goes to `expired`, NOT `denied`
capture_retention_days  int       30              —                             video capture retention. Swept by
                                                                                internal/retention on daemon start and
                                                                                every 24h; <=0 disables pruning

DB-only keys (no env):
  notify_on_review      string    "true"          enable macOS notifications on PR creation
  size_small            JSON      {dur:15m,budget:5,max:30m,util:8}    small task estimates
  size_medium           JSON      {dur:40m,budget:15,max:75m,util:20}  medium task estimates
  size_large            JSON      {dur:90m,budget:25,max:150m,util:40} large task estimates

Precedence: defaults -> DB settings -> env vars
Exception: account keys (claude_config_dir, sandbox_keychain*) DB wins over env
[ro] = readable via GET /api/config, rejected by PUT (validConfigKeys / readOnlyConfigKeys in
       handlers_config.go). Everything else takes effect on the next launch without a restart:
       PUT /api/config re-runs config.Load and hands the result to Scheduler.SetConfig.
```

## CLI Subcommands

```
burnrate serve      runServe()     Foreground daemon: scheduler + HTTP server + web UI
burnrate status     runStatus()    Print live usage snapshot + queue summary
burnrate accounts   runAccounts()  List discovered Claude Code accounts
burnrate resume     runResume()    Print `claude --resume` command for a run id; no id lists every
                                   run that has a session. claude.ResumeCommand builds the string
burnrate token      runToken()     Print a fresh OAuth token for the pinned account (stdout only);
                                   exit 1 when no account is pinned. Substituted into resume commands
burnrate recover    runRecover()   Sweep unpushed branches -> draft PRs, tear down stale
                                   worktrees ({dataDir}/worktrees) and agent workdirs
                                   ({dataDir}/agentwork)
burnrate version    runVersion()   Print version (build-time Version var, default "dev")
```

## Key Invariants

- API trust boundary is the origin check in `internal/server/origin.go`, not the loopback
  bind: requests to `/api/*`, `/health` **and `/mcp` (+ `/mcp/*`)** carrying a cross-site
  `Origin` or `Sec-Fetch-Site` get 403, and CORS is reflected only for the Tauri webview
  origins and the daemon's own loopback origin. Requests with neither header are non-browser
  clients and pass (this is how the claude CLI reaches `/mcp`). See `SECURITY.md`.
  `/mcp` must stay guarded: a cross-origin `text/plain` POST is CORS-safelisted, so it is sent
  with no preflight and no `Origin` — `Sec-Fetch-Site` is the only thing standing between a
  visited web page and `tools/call`
- The per-run MCP config lives at `{dataDir}/mcp/task-N-run-M.json`, never in the run's
  workdir, and is `defer os.Remove`d when the run ends (`runner.writeMCPConfig` + its call
  site in `runner.Run`). In the workdir it made `git.RemoveAgentWorkdir`'s hasFiles() true
  forever — so agent workdirs were never cleaned — and got checkpoint-committed into user
  branches
- The MCP server entry must be `"type": "http"`. Empirically (claude CLI 2.1.220) a
  `streamableHttp` type — or an omitted/bogus one — is **silently skipped**: no error
  anywhere, the server simply never loads. `claude mcp list` cannot verify this; it ignores
  `--mcp-config` entirely. The check that works is booting a session against a probe server
  and watching for `initialize`
- `--permission-mode auto` does NOT cover MCP tools. Without a grant the CLI answers a
  human-loop call with "you haven't granted it yet" and `non_execution_kind: "user-rejected"`
  — exactly what `claude.DenialFrom` classifies as `ErrToolDenied`, so every call would be
  misread as the agent idling and auto-continued. `internal/claude/invoke.go` therefore passes
  `--allowedTools mcp__burnrate-human-loop__*` whenever an MCP config is set (verified: a tool
  covered only by the wildcard executed, with `permission_denials: []`). Both `--mcp-config`
  and `--allowedTools` are variadic, so everything appended after them must start with a dash
- An answered request returns the human's words **in-band**: `RequestService.AwaitResponse`
  loads the response comment and the MCP tool result carries `reply` (+ `result` when the
  human attached a verdict). The agent has no comment-reading tool, so the old "posted as
  comment #N, check the thread" was a dropped message
- Double-injection guard: a reply to a request with a **live** long-poll is marked consumed
  immediately (`store.MarkCommentConsumed`, one comment — not the sweeping
  `MarkCommentsConsumed`), because the long-poll is delivering it. A reply to a **parked**
  request stays unconsumed: prompt injection is its only delivery path. `consumed_by_run = 0`
  means unconsumed, so a delivery with no run to attribute is stored as `-1`
- Requests broadcast + notify from `RequestService.Create`, not from the HTTP handlers.
  MCP tool calls create requests too, and hanging that off the handlers made every
  agent-created request invisible to the UI and the tray
- Screen capture does not exist. `desktop/` implements none of it, so the `capture_screen`
  and `list_capture_targets` MCP tools return `isError: true` and create **no** approval
  request and **no** capture row — a human must never be interrupted to approve a capture
  that cannot run. The service + REST capture plumbing stays as the M2 substrate
- `scope: "run"` capture approvals are a real grant: an in-memory per-run map in
  `RequestService` (grants die with the daemon, which is correct — a grant is a statement
  about a live run). `Create` consults it and settles a covered approval without asking
- Single-instance enforcement via flock on `{dataDir}/daemon.lock`
- The desktop app never adopts a running daemon: every launch calls `reclaim_sidecar_slot`
  (`desktop/src-tauri/src/lib.rs`) to kill the port holder, the `{dataDir}/daemon.lock` holder,
  and any `<sidecar-binary> serve` orphan, then spawns its own and retries up to 3× before
  giving up with a notification. `data_dir()` there mirrors Go's `config.DataDir()`
  (BURNRATE_DATA_DIR, else `$HOME/.burnrate`)
- The desktop menu bar must be built from `tauri::menu::Menu::default` (plus extra submenus if
  needed). macOS has one menu bar per app, so a hand-rolled `.menu()` replaces the standard one
  wholesale and takes ⌘Q / ⌘W with it
- The daemon port reaches the UI by two different routes, and BURNRATE_PORT must keep working on
  both: the desktop shell injects `window.__BURNRATE_PORT__` (`initialization_script`) which
  `detectBaseUrl()` in `web/src/lib/api/client.ts` reads (default 9112), while `next dev` proxies
  /api + /health to `BURNRATE_API_ORIGIN` (`web/next.config.ts` rewrites, same default). The dev
  stack (`make dev`) sets both to DEV_PORT
- `make dev` / `dev-web` / `desktop-dev` run on DEV_PORT (9113) + DEV_DATA_DIR
  (`~/.burnrate-dev`), never the installed app's 9112 + `~/.burnrate`: same data dir means a
  flock fight, and the desktop app kills the lock holder on launch
- The Next dev server listens on **3113**, not Next's default 3000 (`web/package.json` →
<<<<<<< HEAD
  `next dev -p 3113`), which is what `make dev` / `web-dev` / `dev-web` and the PM2
  `burnrate-web` app all inherit. `make kill` frees 9112 / 9113 / 3113
=======
  `next dev -p 3113`), which every target that runs `npm run dev` (`make dev` / `web-dev` /
  `dev-web`) inherits — 3000 is taken by other stacks on dev machines
>>>>>>> origin/main
- SQLite max 1 open connection (serialized access)
- Running tasks cannot be deleted, completed, dismissed, or have status changed by users
- `pr_created` and `running` are never user-settable
- Resumable requires a session_id on the latest run
- Comments consumed only on success, not on launch (prevents silent drop on early failure)
- A successful run posts its full final response as an `agent`-authored comment; only
  non-agent comments are follow-up instructions (runner.userComments)
- Worktrees are removed after every run outcome (success, suspension, error) — work is
  checkpointed (commit+push) first; kept only when a checkpoint cannot save the work
  (commit/push fails, or HEAD is detached so a commit has nowhere to go). On resume, the
  worktree is recreated from the branch. This frees the branch for local checkout/testing.
- A branch is the only thing that licenses deletion. An agent workdir
  (`{dataDir}/agentwork/task-N`, repo-less tasks) is torn down only once detaching its
  checkpointed worktrees leaves nothing behind: any remaining file means the work has no
  backup anywhere, so `git.RemoveAgentWorkdir` keeps the directory and returns
  `git.ErrUnbackedWork` (an expected outcome, logged at info, not a failure). The path is
  stable per task, so the next iteration lands on those files — and is told so by the prompt's
  Prior Run Context, which only promises branches/PRs that a `task_prs` row actually has
- Cleanup on task completion is fire-and-forget and may decline; `burnrate recover` is the only
  thing that retries it
- The scheduler owns the live config: `Scheduler.Config()` is what a launch reads, and
  `PUT /api/config` refreshes it via `SetConfig`. `Server.cfg` is a startup snapshot used only
  for DataDir/Port and as GET defaults. `SetConfig` never touches the three account fields —
  `SetAccount` owns those
- PR state is cached, not live: `internal/prstatus` sweeps **open items only** in `task_prs`
  through `gh pr view --json state,isDraft` (addressed by URL, so no checkout is needed).
  MERGED/CLOSED are terminal and never re-probed. A failure is classified, never allowed to
  clear the cache: `git.ErrPRNotFound` (GitHub 404) is a verdict and is stored as
  `pr_state = 'GONE'`, which the sweep skips forever; anything else (no auth, no network, rate
  limit, unresolvable repo — including a private repo GitHub hides) keeps the last known state
  and increments `pr_probe_failures`, and the sweep then waits `MinAge << failures` capped at
  24h. An explicit `RefreshTask` (the UI refresh button) overrides MinAge, the backoff and GONE,
  but not MERGED/CLOSED — it is the only way a GONE row comes back
- Agent-directed runs produce structured output (## Summary, ## Changes, ## Verification, ## Documentation, ## Worktree Bootstrap) parsed by `internal/runner/output.go:ParseRunOutput` — the Summary section is stored on the task for card display
- Agent-directed runs require output trailers (RESULT, WORKED_IN, REPO, BRANCH, PR) — last occurrence wins
- Window reset detection: resets_at advance >2min OR utilization drop >40 points
- The SSE `usage` event and `GET /api/usage` both serialize `domain.UsageSnapshot`
  (`scheduler.usageSnapshotJSON`); the UI replaces the object wholesale, so a second wire
  shape blanks whatever it omits. No reading yet = no broadcast, never a zero snapshot
- Scheduling policy lives only in internal/scheduling (pure: no DB, network, clock, log);
  internal/scheduler performs its Plan. Task duration is NOT modelled — runs get a flat
  timeout and may straddle a session reset; spend is capped per run by --max-budget-usd
- Idle loop detection: kills after 5 consecutive zero-work result cycles (maxIdleCycles)
- Process group management: Setpgid=true, kills via -pgid, SIGTERM then SIGKILL after 5s
- GIT_TERMINAL_PROMPT=0 injected to prevent git prompts in spawned processes

## Cross-Component Dependencies

```
cmd/burnrate -> config, store, scheduler, server, caffeinate, recovery, log, daemon, usage, retention
server       -> store, config, scheduler, caffeinate, runner, service, checkout, prstatus, mcp, web, log
scheduler    -> store, config, runner, usage, log
runner       -> store, config, claude, git, notify, usage, prompts, log
service      -> domain, store (via interfaces), notify
                (RequestService is where requests broadcast + notify from, not the handlers)
store        -> domain (implements ports)
usage        -> (standalone: HTTP client, keychain, OAuth refresh)
claude       -> (standalone: exec wrapper, event parser, rate limit detection)
config       -> store (reads settings)
recovery     -> store, git, log
prstatus     -> store, domain, git, log   (gh pr view sweep; probe injectable for tests)
checkout     -> domain, git               (switches the user's clones under base_code_dir)
caffeinate   -> (standalone: exec wrapper)
mcp          -> service (MCP streamable-HTTP: ask_human, await_request, request_demo;
                plus list_capture_targets + capture_screen, which always return isError)
notify       -> (standalone: a Notification struct + a callback the server registers)
retention    -> domain, log       (prunes aged capture videos; store injected as a narrow interface)
web          -> (standalone: embedded filesystem)
```

## Repository Interfaces (domain/ports.go)

```
TaskRepository       CreateTask GetTask ListTasks UpdateTask DeleteTask ReorderTasks SetTaskStatus QueuedTasksByOrder TasksByStatus TaskCountsByStatus
RunRepository        CreateRun SetRunSessionID SetRunPID SetRunRateLimitResetAt SetRunStatus FinishRun RunsByStatus ResumableRuns ListRuns LatestRunForTask SetRunBranch SetRunAgentRepo SetRunAgentWorkedIn WindowAggregate
CommentRepository    AddComment GetComment CommentsForTask UnconsumedComments MarkCommentsConsumed MarkCommentConsumed
AttachmentRepository AddAttachment ListAttachments GetAttachment DeleteAttachment
UsageRepository      InsertUsageSnapshot LatestUsageSnapshot TrimUsageSnapshots
SettingsRepository   GetSetting SetSetting AllSettings
HumanRequestRepository CreateHumanRequest GetHumanRequest ListHumanRequests SetHumanRequestStatus SetHumanRequestLive SetHumanRequestResponse PendingRequestCount CancelTaskRequests
CaptureRepository    CreateCapture GetCapture ListCaptures SetCaptureStatus SetCaptureVideoPath SetCaptureTranscript SetCaptureNotes SetCaptureDuration FinishCapture
```

All implemented by `*store.Store`.

`*store.Store` also carries `RequestCountsForRun(runID) (total, pending int, err error)`,
which is not in a port: only the runner uses it (it holds a concrete `*store.Store`), to tell
"the human already replied" apart from "this agent parked without ever asking".

Also not in a port: `ClearHumanRequestLive() (rows int64, err error)` — one UPDATE, called
once from `cmd/burnrate/serve.go` immediately after `store.Open`. `live` describes a running
long poll, so every flag a dead daemon left set is a lie that sorts a request nobody is
waiting on to the top of the queue (`ORDER BY live DESC`) wearing a LIVE badge.

## Service Layer Errors

```
ValidationError{Field,Message} -> 400
NotFoundError{Entity,ID}       -> 404
ConflictError{Message}         -> 409
```

## Scheduler Window States

```
IDLE       no usage data / fresh state
OPEN       window active, launches allowed
SATURATED  5h utilization >= threshold
```

scheduling.WindowStateFor — pure function of the latest reading. There is no DRAINING
state: the end of a session no longer changes what the scheduler does.

## Size Estimates (defaults)

```
small:   expectedDuration=15m  budgetUSD=5   maxTimeout=30m   utilCost=8%
medium:  expectedDuration=40m  budgetUSD=15  maxTimeout=75m   utilCost=20%
large:   expectedDuration=90m  budgetUSD=25  maxTimeout=150m  utilCost=40%
```

## Prompt Templates (prompts/)

```
WorkerNew       fresh task in managed worktree
WorkerResume    resuming interrupted run (--resume sessionID)
WorkerFollowup  applying follow-up comments to existing branch
WorkerAgent     agent-directed mode (no pre-created worktree)
WorkerContinue  in-run nudge after an auto-denied tool call
DenialPolicy    appended to every prompt (unattended runs never wait for approval)
EffortLevels    the 1-4 level-of-effort scale appended to every worker prompt
```

Level 4 (validate end to end) is opt-in: only an explicit user directive selects it, and no
prompt ever tells a worker to promote itself there (TestEffortSectionForbidsSelfRaisingToFour
in internal/runner, TestDryRunPromptCarriesEffortLevel in internal/scheduler for the prompt
the CLI is actually invoked with).

All four worker templates document the Human Loop: the tools under their full runtime names
(`mcp__burnrate-human-loop__ask_human` / `__request_demo` / `__await_request` — the bare names
never appear in a tool list, so the prompts must not use them), `RESULT: WAITING_HUMAN` as a
bare alternative to the per-repo `RESULT:` lines, and the resumed-agent contract from PRD §3.4
(re-verify the dev server, restart via `revival_steps`, re-issue the demo rather than trusting
stale context). Pinned by TestBuildPromptHumanLoop* in internal/runner/humanloop_prompt_test.go.
`prompts/denial-policy.md`'s carve-out uses the same full names and no longer names the capture
tools, which are not implemented.

Careful with the literal string `## Follow-up Instructions`: several runner tests assert a
built prompt does NOT contain it (it is the header the runner injects for unconsumed comments),
so prompt prose must refer to it some other way.

The four worker templates share four sections, each pinned by a test in internal/runner:
doc policy (TestBuildPromptStatesDocPolicy), field-guide pointer
(TestBuildPromptPointsAtFieldGuide), worktree-bootstrap friction
(TestBuildPromptReportsWorktreeFriction), and delegation guidance. Doc policy is the
three-way split: ai.md = map, CLAUDE.md = traps, README.md = humans; deleting stale
prose counts as an update.

## Web UI — Human Loop surface (web/src)

```
stores/request-store.ts             pending requests, replaced wholesale from the SSE `request`
                                    array payload (same shape as GET /api/requests?status=pending)
hooks/use-pending-requests.ts       subscribes the store; feeds the banner + per-task cards
lib/human-requests.ts               parseDemoBody(body) -> DemoBrief | null. A demo request's body
                                    is the JSON the request_demo tool wrote ({steps, expected,
                                    look_for, url, revival_steps}); anything unparseable is
                                    rendered as raw prose rather than dropped.
                                    stripMarkdown / summarizeMarkdown / requestSummary: list
                                    surfaces derive their own one-liner (first prose line, syntax
                                    stripped, <=SUMMARY_MAX_CHARS=100). ask_human sets `title` to
                                    the FULL markdown question — a verbatim duplicate of the head
                                    of `body` — so `title` must never be rendered as a heading
lib/composer-attachments.ts         pure half of paste/drop-a-screenshot-into-a-composer:
                                    imageFilesFrom(DataTransfer) (files first, items as fallback),
                                    isSupportedImage / uploadRejection (allowlist + 10MB mirror of
                                    handlers_attachments.go — SVG excluded, server rejects it),
                                    dragHasFiles(types), composeBodyWithAttachments(body, atts) ->
                                    body + one `![name](/api/attachments/N/data)` line each.
                                    Covered by lib/composer-attachments.test.ts
hooks/use-composer-attachments.ts   upload state + paste/drop handlers for those images, shared by
                                    BOTH composers (request reply form, task comment box) — do not
                                    fork it. Uploads fire on arrival (not on submit), each entry is
                                    uploading|done|error, `uploaded` is what
                                    composeBodyWithAttachments consumes, remove() DELETEs a landed
                                    attachment, clear() runs after a successful submit. Object URLs
                                    are revoked on remove/clear/unmount. Returns `onPaste` and
                                    `dropZoneProps` (spread on the composer wrapper) plus `dragging`
components/attachments/             attachment-gallery + attachment-item (the task's own gallery);
                                    composer-attachment-row -> composer-attachment-chip is the chip
                                    row both composers render above their textarea (spinner ->
                                    thumbnail -> ✕; an error chip shows the reason and is removable)
components/ui/markdown-body.tsx     MarkdownBody: react-markdown + remark-gfm + .prose-comment,
                                    raw HTML off. Every request body / demo-brief field goes
                                    through it; the comment thread renders the same pairing inline.
                                    Relative image URLs pass react-markdown's defaultUrlTransform,
                                    which is what makes an attachment line render as an image
app/globals.css .prose-comment      Tailwind preflight zeroes list markers, so ul/ol must set
                                    list-style-type explicitly (disc/decimal + circle/square and
                                    lower-alpha/lower-roman when nested) — without it agent
                                    markdown lists render as bare indented lines. Task-list items
                                    (`li:has(> input[type=checkbox])`) drop the marker. Headings are
                                    a restrained comment-sized scale (h1 1.25em/700, h2 1.1em/600,
                                    h3 1em/600, h4-h6 1em dim), `img` is capped at max-width:100%
components/tasks/requests/          request-card, request-detail, reply-form, demo-brief,
                                    capture-approval-actions, result-toggle,
                                    pending-requests-banner, pending-request-row, task-requests
                                    (one component per file — see CLAUDE.md).
                                    reply-form accepts images by paste and drag-drop: each uploads
                                    at once to POST /api/tasks/{task_id}/attachments, shows a chip
                                    (spinner -> thumbnail -> ✕), and is appended to the submitted
                                    body as a markdown image line. An upload failure is an inline
                                    error chip and never blocks sending the text; a non-image paste
                                    keeps the browser's plain-text behaviour; dragover only
                                    preventDefaults when the payload has files, so text drags still
                                    drop into the textarea. An image with no prose is a valid submit.
                                    This is a channel to the agent, not just to the reader: a task's
                                    attachments are copied into the run workdir and listed under
                                    `## Image Attachments` in the next prompt (runner.buildPrompt),
                                    so a screenshot pasted into a reply reaches the agent.
                                    request-detail = body + affordances, shared by the task card
                                    and the banner row so both can answer identically. Banner rows
                                    are disclosures: the queue head is open on arrival and answers
                                    in place (reply textarea, demo verdict, capture approve/deny);
                                    "Open task" is the secondary path, not the way to respond.
                                    The expanded panel is `bg-elevated` — the textarea, the
                                    revival block (`surface`) and the idle result chips (`raised`)
                                    all have to stay visible inside it
components/comments/comment-thread  takes a `refreshKey` prop: the thread only refetched on
                                    mount/submit, so a reply landing from outside the browser
                                    (another tab, the desktop app) left it stale. The thread owns
                                    the list; the input is comment-composer.tsx, at the TOP (the
                                    list renders newest-first)
components/comments/                comment-composer: textarea + voice dictation + the same
comment-composer.tsx                useComposerAttachments paste/drop path as the reply form, so a
                                    screenshot pasted into a comment uploads and is appended as
                                    `![name](/api/attachments/N/data)`. Attachments upload
                                    independently of the comment POST, which still 409s while the
                                    task is running — the image is on the task either way
```

## Run Statuses (domain)

```
starting -> running -> succeeded|rate_limited|timed_out|errored
starting -> resuming -> running -> ...
abandoned (set during reconciliation for orphaned runs)
```

## Token Resolution (runner.resolveTokenEnv)

Discovery order: CLAUDE_CODE_OAUTH_TOKEN env -> pinned account keychain -> pinned account credentials file -> default keychain -> default credentials file. Refresh via console.anthropic.com/v1/oauth/token. Single-flight dedup.
