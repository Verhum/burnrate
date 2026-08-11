package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

func mustGitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestCheckpointAndRemove_KeepsWorktreeOnFailedCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()

	bareDir := filepath.Join(tmpDir, "bare.git")
	workDir := filepath.Join(tmpDir, "work")
	wtDir := filepath.Join(tmpDir, "worktree")

	mustGitRun(t, "", "init", "--bare", bareDir)
	mustGitRun(t, "", "clone", bareDir, workDir)
	mustGitRun(t, workDir, "config", "user.name", "Test")
	mustGitRun(t, workDir, "config", "user.email", "test@test.com")
	mustGitRun(t, workDir, "commit", "--allow-empty", "-m", "init")
	mustGitRun(t, workDir, "push", "origin", "HEAD")
	mustGitRun(t, workDir, "worktree", "add", wtDir, "-b", "test-branch")

	os.WriteFile(filepath.Join(wtDir, "dirty.txt"), []byte("uncommitted data"), 0644)

	// Make git commit fail by making the shared objects dir read-only
	objectsDir := filepath.Join(workDir, ".git", "objects")
	os.Chmod(objectsDir, 0555)
	t.Cleanup(func() { os.Chmod(objectsDir, 0755) })

	logger := log.New("", false)
	ctx := context.Background()

	CheckpointAndRemove(ctx, wtDir, workDir, 999, logger)

	if _, err := os.Stat(wtDir); err != nil {
		t.Fatal("worktree was removed despite failed checkpoint with uncommitted changes")
	}
	if _, err := os.Stat(filepath.Join(wtDir, "dirty.txt")); err != nil {
		t.Fatal("dirty.txt was lost")
	}
}

func TestBuildPromptFollowupSection(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	comments := []store.Comment{
		{ID: 1, Body: "fix the bug", CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: 2, Body: "also update docs", CreatedAt: "2025-01-01T01:00:00Z"},
	}

	prompt := buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo", comments: comments})

	if !strings.Contains(prompt, "## Follow-up Instructions") {
		t.Fatal("prompt missing Follow-up Instructions section")
	}
	if !strings.Contains(prompt, "fix the bug") {
		t.Fatal("prompt missing first comment body")
	}
	if !strings.Contains(prompt, "also update docs") {
		t.Fatal("prompt missing second comment body")
	}
	if !strings.Contains(prompt, "Follow-up #1") || !strings.Contains(prompt, "Follow-up #2") {
		t.Fatal("prompt missing numbered follow-up headers")
	}
	if !strings.Contains(prompt, "## Your Task") {
		t.Fatal("prompt missing Your Task section")
	}
}

func TestBuildPromptNoComments(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	prompt := buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})

	if strings.Contains(prompt, "Follow-up Instructions") {
		t.Fatal("prompt should not have Follow-up Instructions without comments")
	}
	if !strings.Contains(prompt, "Worker Instructions — New") {
		t.Fatal("should use new worker prompt")
	}
}

func TestBuildPromptFollowupTemplate(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	comments := []store.Comment{
		{ID: 1, Body: "add tests", CreatedAt: "2025-01-01T00:00:00Z"},
	}

	prompt := buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo", comments: comments})

	if !strings.Contains(prompt, "Worker Instructions — Follow-up") {
		t.Fatal("should use follow-up worker prompt")
	}
	if !strings.Contains(prompt, "## Follow-up Instructions") {
		t.Fatal("should include follow-up section")
	}
}

