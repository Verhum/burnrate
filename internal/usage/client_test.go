package usage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClientFetch(t *testing.T) {
	body := `{
		"five_hour": {"utilization": 42.5, "resets_at": "2025-01-01T05:00:00Z"},
		"seven_day": {"utilization": 20.0, "resets_at": "2025-01-07T00:00:00Z"},
		"seven_day_opus": {"utilization": 15.0, "resets_at": "2025-01-07T00:00:00Z"}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("bad auth: %s", auth)
		}
		if beta := r.Header.Get("anthropic-beta"); beta != "oauth-2025-04-20" {
			t.Errorf("bad beta header: %s", beta)
		}
		ua := r.Header.Get("User-Agent")
		if !strings.HasPrefix(ua, "claude-code/") {
			t.Errorf("User-Agent must start with claude-code/, got %q", ua)
		}
		if !strings.Contains(ua, burnrateUserAgent) {
			t.Errorf("User-Agent must disclose burnrate as a third-party caller, got %q", ua)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("bad Content-Type: %s", ct)
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	c := NewClient(srv.URL)
	snap, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if snap.FiveHour.Utilization != 42.5 {
		t.Errorf("5h util: %f", snap.FiveHour.Utilization)
	}
	if snap.SevenDay.Utilization != 20.0 {
		t.Errorf("7d util: %f", snap.SevenDay.Utilization)
	}
	if snap.SevenDayOpus == nil || snap.SevenDayOpus.Utilization != 15.0 {
		t.Errorf("7d opus: %v", snap.SevenDayOpus)
	}
	if snap.Raw == nil {
		t.Error("raw is nil")
	}
	if len(snap.Limits) != 0 {
		t.Errorf("expected no limits in legacy response, got %d", len(snap.Limits))
	}
}

func TestClientFetchWithLimits(t *testing.T) {
	body := `{
		"five_hour": {"utilization": 42.5, "resets_at": "2025-01-01T05:00:00Z"},
		"seven_day": {"utilization": 20.0, "resets_at": "2025-01-07T00:00:00Z"},
		"limits": [
			{"kind":"session","group":"session","percent":5,"severity":"normal","resets_at":"2025-07-19T22:50:05.036000+00:00","is_active":false},
			{"kind":"weekly_all","group":"weekly","percent":22,"severity":"normal","resets_at":"2025-07-21T04:00:00+00:00","is_active":false},
			{"kind":"weekly_scoped","group":"weekly","percent":17,"severity":"normal","resets_at":"2025-07-21T04:00:00+00:00","is_active":false,"scope":{"model":{"display_name":"Fable"}}}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	c := NewClient(srv.URL)
	snap, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if snap.FiveHour.Utilization != 5 {
		t.Errorf("5h util from limits: got %f, want 5", snap.FiveHour.Utilization)
	}
	if snap.FiveHour.ResetsAt.IsZero() {
		t.Error("5h resets_at should be parsed from limits")
	}
	if snap.SevenDay.Utilization != 22 {
		t.Errorf("7d util from limits: got %f, want 22", snap.SevenDay.Utilization)
	}
	if len(snap.Limits) != 3 {
		t.Fatalf("expected 3 limits, got %d", len(snap.Limits))
	}
	if len(snap.ScopedWeekly) != 1 {
		t.Fatalf("expected 1 scoped weekly, got %d", len(snap.ScopedWeekly))
	}
	if snap.ScopedWeekly[0].Model != "Fable" {
		t.Errorf("scoped model = %q, want Fable", snap.ScopedWeekly[0].Model)
	}
	if snap.ScopedWeekly[0].Percent != 17 {
		t.Errorf("scoped percent = %f, want 17", snap.ScopedWeekly[0].Percent)
	}
}

func TestClient401Retry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2025-01-01T05:00:00Z"},"seven_day":{"utilization":5,"resets_at":"2025-01-07T00:00:00Z"}}`))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	c := NewClient(srv.URL)
	snap, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch after retry: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if snap.FiveHour.Utilization != 10 {
		t.Errorf("util: %f", snap.FiveHour.Utilization)
	}
}

func TestClient429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	c := NewClient(srv.URL)
	_, err := c.Fetch(context.Background())
	if !errors.Is(err, ErrRateLimited429) {
		t.Fatalf("expected ErrRateLimited429, got %v", err)
	}
	if ra := RetryAfterFrom(err); ra != 0 {
		t.Fatalf("expected zero RetryAfter without header, got %s", ra)
	}
}

func TestClient429RetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	c := NewClient(srv.URL)
	_, err := c.Fetch(context.Background())
	if !errors.Is(err, ErrRateLimited429) {
		t.Fatalf("expected ErrRateLimited429, got %v", err)
	}
	if ra := RetryAfterFrom(err); ra != 60*time.Second {
		t.Fatalf("expected 60s RetryAfter, got %s", ra)
	}
}

func TestParseRetryAfterInvalid(t *testing.T) {
	if ra := RetryAfterFrom(errors.New("some other error")); ra != 0 {
		t.Fatalf("expected zero for non-rate-limit error, got %s", ra)
	}
}

func TestMissingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"five_hour":{"utilization":50}}`))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	c := NewClient(srv.URL)
	snap, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if snap.FiveHour.Utilization != 50 {
		t.Errorf("util: %f", snap.FiveHour.Utilization)
	}
	if snap.SevenDayOpus != nil {
		t.Errorf("expected nil opus, got %v", snap.SevenDayOpus)
	}
}

func TestParseAPIResponseLimitsPreferred(t *testing.T) {
	data := []byte(`{
		"five_hour": {"utilization": 99.9, "resets_at": "2025-01-01T05:00:00Z"},
		"seven_day": {"utilization": 88.8, "resets_at": "2025-01-07T00:00:00Z"},
		"limits": [
			{"kind":"session","group":"session","percent":3.5,"severity":"normal","resets_at":"2025-07-19T22:50:05.036000+00:00","is_active":false},
			{"kind":"weekly_all","group":"weekly","percent":18.2,"severity":"normal","resets_at":"2025-07-21T04:00:00+00:00","is_active":false}
		]
	}`)

	snap, err := parseAPIResponse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.FiveHour.Utilization != 3.5 {
		t.Errorf("limits should override five_hour: got %f, want 3.5", snap.FiveHour.Utilization)
	}
	if snap.SevenDay.Utilization != 18.2 {
		t.Errorf("limits should override seven_day: got %f, want 18.2", snap.SevenDay.Utilization)
	}
}

func TestParseAPIResponseNoLimitsFallback(t *testing.T) {
	data := []byte(`{
		"five_hour": {"utilization": 42.5, "resets_at": "2025-01-01T05:00:00Z"},
		"seven_day": {"utilization": 20.0, "resets_at": "2025-01-07T00:00:00Z"}
	}`)

	snap, err := parseAPIResponse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.FiveHour.Utilization != 42.5 {
		t.Errorf("legacy fallback: got %f, want 42.5", snap.FiveHour.Utilization)
	}
	if snap.SevenDay.Utilization != 20.0 {
		t.Errorf("legacy fallback: got %f, want 20.0", snap.SevenDay.Utilization)
	}
}
