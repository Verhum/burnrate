package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Verbatim lines from a real run log (~/.burnrate/logs/run-103.jsonl), trimmed.
const (
	denialEventWithMeta = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"toolu_01JBtVTYHZP6w2zCuk4dKzh3"}]},"session_id":"8835edfb","uuid":"90a489cf","tool_use_result":"User rejected tool use","tool_result_meta":[{"id":"toolu_01JBtVTYHZP6w2zCuk4dKzh3","non_execution_kind":"user-rejected"}]}`
	denialEventNoMeta   = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"toolu_01Dpntob"}]},"session_id":"912670fd","uuid":"a02621db","tool_use_result":"User rejected tool use"}`
	interruptEvent      = `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]},"session_id":"8835edfb","uuid":"852bebe5"}`
	okToolResultEvent   = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok","tool_use_id":"toolu_ok"}]},"session_id":"s","uuid":"u"}`
	toolUseEvent        = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_x","name":"Bash","input":{"command":"ls"}}]}}`

	// The CLI's structured auto-deny event, and the tool_result that follows it
	// carrying the same tool_use_id.
	permissionDeniedEvent  = `{"type":"system","subtype":"permission_denied","tool_name":"Bash","tool_use_id":"toolu_deny1","decision_reason_type":"rule","decision_reason":"deny rule Bash(rm *)","message":"Permission to use Bash has been denied.","uuid":"pd1","session_id":"s"}`
	permissionDeniedResult = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"Permission to use Bash has been denied.","is_error":true,"tool_use_id":"toolu_deny1"}]},"tool_result_meta":[{"id":"toolu_deny1","non_execution_kind":"permission-rule"}],"session_id":"s","uuid":"pd2"}`
)

func TestPermissionDeniedFromEvent(t *testing.T) {
	got := PermissionDeniedFromEvent([]byte(permissionDeniedEvent))
	if got == "" {
		t.Fatal("expected the permission_denied event to be recognized")
	}
	for _, want := range []string{"Permission to use Bash has been denied.", "Bash", "deny rule Bash(rm *)"} {
		if !strings.Contains(got, want) {
			t.Errorf("denial text %q missing %q", got, want)
		}
	}

	// A deny rule can pick its own wording, which the phrase matcher would miss.
	custom := `{"type":"system","subtype":"permission_denied","tool_name":"WebFetch","message":"Nope, network is off.","uuid":"u","session_id":"s"}`
	if got := PermissionDeniedFromEvent([]byte(custom)); !strings.Contains(got, "Nope, network is off.") {
		t.Errorf("expected the custom deny message, got %q", got)
	}
	if got := DenialFromEvent([]byte(custom)); got != "" {
		t.Errorf("phrase matcher should not fire on a system event, got %q", got)
	}

	for _, line := range []string{
		`{"type":"system","subtype":"init","tools":["Bash"]}`,
		`{"type":"system","subtype":"task_notification","message":"done"}`,
		okToolResultEvent,
		`not json`,
	} {
		if got := PermissionDeniedFromEvent([]byte(line)); got != "" {
			t.Errorf("expected no denial for %s, got %q", line, got)
		}
	}
}

// A policy denial surfaces as two events for one blocked call. Both must report
// the same tool_use_id so Invoke counts the call once.
func TestDenialFrom_DedupesByToolUseID(t *testing.T) {
	sys, ok := DenialFrom([]byte(permissionDeniedEvent))
	if !ok {
		t.Fatal("expected the system event to report a denial")
	}
	res, ok := DenialFrom([]byte(permissionDeniedResult))
	if !ok {
		t.Fatal("expected the tool_result to report a denial")
	}
	if sys.ToolUseID != "toolu_deny1" || res.ToolUseID != "toolu_deny1" {
		t.Fatalf("tool_use_ids differ: system=%q result=%q", sys.ToolUseID, res.ToolUseID)
	}

	// The interruption shape has no system event but still identifies the call.
	d, ok := DenialFrom([]byte(denialEventWithMeta))
	if !ok || d.ToolUseID != "toolu_01JBtVTYHZP6w2zCuk4dKzh3" {
		t.Fatalf("got ok=%v id=%q", ok, d.ToolUseID)
	}
	// No metadata: fall back to the tool_result block's id.
	if d, _ := DenialFrom([]byte(denialEventNoMeta)); d.ToolUseID != "toolu_01Dpntob" {
		t.Errorf("expected the block tool_use_id, got %q", d.ToolUseID)
	}
	if _, ok := DenialFrom([]byte(okToolResultEvent)); ok {
		t.Error("expected no denial for a successful tool result")
	}
}

func TestDenialFromEvent_StructuredSignals(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"meta plus tool_use_result", denialEventWithMeta},
		{"tool_use_result only", denialEventNoMeta},
		{"meta only", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"blocked","is_error":true,"tool_use_id":"t1"}]},"tool_result_meta":[{"id":"t1","non_execution_kind":"user-rejected"}]}`},
		{"hook deny kind", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"blocked by hook","is_error":true,"tool_use_id":"t1"}]},"tool_result_meta":[{"id":"t1","non_execution_kind":"hook-denied"}]}`},
		{"prefix only", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. STOP what you are doing.","is_error":true,"tool_use_id":"t1"}]}}`},
		{"content as blocks", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"The user doesn't want to take this action right now. STOP."}],"is_error":true,"tool_use_id":"t1"}]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DenialFromEvent([]byte(c.line)); got == "" {
				t.Fatalf("expected a denial, got none")
			}
		})
	}
}

