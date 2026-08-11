package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/runner"
	"github.com/Verhum/burnrate/internal/scheduling"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func testConfig() config.Config {
	cfg, _ := config.Load(nil)
	cfg.ParallelN = 3
	cfg.UtilThreshold = 100
	cfg.SevenDayThreshold = 95
	cfg.DryRun = true
	return cfg
}

// withSnapshot installs snap as the scheduler's latest usage reading, the way a
// successful fetch would. CapturedAt defaults to now because scheduling refuses
// to act on a stale reading, and a zero time reads as infinitely stale.
func withSnapshot(sched *Scheduler, snap usage.Snapshot) {
	if snap.CapturedAt.IsZero() {
		snap.CapturedAt = time.Now()
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.mu.Unlock()
}

// Session state, reset-edge detection and the launch decision itself are tested
// as pure functions in internal/scheduling. What follows covers the adapter: that
// the daemon actually performs the side effects a Plan asks for.

// --- Plan-driven behaviour ---

// TestEndOfSessionStillLaunches is the daemon-level regression test for the dead
// zone that stalled scheduling for the last ten minutes of every session: work
// was skipped silently because its timeout was computed from the remaining
// window. Remaining time is no longer an input, so a session about to reset must
// still launch at the full timeout.
func TestEndOfSessionStillLaunches(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()

	var mu sync.Mutex
	var gotTimeout time.Duration
	launched := false

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		launched = true
		gotTimeout = params.Timeout
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	st.CreateTask("task", "p", "", "medium", "", "")

	sched := New(st, cfg, usage.NewClient("http://unused"), log.New("", false))
	sched.LaunchFunc = fakeLaunch

	withSnapshot(sched, usage.Snapshot{
		// Two minutes left — inside the old drain margin.
		FiveHour: usage.Window{Utilization: 29, ResetsAt: time.Now().Add(2 * time.Minute)},
		SevenDay: usage.Window{Utilization: 10},
	})
	sched.evaluate(t.Context())
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !launched {
		t.Fatal("nothing launched with 2m left in the session; remaining window must not gate launches")
	}
	if want := cfg.SizeEstimates["medium"].MaxTimeout; gotTimeout != want {
		t.Errorf("timeout = %s, want the full %s — the session end must not truncate it", gotTimeout, want)
	}
}

// TestStatusSaysWorkersBusyNotQueueEmpty is the regression test for the status
// endpoint reporting "queue empty" while tasks sat waiting on a full worker pool.
func TestStatusSaysWorkersBusyNotQueueEmpty(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1

	running, _ := st.CreateTask("running", "p", "", "medium", "", "")
	waiting, _ := st.CreateTask("waiting", "p", "", "medium", "", "")

	sched := New(st, cfg, usage.NewClient("http://unused"), log.New("", false))
	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 29, ResetsAt: time.Now().Add(3 * time.Hour)},
		SevenDay: usage.Window{Utilization: 10},
	})
	sched.mu.Lock()
	sched.inflight[running.ID] = func(error) {}
	sched.mu.Unlock()

	status := sched.Status()

	if status.BlockedReasonCode == string(scheduling.ReasonQueueEmpty) {
		t.Fatal("status claimed queue empty while a task was waiting for a worker")
	}
	if status.BlockedReasonCode != string(scheduling.ReasonWorkersBusy) {
		t.Errorf("BlockedReasonCode = %q, want %q", status.BlockedReasonCode, scheduling.ReasonWorkersBusy)
	}
	if strings.Contains(status.BlockedReason, "window") || strings.Contains(status.BlockedReason, "resets") {
		t.Errorf("BlockedReason %q must not blame a session reset for a full worker pool", status.BlockedReason)
	}

	var waitingEntry *ForecastEntry
	for i := range status.Forecast {
		if status.Forecast[i].TaskID == waiting.ID {
			waitingEntry = &status.Forecast[i]
		}
	}
	if waitingEntry == nil {
		t.Fatalf("no forecast entry for the waiting task %d", waiting.ID)
	}
	if waitingEntry.ReasonCode != string(scheduling.ReasonWorkersBusy) {
		t.Errorf("waiting task reason = %q, want %q", waitingEntry.ReasonCode, scheduling.ReasonWorkersBusy)
	}
	if waitingEntry.Action != string(scheduling.ActionWait) {
		t.Errorf("waiting task action = %q, want %q", waitingEntry.Action, scheduling.ActionWait)
	}
}

// TestForecastEntriesCarryReasons checks the chips report the same verdict the
// daemon acts on, and that a ready task is not described as blocked.
func TestForecastEntriesCarryReasons(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	sched := New(st, testConfig(), usage.NewClient("http://unused"), log.New("", false))
	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 29, ResetsAt: time.Now().Add(3 * time.Hour)},
		SevenDay: usage.Window{Utilization: 10},
	})

	fc := sched.Forecast()
	if len(fc) != 1 {
		t.Fatalf("expected 1 forecast entry, got %d", len(fc))
	}
	if fc[0].TaskID != task.ID {
		t.Errorf("entry is for task %d, want %d", fc[0].TaskID, task.ID)
	}
	if fc[0].Action != string(scheduling.ActionLaunch) {
		t.Errorf("action = %q, want %q", fc[0].Action, scheduling.ActionLaunch)
	}
	if fc[0].Reason == "" {
		t.Error("entry carries no reason text")
	}

	status := sched.Status()
	if status.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want empty while work is launching", status.BlockedReason)
	}
	if status.NextCandidate != "task" {
		t.Errorf("NextCandidate = %q, want %q", status.NextCandidate, "task")
	}
}

