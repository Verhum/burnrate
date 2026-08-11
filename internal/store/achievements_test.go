package store

import (
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

func TestComputeAchievements_Empty(t *testing.T) {
	data := computeAchievements(nil)
	if data.Unlocked != 0 {
		t.Fatalf("expected 0 unlocked, got %d", data.Unlocked)
	}
	if data.Total != len(achievements) {
		t.Fatalf("expected total=%d, got %d", len(achievements), data.Total)
	}
	for _, a := range data.Achievements {
		if a.Unlocked {
			t.Fatalf("achievement %q should not be unlocked with no runs", a.ID)
		}
	}
}

func TestComputeAchievements_FirstBlood(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 1},
	}
	data := computeAchievements(runs)
	a := findAchievement(t, data, "first_blood")
	if !a.Unlocked {
		t.Fatal("first_blood should be unlocked after one run")
	}
}

func TestComputeAchievements_NightOwl(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T03:30:00Z", TaskID: 1, CostUSD: 1},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "night_owl"); !a.Unlocked {
		t.Fatal("night_owl should unlock for a 3:30am run")
	}
	if a := findAchievement(t, data, "early_bird"); a.Unlocked {
		t.Fatal("early_bird should not unlock for a 3:30am run")
	}
}

func TestComputeAchievements_EarlyBird(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T05:30:00Z", TaskID: 1, CostUSD: 1},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "early_bird"); !a.Unlocked {
		t.Fatal("early_bird should unlock for a 5:30am run")
	}
	if a := findAchievement(t, data, "night_owl"); a.Unlocked {
		t.Fatal("night_owl should not unlock for a 5:30am run")
	}
}

func TestComputeAchievements_WeekendWarrior(t *testing.T) {
	sat := nextSaturday()
	runs := []domain.Run{
		{StartedAt: sat + "T12:00:00Z", TaskID: 1, CostUSD: 1},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "weekend_warrior"); !a.Unlocked {
		t.Fatal("weekend_warrior should unlock for a Saturday run")
	}
}

func TestComputeAchievements_HatTrick(t *testing.T) {
	day := streakDay(0)
	runs := []domain.Run{
		{StartedAt: day + "T08:00:00Z", TaskID: 1, CostUSD: 1},
		{StartedAt: day + "T10:00:00Z", TaskID: 2, CostUSD: 1},
		{StartedAt: day + "T14:00:00Z", TaskID: 3, CostUSD: 1},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "hat_trick"); !a.Unlocked {
		t.Fatal("hat_trick should unlock with 3 distinct tasks in one day")
	}
}

func TestComputeAchievements_HatTrickSameTaskDoesNotCount(t *testing.T) {
	day := streakDay(0)
	runs := []domain.Run{
		{StartedAt: day + "T08:00:00Z", TaskID: 1, CostUSD: 1},
		{StartedAt: day + "T10:00:00Z", TaskID: 1, CostUSD: 1},
		{StartedAt: day + "T14:00:00Z", TaskID: 1, CostUSD: 1},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "hat_trick"); a.Unlocked {
		t.Fatal("hat_trick should not unlock for 3 runs of the same task")
	}
}

func TestComputeAchievements_PennyPincher(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 0.25},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "penny_pincher"); !a.Unlocked {
		t.Fatal("penny_pincher should unlock for a $0.25 run")
	}
}

func TestComputeAchievements_PennyPincherZeroCostDoesNotCount(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 0},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "penny_pincher"); a.Unlocked {
		t.Fatal("penny_pincher should not unlock for a $0 run")
	}
}

func TestComputeAchievements_BigSpender(t *testing.T) {
	runs := make([]domain.Run, 20)
	for i := range runs {
		runs[i] = domain.Run{StartedAt: streakDay(i) + "T10:00:00Z", TaskID: int64(i + 1), CostUSD: 5.5}
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "big_spender"); !a.Unlocked {
		t.Fatal("big_spender should unlock at $110 total")
	}
}

func TestComputeAchievements_Centurion(t *testing.T) {
	runs := make([]domain.Run, 100)
	for i := range runs {
		runs[i] = domain.Run{StartedAt: streakDay(i%30) + "T10:00:00Z", TaskID: int64(i%10 + 1), CostUSD: 0.1}
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "centurion"); !a.Unlocked {
		t.Fatal("centurion should unlock at 100 runs")
	}
}

func TestComputeAchievements_Streak(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(2) + "T10:00:00Z", TaskID: 1, CostUSD: 1},
		{StartedAt: streakDay(1) + "T10:00:00Z", TaskID: 2, CostUSD: 1},
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 3, CostUSD: 1},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "streak_3"); !a.Unlocked {
		t.Fatal("streak_3 should unlock for 3 consecutive days")
	}
	if a := findAchievement(t, data, "streak_7"); a.Unlocked {
		t.Fatal("streak_7 should not unlock for 3 consecutive days")
	}
}

func TestComputeAchievements_Polyglot(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(2) + "T10:00:00Z", TaskID: 1, CostUSD: 1, Model: "claude-opus-5"},
		{StartedAt: streakDay(1) + "T10:00:00Z", TaskID: 2, CostUSD: 1, Model: "claude-sonnet-5"},
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 3, CostUSD: 1, Model: "claude-haiku-4-5"},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "polyglot"); !a.Unlocked {
		t.Fatal("polyglot should unlock for 3 distinct models")
	}
}

