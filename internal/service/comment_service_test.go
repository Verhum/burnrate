package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Verhum/burnrate/internal/domain"
)

// ---------------------------------------------------------------------------
// Mock implementations (prefixed with "comment" to avoid conflicts with
// task_service_test.go mocks)
// ---------------------------------------------------------------------------

// commentMockTaskRepo implements domain.TaskRepository.
type commentMockTaskRepo struct {
	tasks         map[int64]*domain.Task
	statusUpdates map[int64]string
	attemptResets []int64
}

func newCommentMockTaskRepo() *commentMockTaskRepo {
	return &commentMockTaskRepo{
		tasks:         make(map[int64]*domain.Task),
		statusUpdates: make(map[int64]string),
	}
}

func (r *commentMockTaskRepo) GetTask(id int64) (*domain.Task, error) {
	t, ok := r.tasks[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return t, nil
}

func (r *commentMockTaskRepo) SetTaskStatus(id int64, status string) error {
	r.statusUpdates[id] = status
	return nil
}

func (r *commentMockTaskRepo) ResetTaskAttempts(id int64) (int64, error) {
	r.attemptResets = append(r.attemptResets, id)
	return 0, nil
}

func (r *commentMockTaskRepo) CreateTask(title, prompt, repoPath, size, model, status string) (*domain.Task, error) {
	panic("not implemented")
}
func (r *commentMockTaskRepo) ListTasks() ([]domain.Task, error) { panic("not implemented") }
func (r *commentMockTaskRepo) UpdateTask(id int64, title, prompt, repoPath, size, model string) (*domain.Task, error) {
	panic("not implemented")
}
func (r *commentMockTaskRepo) DeleteTask(id int64) error             { panic("not implemented") }
func (r *commentMockTaskRepo) ReorderTasks(orderedIDs []int64) error { panic("not implemented") }
func (r *commentMockTaskRepo) QueuedTasksByOrder() ([]domain.Task, error) {
	panic("not implemented")
}
func (r *commentMockTaskRepo) TasksByStatus(status string) ([]domain.Task, error) {
	panic("not implemented")
}
func (r *commentMockTaskRepo) TaskCountsByStatus() (map[string]int, error) {
	panic("not implemented")
}

// commentMockCommentRepo implements domain.CommentRepository.
type commentMockCommentRepo struct {
	comments map[int64][]domain.Comment
	nextID   int64
}

func newCommentMockCommentRepo() *commentMockCommentRepo {
	return &commentMockCommentRepo{
		comments: make(map[int64][]domain.Comment),
		nextID:   1,
	}
}

func (r *commentMockCommentRepo) AddComment(taskID int64, body, author string) (*domain.Comment, error) {
	c := domain.Comment{
		ID:     r.nextID,
		TaskID: taskID,
		Author: author,
		Body:   body,
	}
	r.nextID++
	r.comments[taskID] = append(r.comments[taskID], c)
	return &c, nil
}

func (r *commentMockCommentRepo) CommentsForTask(taskID int64) ([]domain.Comment, error) {
	cs := r.comments[taskID]
	if cs == nil {
		return []domain.Comment{}, nil
	}
	return cs, nil
}

func (r *commentMockCommentRepo) GetComment(id int64) (*domain.Comment, error) {
	for _, cs := range r.comments {
		for i := range cs {
			if cs[i].ID == id {
				c := cs[i]
				return &c, nil
			}
		}
	}
	return nil, fmt.Errorf("no comment %d", id)
}

func (r *commentMockCommentRepo) UnconsumedComments(taskID int64) ([]domain.Comment, error) {
	panic("not implemented")
}

func (r *commentMockCommentRepo) MarkCommentsConsumed(taskID, runID int64) error {
	panic("not implemented")
}

func (r *commentMockCommentRepo) MarkCommentConsumed(commentID, runID int64) error {
	if runID == 0 {
		runID = -1
	}
	for taskID, cs := range r.comments {
		for i := range cs {
			if cs[i].ID == commentID {
				r.comments[taskID][i].ConsumedByRun = runID
				return nil
			}
		}
	}
	return fmt.Errorf("no comment %d", commentID)
}

// commentMockRunRepo implements domain.RunRepository.
type commentMockRunRepo struct {
	latestRuns map[int64]*domain.Run
}

func newCommentMockRunRepo() *commentMockRunRepo {
	return &commentMockRunRepo{
		latestRuns: make(map[int64]*domain.Run),
	}
}

func (r *commentMockRunRepo) LatestRunForTask(taskID int64) (*domain.Run, error) {
	run, ok := r.latestRuns[taskID]
	if !ok {
		return nil, fmt.Errorf("no runs")
	}
	return run, nil
}

func (r *commentMockRunRepo) CreateRun(taskID int64, worktreePath, branch, repoPath, windowID string, attempt int) (*domain.Run, error) {
	panic("not implemented")
}
func (r *commentMockRunRepo) GetRun(id int64) (*domain.Run, error) {
	panic("not implemented")
}
func (r *commentMockRunRepo) SetRunSessionID(id int64, sessionID string) error {
	panic("not implemented")
}
func (r *commentMockRunRepo) SetRunPID(id int64, pid int) error { panic("not implemented") }
func (r *commentMockRunRepo) SetRunRateLimitResetAt(id int64, resetAt string) error {
	panic("not implemented")
}
func (r *commentMockRunRepo) SetRunStatus(id int64, status string) error {
	panic("not implemented")
}
func (r *commentMockRunRepo) FinishRun(id int64, status string, costUSD float64, numTurns int, prURL, errMsg, resultText string) error {
	panic("not implemented")
}
func (r *commentMockRunRepo) RunsByStatus(statuses ...string) ([]domain.Run, error) {
	panic("not implemented")
}
func (r *commentMockRunRepo) ResumableRuns() ([]domain.Run, error) { panic("not implemented") }
func (r *commentMockRunRepo) ListRuns(taskID int64, limit int) ([]domain.Run, error) {
	panic("not implemented")
}
func (r *commentMockRunRepo) SetRunBranch(id int64, branch string) error {
	panic("not implemented")
}
func (r *commentMockRunRepo) SetRunAgentRepo(id int64, repo string) error {
	panic("not implemented")
}
func (r *commentMockRunRepo) SetRunAgentWorkedIn(id int64, workedIn string) error {
	panic("not implemented")
}
func (r *commentMockRunRepo) WindowAggregate(windowID string) (domain.WindowAggregate, error) {
	panic("not implemented")
}

// commentMockSched implements SchedulerGate.
type commentMockSched struct {
	inflight map[int64]bool
}

func newCommentMockSched() *commentMockSched {
	return &commentMockSched{inflight: make(map[int64]bool)}
}

func (s *commentMockSched) IsInflight(taskID int64) bool {
	return s.inflight[taskID]
}

func (s *commentMockSched) RunNow(ctx context.Context, taskID int64) error {
	panic("not implemented")
}

func (s *commentMockSched) CancelTask(taskID int64) bool {
	panic("not implemented")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAddComment_EmptyBody(t *testing.T) {
	svc := NewCommentService(
		newCommentMockTaskRepo(),
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	_, _, err := svc.AddComment(context.Background(), 1, "")
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "body" {
		t.Errorf("expected Field='body', got %q", ve.Field)
	}
}

func TestAddComment_TaskNotFound(t *testing.T) {
	svc := NewCommentService(
		newCommentMockTaskRepo(), // no tasks
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	_, _, err := svc.AddComment(context.Background(), 999, "hello")
	if err == nil {
		t.Fatal("expected error for missing task, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestAddComment_RunningTask(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "running"}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	_, _, err := svc.AddComment(context.Background(), 1, "hello")
	if err == nil {
		t.Fatal("expected error for running task, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestAddComment_InflightTask(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	sched := newCommentMockSched()
	sched.inflight[1] = true

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		sched,
	)

	_, _, err := svc.AddComment(context.Background(), 1, "hello")
	if err == nil {
		t.Fatal("expected error for inflight task, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
}

func TestAddComment_QueuedTaskRequeues(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "queued"}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(), // no runs
		newCommentMockSched(),
	)

	comment, newStatus, err := svc.AddComment(context.Background(), 1, "fix the bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment == nil {
		t.Fatal("expected comment, got nil")
	}
	if comment.Body != "fix the bug" {
		t.Errorf("expected body='fix the bug', got %q", comment.Body)
	}
	if newStatus != "queued" {
		t.Errorf("expected newStatus='queued', got %q", newStatus)
	}
	if taskRepo.statusUpdates[1] != "queued" {
		t.Errorf("expected task status set to 'queued', got %q", taskRepo.statusUpdates[1])
	}
}

func TestAddComment_DoneTaskRequeuesQueued(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "done"}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(), // no runs with session
		newCommentMockSched(),
	)

	comment, newStatus, err := svc.AddComment(context.Background(), 1, "one more thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment == nil {
		t.Fatal("expected comment, got nil")
	}
	if newStatus != "queued" {
		t.Errorf("expected newStatus='queued', got %q", newStatus)
	}
	if taskRepo.statusUpdates[1] != "queued" {
		t.Errorf("expected task status set to 'queued', got %q", taskRepo.statusUpdates[1])
	}
}

func TestAddComment_DoneTaskRequeuesResumable(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "done"}

	runRepo := newCommentMockRunRepo()
	runRepo.latestRuns[1] = &domain.Run{
		ID:        10,
		TaskID:    1,
		SessionID: "sess-abc",
		Status:    "errored",
	}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		runRepo,
		newCommentMockSched(),
	)

	comment, newStatus, err := svc.AddComment(context.Background(), 1, "try again")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment == nil {
		t.Fatal("expected comment, got nil")
	}
	if newStatus != "resumable" {
		t.Errorf("expected newStatus='resumable', got %q", newStatus)
	}
	if taskRepo.statusUpdates[1] != "resumable" {
		t.Errorf("expected task status set to 'resumable', got %q", taskRepo.statusUpdates[1])
	}
}

func TestAddComment_BacklogNoStatusChange(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "backlog"}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	comment, newStatus, err := svc.AddComment(context.Background(), 1, "backlog note")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment == nil {
		t.Fatal("expected comment, got nil")
	}
	if newStatus != "" {
		t.Errorf("expected newStatus='' (no change), got %q", newStatus)
	}
	if _, changed := taskRepo.statusUpdates[1]; changed {
		t.Errorf("expected no status update for backlog task, but got %q", taskRepo.statusUpdates[1])
	}
}

func TestAddComment_PrCreatedRequeuesQueued(t *testing.T) {
	taskRepo := newCommentMockTaskRepo()
	taskRepo.tasks[1] = &domain.Task{ID: 1, Status: "pr_created"}

	svc := NewCommentService(
		taskRepo,
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(), // no runs with session
		newCommentMockSched(),
	)

	comment, newStatus, err := svc.AddComment(context.Background(), 1, "please revise")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment == nil {
		t.Fatal("expected comment, got nil")
	}
	if newStatus != "queued" {
		t.Errorf("expected newStatus='queued', got %q", newStatus)
	}
	if taskRepo.statusUpdates[1] != "queued" {
		t.Errorf("expected task status set to 'queued', got %q", taskRepo.statusUpdates[1])
	}
}

func TestListComments(t *testing.T) {
	commentRepo := newCommentMockCommentRepo()
	commentRepo.comments[1] = []domain.Comment{
		{ID: 1, TaskID: 1, Body: "first"},
		{ID: 2, TaskID: 1, Body: "second"},
	}

	svc := NewCommentService(
		newCommentMockTaskRepo(),
		commentRepo,
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	comments, err := svc.ListComments(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].Body != "first" {
		t.Errorf("expected first comment body='first', got %q", comments[0].Body)
	}
	if comments[1].Body != "second" {
		t.Errorf("expected second comment body='second', got %q", comments[1].Body)
	}
}

func TestListComments_Empty(t *testing.T) {
	svc := NewCommentService(
		newCommentMockTaskRepo(),
		newCommentMockCommentRepo(),
		newCommentMockRunRepo(),
		newCommentMockSched(),
	)

	comments, err := svc.ListComments(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(comments))
	}
}
