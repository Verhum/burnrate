package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

// TestRunRestartsFreshWhenSessionGone covers the resume target vanishing from
// the CLI's config dir — what happens when the daemon is pointed at a different
// account after a session was recorded. The CLI answers --resume with "No
// conversation found with session ID" in milliseconds, so retrying the resume
// spends the task's whole attempt budget in seconds. The run must fall back to
// a fresh session and finish the work instead.
func TestRunRestartsFreshWhenSessionGone(t *testing.T) {
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
all="$*"
printf '%%s\n' "${all//$'\n'/ }" >> %q
if [[ "$*" == *--resume* ]]; then
  echo '{"type":"result","subtype":"error_during_execution","duration_ms":0,"is_error":true,"num_turns":0,"total_cost_usd":0,"session_id":"sess-gone","errors":["No conversation found with session ID: sess-gone"]}'
  exit 1
else
  echo '{"type":"system","subtype":"init","session_id":"sess-new"}'
  echo '{"type":"result","result":"Done.\n\nWORKED_IN: /tmp/x\nREPO: o/r\nBRANCH: b\nPR: https://github.com/o/r/pull/7","num_turns":9,"duration_ms":2000,"total_cost_usd":1.5,"session_id":"sess-new"}'
fi
`, argsLog)
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	task, _ := st.CreateTask("session went missing", "do stuff", "", "small", "", "")
	workdir := filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID))
	os.MkdirAll(workdir, 0755)

	prior, _ := st.CreateRun(task.ID, workdir, "", "", "w0", 1)
	st.SetRunSessionID(prior.ID, "sess-gone")
	st.FinishRun(prior.ID, "rate_limited", 12, 3.0, "", "suspended", "")
	priorRun, _ := st.LatestRunForTask(task.ID)

	cfg := config.Config{
		DataDir:         dataDir,
		WorktreeRoot:    filepath.Join(dataDir, "worktrees"),
		Model:           "opus",
		MaxAttempts:     5,
		MaxAutoContinue: 2,
	}

	if err := Run(context.Background(), st, cfg, *task, priorRun, Params{Timeout: 30 * time.Minute, WindowID: "w1"}, log.New("", false)); err != nil {
		t.Fatalf("run should have restarted fresh, got: %v", err)
	}

	got, _ := st.GetTask(task.ID)
	if got.Status != "pr_created" {
		t.Fatalf("task status = %s, want pr_created", got.Status)
	}
	run, _ := st.LatestRunForTask(task.ID)
	if run.Status != "succeeded" {
		t.Fatalf("run status = %s, want succeeded", run.Status)
	}
	if run.SessionID != "sess-new" {
		t.Errorf("run session_id = %q, want sess-new (the dead id must not stick)", run.SessionID)
	}

	invocations, _ := os.ReadFile(argsLog)
	lines := strings.Split(strings.TrimSpace(string(invocations)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 claude invocations, got %d:\n%s", len(lines), invocations)
	}
	if !strings.Contains(lines[0], "--resume sess-gone") {
		t.Errorf("first invocation should try the recorded session: %s", lines[0])
	}
	if strings.Contains(lines[1], "--resume") {
		t.Errorf("retry must not resume anything: %s", lines[1])
	}
}
