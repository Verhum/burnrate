package server

import (
	"encoding/json"
	"net/http"

	"github.com/Verhum/burnrate/internal/store"
)

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	comments, err := s.commentSvc.ListComments(r.Context(), id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if comments == nil {
		comments = []store.Comment{}
	}
	writeJSON(w, 200, comments)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	comment, _, err := s.commentSvc.AddComment(r.Context(), id, body.Body)
	if err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 201, comment)
}
