package runner

import (
	"strings"
	"testing"

	"github.com/Verhum/burnrate/internal/store"
)

func TestParseEffortLevel(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		want     int
		wantFind bool
	}{
		{"loe colon digit", "Fix the parser. LOE: 4", 4, true},
		{"loe bare digit", "loe 1 — just tell me what's wrong", 1, true},
		{"lowercase spelled out", "level of effort: 2", 2, true},
		{"effort level phrasing", "Effort level = 4", 4, true},
		{"is separator", "The level of effort is 1 here.", 1, true},
		{"redundant level word", "LOE: level 4", 4, true},
		{"named level", "LOE: investigate", EffortInvestigate, true},
		{"named level with trailing prose", "loe: verify the code with unit tests", EffortVerify, true},
		{"named integration", "effort level: integration", EffortValidate, true},
		{"last directive wins", "LOE: 2. On reflection, LOE: 4.", 4, true},
		{"mid sentence", "Please handle this one at loe 4 since it touches billing.", 4, true},

		{"no directive", "Refactor the scheduler and add tests.", 0, false},
		{"bare level number is prose", "This is the level 4 cache, not an effort directive.", 0, false},
		{"keyword without a value", "Level of effort is how thoroughly the agent works.", 0, false},
		{"out of range digit", "LOE: 7", 0, false},
		{"multi digit", "LOE: 42", 0, false},
		{"unknown word", "LOE: whatever", 0, false},
		{"value on the next line", "level of effort:\n4", 0, false},
		{"substring of another word", "The employee had a low sloevel 4 score", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := parseEffortLevel(tc.text)
			if found != tc.wantFind {
				t.Fatalf("found = %v, want %v (text %q)", found, tc.wantFind, tc.text)
			}
			if found && got != tc.want {
				t.Fatalf("level = %d, want %d (text %q)", got, tc.want, tc.text)
			}
		})
	}
}

// The task description that specified this feature enumerates the four levels
// in prose. Reading it must not pin a run to level 1.
func TestParseEffortLevelIgnoresProseAboutTheScale(t *testing.T) {
	text := `Each time the user creates a task the Agent automatically chooses the level of
effort necessary to complete the task. This is not to be confused with model effort.
Level of effort is how thoroughly the agent should complete the problem. there are 4 levels
1) Investigate
2) Write the Code
3) Verify the Code
4) Wholeheartedly attempt to validate the solution.`

	if lvl, found := parseEffortLevel(text); found {
		t.Fatalf("prose about the scale parsed as an explicit directive (level %d)", lvl)
	}
}

func TestResolveEffortLevel(t *testing.T) {
	comment := func(body string) store.Comment { return store.Comment{Body: body} }

	cases := []struct {
		name         string
		taskPrompt   string
		comments     []store.Comment
		want         int
		wantExplicit bool
	}{
		{"nothing anywhere defaults to 3", "ship the feature", nil, DefaultEffortLevel, false},
		{"description directive", "ship the feature. LOE: 4", nil, 4, true},
		{"comment overrides description", "ship it. LOE: 2", []store.Comment{comment("actually LOE: 4")}, 4, true},
		{"newest comment wins", "ship it", []store.Comment{comment("LOE: 1"), comment("LOE: 4")}, 4, true},
		{"comment without directive falls back", "ship it. LOE: 1", []store.Comment{comment("also rename the flag")}, 1, true},
		{"no directive with comments", "ship it", []store.Comment{comment("also rename the flag")}, DefaultEffortLevel, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, explicit := resolveEffortLevel(tc.taskPrompt, tc.comments)
			if got != tc.want || explicit != tc.wantExplicit {
				t.Fatalf("resolveEffortLevel = (%d, %v), want (%d, %v)", got, explicit, tc.want, tc.wantExplicit)
			}
		})
	}
}

func TestBuildPromptIncludesEffortLevels(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "do stuff"}
	resume := &store.Run{ID: 10, SessionID: "sess-1"}

	cases := []struct {
		name   string
		prompt string
	}{
		{"new", buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"resume", buildPrompt(promptInput{task: task, resume: resume, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"followup", buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})},
		{"agent", buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/workdir"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.prompt, "## Level of Effort") {
				t.Fatal("prompt missing Level of Effort section")
			}
			if !strings.Contains(tc.prompt, "**Level for this run: 3 — Verify (default).**") {
				t.Fatal("prompt does not pin the run to the default level 3")
			}
		})
	}
}

// Level 4 is opt-in. Every worker prompt must say so, and the default section
// must not offer risk, money, auth or a boundary crossing as a reason to
// self-promote — that wording is what this test exists to keep out.
func TestEffortSectionForbidsSelfRaisingToFour(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "wire up billing across the service boundary"}

	prompts := map[string]string{
		"new":      buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"}),
		"resume":   buildPrompt(promptInput{task: task, resume: &store.Run{ID: 10, SessionID: "sess-1"}, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"}),
		"followup": buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"}),
		"agent":    buildPrompt(promptInput{task: task, agentMode: true, worktreePath: "/workdir"}),
	}

	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(prompt, "NEVER work at level 4 unless the user explicitly asked for it") {
				t.Fatal("prompt does not forbid working at level 4 unasked")
			}
			if !strings.Contains(prompt, "**Never raise yourself to 4**") {
				t.Fatal("default level section does not forbid self-promotion to 4")
			}
			for _, banned := range []string{"deserves a **4** even when nobody asked", "raise it to 4", "raise to 4"} {
				if strings.Contains(prompt, banned) {
					t.Fatalf("prompt still invites self-promotion to level 4: %q", banned)
				}
			}
		})
	}
}

func TestBuildPromptUsesExplicitEffortLevel(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "wire up billing. LOE: 4"}

	prompt := buildPrompt(promptInput{task: task, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo"})

	if !strings.Contains(prompt, "**Level for this run: 4 — Validate end to end.**") {
		t.Fatal("prompt did not honor the explicit level in the task description")
	}
	if strings.Contains(prompt, "(default)") {
		t.Fatal("prompt still describes the level as the default")
	}
}

func TestBuildPromptEffortLevelFromComment(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "look into the flaky test"}
	comments := []store.Comment{{ID: 1, Body: "just diagnose it for now — LOE: 1", CreatedAt: "2025-01-01T00:00:00Z"}}

	prompt := buildPrompt(promptInput{task: task, isFollowup: true, defaultBranch: "main", worktreePath: "/wt", branch: "branch", repoPath: "/repo", comments: comments})

	if !strings.Contains(prompt, "**Level for this run: 1 — Investigate.**") {
		t.Fatal("prompt did not honor the explicit level from a follow-up comment")
	}
}

func TestBuildContinuePromptCarriesEffortLevel(t *testing.T) {
	task := store.Task{ID: 1, Title: "test task", Prompt: "migrate the schema. LOE: 4"}

	prompt := buildContinuePrompt(task, false, "main", "/wt", "branch", "/repo", "denied")

	if !strings.Contains(prompt, "- LEVEL OF EFFORT: 4 — Validate end to end (requested by the task)") {
		t.Fatal("continue prompt missing the resolved level of effort")
	}

	plain := buildContinuePrompt(store.Task{ID: 2, Title: "t", Prompt: "do stuff"}, true, "", "/workdir", "", "", "")
	if !strings.Contains(plain, "- LEVEL OF EFFORT: 3 — Verify (default)") {
		t.Fatal("continue prompt missing the default level of effort")
	}
}
