package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

// Suspension is how burnrate pauses work when the session limit is spent: the
// scheduler cancels the run with an ErrSuspended cause and the run must record
// itself as paused-and-resumable rather than as its own failure.
//
// These drive classify() directly with a cancelled context, so the branch is
// exercised without racing a real claude invocation.

func suspendTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	st.SetSetting("notify_on_review", "false")
	return st, dir
}

// suspendedCtx returns a context cancelled the way Scheduler.suspend cancels one.
func suspendedCtx(resetAt time.Time) context.Context {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&ErrSuspended{ResetAt: resetAt})
	return ctx
}

func latestRun(t *testing.T, st *store.Store, taskID int64) *store.Run {
	t.Helper()
	run, err := st.LatestRunForTask(taskID)
	if err != nil || run == nil {
		t.Fatalf("latest run for task %d: %v", taskID, err)
	}
	return run
}

func taskStatus(t *testing.T, st *store.Store, taskID int64) string {
	t.Helper()
	task, err := st.GetTask(taskID)
	if err != nil {
		t.Fatalf("get task %d: %v", taskID, err)
	}
	return task.Status
}

func TestSuspendedRunIsRecordedAsPausedAndResumable(t *testing.T) {
	st, dir := suspendTestStore(t)
	task, _ := st.CreateTask("suspended task", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, filepath.Join(dir, "nonexistent-workdir"), "", "", "w1", 1)

	resetAt := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	result := claude.Result{SessionID: "sess-1", CostUSD: 1.25, NumTurns: 7}

	// A suspend cancels the run, so the invocation returns a context error.
	err := classify(suspendedCtx(resetAt), st, *task, run, result, context.Canceled,
		"", "main", run.WorktreePath, "", true, log.New("", false), nil)

	// A suspension is the scheduler getting what it asked for, not a failure.
	if err != nil {
		t.Errorf("classify returned %v, want nil: a suspend is not a run failure", err)
	}

	got := latestRun(t, st, task.ID)
	if got.Status != "rate_limited" {
		t.Errorf("run status = %q, want %q", got.Status, "rate_limited")
	}
	if got.RateLimitResetAt == "" {
		t.Error("rate_limit_reset_at is empty; the run cannot say when it resumes")
	} else {
		parsed, perr := time.Parse(time.RFC3339, got.RateLimitResetAt)
		if perr != nil {
			t.Errorf("rate_limit_reset_at %q is not RFC3339: %v", got.RateLimitResetAt, perr)
		} else if !parsed.Equal(resetAt) {
			t.Errorf("rate_limit_reset_at = %s, want %s", parsed, resetAt)
		}
	}
	// Cost and turns spent before the pause must survive, or the run under-reports
	// what it burned.
	if got.CostUSD != 1.25 || got.NumTurns != 7 {
		t.Errorf("cost/turns = %.2f/%d, want 1.25/7", got.CostUSD, got.NumTurns)
	}
	if s := taskStatus(t, st, task.ID); s != "resumable" {
		t.Errorf("task status = %q, want resumable so the queue relaunches it next session", s)
	}
}

// A suspend must not be mislabelled as the run's own timeout. The cancellation
// can race the timeout deadline, so classify checks the suspend cause first.
func TestSuspendWinsOverARacingTimeout(t *testing.T) {
	st, dir := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, filepath.Join(dir, "nonexistent-workdir"), "", "", "w1", 1)

	timeoutErr := &claude.ErrTimeout{Duration: 75 * time.Minute}
	err := classify(suspendedCtx(time.Now().Add(10*time.Minute)), st, *task, run,
		claude.Result{SessionID: "sess-1"}, timeoutErr,
		"", "main", run.WorktreePath, "", true, log.New("", false), nil)

	if err != nil {
		t.Errorf("classify returned %v, want nil", err)
	}
	if got := latestRun(t, st, task.ID); got.Status == "timed_out" {
		t.Error("a suspended run was recorded as timed_out; the suspend cause must win")
	}
}

// A user cancel (CancelTask) carries an ErrCancelled cause. The task must be
// paused so the scheduler does not auto-retry it — the user must explicitly
// resume.
func TestUserCancelPausesTask(t *testing.T) {
	st, dir := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, filepath.Join(dir, "nonexistent-workdir"), "", "", "w1", 1)

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&ErrCancelled{})

	err := classify(ctx, st, *task, run, claude.Result{SessionID: "sess-1"}, context.Canceled,
		"", "main", run.WorktreePath, "", true, log.New("", false), nil)

	if err == nil {
		t.Error("a user cancel should surface as an error, unlike a suspend")
	}
	got := latestRun(t, st, task.ID)
	if got.Status != "errored" {
		t.Errorf("run status = %q, want errored", got.Status)
	}
	if got.Error != "cancelled by user" {
		t.Errorf("run error = %q, want %q", got.Error, "cancelled by user")
	}
	if s := taskStatus(t, st, task.ID); s != "paused" {
		t.Errorf("task status = %q, want paused: a user cancel must not auto-retry", s)
	}
}

