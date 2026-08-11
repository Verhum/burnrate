package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

func denied(msg string) error { return &claude.ErrToolDenied{Message: msg, Denials: 1} }

// scriptedInvoke returns an invokeFunc that plays back the given outcomes in
// order, recording the options it was called with.
func scriptedInvoke(outcomes []struct {
	res claude.Result
	err error
}, calls *[]claude.Options) invokeFunc {
	i := 0
	return func(_ context.Context, opts claude.Options) (claude.Result, error) {
		*calls = append(*calls, opts)
		if i >= len(outcomes) {
			return claude.Result{}, errors.New("invoked more times than scripted")
		}
		o := outcomes[i]
		i++
		return o.res, o.err
	}
}

type outcome = struct {
	res claude.Result
	err error
}

func TestAutoContinue_RecoversFromDenial(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{
		{claude.Result{SessionID: "sess-1", CostUSD: 1.5, NumTurns: 20}, denied("blocked bash")},
		{claude.Result{SessionID: "sess-1", CostUSD: 2.5, NumTurns: 10, ResultText: "PR: https://github.com/o/r/pull/7"}, nil},
	}, &calls)

	result, continues, err := invokeWithAutoContinue(context.Background(), inv,
		claude.Options{Prompt: "original"},
		autoContinueOptions{
			Max:         3,
			Deadline:    time.Now().Add(time.Hour),
			BuildPrompt: func(denial string) string { return "continue because: " + denial },
		})

	if err != nil {
		t.Fatalf("expected the continuation to clear the error, got %v", err)
	}
	if continues != 1 {
		t.Fatalf("continues = %d, want 1", continues)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 invocations, got %d", len(calls))
	}
	if calls[1].ResumeSessionID != "sess-1" {
		t.Errorf("continuation should resume the session, got %q", calls[1].ResumeSessionID)
	}
	if !strings.Contains(calls[1].Prompt, "continue because: blocked bash") {
		t.Errorf("continuation prompt not applied: %q", calls[1].Prompt)
	}
	if result.CostUSD != 4.0 {
		t.Errorf("cost should accumulate across continuations, got %v", result.CostUSD)
	}
	if result.NumTurns != 30 {
		t.Errorf("turns should accumulate across continuations, got %d", result.NumTurns)
	}
	if result.ResultText == "" {
		t.Error("final result text should come from the last invocation")
	}
}

func TestAutoContinue_StopsAtMax(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{
		{claude.Result{SessionID: "s"}, denied("d1")},
		{claude.Result{SessionID: "s"}, denied("d2")},
		{claude.Result{SessionID: "s"}, denied("d3")},
		{claude.Result{SessionID: "s"}, denied("d4")},
	}, &calls)

	_, continues, err := invokeWithAutoContinue(context.Background(), inv,
		claude.Options{}, autoContinueOptions{Max: 2, BuildPrompt: func(string) string { return "go on" }})

	if continues != 2 {
		t.Fatalf("continues = %d, want 2", continues)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 1 initial + 2 continuations, got %d calls", len(calls))
	}
	if !claude.IsToolDenied(err) {
		t.Fatalf("exhausted auto-continue must surface the denial, got %v", err)
	}
}

func TestAutoContinue_DisabledByZeroMax(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{{claude.Result{SessionID: "s"}, denied("d")}}, &calls)

	_, continues, err := invokeWithAutoContinue(context.Background(), inv, claude.Options{}, autoContinueOptions{Max: 0})
	if continues != 0 || len(calls) != 1 {
		t.Fatalf("expected no continuations, got %d (calls=%d)", continues, len(calls))
	}
	if !claude.IsToolDenied(err) {
		t.Fatalf("expected the denial to surface, got %v", err)
	}
}

func TestAutoContinue_SkipsWhenNoSession(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{{claude.Result{}, denied("d")}}, &calls)

	_, continues, err := invokeWithAutoContinue(context.Background(), inv, claude.Options{}, autoContinueOptions{Max: 3})
	if continues != 0 {
		t.Fatalf("cannot continue without a session; continues = %d", continues)
	}
	if !claude.IsToolDenied(err) {
		t.Fatalf("expected the denial to surface, got %v", err)
	}
}

