package service

import (
	"context"
	"testing"

	"github.com/Verhum/burnrate/internal/domain"
)

// Attempts count interruptions as well as failures, so a task that has been
// carried across enough sessions eventually hits max_attempts and is failed.
// Every route by which a *user* touches a task by hand has to clear that
// history, otherwise re-queueing a capped task just fails it again on the next
// scheduler tick and the UI offers no way out.

func TestUpdateTaskResetsAttempts(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "failed"}
	h.tasks.newestRunID = 42

	if _, err := h.svc.UpdateTask(context.Background(), 1, UpdateTaskInput{
		Title:  "reworded",
		Prompt: "a different ask",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if got := h.tasks.tasks[1].AttemptResetRunID; got != 42 {
		t.Fatalf("AttemptResetRunID = %d, want 42 — an edited task is not the task those attempts were spent on", got)
	}
}

func TestSetTaskStatusResetsAttemptsWhenSchedulable(t *testing.T) {
	for _, tc := range []struct {
		to        string
		hasRun    bool
		wantReset bool
	}{
		{to: "queued", wantReset: true},
		{to: "resumable", hasRun: true, wantReset: true},
		{to: "backlog", wantReset: false},
		{to: "paused", wantReset: false},
		{to: "done", wantReset: false},
		{to: "dismissed", wantReset: false},
		{to: "failed", wantReset: false},
	} {
		t.Run(tc.to, func(t *testing.T) {
			h := newHarness()
			h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "failed"}
			h.tasks.newestRunID = 7
			if tc.hasRun {
				h.runs.latestRuns[1] = &domain.Run{ID: 7, TaskID: 1, SessionID: "sess-1"}
			}

			if _, err := h.svc.SetTaskStatus(context.Background(), 1, tc.to); err != nil {
				t.Fatalf("set status %q: %v", tc.to, err)
			}

			reset := h.tasks.tasks[1].AttemptResetRunID != 0
			if reset != tc.wantReset {
				t.Errorf("moving to %q reset attempts = %v, want %v", tc.to, reset, tc.wantReset)
			}
		})
	}
}

// backlog -> queued can land on "resumable" instead. The reset keys off the
// status actually written, so the upgraded case must reset too.
func TestSetTaskStatusResetsAttemptsOnBacklogUpgrade(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "backlog"}
	h.tasks.newestRunID = 9
	h.runs.latestRuns[1] = &domain.Run{
		ID: 9, TaskID: 1, SessionID: "sess-1", WorktreePath: t.TempDir(),
	}

	status, err := h.svc.SetTaskStatus(context.Background(), 1, "queued")
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if status != "resumable" {
		t.Fatalf("status = %q, want resumable", status)
	}
	if got := h.tasks.tasks[1].AttemptResetRunID; got != 9 {
		t.Fatalf("AttemptResetRunID = %d, want 9", got)
	}
}

func TestResumeTaskResetsAttempts(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "paused"}
	h.tasks.newestRunID = 3

	if _, err := h.svc.ResumeTask(context.Background(), 1); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := h.tasks.tasks[1].AttemptResetRunID; got != 3 {
		t.Fatalf("AttemptResetRunID = %d, want 3", got)
	}
}

// A comment re-queues the task, so it clears the attempt history too — a task
// that gave up at max_attempts would otherwise be failed again on the next tick
// and never read the correction that was just written for it.
func TestAddCommentResetsAttempts(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "failed"}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	if _, _, err := svc.AddComment(context.Background(), 1, "try it this way instead"); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if len(taskRepo.attemptResets) != 1 {
		t.Fatalf("attempt resets = %d, want 1", len(taskRepo.attemptResets))
	}
}

// A backlog task is not re-queued by a comment, so nothing about its (unstarted)
// attempt history changes either.
func TestAddCommentOnBacklogDoesNotResetAttempts(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "backlog"}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	if _, _, err := svc.AddComment(context.Background(), 1, "note for later"); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if len(taskRepo.attemptResets) != 0 {
		t.Errorf("attempt resets = %d, want 0 — a backlog task is not re-queued", len(taskRepo.attemptResets))
	}
}

// The scheduler setting a status is not a manual update — only the service's
// user-facing entry points reset. Nothing else calls ResetTaskAttempts, so a
// task suspended and relaunched by the daemon keeps walking toward the cap.
func TestSchedulerStatusWritesDoNotResetAttempts(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "resumable"}
	h.tasks.newestRunID = 5

	if err := h.tasks.SetTaskStatus(1, "running"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if len(h.tasks.attemptResets) != 0 {
		t.Error("a bare repository status write must not reset attempts")
	}
}
