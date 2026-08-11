# Security

## Read this before you run burnrate

**burnrate runs Claude Code with `--permission-mode auto`.** Anything that can
create a task can execute arbitrary code as your user account. The local HTTP API is a
privileged control plane, not a dashboard.

Concretely, on the machine running the daemon, a task can:

- run any command your user can run, with no approval prompt
- read anything under `base_code_dir`, plus your `~/.claude` directory and `gh` credentials
- push branches and open pull requests on any repo your credentials reach

The agent process also inherits `CLAUDE_CODE_OAUTH_TOKEN`. Treat a burnrate host the same
way you would treat a machine with an unlocked SSH agent.

## The trust boundary

The daemon binds `127.0.0.1` only (`internal/server/server.go`). Loopback is **not** the
boundary by itself — any page in your browser can send requests to `127.0.0.1`. The actual
boundary is the origin check in `internal/server/origin.go`, which refuses requests carrying
a cross-site `Origin` or `Sec-Fetch-Site` and reflects CORS headers only for the Tauri
webview and the daemon's own loopback origin.

That check also closes DNS rebinding, since a rebound hostname still presents the attacker's
origin.

**It assumes the daemon is only ever reachable from localhost.** Do not defeat that:

- no `ssh -L` / `ssh -R` forward to the API port
- no ngrok, Cloudflare Tunnel, Tailscale Funnel, or any other public tunnel
- no reverse proxy in front of it, and no rebinding it to `0.0.0.0`

Any of those turn an unauthenticated code-execution endpoint into a remotely reachable one.
There is no authentication on the API, by design: a token stored on the same machine, in a
file the daemon must serve to its own UI, would be readable by any process running as you
and so would not raise the bar it appears to raise. The origin check is what stops the
realistic attack (a web page you visit); it does not stop another process running as your
user, and nothing here is intended to.

## Multi-user machines

burnrate is designed for a single-user macOS workstation. It is not hardened against other
local users, and the state directory permissions do not currently assume a hostile local
account. Do not run it on a shared host.

## Reporting a vulnerability

Email **security@ver-hum.com** with a description and, if you have one, a reproduction. Please
do not open a public issue for anything that lets a third party reach the API or execute code.

Expect an acknowledgement within a few days. This is a personal project with no SLA and no
bounty program; fixes ship on a best-effort basis.

## Scope

In scope:

- anything that lets a remote page or host reach the API (origin-check bypass, rebinding)
- stored content served back in a way that executes script against the API origin
- config or path handling that lets a task escape its worktree, or that redirects the OAuth
  bearer token off-host
- credential leakage into logs, transcripts, or run artifacts

Out of scope:

- "a task can run arbitrary code" — that is the product, see the top of this file
- anything requiring a process already running as your user
- the deliberate self-defeats listed under **The trust boundary**