// TestSpentSessionSuspendsRunningWorkers covers the pause-and-resume path: a
// spent session must stop in-flight workers with a suspend cause, so the run
// records itself as resumable for the next session rather than as a failure.
func TestSpentSessionSuspendsRunningWorkers(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.UtilThreshold = 80

	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	sched := New(st, cfg, usage.NewClient("http://unused"), log.New("", false))

	causes := make(chan error, 1)
	sched.mu.Lock()
	sched.inflight[task.ID] = func(err error) { causes <- err }
	sched.mu.Unlock()

	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 95, ResetsAt: time.Now().Add(30 * time.Minute)},
		SevenDay: usage.Window{Utilization: 10},
	})
	sched.evaluate(t.Context())

	select {
	case cause := <-causes:
		var susp *runner.ErrSuspended
		if !errors.As(cause, &susp) {
			t.Fatalf("cancellation cause = %v, want *runner.ErrSuspended", cause)
		}
		if susp.ResetAt.IsZero() {
			t.Error("suspension carries no reset time, so the run cannot record when it resumes")
		}
	case <-time.After(time.Second):
		t.Fatal("running worker was not suspended when the session was spent")
	}
}

// TestSpentWeekDoesNotSuspendRunningWorkers guards the asymmetry: a weekly limit
// stops new launches but must not throw away work already in flight.
func TestSpentWeekDoesNotSuspendRunningWorkers(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.SevenDayThreshold = 90

	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	sched := New(st, cfg, usage.NewClient("http://unused"), log.New("", false))
	cancelled := make(chan error, 1)
	sched.mu.Lock()
	sched.inflight[task.ID] = func(err error) { cancelled <- err }
	sched.mu.Unlock()

	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(3 * time.Hour)},
		SevenDay: usage.Window{Utilization: 95, ResetsAt: time.Now().Add(48 * time.Hour)},
	})
	sched.evaluate(t.Context())

	select {
	case <-cancelled:
		t.Fatal("a spent weekly limit must not suspend in-flight work")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestStaleUsageDataBlocksLaunches covers the gate that replaced tick()'s early
// return: with no fresh reading the daemon must not launch, but must still say
// why.
func TestStaleUsageDataBlocksLaunches(t *testing.T) {
	st := testStore(t)
	st.CreateTask("task", "p", "", "medium", "", "")

	launched := false
	sched := New(st, testConfig(), usage.NewClient("http://unused"), log.New("", false))
	sched.LaunchFunc = func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		launched = true
		return nil
	}

	stale := usage.Snapshot{
		CapturedAt: time.Now().Add(-30 * time.Minute),
		FiveHour:   usage.Window{Utilization: 20, ResetsAt: time.Now().Add(3 * time.Hour)},
	}
	withSnapshot(sched, stale)
	sched.evaluate(t.Context())
	time.Sleep(50 * time.Millisecond)

	if launched {
		t.Error("launched against a 30m-old usage reading")
	}
	if code := sched.Status().BlockedReasonCode; code != string(scheduling.ReasonStaleUsageData) {
		t.Errorf("BlockedReasonCode = %q, want %q", code, scheduling.ReasonStaleUsageData)
	}
}

// TestWeeklyBackoffLatchesUntilItsResetTime covers why the backoff is stored at
// all rather than re-derived each tick: the weekly reading can dip back under the
// threshold without the week having actually rolled over, and launching into that
// dip would immediately re-saturate.
func TestWeeklyBackoffLatchesUntilItsResetTime(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.SevenDayThreshold = 90

	st.CreateTask("task", "p", "", "medium", "", "")
	sched := New(st, cfg, usage.NewClient("http://unused"), log.New("", false))

	weeklyReset := time.Now().Add(24 * time.Hour)
	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(3 * time.Hour)},
		SevenDay: usage.Window{Utilization: 95, ResetsAt: weeklyReset},
	})
	sched.recordWeeklyBackoff()

	sched.mu.Lock()
	latched := sched.backoff
	sched.mu.Unlock()
	if latched == nil {
		t.Fatal("no backoff recorded when the weekly limit was spent")
	}
	if !latched.Equal(weeklyReset) {
		t.Errorf("backoff = %s, want the weekly reset %s", latched, weeklyReset)
	}

	// The reading dips back under the threshold before the week actually rolls
	// over. The latch must hold.
	withSnapshot(sched, usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(3 * time.Hour)},
		SevenDay: usage.Window{Utilization: 88, ResetsAt: weeklyReset},
	})
	sched.recordWeeklyBackoff()

	sched.mu.Lock()
	stillLatched := sched.backoff
	sched.mu.Unlock()
	if stillLatched == nil {
		t.Error("backoff cleared on a dip below the threshold; it must hold until the weekly reset")
	}

	// Once the recorded reset time has passed, it clears.
	past := time.Now().Add(-time.Minute)
	sched.mu.Lock()
	sched.backoff = &past
	sched.mu.Unlock()
	sched.recordWeeklyBackoff()

	sched.mu.Lock()
	cleared := sched.backoff
	sched.mu.Unlock()
	if cleared != nil {
		t.Errorf("backoff = %s, want nil once its reset time has passed", cleared)
	}
}

