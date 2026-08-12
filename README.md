# burnrate

Personal macOS daemon that maximizes use of a Claude Code subscription's 5-hour session
quota. Keeps an ordered task queue; the moment the session window resets, launches the
top-N tasks as `claude -p` agents in isolated git worktrees, each ending in a pushed
branch + draft GitHub PR.

Architecture: [`docs/architecture.md`](docs/architecture.md).

> **Before you run it:** burnrate drives Claude Code with
> `--permission-mode auto`. Anything that can create a task can run arbitrary code as
> your user, and the local HTTP API is a privileged control plane — never expose it through
> `ssh -L`, a tunnel, or a reverse proxy. Read [SECURITY.md](SECURITY.md) first.

Licensed under [BSD-3-Clause](LICENSE) — do what you like with it, keep the copyright notice
(which names this repo) in source and in anything you ship built from it. Contributions:
[CONTRIBUTING.md](CONTRIBUTING.md).
Not affiliated with or endorsed by Anthropic; Claude and Claude Code are Anthropic
trademarks. burnrate drives the `claude` CLI you install yourself and bundles nothing.

## Local development quickstart

Prerequisites: Go 1.25+, Node 20+, npm, [pm2](https://pm2.keymetrics.io/) (`npm i -g pm2`).

```bash
git clone https://github.com/Verhum/burnrate.git && cd burnrate
make bootstrap        # install web deps, verify Go compiles (~5s)
make dev              # PM2 starts the daemon + Next.js dev server
```

The dev stack runs on its own port and data directory so it never conflicts with
an installed app:

| Service | URL |
|---|---|
| Web UI | http://localhost:3113 |
| Daemon API | http://127.0.0.1:9113 |

`DRY=1 make dev` stubs all `claude` invocations — useful for working on the UI
without burning tokens.

```bash
make dev-logs         # tail PM2 logs
make dev-restart      # rebuild Go binary, restart daemon (keeps web running)
make dev-stop         # tear down
```

For faster Go iteration without PM2:

```bash
make dev-web          # daemon via `go run` + Next.js dev server
```

## Desktop App

A Tauri v2 macOS app bundles the daemon + web UI into a distributable `.app`/`.dmg`.
See [`desktop/README.md`](desktop/README.md) for build instructions.

```bash
make desktop-dev     # dev mode (Go + Tauri hot-reload)
make desktop-build   # release .app + .dmg
```

### Acting on a task

The same UI bundle runs in two hosts — the desktop app and `http://localhost:9112` in a
browser — and behaves identically in both.

- **Run now**, **Del** and a run's **Cancel** each ask for confirmation in an in-app
  dialog. Enter confirms, Escape cancels.
- A task's **Pull requests** section labels each PR with its repo and number (`api#311`) and
  colors it by state — open, draft, merged, closed. **Refresh status** re-asks `gh`; the
  daemon also polls every five minutes.
- **Checkout** switches every branch the task produced into your own clones under
  `base_code_dir`, so one local dev server can be pointed at the work instead of one per
  worktree. It refuses a clone with uncommitted changes and never resets a local branch to
  origin, and since a clone holds one branch at a time, a task with several PRs in the same
  repo checks out the newest and says so for the rest.
- Results are reported as toasts, bottom right. A launch that works says so; one the
  scheduler refuses shows its reason — `task 71 is already running`,
  `cannot launch: session spent (100%) — new session in 28m` — and stays up until
  dismissed, since that reason is the answer to "why isn't it doing anything?".

These are in-app and transient: always the direct result of something you just did. The
macOS notifications the app also raises are the other channel — events that arrive while
the window is not in front, such as a review being ready.


## Landing site & download analytics

`landing/` is the Next.js site at **burnthemtokens.com**. It hosts the download, the release
manifest, and the updater endpoint — see [`landing/README.md`](landing/README.md).

Downloads and update checks are tracked as **Vercel Web Analytics custom events sent from the
server** — a `.dmg` download is a redirect, which the client-side script can't see. Read them in
the Vercel dashboard under Analytics → Events (`download`, `update_check:*`), broken down by
version and target. Custom events require a Pro team; on Hobby nothing is collected.

Design, counting rules and plan limits: [`docs/specs/spec-x-download-analytics.md`](docs/specs/spec-x-download-analytics.md).

## Building from source

A fresh clone or `git worktree add` builds and tests as-is — `go build ./...` and
`go test ./...` need no Node toolchain. The frontend is a separate step:

```bash
make bootstrap    # install web deps (~5s warm), verify the Go tree compiles
make go-build     # web export + Go binary -> ./burnrate
```

Skipping the frontend build is supported: the binary still serves the JSON API and
SSE stream, and answers UI routes with a 503 naming the commands above instead of
a bare 404. `make dev` serves the UI from the Next.js dev server and needs no
export at all.


## Commands

```bash
go build -o burnrate ./cmd/burnrate    # API-only binary; see "Building from source"

./burnrate serve       # foreground daemon: scheduler + UI at http://127.0.0.1:9112
./burnrate status      # live usage snapshot + queue summary
./burnrate resume      # print the `claude --resume` command for a run (no arg: list resumable runs)
./burnrate token       # print a fresh OAuth token for the pinned account (used by `resume`)
./burnrate recover     # sweep for unpushed branches -> draft PRs, prune stale worktrees
```

## Data

Everything lives under `~/.burnrate/` (override with `BURNRATE_DATA_DIR`):
`burnrate.db` (SQLite), `worktrees/`, `logs/`.

## Key env overrides

`BURNRATE_DRYRUN=1` (stub claude invocations), `BURNRATE_USAGE_URL` (fake usage endpoint
for testing), `BURNRATE_PARALLEL_N`, `BURNRATE_MODEL`, `BURNRATE_PORT`, `BURNRATE_BASE_CODE_DIR`.

## Pinning a Claude Code account

By default burnrate inherits the Claude Code credentials from the current user's
environment. To pin it to a specific account (useful when running as a LaunchAgent
under a sandboxed home directory):

| Variable | Purpose |
|---|---|
| `BURNRATE_CLAUDE_CONFIG_DIR` | Path to the `.claude` config directory for the target account |
| `BURNRATE_SANDBOX_KEYCHAIN` | Path to the sandbox keychain (auto-derived from config dir if not set) |
| `BURNRATE_SANDBOX_KEYCHAIN_PASSWORD_FILE` | Path to the keychain password file (auto-derived if not set) |

Example pinning the daemon to a specific account:

```bash
BURNRATE_CLAUDE_CONFIG_DIR=~/code/myproject/.local_home/.claude ./burnrate serve
```

When `BURNRATE_CLAUDE_CONFIG_DIR` is set, burnrate resolves the OAuth token bundle from
the keychain or credentials file, refreshes it if near-expiry (rotating the refresh
token and persisting before use), and passes both `CLAUDE_CONFIG_DIR` and
`CLAUDE_CODE_OAUTH_TOKEN` to each spawned `claude -p` process. This bypasses the child's
own credential lookup (which cannot access the sandbox keychain). Sandbox keychain paths
are auto-derived from the parent directory (`../Library/Keychains/sandbox.keychain-db`
and `../.keychain-password`).

Mid-run token expiry (runs >1h) is accepted: the run dies, is classified resumable, and
the next launch injects a fresh token.

## Choosing a Claude Code account

burnrate can run against a specific Claude Code login (e.g. a project-local
sandboxed account under `~/code/<proj>/.local_home/.claude`) instead of whatever
the daemon's environment happens to provide.

- **Discover:** `./burnrate accounts` lists the default location (`~/.claude`) and
  every `~/code/*/.local_home/.claude` sandbox, marking the active one.
- **Select (web UI):** Config tab → "Claude Code Account" → pick one → *Use this
  account*. Applies live (token + spawned-run env) and persists across restarts.
  `GET /api/accounts` / `POST /api/accounts/select {config_dir}` back it. Only a
  discovered dir is accepted (no arbitrary keychain paths).
- **Select (env):** set `BURNRATE_CLAUDE_CONFIG_DIR` (and, for a sandbox login,
  `BURNRATE_SANDBOX_KEYCHAIN` + `BURNRATE_SANDBOX_KEYCHAIN_PASSWORD_FILE` —
  auto-derived from the config dir when both files exist) in the daemon's
  environment. Example:

  ```bash
  BURNRATE_CLAUDE_CONFIG_DIR=~/code/myproject/.local_home/.claude ./burnrate serve
  ```

The active account (config dir + keychain item id, never the token) shows in
`burnrate status` and `/api/status`.

**Precedence:** for the three account keys only (`claude_config_dir`,
`sandbox_keychain`, `sandbox_keychain_password_file`), a value chosen in the web
UI and stored in the DB **wins over** the `BURNRATE_*` environment variables.
Everywhere else env overrides the DB, but inverting it for these keys means a
live UI selection is not silently clobbered by the `BURNRATE_CLAUDE_CONFIG_DIR`
in the daemon's environment. The env vars act only as the bootstrap default used
until (and unless) an account is selected in the UI — selecting "inherited
environment" also counts as a selection and overrides the env. A UI selection
applies live (usage polling, spawned-run `CLAUDE_CONFIG_DIR`, and the
`claude --version` probe all re-resolve on the next tick/launch) — no daemon
restart required.

## Cost efficiency analytics

The Usage tab charts **cost per task** and **cost per line of code**, grouped by model and
bucketed by UTC day. Two separate charts, one line per model, over a 7/30/90-day range,
with a table view for the same numbers.

Both figures are ratios over each day's bucket — total spend ÷ tasks, and total spend ÷
lines changed — rather than averages of per-run ratios. A rate-limited attempt that spent
money without landing code therefore raises the day's unit cost, which is the honest
answer, instead of being dropped from the numerator.

To make that possible, each run records:

- **`model`** — what actually ran, read from the CLI's stream (an alias like `opus` resolves
  to a concrete version, and a mid-run fallback can substitute another model). The requested
  model is written before the run, so a run that dies early is still attributable. Runs from
  before this existed are grouped as `unknown`.
