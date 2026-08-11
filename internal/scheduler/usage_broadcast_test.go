package scheduler

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func captureUsageBroadcast(t *testing.T, sched *Scheduler) map[string]json.RawMessage {
	t.Helper()
	var payload any
	var seen bool
	sched.OnBroadcast = func(event string, p any) {
		if event == "usage" {
			payload, seen = p, true
		}
	}
	sched.tick(context.Background(), false)
	if !seen {
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal broadcast: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal broadcast: %v", err)
	}
	return m
}

// The SSE "usage" event and GET /api/usage feed the same Zustand slot, and the
// store replaces it wholesale — so a broadcast carrying different JSON keys
// blanks every field the fetch had populated. usage.Snapshot has no JSON tags,
// so broadcasting it published {"SevenDay":{...}} and the UI's
// seven_day_resets_at went undefined a tick after every page load.
func TestUsageBroadcastUsesEndpointJSONShape(t *testing.T) {
	st := testStore(t)
	sched := New(st, testConfig(), usage.NewClient("http://unused"), log.New("", false))

	weeklyReset := time.Now().Add(52 * time.Hour).UTC().Truncate(time.Second)
	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 8, ResetsAt: time.Now().Add(75 * time.Minute)},
		SevenDay: usage.Window{Utilization: 41, ResetsAt: weeklyReset},
		Raw:      json.RawMessage(`{}`),
	})
	// Backoff keeps tick off the network while still exercising the
	// broadcast-the-last-reading path.
	sched.mu.Lock()
	sched.rateLimitBackoffUntil = time.Now().Add(time.Minute)
	sched.mu.Unlock()

	got := captureUsageBroadcast(t, sched)
	if got == nil {
		t.Fatal("no usage broadcast")
	}

	want := jsonKeys(t, store.UsageSnapshot{})
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("broadcast is missing %q, which GET /api/usage sends", k)
		}
	}
	for k := range got {
		if k == "scoped_weekly" || k == "seven_day_opus_util" {
			continue // omitempty on the zero value used for `want`
		}
		if !contains(want, k) {
			t.Errorf("broadcast has extra key %q; GET /api/usage sends %v", k, want)
		}
	}

	var resetsAt string
	if err := json.Unmarshal(got["seven_day_resets_at"], &resetsAt); err != nil {
		t.Fatalf("seven_day_resets_at: %v", err)
	}
	if resetsAt != weeklyReset.Format(time.RFC3339) {
		t.Errorf("seven_day_resets_at = %q, want %q", resetsAt, weeklyReset.Format(time.RFC3339))
	}
}

// The idle-throttle shortcut re-broadcasts the last reading. With no reading yet
// it used to broadcast the zero Snapshot, which reads on the wire as 0% used and
// no reset times — indistinguishable from a real all-clear.
func TestUsageBroadcastSkippedWithoutReading(t *testing.T) {
	st := testStore(t)
	sched := New(st, testConfig(), usage.NewClient("http://unused"), log.New("", false))

	sched.mu.Lock()
	sched.lastIdleFetchAt = time.Now()
	sched.mu.Unlock()

	if got := captureUsageBroadcast(t, sched); got != nil {
		t.Errorf("broadcast a usage event with no reading: %v", got)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