// TestStaleUsageDataStillReconciles guards the removal of tick()'s early return.
// A missing usage reading used to abort the whole tick, so orphaned runs went
// unreconciled for as long as the usage API kept rate-limiting us — which, in
// production, was hours.
func TestStaleUsageDataStillReconciles(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("task", "p", "", "medium", "", "")

	// A run the DB thinks is live, with a dead PID and a session to resume from.
	run, _ := st.CreateRun(task.ID, "", "", "", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-1")
	st.SetRunStatus(run.ID, "running")
	st.SetTaskStatus(task.ID, "running")

	sched := New(st, testConfig(), usage.NewClient("http://unused"), log.New("", false))
	withSnapshot(sched, usage.Snapshot{
		CapturedAt: time.Now().Add(-45 * time.Minute), // far too stale to schedule against
		FiveHour:   usage.Window{Utilization: 20, ResetsAt: time.Now().Add(3 * time.Hour)},
	})

	sched.reconcile()

	got, _ := st.LatestRunForTask(task.ID)
	if got.Status == "running" {
		t.Error("orphaned run left as running; reconciliation must not depend on fresh usage data")
	}
	updated, _ := st.GetTask(task.ID)
	if updated.Status != "resumable" {
		t.Errorf("task status = %q, want resumable (the run recorded a session)", updated.Status)
	}
}

// --- Evaluate tests ---

func TestEvaluateLaunchOrder(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 2

	var mu sync.Mutex
	var launched []int64

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		launched = append(launched, task.ID)
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	t1, _ := st.CreateTask("first", "p1", "", "medium", "", "")
	t2, _ := st.CreateTask("second", "p2", "", "medium", "", "")
	t3, _ := st.CreateTask("third", "p3", "", "medium", "", "")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}

	withSnapshot(sched, snap)
	sched.evaluate(t.Context())

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(launched) != 2 {
		t.Fatalf("expected 2 launches, got %d", len(launched))
	}
	// Must launch the top-2 by sort_order (t1, t2), never the third.
	got := map[int64]bool{}
	for _, id := range launched {
		got[id] = true
	}
	if !got[t1.ID] || !got[t2.ID] {
		t.Fatalf("expected top-2 tasks %d,%d launched, got %v", t1.ID, t2.ID, launched)
	}
	if got[t3.ID] {
		t.Fatalf("third task %d should not launch when N=2, got %v", t3.ID, launched)
	}
}

func TestEvaluateResumeFirst(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1

	var mu sync.Mutex
	var launchedTask int64
	var wasResume bool

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		launchedTask = task.ID
		wasResume = resume != nil
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	queued, _ := st.CreateTask("queued-task", "p1", "", "medium", "", "")
	resumeTask, _ := st.CreateTask("resume-task", "p2", "", "medium", "", "")

	_ = queued

	st.SetTaskStatus(resumeTask.ID, "resumable")
	r, _ := st.CreateRun(resumeTask.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunSessionID(r.ID, "sess-123")
	st.SetRunStatus(r.ID, "rate_limited")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}

	withSnapshot(sched, snap)
	sched.evaluate(t.Context())

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if launchedTask != resumeTask.ID {
		t.Fatalf("expected resumable task %d to launch first, got %d", resumeTask.ID, launchedTask)
	}
	if !wasResume {
		t.Fatal("expected resume=true")
	}
}

func TestEvaluateSaturatedNoLaunch(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.UtilThreshold = 80

	st.CreateTask("task", "p", "", "medium", "", "")

	launched := false
	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		launched = true
		return nil
	}

	logger := log.New("", false)
	client := usage.NewClient("http://unused")

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 85, ResetsAt: time.Now().Add(3 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}

	ctx := context.Background()
	withSnapshot(sched, snap)
	sched.evaluate(ctx)

	if launched {
		t.Fatal("should not launch when utilization >= threshold")
	}
}

func TestEvaluateNCapRespected(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1

	var mu sync.Mutex
	var launchCount int

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		launchCount++
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	st.CreateTask("A", "p", "", "medium", "", "")
	st.CreateTask("B", "p", "", "medium", "", "")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}

	withSnapshot(sched, snap)
	sched.evaluate(t.Context())
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if launchCount != 1 {
		t.Fatalf("expected 1 launch (N=1), got %d", launchCount)
	}
}

func TestEvaluateAttemptCap(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1
	cfg.MaxAttempts = 3

	launched := false
	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		launched = true
		return nil
	}

	task, _ := st.CreateTask("capped-task", "prompt", "", "medium", "", "")
	st.SetTaskStatus(task.ID, "resumable")
	r, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 3)
	st.SetRunSessionID(r.ID, "sess-123")
	st.SetRunStatus(r.ID, "rate_limited")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}

	withSnapshot(sched, snap)
	sched.evaluate(t.Context())
	time.Sleep(50 * time.Millisecond)

	if launched {
		t.Fatal("should not launch when attempt >= max_attempts")
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "failed" {
		t.Fatalf("expected task status=failed, got %s", got.Status)
	}
}

func TestEvaluateHoldsAtMeasuredUtil100(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.UtilThreshold = 100

	st.CreateTask("task", "p", "", "medium", "", "")

	launched := false
	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		launched = true
		return nil
	}

	logger := log.New("", false)
	client := usage.NewClient("http://unused")

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 100, ResetsAt: time.Now().Add(3 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}

	withSnapshot(sched, snap)
	sched.evaluate(t.Context())

	if launched {
		t.Fatal("should not launch when measured util >= 100 even with threshold=100")
	}
}

