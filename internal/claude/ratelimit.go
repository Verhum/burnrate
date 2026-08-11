package claude

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrRateLimited is returned when Claude CLI reports a rate limit.
type ErrRateLimited struct {
	ResetAt time.Time
	Message string
}

func (e *ErrRateLimited) Error() string {
	if !e.ResetAt.IsZero() {
		return fmt.Sprintf("claude rate limited: %s (resets at %s)", e.Message, e.ResetAt.Format("15:04 MST"))
	}
	return fmt.Sprintf("claude rate limited: %s", e.Message)
}

var resetTimePattern = regexp.MustCompile(`resets\s+(\d{1,2}(?::\d{2})?\s*(?:am|pm))\s+\(([^)]+)\)`)

// IsRateLimitMessage checks if a string contains a Claude rate limit message.
// Matches both straight and curly apostrophes.
func IsRateLimitMessage(s string) bool {
	return strings.Contains(s, "You've hit your limit") ||
		strings.Contains(s, "You’ve hit your limit")
}

// ParseResetTime extracts the reset time from a rate limit message.
// Returns zero time if parsing fails.
func ParseResetTime(msg string) time.Time {
	matches := resetTimePattern.FindStringSubmatch(msg)
	if len(matches) < 3 {
		return time.Time{}
	}

	timeStr := strings.TrimSpace(matches[1])
	tzName := strings.TrimSpace(matches[2])

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return time.Time{}
	}

	now := time.Now().In(loc)

	if !strings.Contains(timeStr, ":") {
		timeStr = strings.Replace(timeStr, "am", ":00am", 1)
		timeStr = strings.Replace(timeStr, "pm", ":00pm", 1)
	}

	parsed, err := time.Parse("3:04pm", strings.ToLower(timeStr))
	if err != nil {
		return time.Time{}
	}

	resetTime := time.Date(now.Year(), now.Month(), now.Day(),
		parsed.Hour(), parsed.Minute(), 0, 0, loc)

	if resetTime.Before(now) {
		resetTime = resetTime.Add(24 * time.Hour)
	}

	return resetTime
}