- **`lines_added` / `lines_removed`** — measured with `git diff <base>...HEAD` in each
  worktree the run reported, so commits that landed on the default branch after the branch
  point are not counted as the agent's output. Only committed work counts.

Line counts are stored twice on purpose. `task_prs` holds each branch's *cumulative* diff,
because a followup run re-measures the whole branch; the run row holds only the *delta*
since the last measurement, so summing runs never double-counts a branch that several runs
built. Measurement is best-effort — a removed worktree or an unresolvable default branch
logs a warning and leaves the run at zero lines rather than failing a successful run.

`GET /api/usage/cost-efficiency?days=30` returns the buckets, per-model totals, and the
chart's series order. That order is computed over *all* history rather than over the
requested range, so narrowing the range never repaints the models that survive the filter.

## What workers are told to do

Each run is prompted from an embedded template in `prompts/` — `worker-new` (fresh task in
a managed worktree), `worker-resume` (continue an interrupted run), `worker-followup`
(apply comments to prior work), `worker-agent` (agent-directed, no pre-created worktree).
All four share the same shape: implement, test, **update the docs**, commit, push, open a
draft PR.

### Level of effort

Every prompt also carries a **level of effort** — how far the worker carries the task before
calling it done. This is not the model's reasoning effort; it is the depth of the deliverable:

