package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Verhum/burnrate/internal/config"
	brlog "github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/scheduler"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
	"github.com/Verhum/burnrate/internal/whisper"
)

func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg, _ := config.Load(nil)
	cfg.DryRun = true
	logger := brlog.New("", false)
	client := usage.NewClient("http://unused")
	sched := scheduler.New(st, cfg, client, logger)

	s := New(st, cfg, sched, nil, whisper.New(dir), logger)
	return s, st
}

func TestCommentOnRunningTask409(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("running-task", "prompt", "", "small", "", "")
	st.SetTaskStatus(task.ID, "running")

	body, _ := json.Marshal(map[string]string{"body": "follow up"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCommentOnDoneTaskRequeuesQueued(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("done-task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "https://github.com/pr/1", "", "")
	st.SetTaskStatus(task.ID, "done")

	body, _ := json.Marshal(map[string]string{"body": "add more tests"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "queued" {
		t.Fatalf("expected status=queued, got %s", got.Status)
	}
}

func TestCommentOnDoneTaskRequeuesResumable(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("resumable-task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-123")
	st.FinishRun(run.ID, "errored", 0.5, 3, "", "some error", "")
	st.SetTaskStatus(task.ID, "done")

	body, _ := json.Marshal(map[string]string{"body": "continue with fix"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "resumable" {
		t.Fatalf("expected status=resumable, got %s", got.Status)
	}
}

func TestListComments(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	st.AddComment(task.ID, "first", "user")
	st.AddComment(task.ID, "second", "user")

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/tasks/%d/comments", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var comments []map[string]any
	json.NewDecoder(rec.Body).Decode(&comments)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
}

func TestDisplayIDInTaskList(t *testing.T) {
	s, st := testServer(t)

	st.CreateTask("alpha", "prompt", "", "small", "", "")
	st.CreateTask("beta", "prompt", "", "small", "", "")

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []map[string]any
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	for _, task := range tasks {
		id := task["id"].(float64)
		expected := fmt.Sprintf("BR%d", int64(id))
		got, ok := task["display_id"].(string)
		if !ok || got != expected {
			t.Fatalf("expected display_id=%s, got %v", expected, task["display_id"])
		}
	}
}

func TestLatestRunStatusInAPI(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("multi-run", "prompt", "", "small", "", "")

	r1, _ := st.CreateRun(task.ID, "/wt/1", "b1", "/r", "w1", 1)
	st.FinishRun(r1.ID, "succeeded", 1.0, 5, "", "", "")

	r2, _ := st.CreateRun(task.ID, "/wt/2", "b2", "/r", "w2", 2)
	st.FinishRun(r2.ID, "errored", 0.5, 3, "", "failed", "")

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []map[string]any
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0]["latest_run_status"] != "errored" {
		t.Fatalf("expected latest_run_status=errored, got %v", tasks[0]["latest_run_status"])
	}
}

func TestBacklogToQueuedPromotion(t *testing.T) {
	t.Run("with_worktree_becomes_resumable", func(t *testing.T) {
		s, st := testServer(t)

		task, err := st.CreateTask("resumable-task", "prompt", "", "small", "", "backlog")
		if err != nil {
			t.Fatalf("create task: %v", err)
		}

		wtDir := t.TempDir()
		run, err := st.CreateRun(task.ID, wtDir, "branch-1", "/repo", "w1", 1)
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		st.SetRunSessionID(run.ID, "sess-123")
		st.FinishRun(run.ID, "rate_limited", 0.5, 5, "", "rate limited", "")

		body, _ := json.Marshal(map[string]string{"status": "queued"})
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		json.NewDecoder(rec.Body).Decode(&resp)
		if resp["status"] != "resumable" {
			t.Fatalf("expected status=resumable in response, got %s", resp["status"])
		}

		got, _ := st.GetTask(task.ID)
		if got.Status != "resumable" {
			t.Fatalf("expected task status=resumable in DB, got %s", got.Status)
		}
	})

	t.Run("without_worktree_becomes_resumable", func(t *testing.T) {
		// Worktrees are cleaned up eagerly (work saved in Git) and recreated
		// on resume, so a missing worktree with a session is still resumable.
		s, st := testServer(t)

		task, err := st.CreateTask("queued-task", "prompt", "", "small", "", "backlog")
		if err != nil {
			t.Fatalf("create task: %v", err)
		}

		run, err := st.CreateRun(task.ID, "/nonexistent/worktree/path", "branch-1", "/repo", "w1", 1)
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		st.SetRunSessionID(run.ID, "sess-456")
		st.FinishRun(run.ID, "rate_limited", 0.5, 5, "", "rate limited", "")

		body, _ := json.Marshal(map[string]string{"status": "queued"})
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		json.NewDecoder(rec.Body).Decode(&resp)
		if resp["status"] != "resumable" {
			t.Fatalf("expected status=resumable in response, got %s", resp["status"])
		}

		got, _ := st.GetTask(task.ID)
		if got.Status != "resumable" {
			t.Fatalf("expected task status=resumable in DB, got %s", got.Status)
		}
	})
}

func TestPrCreatedApproveMovesToDone(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("review-task", "prompt", "", "small", "", "")
	st.SetTaskStatus(task.ID, "pr_created")

	body, _ := json.Marshal(map[string]string{"status": "done"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "done" {
		t.Fatalf("expected done, got %s", got.Status)
	}
}

func TestPrCreatedToBacklogAllowed(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("review-task", "prompt", "", "small", "", "")
	st.SetTaskStatus(task.ID, "pr_created")

	body, _ := json.Marshal(map[string]string{"status": "backlog"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "backlog" {
		t.Fatalf("expected backlog, got %s", got.Status)
	}
}

func TestQueuedToPrCreatedRejectedFromAPI(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	body, _ := json.Marshal(map[string]string{"status": "pr_created"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCommentOnPrCreatedRequeuesQueued(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("pr-task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "https://github.com/pr/1", "", "")
	st.SetTaskStatus(task.ID, "pr_created")

	body, _ := json.Marshal(map[string]string{"body": "needs more tests"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "queued" {
		t.Fatalf("expected queued, got %s", got.Status)
	}
}

func TestCommentOnPrCreatedRequeuesResumable(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("pr-task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-123")
	st.FinishRun(run.ID, "errored", 0.5, 3, "", "some error", "")
	st.SetTaskStatus(task.ID, "pr_created")

	body, _ := json.Marshal(map[string]string{"body": "fix the error"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "resumable" {
		t.Fatalf("expected resumable, got %s", got.Status)
	}
}

func TestNotifyOnReviewConfig(t *testing.T) {
	s, st := testServer(t)

	// Default: notify_on_review should be "true"
	req := httptest.NewRequest("GET", "/api/config", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	var cfg map[string]any
	json.NewDecoder(rec.Body).Decode(&cfg)
	if cfg["notify_on_review"] != "true" {
		t.Fatalf("expected notify_on_review=true, got %v", cfg["notify_on_review"])
	}

	// Set it to false
	body, _ := json.Marshal(map[string]string{"notify_on_review": "false"})
	req = httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Read back
	req = httptest.NewRequest("GET", "/api/config", nil)
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	json.NewDecoder(rec.Body).Decode(&cfg)
	if cfg["notify_on_review"] != "false" {
		t.Fatalf("expected notify_on_review=false after set, got %v", cfg["notify_on_review"])
	}

	_ = st // keep linter happy
}

func TestSetStatusDoneToQueued(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("done-task", "prompt", "", "small", "", "")
	st.SetTaskStatus(task.ID, "done")

	body, _ := json.Marshal(map[string]string{"status": "queued"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := st.GetTask(task.ID)
	if got.Status != "queued" {
		t.Fatalf("expected queued, got %s", got.Status)
	}
}

func TestSetStatusToRunningRejected(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	body, _ := json.Marshal(map[string]string{"status": "running"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetStatusToResumableRejectedWithoutSession(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	body, _ := json.Marshal(map[string]string{"status": "resumable"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetStatusToResumableAllowedWithSession(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "b", "/r", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-123")
	st.FinishRun(run.ID, "errored", 0.5, 3, "", "err", "")

	body, _ := json.Marshal(map[string]string{"status": "resumable"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := st.GetTask(task.ID)
	if got.Status != "resumable" {
		t.Fatalf("expected resumable, got %s", got.Status)
	}
}

func TestCleanupFiresOnDoneFromFailed(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/nonexistent/wt", "b", "/r", "w1", 1)
	st.FinishRun(run.ID, "errored", 0.5, 3, "", "err", "")
	st.SetTaskStatus(task.ID, "failed")

	body, _ := json.Marshal(map[string]string{"status": "done"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := st.GetTask(task.ID)
	if got.Status != "done" {
		t.Fatalf("expected done, got %s", got.Status)
	}
}

func TestCleanupFiresOnDismissedFromFailed(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/nonexistent/wt", "b", "/r", "w1", 1)
	st.FinishRun(run.ID, "errored", 0.5, 3, "", "err", "")
	st.SetTaskStatus(task.ID, "failed")

	body, _ := json.Marshal(map[string]string{"status": "dismissed"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/status", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := st.GetTask(task.ID)
	if got.Status != "dismissed" {
		t.Fatalf("expected dismissed, got %s", got.Status)
	}
}

func TestReorderDuplicateIDs400(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	body, _ := json.Marshal(map[string]any{"ordered_ids": []int64{task.ID, task.ID}})
	req := httptest.NewRequest("POST", "/api/tasks/reorder", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400 for duplicate ids, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReorderUnknownIDs400(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	body, _ := json.Marshal(map[string]any{"ordered_ids": []int64{task.ID, 99999}})
	req := httptest.NewRequest("POST", "/api/tasks/reorder", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400 for unknown ids, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHasSessionInTaskJSON(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	// No run → has_session should be false
	req := httptest.NewRequest("GET", "/api/tasks", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	var tasks []map[string]any
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0]["has_session"] != false {
		t.Fatalf("expected has_session=false with no run, got %v", tasks[0]["has_session"])
	}

	// Add run with session → has_session should be true
	run, _ := st.CreateRun(task.ID, "/wt", "b", "/r", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-123")

	req = httptest.NewRequest("GET", "/api/tasks", nil)
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	json.NewDecoder(rec.Body).Decode(&tasks)
	if tasks[0]["has_session"] != true {
		t.Fatalf("expected has_session=true with session, got %v", tasks[0]["has_session"])
	}
}
