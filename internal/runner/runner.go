package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/git"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/notify"
	"github.com/Verhum/burnrate/internal/scheduling"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
	"github.com/Verhum/burnrate/prompts"
)

func CheckpointAndRemove(ctx context.Context, worktreePath, repoPath string, runID int64, logger *log.Logger) {
	if worktreePath == "" {
		return
	}
	// An empty repo_path identifies an agent-directed run: the worktreePath is an
	// agentwork/task-N dir (not a git repo), so it must be torn down with the
	// agent-workdir cleanup (detach child worktrees, keep on dirty) rather than
	// the managed-worktree checkpoint logic. All lifecycle call sites (task
	// delete, complete/dismiss, scheduler max-attempts) route through here.
	if repoPath == "" {
		cleanupAgentWorkdir(ctx, worktreePath, runID, logger)
		return
	}
	if _, err := os.Stat(worktreePath); err != nil {
		return
	}
	if err := git.Checkpoint(ctx, worktreePath, runID); err != nil {
		logger.Warnf("run %d: checkpoint failed: %v", runID, err)
		if git.HasUncommittedChanges(ctx, worktreePath) {
			logger.Warnf("run %d: keeping worktree %s (uncommitted changes would be lost)", runID, worktreePath)
			return
		}
	}
	git.RemoveWorktree(ctx, repoPath, worktreePath)
}

func cleanupAgentWorkdir(ctx context.Context, workdir string, runID int64, logger *log.Logger) {
	kept, err := git.RemoveAgentWorkdir(ctx, workdir, runID)
	switch {
	case errors.Is(err, git.ErrUnbackedWork):
		// The normal ending for a task with no repo: its deliverables are loose
		// files, and the next iteration of the task lands in this same path.
		logger.Infof("run %d: keeping agent workdir %s, no branch backs the work in it", runID, workdir)
	case kept != "":
		if kept == workdir {
			logger.Warnf("run %d: keeping agent workdir %s: %v", runID, workdir, err)
		} else {
			logger.Warnf("run %d: keeping agent workdir %s (%s: %v)", runID, workdir, kept, err)
		}
	case err != nil:
		logger.Warnf("run %d: removing agent workdir %s: %v", runID, workdir, err)
	}
}

type Params struct {
	Timeout       time.Duration
	WindowID      string
	OnRunMutation func(taskID int64)
}

// ErrSuspended is the cancellation cause the scheduler attaches when it stops a
// run because the session limit is spent. It travels via context.WithCancelCause
// so the run can tell "paused, resume next session" apart from "killed" or
// "errored" and record itself accordingly.
type ErrSuspended struct {
	// ResetAt is when the next session opens, if known.
	ResetAt time.Time
}

func (e *ErrSuspended) Error() string {
	if e.ResetAt.IsZero() {
		return "suspended until the next session"
	}
	return fmt.Sprintf("suspended until the next session at %s", e.ResetAt.UTC().Format(time.RFC3339))
}

// suspendCause reports the suspension that cancelled ctx, or nil.
func suspendCause(ctx context.Context) *ErrSuspended {
	var e *ErrSuspended
	if errors.As(context.Cause(ctx), &e) {
		return e
	}
	return nil
}

// ErrCancelled is the cancellation cause the scheduler attaches when a user
// manually cancels a run. It travels via context.WithCancelCause so the runner
// can distinguish "user asked to stop" from a daemon-level kill or a session
// suspend, and park the task in paused rather than auto-retrying it.
type ErrCancelled struct{}

func (e *ErrCancelled) Error() string { return "cancelled by user" }

func cancelledCause(ctx context.Context) bool {
	var e *ErrCancelled
	return errors.As(context.Cause(ctx), &e)
}

// settleInterrupted records the outcome for a run that stopped before it
// finished, and tears down the workspace. The branch is pushed (via
// Checkpoint) so the work is safe in Git and the worktree can be recreated
// on resume. Removing the worktree eagerly lets the user checkout the branch
// locally for testing without worktree conflicts.
//
// The recoverable/unrecoverable split lives in exactly one place
// (scheduling.TaskStatusAfterInterruption) so this and the daemon's
// reconciliation of orphaned runs cannot drift apart.
func settleInterrupted(ctx context.Context, st *store.Store, task store.Task, runID int64,
	hasSession, agentMode bool, worktreePath, repoPath string, logger *log.Logger) {

	status := scheduling.TaskStatusAfterInterruption(hasSession)
	st.SetTaskStatus(task.ID, status)
	if agentMode {
		cleanupAgentWorkdir(ctx, worktreePath, runID, logger)
	} else {
		CheckpointAndRemove(ctx, worktreePath, repoPath, runID, logger)
	}
}

