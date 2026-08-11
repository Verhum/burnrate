package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Auto-denial handling.
//
// burnrate runs claude with --permission-mode auto and no human at the
// keyboard, yet tool calls can still come back rejected. The tool result claude
// feeds back to the model ends with "STOP what you are doing and wait for the
// user to tell you how to proceed" — advice that is actively harmful
// unattended, because there is no user to wait for. We detect that state so the
// runner can nudge the agent to continue instead of letting the run fail.
//
// There are two distinct producers, and telling them apart matters:
//
//  1. A real auto-deny — a `permissions.deny` rule, `dontAsk` mode, the
//     auto-mode classifier, or a PreToolUse hook vetoes the call. The CLI emits
//     a `system`/`permission_denied` event naming the tool and the deciding
//     component, and the tool_result text is whatever the denying component
//     chose (not necessarily the standard phrase). PermissionDeniedFromEvent
//     catches these.
//
//  2. An interruption. The CLI's synthetic message for an aborted in-flight
//     tool call is stamped non_execution_kind "user-rejected" and carries the
//     standard "wait for the user" phrase, with no permission_denied event.
//     DenialFromEvent catches these.
//
// Forensics over every burnrate run log to date found *only* case 2: three
// "user-rejected" results, zero permission_denied events, each landing two lines
// from the end of its log with a timestamp 1-3s before burnrate's own timeout
// kill. So the denial is a symptom of the kill, not a cause of the stall — which
// is why Invoke checks rate-limit/idle/timeout/cancel *before* reporting
// ErrToolDenied, and why the real damage is downstream: the killed transcript
// now ends with "wait for the user", and the next --resume reads that as its
// freshest instruction. See LogEndedInterrupted.
//
// No CLI flag suppresses case 2: the message is how the CLI closes out an
// aborted turn, and --permission-mode auto (already passed) is the only lever
// over case 1.

// denialPrefixes are the leading texts of the tool_result claude synthesizes
// for a rejected tool call. Matched as a prefix (not a substring) so a tool
// result that merely *quotes* the phrase — a grep over burnrate's own logs, for
// instance — is not mistaken for a denial.
var denialPrefixes = []string{
	"the user doesn't want to proceed with this tool use",
	"the user doesn't want to take this action right now",
}

// interruptMarkers are the synthetic user-turn texts claude appends when a turn
// is cut short around a tool call. They accompany a denial in the transcript
// and are the only trace left in some CLI versions.
var interruptMarkers = []string{
	"[request interrupted by user for tool use]",
	"[request interrupted by user]",
}

// DeniedToolMessage is the canonical text reported when the structured
// rejection signal (tool_use_result / tool_result_meta) fires but the verbatim
// tool_result text is not available.
const DeniedToolMessage = "tool use was auto-denied by the permission layer"

// ErrToolDenied is returned when a tool call was rejected without a human in
// the loop and the agent stopped rather than continuing.
type ErrToolDenied struct {
	Message string
	Denials int
}

func (e *ErrToolDenied) Error() string {
	msg := e.Message
	if msg == "" {
		msg = DeniedToolMessage
	}
	if e.Denials > 1 {
		return fmt.Sprintf("claude stopped after %d auto-denied tool calls: %s", e.Denials, msg)
	}
	return fmt.Sprintf("claude stopped after an auto-denied tool call: %s", msg)
}

// IsToolDenied reports whether err is an ErrToolDenied.
func IsToolDenied(err error) bool {
	_, ok := err.(*ErrToolDenied)
	return ok
}

// Denial describes an observed tool rejection.
type Denial struct {
	Message string
	// ToolUseID identifies the rejected call, when the event names it. Callers
	// counting distinct denials must dedupe on it: a policy denial surfaces
	// twice, first as the system/permission_denied event and again as the
	// tool_result fed back to the model.
	ToolUseID string
}

