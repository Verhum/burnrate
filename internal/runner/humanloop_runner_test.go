package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

// The MCP config must declare type "http" — the claude CLI silently skips
// servers whose type it does not recognise — and must land in the data dir, not
// in the run's workdir, where it kept agent workdirs from ever being cleaned up
// and got checkpoint-committed into user branches.
func TestWriteMCPConfigLandsInDataDirWithHTTPType(t *testing.T) {
	dataDir := t.TempDir()
	workDir := t.TempDir()

	path := writeMCPConfig(dataDir, 9112, 42, 7, log.New("", false))
	if path == "" {
		t.Fatal("expected non-empty config path")
	}

	wantDir := filepath.Join(dataDir, "mcp")
	if got := filepath.Dir(path); got != wantDir {
		t.Fatalf("config dir = %q, want %q", got, wantDir)
	}
	if got := filepath.Base(path); got != "task-42-run-7.json" {
		t.Fatalf("config filename = %q, want task-42-run-7.json", got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var parsed struct {
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	srv, ok := parsed.MCPServers["burnrate-human-loop"]
	if !ok {
		t.Fatalf("no burnrate-human-loop server in %s", data)
	}
	if srv.Type != "http" {
		t.Fatalf("server type = %q, want \"http\" (the CLI silently skips unknown types)", srv.Type)
	}
	if !strings.Contains(srv.URL, "task=42&run=7") {
		t.Fatalf("url %q must address task 42 / run 7", srv.URL)
	}

	// Nothing may be written into the workdir.
	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatalf("read workdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("workdir must stay empty, got %v", entries)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".mcp-burnrate.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy .mcp-burnrate.json must not be written into the workdir (stat err=%v)", err)
	}
}

// classifyWaitingHumanFixture builds a task + agent-mode run and drives
// classify with a WAITING_HUMAN trailer.
func classifyWaitingHumanFixture(t *testing.T, sessionID string, setup func(st *store.Store, taskID, runID int64)) (*store.Store, *store.Task) {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	st.SetSetting("notify_on_review", "false")

	task, _ := st.CreateTask("parked task", "do stuff", "", "small", "", "")
	workdir := filepath.Join(dataDir, "agentwork", fmt.Sprintf("task-%d", task.ID))
	os.MkdirAll(workdir, 0755)
	run, _ := st.CreateRun(task.ID, workdir, "", "", "w1", 1)
	if sessionID != "" {
		st.SetRunSessionID(run.ID, sessionID)
	}
	if setup != nil {
		setup(st, task.ID, run.ID)
	}

	result := claude.Result{SessionID: sessionID, ResultText: "## Summary\n\nNeed input.\n\nRESULT: WAITING_HUMAN\n"}
	if err := classify(context.Background(), st, *task, run, result, nil,
		"", "main", workdir, "", true, log.New("", false), nil); err != nil {
		t.Fatalf("classify: %v", err)
	}

	got, err := st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	return st, got
}

// The reply can land between the MCP long-poll expiring and classify running.
// RequestService.Respond only re-queues a task already in awaiting_human, so
// parking here would strand the task behind an answered request forever.
func TestClassifyWaitingHuman_ReplyInParkWindowRequeues(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sessionID  string
		wantStatus string
	}{
		{"no session", "", "queued"},
		{"with session", "sess-parked", "resumable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requestID int64
			st, got := classifyWaitingHumanFixture(t, tc.sessionID, func(st *store.Store, taskID, runID int64) {
				req, err := st.CreateHumanRequest(taskID, runID, "question", "which one?", "")
				if err != nil {
					t.Fatalf("create request: %v", err)
				}
				requestID = req.ID
				// The human answered while the run was finishing.
				st.AddComment(taskID, "use the second one", "user")
				if err := st.SetHumanRequestStatus(req.ID, "answered"); err != nil {
					t.Fatalf("answer request: %v", err)
				}
			})

			if got.Status != tc.wantStatus {
				t.Fatalf("task status = %s, want %s (an answered request must not be parked)", got.Status, tc.wantStatus)
			}
			if got.AttemptResetRunID == 0 {
				t.Fatal("attempts must be reset when the task is re-queued")
			}

			// The human's reply must survive to feed the next run.
			unconsumed, _ := st.UnconsumedComments(got.ID)
			var reply *store.Comment
			for i := range unconsumed {
				if unconsumed[i].Body == "use the second one" {
					reply = &unconsumed[i]
				}
			}
			if reply == nil {
				t.Fatalf("the human's reply was consumed and would never reach the next run: %+v", unconsumed)
			}

			// The rest of the branch still happened.
			run, _ := st.LatestRunForTask(got.ID)
			if run.Status != "succeeded" {
				t.Fatalf("run status = %s, want succeeded", run.Status)
			}
			if got.Summary != "Need input." {
				t.Fatalf("task summary = %q, want the parsed ## Summary", got.Summary)
			}
			comments, _ := st.CommentsForTask(got.ID)
			foundAgent := false
			for _, c := range comments {
				if c.Author == "agent" {
					foundAgent = true
				}
			}
			if !foundAgent {
				t.Fatal("the agent's result text must still be posted as a comment")
			}
			if r, _ := st.GetHumanRequest(requestID); r.Status != "answered" {
				t.Fatalf("request status = %s, want answered (untouched)", r.Status)
			}
		})
	}
}

func TestClassifyWaitingHuman_PendingRequestStillParks(t *testing.T) {
	st, got := classifyWaitingHumanFixture(t, "sess-parked", func(st *store.Store, taskID, runID int64) {
		if _, err := st.CreateHumanRequest(taskID, runID, "question", "which one?", ""); err != nil {
			t.Fatalf("create request: %v", err)
		}
	})
	if got.Status != "awaiting_human" {
		t.Fatalf("task status = %s, want awaiting_human (the request is still pending)", got.Status)
	}
	if got.AttemptResetRunID != 0 {
		t.Fatal("a parked task must not have its attempts reset")
	}
	_ = st
}

// The agent can park via the trailer without ever calling the MCP tools; that
// run has no requests at all and must still park.
func TestClassifyWaitingHuman_NoRequestsStillParks(t *testing.T) {
	_, got := classifyWaitingHumanFixture(t, "sess-parked", nil)
	if got.Status != "awaiting_human" {
		t.Fatalf("task status = %s, want awaiting_human (no MCP request means nothing was answered)", got.Status)
	}
	if got.AttemptResetRunID != 0 {
		t.Fatal("a parked task must not have its attempts reset")
	}
}

// Every other terminal-success branch marks the follow-ups that fed the run
// consumed; the park branch must too, or they are replayed on every resume.
func TestClassifyWaitingHuman_MarksCommentsConsumed(t *testing.T) {
	st, got := classifyWaitingHumanFixture(t, "sess-parked", func(st *store.Store, taskID, runID int64) {
		st.AddComment(taskID, "also handle the empty case", "user")
	})
	if got.Status != "awaiting_human" {
		t.Fatalf("task status = %s, want awaiting_human", got.Status)
	}
	unconsumed, _ := st.UnconsumedComments(got.ID)
	followups := userComments(unconsumed)
	if len(followups) != 0 {
		t.Fatalf("the follow-ups that fed the run must be consumed, got %+v", followups)
	}
}
