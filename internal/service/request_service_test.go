package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// reqMockRequestRepo stands in for the SQLite-backed store, which serializes
// access internally. AwaitResponse polls this repo from one goroutine while
// Approve/Deny writes to it from another, so the mock has to do that
// serializing itself or -race fails on the mock rather than on anything real.
type reqMockRequestRepo struct {
	mu       sync.Mutex
	requests map[int64]*domain.HumanRequest
	nextID   int64
}

func newReqMockRequestRepo() *reqMockRequestRepo {
	return &reqMockRequestRepo{requests: map[int64]*domain.HumanRequest{}, nextID: 1}
}

func (r *reqMockRequestRepo) CreateHumanRequest(taskID, runID int64, kind, title, body string) (*domain.HumanRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	req := &domain.HumanRequest{
		ID: r.nextID, TaskID: taskID, RunID: runID, Kind: kind,
		Title: title, Body: body, Status: "pending",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	r.nextID++
	r.requests[req.ID] = req
	return req, nil
}

func (r *reqMockRequestRepo) GetHumanRequest(id int64) (*domain.HumanRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.requests[id]
	if !ok {
		return nil, fmt.Errorf("no request %d", id)
	}
	cp := *req
	return &cp, nil
}

func (r *reqMockRequestRepo) ListHumanRequests(status string) ([]domain.HumanRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.HumanRequest
	for _, req := range r.requests {
		if status == "" || req.Status == status {
			out = append(out, *req)
		}
	}
	return out, nil
}

func (r *reqMockRequestRepo) SetHumanRequestStatus(id int64, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.requests[id]
	if !ok {
		return fmt.Errorf("no request %d", id)
	}
	req.Status = status
	return nil
}

func (r *reqMockRequestRepo) SetHumanRequestLive(id int64, live bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req, ok := r.requests[id]; ok {
		req.Live = live
	}
	return nil
}

func (r *reqMockRequestRepo) SetHumanRequestResponse(id int64, commentID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.requests[id]
	if !ok {
		return fmt.Errorf("no request %d", id)
	}
	req.Status = "answered"
	req.ResponseCommentID = commentID
	return nil
}

func (r *reqMockRequestRepo) PendingRequestCount() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, req := range r.requests {
		if req.Status == "pending" {
			n++
		}
	}
	return n, nil
}

func (r *reqMockRequestRepo) CancelTaskRequests(taskID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, req := range r.requests {
		if req.TaskID == taskID && req.Status == "pending" {
			req.Status = "canceled"
		}
	}
	return nil
}

type reqMockSettings struct{ values map[string]string }

func (s *reqMockSettings) GetSetting(key string) (string, bool) {
	v, ok := s.values[key]
	return v, ok
}
func (s *reqMockSettings) SetSetting(key, value string) error {
	s.values[key] = value
	return nil
}
func (s *reqMockSettings) AllSettings() (map[string]string, error) { return s.values, nil }

type reqFixture struct {
	svc      *RequestService
	tasks    *mockTaskRepo
	requests *reqMockRequestRepo
	comments *commentMockCommentRepo
	runs     *commentMockRunRepo
	settings *reqMockSettings
	changes  int
}

func newReqFixture(t *testing.T) *reqFixture {
	t.Helper()
	f := &reqFixture{
		tasks:    newMockTaskRepo(),
		requests: newReqMockRequestRepo(),
		comments: newCommentMockCommentRepo(),
		runs:     newCommentMockRunRepo(),
		settings: &reqMockSettings{values: map[string]string{}},
	}
	f.tasks.tasks[1] = &domain.Task{ID: 1, Title: "t", Status: "running"}
	f.svc = NewRequestService(f.tasks, f.requests, f.comments, f.runs, f.settings, newMockScheduler())
	f.svc.SetOnChange(func() { f.changes++ })
	return f
}

// ---------------------------------------------------------------------------
// C4 — the reply reaches the agent in-band
// ---------------------------------------------------------------------------