func TestBuildPromptResumeWithComments(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	resume := &store.Run{ID: 10, SessionID: "sess-1"}
	comments := []store.Comment{
		{ID: 1, Body: "do more", CreatedAt: "2025-01-01T00:00:00Z"},
	}

	prompt := buildPrompt(promptInput{task: task, resume: resume, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo", comments: comments})

	if !strings.Contains(prompt, "Worker Instructions — Resume") {
		t.Fatal("should use resume prompt template")
	}
	if !strings.Contains(prompt, "## Follow-up Instructions") {
		t.Fatal("resume with comments should include follow-up section")
	}
}

func TestBuildPromptStatesDocPolicy(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	resume := &store.Run{ID: 10, SessionID: "sess-1"}

	cases := []struct {
		name   string
		prompt string
	}{
		{"new", buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"resume", buildPrompt(promptInput{task: task, resume: resume, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"followup", buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"agent", buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/workdir"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.prompt, "## Documentation — The Code Is the Documentation") {
				t.Fatal("prompt missing documentation policy section")
			}
			// The policy is the three-way split; naming all three destinations is
			// what keeps a worker from defaulting to prose in CLAUDE.md.
			for _, dest := range []string{"ai.md", "CLAUDE.md", "README.md"} {
				if !strings.Contains(tc.prompt, dest) {
					t.Fatalf("prompt does not name %s as a documentation destination", dest)
				}
			}
			// Deletion must stay explicitly sanctioned, or the section decays back
			// into an append-only mandate.
			if !strings.Contains(tc.prompt, "Deleting stale documentation is a documentation update") {
				t.Fatal("prompt does not sanction deleting stale documentation")
			}
		})
	}
}

func TestBuildPromptPointsAtFieldGuide(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	resume := &store.Run{ID: 10, SessionID: "sess-1"}

	cases := []struct {
		name   string
		prompt string
	}{
		{"new", buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo", baseCodeDir: "/base"})},
		{"resume", buildPrompt(promptInput{task: task, resume: resume, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo", baseCodeDir: "/base"})},
		{"followup", buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo", baseCodeDir: "/base"})},
		{"agent", buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/workdir", baseCodeDir: "/base"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.prompt, "${BASE_CODE_DIR}/burnrate.md") {
				t.Fatal("prompt does not point the worker at the ${BASE_CODE_DIR}/burnrate.md field guide")
			}
			// The placeholder is resolved by the worker from Runtime Context, so
			// pointing at it is only useful if that block actually carries the
			// value — a hardcoded ~/code path silently missed anyone who moved
			// base_code_dir.
			if !strings.Contains(tc.prompt, "BASE_CODE_DIR: `/base`") {
				t.Fatal("prompt references ${BASE_CODE_DIR} but Runtime Context does not define it")
			}
		})
	}
}

func TestBuildPromptReportsWorktreeFriction(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	resume := &store.Run{ID: 10, SessionID: "sess-1"}

	cases := []struct {
		name   string
		prompt string
	}{
		{"new", buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"resume", buildPrompt(promptInput{task: task, resume: resume, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"followup", buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"agent", buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/workdir"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.prompt, "## Worktree Bootstrap — Report Friction") {
				t.Fatal("prompt missing Worktree Bootstrap — Report Friction section")
			}
			if !strings.Contains(tc.prompt, "## Worktree bootstrap") {
				t.Fatal("prompt does not name the heading the worker should report under")
			}
		})
	}
}

func TestWorktreeFromExistingBranch(t *testing.T) {
	tmpDir := t.TempDir()

	bareDir := filepath.Join(tmpDir, "bare.git")
	workDir := filepath.Join(tmpDir, "work")
	wtDir := filepath.Join(tmpDir, "followup-wt")

	mustGitRun(t, "", "init", "--bare", bareDir)
	mustGitRun(t, "", "clone", bareDir, workDir)
	mustGitRun(t, workDir, "config", "user.name", "Test")
	mustGitRun(t, workDir, "config", "user.email", "test@test.com")
	mustGitRun(t, workDir, "commit", "--allow-empty", "-m", "init")
	mustGitRun(t, workDir, "push", "origin", "HEAD")

	// Create a branch with some work, push it, then delete the worktree.
	origWt := filepath.Join(tmpDir, "orig-wt")
	mustGitRun(t, workDir, "worktree", "add", origWt, "-b", "burnrate/1-test")
	os.WriteFile(filepath.Join(origWt, "file.txt"), []byte("prior work"), 0644)
	mustGitRun(t, origWt, "add", "-A")
	mustGitRun(t, origWt, "commit", "-m", "prior work")
	mustGitRun(t, origWt, "push", "-u", "origin", "burnrate/1-test")
	mustGitRun(t, workDir, "worktree", "remove", "--force", origWt)

	// Set up the store with a completed run
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	task, _ := st.CreateTask("test task", "do stuff", workDir, "small", "", "")
	run, _ := st.CreateRun(task.ID, origWt, "burnrate/1-test", workDir, "w1", 1)
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "https://github.com/pr/1", "", "")
	st.SetTaskStatus(task.ID, "done")

	// Add a follow-up comment
	st.AddComment(task.ID, "add tests", "user")
	st.SetTaskStatus(task.ID, "queued")

	// Simulate the runner's follow-up worktree creation
	ctx := context.Background()
	mustGitRun(t, workDir, "fetch", "origin")

	// Use AddWorktreeFromBranch like the runner would
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "origin", "burnrate/1-test")
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Fatal("branch should exist at origin")
	}

	// Clean up stale local branch ref
	exec.CommandContext(ctx, "git", "branch", "-D", "burnrate/1-test").Run()

	cmd = exec.CommandContext(ctx, "git", "worktree", "add", wtDir, "burnrate/1-test")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worktree add from branch failed: %v\n%s", err, out)
	}

	// Verify the worktree has the prior work
	data, err := os.ReadFile(filepath.Join(wtDir, "file.txt"))
	if err != nil {
		t.Fatal("file.txt should exist in follow-up worktree")
	}
	if string(data) != "prior work" {
		t.Fatalf("expected 'prior work', got %q", string(data))
	}
}

// After a run completes, the worktree is checkpointed and removed. On resume,
// the runner must recreate it from the branch so the session finds its files.
func TestManagedResumeRecreatesRemovedWorktree(t *testing.T) {
	tmpDir := t.TempDir()

	bareDir := filepath.Join(tmpDir, "bare.git")
	workDir := filepath.Join(tmpDir, "work")
	wtDir := filepath.Join(tmpDir, "worktree")

	mustGitRun(t, "", "init", "--bare", bareDir)
	mustGitRun(t, "", "clone", bareDir, workDir)
	mustGitRun(t, workDir, "config", "user.name", "Test")
	mustGitRun(t, workDir, "config", "user.email", "test@test.com")
	mustGitRun(t, workDir, "commit", "--allow-empty", "-m", "init")
	mustGitRun(t, workDir, "push", "origin", "HEAD")

	// Create a branch, commit work, push it, then remove the worktree —
	// simulating what happens after a run is checkpointed and cleaned up.
	mustGitRun(t, workDir, "worktree", "add", wtDir, "-b", "burnrate/5-resume")
	os.WriteFile(filepath.Join(wtDir, "file.txt"), []byte("prior work"), 0644)
	mustGitRun(t, wtDir, "add", "-A")
	mustGitRun(t, wtDir, "commit", "-m", "prior work")
	mustGitRun(t, wtDir, "push", "-u", "origin", "burnrate/5-resume")
	mustGitRun(t, workDir, "worktree", "remove", "--force", wtDir)

	// Confirm the worktree directory is gone.
	if _, err := os.Stat(wtDir); err == nil {
		t.Fatal("worktree should have been removed")
	}

	// Set up a store with a resumable run pointing at the removed worktree.
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	st.SetSetting("notify_on_review", "false")
	task, _ := st.CreateTask("test task", "do stuff", workDir, "small", "", "")
	run, _ := st.CreateRun(task.ID, wtDir, "burnrate/5-resume", workDir, "w1", 1)
	st.SetRunSessionID(run.ID, "sess-resume")
	st.FinishRun(run.ID, "rate_limited", 0.5, 3, "", "suspended", "")
	st.SetTaskStatus(task.ID, "resumable")

	cfg := config.Config{
		WorktreeRoot: filepath.Join(tmpDir, "worktrees"),
		DataDir:      dataDir,
		DryRun:       true,
		MaxAttempts:  5,
	}
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	logger := log.New("", false)
	params := Params{Timeout: 5 * time.Minute, WindowID: "w2"}

	resume, _ := st.LatestRunForTask(task.ID)
	err = Run(context.Background(), st, cfg, *task, resume, params, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The worktree should have been recreated from the branch.
	data, err := os.ReadFile(filepath.Join(wtDir, "file.txt"))
	if err != nil {
		t.Fatalf("file.txt should exist in recreated worktree: %v", err)
	}
	if string(data) != "prior work" {
		t.Fatalf("expected 'prior work', got %q", string(data))
	}
}

func TestFollowupConsumedByRun(t *testing.T) {
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")

	st.AddComment(task.ID, "fix bug", "user")
	st.AddComment(task.ID, "add tests", "user")

	unconsumed, _ := st.UnconsumedComments(task.ID)
	if len(unconsumed) != 2 {
		t.Fatalf("expected 2 unconsumed, got %d", len(unconsumed))
	}

	run, _ := st.CreateRun(task.ID, "/wt", "b", "/r", "w", 1)
	st.MarkCommentsConsumed(task.ID, run.ID)

	unconsumed, _ = st.UnconsumedComments(task.ID)
	if len(unconsumed) != 0 {
		t.Fatalf("expected 0 unconsumed after marking, got %d", len(unconsumed))
	}

	all, _ := st.CommentsForTask(task.ID)
	for _, c := range all {
		if c.ConsumedByRun != run.ID {
			t.Fatalf("comment %d: expected consumed_by_run=%d, got %d", c.ID, run.ID, c.ConsumedByRun)
		}
	}
}

func TestPreflightErrorCreatesRunRow(t *testing.T) {
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	task, _ := st.CreateTask("bogus-task", "do stuff", "/nonexistent/repo/path", "small", "", "")

	cfg := config.Config{
		BaseCodeDir:  "/nonexistent/repo/path",
		WorktreeRoot: t.TempDir(),
		DataDir:      dbDir,
		DryRun:       true,
		MaxAttempts:  5,
	}

	logger := log.New("", false)
	params := Params{Timeout: 5 * time.Minute, WindowID: "w1"}

	err = Run(context.Background(), st, cfg, *task, nil, params, logger)
	if err == nil {
		t.Fatal("expected error from bogus repo path")
	}

	run, runErr := st.LatestRunForTask(task.ID)
	if runErr != nil {
		t.Fatalf("expected a run row, got error: %v", runErr)
	}
	if run == nil {
		t.Fatal("expected a run row to exist for pre-flight failure")
	}
	if run.Status != "errored" {
		t.Fatalf("expected run status=errored, got %s", run.Status)
	}
	if run.Error == "" {
		t.Fatal("expected non-empty error on errored run")
	}
	if strings.Count(run.Error, "detect default branch") > 1 {
		t.Fatalf("double-wrapped error message: %s", run.Error)
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "queued" {
		t.Fatalf("expected task status=queued after pre-flight error, got %s", got.Status)
	}
}

// A task whose repo is persistently misconfigured fails pre-flight every poll.
// Those errored runs never become resume candidates, so the scheduler's
// resume-based max-attempt cap cannot fire on them; preflightError must bound
// the retries itself and mark the task failed at MaxAttempts, otherwise it loops
// forever accumulating one errored run row per poll.
func TestPreflightErrorMarksFailedAfterCap(t *testing.T) {
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	task, _ := st.CreateTask("bogus-task", "do stuff", "/nonexistent/repo/path", "small", "", "")

	cfg := config.Config{
		BaseCodeDir:  "/nonexistent/repo/path",
		WorktreeRoot: t.TempDir(),
		DataDir:      dbDir,
		DryRun:       true,
		MaxAttempts:  3,
	}

	logger := log.New("", false)
	params := Params{Timeout: 5 * time.Minute, WindowID: "w1"}

	// Attempts 1 and 2 should leave the task queued for retry.
	for i := 1; i < cfg.MaxAttempts; i++ {
		if err := Run(context.Background(), st, cfg, *task, nil, params, logger); err == nil {
			t.Fatalf("attempt %d: expected error from bogus repo path", i)
		}
		got, _ := st.GetTask(task.ID)
		if got.Status != "queued" {
			t.Fatalf("attempt %d: expected task queued, got %s", i, got.Status)
		}
		run, _ := st.LatestRunForTask(task.ID)
		if run == nil || run.Attempt != i {
			t.Fatalf("attempt %d: expected run attempt=%d, got %+v", i, i, run)
		}
	}

	// The MaxAttempts-th failure must mark the task failed.
	if err := Run(context.Background(), st, cfg, *task, nil, params, logger); err == nil {
		t.Fatal("final attempt: expected error from bogus repo path")
	}
	got, _ := st.GetTask(task.ID)
	if got.Status != "failed" {
		t.Fatalf("expected task failed after %d pre-flight errors, got %s", cfg.MaxAttempts, got.Status)
	}
	run, _ := st.LatestRunForTask(task.ID)
	if run == nil || run.Attempt != cfg.MaxAttempts {
		t.Fatalf("expected final run attempt=%d, got %+v", cfg.MaxAttempts, run)
	}
	if run.Status != "errored" || run.Error == "" {
		t.Fatalf("expected final run errored with error text, got status=%s err=%q", run.Status, run.Error)
	}
}

func TestCheckpointAndRemove_RemovesCleanWorktree(t *testing.T) {
	tmpDir := t.TempDir()

	bareDir := filepath.Join(tmpDir, "bare.git")
	workDir := filepath.Join(tmpDir, "work")
	wtDir := filepath.Join(tmpDir, "worktree")

	mustGitRun(t, "", "init", "--bare", bareDir)
	mustGitRun(t, "", "clone", bareDir, workDir)
	mustGitRun(t, workDir, "config", "user.name", "Test")
	mustGitRun(t, workDir, "config", "user.email", "test@test.com")
	mustGitRun(t, workDir, "commit", "--allow-empty", "-m", "init")
	mustGitRun(t, workDir, "push", "origin", "HEAD")
	mustGitRun(t, workDir, "worktree", "add", wtDir, "-b", "test-branch-2")

	logger := log.New("", false)
	ctx := context.Background()

	CheckpointAndRemove(ctx, wtDir, workDir, 998, logger)

	if _, err := os.Stat(wtDir); err == nil {
		t.Fatal("clean worktree was not removed")
	}
}

// --- Agent-mode tests ---

func TestParseTrailers_AllPresent(t *testing.T) {
	text := "some output text\nmore output\n\nWORKED_IN: /base/code/burnrate\nREPO: verhum/burnrate\nBRANCH: burnrate/42-fix-bug\nPR: https://github.com/verhum/burnrate/pull/99\n"

	tr := parseTrailers(text)
	if tr.WorkedIn != "/base/code/burnrate" {
		t.Fatalf("WorkedIn: %q", tr.WorkedIn)
	}
	if tr.Repo != "verhum/burnrate" {
		t.Fatalf("Repo: %q", tr.Repo)
	}
	if tr.Branch != "burnrate/42-fix-bug" {
		t.Fatalf("Branch: %q", tr.Branch)
	}
	if tr.PR != "https://github.com/verhum/burnrate/pull/99" {
		t.Fatalf("PR: %q", tr.PR)
	}
}

func TestParseTrailers_NoneValues(t *testing.T) {
	text := "output\nWORKED_IN: /tmp/scratch\nREPO: none\nBRANCH: none\nPR: none\n"

	tr := parseTrailers(text)
	if tr.WorkedIn != "/tmp/scratch" {
		t.Fatalf("WorkedIn: %q", tr.WorkedIn)
	}
	if tr.Repo != "" {
		t.Fatalf("Repo should be empty for 'none': %q", tr.Repo)
	}
	if tr.Branch != "" {
		t.Fatalf("Branch should be empty for 'none': %q", tr.Branch)
	}
	if tr.PR != "" {
		t.Fatalf("PR should be empty for 'none': %q", tr.PR)
	}
}

func TestParseTrailers_Missing(t *testing.T) {
	text := "just some output with no trailers"
	tr := parseTrailers(text)
	if tr.WorkedIn != "" || tr.Repo != "" || tr.Branch != "" || tr.PR != "" {
		t.Fatalf("expected all empty, got %+v", tr)
	}
}

func TestParseTrailers_PrefersLastOccurrence(t *testing.T) {
	// The agent narrates an example "REPO: some/example" mid-message, then emits
	// the authoritative trailer block at the end. The final values must win.
	text := "I plan to work on REPO: some/example as a reference.\n" +
		"BRANCH: draft-idea\n" +
		strings.Repeat("...work...\n", 20) +
		"WORKED_IN: /base/code/burnrate\nREPO: verhum/burnrate\nBRANCH: burnrate/7-real\nPR: https://github.com/verhum/burnrate/pull/7\n"

	tr := parseTrailers(text)
	if tr.Repo != "verhum/burnrate" {
		t.Fatalf("Repo should be last occurrence, got %q", tr.Repo)
	}
	if tr.Branch != "burnrate/7-real" {
		t.Fatalf("Branch should be last occurrence, got %q", tr.Branch)
	}
	if tr.PR != "https://github.com/verhum/burnrate/pull/7" {
		t.Fatalf("PR: %q", tr.PR)
	}
}

func TestParseTrailers_AfterLongOutput(t *testing.T) {
	text := strings.Repeat("line of output\n", 100) +
		"WORKED_IN: /base/code/burnrate\nREPO: verhum/burnrate\nBRANCH: burnrate/1-test\nPR: https://github.com/verhum/burnrate/pull/1\n"

	tr := parseTrailers(text)
	if tr.Repo != "verhum/burnrate" {
		t.Fatalf("Repo: %q", tr.Repo)
	}
	if tr.PR != "https://github.com/verhum/burnrate/pull/1" {
		t.Fatalf("PR: %q", tr.PR)
	}
}

func TestParseTrailers_SurroundingWhitespace(t *testing.T) {
	text := "output\nWORKED_IN:   /path/to/dir   \nREPO:  owner/repo  \nBRANCH:\tmy-branch\t\nPR: https://example.com/pull/1 \n"

	tr := parseTrailers(text)
	if tr.WorkedIn != "/path/to/dir" {
		t.Fatalf("WorkedIn: %q", tr.WorkedIn)
	}
	if tr.Repo != "owner/repo" {
		t.Fatalf("Repo: %q", tr.Repo)
	}
	if tr.Branch != "my-branch" {
		t.Fatalf("Branch: %q", tr.Branch)
	}
	if tr.PR != "https://example.com/pull/1" {
		t.Fatalf("PR: %q", tr.PR)
	}
}

func TestAgentModeDryRun(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	os.MkdirAll(filepath.Join(dataDir, "agentwork"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	st.SetSetting("notify_on_review", "false")

	task, _ := st.CreateTask("test agent task", "do something in ~/code/burnrate", "", "small", "", "")

	cfg := config.Config{
		BaseCodeDir:  "/should/not/be/used",
		WorktreeRoot: filepath.Join(dataDir, "worktrees"),
		DataDir:      dataDir,
		DryRun:       true,
		MaxAttempts:  5,
	}

	logger := log.New("", false)
	params := Params{Timeout: 5 * time.Minute, WindowID: "w1"}

	err = Run(context.Background(), st, cfg, *task, nil, params, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	run, _ := st.LatestRunForTask(task.ID)
	if run == nil {
		t.Fatal("expected a run row")
	}

	expectedWorkdir := filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID))
	if run.WorktreePath != expectedWorkdir {
		t.Fatalf("expected worktree_path=%s, got %s", expectedWorkdir, run.WorktreePath)
	}

	// The run should succeed (no PR is fine in agent mode)
	if run.Status != "succeeded" {
		t.Fatalf("expected succeeded status, got %s", run.Status)
	}

	// Task should be pr_created; workdir is cleaned up asynchronously after
	// success so the user can checkout the branch locally without conflicts.
	got, _ := st.GetTask(task.ID)
	if got.Status != "pr_created" {
		t.Fatalf("expected task status=pr_created, got %s", got.Status)
	}

	promptStr := buildPrompt(promptInput{task: *task, agentMode: true, worktreePath: expectedWorkdir})

	if !strings.Contains(promptStr, "Worker Instructions — Agent-Directed") {
		t.Fatal("prompt should contain agent-directed template")
	}
	if !strings.Contains(promptStr, "WORKED_IN:") {
		t.Fatal("prompt should contain trailer contract (WORKED_IN)")
	}
	if !strings.Contains(promptStr, "WORKDIR:") {
		t.Fatal("prompt should contain WORKDIR context")
	}
	if strings.Contains(promptStr, "REPO_PATH:") {
		t.Fatal("agent-mode prompt should NOT contain REPO_PATH")
	}
}

func TestManagedModeStillWorks(t *testing.T) {
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Managed mode: set repo_path to a bogus path → should get pre-flight error
	task, _ := st.CreateTask("managed-task", "do stuff", "/nonexistent/managed/repo", "small", "", "")

	cfg := config.Config{
		BaseCodeDir:  "/also/not/used",
		WorktreeRoot: t.TempDir(),
		DataDir:      dbDir,
		DryRun:       true,
		MaxAttempts:  5,
	}
	os.MkdirAll(filepath.Join(dbDir, "logs"), 0755)

	logger := log.New("", false)
	params := Params{Timeout: 5 * time.Minute, WindowID: "w1"}

	err = Run(context.Background(), st, cfg, *task, nil, params, logger)
	if err == nil {
		t.Fatal("expected error from bogus managed repo path")
	}

	run, _ := st.LatestRunForTask(task.ID)
	if run == nil {
		t.Fatal("expected a run row")
	}
	if run.Status != "errored" {
		t.Fatalf("expected errored, got %s", run.Status)
	}
}

func TestBuildPromptAgentMode(t *testing.T) {
	task := store.Task{ID: 1, Title: "test agent task", Prompt: "do stuff"}
	prompt := buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/tmp/agentwork/task-1"})

	if !strings.Contains(prompt, "Worker Instructions — Agent-Directed") {
		t.Fatal("should use agent-directed template")
	}
	if !strings.Contains(prompt, "WORKDIR:") {
		t.Fatal("should contain WORKDIR context")
	}
	if strings.Contains(prompt, "REPO_PATH:") {
		t.Fatal("agent mode should not show REPO_PATH")
	}
}

func TestBuildPromptAgentModeWithPriorRun(t *testing.T) {
	task := store.Task{ID: 1, Title: "test agent task", Prompt: "do stuff"}
	prior := &store.Run{
		ID:            10,
		AgentWorkedIn: "/base/code/burnrate",
		AgentRepo:     "verhum/burnrate",
		Branch:        "burnrate/1-work",
		PRURL:         "https://github.com/verhum/burnrate/pull/42",
	}
	comments := []store.Comment{
		{ID: 1, Body: "also fix the tests", CreatedAt: "2025-01-01T00:00:00Z"},
	}

	prompt := buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/tmp/agentwork/task-1", comments: comments, priorRun: prior})

	if !strings.Contains(prompt, "Worker Instructions — Agent-Directed") {
		t.Fatal("should use agent-directed template")
	}
	if !strings.Contains(prompt, "also fix the tests") {
		t.Fatal("should include follow-up comment")
	}
	if !strings.Contains(prompt, "## Prior Run Context") {
		t.Fatal("should include prior run context section")
	}
	if !strings.Contains(prompt, "verhum/burnrate") {
		t.Fatal("should include prior run repo")
	}
	if !strings.Contains(prompt, "burnrate/1-work") {
		t.Fatal("should include prior run branch")
	}
	if !strings.Contains(prompt, "https://github.com/verhum/burnrate/pull/42") {
		t.Fatal("should include prior run PR")
	}
	if !strings.Contains(prompt, "Do NOT redo completed work") {
		t.Fatal("should warn against redoing work")
	}
}

func TestCleanupAgentWorkdir(t *testing.T) {
	tmpDir := t.TempDir()

	repoDir := filepath.Join(tmpDir, "repo")
	mustGitRun(t, "", "init", repoDir)
	mustGitRun(t, repoDir, "config", "user.name", "Test")
	mustGitRun(t, repoDir, "config", "user.email", "test@test.com")
	mustGitRun(t, repoDir, "commit", "--allow-empty", "-m", "init")

	agentDir := filepath.Join(tmpDir, "agentwork", "task-1")
	os.MkdirAll(agentDir, 0755)

	wtDir := filepath.Join(agentDir, "wt")
	mustGitRun(t, repoDir, "worktree", "add", wtDir, "-b", "agent-branch")

	out, _ := exec.Command("git", "-C", repoDir, "worktree", "list").CombinedOutput()
	if !strings.Contains(string(out), "wt") {
		t.Fatalf("worktree not listed: %s", out)
	}

	ctx := context.Background()
	logger := log.New("", false)

	cleanupAgentWorkdir(ctx, agentDir, 1, logger)

	if _, err := os.Stat(agentDir); err == nil {
		t.Fatal("agent workdir was not removed")
	}

	out, _ = exec.Command("git", "-C", repoDir, "worktree", "list").CombinedOutput()
	if strings.Contains(string(out), "wt") {
		t.Fatalf("worktree still listed after cleanup: %s", out)
	}
}

func TestCheckpointAndRemove_AgentModeRoutesToCleanup(t *testing.T) {
	// Task delete / complete / dismiss / scheduler-max-attempts all call
	// CheckpointAndRemove with the run's repo_path, which is empty for
	// agent-directed runs. That must tear down the agent workdir (detach child
	// worktrees + rm), NOT run managed-worktree checkpoint logic on a non-repo.
	tmpDir := t.TempDir()

	repoDir := filepath.Join(tmpDir, "repo")
	mustGitRun(t, "", "init", repoDir)
	mustGitRun(t, repoDir, "config", "user.name", "Test")
	mustGitRun(t, repoDir, "config", "user.email", "test@test.com")
	mustGitRun(t, repoDir, "commit", "--allow-empty", "-m", "init")

	agentDir := filepath.Join(tmpDir, "agentwork", "task-9")
	os.MkdirAll(agentDir, 0755)
	wtDir := filepath.Join(agentDir, "wt")
	mustGitRun(t, repoDir, "worktree", "add", wtDir, "-b", "agent-branch")

	CheckpointAndRemove(context.Background(), agentDir, "", 9, log.New("", false))

	if _, err := os.Stat(agentDir); err == nil {
		t.Fatal("agent workdir was not removed via CheckpointAndRemove")
	}
	out, _ := exec.Command("git", "-C", repoDir, "worktree", "list").CombinedOutput()
	if strings.Contains(string(out), "wt") {
		t.Fatalf("worktree still attached after CheckpointAndRemove: %s", out)
	}
}

// The shape that lost a real deliverable: a task naming no repo at all, whose
// output is a loose file in the workdir — nothing to checkpoint, so nothing
// stands behind it. Pinned at CheckpointAndRemove because that is the entry
// point every lifecycle call site uses, not the git helper underneath it.
func TestCheckpointAndRemove_KeepsARepoLessDeliverable(t *testing.T) {
	agentDir := filepath.Join(t.TempDir(), "agentwork", "task-3")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("mkdir agent workdir: %v", err)
	}
	deliverable := filepath.Join(agentDir, "analytics.html")
	if err := os.WriteFile(deliverable, []byte("<html>report</html>"), 0644); err != nil {
		t.Fatalf("write deliverable: %v", err)
	}

	CheckpointAndRemove(context.Background(), agentDir, "", 3, log.New("", false))

	body, err := os.ReadFile(deliverable)
	if err != nil {
		t.Fatalf("deliverable did not survive cleanup: %v", err)
	}
	if string(body) != "<html>report</html>" {
		t.Fatalf("deliverable was rewritten: %q", body)
	}
}

// newAgentWorkdirRepo returns an initialised checkout plus an agent workdir
// beneath it, the shape every agent-directed run has on disk.
func newAgentWorkdirRepo(t *testing.T, taskID int) (repoDir, agentDir string) {
	t.Helper()
	tmpDir := t.TempDir()

	repoDir = filepath.Join(tmpDir, "repo")
	mustGitRun(t, "", "init", repoDir)
	mustGitRun(t, repoDir, "config", "user.name", "Test")
	mustGitRun(t, repoDir, "config", "user.email", "test@test.com")
	mustGitRun(t, repoDir, "commit", "--allow-empty", "-m", "init")

	agentDir = filepath.Join(tmpDir, "agentwork", fmt.Sprintf("task-%d", taskID))
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("mkdir agent workdir: %v", err)
	}
	return repoDir, agentDir
}

func worktreeListed(t *testing.T, repoDir, name string) bool {
	t.Helper()
	out, _ := exec.Command("git", "-C", repoDir, "worktree", "list").CombinedOutput()
	return strings.Contains(string(out), name)
}

// A stray modified file is the common case — a build artifact the agent's own
// test run touched — and it used to leak the whole workdir forever, because
// cleanup fires once per completion and nothing retried it. Checkpoint the work
// onto its branch instead, the same way a managed worktree is torn down.
func TestCleanupAgentWorkdir_DirtyWorktreeCheckpointed(t *testing.T) {
	repoDir, agentDir := newAgentWorkdirRepo(t, 2)

	wtDir := filepath.Join(agentDir, "wt")
	mustGitRun(t, repoDir, "worktree", "add", wtDir, "-b", "dirty-branch")
	os.WriteFile(filepath.Join(wtDir, "dirty.txt"), []byte("uncommitted"), 0644)

	cleanupAgentWorkdir(context.Background(), agentDir, 2, log.New("", false))

	if _, err := os.Stat(agentDir); err == nil {
		t.Fatal("agent workdir was kept even though the dirty worktree could be checkpointed")
	}
	if worktreeListed(t, repoDir, "wt") {
		t.Fatal("worktree still attached after cleanup")
	}

	out, err := exec.Command("git", "-C", repoDir, "show", "--stat", "--format=%s", "dirty-branch").CombinedOutput()
	if err != nil {
		t.Fatalf("read dirty-branch tip: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "dirty.txt") {
		t.Fatalf("uncommitted file was not checkpointed onto its branch: %s", out)
	}
}

// A detached HEAD has nowhere to push a checkpoint, so committing and then
// removing the worktree would destroy the work. Keep it instead.
func TestCleanupAgentWorkdir_DetachedHeadKept(t *testing.T) {
	repoDir, agentDir := newAgentWorkdirRepo(t, 3)

	wtDir := filepath.Join(agentDir, "wt")
	mustGitRun(t, repoDir, "worktree", "add", "--detach", wtDir)
	os.WriteFile(filepath.Join(wtDir, "dirty.txt"), []byte("uncommitted"), 0644)

	cleanupAgentWorkdir(context.Background(), agentDir, 3, log.New("", false))

	if _, err := os.Stat(agentDir); err != nil {
		t.Fatal("agent workdir was removed despite unsalvageable uncommitted work")
	}
	if _, err := os.Stat(filepath.Join(wtDir, "dirty.txt")); err != nil {
		t.Fatal("dirty.txt was lost")
	}
}

// Agents pick their own layout inside the workdir. A worktree the scan misses is
// still deleted from disk by the final RemoveAll, leaving it registered in the
// owning checkout forever.
func TestCleanupAgentWorkdir_NestedWorktreeDetached(t *testing.T) {
	repoDir, agentDir := newAgentWorkdirRepo(t, 4)

	wtDir := filepath.Join(agentDir, "repos", "burnrate")
	mustGitRun(t, repoDir, "worktree", "add", wtDir, "-b", "nested-branch")

	cleanupAgentWorkdir(context.Background(), agentDir, 4, log.New("", false))

	if _, err := os.Stat(agentDir); err == nil {
		t.Fatal("agent workdir was not removed")
	}
	if worktreeListed(t, repoDir, "burnrate") {
		t.Fatal("nested worktree still attached after cleanup")
	}
}

func TestFollowupAgentMode_NoAddWorktreeFromBranch(t *testing.T) {
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Agent-mode task (empty repo_path)
	task, _ := st.CreateTask("agent task", "work on burnrate", "", "small", "", "")

	// Create a completed run with trailer data
	run, _ := st.CreateRun(task.ID, "/tmp/agentwork/task-1", "", "", "w1", 1)
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "https://github.com/verhum/burnrate/pull/42", "", "")
	st.SetRunBranch(run.ID, "burnrate/1-work")
	st.SetRunAgentRepo(run.ID, "verhum/burnrate")
	st.SetRunAgentWorkedIn(run.ID, "/base/code/burnrate")
	st.SetTaskStatus(task.ID, "done")

	// Add a follow-up comment
	st.AddComment(task.ID, "also fix the tests", "user")
	st.SetTaskStatus(task.ID, "queued")

	// Build the prompt in agent mode — should include prior run context
	comments, _ := st.UnconsumedComments(task.ID)
	latestRun, _ := st.LatestRunForTask(task.ID)

	prompt := buildPrompt(promptInput{task: *task, agentMode: true, worktreePath: "/tmp/agentwork/task-1", comments: comments, priorRun: latestRun})

	if !strings.Contains(prompt, "Worker Instructions — Agent-Directed") {
		t.Fatal("should use agent-directed template")
	}
	if !strings.Contains(prompt, "also fix the tests") {
		t.Fatal("should include follow-up comment")
	}
	if !strings.Contains(prompt, "verhum/burnrate") {
		t.Fatal("should include prior run repo")
	}
	if !strings.Contains(prompt, "burnrate/1-work") {
		t.Fatal("should include prior run branch")
	}
	if !strings.Contains(prompt, "https://github.com/verhum/burnrate/pull/42") {
		t.Fatal("should include prior run PR")
	}
	// Agent mode should NOT have REPO_PATH (managed mode concept)
	if strings.Contains(prompt, "REPO_PATH:") {
		t.Fatal("agent mode follow-up should not use managed-mode REPO_PATH")
	}
}

func TestFireReviewNotification_GatedOff(t *testing.T) {
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	st.SetSetting("notify_on_review", "false")
	task := store.Task{ID: 1, Title: "test"}
	logger := log.New("", false)

	// Should not panic or attempt exec when gated off
	fireReviewNotification(st, task, "", logger)
}

func TestFireReviewNotification_DefaultOn(t *testing.T) {
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	task := store.Task{ID: 42, Title: "fix bug"}
	logger := log.New("", false)

	// With no setting, default is on; will attempt osascript but we expect a
	// non-fatal warning (osascript may not work in CI)
	fireReviewNotification(st, task, "https://github.com/pr/1", logger)
}

func TestAgentModeDryRunSetsPrCreated(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	os.MkdirAll(filepath.Join(dataDir, "agentwork"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	// Disable notification to avoid osascript in test
	st.SetSetting("notify_on_review", "false")

	task, _ := st.CreateTask("agent task", "do something", "", "small", "", "")

	cfg := config.Config{
		WorktreeRoot: filepath.Join(dataDir, "worktrees"),
		DataDir:      dataDir,
		DryRun:       true,
		MaxAttempts:  5,
	}

	logger := log.New("", false)
	params := Params{Timeout: 5 * time.Minute, WindowID: "w1"}

	err = Run(context.Background(), st, cfg, *task, nil, params, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "pr_created" {
		t.Fatalf("expected pr_created, got %s", got.Status)
	}
}

func TestParseResults_MultipleRepos(t *testing.T) {
	text := `Did the work in two repos.

RESULT: acme/api | burnrate/42-webhooks | https://github.com/acme/api/pull/311 | /work/api
RESULT: acme/web | burnrate/42-webhooks | https://github.com/acme/web/pull/98 | /work/web

WORKED_IN: /work/api
REPO: acme/api
BRANCH: burnrate/42-webhooks
PR: https://github.com/acme/api/pull/311`

	results := parseResults(text)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if results[0].Repo != "acme/api" || results[0].Branch != "burnrate/42-webhooks" ||
		results[0].PRURL != "https://github.com/acme/api/pull/311" || results[0].WorkedIn != "/work/api" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[1].Repo != "acme/web" || results[1].PRURL != "https://github.com/acme/web/pull/98" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
	if p := primaryResult(results); p.Repo != "acme/api" {
		t.Fatalf("expected first PR-bearing result as primary, got %+v", p)
	}
}

func TestParseResults_FallsBackToTrailers(t *testing.T) {
	text := `Single repo run.

WORKED_IN: /work/api
REPO: acme/api
BRANCH: burnrate/7-fix
PR: https://github.com/acme/api/pull/5`

	results := parseResults(text)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0].Repo != "acme/api" || results[0].PRURL != "https://github.com/acme/api/pull/5" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestParseResults_IgnoresPlaceholdersAndDuplicates(t *testing.T) {
	text := `I will report like this:
RESULT: <owner/name> | <branch> | <PR URL> | <absolute worktree path>

RESULT: acme/api | b1 | https://github.com/acme/api/pull/5 | /work/api
RESULT: acme/api | b1 | https://github.com/acme/api/pull/5 | /work/api`

	results := parseResults(text)
	if len(results) != 1 {
		t.Fatalf("expected 1 result after filtering, got %d: %+v", len(results), results)
	}
	if results[0].Repo != "acme/api" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestParseResults_NoneFields(t *testing.T) {
	results := parseResults("RESULT: acme/api | b1 | none | /work/api")
	if len(results) != 1 || results[0].PRURL != "" {
		t.Fatalf("expected one result with empty PR URL, got %+v", results)
	}
	if p := primaryResult(results); p.Repo != "acme/api" {
		t.Fatalf("primary should fall back to first result, got %+v", p)
	}
}

func TestParseResults_Empty(t *testing.T) {
	if results := parseResults("no trailers here"); results != nil {
		t.Fatalf("expected nil, got %+v", results)
	}
}

func TestBuildPromptAgentModeIncludesBaseCodeDirAndPriorPRs(t *testing.T) {
	task := store.Task{ID: 42, Title: "multi repo"}
	prompt := buildPrompt(promptInput{
		task:         task,
		agentMode:    true,
		worktreePath: "/work",
		baseCodeDir:  "/base/code",
		priorPRs: []store.TaskPR{
			{Repo: "acme/api", Branch: "b1", PRURL: "https://github.com/acme/api/pull/5", WorkedIn: "/work/api"},
			{Repo: "acme/web", Branch: "b1", PRURL: "https://github.com/acme/web/pull/6", WorkedIn: "/work/web"},
		},
	})

	if !strings.Contains(prompt, "BASE_CODE_DIR: `/base/code`") {
		t.Fatal("prompt missing BASE_CODE_DIR")
	}
	if !strings.Contains(prompt, "https://github.com/acme/api/pull/5") ||
		!strings.Contains(prompt, "https://github.com/acme/web/pull/6") {
		t.Fatal("prompt missing prior PRs")
	}
	if !strings.Contains(prompt, "reuse the existing PRs") {
		t.Fatal("branch-backed prior work should keep the follow-up-commit guidance")
	}
	if strings.Contains(prompt, "still in your working directory") {
		t.Fatal("branch-backed prior work should not claim loose files persist")
	}
}

// workdirWith makes an agent workdir holding the named files, so buildPrompt's
// "your earlier files are still there" claim has something real to check.
func workdirWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuildPromptAgentModeRepoLessPriorWorkPointsAtTheWorkdir(t *testing.T) {
	workdir := workdirWith(t, "burnrate-analytics.html")
	prompt := buildPrompt(promptInput{
		task:         store.Task{ID: 3, Title: "analytics page"},
		agentMode:    true,
		worktreePath: workdir,
		priorPRs:     []store.TaskPR{{WorkedIn: workdir}},
	})

	if !strings.Contains(prompt, "## Prior Run Context") {
		t.Fatal("prompt missing prior run context section")
	}
	if !strings.Contains(prompt, "still in your working directory") {
		t.Fatal("prompt should say the earlier deliverables are still in the workdir")
	}
	if strings.Contains(prompt, "reuse the existing PRs") {
		t.Fatal("prompt must not claim branches or PRs for repo-less prior work")
	}
}

func TestBuildPromptAgentModeMixedPriorWorkDescribesBoth(t *testing.T) {
	workdir := workdirWith(t, "notes.md")
	prompt := buildPrompt(promptInput{
		task:         store.Task{ID: 4, Title: "mixed"},
		agentMode:    true,
		worktreePath: workdir,
		priorPRs: []store.TaskPR{
			{Repo: "acme/api", Branch: "b1", PRURL: "https://github.com/acme/api/pull/5", WorkedIn: workdir + "/api"},
			{WorkedIn: workdir},
		},
	})

	if !strings.Contains(prompt, "reuse the existing PRs") {
		t.Fatal("prompt should keep the follow-up guidance for the pushed branch")
	}
	if !strings.Contains(prompt, "https://github.com/acme/api/pull/5") {
		t.Fatal("prompt missing the pushed branch's PR")
	}
	if !strings.Contains(prompt, "still in your working directory") {
		t.Fatal("prompt should also point at the repo-less deliverables")
	}
}

func TestBuildPromptAgentModeRepoLessPriorWorkStaysQuietOnAnEmptyWorkdir(t *testing.T) {
	workdir := workdirWith(t)
	prompt := buildPrompt(promptInput{
		task:         store.Task{ID: 5, Title: "gone"},
		agentMode:    true,
		worktreePath: workdir,
		priorPRs:     []store.TaskPR{{WorkedIn: workdir}},
	})

	if strings.Contains(prompt, "still in your working directory") {
		t.Fatal("prompt must not claim files that are not there")
	}
	if strings.Contains(prompt, "reuse the existing PRs") {
		t.Fatal("prompt must not claim branches or PRs for repo-less prior work")
	}
}

func TestBuildPromptAgentModePriorRunWithoutBranchPointsAtTheWorkdir(t *testing.T) {
	workdir := workdirWith(t, "report.md")
	prompt := buildPrompt(promptInput{
		task:         store.Task{ID: 6, Title: "no branch"},
		agentMode:    true,
		worktreePath: workdir,
		priorRun:     &store.Run{ID: 9, AgentWorkedIn: workdir},
	})

	if !strings.Contains(prompt, "still in your working directory") {
		t.Fatal("prompt should say the earlier deliverables are still in the workdir")
	}
	if strings.Contains(prompt, "reuse the existing PRs") {
		t.Fatal("prompt must not claim branches or PRs the prior run never made")
	}
}

// buildBranchRepo makes a repo whose `feature` branch adds `added` lines on top
// of main, so recordRunLines has something real to measure.
func buildBranchRepo(t *testing.T, added int) string {
	t.Helper()
	dir := t.TempDir()
	mustGitRun(t, dir, "init", "--initial-branch=main")
	mustGitRun(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGitRun(t, dir, "add", "-A")
	mustGitRun(t, dir, "commit", "-m", "base")

	mustGitRun(t, dir, "checkout", "-b", "feature")
	body := strings.Repeat("line\n", added)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGitRun(t, dir, "add", "-A")
	mustGitRun(t, dir, "commit", "-m", "work")
	return dir
}

func TestRecordRunLines_AttributesBranchGrowthNotTheWholeBranch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	logger := log.New("", false)
	ctx := context.Background()

	task, _ := st.CreateTask("t", "prompt", "", "medium", "", "")
	repo := buildBranchRepo(t, 10)
	results := []AgentResult{{Repo: "o/r", Branch: "feature", PRURL: "https://x/pull/1", WorkedIn: repo}}

	run1, _ := st.CreateRun(task.ID, repo, "feature", "", "w1", 1)
	recordTaskPRs(st, task.ID, run1.ID, results, logger)
	recordRunLines(ctx, st, task.ID, run1.ID, results, "main", logger)

	got, _ := st.LatestRunForTask(task.ID)
	if got.LinesAdded != 10 {
		t.Fatalf("first run should own all 10 lines, got %d", got.LinesAdded)
	}

	// A followup run grows the same branch by 5 lines. It must be credited with 5,
	// not with the branch's 15, or the day's line count double-counts.
	body := strings.Repeat("line\n", 15)
	if err := os.WriteFile(filepath.Join(repo, "work.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGitRun(t, repo, "add", "-A")
	mustGitRun(t, repo, "commit", "-m", "more work")

	run2, _ := st.CreateRun(task.ID, repo, "feature", "", "w1", 2)
	recordTaskPRs(st, task.ID, run2.ID, results, logger)
	recordRunLines(ctx, st, task.ID, run2.ID, results, "main", logger)

	got, _ = st.LatestRunForTask(task.ID)
	if got.ID != run2.ID {
		t.Fatalf("expected run %d, got %d", run2.ID, got.ID)
	}
	if got.LinesAdded != 5 {
		t.Fatalf("followup run should own only the 5 new lines, got %d", got.LinesAdded)
	}

	prs, _ := st.ListTaskPRs(task.ID)
	if len(prs) != 1 || prs[0].LinesAdded != 15 {
		t.Fatalf("branch row should hold the cumulative 15, got %+v", prs)
	}
}

func TestRecordRunLines_UnmeasurableWorktreeIsNotFatal(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	logger := log.New("", false)

	task, _ := st.CreateTask("t", "prompt", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, "/gone", "feature", "", "w1", 1)
	results := []AgentResult{{Repo: "o/r", Branch: "feature", WorkedIn: filepath.Join(t.TempDir(), "not-a-repo")}}
	recordTaskPRs(st, task.ID, run.ID, results, logger)

	// A worktree the agent removed must not fail a successful run; the run just
	// keeps a zero line count while its cost still counts.
	recordRunLines(context.Background(), st, task.ID, run.ID, results, "main", logger)

	got, _ := st.LatestRunForTask(task.ID)
	if got.LinesAdded != 0 || got.LinesRemoved != 0 {
		t.Fatalf("expected zero lines, got +%d/-%d", got.LinesAdded, got.LinesRemoved)
	}
}

func TestRecordRunLines_SkipsResultsWithoutAWorktree(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	logger := log.New("", false)

	task, _ := st.CreateTask("t", "prompt", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, "", "", "", "w1", 1)
	recordRunLines(context.Background(), st, task.ID, run.ID,
		[]AgentResult{{Repo: "o/r", Branch: "", WorkedIn: ""}}, "", logger)

	got, _ := st.LatestRunForTask(task.ID)
	if got.LinesAdded != 0 {
		t.Fatalf("expected zero lines, got %d", got.LinesAdded)
	}
}

// longAgentReport builds a realistic final message well past the old 2000-byte
// clamp, ending in the trailer block a worker is required to print.
func longAgentReport() string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&sb, "- finding %d: a sentence long enough that the whole report clears two kilobytes — naïvely.\n", i)
	}
	sb.WriteString("\nRESULT: o/r | b | https://github.com/o/r/pull/1 | /wt\n")
	return sb.String()
}

func TestClassifyStoresFullAgentResponse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		agentMode bool
	}{
		{"agent mode", true},
		{"managed mode", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()
			st.SetSetting("notify_on_review", "false")

			repoPath := ""
			if !tc.agentMode {
				repoPath = "/repo"
			}
			task, _ := st.CreateTask("t", "prompt", repoPath, "medium", "", "")
			run, _ := st.CreateRun(task.ID, "", "b", repoPath, "w1", 1)

			text := longAgentReport()
			if len(text) <= 2000 {
				t.Fatalf("fixture must exceed the old clamp, got %d bytes", len(text))
			}
			result := claude.Result{SessionID: "s1", ResultText: text}
			if err := classify(context.Background(), st, *task, run, result, nil,
				repoPath, "main", "", "", tc.agentMode, log.New("", false), nil); err != nil {
				t.Fatalf("classify: %v", err)
			}

			comments, _ := st.CommentsForTask(task.ID)
			if len(comments) != 1 {
				t.Fatalf("expected 1 comment, got %d", len(comments))
			}
			got := comments[0]
			if got.Author != "agent" {
				t.Fatalf("expected agent author, got %q", got.Author)
			}
			if got.Body != text {
				t.Fatalf("comment body was altered: got %d bytes, want %d", len(got.Body), len(text))
			}
			if strings.Contains(got.Body, "[truncated]") {
				t.Fatal("comment must not carry a truncation marker")
			}
		})
	}
}

// The agent's own report is posted after the thread is marked consumed, so it
// stays unconsumed forever. It must never come back as a follow-up instruction.
func TestAgentCommentIsNotAFollowupInstruction(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	st.SetSetting("notify_on_review", "false")

	task, _ := st.CreateTask("t", "prompt", "", "medium", "", "")
	st.AddComment(task.ID, "Level of effort: 4", "user")
	run, _ := st.CreateRun(task.ID, "", "b", "", "w1", 1)

	report := longAgentReport() + "\n**Level for this run: 1 — Investigate.**\n"
	if err := classify(context.Background(), st, *task, run,
		claude.Result{SessionID: "s1", ResultText: report}, nil,
		"", "main", "", "", true, log.New("", false), nil); err != nil {
		t.Fatalf("classify: %v", err)
	}

	unconsumed, _ := st.UnconsumedComments(task.ID)
	if len(unconsumed) != 1 || unconsumed[0].Author != "agent" {
		t.Fatalf("expected the agent comment to be the only unconsumed one, got %+v", unconsumed)
	}

	followups := userComments(unconsumed)
	if len(followups) != 0 {
		t.Fatalf("agent report leaked into follow-up instructions: %+v", followups)
	}

	prompt := buildPrompt(promptInput{task: *task, agentMode: true, comments: followups, worktreePath: "/wt"})
	if strings.Contains(prompt, "Follow-up Instructions") {
		t.Fatal("prompt should have no follow-up section when only the agent commented")
	}
	if strings.Contains(prompt, "finding 7:") {
		t.Fatal("agent report text leaked into the next prompt")
	}
	if lvl, explicit := resolveEffortLevel(task.Prompt, followups); explicit || lvl != DefaultEffortLevel {
		t.Fatalf("agent report set the effort level: lvl=%d explicit=%v", lvl, explicit)
	}
}
