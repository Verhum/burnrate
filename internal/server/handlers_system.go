package server

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleGetCaffeinate(w http.ResponseWriter, _ *http.Request) {
	if s.caff == nil {
		writeJSON(w, 200, map[string]any{"active": false, "mode": "unavailable"})
		return
	}
	writeJSON(w, 200, s.caff.Status())
}

func (s *Server) handleToggleCaffeinate(w http.ResponseWriter, _ *http.Request) {
	if s.caff == nil {
		writeError(w, 500, "caffeinate manager not available")
		return
	}
	active := s.caff.Toggle()
	status := s.caff.Status()
	s.hub.broadcast("caffeinate", status)
	writeJSON(w, 200, map[string]any{"active": active, "status": status})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher.Flush()

	ch, unsub := s.hub.subscribe()
	defer unsub()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
