package main

import (
	"testing"
	"time"
)

func TestFormatStartedAt(t *testing.T) {
	// A fixed local "now" so the today/earlier-day split is deterministic.
	loc := time.FixedZone("TEST", -7*3600)
	now := time.Date(2026, 7, 25, 14, 30, 0, 0, loc)

	tests := []struct {
		name string
		iso  string
		want string
	}{
		{
			// Stored UTC converts to the caller's zone before it is read.
			name: "today drops the date",
			iso:  "2026-07-25T20:15:00Z",
			want: "13:15 (1h15m ago)",
		},
		{
			name: "earlier day keeps the date",
			iso:  "2026-07-24T20:15:00Z",
			want: "Jul 24 13:15 (25h15m ago)",
		},
		{
			// Beyond two days the hour count stops being readable.
			name: "last week's run reads in days",
			iso:  "2026-07-16T20:15:00Z",
			want: "Jul 16 13:15 (9d 1h ago)",
		},
		{
			name: "sub-minute reads as just now",
			iso:  "2026-07-25T21:29:30Z",
			want: "14:29 (just now)",
		},
		{
			// A run row written but not yet started, or a clock skew.
			name: "future start shows no elapsed",
			iso:  "2026-07-25T22:00:00Z",
			want: "15:00",
		},
		{
			name: "missing start is not a blank column",
			iso:  "",
			want: "unknown",
		},
		{
			name: "unparseable start falls back to the raw value",
			iso:  "not-a-timestamp",
			want: "not-a-timestamp",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatStartedAt(tc.iso, now); got != tc.want {
				t.Errorf("formatStartedAt(%q) = %q, want %q", tc.iso, got, tc.want)
			}
		})
	}
}