func TestDenialFromEvent_NoFalsePositives(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"assistant narrating the phrase", `{"type":"assistant","message":{"content":[{"type":"text","text":"The user doesn't want to proceed with this tool use. STOP what you are doing — that is the message we detect."}]}}`},
		{"assistant tool_use quoting the phrase", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t","name":"Bash","input":{"command":"grep -r \"The user doesn't want to proceed with this tool use\" ."}}]}}`},
		{"tool_result echoing the phrase mid-output", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"run-103.jsonl: The user doesn't want to proceed with this tool use. STOP what you are doing.","is_error":true,"tool_use_id":"t1"}]}}`},
		{"successful tool_result", okToolResultEvent},
		{"result event", `{"type":"result","result":"done","num_turns":3}`},
		{"garbage", `not json at all`},
		{"cancelled kind is not a denial", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"cancelled","is_error":true,"tool_use_id":"t1"}]},"tool_result_meta":[{"id":"t1","non_execution_kind":"cancelled"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DenialFromEvent([]byte(c.line)); got != "" {
				t.Fatalf("expected no denial, got %q", got)
			}
		})
	}
}

func TestInterruptFromEvent(t *testing.T) {
	if got := InterruptFromEvent([]byte(interruptEvent)); got == "" {
		t.Fatal("expected the interrupt marker to be recognized")
	}
	if got := InterruptFromEvent([]byte(okToolResultEvent)); got != "" {
		t.Fatalf("expected no interrupt, got %q", got)
	}
	assistant := `{"type":"assistant","message":{"content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`
	if got := InterruptFromEvent([]byte(assistant)); got != "" {
		t.Fatalf("assistant text must not count as an interrupt turn, got %q", got)
	}
}

func TestIsToolResultAndToolUseEvents(t *testing.T) {
	if !IsToolResultEvent([]byte(okToolResultEvent)) {
		t.Error("expected tool result event")
	}
	if IsToolResultEvent([]byte(toolUseEvent)) {
		t.Error("assistant tool_use is not a tool result")
	}
	if !IsToolUseEvent([]byte(toolUseEvent)) {
		t.Error("expected tool use event")
	}
	if IsToolUseEvent([]byte(okToolResultEvent)) {
		t.Error("user tool_result is not a tool use")
	}
}

