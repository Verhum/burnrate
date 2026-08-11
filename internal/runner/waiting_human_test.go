package runner

import "testing"

func TestIsWaitingHuman(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"exact trailer", "RESULT: WAITING_HUMAN", true},
		{"with tabs", "RESULT:\tWAITING_HUMAN", true},
		{"with extra text after", "RESULT: WAITING_HUMAN\nWORKED_IN: /tmp", true},
		{"embedded in output", "## Summary\nParked.\n\nRESULT: WAITING_HUMAN\nWORKED_IN: /work", true},
		{"normal success", "RESULT: Verhum/burnrate | burnrate/15 | https://github.com/Verhum/burnrate/pull/99 | /work/burnrate", false},
		{"empty", "", false},
		{"partial match", "RESULT: WAITING", false},
		{"not at line start", "  RESULT: WAITING_HUMAN", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWaitingHuman(tt.text)
			if got != tt.want {
				t.Errorf("isWaitingHuman(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestWriteMCPConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeMCPConfig(dir, 9112, 42, 7, nil)
	if path == "" {
		t.Fatal("expected non-empty config path")
	}
}
