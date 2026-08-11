package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestTaskCRUD(t *testing.T) {
	st := testStore(t)

	task, err := st.CreateTask("task1", "do something", "", "small", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Title != "task1" || task.Size != "small" || task.Status != "queued" {
		t.Fatalf("unexpected task: %+v", task)
	}

	task, err = st.UpdateTask(task.ID, "task1-updated", "do more", "/repo", "medium", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if task.Title != "task1-updated" || task.Size != "medium" || task.RepoPath != "/repo" {
		t.Fatalf("unexpected updated task: %+v", task)
	}

	got, err := st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "task1-updated" {
		t.Fatalf("get mismatch: %+v", got)
	}

	tasks, err := st.ListTasks()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if err := st.DeleteTask(task.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	tasks, err = st.ListTasks()
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestTaskModel(t *testing.T) {
	st := testStore(t)

	task, err := st.CreateTask("task-with-model", "prompt", "", "medium", "claude-sonnet-5", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Model != "claude-sonnet-5" {
		t.Fatalf("expected model claude-sonnet-5, got %q", task.Model)
	}

	got, err := st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Model != "claude-sonnet-5" {
		t.Fatalf("get model mismatch: %q", got.Model)
	}

	tasks, _ := st.ListTasks()
	if tasks[0].Model != "claude-sonnet-5" {
		t.Fatalf("list model mismatch: %q", tasks[0].Model)
	}

	updated, err := st.UpdateTask(task.ID, "task-with-model", "prompt", "", "medium", "claude-opus-5")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Model != "claude-opus-5" {
		t.Fatalf("update model mismatch: %q", updated.Model)
	}

	noModel, err := st.CreateTask("task-no-model", "prompt", "", "medium", "", "")
	if err != nil {
		t.Fatalf("create without model: %v", err)
	}
	if noModel.Model != "" {
		t.Fatalf("expected empty model, got %q", noModel.Model)
	}
}

func TestTaskReorder(t *testing.T) {
	st := testStore(t)

	t1, _ := st.CreateTask("A", "pa", "", "small", "", "")
	t2, _ := st.CreateTask("B", "pb", "", "small", "", "")
	t3, _ := st.CreateTask("C", "pc", "", "small", "", "")

	if err := st.ReorderTasks([]int64{t3.ID, t1.ID, t2.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	tasks, _ := st.ListTasks()
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "C" || tasks[1].Title != "A" || tasks[2].Title != "B" {
		t.Fatalf("order wrong: %s, %s, %s", tasks[0].Title, tasks[1].Title, tasks[2].Title)
	}
}

func TestRunLifecycle(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	run, err := st.CreateRun(task.ID, "/wt/1", "branch-1", "/repo", "2025-01-01T00:00:00Z", 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Status != "starting" || run.Attempt != 1 {
		t.Fatalf("unexpected run: %+v", run)
	}

	if err := st.SetRunSessionID(run.ID, "sess-123"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := st.SetRunPID(run.ID, 12345); err != nil {
		t.Fatalf("set pid: %v", err)
	}
	if err := st.SetRunStatus(run.ID, "running"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	if err := st.FinishRun(run.ID, "succeeded", 1.5, 10, "https://github.com/pr/1", "", ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	got, err := st.getRun(run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "succeeded" || got.CostUSD != 1.5 || got.NumTurns != 10 || got.PRURL != "https://github.com/pr/1" {
		t.Fatalf("unexpected finished run: %+v", got)
	}
	if got.SessionID != "sess-123" || got.PID != 12345 {
		t.Fatalf("session/pid not persisted: %+v", got)
	}

	runs, err := st.RunsByStatus("succeeded")
	if err != nil {
		t.Fatalf("by status: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
}

func TestResumableRuns(t *testing.T) {
	st := testStore(t)

	t1, _ := st.CreateTask("task-a", "pa", "", "small", "", "")
	t2, _ := st.CreateTask("task-b", "pb", "", "small", "", "")

	st.SetTaskStatus(t1.ID, "resumable")
	st.SetTaskStatus(t2.ID, "resumable")

	r1, _ := st.CreateRun(t1.ID, "/wt/1", "b1", "/repo", "w1", 1)
	st.SetRunSessionID(r1.ID, "sess-1")
	st.SetRunStatus(r1.ID, "rate_limited")

	r2, _ := st.CreateRun(t2.ID, "/wt/2", "b2", "/repo", "w1", 1)
	st.SetRunSessionID(r2.ID, "sess-2")
	st.SetRunStatus(r2.ID, "timed_out")

	// Create a run without session_id — should NOT be resumable
	st.CreateTask("task-c", "pc", "", "small", "", "")

	resumable, err := st.ResumableRuns()
	if err != nil {
		t.Fatalf("resumable: %v", err)
	}
	if len(resumable) != 2 {
		t.Fatalf("expected 2 resumable, got %d", len(resumable))
	}
	if resumable[0].TaskID != t1.ID || resumable[1].TaskID != t2.ID {
		t.Fatalf("wrong order: %d, %d", resumable[0].TaskID, resumable[1].TaskID)
	}
}

func TestListRuns(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	st.CreateRun(task.ID, "/wt/1", "b1", "/r", "w1", 1)
	st.CreateRun(task.ID, "/wt/2", "b2", "/r", "w2", 2)

	runs, err := st.ListRuns(task.ID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}

	all, err := st.ListRuns(0, 10)
	if err != nil {
		t.Fatalf("list all runs: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 runs total, got %d", len(all))
	}
}

func TestSettingsRoundtrip(t *testing.T) {
	st := testStore(t)

	if _, ok := st.GetSetting("missing"); ok {
		t.Fatal("expected no value for missing key")
	}

	if err := st.SetSetting("model", "opus"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok := st.GetSetting("model")
	if !ok || v != "opus" {
		t.Fatalf("expected 'opus', got %q (ok=%v)", v, ok)
	}

	if err := st.SetSetting("model", "sonnet"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	v, _ = st.GetSetting("model")
	if v != "sonnet" {
		t.Fatalf("expected 'sonnet', got %q", v)
	}

	all, err := st.AllSettings()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 || all["model"] != "sonnet" {
		t.Fatalf("unexpected all: %+v", all)
	}
}

func TestUsageSnapshot(t *testing.T) {
	st := testStore(t)

	snap, err := st.LatestUsageSnapshot()
	if err != nil {
		t.Fatalf("latest empty: %v", err)
	}
	if snap != nil {
		t.Fatal("expected nil for empty table")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	opusUtil := 45.0
	if err := st.InsertUsageSnapshot(UsageSnapshot{
		CapturedAt:       now,
		FiveHourUtil:     50.5,
		FiveHourResetsAt: "2025-01-01T05:00:00Z",
		SevenDayUtil:     30.0,
		SevenDayResetsAt: "2025-01-07T00:00:00Z",
		SevenDayOpusUtil: &opusUtil,
		RawJSON:          `{"test":true}`,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	snap, err = st.LatestUsageSnapshot()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if snap.FiveHourUtil != 50.5 || snap.SevenDayUtil != 30.0 {
		t.Fatalf("unexpected snap: %+v", snap)
	}
	if snap.SevenDayOpusUtil == nil || *snap.SevenDayOpusUtil != 45.0 {
		t.Fatalf("opus util: %v", snap.SevenDayOpusUtil)
	}

	// Insert another older snapshot and trim
	old := time.Now().Add(-15 * 24 * time.Hour).UTC().Format(time.RFC3339)
	st.InsertUsageSnapshot(UsageSnapshot{
		CapturedAt:       old,
		FiveHourUtil:     10,
		FiveHourResetsAt: "2024-12-01T00:00:00Z",
		SevenDayUtil:     5,
		SevenDayResetsAt: "2024-12-07T00:00:00Z",
		RawJSON:          "{}",
	})

	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	if err := st.TrimUsageSnapshots(cutoff); err != nil {
		t.Fatalf("trim: %v", err)
	}

	snap, _ = st.LatestUsageSnapshot()
	if snap.CapturedAt == old {
		t.Fatal("trim did not remove old snapshot")
	}
}

func TestTaskCountsByStatus(t *testing.T) {
	st := testStore(t)

	st.CreateTask("a", "p", "", "small", "", "")
	st.CreateTask("b", "p", "", "small", "", "")
	t3, _ := st.CreateTask("c", "p", "", "small", "", "")
	st.SetTaskStatus(t3.ID, "done")

	counts, err := st.TaskCountsByStatus()
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts["queued"] != 2 || counts["done"] != 1 {
		t.Fatalf("unexpected counts: %v", counts)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	st1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	st1.Close()

	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	st2.Close()
}

func TestQueuedTasksByOrder(t *testing.T) {
	st := testStore(t)

	st.CreateTask("c", "p", "", "small", "", "")
	st.CreateTask("a", "p", "", "small", "", "")
	st.CreateTask("b", "p", "", "small", "", "")

	tasks, _ := st.ListTasks()
	st.ReorderTasks([]int64{tasks[2].ID, tasks[0].ID, tasks[1].ID})

	queued, err := st.QueuedTasksByOrder()
	if err != nil {
		t.Fatalf("queued: %v", err)
	}
	if len(queued) != 3 {
		t.Fatalf("expected 3, got %d", len(queued))
	}
	if queued[0].Title != "b" || queued[1].Title != "c" || queued[2].Title != "a" {
		t.Fatalf("order wrong: %s, %s, %s", queued[0].Title, queued[1].Title, queued[2].Title)
	}
}

func TestLatestRunForTask(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("t", "p", "", "small", "", "")

	r, err := st.LatestRunForTask(task.ID)
	if err != nil {
		t.Fatalf("latest (empty): %v", err)
	}
	if r != nil {
		t.Fatal("expected nil")
	}

	st.CreateRun(task.ID, "/wt/1", "b1", "/r", "w1", 1)
	r2, _ := st.CreateRun(task.ID, "/wt/2", "b2", "/r", "w2", 2)

	r, err = st.LatestRunForTask(task.ID)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if r.ID != r2.ID {
		t.Fatalf("expected run %d, got %d", r2.ID, r.ID)
	}
}

func TestCreateTaskBacklog(t *testing.T) {
	st := testStore(t)

	task, err := st.CreateTask("backlog-task", "do later", "", "small", "", "backlog")
	if err != nil {
		t.Fatalf("create backlog task: %v", err)
	}
	if task.Status != "backlog" {
		t.Fatalf("expected status=backlog, got %s", task.Status)
	}

	// Backlog tasks should NOT appear in QueuedTasksByOrder
	queued, err := st.QueuedTasksByOrder()
	if err != nil {
		t.Fatalf("queued: %v", err)
	}
	for _, q := range queued {
		if q.ID == task.ID {
			t.Fatal("backlog task should not appear in queued list")
		}
	}

	// Invalid status should fail
	_, err = st.CreateTask("bad-task", "p", "", "small", "", "running")
	if err == nil {
		t.Fatal("expected error for invalid create status")
	}
}

func TestCommentCRUD(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")

	c, err := st.AddComment(task.ID, "please also fix the tests", "user")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if c.Body != "please also fix the tests" || c.ConsumedByRun != 0 || c.TaskID != task.ID {
		t.Fatalf("unexpected comment: %+v", c)
	}

	st.AddComment(task.ID, "and update the docs", "user")

	comments, err := st.CommentsForTask(task.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].Body != "please also fix the tests" || comments[1].Body != "and update the docs" {
		t.Fatalf("wrong order: %s, %s", comments[0].Body, comments[1].Body)
	}
}

func TestCommentConsumedMarking(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "b", "/r", "w", 1)

	st.AddComment(task.ID, "comment 1", "user")
	st.AddComment(task.ID, "comment 2", "user")

	unconsumed, err := st.UnconsumedComments(task.ID)
	if err != nil {
		t.Fatalf("unconsumed: %v", err)
	}
	if len(unconsumed) != 2 {
		t.Fatalf("expected 2 unconsumed, got %d", len(unconsumed))
	}

	if err := st.MarkCommentsConsumed(task.ID, run.ID); err != nil {
		t.Fatalf("mark consumed: %v", err)
	}

	unconsumed, _ = st.UnconsumedComments(task.ID)
	if len(unconsumed) != 0 {
		t.Fatalf("expected 0 unconsumed after marking, got %d", len(unconsumed))
	}

	all, _ := st.CommentsForTask(task.ID)
	for _, c := range all {
		if c.ConsumedByRun != run.ID {
			t.Fatalf("expected consumed_by_run=%d, got %d", run.ID, c.ConsumedByRun)
		}
	}
}

func TestCommentCascadeDelete(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("task1", "prompt", "", "small", "", "")
	st.AddComment(task.ID, "a comment", "user")

	comments, _ := st.CommentsForTask(task.ID)
	if len(comments) != 1 {
		t.Fatal("expected 1 comment before delete")
	}

	if err := st.DeleteTask(task.ID); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	// Comments should be cascade-deleted. Query the raw table since
	// CommentsForTask won't return results for a deleted task.
	var count int
	st.db.QueryRow("SELECT COUNT(*) FROM task_comments").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 comments after cascade delete, got %d", count)
	}
}

func TestAgentRepoColumns(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("agent-task", "do stuff", "", "small", "", "")
	run, err := st.CreateRun(task.ID, "/tmp/agentwork/task-1", "", "", "w1", 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	if run.AgentRepo != "" || run.AgentWorkedIn != "" {
		t.Fatalf("new run should have empty agent fields, got repo=%q worked_in=%q", run.AgentRepo, run.AgentWorkedIn)
	}

	if err := st.SetRunAgentRepo(run.ID, "verhum/burnrate"); err != nil {
		t.Fatalf("set agent_repo: %v", err)
	}
	if err := st.SetRunAgentWorkedIn(run.ID, "/base/code/burnrate"); err != nil {
		t.Fatalf("set agent_worked_in: %v", err)
	}
	if err := st.SetRunBranch(run.ID, "burnrate/1-test"); err != nil {
		t.Fatalf("set branch: %v", err)
	}

	got, err := st.LatestRunForTask(task.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.AgentRepo != "verhum/burnrate" {
		t.Fatalf("expected agent_repo=verhum/burnrate, got %q", got.AgentRepo)
	}
	if got.AgentWorkedIn != "/base/code/burnrate" {
		t.Fatalf("expected agent_worked_in, got %q", got.AgentWorkedIn)
	}
	if got.Branch != "burnrate/1-test" {
		t.Fatalf("expected branch=burnrate/1-test, got %q", got.Branch)
	}
}

func TestLatestRunStatusWins(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("multi-run-task", "prompt", "", "small", "", "")

	r1, _ := st.CreateRun(task.ID, "/wt/1", "b1", "/r", "w1", 1)
	st.FinishRun(r1.ID, "succeeded", 1.0, 5, "https://github.com/pr/1", "", "")

	r2, _ := st.CreateRun(task.ID, "/wt/2", "b2", "/r", "w2", 2)
	st.FinishRun(r2.ID, "errored", 0.5, 3, "", "something broke", "")

	tasks, err := st.ListTasks()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].LatestRunStatus != "errored" {
		t.Fatalf("expected latest_run_status=errored, got %q", tasks[0].LatestRunStatus)
	}

	got, _ := st.GetTask(task.ID)
	if got.LatestRunStatus != "errored" {
		t.Fatalf("GetTask: expected latest_run_status=errored, got %q", got.LatestRunStatus)
	}
}

func TestDisplayID(t *testing.T) {
	st := testStore(t)

	t1, _ := st.CreateTask("first", "p", "", "small", "", "")
	if t1.DisplayID != "BR1" {
		t.Fatalf("expected BR1, got %q", t1.DisplayID)
	}

	t2, _ := st.CreateTask("second", "p", "", "small", "", "")
	tasks, _ := st.ListTasks()
	for _, task := range tasks {
		expected := "BR" + fmt.Sprintf("%d", task.ID)
		if task.DisplayID != expected {
			t.Fatalf("expected %s, got %s", expected, task.DisplayID)
		}
	}

	got, _ := st.GetTask(t2.ID)
	expected := "BR" + fmt.Sprintf("%d", t2.ID)
	if got.DisplayID != expected {
		t.Fatalf("GetTask: expected %s, got %s", expected, got.DisplayID)
	}
}

func TestValidateStatusTransition(t *testing.T) {
	tests := []struct {
		from, to string
		wantErr  bool
	}{
		// Free transitions from any non-running status
		{"done", "queued", false},
		{"done", "backlog", false},
		{"dismissed", "queued", false},
		{"failed", "queued", false},
		{"failed", "done", false},
		{"pr_created", "done", false},
		{"pr_created", "dismissed", false},
		{"pr_created", "queued", false},
		{"pr_created", "backlog", false},
		{"pr_created", "resumable", false},
		{"backlog", "queued", false},
		{"queued", "paused", false},
		{"paused", "queued", false},
		{"resumable", "done", false},
		// pr_created and running are never valid targets
		{"queued", "pr_created", true},
		{"backlog", "pr_created", true},
		{"failed", "pr_created", true},
		{"done", "pr_created", true},
		{"queued", "running", true},
		{"backlog", "running", true},
		{"done", "running", true},
		// running status cannot be changed
		{"running", "queued", true},
		{"running", "done", true},
	}
	for _, tt := range tests {
		err := ValidateStatusTransition(tt.from, tt.to)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s→%s: err=%v, wantErr=%v", tt.from, tt.to, err, tt.wantErr)
		}
	}
}

func TestPrCreatedTaskNotInQueuedList(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("review-me", "prompt", "", "small", "", "")
	st.SetTaskStatus(task.ID, "pr_created")

	queued, err := st.QueuedTasksByOrder()
	if err != nil {
		t.Fatalf("queued: %v", err)
	}
	for _, q := range queued {
		if q.ID == task.ID {
			t.Fatal("pr_created task should not appear in queued list")
		}
	}
}

func TestTasksByStatus(t *testing.T) {
	st := testStore(t)

	t1, _ := st.CreateTask("a", "p", "", "small", "", "")
	st.SetTaskStatus(t1.ID, "pr_created")
	st.CreateTask("b", "p", "", "small", "", "")

	got, err := st.TasksByStatus("pr_created")
	if err != nil {
		t.Fatalf("TasksByStatus: %v", err)
	}
	if len(got) != 1 || got[0].ID != t1.ID {
		t.Fatalf("expected 1 pr_created task, got %d", len(got))
	}
}

func TestSubsetReorder(t *testing.T) {
	st := testStore(t)

	t1, _ := st.CreateTask("A", "p", "", "small", "", "")
	t2, _ := st.CreateTask("B", "p", "", "small", "", "")
	t3, _ := st.CreateTask("C", "p", "", "small", "", "")
	t4, _ := st.CreateTask("D", "p", "", "small", "", "")
	t5, _ := st.CreateTask("E", "p", "", "small", "", "")

	// Subset reorder: [t4, t2] swaps their positions
	// Current orders: t1=10, t2=20, t3=30, t4=40, t5=50
	// Sorted slot values for {t4,t2}: [20, 40]
	// Assign in posted order: t4←20, t2←40
	if err := st.ReorderTasks([]int64{t4.ID, t2.ID}); err != nil {
		t.Fatalf("subset reorder: %v", err)
	}

	tasks, _ := st.ListTasks()
	expected := []string{"A", "D", "C", "B", "E"}
	if len(tasks) != 5 {
		t.Fatalf("expected 5 tasks, got %d", len(tasks))
	}
	for i, exp := range expected {
		if tasks[i].Title != exp {
			t.Fatalf("position %d: expected %s, got %s (order=%.0f)", i, exp, tasks[i].Title, tasks[i].SortOrder)
		}
	}

	// Untouched tasks kept their exact sort_order
	for _, task := range tasks {
		switch task.Title {
		case "A":
			if task.SortOrder != 10 {
				t.Fatalf("A order: expected 10, got %.0f", task.SortOrder)
			}
		case "C":
			if task.SortOrder != 30 {
				t.Fatalf("C order: expected 30, got %.0f", task.SortOrder)
			}
		case "E":
			if task.SortOrder != 50 {
				t.Fatalf("E order: expected 50, got %.0f", task.SortOrder)
			}
		}
	}

	// Full-list reorder still works
	if err := st.ReorderTasks([]int64{t5.ID, t4.ID, t3.ID, t2.ID, t1.ID}); err != nil {
		t.Fatalf("full reorder: %v", err)
	}
	tasks, _ = st.ListTasks()
	expected = []string{"E", "D", "C", "B", "A"}
	for i, exp := range expected {
		if tasks[i].Title != exp {
			t.Fatalf("full reorder position %d: expected %s, got %s", i, exp, tasks[i].Title)
		}
	}

	// Unknown IDs error
	if err := st.ReorderTasks([]int64{t1.ID, 9999}); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestDataDirEnv(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("BURNRATE_DATA_DIR", dir)
	defer os.Unsetenv("BURNRATE_DATA_DIR")

	dbPath := filepath.Join(dir, "test.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	st.Close()
}
