package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/notify"
)

// Wait budgets are configured, not hardcoded. The floor keeps a fat-fingered
// `wait_sec: 1` from turning a blocking tool into a busy-loop; there is
// deliberately no ceiling beyond the configured value, so an operator who sets
// human_request_wait_sec to 1200 gets 1200.
const minWaitSec = 5

const (
	defaultHumanRequestWaitSec    = 600
	defaultCaptureApprovalWaitSec = 120
)

type RequestService struct {
	tasks    domain.TaskRepository
	requests domain.HumanRequestRepository
	comments domain.CommentRepository
	runs     domain.RunRepository
	settings domain.SettingsRepository
	sched    SchedulerGate

	// onChange fires after any mutation to the pending-request set. The server
	// wires it to the SSE broadcast so a request opened by an MCP tool call
	// reaches the UI exactly like one opened over REST.
	onChange func()

	// runGrants records "approved for the rest of this run" decisions
	// (POST /api/requests/{id}/approve with scope "run"). Deliberately
	// in-memory: a grant is a statement about a live run, and a daemon restart
	// kills the run, so there is nothing to persist.
	mu        sync.Mutex
	runGrants map[int64]bool
}

func NewRequestService(
	tasks domain.TaskRepository,
	requests domain.HumanRequestRepository,
	comments domain.CommentRepository,
	runs domain.RunRepository,
	settings domain.SettingsRepository,
	sched SchedulerGate,
) *RequestService {
	return &RequestService{
		tasks:     tasks,
		requests:  requests,
		comments:  comments,
		runs:      runs,
		settings:  settings,
		sched:     sched,
		runGrants: map[int64]bool{},
	}
}

// SetOnChange registers the pending-request broadcast hook, mirroring how the
// server registers notify.SetNotifyFunc. Both live on the service rather than
// in the HTTP handlers so that MCP-created requests are not silently invisible.
func (s *RequestService) SetOnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

