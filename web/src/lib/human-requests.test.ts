import assert from "node:assert/strict";
import { test } from "node:test";

import type { HumanRequest } from "@/lib/api/types";
import {
  SUMMARY_MAX_CHARS,
  parseDemoBody,
  requestSummary,
  requestsForTask,
  sortRequests,
  stripMarkdown,
  summarizeMarkdown,
} from "@/lib/human-requests";

function req(over: Partial<HumanRequest> = {}): HumanRequest {
  return {
    id: 1,
    task_id: 7,
    run_id: 3,
    kind: "question",
    title: "",
    body: "",
    status: "pending",
    live: false,
    created_at: "2026-08-01T00:00:00Z",
    answered_at: "",
    ...over,
  };
}

test("parseDemoBody reads a full brief", () => {
  const brief = parseDemoBody(
    JSON.stringify({
      steps: ["open the app", "click Save"],
      expected: "a toast appears",
      look_for: "no console errors",
      url: "http://localhost:3000/v/ABC",
      revival_steps: { cwd: "/tmp/x", command: "npm run dev", port: 3000 },
    })
  );
  assert.ok(brief);
  assert.deepEqual(brief.steps, ["open the app", "click Save"]);
  assert.equal(brief.expected, "a toast appears");
  assert.equal(brief.look_for, "no console errors");
  assert.equal(brief.url, "http://localhost:3000/v/ABC");
  assert.deepEqual(brief.revival_steps, {
    cwd: "/tmp/x",
    command: "npm run dev",
    port: 3000,
  });
});

test("parseDemoBody returns null for prose, so the card can render it raw", () => {
  assert.equal(parseDemoBody("please check the modal alignment"), null);
  assert.equal(parseDemoBody(""), null);
  assert.equal(parseDemoBody("   "), null);
});

test("parseDemoBody returns null for JSON that is not an object of known fields", () => {
  assert.equal(parseDemoBody("[1,2,3]"), null);
  assert.equal(parseDemoBody('"just a string"'), null);
  assert.equal(parseDemoBody("42"), null);
  assert.equal(parseDemoBody("null"), null);
  assert.equal(parseDemoBody("{}"), null);
  assert.equal(parseDemoBody('{"unrelated": true}'), null);
});

test("parseDemoBody tolerates a truncated or malformed document", () => {
  assert.equal(parseDemoBody('{"steps": ["a", '), null);
});

test("parseDemoBody drops non-string steps and empty strings", () => {
  const brief = parseDemoBody('{"steps": ["a", 3, null, "  ", "b"]}');
  assert.ok(brief);
  assert.deepEqual(brief.steps, ["a", "b"]);
});

test("parseDemoBody survives a brief with only a url", () => {
  const brief = parseDemoBody('{"url": "http://localhost:9112"}');
  assert.ok(brief);
  assert.deepEqual(brief.steps, []);
  assert.equal(brief.url, "http://localhost:9112");
});

test("parseDemoBody accepts a stringified port and ignores an empty revival block", () => {
  const withPort = parseDemoBody('{"steps":["a"],"revival_steps":{"port":"8080"}}');
  assert.equal(withPort?.revival_steps?.port, 8080);

  const emptyRevival = parseDemoBody('{"steps":["a"],"revival_steps":{}}');
  assert.equal(emptyRevival?.revival_steps, undefined);

  const badRevival = parseDemoBody('{"steps":["a"],"revival_steps":"nope"}');
  assert.equal(badRevival?.revival_steps, undefined);
});

test("sortRequests puts live requests first, then oldest-first", () => {
  const sorted = sortRequests([
    req({ id: 1, created_at: "2026-08-01T10:00:00Z" }),
    req({ id: 2, created_at: "2026-08-01T09:00:00Z" }),
    req({ id: 3, created_at: "2026-08-01T11:00:00Z", live: true }),
  ]);
  assert.deepEqual(
    sorted.map((r) => r.id),
    [3, 2, 1]
  );
});

test("sortRequests does not mutate its input", () => {
  const input = [req({ id: 1 }), req({ id: 2, live: true })];
  sortRequests(input);
  assert.deepEqual(
    input.map((r) => r.id),
    [1, 2]
  );
});

test("requestsForTask filters by task and keeps queue order", () => {
  const all = [
    req({ id: 1, task_id: 7, created_at: "2026-08-01T10:00:00Z" }),
    req({ id: 2, task_id: 8, created_at: "2026-08-01T09:00:00Z" }),
    req({ id: 3, task_id: 7, created_at: "2026-08-01T09:30:00Z" }),
  ];
  assert.deepEqual(
    requestsForTask(all, 7).map((r) => r.id),
    [3, 1]
  );
  assert.deepEqual(requestsForTask(all, 99), []);
});

