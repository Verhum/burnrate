package runner

import (
	"testing"

	"github.com/Verhum/burnrate/internal/domain"
)

// The counterpart to TestResumeAfterSuspendConsumesAnAttempt. Attempts chain
// across runs and count interruptions, so a task carried across enough sessions
// walks to max_attempts and is failed. A manual edit or re-queue discounts that
// history, and the next resume is numbered attempt 1 again — which is the number
// runner.Run writes onto the new run row.
func TestResumeAfterManualResetStartsAtAttemptOne(t *testing.T) {
	st, _ := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	suspended, _ := st.CreateRun(task.ID, "", "", "", "w1", 19)
	st.SetRunSessionID(suspended.ID, "sess-1")
	st.FinishRun(suspended.ID, "rate_limited", 0, 0, "", "suspended until the next session", "")
	st.SetTaskStatus(task.ID, "resumable")

	if _, err := st.ResetTaskAttempts(task.ID); err != nil {
		t.Fatalf("reset attempts: %v", err)
	}
	reset, _ := st.GetTask(task.ID)

	prior := latestRun(t, st, task.ID)
	if got := domain.EffectiveAttempt(reset.AttemptResetRunID, prior) + 1; got != 1 {
		t.Fatalf("next attempt = %d, want 1 after a manual reset", got)
	}

	// The run row itself is untouched: history says what actually happened.
	if prior.Attempt != 19 {
		t.Errorf("prior run attempt = %d, want 19 — a reset must not rewrite run history", prior.Attempt)
	}
}

// Runs recorded after the reset chain normally, so the cap still bounds work
// done since the user last intervened.
func TestAttemptsChainAgainAfterAReset(t *testing.T) {
	st, _ := suspendTestStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	old, _ := st.CreateRun(task.ID, "", "", "", "w1", 19)
	st.FinishRun(old.ID, "rate_limited", 0, 0, "", "", "")
	st.ResetTaskAttempts(task.ID)
	reset, _ := st.GetTask(task.ID)

	first, _ := st.CreateRun(task.ID, "", "", "", "w2", 1)
	st.FinishRun(first.ID, "rate_limited", 0, 0, "", "", "")

	prior := latestRun(t, st, task.ID)
	if got := domain.EffectiveAttempt(reset.AttemptResetRunID, prior) + 1; got != 2 {
		t.Fatalf("next attempt = %d, want 2", got)
	}
}
