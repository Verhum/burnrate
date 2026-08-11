#!/usr/bin/env bash
#
# Builds the repository that gets published: the current tree as one commit,
# with no history behind it.
#
# This is the alternative to scrubbing history. A rewrite leaves things only
# GitHub can remove — unreachable objects, and refs/pull/* pointing at
# pre-rewrite commits — so the old repo can never be proven clean by pushing.
# A snapshot has nothing behind its single commit to be unclean, and a new
# remote has no pull refs at all. Nothing is deleted: the private repo stays as
# the archive of history, PRs and review comments.
#
# Usage: scripts/publish-snapshot.sh [--ref <ref>] [--out <dir>] [--force]
#   --ref     commit to publish (default HEAD)
#   --out     where to build it (default $TMPDIR/burnrate-snapshot)
#   --force   overwrite a non-empty --out
#
# It stops before creating or pushing to any remote and prints those commands
# instead: publishing is the one step that cannot be undone.

set -euo pipefail

ref=HEAD
out="${TMPDIR:-/tmp}/burnrate-snapshot"
force=0

while [ $# -gt 0 ]; do
	case "$1" in
	--ref)
		ref="${2:?--ref needs a value}"
		shift 2
		;;
	--out)
		out="${2:?--out needs a value}"
		shift 2
		;;
	--force)
		force=1
		shift
		;;
	*)
		printf 'usage: %s [--ref <ref>] [--out <dir>] [--force]\n' "$0" >&2
		exit 2
		;;
	esac
done

root="$(git rev-parse --show-toplevel)"
cd "$root"

commit="$(git rev-parse --verify "$ref^{commit}")"

# The source is a commit, never the working tree. A dirty tree or a stray
# untracked file is exactly how a credential reaches a "clean" snapshot, and
# `git archive` cannot see either.
if ! git diff --quiet "$commit" -- || ! git diff --cached --quiet "$commit" --; then
	printf 'publish-snapshot: the working tree differs from %s.\n' "${commit:0:12}" >&2
	printf '  Only the commit is published. Commit first, or pass --ref explicitly.\n' >&2
	exit 1
fi

if [ -e "$out" ] && [ -n "$(ls -A "$out" 2>/dev/null)" ] && [ "$force" -eq 0 ]; then
	printf 'publish-snapshot: %s exists and is not empty; pass --force to replace it.\n' "$out" >&2
	exit 1
fi
rm -rf "$out"
mkdir -p "$out"

git archive --format=tar "$commit" | tar -x -C "$out"

# web/out is gitignored but //go:embed all:out needs the directory to exist, so
# a tracked zero-byte placeholder keeps three packages compiling. `git archive`
# ships tracked files, so it survives — but losing it turns the snapshot into a
# repo that does not build, which is a bad first impression and a slow one to
# diagnose. See CLAUDE.md.
if [ ! -f "$out/web/out/.gitkeep" ]; then
	printf 'publish-snapshot: web/out/.gitkeep missing from the export — the snapshot will not compile.\n' >&2
	exit 1
fi

subject="$(git log -1 --format=%s "$commit")"
git -C "$out" init --quiet --initial-branch=main
git -C "$out" add -A
GIT_AUTHOR_DATE="$(git log -1 --format=%aI "$commit")" \
	GIT_COMMITTER_DATE="$(git log -1 --format=%aI "$commit")" \
	git -C "$out" commit --quiet --file=- <<EOF
Initial public release

Snapshot of the private development repository at $(printf '%.12s' "$commit")
("$subject"), published without its history.

The history is not withheld for anything interesting: it is one developer's
working record, it once contained a credential that has since been rotated,
and GitHub keeps pull-request refs pointing at pre-rewrite commits forever.
Publishing a snapshot is the only way to be certain about what is in this
repository. Development from here is public.
EOF

# Deliberately not scripts/secret-scan.sh, which exits 0 when gitleaks is
# absent so that a missing tool never blocks a commit. For the one action that
# cannot be taken back, an unscanned snapshot is a refusal, not a warning.
if ! command -v gitleaks >/dev/null 2>&1; then
	printf 'publish-snapshot: gitleaks is required to sign off a snapshot (brew install gitleaks).\n' >&2
	printf '  The snapshot is built at %s but has NOT been scanned. Do not push it.\n' "$out" >&2
	exit 2
fi

printf '\nscanning the snapshot:\n'
# --exit-code separates "found a credential" from "the scanner itself failed",
# which gitleaks otherwise reports with the same status 1. A snapshot that was
# never scanned must not be mistaken for one that came back dirty, or for one
# that came back clean.
scan=0
gitleaks git --log-opts=--all --no-banner --redact --exit-code 7 \
	--config "$out/.gitleaks.toml" "$out" || scan=$?
case "$scan" in
0) ;;
7)
	printf 'publish-snapshot: the snapshot contains a credential. Fix it in %s first.\n' "$root" >&2
	exit 1
	;;
*)
	printf 'publish-snapshot: gitleaks failed (exit %s); the snapshot is unscanned.\n' "$scan" >&2
	exit 2
	;;
esac

refs="$(git -C "$out" for-each-ref --format='%(refname)')"
if [ "$refs" != "refs/heads/main" ]; then
	printf 'publish-snapshot: unexpected refs in the snapshot:\n%s\n' "$refs" >&2
	exit 1
fi
count="$(git -C "$out" rev-list --count --all)"
if [ "$count" != "1" ]; then
	printf 'publish-snapshot: expected 1 commit, found %s.\n' "$count" >&2
	exit 1
fi

# Not a failure: a home-directory path can be a legitimate example in prose or a
# realistic fixture in a test. Worth seeing the list before it is public.
# `|| true` because git grep exits 1 on no match, which pipefail turns into a
# silent abort right at the end of an otherwise successful run.
personal="$(git -C "$out" grep -Il "$HOME" -- . 2>/dev/null || true)"
if [ -n "$personal" ]; then
	printf '\n%s file(s) still contain your home directory path:\n' \
		"$(printf '%s\n' "$personal" | wc -l | tr -d ' ')"
	printf '%s\n' "$personal" | sed 's/^/  /'
fi

printf '\nsnapshot ready: %s\n' "$out"
printf '  1 commit, %s files, no tags, no remote.\n' \
	"$(git -C "$out" ls-files | wc -l | tr -d ' ')"
cat <<EOF

Review it, then publish by hand — this script will not create a remote for you:

  cd $out
  make bootstrap && make check          # it must build from a fresh clone
  gh repo create <owner>/<name> --public --source=. --remote=origin --push

Afterwards, on the new repo: turn on secret scanning with push protection
(free for public repos, server-side, and --no-verify cannot reach it), and make
the \`secrets\` job a required status check.
EOF
