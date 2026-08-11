package runner

import "testing"

func TestParseRunOutput_CanonicalFormat(t *testing.T) {
	text := `## Summary
Added per-task stats to burnrate.

## Changes
- Backend: TaskStats domain type, store method
- Frontend: Stats fetched alongside tasks

## Verification
2 Go tests + 12 frontend unit tests, all passing.
Level of effort: 3 (Verify).

## Documentation
Updated ai.md route table. No CLAUDE.md changes warranted.

## Worktree Bootstrap
No friction.

RESULT: Verhum/burnrate | burnrate/104 | https://github.com/Verhum/burnrate/pull/5 | /work/burnrate
WORKED_IN: /work/burnrate
REPO: Verhum/burnrate
BRANCH: burnrate/104
PR: https://github.com/Verhum/burnrate/pull/5`

	out := ParseRunOutput(text)

	if out.Summary != "Added per-task stats to burnrate." {
		t.Fatalf("Summary: %q", out.Summary)
	}
	if out.Changes != "- Backend: TaskStats domain type, store method\n- Frontend: Stats fetched alongside tasks" {
		t.Fatalf("Changes: %q", out.Changes)
	}
	if out.Verify != "2 Go tests + 12 frontend unit tests, all passing.\nLevel of effort: 3 (Verify)." {
		t.Fatalf("Verify: %q", out.Verify)
	}
	if out.Docs != "Updated ai.md route table. No CLAUDE.md changes warranted." {
		t.Fatalf("Docs: %q", out.Docs)
	}
	if out.Bootstrap != "No friction." {
		t.Fatalf("Bootstrap: %q", out.Bootstrap)
	}
	if out.Raw != text {
		t.Fatal("Raw should be the full input text")
	}
}

func TestParseRunOutput_LegacyColonHeaders(t *testing.T) {
	text := `Summary:
Added per-task stats.

What changed:
Backend changes only.

Tests:
All passing.

Documentation:
No changes.

RESULT: acme/api | b1 | https://github.com/acme/api/pull/1 | /work`

	out := ParseRunOutput(text)

	if out.Summary != "Added per-task stats." {
		t.Fatalf("Summary: %q", out.Summary)
	}
	if out.Changes != "Backend changes only." {
		t.Fatalf("Changes: %q", out.Changes)
	}
	if out.Verify != "All passing." {
		t.Fatalf("Verify: %q", out.Verify)
	}
	if out.Docs != "No changes." {
		t.Fatalf("Docs: %q", out.Docs)
	}
}

func TestParseRunOutput_MixedHeaders(t *testing.T) {
	text := `## Summary
Did the thing.

What changed:
- Backend fix

## Verification
Tests pass.

Documentation:
Updated ai.md.

## Worktree Bootstrap
No friction.

WORKED_IN: /work
REPO: acme/api
BRANCH: b1
PR: none`

	out := ParseRunOutput(text)

	if out.Summary != "Did the thing." {
		t.Fatalf("Summary: %q", out.Summary)
	}
	if out.Changes != "- Backend fix" {
		t.Fatalf("Changes: %q", out.Changes)
	}
	if out.Verify != "Tests pass." {
		t.Fatalf("Verify: %q", out.Verify)
	}
	if out.Docs != "Updated ai.md." {
		t.Fatalf("Docs: %q", out.Docs)
	}
	if out.Bootstrap != "No friction." {
		t.Fatalf("Bootstrap: %q", out.Bootstrap)
	}
}

func TestParseRunOutput_NoSections(t *testing.T) {
	text := "Just some free-form output with no sections."
	out := ParseRunOutput(text)
	if out.Summary != "" || out.Changes != "" || out.Verify != "" {
		t.Fatalf("expected all empty, got %+v", out)
	}
	if out.Raw != text {
		t.Fatal("Raw should be the full input")
	}
}

func TestParseRunOutput_Empty(t *testing.T) {
	out := ParseRunOutput("")
	if out.Raw != "" || out.Summary != "" {
		t.Fatalf("expected empty output, got %+v", out)
	}
}

func TestParseRunOutput_TrailersStrippedFromSections(t *testing.T) {
	text := `## Summary
Good work done.

RESULT: acme/api | b1 | https://github.com/acme/api/pull/1 | /work
WORKED_IN: /work
REPO: acme/api
BRANCH: b1
PR: https://github.com/acme/api/pull/1`

	out := ParseRunOutput(text)
	if out.Summary != "Good work done." {
		t.Fatalf("Summary should not contain trailers: %q", out.Summary)
	}
}

func TestParseRunOutput_VerifySectionsMerge(t *testing.T) {
	text := `## Tests
3 tests pass.

## Level of Effort
LOE 3 (Verify).

RESULT: acme/api | b1 | none | /work`

	out := ParseRunOutput(text)
	if out.Verify != "3 tests pass.\nLOE 3 (Verify)." {
		t.Fatalf("Verify should merge Tests and LOE sections: %q", out.Verify)
	}
}

func TestParseRunOutput_SummaryOnlySection(t *testing.T) {
	text := `## Summary
Quick fix for the alignment issue.

RESULT: acme/web | b1 | https://github.com/acme/web/pull/3 | /work`

	out := ParseRunOutput(text)
	if out.Summary != "Quick fix for the alignment issue." {
		t.Fatalf("Summary: %q", out.Summary)
	}
	if out.Changes != "" || out.Verify != "" || out.Docs != "" || out.Bootstrap != "" {
		t.Fatalf("non-summary fields should be empty: %+v", out)
	}
}

func TestParseRunOutput_AlternateHeaderNames(t *testing.T) {
	text := `## Summary
Did the work.

## Verify
Tests pass.

## Docs
No changes.

## Bootstrap
No friction.

RESULT: acme/api | b1 | none | /work`

	out := ParseRunOutput(text)
	if out.Summary != "Did the work." {
		t.Fatalf("Summary: %q", out.Summary)
	}
	if out.Verify != "Tests pass." {
		t.Fatalf("Verify: %q", out.Verify)
	}
	if out.Docs != "No changes." {
		t.Fatalf("Docs: %q", out.Docs)
	}
	if out.Bootstrap != "No friction." {
		t.Fatalf("Bootstrap: %q", out.Bootstrap)
	}
}
