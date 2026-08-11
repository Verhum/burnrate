package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const maxIdleCycles = 5

// mcpAllowedTools is the permission grant handed to runs that get an MCP
// config. It is the burnrate human-loop server's whole tool surface: the
// server name is fixed by writeMCPConfig (internal/runner), and a wildcard
// grant means adding a tool to the server does not also require a change here.
const mcpAllowedTools = "mcp__burnrate-human-loop__*"

// defaultDenialGrace bounds how long a process may sit silent after an
// auto-denied tool call before we conclude the agent has taken the denial's
// "wait for the user" instruction literally and stopped. Without this a denial
// can idle away the whole rate-limit window (observed: a 7m timeout kill whose
// last useful event was the denial).
const defaultDenialGrace = 90 * time.Second

// Options configures a Claude CLI invocation.
type Options struct {
	Prompt          string
	Model           string
	BudgetUSD       int
	ResumeSessionID string
	WorkDir         string
	Timeout         time.Duration
	OnPID           func(pid int)
	OnSessionID     func(id string)
	OnEvent         func(line string)
	// OnDenial fires for each auto-denied tool call observed in the stream, for
	// logging. The run's outcome is decided by ErrToolDenied, not by this hook.
	OnDenial   func(message string)
	Stderr     io.Writer
	ClaudePath string
	DryRun     bool
	ExtraEnv   []string
	// MCPConfigPath, when set, is passed as --mcp-config to the claude CLI.
	MCPConfigPath string
	// DenialGrace is how long to wait for the agent to resume work after an
	// auto-denied tool call before killing the process and reporting
	// ErrToolDenied. Zero uses defaultDenialGrace.
	DenialGrace time.Duration
}

// Result captures metadata from a completed Claude invocation.
type Result struct {
	SessionID  string
	ResultText string
	CostUSD    float64
	NumTurns   int
	DurationMS int64
	// Model is the model id the CLI reported for the session, which is what
	// actually ran rather than what was asked for (an alias like "opus"
	// resolves to a concrete version, and a fallback may substitute another
	// model entirely). Empty if the stream never named one.
	Model string
}

// ErrTimeout is returned when a Claude invocation exceeds its wall-clock timeout.
type ErrTimeout struct {
	Duration time.Duration
}

func (e *ErrTimeout) Error() string {
	return fmt.Sprintf("claude timed out after %s", e.Duration)
}

// ErrIdleLoop is returned when Claude produces consecutive zero-work result cycles.
type ErrIdleLoop struct {
	Cycles int
}

func (e *ErrIdleLoop) Error() string {
	return fmt.Sprintf("claude killed after %d consecutive idle cycles", e.Cycles)
}

// ErrSessionNotFound is returned when --resume named a session the CLI cannot
// find. The transcript lives under CLAUDE_CONFIG_DIR keyed by the launch
// directory, so a session created under one account's config dir is invisible
// once the daemon is pointed at another — and the id burnrate persisted stays
// permanently unresumable. Retrying --resume can only fail the same way, so
// callers must fall back to a fresh session instead.
type ErrSessionNotFound struct {
	SessionID string
}

func (e *ErrSessionNotFound) Error() string {
	return fmt.Sprintf("claude has no conversation for session id %s", e.SessionID)
}

// IsSessionNotFound reports whether err came from an unresolvable --resume target.
func IsSessionNotFound(err error) bool {
	var e *ErrSessionNotFound
	return errors.As(err, &e)
}

// mentionsMissingSession matches the CLI's report of an unknown --resume target.
// Matched on the stable prefix rather than the whole sentence so a reworded tail
// (or a quoted vs bare id) still classifies.
func mentionsMissingSession(errs []string) bool {
	for _, s := range errs {
		if strings.Contains(strings.ToLower(s), "no conversation found") {
			return true
		}
	}
	return false
}

func isIdleCycle(evt Event) bool {
	return evt.Type == "result" && evt.NumTurns <= 1 && evt.DurationMS == 0
}