func Run(ctx context.Context, st *store.Store, cfg config.Config, task store.Task, resume *store.Run, params Params, logger *log.Logger) error {
	agentMode := task.RepoPath == ""
	repoPath := task.RepoPath

	unconsumed, _ := st.UnconsumedComments(task.ID)
	comments := userComments(unconsumed)

	var run *store.Run
	var defaultBranch, worktreePath, branch string
	var attempt int
	var isFollowup bool
	var priorRun *store.Run

	if agentMode {
		worktreePath = filepath.Join(cfg.AgentWorkRoot(), fmt.Sprintf("task-%d", task.ID))

		if resume != nil {
			worktreePath = resume.WorktreePath
			branch = resume.Branch
			attempt = domain.EffectiveAttempt(task.AttemptResetRunID, resume) + 1
			priorRun = resume

			if err := os.MkdirAll(worktreePath, 0755); err != nil {
				return preflightError(st, task.ID, params.WindowID, worktreePath, "", "", cfg.MaxAttempts, fmt.Errorf("re-create agent workdir: %w", err), logger, params.OnRunMutation)
			}

			var err error
			run, err = st.CreateRun(task.ID, worktreePath, branch, "", params.WindowID, attempt)
			if err != nil {
				return fmt.Errorf("create resume run: %w", err)
			}
			if resume.SessionID != "" {
				st.SetRunSessionID(run.ID, resume.SessionID)
			}
			st.SetRunStatus(run.ID, "resuming")
			st.SetTaskStatus(task.ID, "running")
			notifyRunMutation(params.OnRunMutation, task.ID)
			logger.Infof("run %d: resuming agent-mode task %d (attempt %d) in %s", run.ID, task.ID, attempt, worktreePath)
		} else {
			if err := os.MkdirAll(worktreePath, 0755); err != nil {
				return preflightError(st, task.ID, params.WindowID, worktreePath, "", "", cfg.MaxAttempts, fmt.Errorf("create agent workdir: %w", err), logger, params.OnRunMutation)
			}

			if len(comments) > 0 {
				priorRun, _ = st.LatestRunForTask(task.ID)
			}

			attempt = 1
			var err error
			run, err = st.CreateRun(task.ID, worktreePath, "", "", params.WindowID, attempt)
			if err != nil {
				return fmt.Errorf("create run: %w", err)
			}
			st.SetTaskStatus(task.ID, "running")
			notifyRunMutation(params.OnRunMutation, task.ID)
			logger.Infof("run %d: starting agent-mode task %d in %s", run.ID, task.ID, worktreePath)
		}
	} else {
		if resume != nil {
			defaultBranch, _ = git.DefaultBranch(ctx, repoPath)
			if defaultBranch == "" {
				defaultBranch = "develop"
			}
			worktreePath = resume.WorktreePath
			branch = resume.Branch
			attempt = domain.EffectiveAttempt(task.AttemptResetRunID, resume) + 1

			if _, statErr := os.Stat(worktreePath); statErr != nil && branch != "" {
				if err := git.Fetch(ctx, repoPath); err != nil {
					logger.Warnf("run: fetch failed for %s: %v", repoPath, err)
				}
				if err := git.AddWorktreeFromBranch(ctx, repoPath, worktreePath, branch); err != nil {
					return preflightError(st, task.ID, params.WindowID, worktreePath, branch, repoPath, cfg.MaxAttempts, fmt.Errorf("re-create worktree for resume: %w", err), logger, params.OnRunMutation)
				}
				logger.Infof("run: re-created worktree %s from branch %s", worktreePath, branch)
			}

			var err error
			run, err = st.CreateRun(task.ID, worktreePath, branch, repoPath, params.WindowID, attempt)
			if err != nil {
				return fmt.Errorf("create resume run: %w", err)
			}
			if resume.SessionID != "" {
				st.SetRunSessionID(run.ID, resume.SessionID)
			}
			st.SetRunStatus(run.ID, "resuming")
			st.SetTaskStatus(task.ID, "running")
			notifyRunMutation(params.OnRunMutation, task.ID)
			logger.Infof("run %d: resuming task %d (attempt %d) in %s", run.ID, task.ID, attempt, worktreePath)
		} else {
			var err error
			defaultBranch, err = git.DefaultBranch(ctx, repoPath)
			if err != nil {
				return preflightError(st, task.ID, params.WindowID, "", "", repoPath, cfg.MaxAttempts, err, logger, params.OnRunMutation)
			}

			if err := git.Fetch(ctx, repoPath); err != nil {
				logger.Warnf("fetch failed for %s: %v", repoPath, err)
			}

			slug := git.Slug(task.Title)
			branch = fmt.Sprintf("burnrate/%d-%s", task.ID, slug)
			worktreePath = filepath.Join(cfg.WorktreeRoot, fmt.Sprintf("task-%d", task.ID))

			if len(comments) > 0 {
				latestRun, _ := st.LatestRunForTask(task.ID)
				if latestRun != nil && latestRun.Branch != "" && git.BranchExistsAtOrigin(ctx, repoPath, latestRun.Branch) {
					isFollowup = true
					branch = latestRun.Branch
					if err := git.AddWorktreeFromBranch(ctx, repoPath, worktreePath, branch); err != nil {
						return preflightError(st, task.ID, params.WindowID, worktreePath, branch, repoPath, cfg.MaxAttempts, fmt.Errorf("add worktree from branch: %w", err), logger, params.OnRunMutation)
					}
				}
			}

			if !isFollowup {
				if err := git.AddWorktree(ctx, repoPath, worktreePath, branch, defaultBranch); err != nil {
					return preflightError(st, task.ID, params.WindowID, worktreePath, branch, repoPath, cfg.MaxAttempts, fmt.Errorf("add worktree: %w", err), logger, params.OnRunMutation)
				}
			}

			attempt = 1
			run, err = st.CreateRun(task.ID, worktreePath, branch, repoPath, params.WindowID, attempt)
			if err != nil {
				git.RemoveWorktree(ctx, repoPath, worktreePath)
				return fmt.Errorf("create run: %w", err)
			}
			st.SetTaskStatus(task.ID, "running")
			notifyRunMutation(params.OnRunMutation, task.ID)
			if isFollowup {
				logger.Infof("run %d: follow-up on task %d in %s (branch %s)", run.ID, task.ID, worktreePath, branch)
			} else {
				logger.Infof("run %d: starting task %d in %s (branch %s)", run.ID, task.ID, worktreePath, branch)
			}
		}
	}

	attachmentFiles := copyAttachments(st, cfg, task.ID, worktreePath, logger)

	// Note: follow-up comments are marked consumed only when the run SUCCEEDS
	// (see classify). Consuming at launch would silently drop the follow-up if
	// the run died before doing any work (e.g. instant auth failure): the
	// comment would be tied to a dead run and neither re-included on the next
	// launch nor reconstructed on resume.
	var priorPRs []store.TaskPR
	if agentMode {
		priorPRs, _ = st.ListTaskPRs(task.ID)
	}

	prompt := buildPrompt(promptInput{
		task:            task,
		resume:          resume,
		isFollowup:      isFollowup,
		agentMode:       agentMode,
		defaultBranch:   defaultBranch,
		worktreePath:    worktreePath,
		branch:          branch,
		repoPath:        repoPath,
		baseCodeDir:     cfg.BaseCodeDir,
		comments:        comments,
		priorRun:        priorRun,
		priorPRs:        priorPRs,
		attachmentFiles: attachmentFiles,
	})

	// If the session we are resuming ended on a denied or interrupted tool call,
	// the last thing in its transcript is claude's "STOP ... wait for the user"
	// tool result. Say so explicitly, or the resumed agent obeys it again and
	// burns another window doing nothing.
	if resume != nil {
		if interrupted, msg := claude.LogEndedInterrupted(runLogPath(cfg, resume.ID)); interrupted {
			prompt += interruptedResumeNote(msg)
			logger.Infof("run %d: prior run %d ended on a denied/interrupted tool call; added continue guidance", run.ID, resume.ID)
		}
	}

	budget := 15
	if e, ok := cfg.SizeEstimates["medium"]; ok && e.BudgetUSD > 0 {
		budget = e.BudgetUSD
	}

	logPath := runLogPath(cfg, run.ID)
	// 0600, not 0644: a run transcript records every file the agent read, so it
	// is the most sensitive thing burnrate writes. Other local *users* are not in
	// the threat model, but there is no reason to hand them the contents either.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		logger.Warnf("run %d: failed to open log file: %v", run.ID, err)
	}
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()

	sessionID := ""
	if resume != nil {
		sessionID = resume.SessionID
	}

	extraEnv, err := resolveTokenEnv(cfg, logger)
	if err != nil {
		st.FinishRun(run.ID, "errored", 0, 0, "", err.Error(), "")
		if sessionID != "" {
			st.SetTaskStatus(task.ID, "resumable")
		} else {
			st.SetTaskStatus(task.ID, "queued")
		}
		notifyRunMutation(params.OnRunMutation, task.ID)
		logger.Errorf("run %d: token resolution failed: %v", run.ID, err)
		return fmt.Errorf("token resolution: %w", err)
	}

	// Lives in the data dir, not the workdir — see writeMCPConfig — and is torn
	// down with the run so {dataDir}/mcp does not accumulate one file per run.
	mcpConfigPath := writeMCPConfig(cfg.DataDir, cfg.Port, task.ID, run.ID, logger)
	if mcpConfigPath != "" {
		defer os.Remove(mcpConfigPath)
	}

	model := cfg.Model
	if task.Model != "" {
		model = task.Model
	}

	opts := claude.Options{
		Prompt:          prompt,
		Model:           model,
		BudgetUSD:       budget,
		ResumeSessionID: sessionID,
		MCPConfigPath:   mcpConfigPath,
		WorkDir:         worktreePath,
		Timeout:         params.Timeout,
		DryRun:          cfg.DryRun,
		ExtraEnv:        extraEnv,
		OnPID: func(pid int) {
			logger.Infof("run %d: claude pid=%d", run.ID, pid)
			st.SetRunPID(run.ID, pid)
		},
		OnSessionID: func(id string) {
			logger.Infof("run %d: session_id=%s", run.ID, id)
			st.SetRunSessionID(run.ID, id)
		},
		OnEvent: func(line string) {
			if logFile != nil {
				logFile.WriteString(line + "\n")
			}
		},
		OnDenial: func(message string) {
			logger.Warnf("run %d: tool use auto-denied: %s", run.ID, firstLine(message))
		},
	}

	st.SetRunStatus(run.ID, "running")
	// Record the requested model before the run rather than only after it, so a
	// run that dies without ever naming a model still lands in the right
	// cost-per-task series. The resolved model overwrites this below.
	st.SetRunModel(run.ID, model)
	notifyRunMutation(params.OnRunMutation, task.ID)

	// A denied tool call must not end the run: claude tells the model to "wait
	// for the user", and unattended there is no user, so the agent stops
	// mid-task and the window is spent for nothing. Nudge the same session to
	// keep going instead, within the run's remaining time.
	var deadline time.Time
	if params.Timeout > 0 {
		deadline = time.Now().Add(params.Timeout)
	}
	autoOpts := autoContinueOptions{
		Max:      cfg.MaxAutoContinue,
		Deadline: deadline,
		BuildPrompt: func(denial string) string {
			return buildContinuePrompt(task, agentMode, defaultBranch, worktreePath, branch, repoPath, denial)
		},
		OnContinue: func(n int, denial string) {
			logger.Warnf("run %d: auto-continuing session after denied tool call (%d/%d): %s",
				run.ID, n, cfg.MaxAutoContinue, firstLine(denial))
			st.SetRunStatus(run.ID, "resuming")
			notifyRunMutation(params.OnRunMutation, task.ID)
		},
	}

	result, continues, invokeErr := invokeWithAutoContinue(ctx, claude.Invoke, opts, autoOpts)

	// The session we meant to resume does not exist in this config dir — most
	// often because the account was switched after the session was created, so
	// its transcript sits under the previous CLAUDE_CONFIG_DIR. There is nothing
	// to reattach to, and retrying --resume fails identically in milliseconds:
	// left alone it spends the task's whole attempt budget in seconds and then
	// marks it failed. The prompt already carries the prior run's context, so
	// start over in a fresh session instead of losing the task.
	if claude.IsSessionNotFound(invokeErr) && opts.ResumeSessionID != "" {
		logger.Warnf("run %d: no transcript for session %s (config dir changed?); restarting fresh",
			run.ID, opts.ResumeSessionID)
		st.SetRunSessionID(run.ID, "")
		sessionID = ""
		opts.ResumeSessionID = ""
		st.SetRunStatus(run.ID, "running")
		notifyRunMutation(params.OnRunMutation, task.ID)

		var freshContinues int
		result, freshContinues, invokeErr = invokeWithAutoContinue(ctx, claude.Invoke, opts, autoOpts)
		continues += freshContinues
	}

	if continues > 0 {
		logger.Infof("run %d: issued %d auto-continue(s) after denied tool calls", run.ID, continues)
	}

	st.SetRunPID(run.ID, 0)

	// An alias ("opus") resolves to a concrete version, and a fallback can
	// substitute a different model outright; the analytics want what ran.
	if result.Model != "" && result.Model != model {
		st.SetRunModel(run.ID, result.Model)
	}

	// A resume run carries a session id from the prior attempt even if this
	// invocation died before re-emitting a system/init event (e.g. an
	// immediate rate limit). Treat that session as captured so the task stays
	// resumable rather than being marked failed (HR2: resume, never restart).
	priorSession := sessionID
	return classify(ctx, st, task, run, result, invokeErr, repoPath, defaultBranch, worktreePath, priorSession, agentMode, logger, params.OnRunMutation)
}

