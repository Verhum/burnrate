package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/scheduler"
)

func (s *Server) handleUsageHistory(w http.ResponseWriter, r *http.Request) {
	hoursStr := r.URL.Query().Get("hours")
	hours := 5.0
	if hoursStr != "" {
		if h, err := strconv.ParseFloat(hoursStr, 64); err == nil && h > 0 && h <= 168 {
			hours = h
		}
	}
	since := time.Now().Add(-time.Duration(hours * float64(time.Hour)))
	snaps, err := s.st.UsageSnapshotsSince(since)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if snaps == nil {
		// A nil slice marshals to `null`, which the UI dereferences as an array.
		writeJSON(w, 200, []any{})
		return
	}
	writeJSON(w, 200, snaps)
}

func (s *Server) handleUsage(w http.ResponseWriter, _ *http.Request) {
	snap, err := s.st.LatestUsageSnapshot()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if snap == nil {
		writeJSON(w, 200, map[string]any{})
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, _ *http.Request) {
	data, err := s.st.Leaderboard()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, data)
}

func (s *Server) handleStreak(w http.ResponseWriter, _ *http.Request) {
	data, err := s.st.Streak()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, data)
}

// maxCostEfficiencyDays bounds the ?days window. A year is more history than the
// chart can render legibly and more than the run table is expected to hold.
const maxCostEfficiencyDays = 365

func (s *Server) handleCostEfficiency(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > maxCostEfficiencyDays {
			writeError(w, 400, "days must be an integer in 1.."+strconv.Itoa(maxCostEfficiencyDays))
			return
		}
		days = n
	}
	data, err := s.st.CostEfficiency(days)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// Nil slices marshal to `null`, which the UI dereferences as an array.
	if data.Models == nil {
		data.Models = []string{}
	}
	if data.Points == nil {
		data.Points = []domain.CostEfficiencyPoint{}
	}
	if data.Totals == nil {
		data.Totals = []domain.CostEfficiencyPoint{}
	}
	writeJSON(w, 200, data)
}

// statusInfo is the single status wire shape. GET /api/status and every SSE
// `status` frame must serialise the same thing: the tray and the web UI read
// pending_request_count, and a broadcast that omitted the field blanked
// whatever the last REST poll had set.
type statusInfo struct {
	scheduler.StatusInfo
	PendingRequestCount int `json:"pending_request_count"`
}

func (s *Server) statusPayload() statusInfo {
	count, _ := s.st.PendingRequestCount()
	return statusInfo{s.sched.Status(), count}
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.statusPayload())
}

func (s *Server) handleAchievements(w http.ResponseWriter, _ *http.Request) {
	data, err := s.st.Achievements()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, data)
}