func TestReconcileKillsOrphan(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()

	cmd := exec.Command("sleep", "300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	orphanPID := cmd.Process.Pid
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	// Override processIsClaude to treat the sleep process as "claude" for this test.
	orig := ProcessIsClaude
	ProcessIsClaude = func(pid int) bool { return true }
	t.Cleanup(func() { ProcessIsClaude = orig })

	task, _ := st.CreateTask("orphan-task", "p", "", "medium", "", "")
	st.SetTaskStatus(task.ID, "running")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunPID(run.ID, orphanPID)
	st.SetRunSessionID(run.ID, "sess-orphan")
	st.SetRunStatus(run.ID, "running")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)

	sched.reconcile()

	cmd.Wait()

	gotRun, _ := st.LatestRunForTask(task.ID)
	if gotRun.Status != "errored" {
		t.Fatalf("expected run status=errored, got %s", gotRun.Status)
	}
	if gotRun.Error != "orphaned claude from previous daemon killed; will resume" {
		t.Fatalf("expected orphan-kill error text, got %q", gotRun.Error)
	}

	gotTask, _ := st.GetTask(task.ID)
	if gotTask.Status != "resumable" {
		t.Fatalf("expected task status=resumable (has session), got %s", gotTask.Status)
	}
}

func TestReconcileRecycledPidNotKilled(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()

	cmd := exec.Command("sleep", "300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	recycledPID := cmd.Process.Pid
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	// processIsClaude returns false — pid was recycled to a non-claude process.
	orig := ProcessIsClaude
	ProcessIsClaude = func(pid int) bool { return false }
	t.Cleanup(func() { ProcessIsClaude = orig })

	task, _ := st.CreateTask("recycled-pid-task", "p", "", "medium", "", "")
	st.SetTaskStatus(task.ID, "running")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunPID(run.ID, recycledPID)
	st.SetRunSessionID(run.ID, "sess-recycled")
	st.SetRunStatus(run.ID, "running")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)

	sched.reconcile()

	// The process should still be alive — we must NOT have killed it.
	if !processAlive(recycledPID) {
		t.Fatal("recycled pid should not have been killed")
	}

	gotRun, _ := st.LatestRunForTask(task.ID)
	if gotRun.Status != "errored" {
		t.Fatalf("expected run status=errored, got %s", gotRun.Status)
	}
	if gotRun.Error != "daemon lost track (pid recycled)" {
		t.Fatalf("expected recycled-pid error text, got %q", gotRun.Error)
	}

	gotTask, _ := st.GetTask(task.ID)
	if gotTask.Status != "resumable" {
		t.Fatalf("expected task status=resumable (has session), got %s", gotTask.Status)
	}
}

func TestReconcileDeadPid(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()

	task, _ := st.CreateTask("dead-task", "p", "", "medium", "", "")
	st.SetTaskStatus(task.ID, "running")
	run, _ := st.CreateRun(task.ID, "/wt", "branch", "/repo", "w1", 1)
	st.SetRunPID(run.ID, 999999999)
	st.SetRunStatus(run.ID, "running")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)

	sched.reconcile()

	gotRun, _ := st.LatestRunForTask(task.ID)
	if gotRun.Status != "errored" {
		t.Fatalf("expected run status=errored, got %s", gotRun.Status)
	}

	gotTask, _ := st.GetTask(task.ID)
	if gotTask.Status != "failed" {
		t.Fatalf("expected task status=failed (no session), got %s", gotTask.Status)
	}
}

func TestStatusPopulated(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 2

	t1, _ := st.CreateTask("task-A", "p1", "", "medium", "", "")
	t2, _ := st.CreateTask("task-B", "p2", "", "medium", "", "")

	r1, _ := st.CreateRun(t1.ID, "/wt1", "branch1", "/repo", "w1", 1)
	st.SetRunStatus(r1.ID, "running")
	r2, _ := st.CreateRun(t2.ID, "/wt2", "branch2", "/repo", "w1", 1)
	st.SetRunStatus(r2.ID, "running")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	withSnapshot(sched, snap)
	sched.mu.Lock()
	sched.inflight[t1.ID] = func(error) {}
	sched.inflight[t2.ID] = func(error) {}
	sched.mu.Unlock()

	status := sched.Status()
	if status.RunningCount != 2 {
		t.Fatalf("expected RunningCount=2, got %d", status.RunningCount)
	}
	if len(status.RunningRuns) != 2 {
		t.Fatalf("expected 2 RunningRuns, got %d", len(status.RunningRuns))
	}
	for _, rr := range status.RunningRuns {
		if rr.RunID == 0 {
			t.Error("RunningRun has zero RunID")
		}
		if rr.Title == "" {
			t.Error("RunningRun has empty Title")
		}
	}

	// A full worker pool with nothing waiting is not a blocked state: both tasks
	// are progressing and the queue is empty. Saturation is only worth reporting
	// when it is actually holding something back — see
	// TestStatusSaysWorkersBusyNotQueueEmpty.
	if status.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want empty: the pool is busy but nothing is waiting", status.BlockedReason)
	}
}

func TestStatusSevenDayResetsAt(t *testing.T) {
	st := testStore(t)
	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, testConfig(), client, logger)

	// No snapshot yet: the field stays absent rather than reporting a zero time.
	if got := sched.Status().SevenDayResetsAt; got != nil {
		t.Fatalf("expected nil SevenDayResetsAt before first snapshot, got %v", got)
	}

	sevenReset := time.Now().Add(3 * 24 * time.Hour).UTC()
	sched.mu.Lock()
	sched.lastSnap = &usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30, ResetsAt: sevenReset},
	}
	sched.mu.Unlock()

	got := sched.Status().SevenDayResetsAt
	if got == nil {
		t.Fatal("expected SevenDayResetsAt to be populated from the snapshot")
	}
	if !got.Equal(sevenReset) {
		t.Fatalf("expected SevenDayResetsAt=%v, got %v", sevenReset, *got)
	}

	// A snapshot without a 7d reset time must not surface a zero timestamp.
	sched.mu.Lock()
	sched.lastSnap = &usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	sched.mu.Unlock()

	if got := sched.Status().SevenDayResetsAt; got != nil {
		t.Fatalf("expected nil SevenDayResetsAt for zero-valued snapshot, got %v", got)
	}
}

