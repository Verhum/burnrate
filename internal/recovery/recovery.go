package recovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/git"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

func Sweep(ctx context.Context, st *store.Store, cfg config.Config, logger *log.Logger) error {
	runs, err := st.RunsByStatus("errored", "timed_out", "rate_limited")
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	for _, r := range runs {
		if r.WorktreePath == "" {
			continue
		}
		// Safety: only ever push branches burnrate created. Never touch a
		// branch that lacks the burnrate/ prefix.
		if !strings.HasPrefix(r.Branch, "burnrate/") {
			logger.Warnf("recovery: skipping run %d, branch %q lacks burnrate/ prefix", r.ID, r.Branch)
			continue
		}
		if _, err := os.Stat(r.WorktreePath); err != nil {
			continue
		}

		task, err := st.GetTask(r.TaskID)
		if err != nil {
			logger.Warnf("recovery: task %d not found: %v", r.TaskID, err)
			continue
		}

		defaultBranch, _ := git.DefaultBranch(ctx, r.RepoPath)
		if defaultBranch == "" {
			defaultBranch = "develop"
		}

		has, err := git.HasUnpushedCommits(ctx, r.WorktreePath, defaultBranch)
		if err != nil {
			continue
		}
		if !has {
			existing, _ := git.Run(ctx, r.WorktreePath, "gh", "pr", "view", r.Branch, "--json", "url", "-q", ".url")
			if strings.HasPrefix(strings.TrimSpace(existing), "https://") {
				prURL := strings.TrimSpace(existing)
				st.FinishRun(r.ID, r.Status, r.CostUSD, r.NumTurns, prURL, r.Error, r.ResultText)
				st.UpsertTaskPR(r.TaskID, r.ID, r.AgentRepo, r.Branch, prURL, r.AgentWorkedIn)
				logger.Infof("recovery: run %d → existing PR %s", r.ID, prURL)
			}
			continue
		}

		logger.Infof("recovery: run %d has unpushed commits, pushing branch %s", r.ID, r.Branch)
		if err := git.PushBranch(ctx, r.WorktreePath, r.Branch); err != nil {
			logger.Warnf("recovery: push failed for run %d: %v", r.ID, err)
			continue
		}

		prURL := findOrCreatePR(ctx, r.WorktreePath, r.Branch, defaultBranch, task.Title, logger)
		if prURL != "" {
			st.FinishRun(r.ID, r.Status, r.CostUSD, r.NumTurns, prURL, r.Error, r.ResultText)
			st.UpsertTaskPR(r.TaskID, r.ID, r.AgentRepo, r.Branch, prURL, r.AgentWorkedIn)
			logger.Infof("recovery: run %d → PR %s", r.ID, prURL)
		}
	}

	return nil
}

// CleanupStale removes the working directories of runs that are over. Both
// layouts are swept: a managed run's git worktree under WorktreeRoot, and an
// agent-directed run's workdir under AgentWorkRoot, which holds worktrees the
// agent added itself. Cleanup on task completion is fire-and-forget and can
// legitimately decline (uncommitted work, an unreachable owning checkout), so
// this is the only thing that ever retries it.
func CleanupStale(ctx context.Context, st *store.Store, cfg config.Config, logger *log.Logger) error {
	active, err := activeWorktrees(st)
	if err != nil {
		return err
	}
	if err := cleanupManagedWorktrees(ctx, cfg.WorktreeRoot, active, logger); err != nil {
		return err
	}
	return cleanupAgentWorkdirs(ctx, cfg.AgentWorkRoot(), active, logger)
}

// activeWorktrees collects every worktree path that must survive a sweep:
// only runs that are actively in flight. Completed and interrupted runs have
// their worktrees removed eagerly (work is saved in Git), so only running
// processes need protection here.
func activeWorktrees(st *store.Store) (map[string]bool, error) {
	active := make(map[string]bool)

	runs, err := st.RunsByStatus("starting", "running", "resuming")
	if err != nil {
		return nil, err
	}
	for _, r := range runs {
		if r.WorktreePath != "" {
			active[r.WorktreePath] = true
		}
	}

	return active, nil
}

func cleanupManagedWorktrees(ctx context.Context, root string, active map[string]bool, logger *log.Logger) error {
	entries, err := readDirs(root)
	if err != nil {
		return err
	}
	for _, name := range entries {
		wtPath := filepath.Join(root, name)
		if active[wtPath] {
			continue
		}

		// Each worktree may belong to a different repo now that a task can span
		// several, so ask the worktree which checkout owns it rather than
		// assuming a single configured repo.
		owner, err := git.OwningRepo(ctx, wtPath)
		if err != nil {
			logger.Warnf("cleanup: cannot resolve owning repo for %s: %v", name, err)
			continue
		}

		logger.Infof("cleanup: checkpointing stale worktree %s before removal", name)
		if err := git.Checkpoint(ctx, wtPath, 0); err != nil {
			logger.Warnf("cleanup: checkpoint failed for %s: %v", name, err)
		}
		git.RemoveWorktree(ctx, owner, wtPath)
	}
	return nil
}

func cleanupAgentWorkdirs(ctx context.Context, root string, active map[string]bool, logger *log.Logger) error {
	entries, err := readDirs(root)
	if err != nil {
		return err
	}
	for _, name := range entries {
		workdir := filepath.Join(root, name)
		if active[workdir] {
			continue
		}
		logger.Infof("cleanup: tearing down stale agent workdir %s", name)
		kept, err := git.RemoveAgentWorkdir(ctx, workdir, 0)
		switch {
		case errors.Is(err, git.ErrUnbackedWork):
			// The normal ending for a task with no repo: its deliverables are
			// loose files, and the next iteration of the task lands in this same
			// path. Every sweep sees it again, so it is not a warning.
			logger.Infof("cleanup: keeping agent workdir %s, no branch backs the work in it", name)
		case kept != "":
			if kept == workdir {
				logger.Warnf("cleanup: keeping agent workdir %s: %v", name, err)
			} else {
				logger.Warnf("cleanup: keeping agent workdir %s (%s: %v)", name, kept, err)
			}
		case err != nil:
			logger.Warnf("cleanup: removing agent workdir %s: %v", name, err)
		}
	}
	return nil
}

func readDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func findOrCreatePR(ctx context.Context, dir, branch, baseBranch, title string, logger *log.Logger) string {
	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	existing, _ := git.Run(cmdCtx, dir, "gh", "pr", "view", branch, "--json", "url", "-q", ".url")
	if strings.HasPrefix(strings.TrimSpace(existing), "https://") {
		return strings.TrimSpace(existing)
	}

	out, err := git.Run(cmdCtx, dir, "gh", "pr", "create",
		"--draft",
		"--head", branch,
		"--base", baseBranch,
		"--title", title,
		"--body", "Automated PR by burnrate (recovered).",
	)
	if err != nil {
		logger.Warnf("recovery: PR creation failed: %v (%s)", err, out)
		return ""
	}
	url := strings.TrimSpace(out)
	if strings.HasPrefix(url, "https://") {
		return url
	}
	return ""
}
