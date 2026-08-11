import assert from "node:assert/strict";
import test from "node:test";
import { resolveEvent, shouldRecord } from "./track";

const SAFARI =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15";

function headers(userAgent?: string): Headers {
  return new Headers(userAgent === undefined ? {} : { "user-agent": userAgent });
}

test("a browser fetching the .dmg is a download", () => {
  const event = resolveEvent(headers(SAFARI), {
    kind: "download",
    version: "0.1.4",
    target: "darwin-aarch64",
    outcome: "served",
  });
  assert.deepEqual(event, {
    name: "download",
    properties: { version: "0.1.4", target: "darwin-aarch64" },
  });
});

test("a crawler fetching the .dmg is not", () => {
  const event = resolveEvent(headers("Mozilla/5.0 (compatible; Googlebot/2.1)"), {
    kind: "download",
    version: "0.1.4",
    target: "darwin-aarch64",
    outcome: "served",
  });
  assert.equal(event.name, "download:bot");
});

test("the native updater is counted even though it looks scripted", () => {
  // Excluding scripted UAs here would zero out the installed-version signal.
  const event = resolveEvent(headers("Go-http-client/2.0"), {
    kind: "update_check",
    version: "0.1.3",
    target: "darwin-aarch64",
    outcome: "update_available",
  });
  assert.deepEqual(event, {
    name: "update_check:update_available",
    properties: { version: "0.1.3", target: "darwin-aarch64" },
  });
});

test("a UA-less .dmg fetch is automated, a UA-less update check is not", () => {
  assert.equal(
    resolveEvent(headers(), { kind: "download", outcome: "served" }).name,
    "download:bot",
  );
  assert.equal(
    resolveEvent(headers(), { kind: "update_check", version: "0.1.4", outcome: "up_to_date" }).name,
    "update_check:up_to_date",
  );
});

test("a HEAD probe is not a download", () => {
  // Next serves HEAD from the GET handler, so a client that probes before
  // fetching would otherwise be counted twice.
  const url = "https://www.burnthemtokens.com/api/releases/latest";
  assert.equal(shouldRecord(new Request(url, { method: "HEAD" })), false);
  assert.equal(shouldRecord(new Request(url)), true);
});

test("ANALYTICS_DISABLED stops recording", () => {
  const previous = process.env.ANALYTICS_DISABLED;
  try {
    process.env.ANALYTICS_DISABLED = "1";
    assert.equal(shouldRecord(new Request("https://www.burnthemtokens.com/")), false);
    process.env.ANALYTICS_DISABLED = "0";
    assert.equal(shouldRecord(new Request("https://www.burnthemtokens.com/")), true);
  } finally {
    if (previous === undefined) delete process.env.ANALYTICS_DISABLED;
    else process.env.ANALYTICS_DISABLED = previous;
  }
});

test("a caller-supplied version is normalized before it reaches the dashboard", () => {
  const event = resolveEvent(headers("Go-http-client/2.0"), {
    kind: "update_check",
    version: "not-a-version",
    target: "'; rm -rf /",
    outcome: "wrong_target",
  });
  assert.deepEqual(event.properties, { version: "unknown", target: "unknown" });
});