// --- Forecast tests ---

// --- Run-now tests ---

func TestRunNowBypassesSaturation(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.UtilThreshold = 80

	var mu sync.Mutex
	var launchedTask int64

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		launchedTask = task.ID
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	task, _ := st.CreateTask("run-now-task", "prompt", "", "medium", "", "")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 95, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.mu.Unlock()

	ctx := t.Context()

	// Normal evaluate should NOT launch (util 95 >= threshold 80)
	withSnapshot(sched, snap)
	sched.evaluate(ctx)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if launchedTask != 0 {
		mu.Unlock()
		t.Fatal("evaluate should not launch at 95% util")
	}
	mu.Unlock()

	// RunNow should bypass saturation
	if err := sched.RunNow(ctx, task.ID); err != nil {
		t.Fatalf("RunNow failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if launchedTask != task.ID {
		t.Fatalf("expected RunNow to launch task %d, got %d", task.ID, launchedTask)
	}
}

func TestRunNowDoubleLaunchGuard(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		<-ctx.Done()
		return nil
	}

	task, _ := st.CreateTask("task", "prompt", "", "medium", "", "")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 10, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 20},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.mu.Unlock()

	ctx := t.Context()

	if err := sched.RunNow(ctx, task.ID); err != nil {
		t.Fatalf("first RunNow failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	err := sched.RunNow(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error on double launch")
	}
}

func TestRunNowWrongStatus(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()

	task, _ := st.CreateTask("task", "prompt", "", "medium", "", "")
	st.SetTaskStatus(task.ID, "done")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 10, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 20},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.mu.Unlock()

	err := sched.RunNow(t.Context(), task.ID)
	if err == nil {
		t.Fatal("expected error for non-queued task")
	}
}

func TestDataDirSmokeSetup(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("BURNRATE_DATA_DIR", dir)
	defer os.Unsetenv("BURNRATE_DATA_DIR")

	dataDir := config.DataDir()
	if err := config.EnsureDirs(dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "logs")); err != nil {
		t.Fatalf("logs dir not created: %v", err)
	}
}

// A dropdown selection (SetAccount) must reach the very next launch: the
// scheduler snapshots the live cfg at launch time, not a startup copy.
func TestEvaluateUsesLiveAccountSelection(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1
	cfg.ClaudeConfigDir = "/startup/.claude"

	var mu sync.Mutex
	var gotConfigDir string

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		gotConfigDir = cfg.ClaudeConfigDir
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	st.CreateTask("t1", "p1", "", "medium", "", "")

	sched := New(st, cfg, usage.NewClient("http://unused"), log.New("", false))
	sched.LaunchFunc = fakeLaunch

	// Simulate a UI account switch after startup.
	sched.SetAccount("/selected/.claude", "", "")

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	withSnapshot(sched, snap)
	sched.evaluate(t.Context())
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if gotConfigDir != "/selected/.claude" {
		t.Fatalf("launch used stale account %q, expected the live selection", gotConfigDir)
	}
}

func TestBacklogTaskNeverLaunched(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 3

	launched := false
	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		launched = true
		<-ctx.Done()
		return nil
	}

	st.CreateTask("backlog-task", "p", "", "medium", "", "backlog")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 10, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 20},
	}

	withSnapshot(sched, snap)
	sched.evaluate(t.Context())
	time.Sleep(100 * time.Millisecond)

	if launched {
		t.Fatal("backlog task should never be launched by the scheduler")
	}
}

func TestBacklogPromotedToQueuedLaunches(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1

	var mu sync.Mutex
	var launchedTask int64

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		launchedTask = task.ID
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	task, _ := st.CreateTask("backlog-task", "p", "", "medium", "", "backlog")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 10, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 20},
	}

	// First evaluate: backlog task should NOT launch
	withSnapshot(sched, snap)
	sched.evaluate(t.Context())
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if launchedTask != 0 {
		mu.Unlock()
		t.Fatal("backlog task should not launch before promotion")
	}
	mu.Unlock()

	// Promote to queued
	st.SetTaskStatus(task.ID, "queued")

	// Second evaluate: now it should launch
	withSnapshot(sched, snap)
	sched.evaluate(t.Context())
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if launchedTask != task.ID {
		t.Fatalf("expected promoted task %d to launch, got %d", task.ID, launchedTask)
	}
}