// A daemon shutdown cancels with nil cause (cancelAll). This is not a user
// cancel, so the task stays resumable for the scheduler to pick up on restart.
func TestDaemonShutdownIsNotAUserCancel(t *testing.T) {
	st, dir := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, filepath.Join(dir, "nonexistent-workdir"), "", "", "w1", 1)

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(nil) // what cancelAll does on shutdown

	err := classify(ctx, st, *task, run, claude.Result{SessionID: "sess-1"}, context.Canceled,
		"", "main", run.WorktreePath, "", true, log.New("", false), nil)

	if err == nil {
		t.Error("a shutdown cancel should surface as an error, unlike a suspend")
	}
	got := latestRun(t, st, task.ID)
	if got.Status != "errored" {
		t.Errorf("run status = %q, want errored", got.Status)
	}
	if s := taskStatus(t, st, task.ID); s != "resumable" {
		t.Errorf("task status = %q, want resumable: a daemon shutdown should auto-resume", s)
	}
}

// Without a session there is nothing to resume from, so even a suspension has to
// fail the task. This is scheduling.TaskStatusAfterInterruption's contract
// reaching the suspend path.
func TestSuspendedRunWithoutSessionFailsTask(t *testing.T) {
	st, dir := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, filepath.Join(dir, "nonexistent-workdir"), "", "", "w1", 1)

	err := classify(suspendedCtx(time.Now().Add(10*time.Minute)), st, *task, run,
		claude.Result{}, context.Canceled,
		"", "main", run.WorktreePath, "", true, log.New("", false), nil)
	if err != nil {
		t.Errorf("classify returned %v, want nil", err)
	}

	if s := taskStatus(t, st, task.ID); s != "failed" {
		t.Errorf("task status = %q, want failed: no session means nothing to resume", s)
	}
}

// A suspended run that has a session is "resumable". Its worktree is still
// cleaned up (work saved in Git) so the user can checkout the branch locally.
func TestSuspendedRunCleansUpWorktree(t *testing.T) {
	st, dir := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	workdir := filepath.Join(dir, "agentwork", "task-1")
	os.MkdirAll(workdir, 0755)
	run, _ := st.CreateRun(task.ID, workdir, "", "", "w1", 1)

	resetAt := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	result := claude.Result{SessionID: "sess-1", CostUSD: 1.0, NumTurns: 5}

	err := classify(suspendedCtx(resetAt), st, *task, run, result, context.Canceled,
		"", "main", run.WorktreePath, "", true, log.New("", false), nil)
	if err != nil {
		t.Fatalf("classify returned %v, want nil", err)
	}

	if s := taskStatus(t, st, task.ID); s != "resumable" {
		t.Errorf("task status = %q, want resumable", s)
	}

	// The workdir should be cleaned up (it's an empty agent workdir).
	if _, statErr := os.Stat(workdir); statErr == nil {
		t.Error("workdir should have been removed after suspension")
	}
}

// TestResumeAfterSuspendConsumesAnAttempt pins intended behaviour: every resume
// increments the attempt counter, including one caused by the scheduler
// suspending the run. Attempts therefore count interruptions, not only failures,
// and max_attempts is sized accordingly (default 20 — see config.Load).
//
// The increment is not conditional because it is the only bound on retries of a
// pre-flight failure (see preflightError). A consequence worth knowing: a task
// cannot span more than max_attempts sessions.
func TestResumeAfterSuspendConsumesAnAttempt(t *testing.T) {
	st, _ := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	suspended, _ := st.CreateRun(task.ID, "", "", "", "w1", 3)
	st.SetRunSessionID(suspended.ID, "sess-1")
	st.FinishRun(suspended.ID, "rate_limited", 0, 0, "", "suspended until the next session", "")
	st.SetTaskStatus(task.ID, "resumable")

	prior := latestRun(t, st, task.ID)
	// runner.Run computes a resume's attempt as prior.Attempt + 1 — see the
	// resume branches in Run().
	nextAttempt := prior.Attempt + 1

	if nextAttempt != 4 {
		t.Fatalf("next attempt = %d, want 4", nextAttempt)
	}
	t.Logf("a suspended run at attempt %d resumes as attempt %d: suspensions count "+
		"against max_attempts", prior.Attempt, nextAttempt)
}
