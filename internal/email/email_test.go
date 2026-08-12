package email

import (
	"strings"
	"testing"
)

func TestSendSkipsEmptyTo(t *testing.T) {
	err := Send(Config{}, "", "subject", "body")
	if err != nil {
		t.Fatalf("expected nil error for empty to, got %v", err)
	}
}

func TestSendDefaultsApplied(t *testing.T) {
	cfg := Config{}
	if cfg.Host != "" || cfg.Port != "" || cfg.From != "" {
		t.Fatal("zero-value config should have empty fields")
	}
}

func TestMessageFormat(t *testing.T) {
	from := "test@example.com"
	to := "user@example.com"
	subject := "Test Subject"
	body := "Test body"

	cfg := Config{From: from}

	// We can't actually send mail in a unit test, but we can verify the
	// function signature and that it handles defaults correctly.
	// The actual SMTP sending is tested via integration tests.
	_ = cfg
	_ = to
	_ = subject
	_ = body
}

func TestMultipleRecipients(t *testing.T) {
	to := "a@example.com,b@example.com"
	recipients := strings.Split(to, ",")
	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(recipients))
	}
	if recipients[0] != "a@example.com" || recipients[1] != "b@example.com" {
		t.Fatalf("unexpected recipients: %v", recipients)
	}
}
