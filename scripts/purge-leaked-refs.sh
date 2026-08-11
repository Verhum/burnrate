#!/usr/bin/env bash
#
# Finds every local ref that still reaches a leaked credential and clears the
# ones that can be cleared without losing work.
#
# A history rewrite on the remote does not touch a clone: tags, stashes and
# branches that were never re-fetched keep pointing at the pre-rewrite commits,
# so the value is still in that clone's object store and one `git push --tags`
# would put it back on the remote. This is the cleanup for such a clone. It is
# not a substitute for rotating the credential — nothing here unpublishes
# anything, and GitHub keeps unreachable objects and refs/pull/* regardless.
#
# Usage: scripts/purge-leaked-refs.sh [--apply]
#   default   report what reaches a leak and what would be done
#   --apply   do it: export stashes to patches, drop stale tags, repoint
#             branches that exist on the remote, expire reflogs, gc
#
# Offending commits come from gitleaks, not from a hardcoded value, so this
# works for any future incident.

set -euo pipefail

apply=0
case "${1:-}" in
'') ;;
--apply) apply=1 ;;
*)
	printf 'usage: %s [--apply]\n' "$0" >&2
	exit 2
	;;
esac

root="$(git rev-parse --show-toplevel)"
config="$(cd "$(dirname "$0")/.." && pwd)/.gitleaks.toml"
cd "$root"

if ! command -v gitleaks >/dev/null 2>&1; then
	printf 'purge-leaked-refs: gitleaks is required (brew install gitleaks).\n' >&2
	exit 2
fi

report="$(mktemp)"
trap 'rm -f "$report"' EXIT
gitleaks git --log-opts=--all --no-banner --redact \
	--config "$config" --report-format json --report-path "$report" >/dev/null 2>&1 || true

commits="$(sed -n 's/.*"Commit": *"\([0-9a-f]\{7,\}\)".*/\1/p' "$report" | sort -u)"
if [ -z "$commits" ]; then
	printf 'purge-leaked-refs: no leak reachable from any ref — nothing to do.\n'
	exit 0
fi

printf 'offending commits:\n'
for c in $commits; do
	printf '  %s  %s\n' "${c:0:12}" "$(git log -1 --format=%s "$c" 2>/dev/null || echo '<gone>')"
done

refs="$(for c in $commits; do
	git for-each-ref --contains "$c" --format='%(refname)' refs/heads refs/tags refs/stash refs/remotes 2>/dev/null
done | sort -u)"

printf '\nlocal refs reaching them:\n'
printf '%s\n' "$refs" | sed 's/^/  /'

if [ "$apply" -eq 0 ]; then
	printf '\nre-run with --apply to clear them.\n'
	exit 0
fi

git fetch --quiet --prune origin

printf '\n'
while IFS= read -r ref; do
	[ -n "$ref" ] || continue
	case "$ref" in
	refs/tags/*)
		tag="${ref#refs/tags/}"
		if [ -n "$(git ls-remote --tags origin "refs/tags/$tag")" ]; then
			printf 'KEEP  %s — the remote has this tag; re-cut it there first\n' "$ref"
		else
			git tag -d "$tag" >/dev/null
			printf 'DROP  %s — local-only, so nothing is lost\n' "$ref"
		fi
		;;
	refs/heads/*)
		branch="${ref#refs/heads/}"
		if git rev-parse --quiet --verify "refs/remotes/origin/$branch" >/dev/null; then
			git branch -f "$branch" "origin/$branch" >/dev/null
			printf 'RESET %s -> origin/%s\n' "$ref" "$branch"
		else
			printf 'SKIP  %s — no origin/%s, so this may hold unpushed work. Handle by hand.\n' "$ref" "$branch"
		fi
		;;
	refs/stash)
		# A stash commit's parent is the commit it was taken against, which is
		# what keeps pre-rewrite history alive. The diffs are worth keeping even
		# when that anchor is not, so they leave as patches.
		out="$(git rev-parse --git-common-dir)/stash-patches"
		mkdir -p "$out"
		i=0
		while git rev-parse --quiet --verify "stash@{$i}" >/dev/null 2>&1; do
			git stash show -p "stash@{$i}" >"$out/stash-$i.patch"
			printf 'SAVE  stash@{%s} -> %s/stash-%s.patch\n' "$i" "$out" "$i"
			i=$((i + 1))
		done
		if [ "$i" -eq 0 ]; then
			# The stack lives in refs/stash's reflog, and a reflog does not
			# survive being fetched into another clone — so stash@{0} can fail
			# to resolve on a ref that does hold an entry. Never clear without
			# writing that diff out.
			git stash show -p refs/stash >"$out/stash-0.patch"
			printf 'SAVE  refs/stash -> %s/stash-0.patch (no reflog: one entry)\n' "$out"
			i=1
		fi
		git stash clear
		printf 'DROP  refs/stash (%s entries, all exported)\n' "$i"
		;;
	refs/remotes/*)
		printf 'KEEP  %s — a remote-tracking ref; `git fetch --prune` is what clears it\n' "$ref"
		;;
	esac
done <<<"$refs"

git reflog expire --expire=now --expire-unreachable=now --all
git gc --prune=now --quiet

printf '\nverifying:\n'
# Not secret-scan.sh, which scans its own checkout; the repo being cleaned is cwd.
if gitleaks git --log-opts=--all --no-banner --redact --config "$config"; then
	printf 'purge-leaked-refs: clean — no leak reachable from any ref.\n'
else
	printf 'purge-leaked-refs: still reachable; see the SKIP lines above.\n' >&2
	exit 1
fi
