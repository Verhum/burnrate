package domain

import "time"

type TaskRepository interface {
	CreateTask(title, prompt, repoPath, size, model, status string) (*Task, error)
	GetTask(id int64) (*Task, error)
	ListTasks() ([]Task, error)
	UpdateTask(id int64, title, prompt, repoPath, size, model string) (*Task, error)
	DeleteTask(id int64) error
	ReorderTasks(orderedIDs []int64) error
	SetTaskStatus(id int64, status string) error
	ResetTaskAttempts(id int64) (int64, error)
	QueuedTasksByOrder() ([]Task, error)
	TasksByStatus(status string) ([]Task, error)
	TaskCountsByStatus() (map[string]int, error)
}

type RunRepository interface {
	CreateRun(taskID int64, worktreePath, branch, repoPath, windowID string, attempt int) (*Run, error)
	GetRun(id int64) (*Run, error)
	SetRunSessionID(id int64, sessionID string) error
	SetRunPID(id int64, pid int) error
	SetRunRateLimitResetAt(id int64, resetAt string) error
	SetRunStatus(id int64, status string) error
	FinishRun(id int64, status string, costUSD float64, numTurns int, prURL, errMsg, resultText string) error
	RunsByStatus(statuses ...string) ([]Run, error)
	ResumableRuns() ([]Run, error)
	ListRuns(taskID int64, limit int) ([]Run, error)
	LatestRunForTask(taskID int64) (*Run, error)
	SetRunBranch(id int64, branch string) error
	SetRunAgentRepo(id int64, repo string) error
	SetRunAgentWorkedIn(id int64, workedIn string) error
	WindowAggregate(windowID string) (WindowAggregate, error)
}

type TaskPRRepository interface {
	UpsertTaskPR(taskID, runID int64, repo, branch, prURL, workedIn string) error
	ListTaskPRs(taskID int64) ([]TaskPR, error)
	AllTaskPRs() (map[int64][]TaskPR, error)
}

type CommentRepository interface {
	AddComment(taskID int64, body, author string) (*Comment, error)
	GetComment(id int64) (*Comment, error)
	CommentsForTask(taskID int64) ([]Comment, error)
	UnconsumedComments(taskID int64) ([]Comment, error)
	MarkCommentsConsumed(taskID, runID int64) error
	// MarkCommentConsumed retires a single comment. Used when a reply is handed
	// to a live long-polling agent in-band, which must not also be re-injected
	// into the next run's prompt.
	MarkCommentConsumed(commentID, runID int64) error
}

type HumanRequestRepository interface {
	CreateHumanRequest(taskID, runID int64, kind, title, body string) (*HumanRequest, error)
	GetHumanRequest(id int64) (*HumanRequest, error)
	ListHumanRequests(status string) ([]HumanRequest, error)
	SetHumanRequestStatus(id int64, status string) error
	SetHumanRequestLive(id int64, live bool) error
	SetHumanRequestResponse(id int64, commentID int64) error
	PendingRequestCount() (int, error)
	CancelTaskRequests(taskID int64) error
}

type CaptureRepository interface {
	CreateCapture(taskID, requestID int64, initiator, targetDesc, mode string) (*Capture, error)
	GetCapture(id int64) (*Capture, error)
	ListCaptures(taskID int64) ([]Capture, error)
	SetCaptureStatus(id int64, status string) error
	SetCaptureVideoPath(id int64, videoPath string) error
	SetCaptureTranscript(id int64, transcript string) error
	SetCaptureNotes(id int64, notes string) error
	SetCaptureDuration(id int64, durationSec float64) error
	FinishCapture(id int64, videoPath, transcript string, durationSec float64) error
}

type AttachmentRepository interface {
	AddAttachment(taskID int64, filename, contentType string, sizeBytes int64) (*Attachment, error)
	ListAttachments(taskID int64) ([]Attachment, error)
	GetAttachment(id int64) (*Attachment, error)
	DeleteAttachment(id int64) error
}

type UsageRepository interface {
	InsertUsageSnapshot(snap UsageSnapshot) error
	LatestUsageSnapshot() (*UsageSnapshot, error)
	TrimUsageSnapshots(olderThan time.Time) error
}

type SettingsRepository interface {
	GetSetting(key string) (string, bool)
	SetSetting(key, value string) error
	AllSettings() (map[string]string, error)
}
