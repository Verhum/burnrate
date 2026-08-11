package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

// TestDryRunPromptCarriesEffortLevel drives the real pipeline — scheduler tick,
// launch, runner prompt assembly, claude.Invoke — and reads the prompt the
// stubbed claude actually received from `BURNRATE_DRYRUN.md`. The unit tests in
// internal/runner prove buildPrompt renders the section; this proves the
// section survives the whole path to the CLI invocation, for both a task that
// names a level and one that doesn't — including the level-4-is-opt-in wording
// that an unpinned task depends on.
func TestDryRunPromptCarriesEffortLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("smoke test")
	}

	cases := []struct {
		name       string
		taskPrompt string
		want       []string
	}{
		{
			name:       "explicit level",
			taskPrompt: "Add a hello.txt file. LOE: 4",
			want:       []string{"**Level for this run: 4 — Validate end to end.**"},
		},
		{
			name:       "default level",
			taskPrompt: "Add a hello.txt file",
			want: []string{
				"**Level for this run: 3 — Verify (default).**",
				// Level 4 is opt-in, so the prompt an unpinned task actually
				// receives has to forbid the worker from picking it.
				"NEVER work at level 4 unless the user explicitly asked for it",
				"**Never raise yourself to 4**",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt := dryRunPrompt(t, tc.taskPrompt)
			if !strings.Contains(prompt, "## Level of Effort") {
				t.Fatal("prompt sent to claude is missing the Level of Effort section")
			}
			for _, want := range tc.want {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt sent to claude does not contain %q", want)
				}
			}
		})
	}
}

// dryRunPrompt runs one task through the scheduler with claude stubbed out and
// returns the prompt the stub was handed.
func dryRunPrompt(t *testing.T, taskPrompt string) string {
	t.Helper()

	dataDir := t.TempDir()
	repoDir := t.TempDir()

	bareDir := filepath.Join(repoDir, "bare.git")
	workDir := filepath.Join(repoDir, "work")
	mustRun(t, "", "git", "init", "--bare", bareDir)
	mustRun(t, "", "git", "clone", bareDir, workDir)
	mustRun(t, workDir, "git", "commit", "--allow-empty", "-m", "init")
	mustRun(t, workDir, "git", "push", "origin", "HEAD")
	mustRun(t, workDir, "git", "remote", "set-head", "origin", "--auto")

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

	t.Setenv("BURNRATE_DATA_DIR", dataDir)
	t.Setenv("BURNRATE_DRYRUN", "1")
	t.Setenv("BURNRATE_USAGE_URL", srv.URL)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")

	config.EnsureDirs(dataDir)

	st, err := store.Open(filepath.Join(dataDir, "burnrate.db"))
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

	// Agent-directed (no repo path), the mode every task created from the UI uses.
	task, err := st.CreateTask("effort smoke task", taskPrompt, "", "small", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sched := New(st, cfg, usage.NewClient(srv.URL), log.New("", true))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go sched.Start(ctx)
	defer sched.Wait()
	defer cancel()

	// The dry-run stub writes the prompt it was given into its work directory.
	promptPath := filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID), "BURNRATE_DRYRUN.md")
	deadline := time.After(25 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", promptPath)
		case <-time.After(200 * time.Millisecond):
			data, err := os.ReadFile(promptPath)
			if err != nil {
				if got, gerr := st.GetTask(task.ID); gerr == nil && got.Status == "failed" {
					latest, _ := st.LatestRunForTask(task.ID)
					t.Fatalf("task failed before the prompt was written: %+v", latest)
				}
				continue
			}
			return string(data)
		}
	}
}
