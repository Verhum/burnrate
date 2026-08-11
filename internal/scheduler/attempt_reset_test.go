package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/runner"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

// The mirror of TestEvaluateAttemptCap: the same capped task, but with the
// attempt history discounted by a manual reset. It must launch, and the run it
// launches must be numbered attempt 1 rather than carrying on from the cap.
func TestEvaluateAttemptCapClearedByManualReset(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1
	cfg.MaxAttempts = 3

	launchedResume := make(chan *store.Run, 1)
	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		launchedResume <- resume
		return nil
	}

	task, _ := st.CreateTask("capped-task", "prompt", "", "medium", "", "")
	st.SetTaskStatus(task.ID, "resumable")
	r, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 3)
	st.SetRunSessionID(r.ID, "sess-123")
	st.SetRunStatus(r.ID, "rate_limited")

	// The user re-queues the task by hand, which is what the service layer does.
	if _, err := st.ResetTaskAttempts(task.ID); err != nil {
		t.Fatalf("reset attempts: %v", err)
	}

	sched := New(st, cfg, usage.NewClient("http://unused"), log.New("", false))
	sched.LaunchFunc = fakeLaunch

	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	})
	sched.evaluate(t.Context())

	select {
	case resume := <-launchedResume:
		if resume == nil {
			t.Fatal("expected the capped task to relaunch as a resume")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a task whose attempts were reset should launch, not stay capped")
	}

	got, _ := st.GetTask(task.ID)
	if got.Status == "failed" {
		t.Fatal("task was failed despite the reset")
	}
}
