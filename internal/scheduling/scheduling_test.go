package scheduling

import (
	"testing"
	"time"
)

// Fixed clock. Nothing in this package reads time.Now(), so every case is
// deterministic and none of them sleep.
var now = time.Date(2026, 7, 25, 15, 52, 0, 0, time.UTC)

func testPolicy() Policy {
	return Policy{
		Workers:          3,
		SessionThreshold: 100,
		WeeklyThreshold:  95,
		MaxAttempts:      5,
		RunTimeout:       75 * time.Minute,
		MaxSnapshotAge:   15 * time.Minute,
	}
}

// healthy is a reading with plenty of quota left in both windows.
func healthy() *Snapshot {
	return &Snapshot{
		CapturedAt:      now.Add(-10 * time.Second),
		SessionUtil:     29,
		SessionResetsAt: now.Add(3 * time.Hour),
		WeeklyUtil:      10,
		WeeklyResetsAt:  now.Add(5 * 24 * time.Hour),
	}
}

func queued(ids ...int64) []Candidate {
	var out []Candidate
	for _, id := range ids {
		out = append(out, Candidate{TaskID: id})
	}
	return out
}

func input(candidates []Candidate, running ...int64) Input {
	return Input{
		Now:        now,
		Snapshot:   healthy(),
		Policy:     testPolicy(),
		Candidates: candidates,
		Running:    running,
	}
}

func actions(p Plan) map[int64]Action {
	out := map[int64]Action{}
	for _, d := range p.Decisions {
		out[d.TaskID] = d.Action
	}
	return out
}

func mustDecision(t *testing.T, p Plan, taskID int64) Decision {
	t.Helper()
	d, ok := p.For(taskID)
	if !ok {
		t.Fatalf("no decision for task %d", taskID)
	}
	return d
}

// --- launching ---

func TestLaunchesUntilWorkersSaturated(t *testing.T) {
	p := Decide(input(queued(1, 2, 3, 4, 5)))

	if got := len(p.Launches()); got != 3 {
		t.Fatalf("launched %d, want 3 (Workers)", got)
	}
	for _, id := range []int64{1, 2, 3} {
		if d := mustDecision(t, p, id); d.Action != ActionLaunch {
			t.Errorf("task %d: action %q, want launch", id, d.Action)
		}
	}
	for _, id := range []int64{4, 5} {
		d := mustDecision(t, p, id)
		if d.Action != ActionWait {
			t.Errorf("task %d: action %q, want wait", id, d.Action)
		}
		if d.Reason.Code != ReasonWorkersBusy {
			t.Errorf("task %d: reason %q, want %q", id, d.Reason.Code, ReasonWorkersBusy)
		}
	}
}

func TestRunningWorkersConsumeSlots(t *testing.T) {
	p := Decide(input(queued(4, 5, 6), 1, 2))

	if got := len(p.Launches()); got != 1 {
		t.Fatalf("launched %d, want 1 (3 workers - 2 running)", got)
	}
	if d := mustDecision(t, p, 4); d.Action != ActionLaunch {
		t.Errorf("task 4: action %q, want launch", d.Action)
	}
}

func TestLaunchTimeoutIsFlat(t *testing.T) {
	in := input(queued(1))
	in.Policy.RunTimeout = 75 * time.Minute
	p := Decide(in)

	if d := mustDecision(t, p, 1); d.Timeout != 75*time.Minute {
		t.Fatalf("timeout %s, want 75m", d.Timeout)
	}
}

func TestCandidateOrderIsPriorityOrder(t *testing.T) {
	// Resumable work is handed to us first; with one slot it must win.
	in := input([]Candidate{
		{TaskID: 9, Resume: true, Attempt: 1},
		{TaskID: 1},
	}, 100, 101)
	p := Decide(in)

	launches := p.Launches()
	if len(launches) != 1 || launches[0].TaskID != 9 {
		t.Fatalf("launches = %+v, want only task 9", launches)
	}
}

