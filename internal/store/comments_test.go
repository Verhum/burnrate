package store

import (
	"path/filepath"
	"testing"
)

func commentStore(t *testing.T) (*Store, int64) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	task, err := st.CreateTask("t", "p", "", "medium", "", "queued")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return st, task.ID
}

func TestGetComment(t *testing.T) {
	st, taskID := commentStore(t)

	added, err := st.AddComment(taskID, "the body", "user")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}

	got, err := st.GetComment(added.ID)
	if err != nil {
		t.Fatalf("get comment: %v", err)
	}
	if got.Body != "the body" || got.Author != "user" || got.TaskID != taskID {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	if _, err := st.GetComment(9999); err == nil {
		t.Fatal("expected an error for a missing comment")
	}
}

// MarkCommentConsumed retires exactly one comment. The sweeping
// MarkCommentsConsumed would take the whole thread with it, which is why the
// in-band reply path needs this narrower one.
func TestMarkCommentConsumedRetiresOnlyThatComment(t *testing.T) {
	st, taskID := commentStore(t)

	delivered, _ := st.AddComment(taskID, "handed to the agent in-band", "user")
	other, _ := st.AddComment(taskID, "still needs delivering", "user")

	if err := st.MarkCommentConsumed(delivered.ID, 42); err != nil {
		t.Fatalf("mark consumed: %v", err)
	}

	got, _ := st.GetComment(delivered.ID)
	if got.ConsumedByRun != 42 {
		t.Fatalf("expected consumed_by_run 42, got %d", got.ConsumedByRun)
	}
	stillOpen, _ := st.GetComment(other.ID)
	if stillOpen.ConsumedByRun != 0 {
		t.Fatalf("the other comment must be untouched, got %d", stillOpen.ConsumedByRun)
	}

	unconsumed, err := st.UnconsumedComments(taskID)
	if err != nil {
		t.Fatalf("unconsumed: %v", err)
	}
	if len(unconsumed) != 1 || unconsumed[0].ID != other.ID {
		t.Fatalf("expected only the undelivered comment to remain, got %+v", unconsumed)
	}
}

// runID 0 is the "unconsumed" sentinel in this column, so a delivery that has
// no run to attribute itself to must still take the comment out of the queue.
func TestMarkCommentConsumedWithZeroRunStillConsumes(t *testing.T) {
	st, taskID := commentStore(t)

	c, _ := st.AddComment(taskID, "delivered with no run", "user")
	if err := st.MarkCommentConsumed(c.ID, 0); err != nil {
		t.Fatalf("mark consumed: %v", err)
	}

	got, _ := st.GetComment(c.ID)
	if got.ConsumedByRun == 0 {
		t.Fatal("run 0 must be stored as a non-zero sentinel, or the comment stays unconsumed")
	}

	unconsumed, _ := st.UnconsumedComments(taskID)
	if len(unconsumed) != 0 {
		t.Fatalf("expected nothing unconsumed, got %d", len(unconsumed))
	}
}

// The sweeping variant keeps its behaviour: everything open on the task goes.
func TestMarkCommentsConsumedSweepsTask(t *testing.T) {
	st, taskID := commentStore(t)

	st.AddComment(taskID, "one", "user")
	st.AddComment(taskID, "two", "user")

	if err := st.MarkCommentsConsumed(taskID, 7); err != nil {
		t.Fatalf("mark consumed: %v", err)
	}
	unconsumed, _ := st.UnconsumedComments(taskID)
	if len(unconsumed) != 0 {
		t.Fatalf("expected the whole thread consumed, got %d", len(unconsumed))
	}
}
