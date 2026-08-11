package service

import (
	"context"

	"github.com/Verhum/burnrate/internal/domain"
)

type CommentService struct {
	tasks    domain.TaskRepository
	comments domain.CommentRepository
	runs     domain.RunRepository
	sched    SchedulerGate
}

func NewCommentService(
	tasks domain.TaskRepository,
	comments domain.CommentRepository,
	runs domain.RunRepository,
	sched SchedulerGate,
) *CommentService {
	return &CommentService{tasks: tasks, comments: comments, runs: runs, sched: sched}
}

func (s *CommentService) ListComments(ctx context.Context, taskID int64) ([]domain.Comment, error) {
	return s.comments.CommentsForTask(taskID)
}

func (s *CommentService) AddComment(ctx context.Context, taskID int64, body string) (*domain.Comment, string, error) {
	if body == "" {
		return nil, "", &ValidationError{Field: "body", Message: "body is required"}
	}
	task, err := s.tasks.GetTask(taskID)
	if err != nil {
		return nil, "", &NotFoundError{Entity: "task", ID: taskID}
	}
	if task.Status == "running" || s.sched.IsInflight(taskID) {
		return nil, "", &ConflictError{Message: "cannot comment on a running task"}
	}

	comment, err := s.comments.AddComment(taskID, body, "user")
	if err != nil {
		return nil, "", err
	}

	latestRun, _ := s.runs.LatestRunForTask(taskID)
	resumable := false
	if latestRun != nil && latestRun.SessionID != "" {
		switch latestRun.Status {
		case "rate_limited", "timed_out", "errored":
			resumable = true
		}
	}

	newStatus := ""
	if task.Status != "backlog" {
		if resumable {
			newStatus = "resumable"
		} else {
			newStatus = "queued"
		}
		s.tasks.SetTaskStatus(taskID, newStatus)
		// A comment is a course correction, and it puts the task back in the
		// queue — so it earns a fresh attempt budget the same way an edit does.
		// Without this a task that gave up at max_attempts would be failed again
		// on the next tick, with the comment never read.
		_, _ = s.tasks.ResetTaskAttempts(taskID)
	}

	return comment, newStatus, nil
}