1. **Investigate** — research and report, with the `file:line` evidence, and no speculative code.
2. **Write the code** — implement it; it compiles and it is committed.
3. **Verify** — implement it, then prove it: edge cases reasoned through, unit tests that would
   fail without the change, and the repo's build, lint, and test commands actually run.
4. **Validate end to end** — verify, then exercise the change through its real entry point
   (start the server and hit the route, run the binary, drive the UI) and show the output.
   **Opt-in only:** a worker reaches level 4 solely because you asked for it.

**Level 3 is the default**, and burnrate picks it automatically — no field to fill in. Absent
an instruction the worker is told to treat a pure research ask as a 1, otherwise work at 3, and
never promote itself to 4 no matter how risky the change looks — it recommends 4 in its summary
instead. To pin a level, say so anywhere in the task description or a follow-up comment —
`LOE: 4`, `level of effort 1`, `effort level: investigate` all work, and the newest follow-up
comment outranks the description. The keyword is required, so prose that happens to mention
"level 4" is not mistaken for a directive. Whatever the level, the run still ends with a
pushed branch and a draft PR — a level 1 run ships its findings as a committed write-up.

All four templates also point the worker at **`burnrate.md` in your `base_code_dir`**, a field
guide that lives alongside the checkouts rather than inside any one repo. It maps the repos burnrate
works in (remote, default branch, stack, build and verify commands) and records the
gotchas that have already cost prior runs a redo, so a worker doesn't rediscover them.
It is advisory — it never overrides the task or a user comment — and it is maintained in
place by the recurring "Burnrate: Dreaming" task, which re-reads completed tasks and their
user comments and folds new learnings in. If the file is absent, workers proceed as before.

