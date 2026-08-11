import assert from "node:assert/strict";
import { test } from "node:test";

import { shouldOpenInShell } from "@/lib/external-link";

test("mailto links leave the webview", () => {
  assert.equal(shouldOpenInShell("mailto:support@ver-hum.com"), true);
  assert.equal(
    shouldOpenInShell("mailto:support@ver-hum.com?subject=Burnrate%20support"),
    true
  );
});

test("remote http(s) links leave the webview", () => {
  assert.equal(shouldOpenInShell("https://github.com/Verhum/burnrate/pull/1"), true);
  assert.equal(shouldOpenInShell("http://example.com/"), true);
});

test("the app's own API stays in the webview", () => {
  assert.equal(shouldOpenInShell("http://127.0.0.1:9112/api/tasks"), false);
  assert.equal(shouldOpenInShell("http://localhost:9112/api/tasks"), false);
});

test("in-app and unparseable hrefs stay in the webview", () => {
  assert.equal(shouldOpenInShell("/tasks"), false);
  assert.equal(shouldOpenInShell("#top"), false);
  assert.equal(shouldOpenInShell("https://"), false);
  assert.equal(shouldOpenInShell(""), false);
  assert.equal(shouldOpenInShell(null), false);
  assert.equal(shouldOpenInShell(undefined), false);
});