// DenialFrom inspects raw for either denial signal — the structured
// system/permission_denied event or a rejected tool_result on a user event — and
// reports the denial when one fires.
func DenialFrom(raw []byte) (Denial, bool) {
	if msg := PermissionDeniedFromEvent(raw); msg != "" {
		var evt Event
		_ = json.Unmarshal(raw, &evt)
		return Denial{Message: msg, ToolUseID: evt.ToolUseID}, true
	}
	if msg := DenialFromEvent(raw); msg != "" {
		return Denial{Message: msg, ToolUseID: deniedToolUseID(raw)}, true
	}
	return Denial{}, false
}

// deniedToolUseID pulls the rejected call's id off a user event, preferring
// tool_result_meta and falling back to the tool_result block itself.
func deniedToolUseID(raw []byte) string {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return ""
	}
	for _, m := range evt.ToolResultMeta {
		if m.ID != "" {
			return m.ID
		}
	}
	var msg AssistantMessage
	if err := json.Unmarshal(evt.Message, &msg); err != nil {
		return ""
	}
	for _, block := range msg.Content {
		if block.Type == "tool_result" && block.ToolUseID != "" {
			return block.ToolUseID
		}
	}
	return ""
}

// DenialFromEvent returns the denial text when raw is a stream-json event
// recording a rejected tool call, or "" otherwise.
//
// Three signals are accepted, in order of reliability:
//  1. tool_result_meta[].non_execution_kind — e.g. "user-rejected"
//  2. tool_use_result — e.g. "User rejected tool use"
//  3. an is_error tool_result block whose text starts with the denial phrase
//
// Only `user` events qualify: an assistant narrating the denial phrase (or a
// Bash result echoing it) must not trip the detector.
func DenialFromEvent(raw []byte) string {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return ""
	}
	if evt.Type != "user" {
		return ""
	}

	blockText := denialTextFromBlocks(evt.Message)

	for _, m := range evt.ToolResultMeta {
		if isRejectedKind(m.NonExecutionKind) {
			if blockText != "" {
				return blockText
			}
			return DeniedToolMessage
		}
	}

	if len(evt.ToolUseResult) > 0 {
		lowered := strings.ToLower(string(evt.ToolUseResult))
		if strings.Contains(lowered, "rejected tool use") || strings.Contains(lowered, "denied tool use") {
			if blockText != "" {
				return blockText
			}
			return DeniedToolMessage
		}
	}

	return blockText
}

// PermissionDeniedFromEvent returns the rejection message when raw is a
// `system`/`permission_denied` event, or "" otherwise.
//
// This is the CLI's structured, unambiguous signal that a tool call was denied
// by policy rather than by an interruption. Unlike DenialFromEvent it does not
// depend on the wording of the tool_result, so it also catches denials whose
// message a deny rule or hook chose itself. The returned text is annotated with
// the tool name and deciding component so the nudge sent back to the agent can
// say which call was blocked and why.
func PermissionDeniedFromEvent(raw []byte) string {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return ""
	}
	if evt.Type != "system" || evt.Subtype != "permission_denied" {
		return ""
	}

	msg := strings.TrimSpace(evt.SystemMessageText())
	if msg == "" {
		msg = DeniedToolMessage
	}

	var detail []string
	if evt.ToolName != "" {
		detail = append(detail, evt.ToolName)
	}
	if reason := strings.TrimSpace(evt.DecisionReason); reason != "" {
		detail = append(detail, reason)
	} else if evt.DecisionReasonType != "" {
		detail = append(detail, evt.DecisionReasonType)
	}
	if len(detail) > 0 {
		return fmt.Sprintf("%s (%s)", msg, strings.Join(detail, ": "))
	}
	return msg
}

// InterruptFromEvent returns the interrupt text when raw is one of claude's
// synthetic "[Request interrupted by user...]" user turns, or "" otherwise.
func InterruptFromEvent(raw []byte) string {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return ""
	}
	if evt.Type != "user" {
		return ""
	}
	var msg AssistantMessage
	if err := json.Unmarshal(evt.Message, &msg); err != nil {
		return ""
	}
	for _, block := range msg.Content {
		if block.Type != "text" {
			continue
		}
		trimmed := strings.ToLower(strings.TrimSpace(block.Text))
		for _, marker := range interruptMarkers {
			if strings.HasPrefix(trimmed, marker) {
				return strings.TrimSpace(block.Text)
			}
		}
	}
	return ""
}

