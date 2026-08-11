import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  formatStreakDay,
  formatStreakRange,
  formatCompact,
  formatStreakSpend,
} from "@/lib/streak";

describe("formatStreakDay", () => {
  it("formats a day key without timezone drift", () => {
    assert.equal(formatStreakDay("2026-01-03"), "Jan 3");
    assert.equal(formatStreakDay("2026-12-31"), "Dec 31");
  });

  it("passes through malformed input", () => {
    assert.equal(formatStreakDay(""), "");
    assert.equal(formatStreakDay("not-a-date"), "not-a-date");
    assert.equal(formatStreakDay("2026-13-01"), "2026-13-01");
  });
});

describe("formatStreakRange", () => {
  it("formats a range", () => {
    assert.equal(formatStreakRange("2026-01-03", "2026-01-24"), "Jan 3–Jan 24");
  });

  it("collapses a single-day range", () => {
    assert.equal(formatStreakRange("2026-01-03", "2026-01-03"), "Jan 3");
  });

  it("is empty when either end is missing", () => {
    assert.equal(formatStreakRange(undefined, "2026-01-03"), "");
    assert.equal(formatStreakRange("2026-01-03", undefined), "");
  });
});

describe("formatCompact", () => {
  it("keeps small numbers whole", () => {
    assert.equal(formatCompact(0), "0");
    assert.equal(formatCompact(950), "950");
  });

  it("abbreviates thousands with one decimal", () => {
    assert.equal(formatCompact(12345), "12.3k");
    assert.equal(formatCompact(1000), "1k");
  });

  it("drops the decimal past 100k", () => {
    assert.equal(formatCompact(234567), "235k");
  });

  it("handles negatives", () => {
    assert.equal(formatCompact(-12345), "-12.3k");
  });
});

describe("formatStreakSpend", () => {
  it("shows cents under $100", () => {
    assert.equal(formatStreakSpend(0), "$0.00");
    assert.equal(formatStreakSpend(42.5), "$42.50");
  });

  it("rounds with commas over $100", () => {
    assert.equal(formatStreakSpend(1234.56), "$1,235");
  });
});
