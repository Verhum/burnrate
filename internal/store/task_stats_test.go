package store

import (
	"testing"
	"time"
)

func TestTaskStatsMap(t *testing.T) {
	st := testStore(t)

	t1, err := st.CreateTask("task-a", "prompt a", "", "small", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := st.CreateTask("task-b", "prompt b", "", "small", "", "")
	if err != nil {
		t.Fatal(err)
	}

	r1, err := st.CreateRun(t1.ID, "/wt1", "branch-a", "/repo", "w1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRunModel(r1.ID, "claude-opus-4"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRunLines(r1.ID, 100, 20); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(r1.ID, "succeeded", 5.50, 10, "", "", ""); err != nil {
		t.Fatal(err)
	}

	r2, err := st.CreateRun(t1.ID, "/wt1", "branch-a", "/repo", "w1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRunLines(r2.ID, 50, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(r2.ID, "succeeded", 3.25, 5, "", "", ""); err != nil {
		t.Fatal(err)
	}

	r3, err := st.CreateRun(t2.ID, "/wt2", "branch-b", "/repo", "w1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRunModel(r3.ID, "claude-sonnet-4"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRunLines(r3.ID, 200, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(r3.ID, "succeeded", 1.00, 3, "", "", ""); err != nil {
		t.Fatal(err)
	}

	// Give the runs some duration: manually set started_at/ended_at.
	now := time.Now().UTC()
	start1 := now.Add(-10 * time.Minute).Format(time.RFC3339)
	end1 := now.Add(-4 * time.Minute).Format(time.RFC3339) // 6 min
	st.db.Exec("UPDATE runs SET started_at=?, ended_at=? WHERE id=?", start1, end1, r1.ID)

	start2 := now.Add(-3 * time.Minute).Format(time.RFC3339)
	end2 := now.Format(time.RFC3339) // 3 min
	st.db.Exec("UPDATE runs SET started_at=?, ended_at=? WHERE id=?", start2, end2, r2.ID)

	m, err := st.TaskStatsMap()
	if err != nil {
		t.Fatal(err)
	}

	s1, ok := m[t1.ID]
	if !ok {
		t.Fatalf("no stats for task %d", t1.ID)
	}
	if s1.Runs != 2 {
		t.Errorf("task %d runs = %d, want 2", t1.ID, s1.Runs)
	}
	if s1.CostUSD != 8.75 {
		t.Errorf("task %d cost = %.2f, want 8.75", t1.ID, s1.CostUSD)
	}
	if s1.LinesAdded != 150 {
		t.Errorf("task %d lines_added = %d, want 150", t1.ID, s1.LinesAdded)
	}
	if s1.LinesRemoved != 30 {
		t.Errorf("task %d lines_removed = %d, want 30", t1.ID, s1.LinesRemoved)
	}
	// Duration should be ~540s (6 + 3 min), allow some julianday rounding.
	if s1.DurationSec < 530 || s1.DurationSec > 550 {
		t.Errorf("task %d duration = %d, want ~540", t1.ID, s1.DurationSec)
	}
	if s1.Model != "claude-opus-4" {
		t.Errorf("task %d model = %q, want claude-opus-4", t1.ID, s1.Model)
	}

	s2, ok := m[t2.ID]
	if !ok {
		t.Fatalf("no stats for task %d", t2.ID)
	}
	if s2.Runs != 1 {
		t.Errorf("task %d runs = %d, want 1", t2.ID, s2.Runs)
	}
	if s2.CostUSD != 1.0 {
		t.Errorf("task %d cost = %.2f, want 1.00", t2.ID, s2.CostUSD)
	}
	if s2.LinesAdded != 200 {
		t.Errorf("task %d lines_added = %d, want 200", t2.ID, s2.LinesAdded)
	}
	if s2.Model != "claude-sonnet-4" {
		t.Errorf("task %d model = %q, want claude-sonnet-4", t2.ID, s2.Model)
	}
}

func TestTaskStatsMapEmpty(t *testing.T) {
	st := testStore(t)

	m, err := st.TaskStatsMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}
