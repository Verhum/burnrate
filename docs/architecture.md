# Architecture

How burnrate is put together, for someone reading the code for the first time. For build and
run instructions see [`README.md`](../README.md); for the trust boundary see
[`SECURITY.md`](../SECURITY.md). [`architecture.html`](architecture.html) is a browsable
companion with the full config-key and state-machine tables.

## The problem it solves

A Claude Code subscription grants quota in rolling 5-hour session windows. Unused quota does
not carry over — a window that closes with nothing queued is quota spent on nothing. burnrate
keeps an ordered task queue and, when a window opens, launches the top-N tasks as `claude -p`
agents in isolated git worktrees, each ending in a pushed branch and a draft GitHub PR.

Two properties drive most of the design:

1. **Finish-within-session.** Only launch work expected to complete before the window closes.
   Size estimates feed a remaining-window gate and derive per-run timeouts.
2. **Resume, never restart.** If a run dies — rate limit, crash, timeout, laptop sleep — the
   claude session id and worktree are persisted so the next window continues with
   `claude --resume`. Redoing completed work is the most expensive failure mode there is.

Property 2 is why the session id is written to the database the instant the `system-init`
stream event is parsed, rather than at the end of the run: a process that dies before it
finishes still has to be resumable.

## Shape

A single Go binary serving an embedded web UI, plus an optional Tauri desktop wrapper around
the same daemon and UI bundle.

```
cmd/burnrate/         serve | status | accounts | resume | token | recover | version
internal/
  domain/             shared model types
  config/             configuration + Claude account discovery/pinning
  store/              SQLite (modernc.org/sqlite, CGO-free), migrations
  usage/              keychain token read + OAuth usage polling
  claude/             claude CLI invocation, stream-json parsing, rate-limit detection
  git/                worktree lifecycle, default-branch detection, dirty checks
  scheduler/          poll loop, window state machine, size estimates
  scheduling/         session-window reset arithmetic
  runner/             per-task execution, failure classification
  service/            application services over store + runner
  server/             HTTP mux, REST API, SSE, origin enforcement
  recovery/           unpushed-branch -> PR sweep, stale-worktree cleanup
  checkout/           switch a task's branches into the user's own clones
  prstatus/           cached PR state, polled independently of run lifecycle
  notify/             notification fan-out (SSE + macOS notifications)
  retention/          prune aged capture recordings
  daemon/             single-instance lock
  launchd/            macOS service plist generation
  log/                logging
  caffeinate/         keep the machine awake across a run
  mcp/                MCP server surface
  whisper/            voice transcription for voice-created tasks
  ble/                "burnboi" ESP32 BLE display + voice button
prompts/              agent prompt templates (go:embed)
web/                  Next.js UI, built and embedded into the binary
desktop/src-tauri/    Tauri v2 macOS app wrapping daemon + UI
landing/              Next.js site: download, release manifest, updater endpoint
```

Runtime state lives under `~/.burnrate/` (override with `BURNRATE_DATA_DIR`): the SQLite
database, worktrees, per-run logs and capture recordings.

## The run lifecycle

1. **Gate.** The scheduler polls usage and evaluates the queue. A task launches only if the
   window has room for its size estimate and it is not already running.
2. **Worktree.** Created by the daemon in Go, not by the agent, so the daemon always knows the
   path and branch for resume and cleanup even if the agent never reports them.
3. **Invoke.** `claude --print --output-format stream-json --verbose --permission-mode auto`
   with the model and budget from config, in the worktree, in its own process group.
   SIGTERM then SIGKILL on timeout, with an idle-loop guard that kills a run making no
   progress. The session id is persisted from the first `system-init` event.
4. **Report.** The agent commits, pushes, and opens a draft PR, printing the URL on its last
   line. `runner` classifies the outcome; `prstatus` tracks the PR from then on, since a PR
   outlives the run that opened it.
5. **Recover.** `recovery` sweeps unpushed branches into PRs and prunes stale worktrees, for
   runs that died between pushing and reporting.

## Usage data

Session quota comes from `GET https://api.anthropic.com/api/oauth/usage`, authorized with the
Claude Code OAuth token read from the macOS Keychain (`security find-generic-password -s
"Claude Code-credentials"`). The token expires roughly hourly and is re-read on 401; it is
never persisted. The endpoint requires a `claude-code/<version>` leading User-Agent token or
it returns persistent 429s — burnrate sends that token and appends its own identifier after
it. This is an undocumented endpoint and the most likely part of burnrate to break.

A fallback signal parses `You've hit your limit ... resets <time>` out of the claude stream
and stderr, so quota exhaustion is still detected if the usage API is unavailable.

## Trust boundary

The daemon binds `127.0.0.1` only, and the local HTTP API is a **privileged control plane, not
a dashboard**: anything that can create a task can execute arbitrary code as you, because
tasks run with `--permission-mode auto`. There is deliberately no bearer token — the daemon
serves the UI, so any token it hands a browser is readable by every same-user process, which
buys the appearance of authentication and no threat coverage. The browser attack it would
otherwise cover is closed by the origin allowlist in `internal/server/origin.go`.

Never expose the port through `ssh -L`, a tunnel, or a reverse proxy.
[`SECURITY.md`](../SECURITY.md) is the full statement.

## Storage

SQLite via `modernc.org/sqlite` (CGO-free, so cross-compilation stays simple), with an
embedded schema and forward migrations tracked in `schema_migrations`. Principal tables:
`tasks`, `runs`, `task_prs`, `task_comments`, `task_attachments`, `human_requests`,
`captures`, `usage_snapshots`, `settings`.

`tasks.sort_order` is a REAL so drag-reordering inserts at the midpoint between neighbors
instead of renumbering the queue.
