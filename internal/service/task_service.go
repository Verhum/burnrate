package service

import (
	"context"
	"fmt"

	"github.com/Verhum/burnrate/internal/domain"
)

type WorktreeCleaner interface {
	CheckpointAndRemove(ctx context.Context, worktreePath, repoPath string, runID int64)
}

type SchedulerGate interface {
	IsInflight(taskID int64) bool
	RunNow(ctx context.Context, taskID int64) error
	CancelTask(taskID int64) bool
}

type CreateTaskInput struct {
	Title    string
	Prompt   string
	RepoPath string
	Model    string
	Status   string
}

type UpdateTaskInput struct {
	Title  string
	Prompt string
	Model  string
	// RepoPath is nil when the caller omitted the field. The task form no
	// longer offers a repo path — the prompt names the repos — so an omitted
	// field must leave a legacy managed task's repo untouched rather than
	// silently converting it to agent-directed mode.
	RepoPath *string
}

// RequestCanceler retires the human requests belonging to a task that has
// stopped being worked on. Satisfied by *RequestService, which also fires the
// pending-request broadcast.
type RequestCanceler interface {
	CancelTaskRequests(ctx context.Context, taskID int64) error
}

type TaskService struct {
	tasks    domain.TaskRepository
	runs     domain.RunRepository
	cleaner  WorktreeCleaner
	sched    SchedulerGate
	requests RequestCanceler
}

func NewTaskService(
	tasks domain.TaskRepository,
	runs domain.RunRepository,
	cleaner WorktreeCleaner,
	sched SchedulerGate,
	requests RequestCanceler,
) *TaskService {
	return &TaskService{tasks: tasks, runs: runs, cleaner: cleaner, sched: sched, requests: requests}
}

// cancelRequests retires a finished task's pending questions. Leaving them
// pending strands them in the queue forever and keeps inflating
// pending_request_count, which drives the tray badge.
func (s *TaskService) cancelRequests(ctx context.Context, id int64) {
	if s.requests == nil {
		return
	}
	s.requests.CancelTaskRequests(ctx, id)
}

func (s *TaskService) CreateTask(ctx context.Context, in CreateTaskInput) (*domain.Task, error) {
	if in.Title == "" {
		return nil, &ValidationError{Field: "title", Message: "title is required"}
	}
	if in.Status != "" && in.Status != "queued" && in.Status != "backlog" {
		return nil, &ValidationError{Field: "status", Message: "status must be 'queued' or 'backlog'"}
	}
	return s.tasks.CreateTask(in.Title, in.Prompt, in.RepoPath, "medium", in.Model, in.Status)
}

func (s *TaskService) UpdateTask(ctx context.Context, id int64, in UpdateTaskInput) (*domain.Task, error) {
	existing, err := s.tasks.GetTask(id)
	if err != nil {
		return nil, &NotFoundError{Entity: "task", ID: id}
	}
	repoPath := existing.RepoPath
	if in.RepoPath != nil {
		repoPath = *in.RepoPath
	}
	task, err := s.tasks.UpdateTask(id, in.Title, in.Prompt, repoPath, "medium", in.Model)
	if err != nil {
		return nil, err
	}
	// An edited task is not the task those attempts were spent on.
	if _, err := s.tasks.ResetTaskAttempts(id); err == nil {
		task, _ = s.tasks.GetTask(id)
	}
	return task, nil
}

// schedulableStatuses are the statuses the scheduler will launch from. Moving a
// task into one of them by hand is the user saying "try this again", so it
// clears the attempt history the same way an edit does — otherwise re-queueing a
// task that hit the cap just fails it again on the next tick.
var schedulableStatuses = map[string]bool{"queued": true, "resumable": true}

func (s *TaskService) DeleteTask(ctx context.Context, id int64) error {
	task, err := s.tasks.GetTask(id)
	if err != nil {
		return &NotFoundError{Entity: "task", ID: id}
	}
	if task.Status == "running" || s.sched.IsInflight(id) {
		return &ConflictError{Message: "cannot delete a running task"}
	}

	latestRun, _ := s.runs.LatestRunForTask(id)
	if latestRun != nil && latestRun.WorktreePath != "" {
		go s.cleaner.CheckpointAndRemove(context.Background(), latestRun.WorktreePath, latestRun.RepoPath, latestRun.ID)
	}

	return s.tasks.DeleteTask(id)
}

func (s *TaskService) ReorderTasks(ctx context.Context, orderedIDs []int64) error {
	if len(orderedIDs) == 0 {
		return &ValidationError{Field: "ordered_ids", Message: "ordered_ids is required"}
	}
	seen := make(map[int64]bool, len(orderedIDs))
	for _, id := range orderedIDs {
		if seen[id] {
			return &ValidationError{Field: "ordered_ids", Message: "duplicate id in ordered_ids"}
		}
		seen[id] = true
	}
	return s.tasks.ReorderTasks(orderedIDs)
}

