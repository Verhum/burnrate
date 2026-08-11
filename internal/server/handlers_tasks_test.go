package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// CreateTask
// ---------------------------------------------------------------------------

func TestCreateTask_Valid(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"title":     "my task",
		"prompt":    "do something",
		"repo_path": "/repo",
	})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var task map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&task); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if task["title"] != "my task" {
		t.Fatalf("expected title='my task', got %v", task["title"])
	}
	if task["prompt"] != "do something" {
		t.Fatalf("expected prompt='do something', got %v", task["prompt"])
	}
	if task["status"] != "queued" {
		t.Fatalf("expected status='queued', got %v", task["status"])
	}
	if task["id"] == nil || task["id"].(float64) == 0 {
		t.Fatal("expected non-zero id")
	}
	if task["display_id"] == nil || task["display_id"] == "" {
		t.Fatal("expected non-empty display_id")
	}
}

func TestCreateTask_MissingTitle(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"prompt": "do something",
	})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTask_InvalidStatus(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"title":  "task",
		"status": "invalid",
	})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTask_DefaultSize(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"title": "task without size",
	})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var task map[string]any
	json.NewDecoder(rec.Body).Decode(&task)
	if task["size"] != "medium" {
		t.Fatalf("expected default size='medium', got %v", task["size"])
	}
}

func TestCreateTask_BacklogStatus(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"title":  "backlog task",
		"status": "backlog",
	})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var task map[string]any
	json.NewDecoder(rec.Body).Decode(&task)
	if task["status"] != "backlog" {
		t.Fatalf("expected status='backlog', got %v", task["status"])
	}
}

// ---------------------------------------------------------------------------
// ListTasks
// ---------------------------------------------------------------------------

func TestListTasks_Empty(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []map[string]any
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected empty array, got %d tasks", len(tasks))
	}
}

func TestListTasks_WithTasks(t *testing.T) {
	s, st := testServer(t)

	st.CreateTask("task-1", "prompt-1", "/repo", "medium", "", "")
	st.CreateTask("task-2", "prompt-2", "/repo", "large", "", "")

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
}