// TestEndOfSessionStillLaunches is the regression test for the dead zone that
// softlocked the daemon for the last 10 minutes of every session. Remaining
// session time is not an input to the decision: a run may straddle a reset, so
// a session about to end must still launch work at the full timeout.
func TestEndOfSessionStillLaunches(t *testing.T) {
	in := input(queued(1))
	in.Snapshot.SessionResetsAt = now.Add(2 * time.Minute)
	p := Decide(in)

	d := mustDecision(t, p, 1)
	if d.Action != ActionLaunch {
		t.Fatalf("action %q with 2m left in session, want launch (runway is not a gate)", d.Action)
	}
	if d.Timeout != testPolicy().RunTimeout {
		t.Errorf("timeout %s, want the full %s — the session end must not truncate it",
			d.Timeout, testPolicy().RunTimeout)
	}
	if p.Global.Blocking() {
		t.Errorf("Global = %+v, want non-blocking", p.Global)
	}
}

func TestElapsedSessionResetStillLaunches(t *testing.T) {
	in := input(queued(1))
	in.Snapshot.SessionResetsAt = now.Add(-1 * time.Minute)
	p := Decide(in)

	if d := mustDecision(t, p, 1); d.Action != ActionLaunch {
		t.Fatalf("action %q with an elapsed reset time, want launch", d.Action)
	}
}

// --- session limit: suspend running work ---

func TestSessionExhaustedSuspendsRunningWorkers(t *testing.T) {
	in := input(queued(4), 1, 2, 3)
	in.Snapshot.SessionUtil = 100
	p := Decide(in)

	if len(p.Suspend) != 3 {
		t.Fatalf("suspend = %+v, want all 3 running workers", p.Suspend)
	}
	for _, d := range p.Suspend {
		if d.Action != ActionSuspend {
			t.Errorf("task %d: action %q, want suspend", d.TaskID, d.Action)
		}
		if d.Reason.Code != ReasonSessionExhausted {
			t.Errorf("task %d: reason %q, want %q", d.TaskID, d.Reason.Code, ReasonSessionExhausted)
		}
	}
	if got := len(p.Launches()); got != 0 {
		t.Errorf("launched %d with the session spent, want 0", got)
	}
	if p.Global.Code != ReasonSessionExhausted {
		t.Errorf("Global = %q, want %q", p.Global.Code, ReasonSessionExhausted)
	}
	if p.WindowState != WindowSaturated {
		t.Errorf("WindowState = %q, want %q", p.WindowState, WindowSaturated)
	}
}

func TestSessionExhaustedSaysHowLongUntilTheNextSession(t *testing.T) {
	in := input(queued(1))
	in.Snapshot.SessionUtil = 100
	in.Snapshot.SessionResetsAt = now.Add(28 * time.Minute)
	p := Decide(in)

	if want := "28m"; !contains_(p.Global.Text, want) {
		t.Errorf("Global.Text = %q, want it to mention %q until the next session", p.Global.Text, want)
	}
}

func TestReasonTextIsTimezoneIndependent(t *testing.T) {
	// Policy renders durations, never clock times: a wall-clock string here
	// would make the same Plan read differently on two machines.
	in := input(queued(1))
	in.Snapshot.SessionUtil = 100
	in.Snapshot.SessionResetsAt = now.Add(28 * time.Minute)

	utc := Decide(in).Global.Text
	in.Now = in.Now.In(time.FixedZone("nowhere", -7*3600))
	in.Snapshot.SessionResetsAt = in.Snapshot.SessionResetsAt.In(time.FixedZone("nowhere", -7*3600))
	shifted := Decide(in).Global.Text

	if utc != shifted {
		t.Errorf("reason text changed with timezone: %q vs %q", utc, shifted)
	}
}

func TestWeeklyExhaustedDoesNotSuspendRunningWorkers(t *testing.T) {
	// A spent week stops new launches, but suspending in-flight work would
	// throw away progress without freeing anything that matters.
	in := input(queued(4), 1, 2)
	in.Snapshot.WeeklyUtil = 95
	p := Decide(in)

	if len(p.Suspend) != 0 {
		t.Fatalf("suspend = %+v, want none for a weekly limit", p.Suspend)
	}
	if got := len(p.Launches()); got != 0 {
		t.Errorf("launched %d with the week spent, want 0", got)
	}
	if p.Global.Code != ReasonWeeklyExhausted {
		t.Errorf("Global = %q, want %q", p.Global.Code, ReasonWeeklyExhausted)
	}
}

func TestWeeklyBackoffBlocksLaunches(t *testing.T) {
	in := input(queued(1))
	in.WeeklyBackoffUntil = now.Add(90 * time.Minute)
	p := Decide(in)

	if got := len(p.Launches()); got != 0 {
		t.Fatalf("launched %d during weekly backoff, want 0", got)
	}
	if p.Global.Code != ReasonWeeklyBackoff {
		t.Errorf("Global = %q, want %q", p.Global.Code, ReasonWeeklyBackoff)
	}
}

