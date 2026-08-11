import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { formatTaskCost, formatTaskDuration, formatLines } from "@/lib/task-stats";

describe("formatTaskCost", () => {
  it("formats zero", () => {
    assert.equal(formatTaskCost(0), "$0");
  });
  it("formats sub-cent", () => {
    assert.equal(formatTaskCost(0.001), "<$0.01");
  });
  it("formats normal cost", () => {
    assert.equal(formatTaskCost(12.4), "$12.40");
  });
  it("formats large cost", () => {
    assert.equal(formatTaskCost(100.5), "$100.50");
  });
});

describe("formatTaskDuration", () => {
  it("formats zero", () => {
    assert.equal(formatTaskDuration(0), "0s");
  });
  it("formats seconds only", () => {
    assert.equal(formatTaskDuration(45), "45s");
  });
  it("formats minutes", () => {
    assert.equal(formatTaskDuration(120), "2m");
  });
  it("formats minutes and seconds shows only minutes", () => {
    assert.equal(formatTaskDuration(125), "2m");
  });
  it("formats hours", () => {
    assert.equal(formatTaskDuration(3720), "1h 2m");
  });
});

describe("formatLines", () => {
  it("formats zero", () => {
    assert.equal(formatLines(0, 0), "0 lines");
  });
  it("formats added only", () => {
    assert.equal(formatLines(340, 0), "+340");
  });
  it("formats removed only", () => {
    assert.equal(formatLines(0, 82), "-82");
  });
  it("formats both", () => {
    assert.equal(formatLines(340, 82), "+340 -82");
  });
});
