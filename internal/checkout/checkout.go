// Package checkout switches a task's branches into the user's own clones.
//
// Every run works in a throwaway worktree, which is the right thing for the
// agent and the wrong thing for trying the result: exercising a change usually
// means one local dev server against one clone. This puts the branches where
// that server already points.
package checkout

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/git"
)

type Status string

const (
	// CheckedOut: the clone is now on the branch. Already: it was already there.
	// Skipped: another PR of this task claimed the same clone. Error: detail says
	// why nothing was changed.
	StatusCheckedOut Status = "checked_out"
	StatusAlready    Status = "already"
	StatusSkipped    Status = "skipped"
	StatusError      Status = "error"
)

type Result struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Task checks out every branch the task produced, one per repo.
//
// A repo can hold only one branch at a time, so when a task produced several
// PRs in the same repo the newest wins and the others come back Skipped rather
// than being silently dropped or fighting each other.
func Task(ctx context.Context, baseCodeDir string, prs []domain.TaskPR) []Result {
	results := make([]Result, 0, len(prs))
	claimed := map[string]string{} // repo -> branch already checked out

	for _, pr := range newestPerRepo(prs) {
		if pr.Repo == "" || pr.Branch == "" {
			continue
		}
		r := Result{Repo: pr.Repo, Branch: pr.Branch}
		if other, ok := claimed[pr.Repo]; ok {
			r.Status = StatusSkipped
			r.Detail = fmt.Sprintf("%s is already on %s; a clone holds one branch at a time", pr.Repo, other)
			results = append(results, r)
			continue
		}

		dir, err := LocalClone(ctx, baseCodeDir, pr.Repo)
		if err != nil {
			r.Status = StatusError
			r.Detail = err.Error()
			results = append(results, r)
			continue
		}
		r.Path = dir

		status, detail, err := checkoutBranch(ctx, dir, pr.Branch)
		if err != nil {
			r.Status = StatusError
			r.Detail = err.Error()
			results = append(results, r)
			continue
		}
		claimed[pr.Repo] = pr.Branch
		r.Status = status
		r.Detail = detail
		results = append(results, r)
	}
	return results
}

// newestPerRepo keeps the highest-id PR per repo, preserving first-seen order so
// the response reads in the order the task's PRs were recorded. Skipped rows are
// emitted for the losers by the caller.
func newestPerRepo(prs []domain.TaskPR) []domain.TaskPR {
	best := map[string]domain.TaskPR{}
	var order []string
	for _, pr := range prs {
		if pr.Repo == "" || pr.Branch == "" {
			continue
		}
		prev, ok := best[pr.Repo]
		if !ok {
			order = append(order, pr.Repo)
		}
		if !ok || pr.ID > prev.ID {
			best[pr.Repo] = pr
		}
	}
	out := make([]domain.TaskPR, 0, len(order))
	for _, repo := range order {
		out = append(out, best[repo])
	}
	// Losers, so the caller can report them.
	for _, pr := range prs {
		if pr.Repo == "" || pr.Branch == "" {
			continue
		}
		if best[pr.Repo].ID != pr.ID {
			out = append(out, pr)
		}
	}
	return out
}

// LocalClone finds the user's checkout of owner/name under baseCodeDir. The
// conventional path (baseCodeDir/name) is tried first and verified against the
// clone's own origin, because a directory name is a guess and the remote is the
// fact; failing that, one level of baseCodeDir is scanned.
func LocalClone(ctx context.Context, baseCodeDir, repo string) (string, error) {
	if baseCodeDir == "" {
		return "", fmt.Errorf("base_code_dir is not configured")
	}
	want := strings.ToLower(repo)

	candidate := filepath.Join(baseCodeDir, path.Base(repo))
	if isRepoFor(ctx, candidate, want) {
		return candidate, nil
	}

	entries, err := os.ReadDir(baseCodeDir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", baseCodeDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(baseCodeDir, e.Name())
		if dir == candidate {
			continue
		}
		if isRepoFor(ctx, dir, want) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no clone of %s under %s", repo, baseCodeDir)
}

func isRepoFor(ctx context.Context, dir, wantLowerSlug string) bool {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	return strings.ToLower(git.OriginSlug(ctx, dir)) == wantLowerSlug
}

// checkoutBranch switches dir to branch without ever discarding local state: it
// refuses on a dirty tree, and it does not reset an existing local branch to
// origin — the user may have commits there that only exist locally.
func checkoutBranch(ctx context.Context, dir, branch string) (Status, string, error) {
	if git.CurrentBranch(ctx, dir) == branch {
		return StatusAlready, "already on " + branch, nil
	}
	if git.HasUncommittedChanges(ctx, dir) {
		return "", "", fmt.Errorf("uncommitted changes in %s; not switching", dir)
	}
	if out, err := git.Run(ctx, dir, "git", "fetch", "origin", branch); err != nil {
		return "", "", fmt.Errorf("fetch %s: %w (%s)", branch, err, out)
	}

	if localBranchExists(ctx, dir, branch) {
		if out, err := git.Run(ctx, dir, "git", "checkout", branch); err != nil {
			return "", "", fmt.Errorf("checkout %s: %w (%s)", branch, err, out)
		}
		return StatusCheckedOut, "switched to existing local " + branch, nil
	}
	if out, err := git.Run(ctx, dir, "git", "checkout", "-b", branch, "--track", "origin/"+branch); err != nil {
		return "", "", fmt.Errorf("checkout -b %s: %w (%s)", branch, err, out)
	}
	return StatusCheckedOut, "created " + branch + " tracking origin/" + branch, nil
}

func localBranchExists(ctx context.Context, dir, branch string) bool {
	_, err := git.Run(ctx, dir, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}
