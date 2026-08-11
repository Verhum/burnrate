package store

import (
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

func TestDetectHighestDailySpend_TodayInTop5(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	runs := []domain.Run{
		{StartedAt: today + "T10:00:00Z", CostUSD: 50, TaskID: 1},
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 40, TaskID: 2},
		{StartedAt: "2026-01-02T10:00:00Z", CostUSD: 30, TaskID: 3},
	}

	entries, todayEntry := detectHighestDailySpend(runs, today)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Date != today || entries[0].Rank != 1 {
		t.Fatalf("expected today at rank 1, got date=%s rank=%d", entries[0].Date, entries[0].Rank)
	}
	if !entries[0].IsToday {
		t.Fatal("expected top entry to have IsToday=true")
	}
	if todayEntry == nil {
		t.Fatal("expected todayEntry to be non-nil")
	}
	if todayEntry.Rank != 1 || todayEntry.PeakSpend != 50 {
		t.Fatalf("unexpected todayEntry: rank=%d spend=%f", todayEntry.Rank, todayEntry.PeakSpend)
	}
}

func TestDetectHighestDailySpend_TodayNotInTop5(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	runs := []domain.Run{
		{StartedAt: today + "T10:00:00Z", CostUSD: 1, TaskID: 1},
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 100, TaskID: 2},
		{StartedAt: "2026-01-02T10:00:00Z", CostUSD: 90, TaskID: 3},
		{StartedAt: "2026-01-03T10:00:00Z", CostUSD: 80, TaskID: 4},
		{StartedAt: "2026-01-04T10:00:00Z", CostUSD: 70, TaskID: 5},
		{StartedAt: "2026-01-05T10:00:00Z", CostUSD: 60, TaskID: 6},
	}

	entries, todayEntry := detectHighestDailySpend(runs, today)

	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.IsToday {
			t.Fatal("no top-5 entry should be today")
		}
	}
	if todayEntry == nil {
		t.Fatal("expected todayEntry non-nil")
	}
	if todayEntry.Rank != 6 {
		t.Fatalf("expected today rank 6, got %d", todayEntry.Rank)
	}
	if todayEntry.PeakSpend != 1 {
		t.Fatalf("expected today spend 1, got %f", todayEntry.PeakSpend)
	}
}

func TestDetectHighestDailySpend_NoRunsToday(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	runs := []domain.Run{
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 50, TaskID: 1},
	}

	_, todayEntry := detectHighestDailySpend(runs, today)

	if todayEntry == nil {
		t.Fatal("expected todayEntry non-nil even with no runs today")
	}
	if todayEntry.Rank != 2 {
		t.Fatalf("expected rank 2, got %d", todayEntry.Rank)
	}
	if todayEntry.PeakSpend != 0 {
		t.Fatalf("expected 0 spend, got %f", todayEntry.PeakSpend)
	}
	if !todayEntry.IsToday {
		t.Fatal("expected IsToday=true")
	}
}

func TestDetectMostTasksDaily_TodayNotInTop5(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	runs := []domain.Run{
		{StartedAt: today + "T10:00:00Z", CostUSD: 1, TaskID: 1},
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 1, TaskID: 10},
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 1, TaskID: 11},
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 1, TaskID: 12},
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 1, TaskID: 13},
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 1, TaskID: 14},
		{StartedAt: "2026-01-02T10:00:00Z", CostUSD: 1, TaskID: 20},
		{StartedAt: "2026-01-02T10:00:00Z", CostUSD: 1, TaskID: 21},
		{StartedAt: "2026-01-02T10:00:00Z", CostUSD: 1, TaskID: 22},
		{StartedAt: "2026-01-02T10:00:00Z", CostUSD: 1, TaskID: 23},
		{StartedAt: "2026-01-03T10:00:00Z", CostUSD: 1, TaskID: 30},
		{StartedAt: "2026-01-03T10:00:00Z", CostUSD: 1, TaskID: 31},
		{StartedAt: "2026-01-03T10:00:00Z", CostUSD: 1, TaskID: 32},
		{StartedAt: "2026-01-04T10:00:00Z", CostUSD: 1, TaskID: 40},
		{StartedAt: "2026-01-04T10:00:00Z", CostUSD: 1, TaskID: 41},
		{StartedAt: "2026-01-05T10:00:00Z", CostUSD: 1, TaskID: 50},
		{StartedAt: "2026-01-05T10:00:00Z", CostUSD: 1, TaskID: 51},
	}

	entries, todayEntry := detectMostTasksDaily(runs, today)

	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	if todayEntry == nil {
		t.Fatal("expected todayEntry non-nil")
	}
	if todayEntry.Count != 1 {
		t.Fatalf("expected today count 1, got %d", todayEntry.Count)
	}
	if todayEntry.Rank != 6 {
		t.Fatalf("expected today rank 6, got %d", todayEntry.Rank)
	}
}

