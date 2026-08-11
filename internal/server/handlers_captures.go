package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	"github.com/Verhum/burnrate/internal/domain"
)

type Capture = domain.Capture

func (s *Server) handleCreateCapture(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID     int64  `json:"task_id"`
		RequestID  int64  `json:"request_id"`
		Initiator  string `json:"initiator"`
		TargetDesc string `json:"target_desc"`
		Mode       string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	cap, err := s.captureSvc.Create(r.Context(), body.TaskID, body.RequestID, body.Initiator, body.TargetDesc, body.Mode)
	if err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 201, cap)
}

func (s *Server) handleGetCapture(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid capture id")
		return
	}
	cap, err := s.captureSvc.Get(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, cap)
}

func (s *Server) handleListCaptures(w http.ResponseWriter, r *http.Request) {
	var taskID int64
	if v := r.URL.Query().Get("task_id"); v != "" {
		taskID, _ = strconv.ParseInt(v, 10, 64)
	}
	caps, err := s.captureSvc.List(r.Context(), taskID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if caps == nil {
		caps = []Capture{}
	}
	writeJSON(w, 200, caps)
}

// handleCaptureVideo serves a finished capture's video file. Without this route
// the path fell through to the SPA handler, which answered 200 with index.html
// — a player asking for a missing recording got an HTML page, not an error.
func (s *Server) handleCaptureVideo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid capture id")
		return
	}
	cap, err := s.captureSvc.Get(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}
	if cap.VideoPath == "" {
		writeError(w, 404, "capture has no video")
		return
	}
	info, statErr := os.Stat(cap.VideoPath)
	if statErr != nil || !info.Mode().IsRegular() {
		writeError(w, 404, "capture video file is missing")
		return
	}
	http.ServeFile(w, r, cap.VideoPath)
}

func (s *Server) handleFinishCapture(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid capture id")
		return
	}
	var body struct {
		VideoPath   string  `json:"video_path"`
		Transcript  string  `json:"transcript"`
		DurationSec float64 `json:"duration_sec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	// Finishing a capture is the claim "this recording exists and is playable".
	// Accepting a path to nothing marked the row `ready` and left the UI
	// offering a video that could never load, so the claim is checked here.
	if body.DurationSec < 0 {
		writeError(w, 400, "duration_sec must not be negative")
		return
	}
	if body.VideoPath != "" {
		info, statErr := os.Stat(body.VideoPath)
		if statErr != nil || !info.Mode().IsRegular() {
			// Failed rather than left `processing`: the capture had its chance
			// and produced nothing usable.
			s.captureSvc.Fail(r.Context(), id)
			writeError(w, 400, "video_path does not name an existing file")
			return
		}
	}
	if err := s.captureSvc.Finish(r.Context(), id, body.VideoPath, body.Transcript, body.DurationSec); err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

func (s *Server) handleSetCaptureNotes(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid capture id")
		return
	}
	var body struct {
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := s.captureSvc.SetNotes(r.Context(), id, body.Notes); err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "updated"})
}
