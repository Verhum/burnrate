import assert from "node:assert/strict";
import test from "node:test";
import { UNKNOWN, eventName, eventProperties, normalizeTarget, normalizeVersion } from "./events";

test("a real download is the bare event name", () => {
  assert.equal(eventName("download", "served", false), "download");
});

test("every non-download outcome is a suffixed sibling", () => {
  assert.equal(eventName("download", "no_release", false), "download:no_release");
  assert.equal(eventName("update_check", "up_to_date", false), "update_check:up_to_date");
  assert.equal(eventName("update_check", "update_available", false), "update_check:update_available");
  assert.equal(eventName("update_check", "wrong_target", false), "update_check:wrong_target");
  assert.equal(eventName("update_check", "no_release", false), "update_check:no_release");
});

test("automated traffic is filed separately on every endpoint", () => {
  assert.equal(eventName("download", "served", true), "download:bot");
  assert.equal(eventName("download", "no_release", true), "download:bot");
  assert.equal(eventName("update_check", "up_to_date", true), "update_check:bot");
});

test("event names stay inside Vercel's 255-character limit", () => {
  for (const kind of ["download", "update_check"] as const) {
    for (const outcome of ["served", "no_release", "up_to_date", "update_available", "wrong_target"] as const) {
      for (const bot of [true, false]) {
        assert.ok(eventName(kind, outcome, bot).length <= 255);
      }
    }
  }
});

test("plausible versions survive, with the v prefix stripped", () => {
  assert.equal(normalizeVersion("0.1.4"), "0.1.4");
  assert.equal(normalizeVersion("v0.1.4"), "0.1.4");
  assert.equal(normalizeVersion("1.2.3-beta.1"), "1.2.3-beta.1");
  assert.equal(normalizeVersion("1.2.3+build7"), "1.2.3+build7");
});

test("implausible versions collapse to one bucket", () => {
  // `current_version` is a caller-supplied query param, so unbounded values would
  // mint a new dashboard row per request.
  const junk = [
    null,
    undefined,
    "",
    "latest",
    "1.2",
    "1.2.3.4",
    "0.1.4; DROP TABLE",
    "9".repeat(300),
    "1.2.3-" + "x".repeat(100),
  ];
  for (const value of junk) {
    assert.equal(normalizeVersion(value), UNKNOWN, String(value).slice(0, 20));
  }
});

test("targets are limited to triple-shaped values", () => {
  assert.equal(normalizeTarget("darwin-aarch64"), "darwin-aarch64");
  assert.equal(normalizeTarget("darwin-x86_64"), "darwin-x86_64");
  assert.equal(normalizeTarget("linux-x86_64"), "linux-x86_64");
  for (const junk of [null, "", "darwin", "Darwin-AArch64", "../../etc", "a-" + "b".repeat(40)]) {
    assert.equal(normalizeTarget(junk), UNKNOWN, String(junk));
  }
});

test("exactly two properties are sent — the Pro plan limit", () => {
  const props = eventProperties({ kind: "download", version: "0.1.4", target: "darwin-aarch64", outcome: "served" });
  assert.deepEqual(Object.keys(props).sort(), ["target", "version"]);
  assert.deepEqual(props, { version: "0.1.4", target: "darwin-aarch64" });
});

test("a missing version and target still produce both properties", () => {
  assert.deepEqual(eventProperties({ kind: "download", outcome: "no_release" }), {
    version: UNKNOWN,
    target: UNKNOWN,
  });
});

test("property values are strings — nested objects are rejected by Vercel", () => {
  const props = eventProperties({ kind: "update_check", version: "v1.0.0", target: "darwin-aarch64", outcome: "up_to_date" });
  for (const value of Object.values(props)) {
    assert.equal(typeof value, "string");
    assert.ok(value.length <= 255);
  }
});
