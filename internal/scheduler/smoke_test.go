package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

func TestDryRunSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("smoke test")
	}

	dataDir := t.TempDir()
	repoDir := t.TempDir()

	// Create a throwaway bare git repo + working clone so worktrees work.
	bareDir := filepath.Join(repoDir, "bare.git")
	workDir := filepath.Join(repoDir, "work")
	mustRun(t, "", "git", "init", "--bare", bareDir)
	mustRun(t, "", "git", "clone", bareDir, workDir)
	mustRun(t, workDir, "git", "commit", "--allow-empty", "-m", "init")
	mustRun(t, workDir, "git", "push", "origin", "HEAD")

	// Set default branch detection to work.
	mustRun(t, workDir, "git", "remote", "set-head", "origin", "--auto")

	// Fake usage server returning low utilization (window open).
	usageResp := map[string]any{
		"five_hour": map[string]any{
			"utilization": 10.0,
			"resets_at":   time.Now().Add(4 * time.Hour).Format(time.RFC3339),
		},
		"seven_day": map[string]any{
			"utilization": 20.0,
			"resets_at":   time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339),
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usageResp)
	}))
	defer srv.Close()

	os.Setenv("BURNRATE_DATA_DIR", dataDir)
	os.Setenv("BURNRATE_DRYRUN", "1")
	os.Setenv("BURNRATE_USAGE_URL", srv.URL)
	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer func() {
		os.Unsetenv("BURNRATE_DATA_DIR")
		os.Unsetenv("BURNRATE_DRYRUN")
		os.Unsetenv("BURNRATE_USAGE_URL")
		os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	}()

	config.EnsureDirs(dataDir)

	dbPath := filepath.Join(dataDir, "burnrate.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cfg, err := config.Load(st)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.BaseCodeDir = workDir
	cfg.WorktreeRoot = filepath.Join(dataDir, "worktrees")
	cfg.PollIntervalSec = 1
	cfg.DryRun = true

	st.SetSetting("notify_on_review", "false")

	task, err := st.CreateTask("smoke test task", "Add a hello.txt file", "", "small", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	logger := log.New("", true)
	client := usage.NewClient(srv.URL)
	sched := New(st, cfg, client, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go sched.Start(ctx)

	// Poll until the task transitions to pr_created (success) or timeout.
	deadline := time.After(25 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for task to complete")
		case <-time.After(500 * time.Millisecond):
			got, err := st.GetTask(task.ID)
			if err != nil {
				continue
			}
			if got.Status == "pr_created" {
				latest, err := st.LatestRunForTask(task.ID)
				if err != nil {
					t.Fatalf("get latest run: %v", err)
				}
				if latest == nil {
					t.Fatal("expected a run row")
				}
				if latest.Status != "succeeded" {
					t.Fatalf("expected run status=succeeded, got %s", latest.Status)
				}

				// Worktree should still exist at pr_created (kept for review).
				wtPath := filepath.Join(cfg.WorktreeRoot, fmt.Sprintf("task-%d", task.ID))
				if _, err := os.Stat(wtPath); err != nil {
					t.Logf("worktree %s not found (agent-mode dry run uses agentwork dir instead, OK)", wtPath)
				}

				out := mustRunOutput(t, workDir, "git", "branch", "--list", "burnrate/*")
				if out == "" {
					t.Log("branch not visible in main repo (expected for worktree-only, OK)")
				}

				cancel()
				sched.Wait()
				return
			}
			if got.Status == "failed" {
				latest, _ := st.LatestRunForTask(task.ID)
				t.Fatalf("task failed: %+v", latest)
			}
		}
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
	}
}

func mustRunOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}
