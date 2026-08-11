package claude

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInvokeHappyPath(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"sess-abc123","model":"opus"}
{"type":"assistant","message":{"content":[{"type":"text","text":"Working on it"}]}}
{"type":"result","result":"All done","duration_ms":5000,"num_turns":3,"total_cost_usd":1.50}
EOF
`)

	var sessionID string
	result, err := Invoke(context.Background(), Options{
		Prompt:      "do something",
		Model:       "opus",
		BudgetUSD:   5,
		ClaudePath:  script,
		WorkDir:     t.TempDir(),
		OnSessionID: func(id string) { sessionID = id },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sessionID != "sess-abc123" {
		t.Errorf("OnSessionID got %q, want %q", sessionID, "sess-abc123")
	}
	if result.SessionID != "sess-abc123" {
		t.Errorf("result.SessionID = %q, want %q", result.SessionID, "sess-abc123")
	}
	if result.ResultText != "All done" {
		t.Errorf("result.ResultText = %q, want %q", result.ResultText, "All done")
	}
	if result.CostUSD != 1.50 {
		t.Errorf("result.CostUSD = %v, want %v", result.CostUSD, 1.50)
	}
	if result.NumTurns != 3 {
		t.Errorf("result.NumTurns = %d, want %d", result.NumTurns, 3)
	}
	if result.DurationMS != 5000 {
		t.Errorf("result.DurationMS = %d, want %d", result.DurationMS, 5000)
	}
}

func TestInvokeTimeout(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"timeout-sess"}'
sleep 300
`)

	var sessionID string
	_, err := Invoke(context.Background(), Options{
		Prompt:      "do something",
		Model:       "opus",
		BudgetUSD:   5,
		ClaudePath:  script,
		WorkDir:     t.TempDir(),
		Timeout:     2 * time.Second,
		OnSessionID: func(id string) { sessionID = id },
	})

	var timeoutErr *ErrTimeout
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected ErrTimeout, got: %v", err)
	}
	if sessionID != "timeout-sess" {
		t.Errorf("OnSessionID should fire before process exit; got %q", sessionID)
	}
}

// TestInvokeTimeoutKillsProcessGroup verifies the SIGTERM is delivered to the
// whole process group, not just the direct child: a backgrounded grandchild
// spawned by the script must also be reaped on timeout.
func TestInvokeTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"pg-sess"}'
sleep 300 &
echo $! > `+pidFile+`
wait
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		Timeout:    2 * time.Second,
	})
	var timeoutErr *ErrTimeout
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected ErrTimeout, got: %v", err)
	}

	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("reading child pid file: %v", readErr)
	}
	childPID, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("parsing child pid %q: %v", data, convErr)
	}

	// Poll: the grandchild should be dead shortly after SIGTERM.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(childPID, 0); err != nil {
			return // process gone — success
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid %d still alive; process group was not killed", childPID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestInvokeIdleLoop(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"idle-sess"}'
echo '{"type":"result","result":"","duration_ms":0,"num_turns":1,"total_cost_usd":0}'
echo '{"type":"result","result":"","duration_ms":0,"num_turns":1,"total_cost_usd":0}'
echo '{"type":"result","result":"","duration_ms":0,"num_turns":1,"total_cost_usd":0}'
echo '{"type":"result","result":"","duration_ms":0,"num_turns":1,"total_cost_usd":0}'
echo '{"type":"result","result":"","duration_ms":0,"num_turns":1,"total_cost_usd":0}'
sleep 300
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		Timeout:    10 * time.Second,
	})

	var idleErr *ErrIdleLoop
	if !errors.As(err, &idleErr) {
		t.Fatalf("expected ErrIdleLoop, got: %v", err)
	}
	if idleErr.Cycles != 5 {
		t.Errorf("expected 5 cycles, got %d", idleErr.Cycles)
	}
}

func TestInvokeIdleLoop_ResetByRealWork(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"idle-reset-sess"}'
echo '{"type":"result","result":"","duration_ms":0,"num_turns":1,"total_cost_usd":0}'
echo '{"type":"result","result":"","duration_ms":0,"num_turns":1,"total_cost_usd":0}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"real work"}]}}'
echo '{"type":"result","result":"done","duration_ms":5000,"num_turns":10,"total_cost_usd":1.0}'
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("expected no error (idle counter reset by real work), got: %v", err)
	}
}

