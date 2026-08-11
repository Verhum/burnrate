import assert from "node:assert/strict";
import { afterEach, test } from "node:test";

import { copyToClipboard } from "@/lib/clipboard";

const g = globalThis as Record<string, unknown>;

afterEach(() => {
  delete g.navigator;
  delete g.document;
});

test("empty text is never copied", async () => {
  assert.equal(await copyToClipboard(""), false);
});

test("the modern path is used when available", async () => {
  const written: string[] = [];
  g.navigator = { clipboard: { writeText: async (t: string) => void written.push(t) } };

  assert.equal(await copyToClipboard("cd '/wt' && claude --resume 'sess-1'"), true);
  assert.deepEqual(written, ["cd '/wt' && claude --resume 'sess-1'"]);
});

// A WKWebView that exposes no navigator.clipboard, or rejects writeText, is the
// reason the execCommand path exists — a rejection must not surface as a throw.
test("a rejected writeText falls back to execCommand", async () => {
  g.navigator = {
    clipboard: {
      writeText: async () => {
        throw new Error("NotAllowedError");
      },
    },
  };
  const doc = fakeDocument();
  g.document = doc;

  assert.equal(await copyToClipboard("resume me"), true);
  assert.equal(doc.copied, "resume me");
  assert.equal(doc.body.children.length, 0, "the scratch textarea must be removed");
});

test("no clipboard API at all still copies via execCommand", async () => {
  g.navigator = {};
  const doc = fakeDocument();
  g.document = doc;

  assert.equal(await copyToClipboard("resume me"), true);
  assert.equal(doc.copied, "resume me");
});

test("both paths unavailable reports failure rather than throwing", async () => {
  g.navigator = {};
  const doc = fakeDocument();
  doc.execCommandResult = false;
  g.document = doc;

  assert.equal(await copyToClipboard("resume me"), false);
  assert.equal(doc.body.children.length, 0, "the scratch textarea must be removed");
});

interface FakeTextarea {
  value: string;
  style: Record<string, string>;
  setAttribute: (k: string, v: string) => void;
  select: () => void;
  setSelectionRange: (a: number, b: number) => void;
}

function fakeDocument() {
  const doc = {
    copied: null as string | null,
    execCommandResult: true,
    selected: null as FakeTextarea | null,
    body: {
      children: [] as FakeTextarea[],
      appendChild(el: FakeTextarea) {
        doc.body.children.push(el);
      },
      removeChild(el: FakeTextarea) {
        doc.body.children = doc.body.children.filter((c) => c !== el);
      },
    },
    createElement(): FakeTextarea {
      return {
        value: "",
        style: {},
        setAttribute: () => {},
        select() {
          doc.selected = this as unknown as FakeTextarea;
        },
        setSelectionRange: () => {},
      };
    },
    execCommand(cmd: string) {
      if (cmd === "copy" && doc.execCommandResult && doc.selected) {
        doc.copied = doc.selected.value;
        return true;
      }
      return false;
    },
  };
  return doc;
}