// Invoke runs the Claude CLI with streaming JSON output, parsing events in real time.
func Invoke(ctx context.Context, opts Options) (Result, error) {
	if opts.DryRun {
		return invokeDryRun(ctx, opts)
	}

	claudePath := opts.ClaudePath
	if claudePath == "" {
		claudePath = "claude"
	}

	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "auto",
		"--model", opts.Model,
		"--max-budget-usd", strconv.Itoa(opts.BudgetUSD),
	}
	if opts.MCPConfigPath != "" {
		args = append(args, "--mcp-config", opts.MCPConfigPath)
		// --permission-mode auto does NOT cover MCP tools. Empirically (claude
		// 2.1.220): a call to an --mcp-config server's tool comes back
		// "Claude requested permissions to use mcp__…, but you haven't granted
		// it yet" with non_execution_kind "user-rejected" — which is exactly the
		// shape DenialFrom classifies as ErrToolDenied, so every human-loop call
		// would be misread as "the agent is idling for a human" and auto-continued.
		// The server-wide wildcard is verified to grant every tool on the server
		// (a tool covered only by the wildcard executed), so the list does not
		// have to enumerate them.
		//
		// Both --mcp-config and --allowedTools are variadic and swallow any
		// following bare argument: everything appended after this point must
		// start with a dash.
		args = append(args, "--allowedTools", mcpAllowedTools)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, "-p", opts.Prompt)

	cmd := exec.Command(claudePath, args...)
	cmd.Dir = opts.WorkDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(opts.ExtraEnv) > 0 {
		cmd.Env = append(os.Environ(), opts.ExtraEnv...)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("starting claude: %w", err)
	}

	if opts.OnPID != nil {
		opts.OnPID(cmd.Process.Pid)
	}

	var timedOut atomic.Bool
	done := make(chan struct{})
	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if opts.Timeout > 0 {
		timer = time.NewTimer(opts.Timeout)
		timeoutCh = timer.C
	}
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
			killProcessGroup(cmd.Process.Pid)
		case <-timeoutCh:
			timedOut.Store(true)
			killProcessGroup(cmd.Process.Pid)
		}
	}()

	var rateLimitMsg string
	var rateLimitMu sync.Mutex

	// Denial tracking. denialPending means "the last thing that happened was an
	// auto-denied tool call and the agent has not resumed work since": it is set
	// on a denial event and cleared as soon as a tool actually runs again. The
	// watchdog only kills a process that has gone silent while pending, so an
	// agent that shrugs off the denial and keeps working is left alone.
	var denialPending atomic.Bool
	var denialKilled atomic.Bool
	var lastEventNanos atomic.Int64
	var denialMsg string
	var denialCount int
	var denialMu sync.Mutex
	seenDenials := map[string]bool{}
	var watchdogOnce sync.Once

	denialGrace := opts.DenialGrace
	if denialGrace <= 0 {
		denialGrace = defaultDenialGrace
	}
	startDenialWatchdog := func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case now := <-ticker.C:
					if !denialPending.Load() {
						continue
					}
					last := time.Unix(0, lastEventNanos.Load())
					if now.Sub(last) >= denialGrace {
						denialKilled.Store(true)
						killProcessGroup(cmd.Process.Pid)
						return
					}
				}
			}
		}()
	}

	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		sc := bufio.NewScanner(stderrPipe)
		sc.Buffer(make([]byte, 64*1024), 64*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if opts.Stderr != nil {
				opts.Stderr.Write(line)
				opts.Stderr.Write([]byte("\n"))
			}
			rateLimitMu.Lock()
			if rateLimitMsg == "" && IsRateLimitMessage(string(bytes.TrimSpace(line))) {
				rateLimitMsg = string(bytes.TrimSpace(line))
			}
			rateLimitMu.Unlock()
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var result Result
	idleCycles := 0
	var idleKilled bool
	var sessionIDFired bool
	var sessionMissing bool

	for scanner.Scan() {
		line := scanner.Bytes()

		if opts.OnEvent != nil {
			opts.OnEvent(string(line))
		}

		lastEventNanos.Store(time.Now().UnixNano())

		rateLimitMu.Lock()
		if rateLimitMsg == "" {
			rateLimitMsg = checkRateLimit(line)
		}
		rateLimitMu.Unlock()

		if denial, ok := DenialFrom(line); ok {
			// A policy denial arrives twice — as the system/permission_denied
			// event and again as the tool_result — so count distinct calls, not
			// events. An unidentified denial always counts: dropping it would
			// hide the only signal we have.
			denialMu.Lock()
			fresh := denial.ToolUseID == "" || !seenDenials[denial.ToolUseID]
			if fresh {
				denialCount++
				denialMsg = denial.Message
				if denial.ToolUseID != "" {
					seenDenials[denial.ToolUseID] = true
				}
			}
			denialMu.Unlock()
			denialPending.Store(true)
			if fresh && opts.OnDenial != nil {
				opts.OnDenial(denial.Message)
			}
			watchdogOnce.Do(startDenialWatchdog)
		} else if IsToolResultEvent(line) || IsToolUseEvent(line) {
			// The agent is acting again, so it is not sitting waiting for a
			// human. Clearing on tool_use as well as tool_result matters: a slow
			// call issued right after a denial (a full build, say) produces no
			// events for minutes and must not be mistaken for a stall.
			denialPending.Store(false)
		}

		evt, parseErr := ParseLine(line)
		if parseErr != nil {
			continue
		}

		// Fire OnSessionID exactly once, the instant the first system/init
		// event yields a session id (burnrate addition for early crash-resume
		// persistence). claude may emit further system events carrying the
		// same id; we must not re-fire.
		if !sessionIDFired && evt.Type == "system" && evt.SessionID != "" {
			result.SessionID = evt.SessionID
			sessionIDFired = true
			if opts.OnSessionID != nil {
				opts.OnSessionID(evt.SessionID)
			}
		}

		// Both system/init and result events name the model; take the latest
		// non-empty one so a mid-run fallback is what gets recorded.
		if evt.Model != "" {
			result.Model = evt.Model
		}

		if evt.Type == "result" {
			result.ResultText = evt.Result
			result.CostUSD = evt.TotalCost
			result.NumTurns = evt.NumTurns
			result.DurationMS = int64(evt.DurationMS)
			if opts.ResumeSessionID != "" && mentionsMissingSession(evt.Errors) {
				sessionMissing = true
			}
		}

		if isIdleCycle(evt) {
			idleCycles++
			if idleCycles >= maxIdleCycles {
				killProcessGroup(cmd.Process.Pid)
				idleKilled = true
				break
			}
		} else if evt.Type != "system" && evt.Type != "" {
			idleCycles = 0
		}
	}

	stderrWg.Wait()
	close(done)
	if timer != nil {
		timer.Stop()
	}

	waitErr := cmd.Wait()

	if rateLimitMsg != "" {
		resetAt := ParseResetTime(rateLimitMsg)
		return result, &ErrRateLimited{
			ResetAt: resetAt,
			Message: rateLimitMsg,
		}
	}

	if idleKilled {
		return result, &ErrIdleLoop{Cycles: idleCycles}
	}

	if timedOut.Load() {
		return result, &ErrTimeout{Duration: opts.Timeout}
	}

	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	// Checked before waitErr: the CLI exits 1 here, so the generic exit-status
	// error would otherwise mask the one failure the caller can recover from.
	if sessionMissing {
		return result, &ErrSessionNotFound{SessionID: opts.ResumeSessionID}
	}

	// Denial is reported only when we did not kill the process for another
	// reason. The CLI marks an in-flight tool call "user-rejected" whenever it is
	// interrupted, so a timeout or cancellation kill leaves a denial in the
	// transcript too — those checks above must win, or every timeout would be
	// misreported as a denial and auto-continued past its window.
	denialMu.Lock()
	pendingMsg, pendingCount := denialMsg, denialCount
	denialMu.Unlock()
	if (denialPending.Load() || denialKilled.Load()) && pendingCount > 0 {
		return result, &ErrToolDenied{Message: pendingMsg, Denials: pendingCount}
	}

	if waitErr != nil {
		return result, waitErr
	}

	return result, nil
}