func TestStatusTransitionValidation(t *testing.T) {
	// Valid transitions — from any non-running status to allowed targets
	valid := []struct{ from, to string }{
		{"backlog", "queued"},
		{"backlog", "done"},
		{"backlog", "dismissed"},
		{"backlog", "paused"},
		{"queued", "backlog"},
		{"queued", "paused"},
		{"queued", "done"},
		{"queued", "dismissed"},
		{"paused", "queued"},
		{"paused", "backlog"},
		{"paused", "done"},
		{"paused", "dismissed"},
		{"resumable", "backlog"},
		{"resumable", "paused"},
		{"resumable", "done"},
		{"resumable", "dismissed"},
		{"resumable", "queued"},
		{"failed", "queued"},
		{"failed", "backlog"},
		{"failed", "dismissed"},
		{"failed", "paused"},
		{"done", "queued"},
		{"done", "backlog"},
		{"dismissed", "queued"},
	}
	for _, tc := range valid {
		if err := store.ValidateStatusTransition(tc.from, tc.to); err != nil {
			t.Errorf("expected valid transition %s→%s, got error: %v", tc.from, tc.to, err)
		}
	}

	// Invalid transitions — running can't change, pr_created/running not user-settable
	invalid := []struct{ from, to string }{
		{"running", "backlog"},
		{"running", "queued"},
		{"running", "done"},
		{"backlog", "running"},
		{"queued", "pr_created"},
		{"done", "pr_created"},
		{"failed", "running"},
	}
	for _, tc := range invalid {
		if err := store.ValidateStatusTransition(tc.from, tc.to); err == nil {
			t.Errorf("expected invalid transition %s→%s to be rejected", tc.from, tc.to)
		}
	}
}

// --- windowDelta tests ---

// --- Observe fresh-window tests ---

// --- Evaluate with fresh window ---

func TestEvaluateFreshWindowLaunches(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.ParallelN = 1

	var mu sync.Mutex
	var launchedTask int64

	fakeLaunch := func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		mu.Lock()
		launchedTask = task.ID
		mu.Unlock()
		<-ctx.Done()
		return nil
	}

	task, _ := st.CreateTask("fresh-window-task", "p", "", "medium", "", "")

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = fakeLaunch

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 0, ResetsAt: time.Time{}},
		SevenDay: usage.Window{Utilization: 10},
	}

	withSnapshot(sched, snap)
	sched.evaluate(t.Context())
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if launchedTask != task.ID {
		t.Fatalf("expected task %d to launch with fresh (zero-ResetsAt) window, got %d", task.ID, launchedTask)
	}
}

// waitForFetchCount polls until fetchCount reaches at least the target value,
// or the timeout expires.
func waitForFetchCount(mu *sync.Mutex, fetchCount *int, target int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return false
		case <-time.After(50 * time.Millisecond):
			mu.Lock()
			n := *fetchCount
			mu.Unlock()
			if n >= target {
				return true
			}
		}
	}
}

func newFakeUsageServer(mu *sync.Mutex, fetchCount *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*fetchCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{"utilization": 10.0, "resets_at": time.Now().Add(4 * time.Hour).Format(time.RFC3339)},
			"seven_day": map[string]any{"utilization": 20.0, "resets_at": time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)},
		})
	}))
}

func TestRefreshNowTriggersImmediateFetch(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.PollIntervalSec = 3600 // won't tick during the test

	var mu sync.Mutex
	var fetchCount int

	srv := newFakeUsageServer(&mu, &fetchCount)
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		<-ctx.Done()
		return nil
	}

	ctx := t.Context()

	go sched.Start(ctx)

	// Wait for startup fetch (claude --version may be slow)
	if !waitForFetchCount(&mu, &fetchCount, 1, 10*time.Second) {
		t.Fatal("timed out waiting for startup fetch")
	}

	mu.Lock()
	startCount := fetchCount
	mu.Unlock()

	// The startup tick records lastFetchAt *after* the fake server has seen the
	// request, so the wait above can return while that write is still pending.
	// Clearing the debounce once would then be overwritten by the in-flight tick
	// and the refresh legitimately suppressed — a rare flake under load. Keep
	// clearing it and re-asking until the fetch lands.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sched.mu.Lock()
		sched.lastFetchAt = time.Time{}
		sched.mu.Unlock()

		sched.RequestRefresh()

		if waitForFetchCount(&mu, &fetchCount, startCount+1, 250*time.Millisecond) {
			return
		}
	}

	mu.Lock()
	endCount := fetchCount
	mu.Unlock()
	t.Fatalf("expected refresh to trigger another fetch; before=%d, after=%d", startCount, endCount)
}

func TestRefreshNowDebouncesRapidMutations(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.PollIntervalSec = 3600

	var mu sync.Mutex
	var fetchCount int

	srv := newFakeUsageServer(&mu, &fetchCount)
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)

	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		<-ctx.Done()
		return nil
	}

	ctx := t.Context()

	go sched.Start(ctx)

	// Wait for startup fetch (claude --version may be slow)
	if !waitForFetchCount(&mu, &fetchCount, 1, 10*time.Second) {
		t.Fatal("timed out waiting for startup fetch")
	}

	mu.Lock()
	startCount := fetchCount
	mu.Unlock()

	// Fire multiple rapid refreshes — should only cause one additional fetch
	// because the debounce window is 20s and the channel is buffered at 1
	for range 5 {
		sched.RequestRefresh()
	}

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	endCount := fetchCount
	mu.Unlock()

	// Should have at most 1 additional fetch (the first refresh), not 5
	additional := endCount - startCount
	if additional > 1 {
		t.Fatalf("debounce failed: expected at most 1 additional fetch after rapid refreshes, got %d", additional)
	}
}

// --- Rate limit backoff tests ---

func noJitter(d time.Duration) time.Duration { return d }