func TestAutoContinue_SkipsWhenWindowNearlyOver(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{{claude.Result{SessionID: "s"}, denied("d")}}, &calls)

	_, continues, _ := invokeWithAutoContinue(context.Background(), inv, claude.Options{},
		autoContinueOptions{Max: 3, Deadline: time.Now().Add(30 * time.Second)})
	if continues != 0 {
		t.Fatalf("expected no continuation with less than %s left, got %d", minContinueWindow, continues)
	}
}

func TestAutoContinue_PassesRemainingTimeAsTimeout(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{
		{claude.Result{SessionID: "s"}, denied("d")},
		{claude.Result{SessionID: "s"}, nil},
	}, &calls)

	invokeWithAutoContinue(context.Background(), inv,
		claude.Options{Timeout: time.Hour},
		autoContinueOptions{Max: 1, Deadline: time.Now().Add(10 * time.Minute), BuildPrompt: func(string) string { return "go" }})

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Timeout > 10*time.Minute || calls[1].Timeout < 9*time.Minute {
		t.Fatalf("continuation timeout should be the remaining window, got %s", calls[1].Timeout)
	}
}

func TestAutoContinue_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{{claude.Result{SessionID: "s"}, denied("d")}}, &calls)
	cancel()

	_, continues, _ := invokeWithAutoContinue(ctx, inv, claude.Options{}, autoContinueOptions{Max: 3})
	if continues != 0 {
		t.Fatalf("a cancelled run must not be continued; continues = %d", continues)
	}
}

func TestAutoContinue_LeavesOtherErrorsAlone(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{{claude.Result{SessionID: "s"}, &claude.ErrTimeout{Duration: time.Minute}}}, &calls)

	_, continues, err := invokeWithAutoContinue(context.Background(), inv, claude.Options{}, autoContinueOptions{Max: 3})
	if continues != 0 {
		t.Fatalf("timeouts must not be auto-continued; continues = %d", continues)
	}
	var timeoutErr *claude.ErrTimeout
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected the timeout to pass through, got %v", err)
	}
}

func TestAutoContinue_KeepsSessionIDForClassification(t *testing.T) {
	var calls []claude.Options
	inv := scriptedInvoke([]outcome{
		{claude.Result{SessionID: "sess-1"}, denied("d")},
		// Continuation died before re-emitting system/init: no session id.
		{claude.Result{}, denied("d again")},
	}, &calls)

	result, _, _ := invokeWithAutoContinue(context.Background(), inv, claude.Options{},
		autoContinueOptions{Max: 1, BuildPrompt: func(string) string { return "go" }})

	if result.SessionID != "sess-1" {
		t.Fatalf("session id must survive so the task stays resumable, got %q", result.SessionID)
	}
}

