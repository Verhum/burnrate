#!/usr/bin/env bash
#
# The one entry point for secret scanning, so its callers cannot drift:
# .githooks/pre-commit (staged), .githooks/pre-push (push), `make secrets`
# (tree), and the CI `secrets` job (history).
#
# Usage: scripts/secret-scan.sh [staged|tree|history|push <rev-list args>...]
#   staged   what `git commit` is about to record
#   tree     the working tree as it stands
#   history  every commit reachable from any ref
#   push     only the commits a push would send, given `git log` selectors
#
# A missing gitleaks warns and exits 0 rather than failing. That is deliberate:
# the hook and `make check` must not break a fresh clone that has not installed
# a Go-adjacent tool yet. Enforcement lives in CI, which installs gitleaks
# unconditionally and cannot be stepped around with `git commit --no-verify`.

set -euo pipefail

mode="${1:-tree}"
root="$(cd "$(dirname "$0")/.." && pwd)"
# push mode scans the repository the caller is pushing from, which is not
# necessarily this script's own checkout — a hook may be shared across worktrees
# via core.hooksPath. Every other mode is about this tree.
if [ "$mode" != push ]; then
	cd "$root"
fi

if ! command -v gitleaks >/dev/null 2>&1; then
	printf 'secret-scan: gitleaks not installed — skipping local scan.\n' >&2
	printf 'secret-scan: install with `brew install gitleaks`; CI enforces regardless.\n' >&2
	exit 0
fi

case "$mode" in
staged) args=(git --staged) ;;
tree) args=(dir .) ;;
history) args=(git --log-opts=--all) ;;
push)
	shift
	# No selectors means nothing new is being pushed.
	[ "$#" -eq 0 ] && exit 0
	args=(git "--log-opts=$*")
	;;
*)
	printf 'secret-scan: unknown mode: %s (expected staged|tree|history|push)\n' "$mode" >&2
	exit 2
	;;
esac

if gitleaks "${args[@]}" --no-banner --redact --config "$root/.gitleaks.toml"; then
	exit 0
fi

cat >&2 <<'EOF'

secret-scan: a credential was detected above.

  Rotate it first. Deleting the line, amending the commit, or rewriting history
  does not revoke anything — GitHub keeps unreachable objects and its own
  refs/pull/* indefinitely, so anything that reaches a remote stays fetchable by
  SHA. Rotation is the only thing that makes a leaked value worthless.

  Then replace the literal with an env-var reference: "${UPLOAD_SECRET:?set it}".

  A genuine false positive is annotated in .gitleaks.toml with the reason it is
  not a secret — never with a bare path exclusion.
EOF
exit 1
