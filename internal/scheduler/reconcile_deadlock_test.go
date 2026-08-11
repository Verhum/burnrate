package scheduler

import (
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/usage"
)

// reconcile() runs under s.mu, and the server's OnBroadcast handler for
// "run_update" calls Scheduler.Status() — which calls plan(), which takes s.mu.
// sync.Mutex is not reentrant, so broadcasting from inside the locked section
// deadlocked the scheduler against itself: the daemon stopped launching
// anything and every /api/status request piled up behind the held lock until
// the process was killed.
//
// This reproduces the exact production path: a run left behind by a previous
// daemon whose process is gone (PID 0), which lands in reconcile's
// "is dead, marking recoverable" branch.
func TestReconcileDoesNotDeadlockWhenBroadcastReadsStatus(t *testing.T) {
	st := testStore(t)
	sched := New(st, testConfig(), usage.NewClient("http://unused"), log.New("", false))

	task, err := st.CreateTask("stale", "prompt", "", "medium", "", "queued")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := st.SetTaskStatus(task.ID, "running"); err != nil {
		t.Fatalf("set task status: %v", err)
	}
	run, err := st.CreateRun(task.ID, "", "", "", "", 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	// PID 0 == "the process that owned this run is gone".
	if err := st.SetRunPID(run.ID, 0); err != nil {
		t.Fatalf("set pid: %v", err)
	}
	if err := st.SetRunStatus(run.ID, "running"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	// Exactly what internal/server wires up for "run_update".
	broadcasts := make(chan int64, 8)
	sched.OnBroadcast = func(event string, payload any) {
		if event != "run_update" {
			return
		}
		_ = sched.Status()
		if id, ok := payload.(int64); ok {
			broadcasts <- id
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.reconcile()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("reconcile deadlocked: OnBroadcast called Status() while reconcile held s.mu")
	}

	select {
	case got := <-broadcasts:
		if got != task.ID {
			t.Errorf("broadcast task id = %d, want %d", got, task.ID)
		}
	default:
		t.Error("reconcile recovered the run but never broadcast run_update")
	}

	// The lock must be free afterwards, not merely un-deadlocked during the call.
	locked := make(chan struct{})
	go func() {
		defer close(locked)
		_ = sched.Status()
	}()
	select {
	case <-locked:
	case <-time.After(10 * time.Second):
		t.Fatal("s.mu still held after reconcile returned")
	}
}