func TestNextRateLimitDelay(t *testing.T) {
	old := jitterFn
	jitterFn = noJitter
	defer func() { jitterFn = old }()

	d := nextRateLimitDelay(0, 0)
	if d != 30*time.Second {
		t.Fatalf("initial delay: got %s, want 30s", d)
	}
	d = nextRateLimitDelay(30*time.Second, 0)
	if d != 60*time.Second {
		t.Fatalf("second delay: got %s, want 60s", d)
	}
	d = nextRateLimitDelay(60*time.Second, 0)
	if d != 120*time.Second {
		t.Fatalf("third delay: got %s, want 120s", d)
	}
	d = nextRateLimitDelay(4*time.Minute, 0)
	if d != 8*time.Minute {
		t.Fatalf("fourth delay: got %s, want 8m", d)
	}
	d = nextRateLimitDelay(8*time.Minute, 0)
	if d != 10*time.Minute {
		t.Fatalf("should cap at 10m: got %s", d)
	}
	d = nextRateLimitDelay(10*time.Minute, 0)
	if d != 10*time.Minute {
		t.Fatalf("should stay at 10m cap: got %s", d)
	}
}

func TestNextRateLimitDelayRetryAfter(t *testing.T) {
	old := jitterFn
	jitterFn = noJitter
	defer func() { jitterFn = old }()

	d := nextRateLimitDelay(0, 90*time.Second)
	if d != 90*time.Second {
		t.Fatalf("retry-after should override initial: got %s, want 1m30s", d)
	}
	d = nextRateLimitDelay(30*time.Second, 45*time.Second)
	if d != 60*time.Second {
		t.Fatalf("exponential should win when larger: got %s, want 1m0s", d)
	}
	d = nextRateLimitDelay(0, 20*time.Minute)
	if d != 10*time.Minute {
		t.Fatalf("retry-after should be capped at max: got %s, want 10m", d)
	}
}

func TestNextRateLimitDelayJitter(t *testing.T) {
	old := jitterFn
	jitterFn = defaultJitter
	defer func() { jitterFn = old }()

	base := 60 * time.Second
	lo := base - time.Duration(float64(base)*0.1)
	hi := base + time.Duration(float64(base)*0.1)
	for i := 0; i < 50; i++ {
		d := nextRateLimitDelay(30*time.Second, 0)
		if d < lo || d > hi {
			t.Fatalf("jittered delay %s out of range [%s, %s]", d, lo, hi)
		}
	}
}

func TestTick429BackoffSkipsFetch(t *testing.T) {
	old := jitterFn
	jitterFn = noJitter
	defer func() { jitterFn = old }()

	st := testStore(t)
	cfg := testConfig()
	cfg.PollIntervalSec = 3600

	var mu sync.Mutex
	var fetchCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetchCount++
		mu.Unlock()
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)
	sched := New(st, cfg, client, logger)

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.lastFetchAt = time.Now()
	sched.mu.Unlock()

	ctx := context.Background()

	sched.tick(ctx, false)

	mu.Lock()
	count1 := fetchCount
	mu.Unlock()
	if count1 != 1 {
		t.Fatalf("expected 1 fetch, got %d", count1)
	}

	sched.mu.Lock()
	if sched.rateLimitDelay != 30*time.Second {
		t.Fatalf("expected 30s backoff delay, got %s", sched.rateLimitDelay)
	}
	if sched.rateLimitBackoffUntil.IsZero() {
		t.Fatal("expected backoff until to be set")
	}
	if sched.rateLimitConsecutive != 1 {
		t.Fatalf("expected consecutive=1, got %d", sched.rateLimitConsecutive)
	}
	sched.mu.Unlock()

	// Second tick: should skip fetch (in backoff)
	sched.tick(ctx, false)

	mu.Lock()
	count2 := fetchCount
	mu.Unlock()
	if count2 != 1 {
		t.Fatalf("expected fetch to be skipped during backoff, got %d total fetches", count2)
	}
}

func TestTick429BackoffClearsOnSuccess(t *testing.T) {
	old := jitterFn
	jitterFn = noJitter
	defer func() { jitterFn = old }()

	st := testStore(t)
	cfg := testConfig()

	var mu sync.Mutex
	var fetchCount int
	returnSuccess := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetchCount++
		success := returnSuccess
		mu.Unlock()
		if !success {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{"utilization": 15.0, "resets_at": time.Now().Add(4 * time.Hour).Format(time.RFC3339)},
			"seven_day": map[string]any{"utilization": 10.0, "resets_at": time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)},
		})
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)
	sched := New(st, cfg, client, logger)

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.lastFetchAt = time.Now()
	sched.mu.Unlock()

	ctx := context.Background()

	// First tick: 429
	sched.tick(ctx, false)

	sched.mu.Lock()
	if sched.rateLimitDelay == 0 {
		t.Fatal("expected backoff after 429")
	}
	if sched.rateLimitConsecutive != 1 {
		t.Fatalf("expected consecutive=1, got %d", sched.rateLimitConsecutive)
	}
	sched.rateLimitBackoffUntil = time.Time{}
	sched.mu.Unlock()

	mu.Lock()
	returnSuccess = true
	mu.Unlock()

	// Next tick: success should clear all backoff state
	sched.tick(ctx, false)

	sched.mu.Lock()
	if sched.rateLimitDelay != 0 {
		t.Fatalf("expected backoff cleared after success, got %s", sched.rateLimitDelay)
	}
	if !sched.rateLimitBackoffUntil.IsZero() {
		t.Fatal("expected backoff until cleared after success")
	}
	if sched.rateLimitConsecutive != 0 {
		t.Fatalf("expected consecutive cleared after success, got %d", sched.rateLimitConsecutive)
	}
	sched.mu.Unlock()
}

