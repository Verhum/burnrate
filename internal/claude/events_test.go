package claude

import (
	"testing"
)

func TestParseLineValid(t *testing.T) {
	raw := `{"type":"system","subtype":"init","session_id":"abc123","model":"opus"}`
	evt, err := ParseLine([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Type != "system" {
		t.Errorf("expected type=system, got %q", evt.Type)
	}
	if evt.Subtype != "init" {
		t.Errorf("expected subtype=init, got %q", evt.Subtype)
	}
	if evt.SessionID != "abc123" {
		t.Errorf("expected session_id=abc123, got %q", evt.SessionID)
	}
	if evt.Model != "opus" {
		t.Errorf("expected model=opus, got %q", evt.Model)
	}
}

func TestParseLineResult(t *testing.T) {
	raw := `{"type":"result","result":"Done","duration_ms":120000,"num_turns":15,"total_cost_usd":3.45}`
	evt, err := ParseLine([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Type != "result" {
		t.Errorf("expected type=result, got %q", evt.Type)
	}
	if evt.Result != "Done" {
		t.Errorf("expected result=Done, got %q", evt.Result)
	}
	if evt.NumTurns != 15 {
		t.Errorf("expected num_turns=15, got %d", evt.NumTurns)
	}
	if evt.TotalCost != 3.45 {
		t.Errorf("expected total_cost=3.45, got %f", evt.TotalCost)
	}
	if evt.DurationMS != 120000 {
		t.Errorf("expected duration_ms=120000, got %d", evt.DurationMS)
	}
}

func TestParseLineInvalidJSON(t *testing.T) {
	_, err := ParseLine([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseLineUnknownType(t *testing.T) {
	raw := `{"type":"unknown_event","data":"something"}`
	evt, err := ParseLine([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error for unknown type: %v", err)
	}
	if evt.Type != "unknown_event" {
		t.Errorf("expected type=unknown_event, got %q", evt.Type)
	}
}

func TestParseLineAssistantMessage(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}`
	evt, err := ParseLine([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Type != "assistant" {
		t.Errorf("expected type=assistant, got %q", evt.Type)
	}
	if evt.Message == nil {
		t.Error("expected non-nil message")
	}
}
