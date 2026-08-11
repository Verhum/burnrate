package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

// ---------------------------------------------------------------------------
// Mock: TaskRepository
// ---------------------------------------------------------------------------

type mockTaskRepo struct {
	tasks         map[int64]*domain.Task
	nextID        int64
	statusSets    []statusSetCall
	attemptResets []int64
	// newestRunID is what ResetTaskAttempts records, standing in for
	// MAX(runs.id) for the task.
	newestRunID int64
}

type statusSetCall struct {
	ID     int64
	Status string
}

func newMockTaskRepo() *mockTaskRepo {
	return &mockTaskRepo{tasks: make(map[int64]*domain.Task), nextID: 1}
}

func (m *mockTaskRepo) CreateTask(title, prompt, repoPath, size, model, status string) (*domain.Task, error) {
	id := m.nextID
	m.nextID++
	t := &domain.Task{
		ID:       id,
		Title:    title,
		Prompt:   prompt,
		RepoPath: repoPath,
		Size:     size,
		Model:    model,
		Status:   status,
	}
	m.tasks[id] = t
	return t, nil
}

func (m *mockTaskRepo) GetTask(id int64) (*domain.Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return t, nil
}

func (m *mockTaskRepo) DeleteTask(id int64) error {
	delete(m.tasks, id)
	return nil
}

func (m *mockTaskRepo) SetTaskStatus(id int64, status string) error {
	t, ok := m.tasks[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	t.Status = status
	m.statusSets = append(m.statusSets, statusSetCall{ID: id, Status: status})
	return nil
}

func (m *mockTaskRepo) ResetTaskAttempts(id int64) (int64, error) {
	t, ok := m.tasks[id]
	if !ok {
		return 0, fmt.Errorf("not found")
	}
	m.attemptResets = append(m.attemptResets, id)
	if m.newestRunID > t.AttemptResetRunID {
		t.AttemptResetRunID = m.newestRunID
	}
	return t.AttemptResetRunID, nil
}

func (m *mockTaskRepo) ReorderTasks(orderedIDs []int64) error {
	return nil
}

func (m *mockTaskRepo) ListTasks() ([]domain.Task, error) { panic("not implemented") }
func (m *mockTaskRepo) UpdateTask(id int64, title, prompt, repoPath, size, model string) (*domain.Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	t.Title = title
	t.Prompt = prompt
	t.RepoPath = repoPath
	t.Size = size
	t.Model = model
	return t, nil
}
func (m *mockTaskRepo) QueuedTasksByOrder() ([]domain.Task, error)  { panic("not implemented") }
func (m *mockTaskRepo) TasksByStatus(string) ([]domain.Task, error) { panic("not implemented") }
func (m *mockTaskRepo) TaskCountsByStatus() (map[string]int, error) { panic("not implemented") }

// ---------------------------------------------------------------------------
// Mock: RunRepository
// ---------------------------------------------------------------------------

type mockRunRepo struct {
	latestRuns map[int64]*domain.Run // taskID -> Run
}

func newMockRunRepo() *mockRunRepo {
	return &mockRunRepo{latestRuns: make(map[int64]*domain.Run)}
}

func (m *mockRunRepo) LatestRunForTask(taskID int64) (*domain.Run, error) {
	r, ok := m.latestRuns[taskID]
	if !ok {
		return nil, fmt.Errorf("no runs")
	}
	return r, nil
}

func (m *mockRunRepo) CreateRun(int64, string, string, string, string, int) (*domain.Run, error) {
	panic("not implemented")
}
func (m *mockRunRepo) GetRun(int64) (*domain.Run, error)          { panic("not implemented") }
func (m *mockRunRepo) SetRunSessionID(int64, string) error        { panic("not implemented") }
func (m *mockRunRepo) SetRunPID(int64, int) error                 { panic("not implemented") }
func (m *mockRunRepo) SetRunRateLimitResetAt(int64, string) error { panic("not implemented") }
func (m *mockRunRepo) SetRunStatus(int64, string) error           { panic("not implemented") }
func (m *mockRunRepo) FinishRun(int64, string, float64, int, string, string, string) error {
	panic("not implemented")
}
func (m *mockRunRepo) RunsByStatus(...string) ([]domain.Run, error) { panic("not implemented") }
func (m *mockRunRepo) ResumableRuns() ([]domain.Run, error)         { panic("not implemented") }
func (m *mockRunRepo) ListRuns(int64, int) ([]domain.Run, error)    { panic("not implemented") }
func (m *mockRunRepo) SetRunBranch(int64, string) error             { panic("not implemented") }
func (m *mockRunRepo) SetRunAgentRepo(int64, string) error          { panic("not implemented") }
func (m *mockRunRepo) SetRunAgentWorkedIn(int64, string) error      { panic("not implemented") }
func (m *mockRunRepo) WindowAggregate(string) (domain.WindowAggregate, error) {
	panic("not implemented")
}

// ---------------------------------------------------------------------------
// Mock: WorktreeCleaner
// ---------------------------------------------------------------------------

type mockCleaner struct {
	mu    sync.Mutex
	calls []cleanerCall
	wg    sync.WaitGroup
}

type cleanerCall struct {
	WorktreePath string
	RepoPath     string
	RunID        int64
}

func (m *mockCleaner) CheckpointAndRemove(ctx context.Context, worktreePath, repoPath string, runID int64) {
	m.mu.Lock()
	m.calls = append(m.calls, cleanerCall{WorktreePath: worktreePath, RepoPath: repoPath, RunID: runID})
	m.mu.Unlock()
	m.wg.Done()
}

func (m *mockCleaner) expectCalls(n int) {
	m.wg.Add(n)
}

func (m *mockCleaner) waitCalls(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cleaner calls")
	}
}

