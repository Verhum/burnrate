#!/usr/bin/env bash
#
# Make a fresh clone or `git worktree add` fully workable: Go builds/tests, the
# web build, and the linters. Idempotent and safe to run on every entry — it is
# a no-op costing ~50ms once a tree is set up.
#
# Usage: scripts/bootstrap.sh [deps|check]
#   deps   (default) ensure web/node_modules matches package-lock.json
#   check  deps, then verify the Go tree compiles
#
# Background: `web/out/` and `web/node_modules/` are both generated and both
# gitignored, so a new worktree started out unable to do anything:
#
#   $ go test ./...
#   web/web.go:9:12: pattern all:out: no matching files found
#   FAIL github.com/Verhum/burnrate/web            [setup failed]
#   FAIL github.com/Verhum/burnrate/internal/server [setup failed]
#   FAIL github.com/Verhum/burnrate/cmd/burnrate    [setup failed]
#
# The `out/` half is now fixed at the source: `web/out/.gitkeep` is tracked, so
# the `//go:embed all:out` pattern always matches and Go work needs no Node
# toolchain at all. Only `node_modules` needs this script, and only for frontend
# work. `npm ci` against a warm npm cache takes ~5s, which is why this installs
# rather than symlinking or cloning from a sibling checkout: a shared or copied
# node_modules risks silently disagreeing with this tree's package-lock.json,
# and saves no measurable time. (A symlinked `web/out` is not even an option —
# `//go:embed` rejects symlinks outright: "cannot embed irregular file".)

set -euo pipefail

mode="${1:-deps}"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

web="$root/web"
lock="$web/package-lock.json"
stamp="$web/node_modules/.burnrate-bootstrap-stamp"

log() { printf 'bootstrap: %s\n' "$1" >&2; }

ensure_deps() {
	if [ ! -f "$lock" ]; then
		log "no web/package-lock.json — skipping web deps"
		return 0
	fi

	# The stamp records the lockfile that produced this node_modules, so a
	# lockfile change on a branch switch reinstalls instead of going stale.
	want="$(shasum -a 256 "$lock" | cut -d' ' -f1)"
	if [ -x "$web/node_modules/.bin/next" ] && [ -f "$stamp" ] &&
		[ "$(cat "$stamp")" = "$want" ]; then
		log "web deps already match package-lock.json"
		return 0
	fi

	log "installing web deps (npm ci)"
	(cd "$web" && npm ci)
	printf '%s\n' "$want" >"$stamp"
}

# The tracked placeholder is what keeps `//go:embed all:out` satisfiable, and
# `next build` wipes web/out/ on its way in, so it needs restoring after a build
# as well as in a tree where one never ran. It is deliberately zero-byte: `touch`
# then restores it byte-identically, leaving `git status` clean without shelling
# out to git or duplicating content anywhere. `npm run build` does the same via
# its `keep-embed-target` script; this covers the rest.
ensure_embed_target() {
	if [ ! -e "$web/out/.gitkeep" ]; then
		log "restoring web/out/.gitkeep (required by //go:embed all:out)"
		mkdir -p "$web/out"
		touch "$web/out/.gitkeep"
	fi
}

# .git/hooks is per-clone and untracked, so a tracked hook only runs once
# core.hooksPath points at it. Set unconditionally when unset; leave an existing
# value alone, since someone who configured their own path meant it.
ensure_hooks() {
	if [ -n "$(git config --local --get core.hooksPath || true)" ]; then
		return 0
	fi
	log "enabling tracked git hooks (core.hooksPath=.githooks)"
	git config --local core.hooksPath .githooks
}

ensure_embed_target
ensure_hooks

case "$mode" in
deps)
	ensure_deps
	;;
check)
	ensure_deps
	log "verifying the Go tree compiles"
	go build ./...
	log "ok — go build ./... clean"
	;;
*)
	log "unknown mode: $mode (expected 'deps' or 'check')"
	exit 2
	;;
esac
