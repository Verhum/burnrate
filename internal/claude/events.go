package claude

import "encoding/json"

// Event represents a top-level event from `claude --output-format stream-json`.
type Event struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype,omitempty"`
	Message    json.RawMessage `json:"message,omitempty"`
	Result     string          `json:"result,omitempty"`
	DurationMS int             `json:"duration_ms,omitempty"`
	NumTurns   int             `json:"num_turns,omitempty"`
	TotalCost  float64         `json:"total_cost_usd,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	Model      string          `json:"model,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`

	// Errors carries CLI-level failures attached to a result event, as opposed
	// to anything the model did. The one that matters here is an unresolvable
	// --resume target ("No conversation found with session ID: ..."), which the
	// CLI reports with num_turns 0, cost 0 and exit status 1 — indistinguishable
	// from a generic crash without this field.
	Errors []string `json:"errors,omitempty"`

	// Tool-rejection signals attached to `user` events by the CLI. ToolUseResult
	// is a RawMessage because its shape varies by CLI version (a bare string
	// like "User rejected tool use", or an object).
	ToolUseResult  json.RawMessage  `json:"tool_use_result,omitempty"`
	ToolResultMeta []ToolResultMeta `json:"tool_result_meta,omitempty"`

	// Fields of the `system`/`permission_denied` event, which the CLI emits when
	// a tool call is auto-denied with no interactive prompt (deny rule, dontAsk
	// mode, auto-mode classifier, headless auto-deny). DecisionReasonType is the
	// deciding component ("rule", "mode", "classifier", "asyncAgent").
	ToolName           string `json:"tool_name,omitempty"`
	ToolUseID          string `json:"tool_use_id,omitempty"`
	DecisionReasonType string `json:"decision_reason_type,omitempty"`
	DecisionReason     string `json:"decision_reason,omitempty"`
}

// SystemMessageText decodes the `message` field of a system event, which is a
// bare JSON string rather than the message object carried by assistant and user
// events. Returns "" when message is absent or not a string.
func (e Event) SystemMessageText() string {
	if len(e.Message) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(e.Message, &s); err != nil {
		return ""
	}
	return s
}

// ToolResultMeta carries per-tool-result metadata, notably why a tool did not
// execute (e.g. non_execution_kind "user-rejected").
type ToolResultMeta struct {
	ID               string `json:"id,omitempty"`
	NonExecutionKind string `json:"non_execution_kind,omitempty"`
}

// AssistantMessage is the decoded content of an assistant event's message field.
type AssistantMessage struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock is a single block within a message.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// ParseLine parses a raw stream-json line into an Event.
// Unknown event types are returned without error; only JSON parse failures error.
func ParseLine(raw []byte) (Event, error) {
	var evt Event
	if err := json.Unmarshal(raw, &evt); err != nil {
		return Event{}, err
	}
	return evt, nil
}