// minContinueWindow is the least time worth handing a continuation. Below this
// the resumed agent could not do meaningful work before the window closes, so
// the run is left resumable for the scheduler to pick up in the next window.
const minContinueWindow = 3 * time.Minute

type invokeFunc func(context.Context, claude.Options) (claude.Result, error)

type autoContinueOptions struct {
	Max         int
	Deadline    time.Time
	BuildPrompt func(denial string) string
	OnContinue  func(n int, denial string)
}

// invokeWithAutoContinue runs claude and, when the invocation ends because a
// tool call was auto-denied, resumes the same session with an explicit
// "keep going" prompt rather than surfacing a dead run. Cost and turns are
// accumulated across continuations so the run record reflects the whole effort.
//
// Returns the final result, how many continuations were issued, and the final
// error (nil when a continuation carried the run to completion).
func invokeWithAutoContinue(ctx context.Context, inv invokeFunc, opts claude.Options, ac autoContinueOptions) (claude.Result, int, error) {
	result, err := inv(ctx, opts)

	session := result.SessionID
	if session == "" {
		session = opts.ResumeSessionID
	}
	totalCost := result.CostUSD
	totalTurns := result.NumTurns
	model := result.Model
	continues := 0

	for claude.IsToolDenied(err) && continues < ac.Max {
		if session == "" || ctx.Err() != nil {
			break
		}
		if !ac.Deadline.IsZero() {
			remaining := time.Until(ac.Deadline)
			if remaining < minContinueWindow {
				break
			}
			opts.Timeout = remaining
		}

		denial := ""
		if d, ok := err.(*claude.ErrToolDenied); ok {
			denial = d.Message
		}
		continues++
		if ac.OnContinue != nil {
			ac.OnContinue(continues, denial)
		}

		opts.ResumeSessionID = session
		if ac.BuildPrompt != nil {
			opts.Prompt = ac.BuildPrompt(denial)
		}

		result, err = inv(ctx, opts)
		totalCost += result.CostUSD
		totalTurns += result.NumTurns
		if result.SessionID != "" {
			session = result.SessionID
		}
		if result.Model != "" {
			model = result.Model
		}
	}

	result.CostUSD = totalCost
	result.NumTurns = totalTurns
	// The final invocation may have died before re-emitting a system/init event;
	// keep the known session id and model so classify sees a resumable run and
	// the analytics still know which model spent the money.
	if result.SessionID == "" {
		result.SessionID = session
	}
	if result.Model == "" {
		result.Model = model
	}
	return result, continues, err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// userComments keeps only the comments a follow-up run should treat as
// instructions. A successful run posts its own final response as an
// agent-authored comment *after* marking the thread consumed, so that comment
// stays unconsumed forever — without this filter the next run would read the
// previous agent's report back to it as the user's follow-up, and parse its
// level-of-effort line as a directive.
func userComments(comments []store.Comment) []store.Comment {
	var out []store.Comment
	for _, c := range comments {
		if c.Author != "agent" {
			out = append(out, c)
		}
	}
	return out
}

func notifyRunMutation(fn func(int64), taskID int64) {
	if fn != nil {
		fn(taskID)
	}
}

func classify(ctx context.Context, st *store.Store, task store.Task, run *store.Run, result claude.Result, invokeErr error, repoPath, defaultBranch, worktreePath, priorSession string, agentMode bool, logger *log.Logger, onMutation func(int64)) error {
	hasSession := result.SessionID != "" || priorSession != ""

	switch {
	case invokeErr == nil:
		if agentMode {
			if isWaitingHuman(result.ResultText) {
				// The human may have answered in the window between the MCP
				// long-poll expiring and this classification. RequestService
				// .Respond only re-queues a task already sitting in
				// awaiting_human, and that status is written below — so a reply
				// that lands here would re-queue nothing and then be parked
				// behind an already-answered request, forever. Answered
				// requests and no pending ones means the reply is waiting: skip
				// the park and re-queue the way Respond would have.
				answeredInParkWindow := false
				if run.ID != 0 {
					total, pending, err := st.RequestCountsForRun(run.ID)
					switch {
					case err != nil:
						// Fail safe: an unavailable DB must never skip the park.
						logger.Warnf("run %d: could not read human requests, parking: %v", run.ID, err)
					case total > 0 && pending == 0:
						answeredInParkWindow = true
					}
				}

				st.FinishRun(run.ID, "succeeded", result.CostUSD, result.NumTurns, "", "", result.ResultText)
				// The follow-ups that fed this run are done with, like every
				// other terminal-success branch — but not when the human's
				// reply beat us here: MarkCommentsConsumed consumes every
				// unconsumed comment on the task, which would swallow that
				// reply before the re-queued run could read it.
				if !answeredInParkWindow {
					st.MarkCommentsConsumed(task.ID, run.ID)
				}
				if result.ResultText != "" {
					st.AddComment(task.ID, result.ResultText, "agent")
				}
				if parsed := ParseRunOutput(result.ResultText); parsed.Summary != "" {
					st.SetTaskSummary(task.ID, parsed.Summary)
				}
				if answeredInParkWindow {
					if hasSession {
						st.SetTaskStatus(task.ID, "resumable")
					} else {
						st.SetTaskStatus(task.ID, "queued")
					}
					st.ResetTaskAttempts(task.ID)
					notifyRunMutation(onMutation, task.ID)
					logger.Infof("run %d: the human replied while the run was finishing, so there is nothing left to wait for — re-queueing the task instead of parking it (cost=$%.2f, turns=%d)", run.ID, result.CostUSD, result.NumTurns)
					go cleanupAgentWorkdir(context.WithoutCancel(ctx), worktreePath, run.ID, logger)
					return nil
				}
				st.SetTaskStatus(task.ID, "awaiting_human")
				notifyRunMutation(onMutation, task.ID)
				notify.HumanRequest(task.ID, task.Title)
				logger.Infof("run %d: agent parked waiting for human (cost=$%.2f, turns=%d)", run.ID, result.CostUSD, result.NumTurns)
				go cleanupAgentWorkdir(context.WithoutCancel(ctx), worktreePath, run.ID, logger)
				return nil
			}

			// A task may span several repos; each reported result becomes its
			// own task PR row. The run's own columns record the primary one so
			// resume context and older single-PR views still work.
			results := parseResults(result.ResultText)
			primary := primaryResult(results)
			prURL := primary.PRURL
			st.FinishRun(run.ID, "succeeded", result.CostUSD, result.NumTurns, prURL, "", result.ResultText)
			if primary.Branch != "" {
				st.SetRunBranch(run.ID, primary.Branch)
			}
			if primary.Repo != "" {
				st.SetRunAgentRepo(run.ID, primary.Repo)
			}
			if primary.WorkedIn != "" {
				st.SetRunAgentWorkedIn(run.ID, primary.WorkedIn)
			}
			recordTaskPRs(st, task.ID, run.ID, results, logger)
			recordRunLines(ctx, st, task.ID, run.ID, results, "", logger)
			st.MarkCommentsConsumed(task.ID, run.ID)
			if result.ResultText != "" {
				st.AddComment(task.ID, result.ResultText, "agent")
			}
			if parsed := ParseRunOutput(result.ResultText); parsed.Summary != "" {
				st.SetTaskSummary(task.ID, parsed.Summary)
			}
			st.SetTaskStatus(task.ID, "pr_created")
			notifyRunMutation(onMutation, task.ID)
			fireReviewNotification(st, task, prURL, logger)
			logger.Infof("run %d: agent-mode succeeded (%d pr(s), primary=%s, cost=$%.2f, turns=%d)", run.ID, len(results), prURL, result.CostUSD, result.NumTurns)
			go cleanupAgentWorkdir(context.WithoutCancel(ctx), worktreePath, run.ID, logger)
			return nil
		}

		prURL := extractPRURL(result.ResultText)
		if prURL == "" {
			logger.Infof("run %d: no PR URL in output, attempting recovery push", run.ID)
			prURL = recoverSingleWorktree(ctx, worktreePath, run.Branch, defaultBranch, task.Title, logger)
		}
		st.FinishRun(run.ID, "succeeded", result.CostUSD, result.NumTurns, prURL, "", result.ResultText)
		if prURL != "" {
			managed := []AgentResult{{
				Repo:     git.OriginSlug(ctx, worktreePath),
				Branch:   run.Branch,
				PRURL:    prURL,
				WorkedIn: worktreePath,
			}}
			recordTaskPRs(st, task.ID, run.ID, managed, logger)
			recordRunLines(ctx, st, task.ID, run.ID, managed, defaultBranch, logger)
		}
		st.MarkCommentsConsumed(task.ID, run.ID)
		if result.ResultText != "" {
			st.AddComment(task.ID, result.ResultText, "agent")
		}
		if parsed := ParseRunOutput(result.ResultText); parsed.Summary != "" {
			st.SetTaskSummary(task.ID, parsed.Summary)
		}
		st.SetTaskStatus(task.ID, "pr_created")
		notifyRunMutation(onMutation, task.ID)
		fireReviewNotification(st, task, prURL, logger)
		logger.Infof("run %d: succeeded (pr=%s, cost=$%.2f, turns=%d)", run.ID, prURL, result.CostUSD, result.NumTurns)
		go CheckpointAndRemove(context.WithoutCancel(ctx), worktreePath, repoPath, run.ID, logger)
		return nil

	// Checked before every failure case: a suspend cancels the run, so whatever
	// error that produced — a bare context cancellation, or a timeout that
	// raced with it — must not be recorded as the run's own fault.
	case suspendCause(ctx) != nil:
		susp := suspendCause(ctx)
		st.FinishRun(run.ID, "rate_limited", result.CostUSD, result.NumTurns, "", susp.Error(), "")
		if !susp.ResetAt.IsZero() {
			st.SetRunRateLimitResetAt(run.ID, susp.ResetAt.UTC().Format(time.RFC3339))
		}
		// ctx is already cancelled, so any workspace teardown needs a live one.
		settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancelSettle()
		settleInterrupted(settleCtx, st, task, run.ID, hasSession, agentMode, worktreePath, repoPath, logger)
		notifyRunMutation(onMutation, task.ID)
		logger.Infof("run %d: suspended, will resume next session (cost=$%.2f, turns=%d)",
			run.ID, result.CostUSD, result.NumTurns)
		// Not an error: the run did what was asked of it and stopped.
		return nil

	case cancelledCause(ctx):
		st.FinishRun(run.ID, "errored", result.CostUSD, result.NumTurns, "", "cancelled by user", "")
		settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancelSettle()
		st.SetTaskStatus(task.ID, "paused")
		if agentMode {
			cleanupAgentWorkdir(settleCtx, worktreePath, run.ID, logger)
		} else {
			CheckpointAndRemove(settleCtx, worktreePath, repoPath, run.ID, logger)
		}
		notifyRunMutation(onMutation, task.ID)
		logger.Infof("run %d: cancelled by user, task paused (cost=$%.2f, turns=%d)",
			run.ID, result.CostUSD, result.NumTurns)
		return invokeErr

	case isRateLimited(invokeErr):
		st.FinishRun(run.ID, "rate_limited", result.CostUSD, result.NumTurns, "", invokeErr.Error(), "")
		if rl, ok := invokeErr.(*claude.ErrRateLimited); ok && !rl.ResetAt.IsZero() {
			st.SetRunRateLimitResetAt(run.ID, rl.ResetAt.UTC().Format(time.RFC3339))
		}
		settleInterrupted(ctx, st, task, run.ID, hasSession, agentMode, worktreePath, repoPath, logger)
		notifyRunMutation(onMutation, task.ID)
		logger.Warnf("run %d: rate limited: %v", run.ID, invokeErr)
		return invokeErr

	case claude.IsToolDenied(invokeErr):
		// Auto-continue is exhausted. Keep the session and worktree so the next
		// attempt resumes with the "you were auto-denied, keep going" guidance
		// rather than throwing the run away. Only a run that never got a session
		// id — which implies no tool call ever ran, so a denial is
		// near-impossible here — falls through to being failed.
		st.FinishRun(run.ID, "errored", result.CostUSD, result.NumTurns, "", invokeErr.Error(), result.ResultText)
		settleInterrupted(ctx, st, task, run.ID, hasSession, agentMode, worktreePath, repoPath, logger)
		notifyRunMutation(onMutation, task.ID)
		if hasSession {
			logger.Warnf("run %d: auto-denied tool call, auto-continue exhausted; task left resumable: %v", run.ID, invokeErr)
		} else {
			logger.Errorf("run %d: auto-denied tool call with no session to resume: %v", run.ID, invokeErr)
		}
		return invokeErr

	case isTimeout(invokeErr):
		st.FinishRun(run.ID, "timed_out", result.CostUSD, result.NumTurns, "", invokeErr.Error(), "")
		settleInterrupted(ctx, st, task, run.ID, hasSession, agentMode, worktreePath, repoPath, logger)
		notifyRunMutation(onMutation, task.ID)
		logger.Warnf("run %d: timed out: %v", run.ID, invokeErr)
		return invokeErr

	default:
		st.FinishRun(run.ID, "errored", result.CostUSD, result.NumTurns, "", invokeErr.Error(), "")
		settleInterrupted(ctx, st, task, run.ID, hasSession, agentMode, worktreePath, repoPath, logger)
		notifyRunMutation(onMutation, task.ID)
		logger.Errorf("run %d: errored: %v", run.ID, invokeErr)
		return invokeErr
	}
}

type promptInput struct {
	task            store.Task
	resume          *store.Run
	isFollowup      bool
	agentMode       bool
	defaultBranch   string
	worktreePath    string
	branch          string
	repoPath        string
	baseCodeDir     string
	comments        []store.Comment
	priorRun        *store.Run
	priorPRs        []store.TaskPR
	attachmentFiles []string
}

func buildPrompt(in promptInput) string {
	task, agentMode := in.task, in.agentMode

	var sb strings.Builder
	if agentMode {
		sb.WriteString(prompts.WorkerAgent)
	} else if in.isFollowup {
		sb.WriteString(prompts.WorkerFollowup)
	} else if in.resume != nil {
		sb.WriteString(prompts.WorkerResume)
	} else {
		sb.WriteString(prompts.WorkerNew)
	}

	sb.WriteString("\n\n## Your Task\n\n")
	fmt.Fprintf(&sb, "- **ID**: %d\n", task.ID)
	fmt.Fprintf(&sb, "- **Title**: %s\n", task.Title)

	if len(in.comments) > 0 {
		sb.WriteString("\n## Follow-up Instructions\n\n")
		for i, c := range in.comments {
			fmt.Fprintf(&sb, "### Follow-up #%d (%s)\n\n%s\n\n", i+1, c.CreatedAt, c.Body)
		}
	}

	if agentMode {
		sb.WriteString("\n## Runtime Context\n\n")
		fmt.Fprintf(&sb, "- WORKDIR: `%s`\n", in.worktreePath)
		if in.baseCodeDir != "" {
			fmt.Fprintf(&sb, "- BASE_CODE_DIR: `%s`\n", in.baseCodeDir)
		}
		// Prior PRs come from task_prs, which spans every repo the task has
		// touched across all attempts — the run's own columns only carry the
		// primary one.
		if len(in.priorPRs) > 0 {
			// A repo-less iteration records a row with only worked_in set, so its
			// work lives in the workdir and nowhere else — promising branches and
			// PRs for it sends this run hunting for things that do not exist.
			var pushed []store.TaskPR
			for _, p := range in.priorPRs {
				if p.Branch != "" || p.PRURL != "" {
					pushed = append(pushed, p)
				}
			}
			sb.WriteString("\n## Prior Run Context\n\n")
			if len(pushed) > 0 {
				sb.WriteString("You previously worked on this task and already produced the following. Do NOT redo completed work; push follow-up commits to these branches and reuse the existing PRs.\n\n")
				for _, p := range pushed {
					fmt.Fprintf(&sb, "- REPO: `%s` | BRANCH: `%s` | PR: %s | WORKED_IN: `%s`\n",
						orDash(p.Repo), orDash(p.Branch), orDash(p.PRURL), orDash(p.WorkedIn))
				}
			} else {
				sb.WriteString("You previously worked on this task. Do NOT redo completed work.\n")
			}
			if len(pushed) < len(in.priorPRs) {
				sb.WriteString(unpushedWorkNote(in.worktreePath))
			}
		} else if pr := in.priorRun; pr != nil && (pr.AgentWorkedIn != "" || pr.AgentRepo != "" || pr.Branch != "" || pr.PRURL != "") {
			sb.WriteString("\n## Prior Run Context\n\n")
			sb.WriteString("You previously worked on this task. Do NOT redo completed work.\n\n")
			if pr.AgentWorkedIn != "" {
				fmt.Fprintf(&sb, "- WORKED_IN: `%s`\n", pr.AgentWorkedIn)
			}
			if pr.AgentRepo != "" {
				fmt.Fprintf(&sb, "- REPO: `%s`\n", pr.AgentRepo)
			}
			if pr.Branch != "" {
				fmt.Fprintf(&sb, "- BRANCH: `%s`\n", pr.Branch)
			}
			if pr.PRURL != "" {
				fmt.Fprintf(&sb, "- PR: %s\n", pr.PRURL)
			}
			if pr.Branch == "" && pr.PRURL == "" {
				sb.WriteString(unpushedWorkNote(in.worktreePath))
			}
		}
	} else {
		sb.WriteString("\n## Runtime Context\n\n")
		fmt.Fprintf(&sb, "- REPO_PATH: `%s`\n", in.repoPath)
		fmt.Fprintf(&sb, "- DEFAULT_BRANCH: `%s`\n", in.defaultBranch)
		fmt.Fprintf(&sb, "- WORKTREE_PATH: `%s`\n", in.worktreePath)
		fmt.Fprintf(&sb, "- BRANCH: `%s`\n", in.branch)
		if in.baseCodeDir != "" {
			fmt.Fprintf(&sb, "- BASE_CODE_DIR: `%s`\n", in.baseCodeDir)
		}
	}

	attachmentFiles := in.attachmentFiles
	if len(attachmentFiles) > 0 {
		sb.WriteString("\n## Image Attachments\n\n")
		sb.WriteString("The following image files have been placed in your working directory. Use the Read tool to view them:\n\n")
		for _, f := range attachmentFiles {
			fmt.Fprintf(&sb, "- `%s`\n", f)
		}
	}

	// How thoroughly to carry the task: default 3, or whatever level the task
	// description or a follow-up comment asked for.
	level, explicit := resolveEffortLevel(task.Prompt, in.comments)
	sb.WriteString("\n")
	sb.WriteString(effortSection(level, explicit))

	sb.WriteString("\n## Task Data\n\n```json\n")
	taskForPrompt := task
	taskForPrompt.Summary = ""
	taskJSON, _ := json.MarshalIndent(taskForPrompt, "", "  ")
	sb.Write(taskJSON)
	sb.WriteString("\n```\n")

	// Last section, so it is the freshest instruction in context: an unattended
	// worker must never stop to wait for approval that cannot arrive.
	sb.WriteString("\n")
	sb.WriteString(prompts.DenialPolicy)

	return sb.String()
}

// unpushedWorkNote points a follow-up run at deliverables an earlier iteration
// left loose in the workdir, which survives a run because nothing else holds
// them. It is empty unless they are really still there — a claim the agent
// cannot check cheaply is worse than no claim.
func unpushedWorkNote(workdir string) string {
	if !workdirHasAgentFiles(workdir) {
		return ""
	}
	return "\nThe files that earlier work produced are still in your working directory. Build on them: read what is there and edit it in place. Do NOT start over from scratch.\n"
}

// workdirHasAgentFiles reports whether the workdir holds anything the agent put
// there. `attachments/` is burnrate's own staging (copyAttachments), so it is not
// evidence of prior work.
func workdirHasAgentFiles(workdir string) bool {
	if workdir == "" {
		return false
	}
	entries, err := os.ReadDir(workdir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		switch e.Name() {
		case ".DS_Store", "attachments":
			continue
		}
		return true
	}
	return false
}

func runLogPath(cfg config.Config, runID int64) string {
	return filepath.Join(cfg.DataDir, "logs", fmt.Sprintf("run-%d.jsonl", runID))
}

// interruptedResumeNote warns a resuming agent that its transcript ends with a
// bogus "wait for the user" tool result.
func interruptedResumeNote(denial string) string {
	var sb strings.Builder
	sb.WriteString("\n## Your Previous Session Ended On A Denied Tool Call\n\n")
	sb.WriteString("The last thing in your transcript is this tool result:\n\n> ")
	sb.WriteString(firstLine(denial))
	sb.WriteString("\n\nThat was burnrate's permission layer (or an interrupted call), **not a human**. ")
	sb.WriteString("Do not wait for anyone and do not restart the task — pick up where you left off, ")
	sb.WriteString("route around the blocked call, and finish the run.\n")
	return sb.String()
}

// buildContinuePrompt is the prompt for an in-run auto-continue: the session is
// resumed, so it carries only the nudge plus the minimum context needed to land
// the work.
func buildContinuePrompt(task store.Task, agentMode bool, defaultBranch, worktreePath, branch, repoPath, denial string) string {
	var sb strings.Builder
	sb.WriteString(prompts.WorkerContinue)

	if denial != "" {
		sb.WriteString("\n## The Denied Call\n\n> ")
		sb.WriteString(firstLine(denial))
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Task\n\n")
	fmt.Fprintf(&sb, "- **ID**: %d\n", task.ID)
	fmt.Fprintf(&sb, "- **Title**: %s\n", task.Title)

	sb.WriteString("\n## Runtime Context\n\n")
	level, explicit := resolveEffortLevel(task.Prompt, nil)
	sb.WriteString(effortLine(level, explicit))
	if agentMode {
		fmt.Fprintf(&sb, "- WORKDIR: `%s`\n", worktreePath)
		sb.WriteString("\nEnd your final message with the required trailers:\n\n")
		sb.WriteString("```\nWORKED_IN: <absolute path where you worked>\nREPO: <owner/name or none>\nBRANCH: <branch name or none>\nPR: <PR URL or none>\n```\n")
	} else {
		fmt.Fprintf(&sb, "- REPO_PATH: `%s`\n", repoPath)
		fmt.Fprintf(&sb, "- DEFAULT_BRANCH: `%s`\n", defaultBranch)
		fmt.Fprintf(&sb, "- WORKTREE_PATH: `%s`\n", worktreePath)
		fmt.Fprintf(&sb, "- BRANCH: `%s`\n", branch)
		sb.WriteString("\nFinish by pushing `")
		sb.WriteString(branch)
		sb.WriteString("` and printing the draft PR URL on the last line of your output.\n")
	}

	sb.WriteString("\n")
	sb.WriteString(prompts.DenialPolicy)

	return sb.String()
}

func copyAttachments(st *store.Store, cfg config.Config, taskID int64, worktreePath string, logger *log.Logger) []string {
	attachments, err := st.ListAttachments(taskID)
	if err != nil || len(attachments) == 0 {
		return nil
	}

	srcDir := filepath.Join(cfg.DataDir, "attachments", fmt.Sprintf("task-%d", taskID))
	dstDir := filepath.Join(worktreePath, "attachments")
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		logger.Warnf("task %d: failed to create attachments dir: %v", taskID, err)
		return nil
	}

	var files []string
	for _, a := range attachments {
		storedName := fmt.Sprintf("%d-%s", a.ID, a.Filename)
		src := filepath.Join(srcDir, storedName)
		dst := filepath.Join(dstDir, a.Filename)

		data, err := os.ReadFile(src)
		if err != nil {
			logger.Warnf("task %d: failed to read attachment %s: %v", taskID, a.Filename, err)
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			logger.Warnf("task %d: failed to copy attachment %s: %v", taskID, a.Filename, err)
			continue
		}
		files = append(files, "attachments/"+a.Filename)
	}
	return files
}

type AgentTrailers struct {
	WorkedIn string
	Repo     string
	Branch   string
	PR       string
}

var (
	trailerWorkedInRe = regexp.MustCompile(`(?m)^WORKED_IN:[ \t]*(.+?)[ \t]*$`)
	trailerRepoRe     = regexp.MustCompile(`(?m)^REPO:[ \t]*(.+?)[ \t]*$`)
	trailerBranchRe   = regexp.MustCompile(`(?m)^BRANCH:[ \t]*(.+?)[ \t]*$`)
	trailerPRRe       = regexp.MustCompile(`(?m)^PR:[ \t]*(.+?)[ \t]*$`)
)

// lastTrailer returns the captured value of the LAST line matching re. The
// agent may echo "REPO: foo" style lines in its narration; the authoritative
// values are the trailer block at the very end of the final message, so we
// always prefer the final occurrence.
func lastTrailer(re *regexp.Regexp, text string) string {
	matches := re.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

func parseTrailers(text string) AgentTrailers {
	t := AgentTrailers{
		WorkedIn: lastTrailer(trailerWorkedInRe, text),
		Repo:     lastTrailer(trailerRepoRe, text),
		Branch:   lastTrailer(trailerBranchRe, text),
		PR:       lastTrailer(trailerPRRe, text),
	}
	if t.WorkedIn == "none" {
		t.WorkedIn = ""
	}
	if t.Repo == "none" {
		t.Repo = ""
	}
	if t.Branch == "none" {
		t.Branch = ""
	}
	if t.PR == "none" {
		t.PR = ""
	}
	return t
}

// AgentResult is one repo's outcome from an agent-directed run. A task that
// spans several repos reports one of these per repo.
type AgentResult struct {
	Repo     string
	Branch   string
	PRURL    string
	WorkedIn string
}

var waitingHumanRe = regexp.MustCompile(`(?m)^RESULT:[ \t]*WAITING_HUMAN\b`)

func isWaitingHuman(text string) bool {
	return waitingHumanRe.MatchString(text)
}

// writeMCPConfig writes the human-loop MCP server config for one run and
// returns its path ("" when it could not be written).
//
// The file lives under {dataDir}/mcp, never in the run's workdir. A config
// dropped into the workdir is an untracked file the agent never created: it
// made git.RemoveAgentWorkdir's hasFiles() permanently true, so agent workdirs
// were never cleaned up, and it got checkpoint-committed and pushed into user
// branches. The caller removes the file when the run finishes.
//
// The server "type" must be "http": the claude CLI silently skips servers whose
// type it does not recognise, so "streamableHttp" registered no server at all
// and reported no error.
func writeMCPConfig(dataDir string, port int, taskID, runID int64, logger *log.Logger) string {
	warnf := func(format string, args ...any) {
		if logger != nil {
			logger.Warnf(format, args...)
		}
	}
	// Defensive: config.EnsureDirs also creates this, but runs driven by tests
	// (and by a data dir created before this key existed) may not have it.
	dir := filepath.Join(dataDir, "mcp")
	if err := os.MkdirAll(dir, 0755); err != nil {
		warnf("run %d: failed to create MCP config dir: %v", runID, err)
		return ""
	}
	configPath := filepath.Join(dir, fmt.Sprintf("task-%d-run-%d.json", taskID, runID))
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d/mcp?task=%d&run=%d", port, taskID, runID)
	config := map[string]any{
		"mcpServers": map[string]any{
			"burnrate-human-loop": map[string]any{
				"type": "http",
				"url":  mcpURL,
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		warnf("run %d: failed to marshal MCP config: %v", runID, err)
		return ""
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		warnf("run %d: failed to write MCP config: %v", runID, err)
		return ""
	}
	return configPath
}

var resultRe = regexp.MustCompile(`(?m)^RESULT:[ \t]*(.+?)[ \t]*$`)

// parseResults reads the pipe-separated `RESULT:` lines an agent emits — one
// per repo — falling back to the single WORKED_IN/REPO/BRANCH/PR trailer block
// for single-repo runs (and for sessions resumed from before multi-repo
// support).
func parseResults(text string) []AgentResult {
	var results []AgentResult
	seen := map[string]bool{}

	for _, m := range resultRe.FindAllStringSubmatch(text, -1) {
		fields := strings.Split(m[1], "|")
		r := AgentResult{
			Repo:     resultField(fields, 0),
			Branch:   resultField(fields, 1),
			PRURL:    resultField(fields, 2),
			WorkedIn: resultField(fields, 3),
		}
		if r.Repo == "" && r.Branch == "" && r.PRURL == "" {
			continue
		}
		key := r.Repo + "\x00" + r.Branch + "\x00" + r.PRURL
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, r)
	}
	if len(results) > 0 {
		return results
	}

	t := parseTrailers(text)
	if t.Repo == "" && t.Branch == "" && t.PR == "" && t.WorkedIn == "" {
		return nil
	}
	return []AgentResult{{Repo: t.Repo, Branch: t.Branch, PRURL: t.PR, WorkedIn: t.WorkedIn}}
}

func resultField(fields []string, i int) string {
	if i >= len(fields) {
		return ""
	}
	v := strings.TrimSpace(fields[i])
	v = strings.Trim(v, "`")
	// Drop the unfilled placeholders of the instruction template ("<branch>")
	// and the explicit not-applicable markers.
	if v == "none" || v == "-" || (strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">")) {
		return ""
	}
	return v
}

// primaryResult picks the result recorded on the run row itself: the first one
// that actually opened a PR, else the first reported.
func primaryResult(results []AgentResult) AgentResult {
	for _, r := range results {
		if r.PRURL != "" {
			return r
		}
	}
	if len(results) > 0 {
		return results[0]
	}
	return AgentResult{}
}

// recordRunLines measures how much code the run's branches actually contain and
// splits the answer two ways: the branch total goes on the task PR row, and only
// what is new since the last measurement is attributed to this run. That is what
// keeps cost-per-line honest when a followup run re-measures a branch an earlier
// run already built (see migration 011).
//
// Measurement is best-effort. A worktree the agent has since removed, a branch
// with no upstream, a repo whose default branch cannot be resolved — none of
// these are worth failing a successful run over, so they are logged and the run
// keeps a zero line count. A zero count does not remove the run's cost from the
// analytics; it correctly shows up as spend that bought no measurable code.
//
// base may be empty, in which case each worktree's own default branch is used.
func recordRunLines(ctx context.Context, st *store.Store, taskID, runID int64, results []AgentResult, base string, logger *log.Logger) {
	var added, removed int
	for _, r := range results {
		if r.WorkedIn == "" || r.Branch == "" {
			continue
		}
		stat, err := git.BranchDiffStat(ctx, r.WorkedIn, base)
		if err != nil {
			logger.Warnf("run %d: could not measure lines for %s@%s: %v", runID, orDash(r.Repo), r.Branch, err)
			continue
		}
		dAdded, dRemoved, err := st.RecordTaskPRLines(taskID, r.Repo, r.Branch, stat.Added, stat.Removed)
		if err != nil {
			logger.Warnf("run %d: could not record lines for %s@%s: %v", runID, orDash(r.Repo), r.Branch, err)
			continue
		}
		added += dAdded
		removed += dRemoved
	}
	if added == 0 && removed == 0 {
		return
	}
	if err := st.SetRunLines(runID, added, removed); err != nil {
		logger.Warnf("run %d: could not store line counts: %v", runID, err)
		return
	}
	logger.Infof("run %d: +%d/-%d lines across %d branch(es)", runID, added, removed, len(results))
}

func recordTaskPRs(st *store.Store, taskID, runID int64, results []AgentResult, logger *log.Logger) {
	for _, r := range results {
		if err := st.UpsertTaskPR(taskID, runID, r.Repo, r.Branch, r.PRURL, r.WorkedIn); err != nil {
			logger.Warnf("run %d: failed to record PR for %s: %v", runID, r.Repo, err)
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

var prURLRe = regexp.MustCompile(`https://github\.com/[^\s]+/pull/\d+`)

func extractPRURL(text string) string {
	matches := prURLRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func recoverSingleWorktree(ctx context.Context, worktree, branch, defaultBranch, title string, logger *log.Logger) string {
	has, err := git.HasUnpushedCommits(ctx, worktree, defaultBranch)
	if err != nil {
		return ""
	}
	if !has {
		existing, _ := git.Run(ctx, worktree, "gh", "pr", "view", branch, "--json", "url", "-q", ".url")
		if strings.HasPrefix(strings.TrimSpace(existing), "https://") {
			return strings.TrimSpace(existing)
		}
		return ""
	}

	if err := git.PushBranch(ctx, worktree, branch); err != nil {
		logger.Warnf("recovery push failed: %v", err)
		return ""
	}

	prURL, err := createPR(ctx, worktree, branch, defaultBranch, title)
	if err != nil {
		logger.Warnf("recovery PR creation failed: %v", err)
		return ""
	}
	return prURL
}

func createPR(ctx context.Context, dir, branch, baseBranch, title string) (string, error) {
	existing, _ := git.Run(ctx, dir, "gh", "pr", "view", branch, "--json", "url", "-q", ".url")
	if strings.HasPrefix(strings.TrimSpace(existing), "https://") {
		return strings.TrimSpace(existing), nil
	}

	out, err := git.Run(ctx, dir, "gh", "pr", "create",
		"--draft",
		"--head", branch,
		"--base", baseBranch,
		"--title", title,
		"--body", "Automated PR by burnrate.",
	)
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w (%s)", err, out)
	}
	url := strings.TrimSpace(out)
	if strings.HasPrefix(url, "https://") {
		return url, nil
	}
	return "", fmt.Errorf("unexpected gh output: %s", url)
}

// resolveTokenEnv builds the env slice for a spawned claude process. When a
// pinned account is configured, it resolves the token bundle, refreshes if
// near-expiry, and injects CLAUDE_CODE_OAUTH_TOKEN so the child is
// authenticated without needing keychain access.
//
// Mid-run expiry (runs >1h) is accepted: the run dies, is classified
// resumable, and the next launch injects a fresh token.
func resolveTokenEnv(cfg config.Config, logger *log.Logger) ([]string, error) {
	if cfg.ClaudeConfigDir == "" {
		return nil, nil
	}

	env := []string{"CLAUDE_CONFIG_DIR=" + cfg.ClaudeConfigDir}

	b, err := usage.BundleForAccount(cfg.ClaudeConfigDir, cfg.SandboxKeychain, cfg.SandboxKeychainPasswordFile)
	if err != nil {
		return nil, fmt.Errorf("resolve token bundle: %w", err)
	}

	tok, refreshed, err := usage.EnsureFresh(b)
	if err != nil {
		return nil, fmt.Errorf("ensure fresh token: %w", err)
	}
	if refreshed {
		logger.Infof("token refreshed for config dir (expires in %s)",
			formatExpiry(usage.ExpiresTime(b)))
	}

	env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+tok)
	return env, nil
}

func formatExpiry(exp time.Time) string {
	if exp.IsZero() {
		return "unknown"
	}
	d := time.Until(exp)
	if d < 0 {
		return "expired"
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func isRateLimited(err error) bool {
	_, ok := err.(*claude.ErrRateLimited)
	return ok
}

func isTimeout(err error) bool {
	_, ok := err.(*claude.ErrTimeout)
	return ok
}

func fireReviewNotification(st *store.Store, task store.Task, prURL string, logger *log.Logger) {
	// Gating is read synchronously so a UI toggle takes effect immediately.
	v, ok := st.GetSetting("notify_on_review")
	if ok && (v == "false" || v == "0") {
		return
	}
	displayID := fmt.Sprintf("BR%d", task.ID)
	// Best-effort: never block the runner. Fire off the run goroutine so
	// classify returns promptly.
	go func() {
		if err := notify.Review(displayID, task.Title, prURL); err != nil {
			logger.Warnf("review notification failed: %v", err)
		}
	}()
}

// preflightError records a pre-flight failure (before the run could start) as a
// visible errored run row and re-queues the task so a transient misconfig
// (e.g. a not-yet-fetched repo) can retry on the next poll. Pre-flight errored
// runs never carry a session id, so they are never picked up as resume
// candidates and the scheduler's resume-based max-attempt cap can never fire on
// them; without a bound here a persistently misconfigured task would loop
// forever, one errored run row per poll. We therefore increment the attempt
// across successive pre-flight failures and mark the task failed once it reaches
// maxAttempts, matching the spec: persistent misconfig surfaces as a failed task
// with a readable error.
func preflightError(st *store.Store, taskID int64, windowID, worktreePath, branch, repoPath string, maxAttempts int, err error, logger *log.Logger, onMutation func(int64)) error {
	var resetRunID int64
	if t, err := st.GetTask(taskID); err == nil {
		resetRunID = t.AttemptResetRunID
	}
	prior, _ := st.LatestRunForTask(taskID)
	attempt := domain.EffectiveAttempt(resetRunID, prior) + 1
	run, createErr := st.CreateRun(taskID, worktreePath, branch, repoPath, windowID, attempt)
	if createErr != nil {
		logger.Errorf("task %d: pre-flight failed and could not create run row: %v (original: %v)", taskID, createErr, err)
		return err
	}
	st.FinishRun(run.ID, "errored", 0, 0, "", err.Error(), "")
	if maxAttempts > 0 && attempt >= maxAttempts {
		st.SetTaskStatus(taskID, "failed")
		logger.Errorf("run %d: pre-flight error for task %d (attempt %d/%d), marking failed: %v", run.ID, taskID, attempt, maxAttempts, err)
	} else {
		st.SetTaskStatus(taskID, "queued")
		logger.Warnf("run %d: pre-flight error for task %d (attempt %d): %v", run.ID, taskID, attempt, err)
	}
	notifyRunMutation(onMutation, taskID)
	return err
}
