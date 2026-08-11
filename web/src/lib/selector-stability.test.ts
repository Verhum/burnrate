import { test } from "node:test";
import assert from "node:assert/strict";

/**
 * Regression test for the Completed-tab crash (#119).
 *
 * `useUsageStore((s) => s.status?.running_runs ?? [])` in TaskCard returned a
 * fresh `[]` on every store read when `status` was null. zustand 5 compares
 * snapshots with Object.is, so the snapshot never settled and React re-rendered
 * until it threw "Maximum update depth exceeded", blanking the page.
 *
 * The fix hoists the empty-array fallback to a module-level constant so the
 * selector returns the same reference every time `status` is null. These tests
 * verify the pattern directly — no DOM or React required.
 */

test("inline ?? [] produces unstable references (the bug)", () => {
  const selector = (status: { running_runs?: number[] } | null) =>
    status?.running_runs ?? [];
  const a = selector(null);
  const b = selector(null);
  assert.notEqual(
    a,
    b,
    "a fresh [] is a new object each time — this is why zustand re-renders forever"
  );
});

test("hoisted constant produces stable references (the fix)", () => {
  const NO_RUNS: number[] = [];
  const selector = (status: { running_runs?: number[] } | null) =>
    status?.running_runs ?? NO_RUNS;
  const a = selector(null);
  const b = selector(null);
  assert.equal(
    a,
    b,
    "same array reference every call — zustand sees no change"
  );
});

test("selector still returns actual data when status is populated", () => {
  const NO_RUNS: number[] = [];
  const realRuns = [1, 2, 3];
  const selector = (status: { running_runs?: number[] } | null) =>
    status?.running_runs ?? NO_RUNS;
  const result = selector({ running_runs: realRuns });
  assert.equal(result, realRuns, "real data passes through unchanged");
});