func (m *mockCleaner) getCalls() []cleanerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]cleanerCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// ---------------------------------------------------------------------------
// Mock: SchedulerGate
// ---------------------------------------------------------------------------

type mockScheduler struct {
	inflightIDs map[int64]bool
	runNowErr   error
}

func newMockScheduler() *mockScheduler {
	return &mockScheduler{inflightIDs: make(map[int64]bool)}
}

func (m *mockScheduler) IsInflight(taskID int64) bool {
	return m.inflightIDs[taskID]
}

func (m *mockScheduler) RunNow(ctx context.Context, taskID int64) error {
	return m.runNowErr
}

func (m *mockScheduler) CancelTask(taskID int64) bool {
	return false
}

// ---------------------------------------------------------------------------
// Helper to build a TaskService with fresh mocks
// ---------------------------------------------------------------------------

// mockRequestCanceler records which tasks had their pending human requests
// retired.
type mockRequestCanceler struct {
	canceled []int64
}

func (m *mockRequestCanceler) CancelTaskRequests(ctx context.Context, taskID int64) error {
	m.canceled = append(m.canceled, taskID)
	return nil
}

func (m *mockRequestCanceler) sawCancel(taskID int64) bool {
	for _, id := range m.canceled {
		if id == taskID {
			return true
		}
	}
	return false
}

type testHarness struct {
	svc      *TaskService
	tasks    *mockTaskRepo
	runs     *mockRunRepo
	cleaner  *mockCleaner
	sched    *mockScheduler
	requests *mockRequestCanceler
}

func newHarness() *testHarness {
	tr := newMockTaskRepo()
	rr := newMockRunRepo()
	c := &mockCleaner{}
	s := newMockScheduler()
	rc := &mockRequestCanceler{}
	return &testHarness{
		svc:      NewTaskService(tr, rr, c, s, rc),
		tasks:    tr,
		runs:     rr,
		cleaner:  c,
		sched:    s,
		requests: rc,
	}
}

// ---------------------------------------------------------------------------
// Tests: CreateTask
// ---------------------------------------------------------------------------