func TestClassifyDeniedLeavesTaskResumable(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	task, _ := st.CreateTask("denied task", "do stuff", "", "small", "", "")
	workdir := filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID))
	os.MkdirAll(workdir, 0755)
	run, _ := st.CreateRun(task.ID, workdir, "", "", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-denied")

	invokeErr := &claude.ErrToolDenied{Message: "blocked", Denials: 2}
	classifyErr := classify(context.Background(), st, *task, run,
		claude.Result{SessionID: "sess-denied", CostUSD: 1, NumTurns: 5},
		invokeErr, "", "", workdir, "sess-denied", true, log.New("", false), nil)

	if !claude.IsToolDenied(classifyErr) {
		t.Fatalf("expected the denial error back, got %v", classifyErr)
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "resumable" {
		t.Fatalf("task status = %s, want resumable (a denial must never fail the task)", got.Status)
	}
	finished, _ := st.LatestRunForTask(task.ID)
	if finished.Status != "errored" {
		t.Errorf("run status = %s, want errored", finished.Status)
	}
	if !strings.Contains(finished.Error, "auto-denied") {
		t.Errorf("run error should explain the denial, got %q", finished.Error)
	}
	// The workdir is cleaned up even for resumable runs: work is saved in Git
	// and the workdir is recreated on resume.
	if _, statErr := os.Stat(workdir); statErr == nil {
		t.Errorf("agent workdir should be cleaned up (work saved in Git): %s", workdir)
	}
}

func TestBuildPromptIncludesDenialPolicy(t *testing.T) {
	task := store.Task{ID: 7, Title: "t", Prompt: "p"}
	for _, tc := range []struct {
		name      string
		agentMode bool
	}{{"agent mode", true}, {"managed mode", false}} {
		t.Run(tc.name, func(t *testing.T) {
			prompt := buildPrompt(promptInput{task: task, agentMode: tc.agentMode, defaultBranch: "main", worktreePath: "/wt", branch: "br", repoPath: "/repo"})
			if !strings.Contains(prompt, "There Is No Human Here") {
				t.Fatal("prompt should carry the denial policy section")
			}
			if !strings.Contains(prompt, "Do not stop, and do not idle waiting for approval") {
				t.Fatal("denial policy body missing")
			}
		})
	}
}

func TestBuildContinuePrompt(t *testing.T) {
	task := store.Task{ID: 42, Title: "fix the thing"}

	agent := buildContinuePrompt(task, true, "", "/work/task-42", "", "", "The user doesn't want to proceed with this tool use.")
	if !strings.Contains(agent, "Continue — Your Last Tool Call Was Auto-Denied") {
		t.Error("missing continue template")
	}
	if !strings.Contains(agent, "WORKED_IN:") {
		t.Error("agent-mode continuation must restate the trailer contract")
	}
	if !strings.Contains(agent, "/work/task-42") {
		t.Error("agent-mode continuation must restate WORKDIR")
	}
	if !strings.Contains(agent, "doesn't want to proceed") {
		t.Error("continuation should quote the denial")
	}
	if !strings.Contains(agent, "fix the thing") {
		t.Error("continuation should identify the task")
	}

	managed := buildContinuePrompt(task, false, "develop", "/wt/task-42", "burnrate/42-fix", "/repo", "blocked")
	if !strings.Contains(managed, "burnrate/42-fix") {
		t.Error("managed continuation must restate the branch")
	}
	if !strings.Contains(managed, "DEFAULT_BRANCH: `develop`") {
		t.Error("managed continuation must restate the default branch")
	}
	if strings.Contains(managed, "WORKED_IN:") {
		t.Error("managed continuation should not ask for agent trailers")
	}
}

func TestInterruptedResumeNote(t *testing.T) {
	note := interruptedResumeNote("The user doesn't want to proceed with this tool use.\nSTOP what you are doing.")
	if !strings.Contains(note, "Ended On A Denied Tool Call") {
		t.Fatal("missing heading")
	}
	if strings.Contains(note, "STOP what you are doing") {
		t.Error("only the first line of the denial should be quoted")
	}
	if !strings.Contains(note, "not a human") {
		t.Error("note should say the denial was not a human")
	}
}

func TestRunAddsInterruptedResumeGuidance(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	os.MkdirAll(filepath.Join(dataDir, "agentwork"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)
	st.SetSetting("notify_on_review", "false")

	task, _ := st.CreateTask("resumed task", "do stuff", "", "small", "", "")
	workdir := filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID))
	os.MkdirAll(workdir, 0755)

	prior, _ := st.CreateRun(task.ID, workdir, "", "", "w0", 1)
	st.SetRunSessionID(prior.ID, "sess-prior")
	st.FinishRun(prior.ID, "errored", 1, 5, "", "denied", "")
	priorRun, _ := st.LatestRunForTask(task.ID)

	// A prior log whose tail is a denial: the dry-run prompt should gain the
	// "keep going" guidance.
	logLine := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use"}`
	os.WriteFile(filepath.Join(dataDir, "logs", fmt.Sprintf("run-%d.jsonl", prior.ID)), []byte(logLine+"\n"), 0644)

	cfg := config.Config{
		DataDir:         dataDir,
		WorktreeRoot:    filepath.Join(dataDir, "worktrees"),
		DryRun:          true,
		MaxAttempts:     5,
		MaxAutoContinue: 3,
	}
	if err := Run(context.Background(), st, cfg, *task, priorRun, Params{Timeout: 5 * time.Minute, WindowID: "w1"}, log.New("", false)); err != nil {
		t.Fatalf("run: %v", err)
	}

	// DryRun writes the prompt it would have sent into the work dir.
	sent, readErr := os.ReadFile(filepath.Join(workdir, "BURNRATE_DRYRUN.md"))
	if readErr != nil {
		t.Fatalf("reading dry-run prompt: %v", readErr)
	}
	if !strings.Contains(string(sent), "Ended On A Denied Tool Call") {
		t.Fatal("resume prompt should warn that the prior session ended on a denial")
	}
}

