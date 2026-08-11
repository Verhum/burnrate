package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

// stubClaude puts a fake `claude` CLI first on PATH. It records the prompt it
// was handed and replays a stream-json transcript ending in resultText, which
// is how a real run's final message reaches the runner.
func stubClaude(t *testing.T, resultText string) (promptFile string) {
	t.Helper()
	dir := t.TempDir()
	promptFile = filepath.Join(dir, "prompt.txt")

	initLine, _ := json.Marshal(map[string]any{
		"type": "system", "subtype": "init",
		"session_id": "sess-e2e", "model": "claude-opus-5",
	})
	resultLine, _ := json.Marshal(map[string]any{
		"type": "result", "subtype": "success",
		"result": resultText, "session_id": "sess-e2e",
		"num_turns": 4, "duration_ms": 1200, "total_cost_usd": 0.42,
	})

	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, append(append(initLine, '\n'), append(resultLine, '\n')...), 0644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	script := "#!/bin/sh\nfor a in \"$@\"; do last=\"$a\"; done\nprintf '%s' \"$last\" > " +
		promptFile + "\ncat " + transcript + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return promptFile
}

// TestRun_FullAgentResponseEndToEnd drives the whole agent-mode path — spawn the
// CLI, parse its stream, classify, persist — and then a follow-up run, to prove
// the response is stored whole and does not come back as its own instructions.
func TestRun_FullAgentResponseEndToEnd(t *testing.T) {
	report := longAgentReport()
	promptFile := stubClaude(t, report)

	dataDir := t.TempDir()
	os.MkdirAll(filepath.Join(dataDir, "agentwork"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	st.SetSetting("notify_on_review", "false")

	task, _ := st.CreateTask("e2e task", "do something", "", "small", "", "")
	cfg := config.Config{
		DataDir:      dataDir,
		WorktreeRoot: filepath.Join(dataDir, "worktrees"),
		Model:        "claude-opus-5",
		MaxAttempts:  5,
	}
	logger := log.New("", false)
	params := Params{Timeout: time.Minute, WindowID: "w1"}

	if err := Run(context.Background(), st, cfg, *task, nil, params, logger); err != nil {
		t.Fatalf("run: %v", err)
	}

	run, _ := st.LatestRunForTask(task.ID)
	if run == nil || run.Status != "succeeded" {
		t.Fatalf("expected a succeeded run, got %+v", run)
	}
	if run.ResultText != report {
		t.Fatalf("run.result_text truncated: got %d bytes, want %d", len(run.ResultText), len(report))
	}

	comments, _ := st.CommentsForTask(task.ID)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Body != report {
		t.Fatalf("comment truncated: got %d bytes, want %d", len(comments[0].Body), len(report))
	}
	if !strings.HasSuffix(strings.TrimSpace(comments[0].Body), "/wt") {
		t.Fatal("comment should keep the trailer block the agent printed last")
	}

	// Second run: the agent's own report is the only unconsumed comment and
	// must not reappear as a follow-up instruction.
	got, _ := st.GetTask(task.ID)
	if err := Run(context.Background(), st, cfg, *got, nil, params, logger); err != nil {
		t.Fatalf("follow-up run: %v", err)
	}

	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	if strings.Contains(string(prompt), "Follow-up Instructions") {
		t.Fatal("agent report was fed back as a follow-up instruction")
	}
	if strings.Contains(string(prompt), "finding 7:") {
		t.Fatal("agent report text leaked into the next prompt")
	}
}
