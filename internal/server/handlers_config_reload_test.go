package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// The daemon used to keep the config.Load snapshot it took at startup, so
// changing the model in the UI saved a setting nothing read: every later launch
// still passed the old --model. The write has to reach the scheduler, which is
// what hands a config to the runner.
func TestUpdateConfig_ReachesScheduler(t *testing.T) {
	s, _ := testServer(t)

	before := s.sched.Config()
	body := []byte(`{"model":"claude-sonnet-5","parallel_n":11,"poll_interval_sec":9}`)
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	after := s.sched.Config()
	if before.Model == after.Model {
		t.Fatalf("model unchanged in scheduler config: %q", after.Model)
	}
	if after.Model != "claude-sonnet-5" {
		t.Fatalf("scheduler model = %q, want claude-sonnet-5", after.Model)
	}
	if after.ParallelN != 11 {
		t.Fatalf("scheduler parallel_n = %d, want 11", after.ParallelN)
	}
	if after.PollIntervalSec != 9 {
		t.Fatalf("scheduler poll_interval_sec = %d, want 9", after.PollIntervalSec)
	}
}

// GET reports base_code_dir/port/usage_url, so a panel that PUT back everything
// it was given tripped the unknown-key check and persisted nothing at all —
// including the model the user had actually edited.
func TestUpdateConfig_ReadOnlyKeyIsNamedAsSuch(t *testing.T) {
	s, st := testServer(t)

	body := []byte(`{"model":"claude-sonnet-5","base_code_dir":"/tmp/elsewhere"}`)
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read-only") {
		t.Fatalf("error should say the key is read-only, got %s", rec.Body.String())
	}
	// The rejection is all-or-nothing, which is why the client must send only
	// the keys it changed.
	if v, ok := st.GetSetting("model"); ok {
		t.Fatalf("model should not have been written, got %q", v)
	}
}

// The account fields are owned by SetAccount, whose selection can be newer than
// the settings table a reload reads.
func TestUpdateConfig_PreservesLiveAccount(t *testing.T) {
	s, _ := testServer(t)
	s.sched.SetAccount("/tmp/acct/.claude", "/tmp/acct/kc", "/tmp/acct/pw")

	body := []byte(`{"model":"claude-sonnet-5"}`)
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := s.sched.ActiveConfigDir(); got != "/tmp/acct/.claude" {
		t.Fatalf("account clobbered by reload: %q", got)
	}
}

func TestUpdateConfig_ZeroPollIntervalDoesNotPanic(t *testing.T) {
	s, _ := testServer(t)

	body := []byte(`{"poll_interval_sec":0}`)
	req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// GET still answers, i.e. nothing in the reload path blew up on the value.
	req = httptest.NewRequest("GET", "/api/config", nil)
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	var cfg map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg["poll_interval_sec"] != float64(0) {
		t.Fatalf("poll_interval_sec = %v, want 0", cfg["poll_interval_sec"])
	}
}