func TestElapsedWeeklyBackoffIsIgnored(t *testing.T) {
	in := input(queued(1))
	in.WeeklyBackoffUntil = now.Add(-1 * time.Minute)
	p := Decide(in)

	if got := len(p.Launches()); got != 1 {
		t.Fatalf("launched %d after backoff elapsed, want 1", got)
	}
}

// --- usage data quality ---

func TestNoUsageDataBlocks(t *testing.T) {
	in := input(queued(1))
	in.Snapshot = nil
	p := Decide(in)

	if got := len(p.Launches()); got != 0 {
		t.Fatalf("launched %d with no usage reading, want 0", got)
	}
	if p.Global.Code != ReasonNoUsageData {
		t.Errorf("Global = %q, want %q", p.Global.Code, ReasonNoUsageData)
	}
	if p.WindowState != WindowIdle {
		t.Errorf("WindowState = %q, want %q", p.WindowState, WindowIdle)
	}
}

func TestStaleUsageDataBlocks(t *testing.T) {
	in := input(queued(1))
	in.Snapshot.CapturedAt = now.Add(-16 * time.Minute)
	p := Decide(in)

	if got := len(p.Launches()); got != 0 {
		t.Fatalf("launched %d against a stale reading, want 0", got)
	}
	if p.Global.Code != ReasonStaleUsageData {
		t.Errorf("Global = %q, want %q", p.Global.Code, ReasonStaleUsageData)
	}
}

func TestFreshEnoughUsageDataLaunches(t *testing.T) {
	in := input(queued(1))
	in.Snapshot.CapturedAt = now.Add(-14 * time.Minute)
	p := Decide(in)

	if got := len(p.Launches()); got != 1 {
		t.Fatalf("launched %d against a 14m reading (limit 15m), want 1", got)
	}
}

func TestSnapshotAgeCheckDisabled(t *testing.T) {
	in := input(queued(1))
	in.Policy.MaxSnapshotAge = 0
	in.Snapshot.CapturedAt = now.Add(-10 * time.Hour)
	p := Decide(in)

	if got := len(p.Launches()); got != 1 {
		t.Fatalf("launched %d with the age check disabled, want 1", got)
	}
}

// --- attempts ---

func TestAttemptCapFailsTask(t *testing.T) {
	in := input([]Candidate{{TaskID: 7, Resume: true, Attempt: 5}})
	p := Decide(in)

	d := mustDecision(t, p, 7)
	if d.Action != ActionFail {
		t.Fatalf("action %q at the attempt cap, want fail", d.Action)
	}
	if d.Reason.Code != ReasonAttemptCap {
		t.Errorf("reason %q, want %q", d.Reason.Code, ReasonAttemptCap)
	}
	if len(p.Failures()) != 1 {
		t.Errorf("Failures() = %+v, want 1 entry", p.Failures())
	}
}

func TestAttemptsBelowCapStillLaunch(t *testing.T) {
	in := input([]Candidate{{TaskID: 7, Resume: true, Attempt: 4}})
	p := Decide(in)

	if d := mustDecision(t, p, 7); d.Action != ActionLaunch {
		t.Fatalf("action %q at attempt 4 of 5, want launch", d.Action)
	}
}

func TestFreshTaskAttemptIsNotCapped(t *testing.T) {
	// A queued task carries no attempt history, so the cap must not apply
	// even if the field is set.
	in := input([]Candidate{{TaskID: 7, Resume: false, Attempt: 99}})
	p := Decide(in)

	if d := mustDecision(t, p, 7); d.Action != ActionLaunch {
		t.Fatalf("action %q for a fresh task, want launch", d.Action)
	}
}

func TestAttemptCapAppliesEvenWhenGated(t *testing.T) {
	// Reporting "session spent" for a task that is actually out of attempts
	// would hide the real state until the next session.
	in := input([]Candidate{{TaskID: 7, Resume: true, Attempt: 5}})
	in.Snapshot.SessionUtil = 100
	p := Decide(in)

	if d := mustDecision(t, p, 7); d.Action != ActionFail {
		t.Fatalf("action %q, want fail even with the session spent", d.Action)
	}
}

// --- manual launch ---

