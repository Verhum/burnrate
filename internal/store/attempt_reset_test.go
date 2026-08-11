package store

import (
	"testing"

	"github.com/Verhum/burnrate/internal/domain"
)

func TestResetTaskAttemptsRecordsNewestRun(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("t", "p", "", "medium", "", "")

	st.CreateRun(task.ID, "", "", "", "w1", 1)
	r2, _ := st.CreateRun(task.ID, "", "", "", "w1", 2)

	got, err := st.ResetTaskAttempts(task.ID)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got != r2.ID {
		t.Fatalf("reset point = %d, want %d (newest run)", got, r2.ID)
	}

	reloaded, err := st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if reloaded.AttemptResetRunID != r2.ID {
		t.Fatalf("task.AttemptResetRunID = %d, want %d", reloaded.AttemptResetRunID, r2.ID)
	}
	if domain.EffectiveAttempt(reloaded.AttemptResetRunID, r2) != 0 {
		t.Error("the run the reset was taken at should no longer count toward the cap")
	}
}

func TestResetTaskAttemptsWithNoRuns(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("t", "p", "", "medium", "", "")

	got, err := st.ResetTaskAttempts(task.ID)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got != 0 {
		t.Fatalf("reset point = %d, want 0 for a task with no runs", got)
	}
}

// A reset must never move backwards. Run ids are global, so a task that has been
// idle since its own last run would otherwise have a later edit lower its reset
// point below an earlier one and un-discount runs that had already been forgiven.
func TestResetTaskAttemptsNeverMovesBackwards(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("t", "p", "", "medium", "", "")
	other, _ := st.CreateTask("other", "p", "", "medium", "", "")

	r1, _ := st.CreateRun(task.ID, "", "", "", "w1", 1)
	st.ResetTaskAttempts(task.ID)

	// Another task's run advances the global id sequence; ours records nothing new.
	st.CreateRun(other.ID, "", "", "", "w1", 1)

	got, _ := st.ResetTaskAttempts(task.ID)
	if got != r1.ID {
		t.Fatalf("reset point = %d, want %d — a second reset with no new runs must hold", got, r1.ID)
	}
}

// After a reset, runs recorded since it still chain normally: only the history
// before the user's intervention is discounted.
func TestEffectiveAttemptCountsRunsAfterTheReset(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("t", "p", "", "medium", "", "")

	st.CreateRun(task.ID, "", "", "", "w1", 19)
	resetAt, _ := st.ResetTaskAttempts(task.ID)

	after, _ := st.CreateRun(task.ID, "", "", "", "w1", 1)
	if got := domain.EffectiveAttempt(resetAt, after); got != 1 {
		t.Fatalf("EffectiveAttempt after the reset = %d, want 1", got)
	}
}