test("requestSummary falls back to a readable label per kind", () => {
  assert.equal(requestSummary(req({ title: "check the modal" })), "check the modal");
  assert.equal(requestSummary(req({ title: "  " })), "Question");
  assert.equal(requestSummary(req({ kind: "demo", title: "" })), "Demo request");
  assert.equal(
    requestSummary(req({ kind: "capture_approval", title: "" })),
    "Screen capture approval"
  );
});

test("stripMarkdown removes emphasis, code, links and block markers", () => {
  assert.equal(stripMarkdown("**Which one?**"), "Which one?");
  assert.equal(stripMarkdown("__loud__ and _quiet_"), "loud and quiet");
  assert.equal(stripMarkdown("~~dropped~~ kept"), "dropped kept");
  assert.equal(stripMarkdown("run `npm test` first"), "run npm test first");
  assert.equal(stripMarkdown("see [the docs](http://x/y)"), "see the docs");
  assert.equal(stripMarkdown("![a diagram](img.png)"), "a diagram");
  assert.equal(stripMarkdown("## Heading"), "Heading");
  assert.equal(stripMarkdown("- bullet"), "bullet");
  assert.equal(stripMarkdown("3. numbered"), "numbered");
  assert.equal(stripMarkdown("> - **quoted bullet**"), "quoted bullet");
});

test("stripMarkdown drops emphasis markers it cannot pair off", () => {
  assert.equal(stripMarkdown("**Question: what now"), "Question: what now");
});

test("stripMarkdown collapses whitespace", () => {
  assert.equal(stripMarkdown("  spaced\tout   words  "), "spaced out words");
});

test("summarizeMarkdown takes the first line with prose in it", () => {
  assert.equal(summarizeMarkdown("\n\n**Q:** which?\n\nmore detail"), "Q: which?");
  assert.equal(summarizeMarkdown("```\ncode\n```"), "code");
  assert.equal(summarizeMarkdown("---\n# Title\nbody"), "Title");
  assert.equal(summarizeMarkdown(""), "");
  assert.equal(summarizeMarkdown("   \n\n  "), "");
});

test("summarizeMarkdown truncates on a word boundary with an ellipsis", () => {
  const long = "word ".repeat(60).trim();
  const out = summarizeMarkdown(long);
  assert.ok(out.length <= SUMMARY_MAX_CHARS + 1, `too long: ${out.length}`);
  assert.ok(out.endsWith("…"));
  assert.ok(!out.includes("wor…"));

  // No usable space late in the window: cut mid-token rather than at char 4.
  const runOn = `xy ${"z".repeat(200)}`;
  const cut = summarizeMarkdown(runOn);
  assert.equal(cut.length, SUMMARY_MAX_CHARS + 1);
});

test("summarizeMarkdown leaves a short line alone", () => {
  assert.equal(summarizeMarkdown("short enough"), "short enough");
});

test("requestSummary compacts a title that is really the whole question", () => {
  const question = [
    "**Which retry policy do you want?**",
    "",
    "- exponential backoff",
    "- fixed 5s",
  ].join("\n");
  // ask_human sets title = the full markdown question, verbatim.
  const summary = requestSummary(req({ title: question, body: question }));
  assert.equal(summary, "Which retry policy do you want?");
  assert.ok(!summary.includes("*"));
});

test("requestSummary falls back to the body when the title is blank", () => {
  assert.equal(
    requestSummary(req({ title: "", body: "## Is the banner readable?\nmore" })),
    "Is the banner readable?"
  );
});

test("requestSummary summarizes a demo brief rather than its JSON", () => {
  const body = JSON.stringify({
    steps: ["open **the app**", "click Save"],
    expected: "a toast appears",
  });
  const summary = requestSummary(req({ kind: "demo", title: "", body }));
  assert.equal(summary, "open the app");
  assert.ok(!summary.includes("{"));

  const noSteps = JSON.stringify({ expected: "a toast appears" });
  assert.equal(
    requestSummary(req({ kind: "demo", title: "", body: noSteps })),
    "a toast appears"
  );

  // A brief whose only field is a url still beats raw JSON.
  const urlOnly = JSON.stringify({ url: "http://localhost:9112" });
  assert.equal(
    requestSummary(req({ kind: "demo", title: "", body: urlOnly })),
    "http://localhost:9112"
  );
});

test("requestSummary reads a non-JSON demo body as prose", () => {
  assert.equal(
    requestSummary(req({ kind: "demo", title: "", body: "*check* the modal" })),
    "check the modal"
  );
});
