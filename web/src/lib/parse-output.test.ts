import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseRunOutput } from "@/lib/parse-output";

describe("parseRunOutput", () => {
  it("parses canonical ## headings", () => {
    const text = [
      "## Summary",
      "Added per-task stats.",
      "",
      "## Changes",
      "- Backend: new store method",
      "- Frontend: stats on cards",
      "",
      "## Verification",
      "2 Go tests pass. LOE 3.",
      "",
      "## Documentation",
      "Updated ai.md.",
      "",
      "## Worktree Bootstrap",
      "No friction.",
      "",
      "RESULT: acme/api | b1 | https://github.com/acme/api/pull/1 | /work",
      "WORKED_IN: /work",
      "REPO: acme/api",
      "BRANCH: b1",
      "PR: https://github.com/acme/api/pull/1",
    ].join("\n");

    const out = parseRunOutput(text);
    assert.equal(out.summary, "Added per-task stats.");
    assert.equal(
      out.changes,
      "- Backend: new store method\n- Frontend: stats on cards"
    );
    assert.equal(out.verify, "2 Go tests pass. LOE 3.");
    assert.equal(out.docs, "Updated ai.md.");
    assert.equal(out.bootstrap, "No friction.");
    assert.equal(out.raw, text);
  });

  it("parses legacy Heading: format", () => {
    const text = [
      "Summary:",
      "Fixed a bug.",
      "",
      "What changed:",
      "Backend only.",
      "",
      "Tests:",
      "All pass.",
      "",
      "RESULT: acme/api | b1 | none | /work",
    ].join("\n");

    const out = parseRunOutput(text);
    assert.equal(out.summary, "Fixed a bug.");
    assert.equal(out.changes, "Backend only.");
    assert.equal(out.verify, "All pass.");
  });

  it("merges verify-aliased sections", () => {
    const text = [
      "## Tests",
      "3 tests pass.",
      "",
      "## Level of Effort",
      "LOE 3.",
      "",
      "RESULT: acme/api | b1 | none | /work",
    ].join("\n");

    const out = parseRunOutput(text);
    assert.equal(out.verify, "3 tests pass.\nLOE 3.");
  });

  it("returns empty sections for no-section text", () => {
    const out = parseRunOutput("Just some free-form output.");
    assert.equal(out.summary, "");
    assert.equal(out.changes, "");
    assert.equal(out.raw, "Just some free-form output.");
  });

  it("handles empty input", () => {
    const out = parseRunOutput("");
    assert.equal(out.summary, "");
    assert.equal(out.raw, "");
  });

  it("strips trailers from section bodies", () => {
    const text = [
      "## Summary",
      "Good work.",
      "",
      "RESULT: acme/api | b1 | https://github.com/acme/api/pull/1 | /work",
      "WORKED_IN: /work",
      "REPO: acme/api",
      "BRANCH: b1",
      "PR: https://github.com/acme/api/pull/1",
    ].join("\n");

    const out = parseRunOutput(text);
    assert.equal(out.summary, "Good work.");
  });

  it("handles alternate header names", () => {
    const text = [
      "## Summary",
      "Did the work.",
      "",
      "## Verify",
      "Tests pass.",
      "",
      "## Docs",
      "No changes.",
      "",
      "## Bootstrap",
      "No friction.",
      "",
      "RESULT: acme/api | b1 | none | /work",
    ].join("\n");

    const out = parseRunOutput(text);
    assert.equal(out.summary, "Did the work.");
    assert.equal(out.verify, "Tests pass.");
    assert.equal(out.docs, "No changes.");
    assert.equal(out.bootstrap, "No friction.");
  });

  it("handles summary-only output", () => {
    const text = [
      "## Summary",
      "Quick fix.",
      "",
      "RESULT: acme/web | b1 | https://github.com/acme/web/pull/3 | /work",
    ].join("\n");

    const out = parseRunOutput(text);
    assert.equal(out.summary, "Quick fix.");
    assert.equal(out.changes, "");
    assert.equal(out.verify, "");
  });
});
