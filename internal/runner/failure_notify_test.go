package runner

import (
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/notify"
	"github.com/Verhum/burnrate/internal/store"
)

func TestFireFailureNotification(t *testing.T) {
	ch := make(chan notify.Notification, 1)
	notify.SetNotifyFunc(func(n notify.Notification) error {
		ch <- n
		return nil
	})
	t.Cleanup(func() { notify.SetNotifyFunc(nil) })

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	logger := log.New("", false)
	FireFailureNotification(st, 42, "fix the bug", "connection refused", logger)

	select {
	case got := <-ch:
		if got.TaskID != 42 {
			t.Fatalf("expected task_id 42, got %d", got.TaskID)
		}
		if got.Title != "burnrate" {
			t.Fatalf("expected title 'burnrate', got %q", got.Title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestFireFailureNotificationGatedBySetting(t *testing.T) {
	ch := make(chan struct{}, 1)
	notify.SetNotifyFunc(func(n notify.Notification) error {
		ch <- struct{}{}
		return nil
	})
	t.Cleanup(func() { notify.SetNotifyFunc(nil) })

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	st.SetSetting("notify_on_failure", "false")

	logger := log.New("", false)
	FireFailureNotification(st, 1, "task", "err", logger)

	select {
	case <-ch:
		t.Fatal("expected notification to be suppressed when notify_on_failure=false")
	case <-time.After(100 * time.Millisecond):
		// expected: no notification
	}
}