func invokeDryRun(ctx context.Context, opts Options) (Result, error) {
	id := fmt.Sprintf("dryrun-%d", time.Now().UnixNano())
	if opts.OnSessionID != nil {
		opts.OnSessionID(id)
	}
	if opts.WorkDir != "" {
		os.WriteFile(filepath.Join(opts.WorkDir, "BURNRATE_DRYRUN.md"), []byte(opts.Prompt), 0644)
	}
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return Result{SessionID: id}, ctx.Err()
	}
	return Result{SessionID: id}, nil
}

func killProcessGroup(pid int) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return
	}
	syscall.Kill(-pgid, syscall.SIGTERM)

	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			syscall.Kill(-pgid, syscall.SIGKILL)
			return
		case <-ticker.C:
			if err := syscall.Kill(-pgid, 0); err != nil {
				return
			}
		}
	}
}

func checkRateLimit(raw []byte) string {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return ""
	}

	if evt.Type == "result" && IsRateLimitMessage(evt.Result) {
		return evt.Result
	}

	if evt.Type == "assistant" && evt.Message != nil {
		var msg AssistantMessage
		if err := json.Unmarshal(evt.Message, &msg); err == nil {
			for _, block := range msg.Content {
				if block.Type == "text" && IsRateLimitMessage(block.Text) {
					return block.Text
				}
			}
		}
	}

	return ""
}