func TestCreateTask_Success(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	task, err := h.svc.CreateTask(ctx, CreateTaskInput{
		Title:    "Implement feature X",
		Prompt:   "Do the thing",
		RepoPath: "/repo",
		Status:   "queued",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Title != "Implement feature X" {
		t.Errorf("title = %q, want %q", task.Title, "Implement feature X")
	}
	if task.Size != "medium" {
		t.Errorf("size = %q, want %q", task.Size, "medium")
	}
	if task.Status != "queued" {
		t.Errorf("status = %q, want %q", task.Status, "queued")
	}
}

func TestCreateTask_DefaultSizeMedium(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	task, err := h.svc.CreateTask(ctx, CreateTaskInput{
		Title: "No size specified",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Size != "medium" {
		t.Errorf("size = %q, want %q", task.Size, "medium")
	}
}

func TestCreateTask_EmptyTitle(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	_, err := h.svc.CreateTask(ctx, CreateTaskInput{Title: ""})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "title" {
		t.Errorf("field = %q, want %q", ve.Field, "title")
	}
}

func TestCreateTask_StatusBacklog(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	task, err := h.svc.CreateTask(ctx, CreateTaskInput{
		Title:  "Backlog task",
		Status: "backlog",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != "backlog" {
		t.Errorf("status = %q, want %q", task.Status, "backlog")
	}
}

func TestCreateTask_StatusEmptyAllowed(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	_, err := h.svc.CreateTask(ctx, CreateTaskInput{
		Title:  "No status",
		Status: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateTask_InvalidStatus(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	for _, status := range []string{"running", "done", "paused", "resumable", "failed", "pr_created", "dismissed", "nonsense"} {
		_, err := h.svc.CreateTask(ctx, CreateTaskInput{
			Title:  "Bad status",
			Status: status,
		})
		if err == nil {
			t.Errorf("status %q: expected error, got nil", status)
			continue
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("status %q: expected ValidationError, got %T: %v", status, err, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: DeleteTask
// ---------------------------------------------------------------------------

func TestDeleteTask_Success(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	err := h.svc.DeleteTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := h.tasks.tasks[1]; ok {
		t.Error("task should have been deleted")
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.DeleteTask(ctx, 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestDeleteTask_RunningStatus(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "running"}

	err := h.svc.DeleteTask(ctx, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestDeleteTask_Inflight(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.sched.inflightIDs[1] = true

	err := h.svc.DeleteTask(ctx, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestDeleteTask_CleansUpWorktree(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		TaskID:       1,
		WorktreePath: "/tmp/wt-1",
		RepoPath:     "/repo",
	}

	h.cleaner.expectCalls(1)
	err := h.svc.DeleteTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h.cleaner.waitCalls(t)
	calls := h.cleaner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 cleaner call, got %d", len(calls))
	}
	c := calls[0]
	if c.WorktreePath != "/tmp/wt-1" || c.RepoPath != "/repo" || c.RunID != 10 {
		t.Errorf("cleaner call = %+v, unexpected", c)
	}
}

func TestDeleteTask_NoWorktreeNoCleanup(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "done"}
	// latest run has empty worktree path
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		TaskID:       1,
		WorktreePath: "",
	}

	err := h.svc.DeleteTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.cleaner.calls) != 0 {
		t.Errorf("expected no cleaner calls, got %d", len(h.cleaner.calls))
	}
}

// ---------------------------------------------------------------------------
// Tests: ReorderTasks
// ---------------------------------------------------------------------------

func TestReorderTasks_Success(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.ReorderTasks(ctx, []int64{3, 1, 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReorderTasks_Empty(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.ReorderTasks(ctx, []int64{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "ordered_ids" {
		t.Errorf("field = %q, want %q", ve.Field, "ordered_ids")
	}
}

func TestReorderTasks_Duplicates(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.ReorderTasks(ctx, []int64{1, 2, 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Tests: PauseTask
// ---------------------------------------------------------------------------

func TestPauseTask_FromQueued(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	err := h.svc.PauseTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.tasks.tasks[1].Status != "paused" {
		t.Errorf("status = %q, want %q", h.tasks.tasks[1].Status, "paused")
	}
}

func TestPauseTask_FromResumable(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "resumable"}

	err := h.svc.PauseTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.tasks.tasks[1].Status != "paused" {
		t.Errorf("status = %q, want %q", h.tasks.tasks[1].Status, "paused")
	}
}

func TestPauseTask_NotFound(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.PauseTask(ctx, 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestPauseTask_WrongStatus(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	for _, status := range []string{"running", "done", "paused", "backlog", "dismissed"} {
		h.tasks.tasks[1] = &domain.Task{ID: 1, Status: status}
		err := h.svc.PauseTask(ctx, 1)
		if err == nil {
			t.Errorf("status %q: expected error, got nil", status)
			continue
		}
		var ce *ConflictError
		if !errors.As(err, &ce) {
			t.Errorf("status %q: expected ConflictError, got %T: %v", status, err, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: ResumeTask
// ---------------------------------------------------------------------------

func TestResumeTask_ToQueued(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "paused"}
	// no latest run => defaults to "queued"

	newStatus, err := h.svc.ResumeTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newStatus != "queued" {
		t.Errorf("newStatus = %q, want %q", newStatus, "queued")
	}
	if h.tasks.tasks[1].Status != "queued" {
		t.Errorf("task status = %q, want %q", h.tasks.tasks[1].Status, "queued")
	}
}

func TestResumeTask_ToResumable_RateLimited(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "paused"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:        10,
		TaskID:    1,
		SessionID: "sess-123",
		Status:    "rate_limited",
	}

	newStatus, err := h.svc.ResumeTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newStatus != "resumable" {
		t.Errorf("newStatus = %q, want %q", newStatus, "resumable")
	}
}

func TestResumeTask_ToResumable_TimedOut(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "paused"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:        10,
		TaskID:    1,
		SessionID: "sess-456",
		Status:    "timed_out",
	}

	newStatus, err := h.svc.ResumeTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newStatus != "resumable" {
		t.Errorf("newStatus = %q, want %q", newStatus, "resumable")
	}
}

func TestResumeTask_ToResumable_Errored(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "paused"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:        10,
		TaskID:    1,
		SessionID: "sess-789",
		Status:    "errored",
	}

	newStatus, err := h.svc.ResumeTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newStatus != "resumable" {
		t.Errorf("newStatus = %q, want %q", newStatus, "resumable")
	}
}

func TestResumeTask_SessionButCompletedStatus(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "paused"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:        10,
		TaskID:    1,
		SessionID: "sess-abc",
		Status:    "completed", // not one of the resumable statuses
	}

	newStatus, err := h.svc.ResumeTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newStatus != "queued" {
		t.Errorf("newStatus = %q, want %q", newStatus, "queued")
	}
}

func TestResumeTask_RunWithNoSessionID(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "paused"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:        10,
		TaskID:    1,
		SessionID: "", // no session
		Status:    "rate_limited",
	}

	newStatus, err := h.svc.ResumeTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newStatus != "queued" {
		t.Errorf("newStatus = %q, want %q", newStatus, "queued")
	}
}

func TestResumeTask_NotFound(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	_, err := h.svc.ResumeTask(ctx, 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestResumeTask_NotPaused(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	for _, status := range []string{"queued", "running", "done", "backlog", "resumable"} {
		h.tasks.tasks[1] = &domain.Task{ID: 1, Status: status}
		_, err := h.svc.ResumeTask(ctx, 1)
		if err == nil {
			t.Errorf("status %q: expected error, got nil", status)
			continue
		}
		var ce *ConflictError
		if !errors.As(err, &ce) {
			t.Errorf("status %q: expected ConflictError, got %T: %v", status, err, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: CompleteTask
// ---------------------------------------------------------------------------

func TestCompleteTask_Success(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	err := h.svc.CompleteTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.tasks.tasks[1].Status != "done" {
		t.Errorf("status = %q, want %q", h.tasks.tasks[1].Status, "done")
	}
}

func TestCompleteTask_NotFound(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.CompleteTask(ctx, 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestCompleteTask_Running(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "running"}

	err := h.svc.CompleteTask(ctx, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestCompleteTask_Inflight(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.sched.inflightIDs[1] = true

	err := h.svc.CompleteTask(ctx, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestCompleteTask_CleansUpWorktree(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		WorktreePath: "/tmp/wt-complete",
		RepoPath:     "/repo",
	}

	h.cleaner.expectCalls(1)
	err := h.svc.CompleteTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h.cleaner.waitCalls(t)
	calls := h.cleaner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 cleaner call, got %d", len(calls))
	}
	if calls[0].WorktreePath != "/tmp/wt-complete" {
		t.Errorf("worktree path = %q, want %q", calls[0].WorktreePath, "/tmp/wt-complete")
	}
}

// ---------------------------------------------------------------------------
// Tests: DismissTask
// ---------------------------------------------------------------------------

func TestDismissTask_Success(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	err := h.svc.DismissTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.tasks.tasks[1].Status != "dismissed" {
		t.Errorf("status = %q, want %q", h.tasks.tasks[1].Status, "dismissed")
	}
}

func TestDismissTask_NotFound(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.DismissTask(ctx, 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestDismissTask_Running(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "running"}

	err := h.svc.DismissTask(ctx, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestDismissTask_Inflight(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "done"}
	h.sched.inflightIDs[1] = true

	err := h.svc.DismissTask(ctx, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestDismissTask_CleansUpWorktree(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "failed"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           20,
		WorktreePath: "/tmp/wt-dismiss",
		RepoPath:     "/repo",
	}

	h.cleaner.expectCalls(1)
	err := h.svc.DismissTask(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h.cleaner.waitCalls(t)
	calls := h.cleaner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 cleaner call, got %d", len(calls))
	}
	if calls[0].WorktreePath != "/tmp/wt-dismiss" {
		t.Errorf("worktree path = %q, want %q", calls[0].WorktreePath, "/tmp/wt-dismiss")
	}
}

// ---------------------------------------------------------------------------
// Tests: SetTaskStatus
// ---------------------------------------------------------------------------

func TestSetTaskStatus_EmptyStatus(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	_, err := h.svc.SetTaskStatus(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_NotFound(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	_, err := h.svc.SetTaskStatus(ctx, 999, "queued")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_RunningConflict(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "running"}

	_, err := h.svc.SetTaskStatus(ctx, 1, "queued")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_InflightConflict(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.sched.inflightIDs[1] = true

	_, err := h.svc.SetTaskStatus(ctx, 1, "backlog")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_RejectPRCreated(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	_, err := h.svc.SetTaskStatus(ctx, 1, "pr_created")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_RejectRunning(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	_, err := h.svc.SetTaskStatus(ctx, 1, "running")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_ResumableRequiresSession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	// no latest run at all

	_, err := h.svc.SetTaskStatus(ctx, 1, "resumable")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_ResumableNoSession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:        10,
		SessionID: "", // no session
	}

	_, err := h.svc.SetTaskStatus(ctx, 1, "resumable")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_ResumableWithSession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:        10,
		SessionID: "sess-ok",
	}

	status, err := h.svc.SetTaskStatus(ctx, 1, "resumable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "resumable" {
		t.Errorf("status = %q, want %q", status, "resumable")
	}
}

func TestSetTaskStatus_ValidTransitions(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	for _, to := range []string{"queued", "backlog", "paused", "done", "dismissed", "failed"} {
		h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
		h.cleaner.mu.Lock()
		h.cleaner.calls = nil
		h.cleaner.mu.Unlock()

		status, err := h.svc.SetTaskStatus(ctx, 1, to)
		if err != nil {
			t.Errorf("transition to %q: unexpected error: %v", to, err)
			continue
		}
		if status != to {
			t.Errorf("transition to %q: status = %q", to, status)
		}
	}
}

func TestSetTaskStatus_UnknownTarget(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	_, err := h.svc.SetTaskStatus(ctx, 1, "nonsense")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestSetTaskStatus_DoneCleansWorktree(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		WorktreePath: "/tmp/wt-done",
		RepoPath:     "/repo",
	}

	h.cleaner.expectCalls(1)
	_, err := h.svc.SetTaskStatus(ctx, 1, "done")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h.cleaner.waitCalls(t)
	calls := h.cleaner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 cleaner call, got %d", len(calls))
	}
}

func TestSetTaskStatus_DismissedCleansWorktree(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		WorktreePath: "/tmp/wt-dismissed",
		RepoPath:     "/repo",
	}

	h.cleaner.expectCalls(1)
	_, err := h.svc.SetTaskStatus(ctx, 1, "dismissed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h.cleaner.waitCalls(t)
	calls := h.cleaner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 cleaner call, got %d", len(calls))
	}
}

func TestSetTaskStatus_BacklogToQueuedBecomesResumable(t *testing.T) {
	// When transitioning backlog -> queued, if the latest run has a session
	// the final status should be "resumable" regardless of whether the
	// worktree still exists on disk (it is recreated on resume).
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "backlog"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		SessionID:    "sess-backlog",
		WorktreePath: "/nonexistent/removed/worktree",
		RepoPath:     "/repo",
	}

	status, err := h.svc.SetTaskStatus(ctx, 1, "queued")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "resumable" {
		t.Errorf("status = %q, want %q", status, "resumable")
	}
}

func TestSetTaskStatus_BacklogToQueuedBecomesResumable_NoWorktree(t *testing.T) {
	// When transitioning backlog -> queued with a session but no worktree on
	// disk, the status should still be "resumable" — the worktree is
	// recreated from the branch on resume.
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "backlog"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		SessionID:    "sess-backlog",
		WorktreePath: "/nonexistent/path/that/does/not/exist",
		RepoPath:     "/repo",
	}

	status, err := h.svc.SetTaskStatus(ctx, 1, "queued")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "resumable" {
		t.Errorf("status = %q, want %q", status, "resumable")
	}
}

func TestSetTaskStatus_BacklogToQueuedStaysQueued_NoSession(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "backlog"}
	h.runs.latestRuns[1] = &domain.Run{
		ID:           10,
		SessionID:    "",
		WorktreePath: "/tmp/wt",
	}

	status, err := h.svc.SetTaskStatus(ctx, 1, "queued")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "queued" {
		t.Errorf("status = %q, want %q", status, "queued")
	}
}

// ---------------------------------------------------------------------------
// Tests: ValidateStatusTransition
// ---------------------------------------------------------------------------

func TestValidateStatusTransition_FromRunning(t *testing.T) {
	err := ValidateStatusTransition("running", "queued")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateStatusTransition_ToPRCreated(t *testing.T) {
	err := ValidateStatusTransition("queued", "pr_created")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateStatusTransition_ToRunning(t *testing.T) {
	err := ValidateStatusTransition("queued", "running")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateStatusTransition_UnknownTarget(t *testing.T) {
	err := ValidateStatusTransition("queued", "bogus")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateStatusTransition_AllValidTargets(t *testing.T) {
	validTargets := []string{"queued", "backlog", "paused", "done", "dismissed", "failed", "resumable"}
	for _, to := range validTargets {
		err := ValidateStatusTransition("queued", to)
		if err != nil {
			t.Errorf("transition queued -> %q: unexpected error: %v", to, err)
		}
	}
}

func TestValidateStatusTransition_ValidFromVariousSources(t *testing.T) {
	sources := []string{"queued", "backlog", "paused", "done", "dismissed", "failed", "resumable", "pr_created"}
	for _, from := range sources {
		err := ValidateStatusTransition(from, "queued")
		if err != nil {
			t.Errorf("transition %q -> queued: unexpected error: %v", from, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: UpdateTask repo path handling
// ---------------------------------------------------------------------------

func TestUpdateTask_OmittedRepoPathPreservesExisting(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	created, err := h.svc.CreateTask(ctx, CreateTaskInput{
		Title:    "legacy managed task",
		Prompt:   "do the thing",
		RepoPath: "/repos/legacy",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The task form no longer sends repo_path; an omitted field must not
	// silently convert a managed task into an agent-directed one.
	updated, err := h.svc.UpdateTask(ctx, created.ID, UpdateTaskInput{
		Title:  "legacy managed task",
		Prompt: "do the thing differently",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.RepoPath != "/repos/legacy" {
		t.Fatalf("repo path was clobbered: %q", updated.RepoPath)
	}
}

func TestUpdateTask_ExplicitRepoPathWins(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	created, _ := h.svc.CreateTask(ctx, CreateTaskInput{
		Title:    "legacy managed task",
		Prompt:   "p",
		RepoPath: "/repos/legacy",
	})

	empty := ""
	updated, err := h.svc.UpdateTask(ctx, created.ID, UpdateTaskInput{
		Title:    "legacy managed task",
		Prompt:   "p",
		RepoPath: &empty,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.RepoPath != "" {
		t.Fatalf("explicit empty repo path was ignored: %q", updated.RepoPath)
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.UpdateTask(context.Background(), 999, UpdateTaskInput{Title: "x"}); err == nil {
		t.Fatal("expected error for unknown task")
	}
}

// ---------------------------------------------------------------------------
// F11 — a finished task's pending requests are retired
// ---------------------------------------------------------------------------

// store.CancelTaskRequests existed but nothing called it, so dismissing a task
// left its questions pending forever and kept pending_request_count inflated.
func TestDismissTaskCancelsPendingRequests(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	if err := h.svc.DismissTask(context.Background(), 1); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if !h.requests.sawCancel(1) {
		t.Fatal("dismissing a task must retire its pending human requests")
	}
}

func TestCompleteTaskCancelsPendingRequests(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	if err := h.svc.CompleteTask(context.Background(), 1); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !h.requests.sawCancel(1) {
		t.Fatal("completing a task must retire its pending human requests")
	}
}

func TestSetTaskStatusToDoneCancelsPendingRequests(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	if _, err := h.svc.SetTaskStatus(context.Background(), 1, "done"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if !h.requests.sawCancel(1) {
		t.Fatal("moving a task to done must retire its pending human requests")
	}
}

// A task that is merely paused is still being worked on: its questions stay.
func TestPauseTaskKeepsPendingRequests(t *testing.T) {
	h := newHarness()
	h.tasks.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	if err := h.svc.PauseTask(context.Background(), 1); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if h.requests.sawCancel(1) {
		t.Fatal("pausing must not retire pending human requests")
	}
}
