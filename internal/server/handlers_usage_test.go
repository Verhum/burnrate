package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Verhum/burnrate/internal/store"
)

func TestGetUsage_NoSnapshot(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/usage", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("expected empty object, got %v", body)
	}
}

func TestGetUsage_WithSnapshot(t *testing.T) {
	s, st := testServer(t)

	err := st.InsertUsageSnapshot(store.UsageSnapshot{
		CapturedAt:       "2025-07-20T12:00:00Z",
		FiveHourUtil:     42.5,
		FiveHourResetsAt: "2025-07-20T17:00:00Z",
		SevenDayUtil:     25.0,
		SevenDayResetsAt: "2025-07-27T00:00:00Z",
		RawJSON:          `{"test":true}`,
	})
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/usage", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["five_hour_util"].(float64) != 42.5 {
		t.Fatalf("expected five_hour_util=42.5, got %v", body["five_hour_util"])
	}
	if body["seven_day_util"].(float64) != 25.0 {
		t.Fatalf("expected seven_day_util=25.0, got %v", body["seven_day_util"])
	}
	if body["captured_at"].(string) != "2025-07-20T12:00:00Z" {
		t.Fatalf("expected captured_at=2025-07-20T12:00:00Z, got %v", body["captured_at"])
	}
}

func TestGetStatus(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, ok := body["window_state"]; !ok {
		t.Fatal("expected window_state field in status response")
	}
	if _, ok := body["running_count"]; !ok {
		t.Fatal("expected running_count field in status response")
	}

	runningCount := body["running_count"].(float64)
	if runningCount != 0 {
		t.Fatalf("expected running_count=0 with DryRun scheduler, got %v", runningCount)
	}
}

func TestUsageHistory_EmptyIsArrayNotNull(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/usage/history?hours=5", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The UI treats this as an array; `null` crashes the usage tab.
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("expected [] for an empty history, got %s", got)
	}
}

// TestStatusForecastJSONContract pins the wire shape the web depends on.
//
// web/src/lib/api/types.ts declares ForecastEntry with action/reason_code/reason
// and no eta, and the forecast chip switches on reason_code. Nothing in the Go
// build checks that, so a rename here would surface as chips silently falling
// back to neutral styling rather than as a failure.
func TestStatusForecastJSONContract(t *testing.T) {
	s, st := testServer(t)
	st.CreateTask("queued-task", "p", "", "medium", "", "")

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		WindowState string `json:"window_state"`
		Forecast    []struct {
			TaskID     int64  `json:"task_id"`
			Action     string `json:"action"`
			ReasonCode string `json:"reason_code"`
			Reason     string `json:"reason"`
		} `json:"forecast"`
	}
	raw := rec.Body.Bytes()
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode status: %v", err)
	}

	// No usage reading has been fetched, so the one candidate must be waiting on
	// exactly that — and must say so.
	if len(payload.Forecast) != 1 {
		t.Fatalf("forecast has %d entries, want 1", len(payload.Forecast))
	}
	e := payload.Forecast[0]
	if e.Action != "wait" {
		t.Errorf("action = %q, want wait", e.Action)
	}
	if e.ReasonCode != "no_usage_data" {
		t.Errorf("reason_code = %q, want no_usage_data", e.ReasonCode)
	}
	if e.Reason == "" {
		t.Error("reason text is empty; the chip would render blank")
	}

	// The ETA field is gone on purpose: with no duration model, any predicted
	// start time was invented. Its return would mean the guesswork came back.
	if bytes.Contains(raw, []byte(`"eta"`)) {
		t.Error(`status payload still carries an "eta" field`)
	}

	if payload.WindowState != "IDLE" {
		t.Errorf("window_state = %q, want IDLE before the first usage fetch", payload.WindowState)
	}
}

func TestCostEfficiency_EmptyReturnsArraysNotNull(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/usage/cost-efficiency", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The UI iterates these, so `null` would be a runtime error, not an empty chart.
	body := rec.Body.String()
	for _, want := range []string{`"models":[]`, `"points":[]`, `"totals":[]`, `"days":30`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in %s", want, body)
		}
	}
}

func TestCostEfficiency_GroupsByModel(t *testing.T) {
	s, st := testServer(t)

	task, _ := st.CreateTask("t", "prompt", "", "medium", "", "")
	run, err := st.CreateRun(task.ID, "/wt", "b", "/repo", "w1", 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	st.SetRunModel(run.ID, "claude-opus-4-6")
	st.SetRunLines(run.ID, 80, 20)
	st.FinishRun(run.ID, "succeeded", 5, 3, "", "", "")

	req := httptest.NewRequest("GET", "/api/usage/cost-efficiency?days=7", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Days   int      `json:"days"`
		Models []string `json:"models"`
		Points []struct {
			Model       string  `json:"model"`
			Tasks       int     `json:"tasks"`
			CostPerTask float64 `json:"cost_per_task"`
			CostPerLine float64 `json:"cost_per_line"`
		} `json:"points"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Days != 7 {
		t.Fatalf("expected days=7, got %d", body.Days)
	}
	if len(body.Models) != 1 || body.Models[0] != "claude-opus-4-6" {
		t.Fatalf("unexpected models %v", body.Models)
	}
	if len(body.Points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(body.Points))
	}
	p := body.Points[0]
	if p.Tasks != 1 || p.CostPerTask != 5 || p.CostPerLine != 0.05 {
		t.Fatalf("unexpected point %+v", p)
	}
}

func TestCostEfficiency_RejectsBadDays(t *testing.T) {
	s, _ := testServer(t)

	for _, q := range []string{"days=0", "days=-1", "days=abc", "days=9999"} {
		req := httptest.NewRequest("GET", "/api/usage/cost-efficiency?"+q, nil)
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("%s: expected 400, got %d: %s", q, rec.Code, rec.Body.String())
		}
	}
}