func (s *RequestService) notifyChange() {
	s.mu.Lock()
	fn := s.onChange
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// HumanRequestWaitSec and CaptureApprovalWaitSec read the settings table on
// every call, so a PUT /api/config changes the next tool call's budget without
// a daemon restart — the same live-read pattern CaptureService.AutoApproveEnabled
// uses.
func (s *RequestService) HumanRequestWaitSec() int {
	return s.settingInt("human_request_wait_sec", defaultHumanRequestWaitSec)
}

func (s *RequestService) CaptureApprovalWaitSec() int {
	return s.settingInt("capture_approval_wait_sec", defaultCaptureApprovalWaitSec)
}

func (s *RequestService) settingInt(key string, def int) int {
	if s.settings == nil {
		return def
	}
	v, ok := s.settings.GetSetting(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// ClampWait resolves an agent-requested wait against the configured budget:
// unset (or over budget) takes the configured value, and nothing goes below
// the floor.
func ClampWait(requested, configured int) int {
	if configured < minWaitSec {
		configured = minWaitSec
	}
	if requested <= 0 || requested > configured {
		return configured
	}
	if requested < minWaitSec {
		return minWaitSec
	}
	return requested
}

func (s *RequestService) Create(ctx context.Context, taskID, runID int64, kind, title, body string) (*domain.HumanRequest, error) {
	if kind != "question" && kind != "demo" && kind != "capture_approval" {
		return nil, &ValidationError{Field: "kind", Message: "kind must be question, demo, or capture_approval"}
	}
	if _, err := s.tasks.GetTask(taskID); err != nil {
		return nil, &NotFoundError{Entity: "task", ID: taskID}
	}

	// A scope:"run" approval already covers this run, so opening another
	// approval request would ask the human a question they have answered.
	preApproved := kind == "capture_approval" && s.HasRunGrant(runID)

	req, err := s.requests.CreateHumanRequest(taskID, runID, kind, title, body)
	if err != nil {
		return nil, err
	}

	if preApproved {
		if err := s.requests.SetHumanRequestStatus(req.ID, "answered"); err == nil {
			req.Status = "answered"
		}
		return req, nil
	}

	// Both of these used to live in the HTTP handlers, which is why a request
	// opened by an MCP tool call reached neither the UI nor the notification
	// centre. They belong to the creation itself, not to one of its callers.
	s.notifyChange()
	if kind == "capture_approval" {
		notify.CaptureApproval(taskID, req.ID, title, "")
	} else {
		notify.RequestCreated(taskID, req.ID, title)
	}

	return req, nil
}

func (s *RequestService) Get(ctx context.Context, id int64) (*domain.HumanRequest, error) {
	req, err := s.requests.GetHumanRequest(id)
	if err != nil {
		return nil, &NotFoundError{Entity: "request", ID: id}
	}
	return req, nil
}

func (s *RequestService) List(ctx context.Context, status string) ([]domain.HumanRequest, error) {
	return s.requests.ListHumanRequests(status)
}

func (s *RequestService) PendingCount(ctx context.Context) (int, error) {
	return s.requests.PendingRequestCount()
}

type RespondInput struct {
	RequestID int64
	Body      string
	Result    string
}

func (s *RequestService) Respond(ctx context.Context, in RespondInput) (*domain.Comment, error) {
	req, err := s.requests.GetHumanRequest(in.RequestID)
	if err != nil {
		return nil, &NotFoundError{Entity: "request", ID: in.RequestID}
	}
	if req.Status != "pending" {
		return nil, &ConflictError{Message: "request is not pending"}
	}

	body := in.Body
	if in.Result != "" {
		body = resultPrefix + in.Result + "\n\n" + body
	}

	comment, err := s.comments.AddComment(req.TaskID, body, "user")
	if err != nil {
		return nil, err
	}

	s.requests.SetHumanRequestResponse(in.RequestID, comment.ID)

	// Double-injection guard. A live long-poll is about to hand this exact text
	// back to the agent as the tool result, so leaving the comment unconsumed
	// would make the next run's "## Follow-up Instructions" repeat it. A parked
	// request has no one to hand it to, so its comment must stay unconsumed —
	// that injection is the only delivery path it has.
	if req.Live {
		s.comments.MarkCommentConsumed(comment.ID, req.RunID)
	}

	task, err := s.tasks.GetTask(req.TaskID)
	if err != nil {
		s.notifyChange()
		return comment, nil
	}

	if task.Status == "awaiting_human" {
		latestRun, _ := s.runs.LatestRunForTask(req.TaskID)
		if latestRun != nil && latestRun.SessionID != "" {
			s.tasks.SetTaskStatus(req.TaskID, "resumable")
		} else {
			s.tasks.SetTaskStatus(req.TaskID, "queued")
		}
		s.tasks.ResetTaskAttempts(req.TaskID)
	}

	s.notifyChange()
	return comment, nil
}

// Approve settles a request without a written reply. scope "run" additionally
// records a standing grant, so the rest of that run's approval requests are
// answered without troubling the human again.
func (s *RequestService) Approve(ctx context.Context, id int64, scope string) error {
	req, err := s.requests.GetHumanRequest(id)
	if err != nil {
		return &NotFoundError{Entity: "request", ID: id}
	}
	if req.Status != "pending" {
		return &ConflictError{Message: "request is not pending"}
	}
	if scope == "run" && req.RunID != 0 {
		s.grantRun(req.RunID)
	}
	if err := s.requests.SetHumanRequestStatus(id, "answered"); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

// Deny is the human saying no. It is not the same thing as a wait budget
// running out (see Expire): a denial is an answer, and the agent is told so.
func (s *RequestService) Deny(ctx context.Context, id int64) error {
	req, err := s.requests.GetHumanRequest(id)
	if err != nil {
		return &NotFoundError{Entity: "request", ID: id}
	}
	if req.Status != "pending" {
		return &ConflictError{Message: "request is not pending"}
	}
	if err := s.requests.SetHumanRequestStatus(id, "denied"); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

// Expire retires a request whose wait budget ran out with nobody answering.
// This is the only writer of the `expired` status, which was unreachable while
// the timeout path wrote `denied` and made a real denial indistinguishable
// from silence.
func (s *RequestService) Expire(ctx context.Context, id int64) error {
	req, err := s.requests.GetHumanRequest(id)
	if err != nil {
		return &NotFoundError{Entity: "request", ID: id}
	}
	if req.Status != "pending" {
		return nil
	}
	if err := s.requests.SetHumanRequestStatus(id, "expired"); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

// CancelTaskRequests retires every pending request on a task. A dismissed or
// completed task's questions can never be answered usefully, and leaving them
// pending inflates pending_request_count forever.
func (s *RequestService) CancelTaskRequests(ctx context.Context, taskID int64) error {
	if err := s.requests.CancelTaskRequests(taskID); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

func (s *RequestService) grantRun(runID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runGrants[runID] = true
}

// HasRunGrant reports whether the human has approved captures for the whole of
// this run.
func (s *RequestService) HasRunGrant(runID int64) bool {
	if runID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runGrants[runID]
}

// ReleaseRunGrant drops a run's standing grant.
//
// Nothing calls this yet: run ids are never reused, so a stale grant cannot be
// inherited by a later run, and the whole map dies with the daemon. It exists
// for the desktop capture work (M2), which will want to revoke a grant while a
// run is still going.
func (s *RequestService) ReleaseRunGrant(runID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runGrants, runID)
}

const resultPrefix = "**Result:** "

// AwaitOutcome is how a long-poll ended. The agent has no tool for reading the
// comment thread, so an answered request has to carry the human's actual words
// back in Reply — pointing at a comment id was the same as saying nothing.
type AwaitOutcome struct {
	Request *domain.HumanRequest
	// Reply is the full body of the response comment.
	Reply string
	// Result is the structured verdict the human attached (pass/fail/blocked),
	// parsed back off the comment body, when they attached one.
	Result string
	// TimedOut means the budget expired with the request still pending. The
	// request stays answerable — this is not a denial.
	TimedOut bool
}

// AwaitResponse blocks until the request leaves `pending` or the timeout
// expires.
func (s *RequestService) AwaitResponse(ctx context.Context, id int64, timeoutSec int) (*AwaitOutcome, error) {
	// Existence is checked before anything is written: awaiting a request that
	// does not exist used to flip `live` on and then surface a raw SQL error as
	// a 500.
	req, err := s.requests.GetHumanRequest(id)
	if err != nil {
		return nil, &NotFoundError{Entity: "request", ID: id}
	}
	if req.Status != "pending" {
		return s.outcome(req, false), nil
	}

	timeoutSec = ClampWait(timeoutSec, s.HumanRequestWaitSec())

	s.requests.SetHumanRequestLive(id, true)
	defer s.requests.SetHumanRequestLive(id, false)

	deadline := time.After(time.Duration(timeoutSec) * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			latest, err := s.requests.GetHumanRequest(id)
			if err != nil {
				return nil, err
			}
			return s.outcome(latest, latest.Status == "pending"), nil
		case <-deadline:
			latest, err := s.requests.GetHumanRequest(id)
			if err != nil {
				return nil, err
			}
			return s.outcome(latest, latest.Status == "pending"), nil
		case <-ticker.C:
			latest, err := s.requests.GetHumanRequest(id)
			if err != nil {
				return nil, err
			}
			if latest.Status != "pending" {
				return s.outcome(latest, false), nil
			}
		}
	}
}

// AwaitApproval waits on a capture_approval request and, unlike AwaitResponse,
// retires it when the budget runs out: an approval nobody granted in time is
// spent, not merely unanswered, and leaving it pending would keep nagging the
// human about a capture the agent has already given up on.
//
// This is the M2 substrate for desktop capture. The MCP capture tools refuse
// outright today (nothing implements capture), so the REST approval endpoints
// are its only live caller.
func (s *RequestService) AwaitApproval(ctx context.Context, id int64, timeoutSec int) (*AwaitOutcome, error) {
	timeoutSec = ClampWait(timeoutSec, s.CaptureApprovalWaitSec())

	out, err := s.AwaitResponse(ctx, id, timeoutSec)
	if err != nil {
		return nil, err
	}
	if !out.TimedOut {
		return out, nil
	}

	// Expiry and denial used to write the same status, which made "the human
	// said no" indistinguishable from "the human was away from the keyboard".
	if err := s.Expire(ctx, id); err != nil {
		return out, nil
	}
	if req, gerr := s.requests.GetHumanRequest(id); gerr == nil {
		out.Request = req
	}
	return out, nil
}

func (s *RequestService) outcome(req *domain.HumanRequest, timedOut bool) *AwaitOutcome {
	out := &AwaitOutcome{Request: req, TimedOut: timedOut}
	if req.Status == "answered" && req.ResponseCommentID != 0 && s.comments != nil {
		if c, err := s.comments.GetComment(req.ResponseCommentID); err == nil && c != nil {
			out.Reply = c.Body
			out.Result = parseResultMarker(c.Body)
		}
	}
	return out
}

// parseResultMarker recovers the structured verdict Respond folds into the
// comment body, so the tool result can report it as its own field without a
// second column to keep in step.
func parseResultMarker(body string) string {
	if !strings.HasPrefix(body, resultPrefix) {
		return ""
	}
	rest := body[len(resultPrefix):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSpace(rest)
}
