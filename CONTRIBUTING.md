# Contributing

Thanks for looking. burnrate is a personal tool that got useful enough to publish, so
maintenance is best-effort and the bar for new surface area is high.

Read [SECURITY.md](SECURITY.md) first if you are touching `internal/server/`. Do not report
security issues here — see that file for the address.

## Setup

macOS, Go 1.23+, Node 20+, and the `claude` CLI on your `PATH`. Also
`brew install gitleaks` — without it the secret scan below quietly skips.

```bash
make bootstrap    # web deps, verify the tree compiles, enable the tracked git hooks
make dev          # Go server + Next.js dev server
make go-dev-dry   # daemon only, BURNRATE_DRYRUN=1 — stubs `claude`, never spends quota
```

`make bootstrap` points `core.hooksPath` at `.githooks/`, which is how the pre-commit secret
scan starts running. It is per-clone and untracked, so a new worktree needs its own
`make bootstrap`.

Use `make go-dev-dry` for anything you do not need a real agent for. A non-dry run spends real
subscription quota and will push branches.

## Before you open a PR

```bash
make check        # lint + test + web build. This is the gate CI runs.
make test-race    # if you touched the scheduler, runner, or SSE hub
```

Tests use real SQLite via `store.Open(":memory:")`. There are no mocks for the DB layer, so
write tests against a real store rather than introducing one.

## What the review will ask

- **Does the change belong in `internal/scheduling`?** Scheduling policy lives there and is
  pure — no DB, network, clock, or logging. The daemon around it renders the same `Plan` the
  UI does, and that shared derivation is the only reason they cannot disagree.
- **Did you add a test that fails without the change?** A test that passes either way is not
  evidence.
- **Is the comment carrying its weight?** See below.
- **Did you touch a documented trap?** `CLAUDE.md` lists the invariants that span files. If
  your change makes one of them wrong, update it in the same commit.

## Documentation

The code is the documentation. There are three places anything belongs:

- **`ai.md`** — the map. Update it when you add, rename, or remove a route, message type,
  table, config key, env var, CLI subcommand, or port. Terse and greppable: what exists and
  where, never how it works.
- **`CLAUDE.md`** — traps only. Things the code cannot tell you: a file that must exist or the
  build breaks, an invariant spanning several files. If you could learn it by reading the
  code, it does not go here.
- **`README.md`** — for humans. Touch it only when you change how someone installs,
  configures, or runs burnrate.

A comment that restates the next line is noise; prefer a clearer name. Comment for the things
code cannot show — a protocol quirk, a non-obvious invariant, why the obvious approach fails.
**Deleting stale documentation is a documentation update** — if you read something that is now
wrong, delete it in the same commit.

No changelog entries, and no doc section added to prove you thought about docs.

## Licensing

By contributing you agree your contribution is licensed under BSD-3-Clause, matching
[LICENSE](LICENSE).

The repo URL in the copyright line is load-bearing, not decoration: clauses 1 and 2 require
retaining that notice, so it is what makes a fork credit *this repo* rather than a bare name.
Don't tidy it out.
