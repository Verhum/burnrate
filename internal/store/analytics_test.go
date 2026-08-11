package store

import (
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

// seedRun creates a run for the given task and backdates it to daysAgo, then
// records the model, cost and line churn a finished run would carry.
func seedRun(t *testing.T, st *Store, taskID int64, daysAgo int, model string, cost float64, added, removed int) *Run {
	t.Helper()
	run, err := st.CreateRun(taskID, "/wt", "b", "/repo", "w1", 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	when := time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	if _, err := st.db.Exec("UPDATE runs SET started_at=?, ended_at=? WHERE id=?", when, when, run.ID); err != nil {
		t.Fatalf("backdate run: %v", err)
	}
	if err := st.SetRunModel(run.ID, model); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if err := st.SetRunLines(run.ID, added, removed); err != nil {
		t.Fatalf("set lines: %v", err)
	}
	if err := st.FinishRun(run.ID, "succeeded", cost, 3, "", "", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	// FinishRun stamps ended_at with now; re-backdate so the row stays in the
	// intended bucket.
	if _, err := st.db.Exec("UPDATE runs SET started_at=?, ended_at=? WHERE id=?", when, when, run.ID); err != nil {
		t.Fatalf("re-backdate run: %v", err)
	}
	return run
}

func mustTask(t *testing.T, st *Store, title string) int64 {
	t.Helper()
	task, err := st.CreateTask(title, "prompt", "", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task.ID
}

func pointFor(pts []domain.CostEfficiencyPoint, day, model string) *domain.CostEfficiencyPoint {
	for i := range pts {
		if pts[i].Date == day && pts[i].Model == model {
			return &pts[i]
		}
	}
	return nil
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func TestCostEfficiency_GroupsByDayAndModel(t *testing.T) {
	st := testStore(t)
	a := mustTask(t, st, "a")
	b := mustTask(t, st, "b")

	// Two tasks on opus today: $6 total, 300 lines total.
	seedRun(t, st, a, 0, "claude-opus-4-6", 4, 150, 50)
	seedRun(t, st, b, 0, "claude-opus-4-6", 2, 80, 20)
	// One task on sonnet today.
	seedRun(t, st, a, 0, "claude-sonnet-4-6", 1, 40, 10)

	ce, err := st.CostEfficiency(7)
	if err != nil {
		t.Fatalf("cost efficiency: %v", err)
	}

	opus := pointFor(ce.Points, today(), "claude-opus-4-6")
	if opus == nil {
		t.Fatalf("no opus bucket in %+v", ce.Points)
	}
	if opus.Runs != 2 || opus.Tasks != 2 {
		t.Fatalf("expected 2 runs / 2 tasks, got %d/%d", opus.Runs, opus.Tasks)
	}
	if opus.CostUSD != 6 {
		t.Fatalf("expected $6, got %v", opus.CostUSD)
	}
	if opus.LinesChanged != 300 {
		t.Fatalf("expected 300 lines, got %d", opus.LinesChanged)
	}
	if opus.CostPerTask != 3 {
		t.Fatalf("expected $3/task, got %v", opus.CostPerTask)
	}
	if opus.CostPerLine != 0.02 {
		t.Fatalf("expected $0.02/line, got %v", opus.CostPerLine)
	}

	sonnet := pointFor(ce.Points, today(), "claude-sonnet-4-6")
	if sonnet == nil || sonnet.CostPerTask != 1 {
		t.Fatalf("unexpected sonnet bucket: %+v", sonnet)
	}
}

func TestCostEfficiency_UnrecordedModelBecomesUnknown(t *testing.T) {
	st := testStore(t)
	a := mustTask(t, st, "a")
	seedRun(t, st, a, 0, "", 2, 10, 0)

	ce, err := st.CostEfficiency(7)
	if err != nil {
		t.Fatalf("cost efficiency: %v", err)
	}
	if len(ce.Models) != 1 || ce.Models[0] != domain.UnknownModel {
		t.Fatalf("expected models=[unknown], got %v", ce.Models)
	}
	if p := pointFor(ce.Points, today(), domain.UnknownModel); p == nil {
		t.Fatalf("no unknown bucket in %+v", ce.Points)
	}
}

func TestCostEfficiency_ExcludesRunsOutsideRange(t *testing.T) {
	st := testStore(t)
	a := mustTask(t, st, "a")
	seedRun(t, st, a, 0, "opus", 1, 10, 0)
	seedRun(t, st, a, 40, "opus", 99, 999, 0)

	ce, err := st.CostEfficiency(7)
	if err != nil {
		t.Fatalf("cost efficiency: %v", err)
	}
	var total float64
	for _, p := range ce.Points {
		total += p.CostUSD
	}
	if total != 1 {
		t.Fatalf("expected only the in-range $1, got $%v", total)
	}
	// Series order still knows about the older run's model, since the palette
	// mapping must not shift when the range narrows.
	if len(ce.Models) != 1 || ce.Models[0] != "opus" {
		t.Fatalf("unexpected models %v", ce.Models)
	}
}

func TestCostEfficiency_ModelOrderIsOldestFirstAndRangeIndependent(t *testing.T) {
	st := testStore(t)
	a := mustTask(t, st, "a")
	seedRun(t, st, a, 50, "first", 1, 10, 0)
	seedRun(t, st, a, 20, "second", 1, 10, 0)
	seedRun(t, st, a, 0, "third", 1, 10, 0)

	for _, days := range []int{7, 30, 90} {
		ce, err := st.CostEfficiency(days)
		if err != nil {
			t.Fatalf("cost efficiency(%d): %v", days, err)
		}
		want := []string{"first", "second", "third"}
		if len(ce.Models) != len(want) {
			t.Fatalf("days=%d: got models %v, want %v", days, ce.Models, want)
		}
		for i := range want {
			if ce.Models[i] != want[i] {
				t.Fatalf("days=%d: got models %v, want %v", days, ce.Models, want)
			}
		}
	}
}

func TestCostEfficiency_SkipsRunsWithNeitherCostNorLines(t *testing.T) {
	st := testStore(t)
	a := mustTask(t, st, "a")
	seedRun(t, st, a, 0, "opus", 0, 0, 0)

	ce, err := st.CostEfficiency(7)
	if err != nil {
		t.Fatalf("cost efficiency: %v", err)
	}
	if len(ce.Points) != 0 {
		t.Fatalf("expected no buckets, got %+v", ce.Points)
	}
}

func TestCostEfficiency_ZeroDenominatorLeavesRatioAtZero(t *testing.T) {
	st := testStore(t)
	a := mustTask(t, st, "a")
	// Spend with no measurable lines: cost-per-task is real, cost-per-line is not.
	seedRun(t, st, a, 0, "opus", 5, 0, 0)

	ce, err := st.CostEfficiency(7)
	if err != nil {
		t.Fatalf("cost efficiency: %v", err)
	}
	p := pointFor(ce.Points, today(), "opus")
	if p == nil {
		t.Fatalf("no bucket in %+v", ce.Points)
	}
	if p.CostPerTask != 5 {
		t.Fatalf("expected $5/task, got %v", p.CostPerTask)
	}
	if p.CostPerLine != 0 {
		t.Fatalf("expected $0/line for a zero denominator, got %v", p.CostPerLine)
	}
}

func TestCostEfficiency_TotalsRollUpPerModel(t *testing.T) {
	st := testStore(t)
	a := mustTask(t, st, "a")
	b := mustTask(t, st, "b")
	seedRun(t, st, a, 0, "opus", 4, 100, 0)
	seedRun(t, st, b, 1, "opus", 2, 100, 0)
	seedRun(t, st, a, 0, "sonnet", 1, 50, 0)

	ce, err := st.CostEfficiency(7)
	if err != nil {
		t.Fatalf("cost efficiency: %v", err)
	}
	var opus *domain.CostEfficiencyPoint
	for i := range ce.Totals {
		if ce.Totals[i].Model == "opus" {
			opus = &ce.Totals[i]
		}
	}
	if opus == nil {
		t.Fatalf("no opus total in %+v", ce.Totals)
	}
	if opus.CostUSD != 6 || opus.Tasks != 2 || opus.LinesChanged != 200 {
		t.Fatalf("unexpected opus total: %+v", opus)
	}
	if opus.CostPerTask != 3 || opus.CostPerLine != 0.03 {
		t.Fatalf("unexpected opus ratios: %+v", opus)
	}
	if opus.Date != "" {
		t.Fatalf("totals row should have no date, got %q", opus.Date)
	}
}

func TestRecordTaskPRLines_ReturnsDeltaNotBranchTotal(t *testing.T) {
	st := testStore(t)
	taskID := mustTask(t, st, "a")
	if err := st.UpsertTaskPR(taskID, 1, "o/r", "br", "https://x/pull/1", "/wt"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// First run builds the branch.
	dAdded, dRemoved, err := st.RecordTaskPRLines(taskID, "o/r", "br", 100, 20)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if dAdded != 100 || dRemoved != 20 {
		t.Fatalf("first measurement should be all new, got +%d/-%d", dAdded, dRemoved)
	}

	// A followup run re-measures the whole branch; only the growth is new.
	dAdded, dRemoved, err = st.RecordTaskPRLines(taskID, "o/r", "br", 130, 25)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if dAdded != 30 || dRemoved != 5 {
		t.Fatalf("expected delta +30/-5, got +%d/-%d", dAdded, dRemoved)
	}

	// A branch that shrinks must not hand the run a negative count.
	dAdded, dRemoved, err = st.RecordTaskPRLines(taskID, "o/r", "br", 90, 10)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if dAdded != 0 || dRemoved != 0 {
		t.Fatalf("expected clamped 0/0, got +%d/-%d", dAdded, dRemoved)
	}

	prs, err := st.ListTaskPRs(taskID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prs) != 1 || prs[0].LinesAdded != 90 || prs[0].LinesRemoved != 10 {
		t.Fatalf("branch should hold the latest cumulative total, got %+v", prs)
	}
}

func TestRecordTaskPRLines_MissingRow(t *testing.T) {
	st := testStore(t)
	taskID := mustTask(t, st, "a")
	if _, _, err := st.RecordTaskPRLines(taskID, "o/r", "nope", 1, 1); err == nil {
		t.Fatal("expected an error for an unrecorded branch")
	}
}

func TestSetRunModelAndLinesPersist(t *testing.T) {
	st := testStore(t)
	taskID := mustTask(t, st, "a")
	run := seedRun(t, st, taskID, 0, "claude-opus-4-6", 2, 30, 4)

	got, err := st.LatestRunForTask(taskID)
	if err != nil {
		t.Fatalf("latest run: %v", err)
	}
	if got.ID != run.ID {
		t.Fatalf("unexpected run %d", got.ID)
	}
	if got.Model != "claude-opus-4-6" {
		t.Fatalf("model not persisted: %q", got.Model)
	}
	if got.LinesAdded != 30 || got.LinesRemoved != 4 || got.LinesChanged() != 34 {
		t.Fatalf("lines not persisted: +%d/-%d", got.LinesAdded, got.LinesRemoved)
	}
}
