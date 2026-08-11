package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Verhum/burnrate/internal/service"
	"github.com/Verhum/burnrate/internal/store"
)

func (s *Server) handleListTasks(w http.ResponseWriter, _ *http.Request) {
	tasks, err := s.st.ListTasks()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	writeJSON(w, 200, tasks)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		Prompt   string `json:"prompt"`
		RepoPath string `json:"repo_path"`
		Size     string `json:"size"`
		Model    string `json:"model"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	task, err := s.taskSvc.CreateTask(r.Context(), service.CreateTaskInput{
		Title:    body.Title,
		Prompt:   body.Prompt,
		RepoPath: body.RepoPath,
		Model:    body.Model,
		Status:   body.Status,
	})
	if err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 201, task)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	var body struct {
		Title    string  `json:"title"`
		Prompt   string  `json:"prompt"`
		RepoPath *string `json:"repo_path"`
		Size     string  `json:"size"`
		Model    string  `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	task, err := s.taskSvc.UpdateTask(r.Context(), id, service.UpdateTaskInput{
		Title:    body.Title,
		Prompt:   body.Prompt,
		Model:    body.Model,
		RepoPath: body.RepoPath,
	})
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, task)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	if err := s.taskSvc.DeleteTask(r.Context(), id); err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) handleReorderTasks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderedIDs []int64 `json:"ordered_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := s.taskSvc.ReorderTasks(r.Context(), body.OrderedIDs); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, 400, err.Error())
			return
		}
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, map[string]string{"status": "reordered"})
}

func (s *Server) handlePauseTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	if err := s.taskSvc.PauseTask(r.Context(), id); err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	newStatus, err := s.taskSvc.ResumeTask(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, map[string]string{"status": newStatus})
}

func (s *Server) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	if err := s.taskSvc.CompleteTask(r.Context(), id); err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, map[string]string{"status": "done"})
}

func (s *Server) handleDismissTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	if err := s.taskSvc.DismissTask(r.Context(), id); err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, map[string]string{"status": "dismissed"})
}

func (s *Server) handleSetTaskStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	finalStatus, err := s.taskSvc.SetTaskStatus(r.Context(), id, body.Status)
	if err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 200, map[string]string{"status": finalStatus})
}

func (s *Server) handleTaskStats(w http.ResponseWriter, _ *http.Request) {
	m, err := s.st.TaskStatsMap()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) handleRunNow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	if err := s.taskSvc.RunNow(r.Context(), id); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	s.broadcastTasks()
	s.hub.broadcast("status", s.statusPayload())
	writeJSON(w, 200, map[string]string{"status": "launched"})
}