func TestForceBypassesGatesAndWorkerCap(t *testing.T) {
	in := input(queued(9), 1, 2, 3)
	in.Snapshot.SessionUtil = 100
	in.Force = map[int64]bool{9: true}
	p := Decide(in)

	d := mustDecision(t, p, 9)
	if d.Action != ActionLaunch {
		t.Fatalf("action %q for a forced task, want launch", d.Action)
	}
	if d.Timeout != testPolicy().RunTimeout {
		t.Errorf("timeout %s, want the full %s", d.Timeout, testPolicy().RunTimeout)
	}
}

func TestForceDoesNotRelaunchRunningTask(t *testing.T) {
	in := input(queued(1), 1)
	in.Force = map[int64]bool{1: true}
	p := Decide(in)

	d := mustDecision(t, p, 1)
	if d.Action != ActionWait {
		t.Fatalf("action %q for an already-running task, want wait", d.Action)
	}
	if d.Reason.Code != ReasonRunning {
		t.Errorf("reason %q, want %q", d.Reason.Code, ReasonRunning)
	}
}

func TestForceDoesNotBypassAttemptCap(t *testing.T) {
	in := input([]Candidate{{TaskID: 7, Resume: true, Attempt: 5}})
	in.Force = map[int64]bool{7: true}
	p := Decide(in)

	if d := mustDecision(t, p, 7); d.Action != ActionFail {
		t.Fatalf("action %q, want fail — a manual launch must not loop past the cap", d.Action)
	}
}

func TestForceDoesNotStealSlotsFromTheQueue(t *testing.T) {
	// One free worker, one forced task, one ordinary candidate: the forced
	// launch is an extra, so the ordinary candidate still gets its slot.
	in := input(queued(4, 9), 1, 2)
	in.Force = map[int64]bool{9: true}
	p := Decide(in)

	if d := mustDecision(t, p, 4); d.Action != ActionLaunch {
		t.Errorf("task 4: action %q, want launch (it owns the free slot)", d.Action)
	}
	if d := mustDecision(t, p, 9); d.Action != ActionLaunch {
		t.Errorf("task 9: action %q, want launch (forced)", d.Action)
	}
}

// --- Global reason honesty ---
//
// These are the regression tests for the status endpoint reporting
// "queue empty" and the task chips reporting "next window (resets at 16:20)"
// while four tasks sat waiting on a full worker pool.

func TestGlobalNeverClaimsQueueEmptyWithCandidatesWaiting(t *testing.T) {
	p := Decide(input(queued(1, 2, 3, 4), 10, 11, 12))

	if p.Global.Code == ReasonQueueEmpty {
		t.Fatal("Global claimed queue empty with 4 candidates waiting")
	}
	if p.Global.Code != ReasonWorkersBusy {
		t.Errorf("Global = %q, want %q", p.Global.Code, ReasonWorkersBusy)
	}
}

func TestGlobalReportsQueueEmptyOnlyWhenEmpty(t *testing.T) {
	p := Decide(input(nil))

	if p.Global.Code != ReasonQueueEmpty {
		t.Fatalf("Global = %q, want %q", p.Global.Code, ReasonQueueEmpty)
	}
	if p.Global.Blocking() {
		t.Error("an empty queue is not a blocked state")
	}
}

func TestWorkersBusyIsNotReportedAsASessionWait(t *testing.T) {
	p := Decide(input(queued(1), 10, 11, 12))

	d := mustDecision(t, p, 1)
	if d.Reason.Code == ReasonSessionExhausted {
		t.Fatal("a full worker pool was reported as a spent session")
	}
	if d.Reason.Code != ReasonWorkersBusy {
		t.Fatalf("reason %q, want %q", d.Reason.Code, ReasonWorkersBusy)
	}
	if contains_(d.Reason.Text, "window") || contains_(d.Reason.Text, "resets") {
		t.Errorf("reason text %q must not talk about session resets", d.Reason.Text)
	}
}

// A running candidate is not a blocked one. If "running" were allowed to explain
// the queue, it would mask the real blocker behind it.
func TestGlobalSkipsRunningCandidateAndReportsRealBlocker(t *testing.T) {
	// Task 1 holds the only worker and is also still listed as a candidate;
	// task 2 is genuinely waiting on the pool.
	in := input(queued(1, 2), 1)
	in.Policy.Workers = 1
	p := Decide(in)

	if got := mustDecision(t, p, 1).Reason.Code; got != ReasonRunning {
		t.Fatalf("task 1 reason = %q, want %q", got, ReasonRunning)
	}
	if p.Global.Code != ReasonWorkersBusy {
		t.Errorf("Global = %q, want %q — a running task must not mask the real blocker",
			p.Global.Code, ReasonWorkersBusy)
	}
	if !p.Global.Blocking() {
		t.Error("Global should be blocking: task 2 cannot start")
	}
}