func TestInvokeRateLimitStderr(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"rl-stderr-sess"}'
echo "You've hit your limit · resets 10pm (America/New_York)" >&2
echo '{"type":"result","result":"","duration_ms":0,"num_turns":0,"total_cost_usd":0}'
`)

	var stderrBuf bytes.Buffer
	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		Stderr:     &stderrBuf,
	})

	var rlErr *ErrRateLimited
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected ErrRateLimited, got: %v", err)
	}
	if !strings.Contains(rlErr.Message, "hit your limit") {
		t.Errorf("expected rate limit message, got: %s", rlErr.Message)
	}
	if stderrBuf.Len() == 0 {
		t.Error("expected stderr to be written to Stderr writer")
	}
}

func TestInvokeRateLimitResultEvent(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"rl-result-sess"}
{"type":"result","result":"You've hit your limit · resets 10pm (America/New_York)","duration_ms":0,"num_turns":1,"total_cost_usd":0}
EOF
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})

	var rlErr *ErrRateLimited
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected ErrRateLimited, got: %v", err)
	}
}

func TestInvokeDryRun(t *testing.T) {
	dir := t.TempDir()
	var sessionID string

	result, err := Invoke(context.Background(), Options{
		DryRun:      true,
		Prompt:      "test prompt content",
		WorkDir:     dir,
		OnSessionID: func(id string) { sessionID = id },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(sessionID, "dryrun-") {
		t.Errorf("expected dryrun- prefix, got: %s", sessionID)
	}
	if result.SessionID != sessionID {
		t.Errorf("result.SessionID = %q, want %q", result.SessionID, sessionID)
	}

	data, err := os.ReadFile(filepath.Join(dir, "BURNRATE_DRYRUN.md"))
	if err != nil {
		t.Fatalf("expected BURNRATE_DRYRUN.md: %v", err)
	}
	if string(data) != "test prompt content" {
		t.Errorf("expected prompt in file, got: %s", string(data))
	}
}

func TestInvokeDryRun_CtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Invoke(ctx, Options{
		DryRun:  true,
		Prompt:  "test",
		WorkDir: t.TempDir(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestInvokeOnEvent(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"event-sess"}'
echo '{"type":"result","result":"done","duration_ms":100,"num_turns":2,"total_cost_usd":0.1}'
`)

	var events []string
	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		OnEvent:    func(line string) { events = append(events, line) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestInvokeCtxCancel(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"cancel-sess"}'
sleep 300
`)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_, err := Invoke(ctx, Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestIsIdleCycle(t *testing.T) {
	tests := []struct {
		name string
		evt  Event
		want bool
	}{
		{
			name: "zero-work result",
			evt:  Event{Type: "result", NumTurns: 1, DurationMS: 0},
			want: true,
		},
		{
			name: "zero turns zero duration",
			evt:  Event{Type: "result", NumTurns: 0, DurationMS: 0},
			want: true,
		},
		{
			name: "real work result",
			evt:  Event{Type: "result", NumTurns: 15, DurationMS: 120000},
			want: false,
		},
		{
			name: "result with duration but one turn",
			evt:  Event{Type: "result", NumTurns: 1, DurationMS: 5000},
			want: false,
		},
		{
			name: "system event",
			evt:  Event{Type: "system", Subtype: "init"},
			want: false,
		},
		{
			name: "assistant event",
			evt:  Event{Type: "assistant"},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isIdleCycle(tc.evt)
			if got != tc.want {
				t.Errorf("isIdleCycle(%+v) = %v, want %v", tc.evt, got, tc.want)
			}
		})
	}
}

func TestInvokeOnPID(t *testing.T) {
	script := writeScript(t, `
echo '{"type":"system","subtype":"init","session_id":"pid-sess"}'
echo '{"type":"result","result":"done","duration_ms":100,"num_turns":2,"total_cost_usd":0.1}'
`)

	var gotPID int
	result, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		OnPID:      func(pid int) { gotPID = pid },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPID <= 0 {
		t.Fatalf("OnPID not called or got invalid pid: %d", gotPID)
	}
	if err := syscall.Kill(gotPID, 0); err == nil {
		t.Logf("pid %d still alive at test end (process may be lingering)", gotPID)
	}
	if result.ResultText != "done" {
		t.Errorf("result.ResultText = %q, want %q", result.ResultText, "done")
	}
}

func TestInvokeOnSessionIDFiresOnce(t *testing.T) {
	// Two system events carry the same session id (claude can re-emit init on
	// resume); OnSessionID must still fire exactly once.
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"once-sess"}
{"type":"system","subtype":"init","session_id":"once-sess"}
{"type":"result","result":"done","duration_ms":5000,"num_turns":3,"total_cost_usd":1.0}
EOF
`)

	var calls []string
	_, err := Invoke(context.Background(), Options{
		Prompt:      "do something",
		Model:       "opus",
		BudgetUSD:   5,
		ClaudePath:  script,
		WorkDir:     t.TempDir(),
		OnSessionID: func(id string) { calls = append(calls, id) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 {
		t.Errorf("OnSessionID fired %d times, want exactly 1 (%v)", len(calls), calls)
	}
}

func TestInvokeUsesPermissionModeAuto(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"perm-sess"}
{"type":"result","result":"done","duration_ms":100,"num_turns":2,"total_cost_usd":0.1}
EOF
`)

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	wrapper := filepath.Join(dir, "claude")

	wrapperContent := "#!/bin/bash\necho \"$@\" > " + argsFile + "\nexec " + script + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: wrapper,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--permission-mode auto") {
		t.Errorf("expected --permission-mode auto in args, got: %s", string(args))
	}
	if strings.Contains(string(args), "--dangerously-skip-permissions") {
		t.Errorf("should not pass --dangerously-skip-permissions, got: %s", string(args))
	}
}

// --permission-mode auto does not extend to MCP tools: without an explicit
// grant the CLI answers every human-loop call with "you haven't granted it
// yet", which burnrate's denial detector reads as ErrToolDenied. Verified
// against the real CLI (2.1.220).
func TestInvokeGrantsMCPToolsWhenConfigured(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"mcp-sess"}
{"type":"result","result":"done","duration_ms":100,"num_turns":2,"total_cost_usd":0.1}
EOF
`)

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	wrapper := filepath.Join(dir, "claude")
	mcpConfig := filepath.Join(dir, "task-1-run-2.json")
	if err := os.WriteFile(mcpConfig, []byte(`{"mcpServers":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	wrapperContent := "#!/bin/bash\necho \"$@\" > " + argsFile + "\nexec " + script + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := Invoke(context.Background(), Options{
		Prompt:        "do something",
		Model:         "opus",
		BudgetUSD:     5,
		ClaudePath:    wrapper,
		WorkDir:       t.TempDir(),
		MCPConfigPath: mcpConfig,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--mcp-config "+mcpConfig) {
		t.Errorf("expected --mcp-config in args, got: %s", string(args))
	}
	if !strings.Contains(string(args), "--allowedTools mcp__burnrate-human-loop__*") {
		t.Errorf("expected the human-loop tool grant in args, got: %s", string(args))
	}
	// Both flags are variadic and swallow following bare arguments, so the
	// prompt must stay behind its -p.
	if !strings.Contains(string(args), "-p do something") {
		t.Errorf("prompt must follow -p, got: %s", string(args))
	}
}

// No MCP config means no grant: an unrelated run must not carry a permission
// grant for a server it was never given.
func TestInvokeOmitsMCPGrantWithoutConfig(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"nomcp-sess"}
{"type":"result","result":"done","duration_ms":100,"num_turns":2,"total_cost_usd":0.1}
EOF
`)

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	wrapper := filepath.Join(dir, "claude")

	wrapperContent := "#!/bin/bash\necho \"$@\" > " + argsFile + "\nexec " + script + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: wrapper,
		WorkDir:    t.TempDir(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args, _ := os.ReadFile(argsFile)
	if strings.Contains(string(args), "--allowedTools") {
		t.Errorf("no MCP config means no tool grant, got: %s", string(args))
	}
}

func TestInvokeResumeSessionID(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"resumed-sess"}
{"type":"result","result":"resumed work done","duration_ms":3000,"num_turns":2,"total_cost_usd":0.5}
EOF
`)

	// Verify --resume flag is passed by checking the script receives it in args.
	// We use a wrapper that logs args and then runs the inner script.
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	wrapper := filepath.Join(dir, "claude")
	inner := script

	wrapperContent := "#!/bin/bash\necho \"$@\" > " + argsFile + "\nexec " + inner + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0755); err != nil {
		t.Fatal(err)
	}

	result, err := Invoke(context.Background(), Options{
		Prompt:          "continue work",
		Model:           "opus",
		BudgetUSD:       5,
		ClaudePath:      wrapper,
		WorkDir:         t.TempDir(),
		ResumeSessionID: "prev-session-42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--resume prev-session-42") {
		t.Errorf("expected --resume flag in args, got: %s", string(args))
	}
	if result.ResultText != "resumed work done" {
		t.Errorf("result.ResultText = %q, want %q", result.ResultText, "resumed work done")
	}
}

// The account selected via the UI must reach the spawned claude process:
// runner.Run puts CLAUDE_CONFIG_DIR into ExtraEnv, and Invoke must pass it
// through to the child. A fake claude records the env var it observed.
func TestInvokePassesExtraEnvToClaude(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "captured-env")
	script := writeScript(t, `
echo -n "$CLAUDE_CONFIG_DIR" > `+envFile+`
echo '{"type":"system","subtype":"init","session_id":"sess-env"}'
echo '{"type":"result","result":"ok","duration_ms":1,"num_turns":2,"total_cost_usd":0.1}'
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "do something",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
		ExtraEnv:   []string{"CLAUDE_CONFIG_DIR=/selected/.claude"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("reading captured env: %v", err)
	}
	if string(got) != "/selected/.claude" {
		t.Fatalf("claude saw CLAUDE_CONFIG_DIR=%q, want /selected/.claude", string(got))
	}
}

func TestInvokeCapturesResolvedModel(t *testing.T) {
	// The CLI resolves an alias to a concrete version; the analytics want what
	// actually ran, not what was asked for.
	script := writeScript(t, `
cat <<'STREAM'
{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-6-20260101"}
{"type":"result","result":"done","duration_ms":10,"num_turns":1,"total_cost_usd":0.5}
STREAM
`)

	result, err := Invoke(context.Background(), Options{
		Prompt:     "go",
		Model:      "opus",
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Model != "claude-opus-4-6-20260101" {
		t.Errorf("result.Model = %q, want the resolved id", result.Model)
	}
}

func TestInvokeModelTakesLatestNonEmpty(t *testing.T) {
	// A mid-run fallback substitutes another model; the last one named is the one
	// that finished the work.
	script := writeScript(t, `
cat <<'STREAM'
{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-6"}
{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}
{"type":"system","subtype":"model_fallback","model":"claude-sonnet-4-6"}
{"type":"result","result":"done","duration_ms":10,"num_turns":2,"total_cost_usd":0.5}
STREAM
`)

	result, err := Invoke(context.Background(), Options{
		Prompt:     "go",
		Model:      "opus",
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Model != "claude-sonnet-4-6" {
		t.Errorf("result.Model = %q, want the fallback model", result.Model)
	}
}

func TestInvokeModelEmptyWhenStreamNeverNamesOne(t *testing.T) {
	script := writeScript(t, `
cat <<'STREAM'
{"type":"system","subtype":"init","session_id":"s1"}
{"type":"result","result":"done","duration_ms":10,"num_turns":1,"total_cost_usd":0.5}
STREAM
`)

	result, err := Invoke(context.Background(), Options{
		Prompt:     "go",
		Model:      "opus",
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The runner falls back to the configured model, so an empty value here is
	// the contract rather than a bug.
	if result.Model != "" {
		t.Errorf("result.Model = %q, want empty", result.Model)
	}
}

// TestInvokeSessionNotFound covers a --resume target the CLI cannot resolve: it
// reports the failure only in the result event's `errors` array and exits 1, so
// without classification the caller sees a bare "exit status 1" and retries the
// same doomed resume.
func TestInvokeSessionNotFound(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF2'
{"type":"result","subtype":"error_during_execution","duration_ms":0,"is_error":true,"num_turns":0,"total_cost_usd":0,"session_id":"gone-42","errors":["No conversation found with session ID: gone-42"]}
EOF2
exit 1
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:          "continue work",
		Model:           "opus",
		BudgetUSD:       5,
		ClaudePath:      script,
		WorkDir:         t.TempDir(),
		ResumeSessionID: "gone-42",
	})
	if !IsSessionNotFound(err) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	var e *ErrSessionNotFound
	if errors.As(err, &e) && e.SessionID != "gone-42" {
		t.Errorf("SessionID = %q, want gone-42", e.SessionID)
	}
}

// A fresh invocation that happens to carry an errors array is just a failed
// run: with no --resume there is no session to fall back from, and classifying
// it as one would restart work that never had a transcript to lose.
func TestInvokeSessionNotFoundOnlyWhenResuming(t *testing.T) {
	script := writeScript(t, `
cat <<'EOF2'
{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":0,"errors":["No conversation found with session ID: whatever"]}
EOF2
exit 1
`)

	_, err := Invoke(context.Background(), Options{
		Prompt:     "start work",
		Model:      "opus",
		BudgetUSD:  5,
		ClaudePath: script,
		WorkDir:    t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected the exit status to surface as an error")
	}
	if IsSessionNotFound(err) {
		t.Errorf("err = %v, should not be classified as a missing resume target", err)
	}
}