// The agent has no comment-reading tool, so an answered await that does not
// carry the human's words is a dropped message.
func TestAwaitResponseCarriesReplyBody(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, err := f.svc.Create(ctx, 1, 7, "question", "which port?", "which port?")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := f.svc.Respond(ctx, RespondInput{
		RequestID: req.ID,
		Body:      "use 8080, the other one is taken",
		Result:    "pass",
	}); err != nil {
		t.Fatalf("respond: %v", err)
	}

	out, err := f.svc.AwaitResponse(ctx, req.ID, 5)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if out.Request.Status != "answered" {
		t.Fatalf("expected answered, got %q", out.Request.Status)
	}
	if want := "use 8080, the other one is taken"; !strings.Contains(out.Reply, want) {
		t.Fatalf("reply must carry the human's words, got %q", out.Reply)
	}
	if out.Result != "pass" {
		t.Fatalf("expected parsed result %q, got %q", "pass", out.Result)
	}
	if out.TimedOut {
		t.Fatal("an answered request did not time out")
	}
}

// ---------------------------------------------------------------------------
// C5 — double-injection guard
// ---------------------------------------------------------------------------

// A live long-poll delivers the reply in-band, so the comment must not also be
// re-injected into the next run's prompt.
func TestRespondConsumesCommentWhenLive(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, _ := f.svc.Create(ctx, 1, 7, "question", "q", "q")
	f.requests.requests[req.ID].Live = true

	comment, err := f.svc.Respond(ctx, RespondInput{RequestID: req.ID, Body: "answer"})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	stored, err := f.comments.GetComment(comment.ID)
	if err != nil {
		t.Fatalf("get comment: %v", err)
	}
	if stored.ConsumedByRun != 7 {
		t.Fatalf("a live reply must be consumed by the run it was delivered to, got %d", stored.ConsumedByRun)
	}
}

// A parked request has nobody to hand the reply to, so prompt injection is the
// only delivery path it has — the comment must stay unconsumed.
func TestRespondLeavesCommentUnconsumedWhenParked(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, _ := f.svc.Create(ctx, 1, 7, "question", "q", "q")
	f.requests.requests[req.ID].Live = false

	comment, err := f.svc.Respond(ctx, RespondInput{RequestID: req.ID, Body: "answer"})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	stored, _ := f.comments.GetComment(comment.ID)
	if stored.ConsumedByRun != 0 {
		t.Fatalf("a parked reply must stay unconsumed, got %d", stored.ConsumedByRun)
	}
}

// A live request whose run id is 0 still has to be marked consumed — and 0
// means "unconsumed" in that column, so it needs the sentinel.
func TestRespondConsumesLiveCommentWithNoRun(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, _ := f.svc.Create(ctx, 1, 0, "question", "q", "q")
	f.requests.requests[req.ID].Live = true

	comment, _ := f.svc.Respond(ctx, RespondInput{RequestID: req.ID, Body: "answer"})
	stored, _ := f.comments.GetComment(comment.ID)
	if stored.ConsumedByRun == 0 {
		t.Fatal("a runless live reply must still be marked consumed")
	}
}

// ---------------------------------------------------------------------------
// D7 — creation broadcasts, wherever it came from
// ---------------------------------------------------------------------------