func TestGlobalIsReadyWhileLaunching(t *testing.T) {
	p := Decide(input(queued(1, 2)))

	if p.Global.Code != ReasonReady {
		t.Fatalf("Global = %q, want %q", p.Global.Code, ReasonReady)
	}
	if p.Global.Blocking() {
		t.Error("ready must not be blocking")
	}
}

func TestGlobalReportsHighestPriorityBlocker(t *testing.T) {
	// Task 7 is out of attempts and task 1 is behind a full pool. The queue's
	// real problem is the full pool, not the dead task.
	in := input([]Candidate{{TaskID: 7, Resume: true, Attempt: 5}, {TaskID: 1}}, 10, 11, 12)
	p := Decide(in)

	if p.Global.Code != ReasonWorkersBusy {
		t.Fatalf("Global = %q, want %q", p.Global.Code, ReasonWorkersBusy)
	}
}

func TestEveryWaitingCandidateCarriesAReason(t *testing.T) {
	in := input(queued(1, 2, 3, 4, 5), 10)
	in.Snapshot.SessionUtil = 100
	p := Decide(in)

	for _, d := range p.Decisions {
		if d.Reason.Code == "" || d.Reason.Text == "" {
			t.Errorf("task %d: empty reason %+v", d.TaskID, d.Reason)
		}
	}
}

func TestDecisionsCoverEveryCandidateInOrder(t *testing.T) {
	ids := []int64{5, 3, 9, 1}
	p := Decide(input(queued(ids...)))

	if len(p.Decisions) != len(ids) {
		t.Fatalf("got %d decisions, want %d", len(p.Decisions), len(ids))
	}
	for i, want := range ids {
		if p.Decisions[i].TaskID != want {
			t.Errorf("decision %d is task %d, want %d", i, p.Decisions[i].TaskID, want)
		}
	}
}

func TestZeroWorkersLaunchesNothing(t *testing.T) {
	in := input(queued(1, 2))
	in.Policy.Workers = 0
	p := Decide(in)

	if got := len(p.Launches()); got != 0 {
		t.Fatalf("launched %d with 0 workers, want 0", got)
	}
	if d := mustDecision(t, p, 1); d.Reason.Code != ReasonWorkersBusy {
		t.Errorf("reason %q, want %q", d.Reason.Code, ReasonWorkersBusy)
	}
}

func TestOversubscribedWorkersLaunchNothing(t *testing.T) {
	// More running than the cap allows (the cap was lowered under us).
	p := Decide(input(queued(1), 10, 11, 12, 13))

	if got := len(p.Launches()); got != 0 {
		t.Fatalf("launched %d while oversubscribed, want 0", got)
	}
}

// --- window state ---