func TestGetTask_NotFound(t *testing.T) {
	// There is no GET single task endpoint; listing with no tasks returns empty array.
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []map[string]any
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

// ---------------------------------------------------------------------------
// UpdateTask
// ---------------------------------------------------------------------------

func TestUpdateTask_Valid(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("original", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	body, _ := json.Marshal(map[string]string{
		"title":     "updated",
		"prompt":    "new prompt",
		"repo_path": "/new-repo",
	})
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/tasks/%d", task.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updated map[string]any
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated["title"] != "updated" {
		t.Fatalf("expected title='updated', got %v", updated["title"])
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"title": "updated",
	})
	req := httptest.NewRequest("PUT", "/api/tasks/99999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// DeleteTask
// ---------------------------------------------------------------------------

func TestDeleteTask_Exists(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("to-delete", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "deleted" {
		t.Fatalf("expected status='deleted', got %v", resp["status"])
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("DELETE", "/api/tasks/99999", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteTask_RunningTask(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("running-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	st.SetTaskStatus(task.ID, "running")

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PauseTask
// ---------------------------------------------------------------------------

func TestPauseTask_Queued(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("queued-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/pause", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "paused" {
		t.Fatalf("expected status='paused', got %v", resp["status"])
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "paused" {
		t.Fatalf("expected DB status='paused', got %s", got.Status)
	}
}

func TestPauseTask_NotQueued(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("paused-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	st.SetTaskStatus(task.ID, "paused")

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/pause", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ResumeTask
// ---------------------------------------------------------------------------

func TestResumeTask_Paused(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("paused-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	st.SetTaskStatus(task.ID, "paused")

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/resume", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	// Without a session the resume should set status to "queued"
	if resp["status"] != "queued" {
		t.Fatalf("expected status='queued', got %v", resp["status"])
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "queued" {
		t.Fatalf("expected DB status='queued', got %s", got.Status)
	}
}

func TestResumeTask_NotPaused(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("queued-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	// Task is "queued" by default, not "paused"

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/resume", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// CompleteTask
// ---------------------------------------------------------------------------

func TestCompleteTask_Valid(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("queued-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/complete", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "done" {
		t.Fatalf("expected status='done', got %v", resp["status"])
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "done" {
		t.Fatalf("expected DB status='done', got %s", got.Status)
	}
}

func TestCompleteTask_RunningTask(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("running-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	st.SetTaskStatus(task.ID, "running")

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/complete", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// DismissTask
// ---------------------------------------------------------------------------

func TestDismissTask_Valid(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("queued-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/dismiss", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "dismissed" {
		t.Fatalf("expected status='dismissed', got %v", resp["status"])
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "dismissed" {
		t.Fatalf("expected DB status='dismissed', got %s", got.Status)
	}
}

func TestDismissTask_RunningTask(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("running-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	st.SetTaskStatus(task.ID, "running")

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/dismiss", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ReorderTasks
// ---------------------------------------------------------------------------

func TestReorderTasks_Success(t *testing.T) {
	s, st := testServer(t)

	t1, _ := st.CreateTask("task-1", "prompt", "/repo", "medium", "", "")
	t2, _ := st.CreateTask("task-2", "prompt", "/repo", "medium", "", "")
	t3, _ := st.CreateTask("task-3", "prompt", "/repo", "medium", "", "")

	// Reverse the order: 3, 2, 1
	body, _ := json.Marshal(map[string]any{
		"ordered_ids": []int64{t3.ID, t2.ID, t1.ID},
	})
	req := httptest.NewRequest("POST", "/api/tasks/reorder", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "reordered" {
		t.Fatalf("expected status='reordered', got %v", resp["status"])
	}

	// Verify the order changed by listing tasks (sorted by sort_order)
	listReq := httptest.NewRequest("GET", "/api/tasks", nil)
	listRec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(listRec, listReq)

	var tasks []map[string]any
	json.NewDecoder(listRec.Body).Decode(&tasks)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	// After reorder, t3 should be first, t2 second, t1 third
	if int64(tasks[0]["id"].(float64)) != t3.ID {
		t.Fatalf("expected first task id=%d, got %v", t3.ID, tasks[0]["id"])
	}
	if int64(tasks[1]["id"].(float64)) != t2.ID {
		t.Fatalf("expected second task id=%d, got %v", t2.ID, tasks[1]["id"])
	}
	if int64(tasks[2]["id"].(float64)) != t1.ID {
		t.Fatalf("expected third task id=%d, got %v", t1.ID, tasks[2]["id"])
	}
}

// TestReorderTasks_DuplicateIDs is already in server_test.go as TestReorderDuplicateIDs400.

// ---------------------------------------------------------------------------
// RunNow
// ---------------------------------------------------------------------------

func TestRunNow_QueuedTask(t *testing.T) {
	s, st := testServer(t)

	task, err := st.CreateTask("queued-task", "prompt", "/repo", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/tasks/%d/run-now", task.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	// The scheduler accepts the run-now request and launches asynchronously
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "launched" {
		t.Fatalf("expected status='launched', got %v", resp["status"])
	}
}

func TestCreateTask_WithModel(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"title":  "model task",
		"prompt": "do something",
		"model":  "claude-sonnet-5",
	})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var task map[string]any
	json.NewDecoder(rec.Body).Decode(&task)
	if task["model"] != "claude-sonnet-5" {
		t.Fatalf("expected model='claude-sonnet-5', got %v", task["model"])
	}
}

func TestListModels(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/models", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var models []map[string]string
	json.NewDecoder(rec.Body).Decode(&models)
	if len(models) == 0 {
		t.Fatal("expected non-empty models list")
	}
	if models[0]["id"] == "" || models[0]["name"] == "" {
		t.Fatalf("model entry missing id or name: %+v", models[0])
	}
}

func TestRunNow_NotFound(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("POST", "/api/tasks/99999/run-now", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	// Non-existent task also causes scheduler.RunNow to error, handler returns 409
	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}
