# burnrate

A macOS daemon that maximizes Claude Code subscription utilization: it keeps an ordered task
queue, watches the Anthropic usage API, and runs tasks as `claude -p` agents in isolated git
worktrees, each producing a pushed branch and a draft PR. One task may span several repos.

Go backend + embedded Next.js frontend + optional Tauri desktop wrapper. Single direct Go
dependency: `modernc.org/sqlite`.

## The code is the documentation

Write code that explains itself and keep this file short. Go and TypeScript are readable
languages; a comment that restates the next line is noise, and a prose section that restates a
package is worse — it goes stale silently and gets believed anyway.

- **What it does** → read the code.
- **Where things live** → `ai.md`. It is a *map*: routes, schema, state machines, config keys,
  invariants. Terse and greppable — what exists and where, never how it works.
- **This file** → only what the code cannot tell you: how to run things, and the traps that
  have already cost someone a wasted run.
- **How a human runs it** → `README.md`.

Do not add a section here that narrates a package, restates a diff, or records a decision's
history. **Deleting stale documentation is a documentation update** — if you read something
here that is now wrong, or that only restates code, delete it in the same commit and say so.
That is usually the higher-value edit.

The same policy is stated to every worker in `prompts/worker-*.md` and pinned by
`runner.TestBuildPromptStatesDocPolicy`.

## Commands

```bash
make bootstrap    # fresh clone/worktree: install web deps, verify the tree compiles
make dev          # whole stack: desktop app (owns the daemon) + Next dev server
make dev-web      # daemon (go run) + Next dev server, no desktop app
make go-dev       # Go daemon only, DEV_PORT, against the live data dir
make go-dev-dry   # same, BURNRATE_DRYRUN=1 (stubs claude)
make check        # full gate: lint + go test + web test + secret scan + web build
make secrets      # gitleaks over the working tree
make secrets-history  # gitleaks over every commit reachable from any ref
make snapshot     # build the history-free repo to publish; stops before pushing
make web-test     # frontend unit tests only (node --test, no npm install needed)
make test-race    # go test -race ./...
make recover      # sweep unpushed branches -> draft PRs
make desktop-dev  # desktop app alone, on the dev port + data dir
```

`dev`, `dev-web` and `desktop-dev` run on `DEV_PORT` (9113) with `DEV_DATA_DIR`
(`~/.burnrate-dev`), so they cannot race the installed app for `{dataDir}/daemon.lock` or
launch runs off the live queue. `DEV_DATA_DIR=~/.burnrate` opts into real data — quit the
installed app first, because the desktop app kills whatever holds its port or lock on launch.

Tests use real SQLite (`store.Open(":memory:")`) — there are no mocks for the DB layer.

## Traps

**`web/out/.gitkeep` is load-bearing.** The export dir is gitignored but `//go:embed all:out`
needs it, so a tracked zero-byte placeholder keeps `web`, `internal/server`, and
`cmd/burnrate` compiling in a fresh checkout. Delete it and three packages fail to *build*.
See `web/AGENTS.md`.

**The origin check is the API's only trust boundary.** The loopback bind is not one — any
page in the user's browser can reach `127.0.0.1`, and `POST /api/tasks/{id}/run-now` launches
`claude --permission-mode auto`. `internal/server/origin.go` refuses cross-site
requests and allowlists the Tauri webview, which loads embedded assets and so is genuinely
cross-origin against the sidecar. Two ways to silently break it: add an allowlist entry that
is not a local origin, or allow requests that carry no `Origin` *and* no `Sec-Fetch-Site` to
start mattering — that gap is deliberate (the tray's reqwest calls and the Next dev proxy land
there) and is safe only because a browser always sends one of the two. There is intentionally
no API token; `SECURITY.md` explains why one would not raise the bar it appears to.

**The desktop app grants exactly one shell permission, and that is deliberate.**
`capabilities/default.json` lists `shell:allow-open` and nothing else, because that is all
either side actually calls — `shell().open()` in `lib.rs` and `plugin:shell|open` from
`app-shell.tsx`. The sidecar is launched with `StdCommand`, which is native Rust and needs no
capability at all, so `allow-execute`/`allow-spawn`/`allow-stdin-write`/`allow-kill` buy
nothing and cost a lot: `remote.urls` extends every listed permission to any page served from
`http://127.0.0.1:*`, and the CSP still carries `unsafe-eval`. Adding one back to "fix" a
spawn problem is almost certainly the wrong fix — `StdCommand` is the one that works.

