package recovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

// agentRun creates a task in the given status with an agent-directed run — empty
// repo_path, a workdir under agentwork/ — plus a worktree inside that workdir on
// its own branch. Returns the workdir and the checkout that owns the worktree.
func agentRun(t *testing.T, st *store.Store, dataDir, status string, taskID int) (workdir, repoDir string) {
	t.Helper()

	repoDir = filepath.Join(t.TempDir(), "repo")
	mustGit(t, "", "init", repoDir)
	mustGit(t, repoDir, "config", "user.name", "Test")
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "commit", "--allow-empty", "-m", "init")

	task, err := st.CreateTask(fmt.Sprintf("task %d", taskID), "prompt", "", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	workdir = filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID))
	if err := os.MkdirAll(workdir, 0755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	mustGit(t, repoDir, "worktree", "add", filepath.Join(workdir, "wt"),
		"-b", fmt.Sprintf("burnrate/%d-work", task.ID))

	run, err := st.CreateRun(task.ID, workdir, "", "", "w1", 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "", "", "")
	if err := st.SetTaskStatus(task.ID, status); err != nil {
		t.Fatalf("set task status: %v", err)
	}
	return workdir, repoDir
}

func newStore(t *testing.T) (*store.Store, config.Config) {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, config.Config{DataDir: dataDir, WorktreeRoot: filepath.Join(dataDir, "worktrees")}
}

func worktreeListed(t *testing.T, repoDir string) bool {
	t.Helper()
	out, _ := exec.Command("git", "-C", repoDir, "worktree", "list").CombinedOutput()
	return strings.Contains(string(out), "/wt")
}

// Every burnrate task since agent-directed mode landed stores its run under
// agentwork/, not worktrees/ — a sweep that reads only WorktreeRoot never sees
// the real workload, so a workdir that cleanup-on-completion declined to remove
// is leaked with nothing left to retry it.
func TestCleanupStale_RemovesFinishedAgentWorkdir(t *testing.T) {
	st, cfg := newStore(t)
	workdir, repoDir := agentRun(t, st, cfg.DataDir, "done", 1)

	if err := CleanupStale(context.Background(), st, cfg, log.New("", false)); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(workdir); err == nil {
		t.Fatal("agent workdir of a done task was not swept")
	}
	if worktreeListed(t, repoDir) {
		t.Fatal("worktree still attached after sweep")
	}
}

// pr_created worktrees are cleaned up eagerly by the runner (work is safe in
// Git), so the recovery sweep removes any that remain.
func TestCleanupStale_RemovesPRCreatedAgentWorkdir(t *testing.T) {
	st, cfg := newStore(t)
	workdir, repoDir := agentRun(t, st, cfg.DataDir, "pr_created", 1)

	if err := CleanupStale(context.Background(), st, cfg, log.New("", false)); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(workdir); err == nil {
		t.Fatal("agent workdir of a pr_created task was not swept")
	}
	if worktreeListed(t, repoDir) {
		t.Fatal("worktree still attached after sweep")
	}
}

func TestCleanupStale_KeepsRunningAgentWorkdir(t *testing.T) {
	st, cfg := newStore(t)

	dataDir := cfg.DataDir
	repoDir := filepath.Join(t.TempDir(), "repo")
	mustGit(t, "", "init", repoDir)
	mustGit(t, repoDir, "config", "user.name", "Test")
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "commit", "--allow-empty", "-m", "init")

	task, _ := st.CreateTask("in flight", "prompt", "", "medium", "", "")
	workdir := filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID))
	os.MkdirAll(workdir, 0755)
	mustGit(t, repoDir, "worktree", "add", filepath.Join(workdir, "wt"), "-b", "burnrate/live")
	st.CreateRun(task.ID, workdir, "", "", "w1", 1)
	st.SetTaskStatus(task.ID, "running")

	if err := CleanupStale(context.Background(), st, cfg, log.New("", false)); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(workdir); err != nil {
		t.Fatal("agent workdir of a running task was swept out from under it")
	}
}

func TestCleanupStale_MissingRootsAreNotAnError(t *testing.T) {
	st, _ := newStore(t)
	cfg := config.Config{DataDir: filepath.Join(t.TempDir(), "absent"), WorktreeRoot: filepath.Join(t.TempDir(), "absent")}

	if err := CleanupStale(context.Background(), st, cfg, log.New("", false)); err != nil {
		t.Fatalf("CleanupStale on a fresh install: %v", err)
	}
}
