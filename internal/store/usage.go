package store

import (
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

type ScopedWeekly = domain.ScopedWeekly

type UsageSnapshot = domain.UsageSnapshot

func (s *Store) InsertUsageSnapshot(snap UsageSnapshot) error {
	_, err := s.db.Exec(`
		INSERT INTO usage_snapshots (captured_at, five_hour_util, five_hour_resets_at,
		    seven_day_util, seven_day_resets_at, seven_day_opus_util, raw_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		snap.CapturedAt, snap.FiveHourUtil, snap.FiveHourResetsAt,
		snap.SevenDayUtil, snap.SevenDayResetsAt, snap.SevenDayOpusUtil, snap.RawJSON,
	)
	return err
}

func (s *Store) LatestUsageSnapshot() (*UsageSnapshot, error) {
	snap := &UsageSnapshot{}
	var opusUtil sql.NullFloat64
	err := s.db.QueryRow(`
		SELECT captured_at, five_hour_util, five_hour_resets_at,
		       seven_day_util, seven_day_resets_at, seven_day_opus_util, raw_json
		FROM usage_snapshots ORDER BY captured_at DESC LIMIT 1`,
	).Scan(&snap.CapturedAt, &snap.FiveHourUtil, &snap.FiveHourResetsAt,
		&snap.SevenDayUtil, &snap.SevenDayResetsAt, &opusUtil, &snap.RawJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if opusUtil.Valid {
		v := opusUtil.Float64
		snap.SevenDayOpusUtil = &v
	}

	snap.ScopedWeekly = extractScopedWeekly(snap.RawJSON)

	return snap, nil
}

func (s *Store) UsageSnapshotsSince(since time.Time) ([]UsageSnapshot, error) {
	rows, err := s.db.Query(`
		SELECT captured_at, five_hour_util, five_hour_resets_at,
		       seven_day_util, seven_day_resets_at, seven_day_opus_util, raw_json
		FROM usage_snapshots
		WHERE captured_at >= ?
		ORDER BY captured_at ASC`,
		since.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []UsageSnapshot
	for rows.Next() {
		var snap UsageSnapshot
		var opusUtil sql.NullFloat64
		if err := rows.Scan(&snap.CapturedAt, &snap.FiveHourUtil, &snap.FiveHourResetsAt,
			&snap.SevenDayUtil, &snap.SevenDayResetsAt, &opusUtil, &snap.RawJSON); err != nil {
			return nil, err
		}
		if opusUtil.Valid {
			v := opusUtil.Float64
			snap.SevenDayOpusUtil = &v
		}
		snaps = append(snaps, snap)
	}
	return snaps, rows.Err()
}

func (s *Store) TrimUsageSnapshots(olderThan time.Time) error {
	_, err := s.db.Exec("DELETE FROM usage_snapshots WHERE captured_at < ?",
		olderThan.UTC().Format(time.RFC3339))
	return err
}

type usagePoint struct {
	capturedAt     time.Time
	fiveHourUtil   float64
	fiveHourResets string
}

func (s *Store) Leaderboard() (*domain.LeaderboardData, error) {
	rows, err := s.db.Query(`
		SELECT captured_at, five_hour_util, five_hour_resets_at
		FROM usage_snapshots
		ORDER BY captured_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []usagePoint
	for rows.Next() {
		var p usagePoint
		var ca string
		if err := rows.Scan(&ca, &p.fiveHourUtil, &p.fiveHourResets); err != nil {
			return nil, err
		}
		p.capturedAt, _ = time.Parse(time.RFC3339, ca)
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	runs, err := s.ListRuns(0, 10000)
	if err != nil {
		return nil, err
	}

	today := time.Now().UTC().Format("2006-01-02")

	burns, todayBurn := detectFastestBurns(points, today)
	daily, todayDaily := detectHighestDailySpend(runs, today)
	rates, todayRate := detectMaxBurnRates(runs, today)
	tasks, todayTasks := detectMostTasksDaily(runs, today)

	return &domain.LeaderboardData{
		FastestBurns:      burns,
		HighestDailySpend: daily,
		MaxBurnRates:      rates,
		MostTasksDaily:    tasks,
		TodayFastestBurn:  todayBurn,
		TodayDailySpend:   todayDaily,
		TodayMaxBurnRate:  todayRate,
		TodayMostTasks:    todayTasks,
	}, nil
}

func detectFastestBurns(points []usagePoint, today string) ([]domain.FastestBurnEntry, *domain.FastestBurnEntry) {
	type burnSession struct {
		startedAt time.Time
		reachedAt time.Time
		startUtil float64
		peakUtil  float64
	}

	var sessions []burnSession
	var sessionStart *time.Time
	var sessionStartUtil float64
	var lastResets string

	for i, p := range points {
		if i > 0 && (p.fiveHourResets != lastResets || p.fiveHourUtil < points[i-1].fiveHourUtil-40) {
			sessionStart = nil
		}
		lastResets = p.fiveHourResets

		if sessionStart == nil && p.fiveHourUtil < 10 {
			t := p.capturedAt
			sessionStart = &t
			sessionStartUtil = p.fiveHourUtil
		}

		if sessionStart != nil && p.fiveHourUtil >= 95 {
			sessions = append(sessions, burnSession{
				startedAt: *sessionStart,
				reachedAt: p.capturedAt,
				startUtil: sessionStartUtil,
				peakUtil:  p.fiveHourUtil,
			})
			sessionStart = nil
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		di := sessions[i].reachedAt.Sub(sessions[i].startedAt)
		dj := sessions[j].reachedAt.Sub(sessions[j].startedAt)
		return di < dj
	})

	var out []domain.FastestBurnEntry
	var todayEntry *domain.FastestBurnEntry
	for i, s := range sessions {
		dur := s.reachedAt.Sub(s.startedAt)
		isToday := s.reachedAt.UTC().Format("2006-01-02") == today
		entry := domain.FastestBurnEntry{
			Rank:      i + 1,
			StartedAt: s.startedAt.Format(time.RFC3339),
			ReachedAt: s.reachedAt.Format(time.RFC3339),
			DurationS: int(dur.Seconds()),
			StartUtil: s.startUtil,
			PeakUtil:  s.peakUtil,
			IsToday:   isToday,
		}
		if i < 5 {
			out = append(out, entry)
		}
		if isToday && todayEntry == nil {
			e := entry
			todayEntry = &e
		}
	}
	return out, todayEntry
}

func detectHighestDailySpend(runs []domain.Run, today string) ([]domain.HighestDailyEntry, *domain.HighestDailyEntry) {
	daily := make(map[string]float64)
	for _, r := range runs {
		if r.StartedAt == "" || r.CostUSD <= 0 {
			continue
		}
		day := r.StartedAt[:10]
		daily[day] += r.CostUSD
	}

	type dayEntry struct {
		date  string
		spend float64
	}
	var entries []dayEntry
	for d, s := range daily {
		entries = append(entries, dayEntry{d, s})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].spend > entries[j].spend
	})

	var out []domain.HighestDailyEntry
	var todayEntry *domain.HighestDailyEntry
	for i, e := range entries {
		isToday := e.date == today
		entry := domain.HighestDailyEntry{
			Rank:      i + 1,
			Date:      e.date,
			PeakSpend: e.spend,
			IsToday:   isToday,
		}
		if i < 5 {
			out = append(out, entry)
		}
		if isToday {
			todayEntry = &domain.HighestDailyEntry{
				Rank:      i + 1,
				Date:      e.date,
				PeakSpend: e.spend,
				IsToday:   true,
			}
		}
	}
	if todayEntry == nil {
		todayEntry = &domain.HighestDailyEntry{
			Rank:      len(entries) + 1,
			Date:      today,
			PeakSpend: 0,
			IsToday:   true,
		}
	}
	return out, todayEntry
}

func detectMaxBurnRates(runs []domain.Run, today string) ([]domain.MaxBurnRateEntry, *domain.MaxBurnRateEntry) {
	type burnEntry struct {
		taskID   int64
		date     string
		cost     float64
		ratePerH float64
	}
	var entries []burnEntry
	for _, r := range runs {
		if r.CostUSD <= 0 || r.StartedAt == "" || r.EndedAt == "" {
			continue
		}
		start, err1 := time.Parse(time.RFC3339, r.StartedAt)
		end, err2 := time.Parse(time.RFC3339, r.EndedAt)
		if err1 != nil || err2 != nil {
			continue
		}
		dur := end.Sub(start)
		if dur < time.Minute {
			continue
		}
		ratePerH := r.CostUSD / dur.Hours()
		entries = append(entries, burnEntry{
			taskID:   r.TaskID,
			date:     r.StartedAt[:10],
			cost:     r.CostUSD,
			ratePerH: ratePerH,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ratePerH > entries[j].ratePerH
	})

	var out []domain.MaxBurnRateEntry
	var todayEntry *domain.MaxBurnRateEntry
	for i, e := range entries {
		isToday := e.date == today
		entry := domain.MaxBurnRateEntry{
			Rank:     i + 1,
			Date:     e.date,
			TaskID:   e.taskID,
			CostUSD:  e.cost,
			RatePerH: e.ratePerH,
			IsToday:  isToday,
		}
		if i < 5 {
			out = append(out, entry)
		}
		if isToday && todayEntry == nil {
			todayEntry = &domain.MaxBurnRateEntry{
				Rank:     i + 1,
				Date:     e.date,
				TaskID:   e.taskID,
				CostUSD:  e.cost,
				RatePerH: e.ratePerH,
				IsToday:  true,
			}
		}
	}
	return out, todayEntry
}

func detectMostTasksDaily(runs []domain.Run, today string) ([]domain.MostTasksDailyEntry, *domain.MostTasksDailyEntry) {
	daily := make(map[string]map[int64]bool)
	for _, r := range runs {
		if r.StartedAt == "" || r.CostUSD <= 0 {
			continue
		}
		day := r.StartedAt[:10]
		if daily[day] == nil {
			daily[day] = make(map[int64]bool)
		}
		daily[day][r.TaskID] = true
	}

	type dayEntry struct {
		date  string
		count int
	}
	var entries []dayEntry
	for d, tasks := range daily {
		entries = append(entries, dayEntry{d, len(tasks)})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	var out []domain.MostTasksDailyEntry
	var todayEntry *domain.MostTasksDailyEntry
	for i, e := range entries {
		isToday := e.date == today
		entry := domain.MostTasksDailyEntry{
			Rank:    i + 1,
			Date:    e.date,
			Count:   e.count,
			IsToday: isToday,
		}
		if i < 5 {
			out = append(out, entry)
		}
		if isToday {
			todayEntry = &domain.MostTasksDailyEntry{
				Rank:    i + 1,
				Date:    e.date,
				Count:   e.count,
				IsToday: true,
			}
		}
	}
	if todayEntry == nil {
		todayEntry = &domain.MostTasksDailyEntry{
			Rank:    len(entries) + 1,
			Date:    today,
			Count:   0,
			IsToday: true,
		}
	}
	return out, todayEntry
}

func extractScopedWeekly(rawJSON string) []ScopedWeekly {
	if rawJSON == "" {
		return nil
	}
	var resp struct {
		Limits []struct {
			Kind    string  `json:"kind"`
			Percent float64 `json:"percent"`
			Scope   *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		return nil
	}
	var out []ScopedWeekly
	for _, l := range resp.Limits {
		if l.Kind == "weekly_scoped" && l.Scope != nil && l.Scope.Model != nil {
			out = append(out, ScopedWeekly{
				Model:   l.Scope.Model.DisplayName,
				Percent: l.Percent,
			})
		}
	}
	return out
}