// TestRunAutoContinuesDeniedToolCall drives the whole path — Invoke, denial
// detection, in-run continuation, classification — through a fake `claude` on
// PATH. The first invocation ends on an auto-denied tool call; the resumed one
// finishes the task.
func TestRunAutoContinuesDeniedToolCall(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	os.MkdirAll(filepath.Join(dataDir, "agentwork"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)
	st.SetSetting("notify_on_review", "false")

	binDir := t.TempDir()
	argsLog := filepath.Join(binDir, "args.log")
	script := fmt.Sprintf(`#!/bin/bash
# One line per invocation: newlines in the prompt are flattened so the log stays
# one record per call.
all="$*"
printf '%%s\n' "${all//$'\n'/ }" >> %q
if [[ "$*" == *--resume* ]]; then
  echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
  echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"git push"}}]}}'
  echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"pushed","tool_use_id":"t2"}]}}'
  echo '{"type":"result","result":"Done.\n\nWORKED_IN: /tmp/x\nREPO: o/r\nBRANCH: b\nPR: https://github.com/o/r/pull/9","num_turns":6,"duration_ms":2000,"total_cost_usd":2.0,"session_id":"sess-1"}'
else
  echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
  echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go build ./..."}}]}}'
  echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn'"'"'t want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use","tool_result_meta":[{"id":"t1","non_execution_kind":"user-rejected"}]}'
  echo '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}'
  echo '{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":4,"duration_ms":1000,"total_cost_usd":1.0,"session_id":"sess-1"}'
  exit 1
fi
`, argsLog)
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	task, _ := st.CreateTask("denied then continued", "do stuff", "", "small", "", "")
	cfg := config.Config{
		DataDir:         dataDir,
		WorktreeRoot:    filepath.Join(dataDir, "worktrees"),
		Model:           "opus",
		MaxAttempts:     5,
		MaxAutoContinue: 2,
	}

	if err := Run(context.Background(), st, cfg, *task, nil, Params{Timeout: 30 * time.Minute, WindowID: "w1"}, log.New("", false)); err != nil {
		t.Fatalf("the run should have recovered from the denial, got: %v", err)
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "pr_created" {
		t.Fatalf("task status = %s, want pr_created", got.Status)
	}
	run, _ := st.LatestRunForTask(task.ID)
	if run.Status != "succeeded" {
		t.Fatalf("run status = %s, want succeeded", run.Status)
	}
	if run.PRURL != "https://github.com/o/r/pull/9" {
		t.Errorf("pr_url = %q", run.PRURL)
	}
	if run.CostUSD != 3.0 {
		t.Errorf("cost = %v, want 3.0 (both invocations)", run.CostUSD)
	}
	if run.NumTurns != 10 {
		t.Errorf("num_turns = %d, want 10 (both invocations)", run.NumTurns)
	}

	invocations, _ := os.ReadFile(argsLog)
	lines := strings.Split(strings.TrimSpace(string(invocations)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 claude invocations, got %d:\n%s", len(lines), invocations)
	}
	if strings.Contains(lines[0], "--resume") {
		t.Error("first invocation should not resume")
	}
	if !strings.Contains(lines[1], "--resume sess-1") {
		t.Errorf("continuation should resume sess-1: %s", lines[1])
	}
	if !strings.Contains(lines[1], "Your Last Tool Call Was Auto-Denied") {
		t.Errorf("continuation should carry the continue prompt: %s", lines[1])
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("  hello\nworld  "); got != "hello" {
		t.Errorf("firstLine = %q", got)
	}
	long := strings.Repeat("x", 400)
	if got := firstLine(long); len(got) != 203 {
		t.Errorf("long line should be truncated, got len %d", len(got))
	}
}
