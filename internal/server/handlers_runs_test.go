package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListRuns_Empty(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/runs", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var runs []map[string]any
	json.NewDecoder(rec.Body).Decode(&runs)
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(runs))
	}
}

func TestListRuns_WithRuns(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	r1, _ := st.CreateRun(task.ID, "/wt/1", "b1", "/repo", "w1", 1)
	st.FinishRun(r1.ID, "succeeded", 1.0, 5, "", "", "")
	r2, _ := st.CreateRun(task.ID, "/wt/2", "b2", "/repo", "w2", 2)
	st.FinishRun(r2.ID, "succeeded", 2.0, 10, "", "", "")

	req := httptest.NewRequest("GET", "/api/runs", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var runs []map[string]any
	json.NewDecoder(rec.Body).Decode(&runs)
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestListRuns_FilterByTaskID(t *testing.T) {
	s, st := testServer(t)

	task1, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	task2, _ := st.CreateTask("task2", "prompt", "", "small", "", "")
	r1, _ := st.CreateRun(task1.ID, "/wt/1", "b1", "/repo", "w1", 1)
	st.FinishRun(r1.ID, "succeeded", 1.0, 5, "", "", "")
	r2, _ := st.CreateRun(task2.ID, "/wt/2", "b2", "/repo", "w2", 1)
	st.FinishRun(r2.ID, "succeeded", 2.0, 10, "", "", "")

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/runs?task_id=%d", task1.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var runs []map[string]any
	json.NewDecoder(rec.Body).Decode(&runs)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	gotTaskID := int64(runs[0]["task_id"].(float64))
	if gotTaskID != task1.ID {
		t.Fatalf("expected task_id=%d, got %d", task1.ID, gotTaskID)
	}
}

func TestRunResume_WithSession(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt/agentwork/task-1", "b1", "/repo", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-123")

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/runs/%d/resume", run.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["session_id"] != "sess-123" {
		t.Fatalf("expected session_id=sess-123, got %v", resp["session_id"])
	}
	cmd, _ := resp["command"].(string)
	if !strings.Contains(cmd, "cd '/wt/agentwork/task-1'") {
		t.Errorf("command must cd into the worktree, got %q", cmd)
	}
	if !strings.Contains(cmd, "claude --resume 'sess-123'") {
		t.Errorf("command must resume the session, got %q", cmd)
	}
}

func TestRunResume_NoSession(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "b1", "/repo", "w1", 1)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/runs/%d/resume", run.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["command"] != "" {
		t.Fatalf("a run with no session must offer no command, got %v", resp["command"])
	}
}

func TestRunResume_NotFound(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/runs/99999/resume", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelRun_ActiveRun(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	st.SetTaskStatus(task.ID, "running")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/cancel", run.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "cancelled" {
		t.Fatalf("expected status=cancelled, got %s", resp["status"])
	}

	// Verify run is abandoned
	runs, _ := st.ListRuns(task.ID, 10)
	for _, r := range runs {
		if r.ID == run.ID {
			if r.Status != "abandoned" {
				t.Fatalf("expected run status=abandoned, got %s", r.Status)
			}
		}
	}

	// Verify task is paused (user cancel never auto-retries)
	got, _ := st.GetTask(task.ID)
	if got.Status != "paused" {
		t.Fatalf("expected task status=paused, got %s", got.Status)
	}
}

func TestCancelRun_ActiveRunWithSession(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	st.SetTaskStatus(task.ID, "running")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-123")

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/cancel", run.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "cancelled" {
		t.Fatalf("expected status=cancelled, got %s", resp["status"])
	}

	// Verify task is paused (user cancel never auto-retries, even with session)
	got, _ := st.GetTask(task.ID)
	if got.Status != "paused" {
		t.Fatalf("expected task status=paused, got %s", got.Status)
	}
}

func TestCancelRun_NotActiveRun(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.FinishRun(run.ID, "succeeded", 1.0, 5, "", "", "")

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/runs/%d/cancel", run.ID), nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelRun_NotFound(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("POST", "/api/runs/99999/cancel", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