func TestTick429RetryAfterHeader(t *testing.T) {
	old := jitterFn
	jitterFn = noJitter
	defer func() { jitterFn = old }()

	st := testStore(t)
	cfg := testConfig()
	cfg.PollIntervalSec = 3600

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)
	sched := New(st, cfg, client, logger)

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.lastFetchAt = time.Now()
	sched.mu.Unlock()

	sched.tick(context.Background(), false)

	sched.mu.Lock()
	if sched.rateLimitDelay != 120*time.Second {
		t.Fatalf("expected 120s from Retry-After, got %s", sched.rateLimitDelay)
	}
	sched.mu.Unlock()
}

func TestTick429ConsecutiveEscalation(t *testing.T) {
	old := jitterFn
	jitterFn = noJitter
	defer func() { jitterFn = old }()

	st := testStore(t)
	cfg := testConfig()
	cfg.PollIntervalSec = 3600

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)
	sched := New(st, cfg, client, logger)

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.lastFetchAt = time.Now()
	sched.mu.Unlock()

	ctx := context.Background()
	expectedDelays := []time.Duration{
		30 * time.Second, 60 * time.Second, 120 * time.Second,
		240 * time.Second, 480 * time.Second, 10 * time.Minute,
	}
	for i, want := range expectedDelays {
		sched.mu.Lock()
		sched.rateLimitBackoffUntil = time.Time{}
		sched.mu.Unlock()

		sched.tick(ctx, false)

		sched.mu.Lock()
		got := sched.rateLimitDelay
		n := sched.rateLimitConsecutive
		sched.mu.Unlock()

		if got != want {
			t.Fatalf("attempt %d: got %s, want %s", i+1, got, want)
		}
		if n != i+1 {
			t.Fatalf("attempt %d: consecutive=%d, want %d", i+1, n, i+1)
		}
	}
}

func TestIdleThrottleSkipsFetch(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.PollIntervalSec = 3600

	var mu sync.Mutex
	var fetchCount int

	srv := newFakeUsageServer(&mu, &fetchCount)
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)
	sched := New(st, cfg, client, logger)

	ctx := context.Background()

	// First tick: no idle fetch recorded yet, should fetch
	sched.tick(ctx, false)

	mu.Lock()
	count1 := fetchCount
	mu.Unlock()
	if count1 != 1 {
		t.Fatalf("expected 1 fetch on first tick, got %d", count1)
	}

	// Second tick: scheduler is idle (no inflight, no candidates) and just fetched
	sched.tick(ctx, false)

	mu.Lock()
	count2 := fetchCount
	mu.Unlock()
	if count2 != 1 {
		t.Fatalf("expected idle throttle to skip fetch, got %d total fetches", count2)
	}

	// Force refresh should bypass idle throttle
	sched.tick(ctx, true)

	mu.Lock()
	count3 := fetchCount
	mu.Unlock()
	if count3 != 2 {
		t.Fatalf("expected force refresh to bypass idle throttle, got %d total fetches", count3)
	}
}

func TestIdleThrottleAllowsFetchWithCandidates(t *testing.T) {
	st := testStore(t)
	cfg := testConfig()
	cfg.PollIntervalSec = 3600

	var mu sync.Mutex
	var fetchCount int

	srv := newFakeUsageServer(&mu, &fetchCount)
	defer srv.Close()

	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	logger := log.New("", false)
	client := usage.NewClient(srv.URL)
	sched := New(st, cfg, client, logger)
	sched.LaunchFunc = func(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params runner.Params, logger *log.Logger) error {
		<-ctx.Done()
		return nil
	}

	ctx := t.Context()

	// First tick: fetch succeeds, sets idle fetch timestamp
	sched.tick(ctx, false)

	mu.Lock()
	count1 := fetchCount
	mu.Unlock()
	if count1 != 1 {
		t.Fatalf("expected 1 fetch, got %d", count1)
	}

	// Add a queued task — idle throttle should NOT apply
	st.CreateTask("candidate", "p", "", "medium", "", "")

	sched.tick(ctx, false)

	mu.Lock()
	count2 := fetchCount
	mu.Unlock()
	if count2 != 2 {
		t.Fatalf("expected fetch when candidates exist, got %d total fetches", count2)
	}
}

func TestStatusRateLimitFields(t *testing.T) {
	old := jitterFn
	jitterFn = noJitter
	defer func() { jitterFn = old }()

	st := testStore(t)
	cfg := testConfig()

	logger := log.New("", false)
	client := usage.NewClient("http://unused")
	sched := New(st, cfg, client, logger)

	snap := usage.Snapshot{
		FiveHour: usage.Window{Utilization: 20, ResetsAt: time.Now().Add(4 * time.Hour)},
		SevenDay: usage.Window{Utilization: 30},
	}
	sched.mu.Lock()
	sched.lastSnap = &snap
	sched.mu.Unlock()

	// No rate limiting: fields should be empty
	status := sched.Status()
	if status.RateLimitUntil != nil {
		t.Fatal("expected nil RateLimitUntil when not rate limited")
	}
	if status.RateLimitAttempt != 0 {
		t.Fatalf("expected 0 RateLimitAttempt, got %d", status.RateLimitAttempt)
	}

	// Simulate rate limiting
	sched.mu.Lock()
	sched.rateLimitBackoffUntil = time.Now().Add(30 * time.Second)
	sched.rateLimitConsecutive = 3
	sched.mu.Unlock()

	status = sched.Status()
	if status.RateLimitUntil == nil {
		t.Fatal("expected RateLimitUntil to be set during rate limiting")
	}
	if status.RateLimitAttempt != 3 {
		t.Fatalf("expected RateLimitAttempt=3, got %d", status.RateLimitAttempt)
	}
}