func (s *TaskService) PauseTask(ctx context.Context, id int64) error {
	task, err := s.tasks.GetTask(id)
	if err != nil {
		return &NotFoundError{Entity: "task", ID: id}
	}
	if task.Status != "queued" && task.Status != "resumable" {
		return &ConflictError{Message: "can only pause queued or resumable tasks"}
	}
	return s.tasks.SetTaskStatus(id, "paused")
}

func (s *TaskService) ResumeTask(ctx context.Context, id int64) (string, error) {
	task, err := s.tasks.GetTask(id)
	if err != nil {
		return "", &NotFoundError{Entity: "task", ID: id}
	}
	if task.Status != "paused" {
		return "", &ConflictError{Message: "can only resume paused tasks"}
	}
	newStatus := "queued"
	latestRun, err := s.runs.LatestRunForTask(id)
	if err == nil && latestRun != nil && latestRun.SessionID != "" {
		switch latestRun.Status {
		case "rate_limited", "timed_out", "errored":
			newStatus = "resumable"
		}
	}
	if err := s.tasks.SetTaskStatus(id, newStatus); err != nil {
		return "", err
	}
	// Both targets are schedulable, so un-pausing always earns a fresh cap.
	_, _ = s.tasks.ResetTaskAttempts(id)
	return newStatus, nil
}

func (s *TaskService) CompleteTask(ctx context.Context, id int64) error {
	task, err := s.tasks.GetTask(id)
	if err != nil {
		return &NotFoundError{Entity: "task", ID: id}
	}
	if task.Status == "running" || s.sched.IsInflight(id) {
		return &ConflictError{Message: "cannot complete a running task"}
	}
	if latestRun, _ := s.runs.LatestRunForTask(id); latestRun != nil && latestRun.WorktreePath != "" {
		go s.cleaner.CheckpointAndRemove(context.Background(), latestRun.WorktreePath, latestRun.RepoPath, latestRun.ID)
	}
	s.cancelRequests(ctx, id)
	return s.tasks.SetTaskStatus(id, "done")
}

func (s *TaskService) DismissTask(ctx context.Context, id int64) error {
	task, err := s.tasks.GetTask(id)
	if err != nil {
		return &NotFoundError{Entity: "task", ID: id}
	}
	if task.Status == "running" || s.sched.IsInflight(id) {
		return &ConflictError{Message: "cannot dismiss a running task"}
	}
	if latestRun, _ := s.runs.LatestRunForTask(id); latestRun != nil && latestRun.WorktreePath != "" {
		go s.cleaner.CheckpointAndRemove(context.Background(), latestRun.WorktreePath, latestRun.RepoPath, latestRun.ID)
	}
	s.cancelRequests(ctx, id)
	return s.tasks.SetTaskStatus(id, "dismissed")
}

func (s *TaskService) SetTaskStatus(ctx context.Context, id int64, status string) (string, error) {
	if status == "" {
		return "", &ValidationError{Field: "status", Message: "status is required"}
	}
	task, err := s.tasks.GetTask(id)
	if err != nil {
		return "", &NotFoundError{Entity: "task", ID: id}
	}
	if task.Status == "running" || s.sched.IsInflight(id) {
		return "", &ConflictError{Message: "cannot change status of a running task"}
	}
	if err := ValidateStatusTransition(task.Status, status); err != nil {
		return "", &ConflictError{Message: err.Error()}
	}
	if status == "resumable" {
		latestRun, _ := s.runs.LatestRunForTask(id)
		if latestRun == nil || latestRun.SessionID == "" {
			return "", &ConflictError{Message: "no session to resume — use queued"}
		}
	}
	if status == "done" || status == "dismissed" {
		latestRun, _ := s.runs.LatestRunForTask(id)
		if latestRun != nil && latestRun.WorktreePath != "" {
			go s.cleaner.CheckpointAndRemove(context.Background(), latestRun.WorktreePath, latestRun.RepoPath, latestRun.ID)
		}
		s.cancelRequests(ctx, id)
	}
	finalStatus := status
	if task.Status == "backlog" && status == "queued" {
		latestRun, _ := s.runs.LatestRunForTask(id)
		if latestRun != nil && latestRun.SessionID != "" {
			finalStatus = "resumable"
		}
	}
	if err := s.tasks.SetTaskStatus(id, finalStatus); err != nil {
		return "", err
	}
	if schedulableStatuses[finalStatus] {
		_, _ = s.tasks.ResetTaskAttempts(id)
	}
	return finalStatus, nil
}

func (s *TaskService) RunNow(ctx context.Context, id int64) error {
	return s.sched.RunNow(ctx, id)
}

func ValidateStatusTransition(from, to string) error {
	if from == "running" {
		return fmt.Errorf("cannot change status of a running task")
	}
	if to == "pr_created" {
		return fmt.Errorf("pr_created is set by the runner, not by users")
	}
	if to == "running" {
		return fmt.Errorf("running is set by the scheduler, not by users")
	}
	validTargets := map[string]bool{
		"queued": true, "backlog": true, "paused": true,
		"done": true, "dismissed": true, "failed": true, "resumable": true,
	}
	if !validTargets[to] {
		return fmt.Errorf("unknown target status %q", to)
	}
	return nil
}
