import assert from "node:assert/strict";
import { test } from "node:test";

test("use-recorder module exports expected functions", async () => {
  const mod = await import("@/lib/use-recorder");
  assert.equal(typeof mod.useRecorder, "function");
});
