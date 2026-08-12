package notify

import (
	"fmt"
	"sync"

	"log"
)

// Notification is the payload every producer in this package emits and the
// wire shape of the SSE `notification` event. TaskID and RequestID are
// optional deep-link targets: they are omitted when zero, so a notification
// with nothing to link to stays the plain {title, body} the desktop app
// already understood.
type Notification struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	TaskID    int64  `json:"task_id,omitempty"`
	RequestID int64  `json:"request_id,omitempty"`
}

var (
	notifyFunc func(n Notification) error
	emailFunc  func(subject, body string) error
	mu         sync.RWMutex
)

// SetNotifyFunc sets the callback invoked when a notification should be
// displayed. The Tauri desktop app uses this to route notifications through
// the native macOS notification center. When no callback is set,
// notifications are silently dropped.
func SetNotifyFunc(fn func(n Notification) error) {
	mu.Lock()
	defer mu.Unlock()
	notifyFunc = fn
}

// SetEmailFunc sets the callback invoked when an email notification should be
// sent. The callback receives the subject and body; the implementation is
// responsible for resolving the recipient and SMTP config from the settings
// table. When no callback is set, email notifications are silently skipped.
func SetEmailFunc(fn func(subject, body string) error) {
	mu.Lock()
	defer mu.Unlock()
	emailFunc = fn
}

func sendEmail(subject, body string) {
	mu.RLock()
	fn := emailFunc
	mu.RUnlock()

	if fn == nil {
		return
	}
	if err := fn(subject, body); err != nil {
		log.Printf("[notify] email send failed: %v", err)
	}
}

func emit(n Notification) error {
	mu.RLock()
	fn := notifyFunc
	mu.RUnlock()

	if fn != nil {
		return fn(n)
	}

	log.Printf("[notify] no notification handler registered, skipping: %s", n.Body)
	return nil
}

// RequestCreated fires when a human request is opened, from either the REST
// handlers or an MCP tool call. It carries the request id so a click can
// deep-link straight to the thing that needs answering.
func RequestCreated(taskID, requestID int64, title string) error {
	return emit(Notification{
		Title:     "burnrate",
		Body:      fmt.Sprintf("BR%d %s — needs your input", taskID, title),
		TaskID:    taskID,
		RequestID: requestID,
	})
}

// HumanRequest fires when a run parks itself waiting on a human (the
// RESULT: WAITING_HUMAN trailer). There is no single request to point at —
// the agent may have opened several, or none — so only the task is linked.
func HumanRequest(taskID int64, title string) error {
	return emit(Notification{
		Title:  "burnrate",
		Body:   fmt.Sprintf("BR%d %s — needs your input", taskID, title),
		TaskID: taskID,
	})
}

// CaptureApproval is the notification for an agent asking to record the
// screen. Screen capture is not implemented yet (the MCP capture tools refuse
// outright), so nothing calls this today; it is kept wired for the REST
// approval path, which is the substrate the desktop capture work builds on.
func CaptureApproval(taskID, requestID int64, title, note string) error {
	body := fmt.Sprintf("BR%d %s — agent wants to capture screen", taskID, title)
	if note != "" {
		body += ": " + note
	}
	return emit(Notification{
		Title:     "burnrate",
		Body:      body,
		TaskID:    taskID,
		RequestID: requestID,
	})
}

// TaskFailed fires when a task is marked failed. It emits both an SSE
// notification (for the desktop app) and an email (if configured).
func TaskFailed(taskID int64, displayID, title, errorMsg string) error {
	body := fmt.Sprintf("%s %s — failed", displayID, title)
	if errorMsg != "" {
		summary := errorMsg
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		body += ": " + summary
	}
	err := emit(Notification{
		Title:  "burnrate",
		Body:   body,
		TaskID: taskID,
	})

	emailSubject := fmt.Sprintf("burnrate: %s %s failed", displayID, title)
	emailBody := fmt.Sprintf("Task %s (%s) has failed.\n\nError:\n%s", displayID, title, errorMsg)
	sendEmail(emailSubject, emailBody)

	return err
}

// Review fires when a run opens a PR. displayID is the human-facing task label
// ("BR42"), not the numeric id, and the numeric id is not threaded down to
// this call site — so the payload carries no task_id and the desktop app falls
// back to opening the app rather than a specific task.
func Review(displayID, title, prURL string) error {
	body := fmt.Sprintf("%s %s — ready for review", displayID, title)
	if prURL != "" {
		body += "\n" + prURL
	}
	return emit(Notification{Title: "burnrate", Body: body})
}
