package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Verhum/burnrate/internal/service"
	"github.com/Verhum/burnrate/internal/store"
)

func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	requests, err := s.requestSvc.List(r.Context(), status)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if requests == nil {
		requests = []store.HumanRequest{}
	}
	writeJSON(w, 200, requests)
}

func (s *Server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID int64  `json:"task_id"`
		RunID  int64  `json:"run_id"`
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	req, err := s.requestSvc.Create(r.Context(), body.TaskID, body.RunID, body.Kind, body.Title, body.Body)
	if err != nil {
		serviceError(w, err)
		return
	}
	// The broadcast lives in RequestService.Create so that MCP-created requests
	// get it too.
	writeJSON(w, 201, req)
}

func (s *Server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid request id")
		return
	}
	req, err := s.requestSvc.Get(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, req)
}

func (s *Server) handleAwaitRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid request id")
		return
	}
	timeoutSec := 55
	if v := r.URL.Query().Get("timeout_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSec = n
		}
	}
	out, err := s.requestSvc.AwaitResponse(r.Context(), id, timeoutSec)
	if err != nil {
		// A missing id is a 404 now: AwaitResponse checks existence before it
		// touches the row, so this no longer surfaces a raw SQL error as a 500.
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, out.Request)
}

func (s *Server) handleRespondRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid request id")
		return
	}
	var body struct {
		Body   string `json:"body"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	comment, err := s.requestSvc.Respond(r.Context(), service.RespondInput{
		RequestID: id,
		Body:      body.Body,
		Result:    body.Result,
	})
	if err != nil {
		serviceError(w, err)
		return
	}
	// Requests + status broadcast from RequestService.Respond; the task list is
	// the server's own concern (responding can re-queue a parked task).
	s.broadcastTasks()
	writeJSON(w, 200, comment)
}

func (s *Server) handleApproveRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid request id")
		return
	}
	var body struct {
		Scope string `json:"scope"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := s.requestSvc.Approve(r.Context(), id, body.Scope); err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "approved"})
}

func (s *Server) handleDenyRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid request id")
		return
	}
	if err := s.requestSvc.Deny(r.Context(), id); err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "denied"})
}

func (s *Server) broadcastRequests() {
	requests, err := s.st.ListHumanRequests("pending")
	if err != nil {
		return
	}
	if requests == nil {
		requests = []store.HumanRequest{}
	}
	s.hub.broadcast("request", requests)
}
