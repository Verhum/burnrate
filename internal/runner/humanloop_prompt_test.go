package runner

import (
	"strings"
	"testing"

	"github.com/Verhum/burnrate/internal/store"
)

// humanLoopPromptCases builds one prompt per worker template, mirroring the
// four modes buildPrompt selects between.
func humanLoopPromptCases() []struct {
	name   string
	prompt string
} {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	resume := &store.Run{ID: 10, SessionID: "sess-1"}

	return []struct {
		name   string
		prompt string
	}{
		{"new", buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"resume", buildPrompt(promptInput{task: task, resume: resume, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"followup", buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"agent", buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/workdir"})},
	}
}

// The tools are only callable under their fully qualified runtime names — the
// MCP server is registered as "burnrate-human-loop", so bare names like
// "ask_human" match nothing in the agent's tool list.
func TestBuildPromptHumanLoopNamesToolsFully(t *testing.T) {
	tools := []string{
		"mcp__burnrate-human-loop__ask_human",
		"mcp__burnrate-human-loop__request_demo",
		"mcp__burnrate-human-loop__await_request",
	}

	for _, tc := range humanLoopPromptCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, tool := range tools {
				if !strings.Contains(tc.prompt, tool) {
					t.Fatalf("prompt does not name the human-loop tool %s", tool)
				}
			}
		})
	}
}

// Parking is only reachable if the worker knows the exact trailer; the runner
// matches it with waitingHumanRe (^RESULT:[ \t]*WAITING_HUMAN\b).
func TestBuildPromptHumanLoopDocumentsWaitingTrailer(t *testing.T) {
	for _, tc := range humanLoopPromptCases() {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.prompt, "RESULT: WAITING_HUMAN") {
				t.Fatal("prompt does not document the RESULT: WAITING_HUMAN trailer")
			}
			if !waitingHumanRe.MatchString(tc.prompt) {
				t.Fatal("the trailer as written in the prompt does not match waitingHumanRe")
			}
		})
	}
}

// DenialPolicy is appended to every prompt, so assert on the built prompt
// rather than the template: the carve-out has to survive assembly.
func TestBuildPromptHumanLoopDenialCarveOutUsesFullNames(t *testing.T) {
	for _, tc := range humanLoopPromptCases() {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.prompt, "There Is No Human Here") {
				t.Fatal("prompt should carry the denial policy section")
			}
			if !strings.Contains(tc.prompt, "sanctioned* way to wait") {
				t.Fatal("denial policy is missing the human-loop carve-out")
			}
			for _, tool := range []string{
				"mcp__burnrate-human-loop__ask_human",
				"mcp__burnrate-human-loop__request_demo",
				"mcp__burnrate-human-loop__await_request",
			} {
				if !strings.Contains(tc.prompt, tool) {
					t.Fatalf("denial carve-out must name %s", tool)
				}
			}
		})
	}
}

// Desktop capture does not exist, and the MCP server hard-errors on those two
// tools. Naming them anywhere as usable sends the worker after a dead end.
func TestBuildPromptHumanLoopOmitsUnimplementedCapture(t *testing.T) {
	for _, tc := range humanLoopPromptCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, banned := range []string{"`capture_screen`", "`list_capture_targets`"} {
				if strings.Contains(tc.prompt, banned) {
					t.Fatalf("prompt still advertises unimplemented capture tool %s", banned)
				}
			}
		})
	}
}
