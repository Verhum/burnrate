package server

import "testing"

func TestParseClaudeOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		fallback  string
		wantT     string
		wantP     string
		wantValid bool
	}{
		{
			name:      "standard format",
			output:    "VALID: yes\nTITLE: Add dark mode\nPROMPT: Implement dark mode toggle in the settings page",
			fallback:  "raw text",
			wantT:     "Add dark mode",
			wantP:     "Implement dark mode toggle in the settings page",
			wantValid: true,
		},
		{
			name:      "legacy format without VALID line",
			output:    "TITLE: Add dark mode\nPROMPT: Implement dark mode toggle in the settings page",
			fallback:  "raw text",
			wantT:     "Add dark mode",
			wantP:     "Implement dark mode toggle in the settings page",
			wantValid: true,
		},
		{
			name:      "empty output falls back",
			output:    "",
			fallback:  "I want to add dark mode",
			wantT:     "I want to add dark mode",
			wantP:     "",
			wantValid: true,
		},
		{
			name:      "multiline prompt",
			output:    "VALID: yes\nTITLE: Fix the login bug\nPROMPT: The login form breaks when\nthe user enters special characters.\nPlease fix the validation.",
			fallback:  "fix login",
			wantT:     "Fix the login bug",
			wantP:     "The login form breaks when\nthe user enters special characters.\nPlease fix the validation.",
			wantValid: true,
		},
		{
			name:      "extra whitespace",
			output:    "  VALID:  yes  \n  TITLE:   Refactor auth   \n  PROMPT:   Clean up the auth module  ",
			fallback:  "raw",
			wantT:     "Refactor auth",
			wantP:     "Clean up the auth module",
			wantValid: true,
		},
		{
			name:      "invalid transcription",
			output:    "VALID: no",
			fallback:  "um yeah so like whatever",
			wantT:     "",
			wantP:     "",
			wantValid: false,
		},
		{
			name:      "invalid with extra whitespace",
			output:    "  VALID:  no  ",
			fallback:  "gibberish",
			wantT:     "",
			wantP:     "",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotT, gotP, gotValid := parseClaudeOutput(tt.output, tt.fallback)
			if gotValid != tt.wantValid {
				t.Errorf("valid = %v, want %v", gotValid, tt.wantValid)
			}
			if gotT != tt.wantT {
				t.Errorf("title = %q, want %q", gotT, tt.wantT)
			}
			if gotP != tt.wantP {
				t.Errorf("prompt = %q, want %q", gotP, tt.wantP)
			}
		})
	}
}
