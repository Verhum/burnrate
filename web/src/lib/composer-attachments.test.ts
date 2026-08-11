import assert from "node:assert/strict";
import { test } from "node:test";

import {
  MAX_UPLOAD_BYTES,
  attachmentDataPath,
  attachmentMarkdown,
  composeBodyWithAttachments,
  dragHasFiles,
  imageFilesFrom,
  isSupportedImage,
  resolveAttachmentSrc,
  uploadRejection,
} from "@/lib/composer-attachments";

function file(name: string, type: string, size = 1024): File {
  return { name, type, size } as unknown as File;
}

test("screenshot MIME types are accepted, SVG is not", () => {
  assert.equal(isSupportedImage(file("shot.png", "image/png")), true);
  assert.equal(isSupportedImage(file("photo.JPG", "image/jpeg")), true);
  // The server rejects SVG outright; accepting it here would only fail late.
  assert.equal(isSupportedImage(file("logo.svg", "image/svg+xml")), false);
  assert.equal(isSupportedImage(file("notes.txt", "text/plain")), false);
});

test("a typeless file falls back to its extension", () => {
  assert.equal(isSupportedImage(file("Screenshot 2026-08-02.png", "")), true);
  assert.equal(isSupportedImage(file("report", "")), false);
});

test("uploadRejection names the reason, or nothing", () => {
  assert.equal(uploadRejection(file("a.png", "image/png")), null);
  assert.equal(uploadRejection(file("a.txt", "text/plain")), "not a supported image");
  assert.equal(
    uploadRejection(file("big.png", "image/png", MAX_UPLOAD_BYTES + 1)),
    "too large (max 10 MB)"
  );
  assert.equal(uploadRejection(file("edge.png", "image/png", MAX_UPLOAD_BYTES)), null);
});

test("images come out of a clipboard payload, non-images do not", () => {
  const png = file("shot.png", "image/png");
  const txt = file("notes.txt", "text/plain");
  assert.deepEqual(imageFilesFrom({ files: [png, txt] }), [png]);
  assert.deepEqual(imageFilesFrom({ files: [txt] }), []);
  assert.deepEqual(imageFilesFrom(null), []);
});

test("the items list is the fallback when files is empty", () => {
  const png = file("shot.png", "image/png");
  const dt = {
    files: [],
    items: [
      { kind: "string", type: "text/plain", getAsFile: () => null },
      { kind: "file", type: "image/png", getAsFile: () => png },
      { kind: "file", type: "application/pdf", getAsFile: () => file("a.pdf", "application/pdf") },
    ],
  };
  assert.deepEqual(imageFilesFrom(dt), [png]);
});

test("an item that reports no file is skipped", () => {
  const dt = {
    items: [{ kind: "file", type: "image/png", getAsFile: () => null }],
  };
  assert.deepEqual(imageFilesFrom(dt), []);
});

// preventDefault on dragover has to be conditional: claiming a text drag would
// break dropping selected text into the textarea.
test("dragHasFiles only claims file drags", () => {
  assert.equal(dragHasFiles(["Files"]), true);
  assert.equal(dragHasFiles(["text/plain", "Files"]), true);
  assert.equal(dragHasFiles(["text/plain"]), false);
  assert.equal(dragHasFiles([]), false);
  assert.equal(dragHasFiles(undefined), false);
});

test("the data path is relative so the daemon serves it", () => {
  assert.equal(attachmentDataPath(42), "/api/attachments/42/data");
});

test("markdown alt text survives brackets and newlines", () => {
  assert.equal(
    attachmentMarkdown({ id: 7, filename: "shot.png" }),
    "![shot.png](/api/attachments/7/data)"
  );
  assert.equal(
    attachmentMarkdown({ id: 7, filename: "a [weird] name.png" }),
    "![a \\[weird\\] name.png](/api/attachments/7/data)"
  );
  assert.equal(
    attachmentMarkdown({ id: 7, filename: "two\nlines.png" }),
    "![two lines.png](/api/attachments/7/data)"
  );
  assert.equal(attachmentMarkdown({ id: 7, filename: "  " }), "![image](/api/attachments/7/data)");
});

test("composeBodyWithAttachments appends one image line per attachment", () => {
  assert.equal(composeBodyWithAttachments("  looks wrong  ", []), "looks wrong");
  assert.equal(
    composeBodyWithAttachments("looks wrong", [
      { id: 1, filename: "a.png" },
      { id: 2, filename: "b.png" },
    ]),
    "looks wrong\n\n![a.png](/api/attachments/1/data)\n\n![b.png](/api/attachments/2/data)"
  );
});

// An image with no prose is a complete answer — "here, look".
test("attachments alone are a valid body", () => {
  assert.equal(
    composeBodyWithAttachments("   ", [{ id: 3, filename: "c.png" }]),
    "![c.png](/api/attachments/3/data)"
  );
});

test("resolveAttachmentSrc prepends baseUrl for /api/ paths only", () => {
  const base = "http://127.0.0.1:9112";
  assert.equal(
    resolveAttachmentSrc("/api/attachments/42/data", base),
    "http://127.0.0.1:9112/api/attachments/42/data"
  );
  assert.equal(
    resolveAttachmentSrc("/api/attachments/42/data", ""),
    "/api/attachments/42/data"
  );
  assert.equal(
    resolveAttachmentSrc("https://example.com/img.png", base),
    "https://example.com/img.png"
  );
});
