package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Verhum/burnrate/internal/config"
)

// Writable subset of the settings GET /api/config reports. Three keys are
// deliberately read-only over HTTP and must be set via env var or the DB:
// usage_url, because internal/usage sends the Claude OAuth bearer token to it
// and a writable value is a token-exfiltration primitive on next restart; port
// and base_code_dir, because both take effect only at startup, so accepting a
// write would silently disagree with the running daemon.
var validConfigKeys = map[string]bool{
	"parallel_n":                 true,
	"util_threshold":             true,
	"sevenday_threshold":         true,
	"model":                      true,
	"poll_interval_sec":          true,
	"max_attempts":               true,
	"max_auto_continue":          true,
	"worktree_root":              true,
	"dry_run":                    true,
	"notify_on_review":           true,
	"onboarding_completed":       true,
	"human_request_wait_sec":     true,
	"agent_capture_auto_approve": true,
	"capture_approval_wait_sec":  true,
	"capture_retention_days":     true,
}

// Keys GET /api/config reports that PUT will never accept. Named separately from
// "unknown" so the error says which of the two problems it is — the config panel
// renders these, and a typo and a deliberate omission want different fixes.
var readOnlyConfigKeys = map[string]bool{
	"base_code_dir": true,
	"port":          true,
	"usage_url":     true,
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	settings, err := s.st.AllSettings()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	settingInt := func(key string, def int) int {
		if v, ok := settings[key]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return def
	}
	settingFloat := func(key string, def float64) float64 {
		if v, ok := settings[key]; ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
		return def
	}
	settingStr := func(key, def string) string {
		if v, ok := settings[key]; ok {
			return v
		}
		return def
	}
	settingBool := func(key string, def bool) bool {
		if v, ok := settings[key]; ok {
			return v == "true" || v == "1"
		}
		return def
	}

	m := map[string]any{
		"parallel_n":         settingInt("parallel_n", s.cfg.ParallelN),
		"util_threshold":     settingFloat("util_threshold", s.cfg.UtilThreshold),
		"sevenday_threshold": settingFloat("sevenday_threshold", s.cfg.SevenDayThreshold),
		"model":              settingStr("model", s.cfg.Model),
		"poll_interval_sec":  settingInt("poll_interval_sec", s.cfg.PollIntervalSec),
		"max_attempts":       settingInt("max_attempts", s.cfg.MaxAttempts),
		"max_auto_continue":  settingInt("max_auto_continue", s.cfg.MaxAutoContinue),
		"base_code_dir":      settingStr("base_code_dir", s.cfg.BaseCodeDir),
		"worktree_root":      settingStr("worktree_root", s.cfg.WorktreeRoot),
		"port":               settingInt("port", s.cfg.Port),
		"usage_url":          settingStr("usage_url", s.cfg.UsageURL),
		"dry_run":            settingBool("dry_run", s.cfg.DryRun),
		"notify_on_review":   settingStr("notify_on_review", "true"),
		// Drives the first-run tutorial. Unset (fresh install) => false => the
		// UI auto-opens the tour once, then writes true.
		"onboarding_completed":       settingBool("onboarding_completed", false),
		"human_request_wait_sec":     settingInt("human_request_wait_sec", 600),
		"agent_capture_auto_approve": settingBool("agent_capture_auto_approve", false),
		"capture_approval_wait_sec":  settingInt("capture_approval_wait_sec", 120),
		"capture_retention_days":     settingInt("capture_retention_days", 30),
	}
	writeJSON(w, 200, m)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	for k := range body {
		if !validConfigKeys[k] {
			if readOnlyConfigKeys[k] {
				writeError(w, 400, fmt.Sprintf("read-only config key: %s (set it via env var or the settings table, then restart)", k))
				return
			}
			writeError(w, 400, fmt.Sprintf("unknown config key: %s", k))
			return
		}
	}
	for k, v := range body {
		if err := s.st.SetSetting(k, fmt.Sprintf("%v", v)); err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}
	if err := s.reloadConfig(); err != nil {
		writeError(w, 500, fmt.Sprintf("settings saved but reload failed: %v", err))
		return
	}
	writeJSON(w, 200, map[string]string{"status": "updated"})
}

// reloadConfig re-derives the config from defaults -> settings -> env and hands
// it to the scheduler, so a write to /api/config changes what the *next* run is
// launched with. Without this the daemon kept the snapshot config.Load produced
// at startup and a model change appeared to save while nothing used it.
//
// s.cfg is deliberately not replaced: the fields the server itself reads from it
// (DataDir, Port) take effect only at startup and are not writable here, and
// GET /api/config already reads the settings table with s.cfg as mere defaults.
func (s *Server) reloadConfig() error {
	cfg, err := config.Load(s.st)
	if err != nil {
		return err
	}
	s.sched.SetConfig(cfg)
	s.hub.broadcast("status", s.statusPayload())
	return nil
}