func TestWindowStateFor(t *testing.T) {
	tests := []struct {
		name string
		snap *Snapshot
		want string
	}{
		{"no reading", nil, WindowIdle},
		{"untouched session", &Snapshot{}, WindowIdle},
		{"partly used", &Snapshot{SessionUtil: 29}, WindowOpen},
		{"known reset, no usage", &Snapshot{SessionResetsAt: now}, WindowOpen},
		{"at threshold", &Snapshot{SessionUtil: 100}, WindowSaturated},
		{"over threshold", &Snapshot{SessionUtil: 120}, WindowSaturated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := WindowStateFor(tc.snap, 100); got != tc.want {
				t.Errorf("WindowStateFor() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- reset detection ---

func TestResetDetectorAdvancingResetTime(t *testing.T) {
	var d ResetDetector

	if d.Observe(Snapshot{SessionUtil: 50, SessionResetsAt: now.Add(3 * time.Hour)}) {
		t.Error("first reading must not report a reset")
	}
	if !d.Observe(Snapshot{SessionUtil: 10, SessionResetsAt: now.Add(8 * time.Hour)}) {
		t.Error("expected a reset when the reset time jumps forward")
	}
}

func TestResetDetectorUtilDrop(t *testing.T) {
	var d ResetDetector

	d.Observe(Snapshot{SessionUtil: 85, SessionResetsAt: now.Add(2 * time.Hour)})
	if !d.Observe(Snapshot{SessionUtil: 3, SessionResetsAt: now.Add(2 * time.Hour)}) {
		t.Error("expected a reset when utilization collapses")
	}
}

func TestResetDetectorIgnoresJitter(t *testing.T) {
	var d ResetDetector

	d.Observe(Snapshot{SessionUtil: 50, SessionResetsAt: now.Add(2 * time.Hour)})
	if d.Observe(Snapshot{SessionUtil: 51, SessionResetsAt: now.Add(2*time.Hour + 30*time.Second)}) {
		t.Error("a refined reset estimate and a 1% move are not a new session")
	}
}

func TestResetDetectorIgnoresSmallUtilDrop(t *testing.T) {
	var d ResetDetector

	d.Observe(Snapshot{SessionUtil: 50, SessionResetsAt: now.Add(2 * time.Hour)})
	if d.Observe(Snapshot{SessionUtil: 20, SessionResetsAt: now.Add(2 * time.Hour)}) {
		t.Error("a 30-point drop is under the threshold and is not a new session")
	}
}

// --- contract guards ---

// TestReasonCodesAreExhaustive fails when a reason code is added or renamed.
//
// Reason codes cross two boundaries that the compiler cannot check: the JSON
// status API, and the ReasonCode union in web/src/lib/api/types.ts that the
// forecast chip switches on. A new code that the web does not know about falls
// through to the neutral style silently, so adding one should be a deliberate
// act with this list and that union updated together.
func TestReasonCodesAreExhaustive(t *testing.T) {
	want := map[ReasonCode]bool{
		ReasonReady:            true,
		ReasonRunning:          true,
		ReasonWorkersBusy:      true,
		ReasonSessionExhausted: true,
		ReasonWeeklyExhausted:  true,
		ReasonWeeklyBackoff:    true,
		ReasonAttemptCap:       true,
		ReasonNoUsageData:      true,
		ReasonStaleUsageData:   true,
		ReasonQueueEmpty:       true,
	}
	// Every code Decide can emit, gathered by exercising each branch.
	emitted := map[ReasonCode]bool{}
	note := func(p Plan) {
		emitted[p.Global.Code] = true
		for _, d := range p.Decisions {
			emitted[d.Reason.Code] = true
		}
		for _, d := range p.Suspend {
			emitted[d.Reason.Code] = true
		}
	}

	note(Decide(input(queued(1))))                                          // ready
	note(Decide(input(nil)))                                                // queue empty
	note(Decide(input(queued(1, 2, 3, 4), 1, 2, 3)))                        // running + workers busy
	note(Decide(input([]Candidate{{TaskID: 7, Resume: true, Attempt: 5}}))) // attempt cap

	saturated := input(queued(1), 2)
	saturated.Snapshot.SessionUtil = 100
	note(Decide(saturated)) // session exhausted (+ suspend)

	weekly := input(queued(1))
	weekly.Snapshot.WeeklyUtil = 95
	note(Decide(weekly)) // weekly exhausted

	backoff := input(queued(1))
	backoff.WeeklyBackoffUntil = now.Add(time.Hour)
	note(Decide(backoff)) // weekly backoff

	nodata := input(queued(1))
	nodata.Snapshot = nil
	note(Decide(nodata)) // no usage data

	stale := input(queued(1))
	stale.Snapshot.CapturedAt = now.Add(-time.Hour)
	note(Decide(stale)) // stale usage data

	for code := range want {
		if !emitted[code] {
			t.Errorf("reason code %q is declared but no Decide branch emits it — dead code, or an untested branch", code)
		}
	}
	for code := range emitted {
		if !want[code] {
			t.Errorf("Decide emitted undeclared reason code %q — add it here and to the ReasonCode union in web/src/lib/api/types.ts", code)
		}
	}
}

// --- interruption status ---

func TestTaskStatusAfterInterruption(t *testing.T) {
	if got := TaskStatusAfterInterruption(true); got != "resumable" {
		t.Errorf("with a session: %q, want resumable", got)
	}
	if got := TaskStatusAfterInterruption(false); got != "failed" {
		t.Errorf("without a session: %q, want failed", got)
	}
}

// contains_ is a substring check; the name avoids colliding with the package's
// contains helper for int64 slices.
func contains_(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
