package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetConfig_ReturnsDefaults(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var cfg map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	expectedKeys := []string{
		"parallel_n",
		"util_threshold",
		"sevenday_threshold",
		"model",
		"poll_interval_sec",
		"max_attempts",
		"max_auto_continue",
		"base_code_dir",
		"worktree_root",
		"port",
		"usage_url",
		"dry_run",
		"notify_on_review",
	}
	for _, key := range expectedKeys {
		if _, ok := cfg[key]; !ok {
			t.Errorf("expected key %q in config response, but it was missing", key)
		}
	}

	// testServer sets DryRun = true
	if cfg["dry_run"] != true {
		t.Fatalf("expected dry_run=true, got %v", cfg["dry_run"])
	}
}

func TestUpdateConfig_ValidKey(t *testing.T) {
	s, _ := testServer(t)

	// PUT a valid key
	body, _ := json.Marshal(map[string]string{"model": "opus"})
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "updated" {
		t.Fatalf("expected status=updated, got %s", resp["status"])
	}

	// GET config should reflect the updated value from the DB
	req = httptest.NewRequest("GET", "/api/config", nil)
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var cfg map[string]any
	json.NewDecoder(rec.Body).Decode(&cfg)
	if cfg["model"] != "opus" {
		t.Fatalf("expected model=opus after update, got %v", cfg["model"])
	}
}

func TestUpdateConfig_UnknownKey(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{"unknown_key": "value"})
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateConfig_InvalidJSON(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateConfig_MultipleKeys(t *testing.T) {
	s, _ := testServer(t)

	body, _ := json.Marshal(map[string]string{"max_attempts": "10", "poll_interval_sec": "20"})
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "updated" {
		t.Fatalf("expected status=updated, got %s", resp["status"])
	}
}

func TestUpdateConfig_MixedTypes(t *testing.T) {
	s, _ := testServer(t)

	body := []byte(`{"model":"claude-opus-4-6","parallel_n":6,"dry_run":false,"max_auto_continue":3}`)
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "updated" {
		t.Fatalf("expected status=updated, got %s", resp["status"])
	}
}

func TestConfig_OnboardingFlagRoundTrip(t *testing.T) {
	s, _ := testServer(t)

	// Fresh install: the flag is absent from settings, so the UI sees false
	// and runs the tutorial once.
	req := httptest.NewRequest("GET", "/api/config", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	var cfg map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cfg["onboarding_completed"] != false {
		t.Fatalf("expected onboarding_completed=false by default, got %v", cfg["onboarding_completed"])
	}

	// Finishing (or skipping) the tutorial persists the flag.
	body := []byte(`{"onboarding_completed":true}`)
	req = httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/config", nil)
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	cfg = map[string]any{}
	json.NewDecoder(rec.Body).Decode(&cfg)
	if cfg["onboarding_completed"] != true {
		t.Fatalf("expected onboarding_completed=true after update, got %v", cfg["onboarding_completed"])
	}
}
