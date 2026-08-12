package notify

import (
	"encoding/json"
	"testing"
)

func TestReviewUsesCallbackWhenSet(t *testing.T) {
	var callbackTitle, callbackBody string
	SetNotifyFunc(func(n Notification) error {
		callbackTitle = n.Title
		callbackBody = n.Body
		return nil
	})
	t.Cleanup(func() { SetNotifyFunc(nil) })

	if err := Review("BR10", "add feature", "https://github.com/org/repo/pull/5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callbackTitle != "burnrate" {
		t.Fatalf("expected title %q, got %q", "burnrate", callbackTitle)
	}
	if !contains(callbackBody, "BR10 add feature — ready for review") {
		t.Fatalf("expected body to contain task info, got %q", callbackBody)
	}
	if !contains(callbackBody, "https://github.com/org/repo/pull/5") {
		t.Fatalf("expected body to contain PR URL, got %q", callbackBody)
	}
}

func TestReviewNoOpWhenCallbackNil(t *testing.T) {
	SetNotifyFunc(nil)

	if err := Review("BR99", "no handler test", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReviewNoPRURL(t *testing.T) {
	var callbackBody string
	SetNotifyFunc(func(n Notification) error {
		callbackBody = n.Body
		return nil
	})
	t.Cleanup(func() { SetNotifyFunc(nil) })

	if err := Review("BR7", "update docs", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(callbackBody, "BR7 update docs — ready for review") {
		t.Fatalf("expected body to contain task info, got %q", callbackBody)
	}
	if contains(callbackBody, "http") {
		t.Fatalf("expected body without URL, got %q", callbackBody)
	}
}

func TestReviewGatedBySetting(t *testing.T) {
	var called bool
	SetNotifyFunc(func(n Notification) error {
		called = true
		return nil
	})
	t.Cleanup(func() { SetNotifyFunc(nil) })

	if err := Review("BR1", "task", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("Review should always fire when called (gating is external)")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// The SSE `notification` contract: ids ride along when there is something to
// deep-link to, and are omitted from the JSON entirely when there is not.
func TestRequestCreatedCarriesIDs(t *testing.T) {
	var got Notification
	SetNotifyFunc(func(n Notification) error {
		got = n
		return nil
	})
	t.Cleanup(func() { SetNotifyFunc(nil) })

	if err := RequestCreated(42, 7, "check the modal"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TaskID != 42 || got.RequestID != 7 {
		t.Fatalf("expected task 42 / request 7, got %+v", got)
	}
	if !contains(got.Body, "BR42 check the modal") {
		t.Fatalf("unexpected body %q", got.Body)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"task_id":42`, `"request_id":7`, `"title":"burnrate"`} {
		if !contains(string(data), want) {
			t.Fatalf("payload %s missing %s", data, want)
		}
	}
}

func TestTaskFailedEmitsSSEAndEmail(t *testing.T) {
	var got Notification
	SetNotifyFunc(func(n Notification) error {
		got = n
		return nil
	})
	var emailSubject, emailBody string
	SetEmailFunc(func(subject, body string) error {
		emailSubject = subject
		emailBody = body
		return nil
	})
	t.Cleanup(func() {
		SetNotifyFunc(nil)
		SetEmailFunc(nil)
	})

	if err := TaskFailed(42, "BR42", "fix the widget", "connection refused"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.TaskID != 42 {
		t.Fatalf("expected task_id 42, got %d", got.TaskID)
	}
	if !contains(got.Body, "BR42 fix the widget — failed") {
		t.Fatalf("expected SSE body to contain task info, got %q", got.Body)
	}
	if !contains(got.Body, "connection refused") {
		t.Fatalf("expected SSE body to contain error, got %q", got.Body)
	}
	if !contains(emailSubject, "BR42") || !contains(emailSubject, "failed") {
		t.Fatalf("expected email subject to mention task and failure, got %q", emailSubject)
	}
	if !contains(emailBody, "connection refused") {
		t.Fatalf("expected email body to contain error, got %q", emailBody)
	}
}

func TestTaskFailedNoEmailWhenNotSet(t *testing.T) {
	SetNotifyFunc(func(n Notification) error { return nil })
	SetEmailFunc(nil)
	t.Cleanup(func() {
		SetNotifyFunc(nil)
	})

	if err := TaskFailed(1, "BR1", "task", "err"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTaskFailedTruncatesLongError(t *testing.T) {
	var got Notification
	SetNotifyFunc(func(n Notification) error {
		got = n
		return nil
	})
	t.Cleanup(func() {
		SetNotifyFunc(nil)
		SetEmailFunc(nil)
	})

	longErr := ""
	for i := 0; i < 300; i++ {
		longErr += "x"
	}

	if err := TaskFailed(1, "BR1", "task", longErr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Body) > 300 {
		t.Fatalf("expected SSE body to be truncated, got len %d", len(got.Body))
	}
	if !contains(got.Body, "...") {
		t.Fatalf("expected truncation marker, got %q", got.Body)
	}
}

func TestReviewOmitsZeroIDs(t *testing.T) {
	var got Notification
	SetNotifyFunc(func(n Notification) error {
		got = n
		return nil
	})
	t.Cleanup(func() { SetNotifyFunc(nil) })

	if err := Review("BR3", "task", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := json.Marshal(got)
	if contains(string(data), "task_id") || contains(string(data), "request_id") {
		t.Fatalf("zero ids must be omitted, got %s", data)
	}
}
