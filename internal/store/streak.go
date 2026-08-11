package store

import (
	"sort"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

// streakRunLimit matches the leaderboard's cap; the table is one row per run
// attempt, so this is years of history rather than a real truncation.
const streakRunLimit = 10000

// Streak reports activity streaks and lifetime totals over all recorded runs.
func (s *Store) Streak() (*domain.StreakData, error) {
	runs, err := s.ListRuns(0, streakRunLimit)
	if err != nil {
		return nil, err
	}
	return computeStreak(runs, time.Now().UTC().Format("2006-01-02")), nil
}

// computeStreak buckets runs into UTC days and walks the consecutive-day
// chains. When two chains tie for longest, the more recent one wins — it is
// the one the user can still relate to.
func computeStreak(runs []domain.Run, today string) *domain.StreakData {
	data := &domain.StreakData{}
	tasks := map[int64]bool{}
	daySet := map[string]bool{}
	for _, r := range runs {
		if len(r.StartedAt) < 10 {
			continue
		}
		daySet[r.StartedAt[:10]] = true
		data.TotalRuns++
		data.TotalCostUSD += r.CostUSD
		data.LinesAdded += r.LinesAdded
		data.LinesRemoved += r.LinesRemoved
		tasks[r.TaskID] = true
	}
	data.TotalTasks = len(tasks)
	data.ActiveDays = len(daySet)
	if len(daySet) == 0 {
		return data
	}

	days := make([]string, 0, len(daySet))
	for d := range daySet {
		days = append(days, d)
	}
	sort.Strings(days)
	data.FirstDay = days[0]

	chainStart, chainLen, prev := days[0], 1, days[0]
	endChain := func() {
		if chainLen >= data.LongestStreak {
			data.LongestStreak = chainLen
			data.LongestStart = chainStart
			data.LongestEnd = prev
		}
		if prev == today || nextDay(prev) == today {
			data.CurrentStreak = chainLen
		}
	}
	for _, d := range days[1:] {
		if d == nextDay(prev) {
			chainLen++
		} else {
			endChain()
			chainStart, chainLen = d, 1
		}
		prev = d
	}
	endChain()
	return data
}

func nextDay(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}