func TestCreateFiresChangeHook(t *testing.T) {
	f := newReqFixture(t)
	if _, err := f.svc.Create(context.Background(), 1, 7, "question", "q", "q"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if f.changes != 1 {
		t.Fatalf("expected exactly 1 broadcast on create, got %d", f.changes)
	}
}

func TestCreateRejectsUnknownTaskAndKind(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Create(ctx, 1, 0, "nonsense", "t", "b"); err == nil {
		t.Fatal("expected a validation error for an unknown kind")
	}
	if _, err := f.svc.Create(ctx, 999, 0, "question", "t", "b"); err == nil {
		t.Fatal("expected a not-found error for an unknown task")
	}
}

// ---------------------------------------------------------------------------
// F12 — deny is not the same thing as a timeout
// ---------------------------------------------------------------------------

func TestDenyAndExpireAreDistinctStatuses(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	denied, _ := f.svc.Create(ctx, 1, 7, "capture_approval", "may I?", "b")
	if err := f.svc.Deny(ctx, denied.ID); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if got := f.requests.requests[denied.ID].Status; got != "denied" {
		t.Fatalf("explicit denial must write `denied`, got %q", got)
	}

	expired, _ := f.svc.Create(ctx, 1, 7, "capture_approval", "may I?", "b")
	if err := f.svc.Expire(ctx, expired.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if got := f.requests.requests[expired.ID].Status; got != "expired" {
		t.Fatalf("a spent wait budget must write `expired`, got %q", got)
	}
}

// The timeout path must retire the request as `expired`, which was unreachable
// while the timeout wrote `denied`.
func TestAwaitApprovalExpiresOnTimeout(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	f.settings.values["capture_approval_wait_sec"] = "5"

	req, _ := f.svc.Create(ctx, 1, 7, "capture_approval", "may I?", "b")

	out, err := f.svc.AwaitApproval(ctx, req.ID, 5)
	if err != nil {
		t.Fatalf("await approval: %v", err)
	}
	if !out.TimedOut {
		t.Fatal("expected a timeout")
	}
	if got := out.Request.Status; got != "expired" {
		t.Fatalf("expected `expired`, got %q", got)
	}
}

// An explicit denial arriving during the wait must survive as `denied`.
func TestAwaitApprovalKeepsExplicitDenial(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, _ := f.svc.Create(ctx, 1, 7, "capture_approval", "may I?", "b")
	go func() {
		time.Sleep(200 * time.Millisecond)
		f.svc.Deny(context.Background(), req.ID)
	}()

	out, err := f.svc.AwaitApproval(ctx, req.ID, 10)
	if err != nil {
		t.Fatalf("await approval: %v", err)
	}
	if out.TimedOut {
		t.Fatal("a denial is an answer, not a timeout")
	}
	if got := out.Request.Status; got != "denied" {
		t.Fatalf("expected `denied`, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// F13 — awaiting something that does not exist is a 404, not a 500
// ---------------------------------------------------------------------------

func TestAwaitResponseUnknownIDIsNotFound(t *testing.T) {
	f := newReqFixture(t)
	_, err := f.svc.AwaitResponse(context.Background(), 4242, 5)
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := err.(*NotFoundError); !ok {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

// Existence is checked before anything is written, so a bogus id must not have
// flipped `live` on the way out.
func TestAwaitResponseUnknownIDWritesNothing(t *testing.T) {
	f := newReqFixture(t)
	f.svc.AwaitResponse(context.Background(), 4242, 5)
	if len(f.requests.requests) != 0 {
		t.Fatalf("nothing should have been created, got %d rows", len(f.requests.requests))
	}
}

// ---------------------------------------------------------------------------
// G14 — wait budgets come from config, with a floor
// ---------------------------------------------------------------------------

func TestWaitBudgetsReadConfigLive(t *testing.T) {
	f := newReqFixture(t)

	if got := f.svc.HumanRequestWaitSec(); got != defaultHumanRequestWaitSec {
		t.Fatalf("unset should fall back to %d, got %d", defaultHumanRequestWaitSec, got)
	}
	// A PUT /api/config lands in the settings table; the next call must see it
	// without a restart.
	f.settings.values["human_request_wait_sec"] = "1200"
	if got := f.svc.HumanRequestWaitSec(); got != 1200 {
		t.Fatalf("expected the configured 1200, got %d", got)
	}
	f.settings.values["capture_approval_wait_sec"] = "45"
	if got := f.svc.CaptureApprovalWaitSec(); got != 45 {
		t.Fatalf("expected the configured 45, got %d", got)
	}
}

func TestClampWait(t *testing.T) {
	cases := []struct{ requested, configured, want int }{
		{0, 600, 600},    // unset takes the configured budget
		{-5, 600, 600},   // nonsense takes the configured budget
		{30, 600, 30},    // a modest request is honoured
		{9000, 600, 600}, // over budget is capped at the configured value
		{1, 600, 5},      // below the floor is raised to it
		{0, 1200, 1200},  // no hardcoded 600 ceiling any more
		{0, 1, 5},        // a silly configured value still respects the floor
	}
	for _, c := range cases {
		if got := ClampWait(c.requested, c.configured); got != c.want {
			t.Errorf("ClampWait(%d, %d) = %d, want %d", c.requested, c.configured, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// H17 — scope:"run" grants
// ---------------------------------------------------------------------------

func TestApproveRunScopeGrantsForWholeRun(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	first, _ := f.svc.Create(ctx, 1, 7, "capture_approval", "may I?", "b")
	if f.svc.HasRunGrant(7) {
		t.Fatal("no grant should exist before approval")
	}
	if err := f.svc.Approve(ctx, first.ID, "run"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !f.svc.HasRunGrant(7) {
		t.Fatal("scope=run must grant for the whole run")
	}

	// The next approval on the same run is settled without troubling the human.
	before := f.changes
	second, err := f.svc.Create(ctx, 1, 7, "capture_approval", "may I again?", "b")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if second.Status != "answered" {
		t.Fatalf("a pre-approved request must be born answered, got %q", second.Status)
	}
	if f.changes != before {
		t.Fatal("a pre-approved request must not notify the human")
	}

	// A different run is unaffected.
	if f.svc.HasRunGrant(8) {
		t.Fatal("a grant must not leak to another run")
	}
	other, _ := f.svc.Create(ctx, 1, 8, "capture_approval", "may I?", "b")
	if other.Status != "pending" {
		t.Fatalf("another run's approval must still be pending, got %q", other.Status)
	}
}

func TestApproveOnceDoesNotGrantRun(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, _ := f.svc.Create(ctx, 1, 7, "capture_approval", "may I?", "b")
	if err := f.svc.Approve(ctx, req.ID, "once"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if f.svc.HasRunGrant(7) {
		t.Fatal("scope=once must not grant for the whole run")
	}
}

func TestReleaseRunGrant(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, _ := f.svc.Create(ctx, 1, 7, "capture_approval", "may I?", "b")
	f.svc.Approve(ctx, req.ID, "run")
	f.svc.ReleaseRunGrant(7)
	if f.svc.HasRunGrant(7) {
		t.Fatal("a released grant must be gone")
	}
}

// A grant keyed on run 0 would cover every runless request at once.
func TestRunGrantIgnoresZeroRun(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	req, _ := f.svc.Create(ctx, 1, 0, "capture_approval", "may I?", "b")
	f.svc.Approve(ctx, req.ID, "run")
	if f.svc.HasRunGrant(0) {
		t.Fatal("run 0 must never hold a grant")
	}
}

// ---------------------------------------------------------------------------
// F11 — cancelling a task's requests
// ---------------------------------------------------------------------------

func TestCancelTaskRequestsRetiresPendingOnly(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()

	pending, _ := f.svc.Create(ctx, 1, 7, "question", "q1", "b")
	answered, _ := f.svc.Create(ctx, 1, 7, "question", "q2", "b")
	f.svc.Respond(ctx, RespondInput{RequestID: answered.ID, Body: "done"})

	if err := f.svc.CancelTaskRequests(ctx, 1); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := f.requests.requests[pending.ID].Status; got != "canceled" {
		t.Fatalf("pending request should be canceled, got %q", got)
	}
	if got := f.requests.requests[answered.ID].Status; got != "answered" {
		t.Fatalf("an answered request must not be rewritten, got %q", got)
	}

	n, _ := f.svc.PendingCount(ctx)
	if n != 0 {
		t.Fatalf("pending count should be 0 after cancelling, got %d", n)
	}
}
