package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/domain"
)

type RunService struct {
	tasks domain.TaskRepository
	runs  domain.RunRepository
	sched SchedulerGate
	cfg   RunServiceConfig
}

type RunServiceConfig struct {
	DataDir         string
	ClaudeConfigDir string
	// TokenCommand is substituted into a resume command to authenticate a pinned
	// account. See claude.ResumeTarget.TokenCommand.
	TokenCommand string
}

// RunResume is everything a human needs to reattach to a run's Claude session
// from a terminal. Command is empty when the run recorded no session, which is
// the same condition that makes its task unresumable by the daemon.
type RunResume struct {
	RunID           int64  `json:"run_id"`
	SessionID       string `json:"session_id"`
	WorktreePath    string `json:"worktree_path"`
	ClaudeConfigDir string `json:"claude_config_dir"`
	Command         string `json:"command"`
}

type RunEvent struct {
	Type         string          `json:"type"`
	SessionID    string          `json:"session_id,omitempty"`
	Model        string          `json:"model,omitempty"`
	Text         string          `json:"text,omitempty"`
	ToolName     string          `json:"tool_name,omitempty"`
	InputSummary string          `json:"input_summary,omitempty"`
	InputFull    json.RawMessage `json:"input_full,omitempty"`
	Output       string          `json:"output,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	NumTurns     int             `json:"num_turns,omitempty"`
	Duration     int             `json:"duration,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	Message      string          `json:"message,omitempty"`
	Raw          string          `json:"raw,omitempty"`
}

func NewRunService(
	tasks domain.TaskRepository,
	runs domain.RunRepository,
	sched SchedulerGate,
	cfg RunServiceConfig,
) *RunService {
	return &RunService{tasks: tasks, runs: runs, sched: sched, cfg: cfg}
}

func (s *RunService) ListRuns(ctx context.Context, taskID int64, limit int) ([]domain.Run, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.runs.ListRuns(taskID, limit)
}

func (s *RunService) CancelRun(ctx context.Context, runID int64) (string, error) {
	runs, err := s.runs.ListRuns(0, 1000)
	if err != nil {
		return "", err
	}
	var run *domain.Run
	for _, r := range runs {
		if r.ID == runID {
			run = &r
			break
		}
	}
	if run == nil {
		return "", &NotFoundError{Entity: "run", ID: runID}
	}
	if run.Status != "starting" && run.Status != "running" && run.Status != "resuming" {
		return "", &ConflictError{Message: "run is not active"}
	}
	if s.sched.CancelTask(run.TaskID) {
		return "cancelling", nil
	}
	s.runs.FinishRun(runID, "abandoned", run.CostUSD, run.NumTurns, run.PRURL, "cancelled by user", "")
	s.tasks.SetTaskStatus(run.TaskID, "paused")
	return "cancelled", nil
}

func (s *RunService) ResumeInfo(ctx context.Context, runID int64) (RunResume, error) {
	run, err := s.runs.GetRun(runID)
	if err != nil {
		return RunResume{}, &NotFoundError{Entity: "run", ID: runID}
	}
	return RunResume{
		RunID:           run.ID,
		SessionID:       run.SessionID,
		WorktreePath:    run.WorktreePath,
		ClaudeConfigDir: s.cfg.ClaudeConfigDir,
		Command: claude.ResumeCommand(claude.ResumeTarget{
			SessionID:    run.SessionID,
			WorkDir:      run.WorktreePath,
			ConfigDir:    s.cfg.ClaudeConfigDir,
			TokenCommand: s.cfg.TokenCommand,
		}),
	}, nil
}

func (s *RunService) GetRunLogPath(runID int64) string {
	return fmt.Sprintf("%s/logs/run-%d.jsonl", s.cfg.DataDir, runID)
}
