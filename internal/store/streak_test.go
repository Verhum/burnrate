package store

import (
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

// streakDay returns the UTC day key offset days before today.
func streakDay(offset int) string {
	return time.Now().UTC().AddDate(0, 0, -offset).Format("2006-01-02")
}

func TestComputeStreak_Empty(t *testing.T) {
	data := computeStreak(nil, streakDay(0))

	if data.CurrentStreak != 0 || data.LongestStreak != 0 || data.ActiveDays != 0 {
		t.Fatalf("expected zeroes, got %+v", data)
	}
	if data.FirstDay != "" || data.LongestStart != "" {
		t.Fatalf("expected empty dates, got %+v", data)
	}
}

func TestComputeStreak_SingleDayToday(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 5, LinesAdded: 10, LinesRemoved: 2},
	}

	data := computeStreak(runs, streakDay(0))

	if data.CurrentStreak != 1 || data.LongestStreak != 1 {
		t.Fatalf("expected streaks of 1, got current=%d longest=%d", data.CurrentStreak, data.LongestStreak)
	}
	if data.ActiveDays != 1 || data.FirstDay != streakDay(0) {
		t.Fatalf("unexpected days: %+v", data)
	}
	if data.TotalRuns != 1 || data.TotalTasks != 1 || data.TotalCostUSD != 5 {
		t.Fatalf("unexpected totals: %+v", data)
	}
	if data.LinesAdded != 10 || data.LinesRemoved != 2 {
		t.Fatalf("unexpected lines: %+v", data)
	}
}

func TestComputeStreak_EndsYesterdayStillCurrent(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(3) + "T10:00:00Z", TaskID: 1},
		{StartedAt: streakDay(2) + "T10:00:00Z", TaskID: 2},
		{StartedAt: streakDay(1) + "T10:00:00Z", TaskID: 3},
	}

	data := computeStreak(runs, streakDay(0))

	if data.CurrentStreak != 3 {
		t.Fatalf("streak ending yesterday should still be current, got %d", data.CurrentStreak)
	}
}

func TestComputeStreak_EndedTwoDaysAgoIsBroken(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(4) + "T10:00:00Z", TaskID: 1},
		{StartedAt: streakDay(3) + "T10:00:00Z", TaskID: 2},
		{StartedAt: streakDay(2) + "T10:00:00Z", TaskID: 3},
	}

	data := computeStreak(runs, streakDay(0))

	if data.CurrentStreak != 0 {
		t.Fatalf("streak ending two days ago should be broken, got %d", data.CurrentStreak)
	}
	if data.LongestStreak != 3 {
		t.Fatalf("longest streak should survive the break, got %d", data.LongestStreak)
	}
	if data.LongestStart != streakDay(4) || data.LongestEnd != streakDay(2) {
		t.Fatalf("unexpected longest range: %s..%s", data.LongestStart, data.LongestEnd)
	}
}

func TestComputeStreak_LongestVsCurrent(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(10) + "T10:00:00Z", TaskID: 1},
		{StartedAt: streakDay(9) + "T10:00:00Z", TaskID: 2},
		{StartedAt: streakDay(8) + "T10:00:00Z", TaskID: 3},
		{StartedAt: streakDay(7) + "T10:00:00Z", TaskID: 4},
		{StartedAt: streakDay(1) + "T10:00:00Z", TaskID: 5},
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 6},
	}

	data := computeStreak(runs, streakDay(0))

	if data.CurrentStreak != 2 {
		t.Fatalf("expected current streak 2, got %d", data.CurrentStreak)
	}
	if data.LongestStreak != 4 {
		t.Fatalf("expected longest streak 4, got %d", data.LongestStreak)
	}
	if data.ActiveDays != 6 || data.FirstDay != streakDay(10) {
		t.Fatalf("unexpected days: %+v", data)
	}
}

func TestComputeStreak_TieGoesToMostRecent(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(9) + "T10:00:00Z", TaskID: 1},
		{StartedAt: streakDay(8) + "T10:00:00Z", TaskID: 2},
		{StartedAt: streakDay(3) + "T10:00:00Z", TaskID: 3},
		{StartedAt: streakDay(2) + "T10:00:00Z", TaskID: 4},
	}

	data := computeStreak(runs, streakDay(0))

	if data.LongestStreak != 2 {
		t.Fatalf("expected longest streak 2, got %d", data.LongestStreak)
	}
	if data.LongestStart != streakDay(3) || data.LongestEnd != streakDay(2) {
		t.Fatalf("tie should go to the most recent chain, got %s..%s", data.LongestStart, data.LongestEnd)
	}
}

func TestComputeStreak_MultipleRunsSameDay(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T08:00:00Z", TaskID: 1, CostUSD: 3},
		{StartedAt: streakDay(0) + "T12:00:00Z", TaskID: 1, CostUSD: 4},
		{StartedAt: streakDay(0) + "T16:00:00Z", TaskID: 2, CostUSD: 1},
	}

	data := computeStreak(runs, streakDay(0))

	if data.CurrentStreak != 1 || data.ActiveDays != 1 {
		t.Fatalf("same-day runs should count one day, got %+v", data)
	}
	if data.TotalRuns != 3 || data.TotalTasks != 2 || data.TotalCostUSD != 8 {
		t.Fatalf("unexpected totals: %+v", data)
	}
}

func TestComputeStreak_IgnoresRunsWithoutStart(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: "", TaskID: 1, CostUSD: 100},
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 2, CostUSD: 1},
	}

	data := computeStreak(runs, streakDay(0))

	if data.TotalRuns != 1 || data.TotalCostUSD != 1 {
		t.Fatalf("run without start should be ignored, got %+v", data)
	}
	if data.ActiveDays != 1 {
		t.Fatalf("expected 1 active day, got %d", data.ActiveDays)
	}
}

func TestComputeStreak_UnsortedInput(t *testing.T) {
	// ListRuns returns newest-first; the walk must not depend on order.
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 3},
		{StartedAt: streakDay(2) + "T10:00:00Z", TaskID: 1},
		{StartedAt: streakDay(1) + "T10:00:00Z", TaskID: 2},
	}

	data := computeStreak(runs, streakDay(0))

	if data.CurrentStreak != 3 || data.LongestStreak != 3 {
		t.Fatalf("expected streaks of 3, got current=%d longest=%d", data.CurrentStreak, data.LongestStreak)
	}
}

func TestStoreStreak_ReadsRuns(t *testing.T) {
	st := testStore(t)
	taskID := mustTask(t, st, "streak task")
	seedRun(t, st, taskID, 0, "claude-opus-5", 2.5, 10, 3)

	data, err := st.Streak()
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if data.CurrentStreak != 1 || data.TotalRuns != 1 || data.TotalTasks != 1 {
		t.Fatalf("unexpected streak data: %+v", data)
	}
	if data.TotalCostUSD != 2.5 || data.LinesAdded != 10 || data.LinesRemoved != 3 {
		t.Fatalf("unexpected totals: %+v", data)
	}
}