func TestErrToolDeniedMessage(t *testing.T) {
	single := (&ErrToolDenied{Message: "blocked", Denials: 1}).Error()
	if !strings.Contains(single, "auto-denied tool call") || !strings.Contains(single, "blocked") {
		t.Fatalf("unexpected message: %q", single)
	}
	multi := (&ErrToolDenied{Message: "blocked", Denials: 3}).Error()
	if !strings.Contains(multi, "3 auto-denied") {
		t.Fatalf("unexpected message: %q", multi)
	}
	empty := (&ErrToolDenied{}).Error()
	if !strings.Contains(empty, DeniedToolMessage) {
		t.Fatalf("expected fallback message, got %q", empty)
	}
	if !IsToolDenied(&ErrToolDenied{}) {
		t.Error("IsToolDenied should match ErrToolDenied")
	}
	if IsToolDenied(errors.New("nope")) {
		t.Error("IsToolDenied should not match other errors")
	}
}

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run-1.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLogEndedInterrupted(t *testing.T) {
	t.Run("denial at the tail", func(t *testing.T) {
		path := writeLog(t, toolUseEvent, denialEventWithMeta, interruptEvent,
			`{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":50}`)
		ended, msg := LogEndedInterrupted(path)
		if !ended {
			t.Fatal("expected the log to be seen as ending on a denial")
		}
		if !strings.Contains(msg, "doesn't want to proceed") {
			t.Fatalf("expected the denial text, got %q", msg)
		}
	})

	t.Run("agent kept working after the denial", func(t *testing.T) {
		path := writeLog(t, denialEventWithMeta, toolUseEvent, okToolResultEvent,
			`{"type":"result","result":"done","num_turns":10}`)
		if ended, _ := LogEndedInterrupted(path); ended {
			t.Fatal("a denial the agent recovered from must not need a nudge")
		}
	})

	t.Run("interrupt only", func(t *testing.T) {
		path := writeLog(t, toolUseEvent, interruptEvent)
		if ended, _ := LogEndedInterrupted(path); !ended {
			t.Fatal("expected the interrupt marker alone to count")
		}
	})

	t.Run("clean log", func(t *testing.T) {
		path := writeLog(t, toolUseEvent, okToolResultEvent, `{"type":"result","result":"done"}`)
		if ended, _ := LogEndedInterrupted(path); ended {
			t.Fatal("clean log should not report an interruption")
		}
	})

	t.Run("missing log", func(t *testing.T) {
		if ended, _ := LogEndedInterrupted(filepath.Join(t.TempDir(), "absent.jsonl")); ended {
			t.Fatal("missing log should report false")
		}
	})

	t.Run("denial outside the scanned tail", func(t *testing.T) {
		lines := []string{denialEventWithMeta}
		for range logTailLines + 5 {
			lines = append(lines, toolUseEvent)
		}
		path := writeLog(t, lines...)
		if ended, _ := LogEndedInterrupted(path); ended {
			t.Fatal("an ancient denial should not trip the tail scan")
		}
	})
}

func TestInvokeToolDenied(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"denied-sess"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go build ./..."}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. The tool use was rejected. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use","tool_result_meta":[{"id":"t1","non_execution_kind":"user-rejected"}]}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}
{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":4,"duration_ms":1000,"total_cost_usd":0.5,"session_id":"denied-sess"}
EOF
`)

	var denials []string
	result, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		OnDenial:   func(msg string) { denials = append(denials, msg) },
	})

	var denied *ErrToolDenied
	if !errors.As(err, &denied) {
		t.Fatalf("expected ErrToolDenied, got %v", err)
	}
	if denied.Denials != 1 {
		t.Errorf("Denials = %d, want 1", denied.Denials)
	}
	if len(denials) != 1 {
		t.Errorf("OnDenial fired %d times, want 1", len(denials))
	}
	if result.SessionID != "denied-sess" {
		t.Errorf("session id lost: %q", result.SessionID)
	}
	if result.CostUSD != 0.5 {
		t.Errorf("cost should still be captured, got %v", result.CostUSD)
	}
}

// A real policy denial emits system/permission_denied *and* the tool_result the
// model sees. One blocked call must be reported once, with the richer message.
func TestInvokeCountsPolicyDenialOnce(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"policy-sess"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_deny1","name":"Bash","input":{"command":"rm -rf /"}}]}}
`+permissionDeniedEvent+`
`+permissionDeniedResult+`
{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":3,"duration_ms":1000,"total_cost_usd":0.2,"session_id":"policy-sess"}
EOF
`)

	var denials []string
	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		OnDenial:   func(msg string) { denials = append(denials, msg) },
	})

	var denied *ErrToolDenied
	if !errors.As(err, &denied) {
		t.Fatalf("expected ErrToolDenied, got %v", err)
	}
	if denied.Denials != 1 {
		t.Errorf("Denials = %d, want 1 (the system event and tool_result are one blocked call)", denied.Denials)
	}
	if len(denials) != 1 {
		t.Fatalf("OnDenial fired %d times, want 1", len(denials))
	}
	if !strings.Contains(denials[0], "deny rule Bash(rm *)") {
		t.Errorf("expected the structured event's reason to win, got %q", denials[0])
	}
}