func TestDetectMostTasksDaily_NoRunsToday(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	runs := []domain.Run{
		{StartedAt: "2026-01-01T10:00:00Z", CostUSD: 1, TaskID: 1},
	}

	_, todayEntry := detectMostTasksDaily(runs, today)

	if todayEntry == nil {
		t.Fatal("expected todayEntry non-nil")
	}
	if todayEntry.Count != 0 {
		t.Fatalf("expected 0 tasks, got %d", todayEntry.Count)
	}
	if todayEntry.Rank != 2 {
		t.Fatalf("expected rank 2, got %d", todayEntry.Rank)
	}
}

func TestDetectMaxBurnRates_TodayEntry(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	runs := []domain.Run{
		{StartedAt: today + "T10:00:00Z", EndedAt: today + "T11:00:00Z", CostUSD: 5, TaskID: 1},
		{StartedAt: "2026-01-01T10:00:00Z", EndedAt: "2026-01-01T11:00:00Z", CostUSD: 50, TaskID: 2},
		{StartedAt: "2026-01-02T10:00:00Z", EndedAt: "2026-01-02T11:00:00Z", CostUSD: 40, TaskID: 3},
		{StartedAt: "2026-01-03T10:00:00Z", EndedAt: "2026-01-03T11:00:00Z", CostUSD: 30, TaskID: 4},
		{StartedAt: "2026-01-04T10:00:00Z", EndedAt: "2026-01-04T11:00:00Z", CostUSD: 20, TaskID: 5},
		{StartedAt: "2026-01-05T10:00:00Z", EndedAt: "2026-01-05T11:00:00Z", CostUSD: 10, TaskID: 6},
	}

	entries, todayEntry := detectMaxBurnRates(runs, today)

	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	if todayEntry == nil {
		t.Fatal("expected todayEntry non-nil")
	}
	if todayEntry.Rank != 6 {
		t.Fatalf("expected today rank 6, got %d", todayEntry.Rank)
	}
	if todayEntry.RatePerH != 5 {
		t.Fatalf("expected today rate 5/h, got %f", todayEntry.RatePerH)
	}
}

func TestDetectMaxBurnRates_NoRunsToday(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	runs := []domain.Run{
		{StartedAt: "2026-01-01T10:00:00Z", EndedAt: "2026-01-01T11:00:00Z", CostUSD: 50, TaskID: 1},
	}

	_, todayEntry := detectMaxBurnRates(runs, today)

	if todayEntry != nil {
		t.Fatalf("expected nil todayEntry when no runs today, got rank=%d", todayEntry.Rank)
	}
}

func TestDetectFastestBurns_TodaySession(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	todayT, _ := time.Parse("2006-01-02", today)

	points := []usagePoint{
		{capturedAt: todayT.Add(1 * time.Hour), fiveHourUtil: 5, fiveHourResets: "r1"},
		{capturedAt: todayT.Add(2 * time.Hour), fiveHourUtil: 50, fiveHourResets: "r1"},
		{capturedAt: todayT.Add(3 * time.Hour), fiveHourUtil: 96, fiveHourResets: "r1"},
	}

	entries, todayEntry := detectFastestBurns(points, today)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].IsToday {
		t.Fatal("expected entry IsToday=true")
	}
	if todayEntry == nil {
		t.Fatal("expected todayEntry non-nil")
	}
	if todayEntry.Rank != 1 {
		t.Fatalf("expected today rank 1, got %d", todayEntry.Rank)
	}
}

func TestDetectFastestBurns_NoSessionToday(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")

	points := []usagePoint{
		{capturedAt: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), fiveHourUtil: 5, fiveHourResets: "r1"},
		{capturedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), fiveHourUtil: 96, fiveHourResets: "r1"},
	}

	_, todayEntry := detectFastestBurns(points, today)

	if todayEntry != nil {
		t.Fatal("expected nil todayEntry when no session today")
	}
}
