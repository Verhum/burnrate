package store

import (
	"fmt"
	"sort"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

func (s *Store) Achievements() (*domain.AchievementsData, error) {
	runs, err := s.ListRuns(0, streakRunLimit)
	if err != nil {
		return nil, err
	}
	return computeAchievements(runs), nil
}

type achievementDef struct {
	ID          string
	Name        string
	Description string
	Icon        string
	Check       func(stats *runStats) (bool, string)
	Progress    func(stats *runStats) (float64, string) // (fraction 0-1, label like "37 / 100 runs")
}

type runStats struct {
	totalRuns      int
	totalTasks     int
	totalCostUSD   float64
	totalLines     int
	activeDays     int
	currentStreak  int
	longestStreak  int
	hasNightRun    bool
	nightRunAt     string
	hasEarlyRun    bool
	earlyRunAt     string
	hasWeekendRun  bool
	weekendRunAt   string
	maxTasksInDay  int
	maxTasksDayAt  string
	cheapestRun    float64
	cheapestRunAt  string
	costliestRun   float64
	costliestRunAt string
	firstRunAt     string
	distinctModels int
}

var achievements = []achievementDef{
	{
		ID: "first_blood", Name: "First Blood",
		Description: "Complete your first task",
		Icon:        "drop",
		Check: func(s *runStats) (bool, string) {
			return s.totalRuns >= 1, s.firstRunAt
		},
	},
	{
		ID: "night_owl", Name: "Night Owl",
		Description: "Run a task between midnight and 5am",
		Icon:        "moon",
		Check: func(s *runStats) (bool, string) {
			return s.hasNightRun, s.nightRunAt
		},
	},
	{
		ID: "early_bird", Name: "Early Bird",
		Description: "Run a task between 5am and 7am",
		Icon:        "sun",
		Check: func(s *runStats) (bool, string) {
			return s.hasEarlyRun, s.earlyRunAt
		},
	},
	{
		ID: "weekend_warrior", Name: "Weekend Warrior",
		Description: "Run a task on a weekend",
		Icon:        "shield",
		Check: func(s *runStats) (bool, string) {
			return s.hasWeekendRun, s.weekendRunAt
		},
	},
	{
		ID: "hat_trick", Name: "Hat Trick",
		Description: "Complete 3 or more tasks in a single day",
		Icon:        "star",
		Check: func(s *runStats) (bool, string) {
			return s.maxTasksInDay >= 3, s.maxTasksDayAt
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(float64(s.maxTasksInDay), 3), fmt.Sprintf("%d / 3 tasks in a day", s.maxTasksInDay)
		},
	},
	{
		ID: "penny_pincher", Name: "Penny Pincher",
		Description: "Complete a task for under $0.50",
		Icon:        "coin",
		Check: func(s *runStats) (bool, string) {
			return s.cheapestRun > 0 && s.cheapestRun < 0.50, s.cheapestRunAt
		},
	},
	{
		ID: "big_spender", Name: "Big Spender",
		Description: "Spend $100 or more lifetime",
		Icon:        "flame",
		Check: func(s *runStats) (bool, string) {
			return s.totalCostUSD >= 100, ""
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(s.totalCostUSD, 100), fmt.Sprintf("$%.0f / $100", s.totalCostUSD)
		},
	},
	{
		ID: "centurion", Name: "Centurion",
		Description: "Complete 100 runs",
		Icon:        "trophy",
		Check: func(s *runStats) (bool, string) {
			return s.totalRuns >= 100, ""
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(float64(s.totalRuns), 100), fmt.Sprintf("%d / 100 runs", s.totalRuns)
		},
	},
	{
		ID: "streak_3", Name: "On a Roll",
		Description: "Reach a 3-day streak",
		Icon:        "bolt",
		Check: func(s *runStats) (bool, string) {
			return s.longestStreak >= 3, ""
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(float64(s.longestStreak), 3), fmt.Sprintf("%d / 3 day streak", s.longestStreak)
		},
	},
	{
		ID: "streak_7", Name: "Unstoppable",
		Description: "Reach a 7-day streak",
		Icon:        "rocket",
		Check: func(s *runStats) (bool, string) {
			return s.longestStreak >= 7, ""
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(float64(s.longestStreak), 7), fmt.Sprintf("%d / 7 day streak", s.longestStreak)
		},
	},
	{
		ID: "prolific", Name: "Prolific",
		Description: "Work on 10 distinct tasks",
		Icon:        "layers",
		Check: func(s *runStats) (bool, string) {
			return s.totalTasks >= 10, ""
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(float64(s.totalTasks), 10), fmt.Sprintf("%d / 10 tasks", s.totalTasks)
		},
	},
	{
		ID: "polyglot", Name: "Polyglot",
		Description: "Use 3 or more different models",
		Icon:        "grid",
		Check: func(s *runStats) (bool, string) {
			return s.distinctModels >= 3, ""
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(float64(s.distinctModels), 3), fmt.Sprintf("%d / 3 models", s.distinctModels)
		},
	},
	{
		ID: "thousand_lines", Name: "Thousand Lines",
		Description: "Write 1,000 lines of code",
		Icon:        "code",
		Check: func(s *runStats) (bool, string) {
			return s.totalLines >= 1000, ""
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(float64(s.totalLines), 1000), fmt.Sprintf("%d / 1,000 lines", s.totalLines)
		},
	},
	{
		ID: "whale", Name: "Whale",
		Description: "Spend $10+ on a single run",
		Icon:        "wave",
		Check: func(s *runStats) (bool, string) {
			return s.costliestRun >= 10, s.costliestRunAt
		},
		Progress: func(s *runStats) (float64, string) {
			return clampProgress(s.costliestRun, 10), fmt.Sprintf("$%.2f / $10 best run", s.costliestRun)
		},
	},
}

func computeAchievements(runs []domain.Run) *domain.AchievementsData {
	stats := gatherStats(runs)

	result := &domain.AchievementsData{
		Achievements: make([]domain.Achievement, 0, len(achievements)),
		Total:        len(achievements),
	}

	for _, def := range achievements {
		unlocked, at := def.Check(stats)
		a := domain.Achievement{
			ID:          def.ID,
			Name:        def.Name,
			Description: def.Description,
			Icon:        def.Icon,
			Unlocked:    unlocked,
		}
		if unlocked {
			a.UnlockedAt = at
			result.Unlocked++
		} else if def.Progress != nil {
			a.Progress, a.ProgressText = def.Progress(stats)
		}
		result.Achievements = append(result.Achievements, a)
	}

	return result
}

func gatherStats(runs []domain.Run) *runStats {
	s := &runStats{cheapestRun: -1}
	tasks := map[int64]bool{}
	dayTasks := map[string]map[int64]bool{}
	models := map[string]bool{}

	for _, r := range runs {
		if len(r.StartedAt) < 10 {
			continue
		}
		s.totalRuns++
		s.totalCostUSD += r.CostUSD
		s.totalLines += r.LinesAdded
		tasks[r.TaskID] = true

		if r.Model != "" {
			models[r.Model] = true
		}

		day := r.StartedAt[:10]
		if dayTasks[day] == nil {
			dayTasks[day] = map[int64]bool{}
		}
		dayTasks[day][r.TaskID] = true

		if s.firstRunAt == "" || r.StartedAt < s.firstRunAt {
			s.firstRunAt = r.StartedAt
		}

		if r.CostUSD > 0 && (s.cheapestRun < 0 || r.CostUSD < s.cheapestRun) {
			s.cheapestRun = r.CostUSD
			s.cheapestRunAt = r.StartedAt
		}
		if r.CostUSD > s.costliestRun {
			s.costliestRun = r.CostUSD
			s.costliestRunAt = r.StartedAt
		}

		t, err := time.Parse(time.RFC3339, r.StartedAt)
		if err != nil {
			continue
		}
		hour := t.Hour()
		if hour >= 0 && hour < 5 && !s.hasNightRun {
			s.hasNightRun = true
			s.nightRunAt = r.StartedAt
		}
		if hour >= 5 && hour < 7 && !s.hasEarlyRun {
			s.hasEarlyRun = true
			s.earlyRunAt = r.StartedAt
		}
		wd := t.Weekday()
		if (wd == time.Saturday || wd == time.Sunday) && !s.hasWeekendRun {
			s.hasWeekendRun = true
			s.weekendRunAt = r.StartedAt
		}
	}

	s.totalTasks = len(tasks)
	s.distinctModels = len(models)
	s.activeDays = len(dayTasks)

	for day, ts := range dayTasks {
		if len(ts) > s.maxTasksInDay {
			s.maxTasksInDay = len(ts)
			s.maxTasksDayAt = day + "T00:00:00Z"
		}
	}

	// Compute streaks
	days := make([]string, 0, len(dayTasks))
	for d := range dayTasks {
		days = append(days, d)
	}
	sort.Strings(days)
	if len(days) > 0 {
		today := time.Now().UTC().Format("2006-01-02")
		chainLen, prev := 1, days[0]
		endChain := func() {
			if chainLen > s.longestStreak {
				s.longestStreak = chainLen
			}
			if prev == today || nextDay(prev) == today {
				s.currentStreak = chainLen
			}
		}
		for _, d := range days[1:] {
			if d == nextDay(prev) {
				chainLen++
			} else {
				endChain()
				chainLen = 1
			}
			prev = d
		}
		endChain()
	}

	return s
}

func clampProgress(current, target float64) float64 {
	if target <= 0 {
		return 0
	}
	p := current / target
	if p > 1 {
		p = 1
	}
	if p < 0 {
		p = 0
	}
	return p
}