// IsToolResultEvent reports whether raw is a user event carrying a tool result,
// i.e. evidence that a tool actually ran. Used to tell "the agent kept working
// after the denial" from "the agent stopped and is waiting".
func IsToolResultEvent(raw []byte) bool {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return false
	}
	if evt.Type != "user" {
		return false
	}
	var msg AssistantMessage
	if err := json.Unmarshal(evt.Message, &msg); err != nil {
		return false
	}
	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
}

// IsToolUseEvent reports whether raw is an assistant event issuing a tool call.
func IsToolUseEvent(raw []byte) bool {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return false
	}
	if evt.Type != "assistant" || len(evt.Message) == 0 {
		return false
	}
	var msg AssistantMessage
	if err := json.Unmarshal(evt.Message, &msg); err != nil {
		return false
	}
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

func denialTextFromBlocks(rawMsg json.RawMessage) string {
	if len(rawMsg) == 0 {
		return ""
	}
	var msg AssistantMessage
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return ""
	}
	for _, block := range msg.Content {
		if block.Type != "tool_result" || !block.IsError {
			continue
		}
		text := strings.TrimSpace(contentText(block.Content))
		if hasDenialPrefix(text) {
			return text
		}
	}
	return ""
}

func hasDenialPrefix(text string) bool {
	lowered := strings.ToLower(strings.TrimSpace(text))
	for _, p := range denialPrefixes {
		if strings.HasPrefix(lowered, p) {
			return true
		}
	}
	return false
}

// rejectedKinds are the non_execution_kind values that mean a tool call was
// vetoed and the model was told to wait for a human. The CLI's full enum is
// user-rejected, permission-rule, automode-blocked, automode-unavailable,
// automode-parsing-error, interrupted, cancelled; the last two are deliberately
// absent, since a run we aborted ourselves is classified by why we aborted it.
var rejectedKinds = map[string]bool{
	"user-rejected":          true,
	"permission-rule":        true,
	"automode-blocked":       true,
	"automode-unavailable":   true,
	"automode-parsing-error": true,
}

func isRejectedKind(kind string) bool {
	k := strings.ToLower(strings.TrimSpace(kind))
	if rejectedKinds[k] {
		return true
	}
	// Tolerate kinds a newer CLI may add (e.g. "hook-denied"), while still
	// excluding the abort kinds handled above.
	if k == "interrupted" || k == "cancelled" || k == "canceled" {
		return false
	}
	return strings.Contains(k, "reject") || strings.Contains(k, "denied") || strings.Contains(k, "deny")
}

// contentText flattens a tool_result content field, which the CLI emits either
// as a bare string or as an array of content blocks.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// logTailLines is how many trailing stream-json lines LogEndedInterrupted
// inspects. A denial lands 2-3 lines from the end (denial, interrupt, result),
// so this is generous while staying cheap.
const logTailLines = 40

// LogEndedInterrupted reports whether the tail of a run's stream-json log shows
// the session ending on a denied or interrupted tool call. The runner uses this
// to warn a *resuming* agent that the last thing in its transcript is a bogus
// "wait for the user" instruction, so it continues instead of stopping again.
func LogEndedInterrupted(path string) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()

	tail := make([]string, 0, logTailLines)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if len(tail) == logTailLines {
			tail = tail[1:]
		}
		tail = append(tail, line)
	}

	// A denial only matters if it is the *last* thing that happened: an agent
	// that shrugged off the denial and went on working needs no nudge.
	var msg string
	for _, line := range tail {
		raw := []byte(line)
		if d, ok := DenialFrom(raw); ok {
			msg = d.Message
			continue
		}
		if i := InterruptFromEvent(raw); i != "" {
			if msg == "" {
				msg = i
			}
			continue
		}
		if IsToolResultEvent(raw) {
			msg = ""
		}
	}
	return msg != "", msg
}