func TestComputeAchievements_ThousandLines(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 1, LinesAdded: 1001},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "thousand_lines"); !a.Unlocked {
		t.Fatal("thousand_lines should unlock for 1001 lines")
	}
}

func TestComputeAchievements_Whale(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 12.50},
	}
	data := computeAchievements(runs)
	if a := findAchievement(t, data, "whale"); !a.Unlocked {
		t.Fatal("whale should unlock for a $12.50 single run")
	}
}

func TestComputeAchievements_UnlockedCount(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 0.25, LinesAdded: 50},
	}
	data := computeAchievements(runs)
	counted := 0
	for _, a := range data.Achievements {
		if a.Unlocked {
			counted++
		}
	}
	if counted != data.Unlocked {
		t.Fatalf("unlocked count mismatch: field=%d actual=%d", data.Unlocked, counted)
	}
}

func TestStoreAchievements_ReadsRuns(t *testing.T) {
	st := testStore(t)
	taskID := mustTask(t, st, "achievement task")
	seedRun(t, st, taskID, 0, "claude-opus-5", 2.5, 10, 3)

	data, err := st.Achievements()
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	if data.Total != len(achievements) {
		t.Fatalf("expected total=%d, got %d", len(achievements), data.Total)
	}
	a := findAchievement(t, data, "first_blood")
	if !a.Unlocked {
		t.Fatal("first_blood should be unlocked after seeding a run")
	}
}

func TestComputeAchievements_ProgressOnLockedNumeric(t *testing.T) {
	runs := make([]domain.Run, 37)
	for i := range runs {
		runs[i] = domain.Run{StartedAt: streakDay(i%30) + "T10:00:00Z", TaskID: int64(i%5 + 1), CostUSD: 1.5, LinesAdded: 20}
	}
	data := computeAchievements(runs)

	centurion := findAchievement(t, data, "centurion")
	if centurion.Unlocked {
		t.Fatal("centurion should still be locked at 37 runs")
	}
	if centurion.Progress < 0.36 || centurion.Progress > 0.38 {
		t.Fatalf("centurion progress: want ~0.37, got %f", centurion.Progress)
	}
	if centurion.ProgressText != "37 / 100 runs" {
		t.Fatalf("centurion progress_text: want '37 / 100 runs', got %q", centurion.ProgressText)
	}

	prolific := findAchievement(t, data, "prolific")
	if prolific.Progress < 0.49 || prolific.Progress > 0.51 {
		t.Fatalf("prolific progress: want 0.5 (5/10 tasks), got %f", prolific.Progress)
	}

	thousandLines := findAchievement(t, data, "thousand_lines")
	wantLines := float64(37*20) / 1000.0
	if thousandLines.Progress < wantLines-0.01 || thousandLines.Progress > wantLines+0.01 {
		t.Fatalf("thousand_lines progress: want ~%f, got %f", wantLines, thousandLines.Progress)
	}
}

func TestComputeAchievements_ProgressZeroWhenNoRuns(t *testing.T) {
	data := computeAchievements(nil)
	for _, a := range data.Achievements {
		if a.Progress != 0 {
			t.Fatalf("achievement %q should have zero progress with no runs, got %f", a.ID, a.Progress)
		}
	}
}

func TestComputeAchievements_ProgressOmittedWhenUnlocked(t *testing.T) {
	runs := make([]domain.Run, 100)
	for i := range runs {
		runs[i] = domain.Run{StartedAt: streakDay(i%30) + "T10:00:00Z", TaskID: int64(i%10 + 1), CostUSD: 5.5, LinesAdded: 100}
	}
	data := computeAchievements(runs)
	centurion := findAchievement(t, data, "centurion")
	if !centurion.Unlocked {
		t.Fatal("centurion should be unlocked at 100 runs")
	}
	if centurion.Progress != 0 {
		t.Fatalf("unlocked achievement should have zero progress, got %f", centurion.Progress)
	}
	if centurion.ProgressText != "" {
		t.Fatalf("unlocked achievement should have empty progress_text, got %q", centurion.ProgressText)
	}
}

func TestComputeAchievements_BooleanAchievementsNoProgress(t *testing.T) {
	runs := []domain.Run{
		{StartedAt: streakDay(0) + "T10:00:00Z", TaskID: 1, CostUSD: 1},
	}
	data := computeAchievements(runs)
	for _, id := range []string{"night_owl", "early_bird", "weekend_warrior", "penny_pincher"} {
		a := findAchievement(t, data, id)
		if !a.Unlocked && a.Progress != 0 {
			t.Fatalf("boolean achievement %q should have no progress bar, got %f", id, a.Progress)
		}
	}
}

func findAchievement(t *testing.T, data *domain.AchievementsData, id string) domain.Achievement {
	t.Helper()
	for _, a := range data.Achievements {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("achievement %q not found", id)
	return domain.Achievement{}
}

func nextSaturday() string {
	t := time.Now().UTC()
	for t.Weekday() != time.Saturday {
		t = t.AddDate(0, 0, 1)
	}
	return t.Format("2006-01-02")
}