**The local secret scan is advisory; the CI `secrets` job is the gate.** `scripts/secret-scan.sh`
exits 0 when gitleaks is not installed, so `make check` passing does not mean the tree was
scanned — `brew install gitleaks` if you want the hooks to do anything. When it does
fire, rotate before you rewrite: GitHub keeps unreachable objects and its own `refs/pull/*`
forever, so a force-push does not unpublish anything. `.gitleaks.toml` allowlists exactly one
value, `internal/usage/refresh.go`'s `oauthClientID` — a public OAuth client identifier that is
a bare UUID and so scores like a secret. Scope any new allowlist to the value, never the file;
`scripts/secretscan_test.go` fails if that entry is widened to a path.

**`make secrets-history` fails on a clone, not on a branch.** It scans `--all`, and a worktree
shares its parent clone's refs and object store — so a stale tag or stash in the *clone* fails
the scan inside every worktree of it, no matter how clean your branch is. Check where the
finding actually lives (`git for-each-ref --contains <commit>`) before touching your diff;
`scripts/purge-leaked-refs.sh` reports and clears local refs that reach a leak. Note that
`scripts/secret-scan.sh` cds to its own checkout in every mode but `push`, so pointing it at
another repo by cd-ing there silently scans the wrong one.

**Scheduling policy lives only in `internal/scheduling`** — pure, no DB, network, clock, or
logging. `internal/scheduler` is the daemon around it. Never put a scheduling rule in the
daemon: launching, `GET /api/status`, the per-task forecast chips, and the logs all render the
same `Plan`, and that shared derivation is the only reason the UI can't contradict the daemon.

**Attempts count interruptions, not just failures.** A session suspension consumes an attempt
exactly like an error does, because the increment is the only thing bounding retries of a
persistently misconfigured task in `runner.preflightError`. Hence `max_attempts` defaults to 20
rather than a failure-shaped number. Any user touch (edit, status change, resume, comment)
clears the count via `store.ResetTaskAttempts`; nothing the daemon does clears it.

**A run's start time has three renderers that must agree** — `web/src/lib/format.ts`,
`desktop/src-tauri/src/lib.rs`, `cmd/burnrate/status.go`. One shape (`2:14pm`, dated once older
than today) so the window, the tray, the terminal, and `logs/run-<id>.jsonl` read against each
other. Change one, change all three.

**Line counts are split across two tables on purpose.** `task_prs` holds each branch's
*cumulative* diff (a followup run re-measures the whole branch); `runs` holds only the *delta*
since the last measurement. Summing `runs` therefore never double-counts a branch that several
runs contributed to — don't "fix" one to match the other.

**`SERIES_COLORS` order is a validated colorblind-safe sequence.** Do not reorder or extend it;
a sixth model folds into a gray "other" series.

**Config precedence is defaults → DB settings → env vars, except account keys.**
`claude_config_dir` and `sandbox_keychain*` invert it: the DB wins over the environment.

**All DB times are UTC RFC3339 strings**, converted to local only at the edges.

**Resuming a session by hand needs three things, not one.** `claude --resume <id>` alone
answers *"No conversation found with session ID"*: the CLI resolves the id against the
current directory's project history, so you must be in the run's worktree. Add
`CLAUDE_CONFIG_DIR` and it finds the transcript but answers *"Not logged in · Please run
/login"*, because a pinned account's credentials live in a sandbox keychain the CLI cannot
read — the same reason `runner.resolveTokenEnv` injects `CLAUDE_CODE_OAUTH_TOKEN`. All three
are what `claude.ResumeCommand` emits; both failures were reproduced, and each looks like a
different bug than it is.

## Landing site (`landing/`)

Separate Next.js app on Vercel (burnthemtokens.com), *not* embedded in the Go binary — that's
`web/`. Own `package.json`. See `landing/README.md`.

Download analytics send Vercel Web Analytics **custom events** server-side, because a `.dmg`
download is a 307 the client-side `<Analytics />` never sees. Two things bite:

- **Custom events are Pro-only.** On Hobby this code runs and silently records nothing.
- **Vercel allows 2 custom properties per event**, so only `version` and `target` are
  properties; every other dimension is folded into the event *name* (`download:bot`,
  `update_check:up_to_date`, …).

Counting rules that look like bugs but aren't: crawlers are excluded everywhere, but *scripted*
clients (`curl`, `Go-http-client`, no UA) count as automated only for `download` — for
`update_check` they are the expected native caller, and excluding them would zero out active
installs. HEAD is not a download; a JSON read of the manifest is not a download.

Events are metered, so `ANALYTICS_DISABLED=1` is the kill switch. Tests: `cd landing && npm test`
(Node's built-in runner against the `.ts` sources, no framework).
