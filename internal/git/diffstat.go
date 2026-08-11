package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// DiffStat is the churn of a branch: how many files it touched and how many
// lines it added and removed.
type DiffStat struct {
	FilesChanged int
	Added        int
	Removed      int
}

// Changed is the total line churn, the denominator of cost per line of code.
func (d DiffStat) Changed() int { return d.Added + d.Removed }

// BranchDiffStat measures the work a worktree's branch has done: the diff from
// its merge-base with the default branch up to HEAD, so commits that landed on
// the default branch after the worktree was created are not counted as the
// agent's output.
//
// Only committed work counts. An agent that leaves changes uncommitted has not
// delivered them, and the runner's own checkpoint commits any such leftovers
// before this is called on the interrupted path.
//
// The default branch is resolved from the worktree itself; pass a non-empty
// base to skip that lookup. Returns an error rather than a zero stat when the
// diff cannot be taken, so callers can tell "no lines" from "did not measure".
func BranchDiffStat(ctx context.Context, worktree, base string) (DiffStat, error) {
	ref, err := diffBaseRef(ctx, worktree, base)
	if err != nil {
		return DiffStat{}, err
	}

	// The three-dot form diffs merge-base(ref, HEAD)..HEAD, which is exactly the
	// branch's own contribution and needs no separate merge-base call.
	out, err := Run(ctx, worktree, "git", "diff", "--numstat", ref+"...HEAD")
	if err != nil {
		return DiffStat{}, fmt.Errorf("diff %s...HEAD in %s: %w", ref, worktree, err)
	}
	return parseNumstat(out), nil
}

// diffBaseRef picks the ref to diff against, preferring the remote-tracking
// branch (stable even if the local branch has moved on) and falling back to the
// local one.
func diffBaseRef(ctx context.Context, worktree, base string) (string, error) {
	if base == "" {
		b, err := DefaultBranch(ctx, worktree)
		if err != nil {
			return "", fmt.Errorf("resolve default branch for %s: %w", worktree, err)
		}
		base = b
	}
	if strings.Contains(base, "/") {
		// Already qualified (e.g. "origin/main").
		return base, nil
	}
	for _, candidate := range []string{"origin/" + base, base} {
		if _, err := Run(ctx, worktree, "git", "rev-parse", "--verify", "--quiet", candidate+"^{commit}"); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no such base ref %q in %s", base, worktree)
}

// parseNumstat reads `git diff --numstat` output. Binary files report "-" for
// both counts and contribute a file but no lines.
func parseNumstat(out string) DiffStat {
	var st DiffStat
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		st.FilesChanged++
		if n, err := strconv.Atoi(fields[0]); err == nil {
			st.Added += n
		}
		if n, err := strconv.Atoi(fields[1]); err == nil {
			st.Removed += n
		}
	}
	return st
}
