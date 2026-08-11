package store

import (
	"testing"
)

func TestHumanRequestCRUD(t *testing.T) {
	st := testStore(t)

	task, err := st.CreateTask("test task", "do something", "", "medium", "", "queued")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req, err := st.CreateHumanRequest(task.ID, 0, "question", "Does it look right?", "The modal is off-center")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req.Kind != "question" || req.Status != "pending" || req.Title != "Does it look right?" {
		t.Fatalf("unexpected request: %+v", req)
	}

	got, err := st.GetHumanRequest(req.ID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got.TaskID != task.ID || got.Body != "The modal is off-center" {
		t.Fatalf("get mismatch: %+v", got)
	}

	requests, err := st.ListHumanRequests("")
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}

	pending, err := st.ListHumanRequests("pending")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	count, err := st.PendingRequestCount()
	if err != nil {
		t.Fatalf("pending count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	if err := st.SetHumanRequestStatus(req.ID, "answered"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	got, _ = st.GetHumanRequest(req.ID)
	if got.Status != "answered" {
		t.Fatalf("expected answered, got %s", got.Status)
	}

	count, _ = st.PendingRequestCount()
	if count != 0 {
		t.Fatalf("expected 0 pending after answer, got %d", count)
	}
}

func TestHumanRequestLive(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")
	req, _ := st.CreateHumanRequest(task.ID, 0, "question", "q", "body")

	if err := st.SetHumanRequestLive(req.ID, true); err != nil {
		t.Fatalf("set live: %v", err)
	}
	got, _ := st.GetHumanRequest(req.ID)
	if !got.Live {
		t.Fatal("expected live=true")
	}

	if err := st.SetHumanRequestLive(req.ID, false); err != nil {
		t.Fatalf("unset live: %v", err)
	}
	got, _ = st.GetHumanRequest(req.ID)
	if got.Live {
		t.Fatal("expected live=false")
	}
}

// A daemon restart leaves `live` set on requests whose long poll died with the
// previous process; ClearHumanRequestLive is the startup reconciliation that
// stops them sorting first and wearing a LIVE badge forever.
func TestClearHumanRequestLive(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")
	stale1, _ := st.CreateHumanRequest(task.ID, 0, "question", "q1", "")
	stale2, _ := st.CreateHumanRequest(task.ID, 0, "question", "q2", "")
	quiet, _ := st.CreateHumanRequest(task.ID, 0, "question", "q3", "")
	// Answered rows can carry the flag too, and must not be left behind.
	answered, _ := st.CreateHumanRequest(task.ID, 0, "question", "q4", "")

	st.SetHumanRequestLive(stale1.ID, true)
	st.SetHumanRequestLive(stale2.ID, true)
	st.SetHumanRequestLive(answered.ID, true)
	st.SetHumanRequestStatus(answered.ID, "answered")

	n, err := st.ClearHumanRequestLive()
	if err != nil {
		t.Fatalf("clear live: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 rows cleared, got %d", n)
	}

	for _, id := range []int64{stale1.ID, stale2.ID, quiet.ID, answered.ID} {
		got, err := st.GetHumanRequest(id)
		if err != nil {
			t.Fatalf("get request %d: %v", id, err)
		}
		if got.Live {
			t.Fatalf("request %d still live after clear", id)
		}
	}

	// Idempotent: a second daemon start has nothing left to reconcile.
	n, err = st.ClearHumanRequestLive()
	if err != nil {
		t.Fatalf("clear live (second call): %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows on a second clear, got %d", n)
	}
}

func TestCancelTaskRequests(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")
	st.CreateHumanRequest(task.ID, 0, "question", "q1", "")
	st.CreateHumanRequest(task.ID, 0, "question", "q2", "")

	count, _ := st.PendingRequestCount()
	if count != 2 {
		t.Fatalf("expected 2 pending, got %d", count)
	}

	if err := st.CancelTaskRequests(task.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	count, _ = st.PendingRequestCount()
	if count != 0 {
		t.Fatalf("expected 0 after cancel, got %d", count)
	}
}

func TestRequestCountsForRun(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")

	// A run nobody asked anything about.
	total, pending, err := st.RequestCountsForRun(99)
	if err != nil {
		t.Fatalf("counts for empty run: %v", err)
	}
	if total != 0 || pending != 0 {
		t.Fatalf("expected 0/0 for a run with no requests, got %d/%d", total, pending)
	}

	r1, _ := st.CreateHumanRequest(task.ID, 7, "question", "q1", "")
	r2, _ := st.CreateHumanRequest(task.ID, 7, "question", "q2", "")
	// A request on a different run must not be counted.
	st.CreateHumanRequest(task.ID, 8, "question", "other", "")

	total, pending, _ = st.RequestCountsForRun(7)
	if total != 2 || pending != 2 {
		t.Fatalf("expected 2/2, got %d/%d", total, pending)
	}

	st.SetHumanRequestStatus(r1.ID, "answered")
	total, pending, _ = st.RequestCountsForRun(7)
	if total != 2 || pending != 1 {
		t.Fatalf("expected 2/1 after one answer, got %d/%d", total, pending)
	}

	st.SetHumanRequestStatus(r2.ID, "denied")
	total, pending, _ = st.RequestCountsForRun(7)
	if total != 2 || pending != 0 {
		t.Fatalf("expected 2/0 once nothing is pending, got %d/%d", total, pending)
	}

	total, pending, _ = st.RequestCountsForRun(8)
	if total != 1 || pending != 1 {
		t.Fatalf("expected 1/1 for the other run, got %d/%d", total, pending)
	}
}

func TestListHumanRequests_LiveFirst(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")
	r1, _ := st.CreateHumanRequest(task.ID, 0, "question", "first", "")
	r2, _ := st.CreateHumanRequest(task.ID, 0, "question", "second", "")

	st.SetHumanRequestLive(r2.ID, true)

	requests, _ := st.ListHumanRequests("pending")
	if len(requests) != 2 {
		t.Fatalf("expected 2, got %d", len(requests))
	}
	if requests[0].ID != r2.ID {
		t.Fatalf("expected live request (id=%d) first, got id=%d", r2.ID, requests[0].ID)
	}
	if requests[1].ID != r1.ID {
		t.Fatalf("expected non-live request (id=%d) second, got id=%d", r1.ID, requests[1].ID)
	}
}
