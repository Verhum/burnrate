import assert from "node:assert/strict";
import { test } from "node:test";

import type { Task, TaskPR } from "@/lib/api/types";
import {
  prLabel,
  prNumber,
  prStateColor,
  prVisualState,
  taskPRs,
} from "@/lib/task-prs";

function pr(over: Partial<TaskPR> = {}): TaskPR {
  return {
    id: 1,
    task_id: 7,
    run_id: 3,
    repo: "acme/api",
    branch: "burnrate/7-x",
    pr_url: "https://github.com/acme/api/pull/311",
    worked_in: "/tmp/api",
    created_at: "2026-07-29T00:00:00Z",
    lines_added: 0,
    lines_removed: 0,
    pr_state: "",
    pr_is_draft: false,
    pr_checked_at: "",
    ...over,
  };
}

function task(over: Partial<Task> = {}): Task {
  return {
    id: 7,
    display_id: "",
    title: "t",
    prompt: "",
    repo_path: "",
    status: "pr_created",
    has_session: false,
    latest_run_status: "",
    latest_run_pr_url: "",
    attempt_reset_run_id: 0,
    sort_order: 0,
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:00Z",
    ...over,
  } as Task;
}

test("prNumber reads the number out of a GitHub PR url", () => {
  assert.equal(prNumber(pr()), 311);
  assert.equal(prNumber(pr({ pr_url: "https://example.com/x" })), 0);
});

test("a label always carries the PR number so two PRs are distinguishable", () => {
  assert.equal(prLabel(pr(), "PR"), "api #311");

  // Two PRs from the same repo is the case a repo-only label could not tell
  // apart — the whole reason the number is in the label.
  const a = prLabel(pr({ pr_url: "https://github.com/acme/api/pull/311" }), "PR");
  const b = prLabel(pr({ pr_url: "https://github.com/acme/api/pull/312" }), "PR");
  assert.notEqual(a, b);
});

test("label falls back to the number alone when the repo is unknown", () => {
  assert.equal(prLabel(pr({ repo: "" }), "View PR"), "#311");
  assert.equal(
    prLabel(pr({ repo: "", pr_url: "https://example.com/x" }), "View PR"),
    "View PR"
  );
});

test("visual state folds isDraft into OPEN only", () => {
  assert.equal(prVisualState(pr({ pr_state: "OPEN" })), "open");
  assert.equal(prVisualState(pr({ pr_state: "OPEN", pr_is_draft: true })), "draft");
  assert.equal(prVisualState(pr({ pr_state: "MERGED", pr_is_draft: true })), "merged");
  assert.equal(prVisualState(pr({ pr_state: "CLOSED" })), "closed");
  assert.equal(prVisualState(pr()), "unknown");
});

test("every state maps to a color token declared in globals.css", async () => {
  const { readFileSync } = await import("node:fs");
  const css = readFileSync(
    new URL("../app/globals.css", import.meta.url),
    "utf8"
  );
  for (const state of ["OPEN", "MERGED", "CLOSED", ""]) {
    for (const draft of [false, true]) {
      const cls = prStateColor(pr({ pr_state: state, pr_is_draft: draft }));
      const token = cls.replace(/^text-/, "");
      // The Tailwind palette is closed (`--color-*: initial`); an undeclared
      // token compiles to no CSS at all, with no error.
      assert.match(
        css,
        new RegExp(`--color-${token}:`),
        `${cls} has no --color-${token} in @theme`
      );
    }
  }
});

test("taskPRs falls back to the legacy single url with a probe-less state", () => {
  const prs = taskPRs(
    task({ latest_run_pr_url: "https://github.com/acme/api/pull/9" })
  );
  assert.equal(prs.length, 1);
  assert.equal(prVisualState(prs[0]), "unknown");
  assert.equal(prLabel(prs[0], "PR"), "#9");
});
