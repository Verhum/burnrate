package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAddComment_EmptyBody(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	body, _ := json.Marshal(map[string]string{"body": ""})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddComment_InvalidJSON(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("task", "prompt", "", "small", "", "")

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddComment_TaskNotFound(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{"body": "hello"})
	req := httptest.NewRequest("POST", "/api/tasks/99999/comments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddComment_QueuedTask(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("queued-task", "prompt", "", "small", "", "")
	// Default status is "queued"

	body, _ := json.Marshal(map[string]string{"body": "a comment"})
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

func TestAddComment_BacklogTask(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("backlog-task", "prompt", "", "small", "", "backlog")

	body, _ := json.Marshal(map[string]string{"body": "backlog comment"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/comments", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "backlog" {
		t.Fatalf("expected status=backlog, got %s", got.Status)
	}
}

func TestAddComment_FailedTaskRequeuesQueued(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("failed-task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "", "", "")
	st.SetTaskStatus(task.ID, "failed")

	body, _ := json.Marshal(map[string]string{"body": "retry please"})
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

func TestAddComment_FailedTaskRequeuesResumable(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("failed-resumable", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-456")
	st.FinishRun(run.ID, "errored", 0.5, 3, "", "some error", "")
	st.SetTaskStatus(task.ID, "failed")

	body, _ := json.Marshal(map[string]string{"body": "fix and continue"})
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

func TestAddComment_DismissedTaskRequeuesQueued(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("dismissed-task", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "", "", "")
	st.SetTaskStatus(task.ID, "dismissed")

	body, _ := json.Marshal(map[string]string{"body": "un-dismiss"})
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

func TestListComments_Empty(t *testing.T) {
	s, st := testServer(t)
	task, _ := st.CreateTask("no-comments", "prompt", "", "small", "", "")

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/tasks/%d/comments", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var comments []map[string]any
	json.NewDecoder(rec.Body).Decode(&comments)
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(comments))
	}
}

func TestListComments_TaskNotFound(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/tasks/99999/comments", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var comments []map[string]any
	json.NewDecoder(rec.Body).Decode(&comments)
	if len(comments) != 0 {
		t.Fatalf("expected empty array, got %d comments", len(comments))
	}
}