Workers are told that **the code is the documentation**. Go and TypeScript are readable, so a
comment that restates the next line is noise and a prose doc that narrates a package is worse —
it goes stale silently and gets believed anyway. Documentation is not a per-change ritual;
there are three destinations and nothing else: `ai.md` is the map (routes, schema, config keys,
invariants — what exists and where), `CLAUDE.md` holds only traps the code cannot tell you, and
`README.md` is for humans running the thing. Deleting a paragraph that is now wrong, or that
only restates code, counts as leaving the docs true and is usually the better edit. If none of
the three applies, the worker writes nothing and says so in a line.

Workers also report **worktree bootstrap friction**. A fresh worktree should build and test
with no setup step; when it doesn't — a gitignored artifact needed at compile time, deps that
must be installed before the test command works, a config file copied by hand — the worker
works around it and then says so in its final output, naming the error, the workaround, and
the one-time fix that would remove it. That cost is per-repo and permanent, so a run that
absorbs it silently leaves every later run to pay it again. burnrate's own case was
`web/out`: gitignored but `//go:embed`ed, so three packages failed to compile in a fresh
checkout until `web/out/.gitkeep` and `make bootstrap` landed.

## How it decides to launch

- Polls `api.anthropic.com/api/oauth/usage` every 5s (OAuth token read live from the
  macOS Keychain; the `User-Agent` must start with `claude-code/<ver>` or the endpoint 429s,
  so burnrate identifies itself in a token appended after that prefix).
- Window state machine: IDLE → OPEN → SATURATED (util ≥ threshold, default 100%).
  Resets detected every tick (resets_at advanced OR util dropped sharply), so sleep/wake
  is handled by catch-up, not exact timers.
- When util_threshold ≥ 100 (the default), all utilization-based launch gating is
  disabled — cumulative headroom and per-tick saturation checks are skipped. Hitting the
  limit mid-run is fine because rate-limited runs resume next window. Launches are still
  held at measured util ≥ 100 (the API would reject). Set BURNRATE_UTIL_THRESHOLD < 100
  to restore conservative gating.
- Only launches a task if its size estimate says it will finish before the window closes;
  per-run timeout is capped at time-to-reset minus a drain margin.
- Interrupted runs (rate limit, timeout, crash) keep their worktree + claude session id and
  are RESUMED next window with `claude --resume` — never restarted from scratch.