func TestInvokeDenialRecoveredByAgent(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"recovered-sess"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go build ./internal/..."}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok","tool_use_id":"t2"}]}}
{"type":"result","result":"https://github.com/o/r/pull/1","num_turns":6,"duration_ms":2000,"total_cost_usd":1.0}
EOF
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("an agent that worked past the denial must not error: %v", err)
	}
}

func TestInvokeDenialStallKillsProcess(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"stall-sess"}'
echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn'"'"'t want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use"}'
sleep 300
`)

	start := time.Now()
	_, err := Invoke(context.Background(), Options{
		Prompt:      "do something",
		Model:       "opus",
		BudgetUSD:   5,
		ClaudePath:  script,
		WorkDir:     t.TempDir(),
		DenialGrace: 2 * time.Second,
	})

	var denied *ErrToolDenied
	if !errors.As(err, &denied) {
		t.Fatalf("expected ErrToolDenied, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("watchdog took too long: %s", elapsed)
	}
}

func TestInvokeDenialWatchdogSparesSlowRetry(t *testing.T) {
	// An agent that answers a denial with a slow call (a full build, say) emits
	// no events while it runs. The watchdog must not mistake that for a stall.
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"slow-retry-sess"}'
echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn'"'"'t want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go build ./..."}}]}}'
sleep 4
echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok","tool_use_id":"t2"}]}}'
echo '{"type":"result","result":"https://github.com/o/r/pull/2","num_turns":8,"duration_ms":4000,"total_cost_usd":1.0}'
`)

	result, err := Invoke(context.Background(), Options{
		Prompt:      "do something",
		Model:       "opus",
		BudgetUSD:   5,
		ClaudePath:  script,
		WorkDir:     t.TempDir(),
		DenialGrace: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("a slow retry after a denial must not be killed: %v", err)
	}
	if result.NumTurns != 8 {
		t.Errorf("expected the run to finish, got %d turns", result.NumTurns)
	}
}

func TestInvokeTimeoutWinsOverDenial(t *testing.T) {
	// The CLI marks an in-flight tool call "user-rejected" whenever it is
	// interrupted, so a timeout kill leaves a denial in the stream too. The
	// timeout must still be reported, or burnrate would auto-continue a run
	// whose window is already gone.
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"timeout-denial-sess"}'
echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn'"'"'t want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use"}'
sleep 300
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:      "do something",
		Model:       "opus",
		BudgetUSD:   5,
		ClaudePath:  script,
		WorkDir:     t.TempDir(),
		Timeout:     2 * time.Second,
		DenialGrace: 10 * time.Minute,
	})

	var timeoutErr *ErrTimeout
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected ErrTimeout to win, got %v", err)
	}
}

func TestInvokeCtxCancelWinsOverDenial(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"cancel-denial-sess"}'
echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn'"'"'t want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use"}'
sleep 300
`)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	_, err := Invoke(ctx, Options{
		Prompt:      "do something",
		Model:       "opus",
		BudgetUSD:   5,
		ClaudePath:  script,
		WorkDir:     t.TempDir(),
		DenialGrace: 10 * time.Minute,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to win, got %v", err)
	}
}

func TestInvokeRateLimitWinsOverDenial(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"rl-denial-sess"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"t1"}]},"tool_use_result":"User rejected tool use"}
{"type":"result","result":"You've hit your limit, resets 2pm (America/Los_Angeles)","is_error":true,"num_turns":2}
EOF
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})

	var rl *ErrRateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimited to win, got %v", err)
	}
}
