package claude

import (
	"fmt"
	"testing"
	"time"
)

func TestIsRateLimitMessage(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"You've hit your limit · resets 10pm (America/New_York)", true},
		{"You've hit your limit", true},
		{"You’ve hit your limit · resets 2am (America/Los_Angeles)", true},
		{"You’ve hit your limit", true},
		{"Build succeeded", false},
		{"", false},
		{"rate limit exceeded", false},
		{"Something You've hit your limit something", true},
		{"Something You’ve hit your limit something", true},
	}
	for _, tc := range tests {
		result := IsRateLimitMessage(tc.input)
		if result != tc.expected {
			t.Errorf("IsRateLimitMessage(%q) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}

func TestParseResetTime(t *testing.T) {
	msg := "You've hit your limit · resets 10pm (America/New_York)"
	resetAt := ParseResetTime(msg)
	if resetAt.IsZero() {
		t.Fatal("Expected non-zero reset time")
	}

	loc, _ := time.LoadLocation("America/New_York")
	if resetAt.Location().String() != loc.String() {
		t.Errorf("Expected timezone %s, got %s", loc, resetAt.Location())
	}
	if resetAt.Hour() != 22 || resetAt.Minute() != 0 {
		t.Errorf("Expected 22:00, got %02d:%02d", resetAt.Hour(), resetAt.Minute())
	}
}

func TestParseResetTime_AM(t *testing.T) {
	msg := "You've hit your limit · resets 2am (America/Los_Angeles)"
	resetAt := ParseResetTime(msg)
	if resetAt.IsZero() {
		t.Fatal("Expected non-zero reset time")
	}
	if resetAt.Hour() != 2 || resetAt.Minute() != 0 {
		t.Errorf("Expected 02:00, got %02d:%02d", resetAt.Hour(), resetAt.Minute())
	}
}

func TestParseResetTime_WithMinutes(t *testing.T) {
	msg := "resets 10:30pm (America/New_York)"
	resetAt := ParseResetTime(msg)
	if resetAt.IsZero() {
		t.Fatal("Expected non-zero reset time")
	}
	if resetAt.Hour() != 22 || resetAt.Minute() != 30 {
		t.Errorf("Expected 22:30, got %02d:%02d", resetAt.Hour(), resetAt.Minute())
	}
}

func TestParseResetTime_CurlyApostrophe(t *testing.T) {
	msg := "You’ve hit your limit · resets 3pm (America/Chicago)"
	resetAt := ParseResetTime(msg)
	if resetAt.IsZero() {
		t.Fatal("Expected non-zero reset time for curly apostrophe message")
	}
	if resetAt.Hour() != 15 || resetAt.Minute() != 0 {
		t.Errorf("Expected 15:00, got %02d:%02d", resetAt.Hour(), resetAt.Minute())
	}
}

func TestParseResetTime_RollsToTomorrow(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(loc)

	// Build a reset time that's already passed today
	pastHour := now.Hour() - 1
	if pastHour < 0 {
		t.Skip("Cannot test rolling at midnight boundary")
	}
	var ampm string
	displayHour := pastHour
	if pastHour == 0 {
		displayHour = 12
		ampm = "am"
	} else if pastHour < 12 {
		ampm = "am"
	} else if pastHour == 12 {
		ampm = "pm"
	} else {
		displayHour = pastHour - 12
		ampm = "pm"
	}

	msg := fmt.Sprintf("resets %d%s (America/New_York)", displayHour, ampm)
	resetAt := ParseResetTime(msg)
	if resetAt.IsZero() {
		t.Fatal("Expected non-zero reset time")
	}
	if !resetAt.After(now) {
		t.Errorf("Expected reset time (%v) to be after now (%v)", resetAt, now)
	}
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, pastHour, 0, 0, 0, loc)
	if resetAt.Hour() != tomorrow.Hour() || resetAt.Day() != tomorrow.Day() {
		t.Errorf("Expected tomorrow %02d:00, got %v", pastHour, resetAt)
	}
}

func TestParseResetTime_NoMatch(t *testing.T) {
	resetAt := ParseResetTime("Build succeeded")
	if !resetAt.IsZero() {
		t.Errorf("Expected zero time, got %v", resetAt)
	}
}

func TestParseResetTime_InvalidTimezone(t *testing.T) {
	msg := "resets 10pm (Fake/Timezone)"
	resetAt := ParseResetTime(msg)
	if !resetAt.IsZero() {
		t.Errorf("Expected zero time for invalid timezone, got %v", resetAt)
	}
}

func TestCheckRateLimit_ResultEvent(t *testing.T) {
	raw := `{"type":"result","result":"You've hit your limit · resets 10pm (America/New_York)","duration_ms":0,"num_turns":1,"total_cost_usd":3.24}`
	msg := checkRateLimit([]byte(raw))
	if msg == "" {
		t.Fatal("Expected rate limit message from result event")
	}
	if !IsRateLimitMessage(msg) {
		t.Errorf("Expected rate limit message, got: %s", msg)
	}
}

func TestCheckRateLimit_AssistantEvent(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"You've hit your limit · resets 10pm (America/New_York)"}]}}`
	msg := checkRateLimit([]byte(raw))
	if msg == "" {
		t.Fatal("Expected rate limit message from assistant event")
	}
}

func TestCheckRateLimit_NoRateLimit(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"I will read the file now."}]}}`
	msg := checkRateLimit([]byte(raw))
	if msg != "" {
		t.Errorf("Expected empty string, got: %s", msg)
	}
}

func TestCheckRateLimit_InvalidJSON(t *testing.T) {
	msg := checkRateLimit([]byte("not json"))
	if msg != "" {
		t.Errorf("Expected empty string for invalid JSON, got: %s", msg)
	}
}

func TestErrRateLimited_Error(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	resetAt := time.Date(2025, 1, 15, 22, 0, 0, 0, loc)

	err := &ErrRateLimited{
		ResetAt: resetAt,
		Message: "You've hit your limit",
	}
	errStr := err.Error()
	if errStr == "" {
		t.Fatal("Expected non-empty error string")
	}

	err2 := &ErrRateLimited{
		Message: "You've hit your limit",
	}
	errStr2 := err2.Error()
	if errStr2 == "" {
		t.Fatal("Expected non-empty error string")
	}
}
