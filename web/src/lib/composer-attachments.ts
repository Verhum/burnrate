/**
 * Screenshot-into-a-composer plumbing, kept free of React and the DOM so it can
 * be unit tested. Shared by both text composers: the request reply form and the
 * task comment box.
 *
 * A pasted screenshot used to degrade to its filename as text. The fix is to
 * pull the image out of the clipboard/drop payload, upload it to the task's
 * existing attachment endpoint, and append a markdown image line to the body —
 * which both renders inline in the comment thread and rides the runner's
 * `## Image Attachments` path into the agent's next run.
 */

/** Mirrors `storableImageTypes` in internal/server/handlers_attachments.go. */
export const SUPPORTED_IMAGE_TYPES = [
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "image/bmp",
] as const;

/** Mirrors `maxUploadSize` in internal/server/handlers_attachments.go. */
export const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

const EXTENSION_TYPES: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  bmp: "image/bmp",
};

/** The minimum of `File` this module touches — keeps tests DOM-free. */
export interface FileLike {
  name: string;
  type: string;
  size: number;
}

export interface DataTransferItemLike {
  kind: string;
  type: string;
  getAsFile(): File | null;
}

export interface DataTransferLike {
  types?: readonly string[];
  files?: ArrayLike<File> | null;
  items?: ArrayLike<DataTransferItemLike> | null;
}

function isSupportedType(type: string): boolean {
  return (SUPPORTED_IMAGE_TYPES as readonly string[]).includes(
    type.toLowerCase().split(";")[0].trim()
  );
}

/**
 * True for the image kinds the server will actually store. SVG is deliberately
 * absent: the server rejects it (an SVG served back from the API origin is
 * script execution), so accepting it here would only produce a late failure.
 */
export function isSupportedImage(file: FileLike): boolean {
  if (file.type) return isSupportedType(file.type);
  // Some drops (and Finder pastes) hand over a typeless File; fall back to the
  // extension rather than dropping a perfectly good PNG.
  const ext = file.name.toLowerCase().split(".").pop() ?? "";
  return ext in EXTENSION_TYPES;
}

/**
 * Why an upload would be refused, or null if it is fine. Checked client-side so
 * an oversize paste fails instantly with the same wording the server uses.
 */
export function uploadRejection(file: FileLike): string | null {
  if (!isSupportedImage(file)) return "not a supported image";
  if (file.size > MAX_UPLOAD_BYTES) return "too large (max 10 MB)";
  return null;
}

/**
 * The image files inside a paste or drop payload.
 *
 * `files` is the reliable list in Chrome and Safari; `items` is the fallback for
 * payloads that expose the image only as an item (and it is what a synthetic
 * clipboard event in a test usually carries).
 */
export function imageFilesFrom(dt: DataTransferLike | null | undefined): File[] {
  if (!dt) return [];

  const fromFiles = dt.files ? Array.from(dt.files) : [];
  const picked = fromFiles.filter((f) => isSupportedImage(f));
  if (picked.length > 0) return picked;

  if (!dt.items) return [];
  const out: File[] = [];
  for (const item of Array.from(dt.items)) {
    if (item.kind !== "file") continue;
    if (item.type && !isSupportedType(item.type)) continue;
    const file = item.getAsFile();
    if (file && isSupportedImage(file)) out.push(file);
  }
  return out;
}

/**
 * Whether a dragover carries files. Guards `preventDefault()`: claim a file drag
 * (so the browser does not navigate to the image) but leave a text drag alone,
 * so dropping selected text into the textarea keeps working.
 */
export function dragHasFiles(types: readonly string[] | undefined | null): boolean {
  return !!types && Array.from(types).includes("Files");
}

export interface UploadedAttachmentRef {
  id: number;
  filename: string;
}

/**
 * Relative path stored in comment markdown. The rendering layer (comment-item's
 * custom img component) rewrites it to an absolute URL when the app runs inside
 * the Tauri desktop shell, where the webview origin is tauri://localhost.
 */
export function attachmentDataPath(id: number): string {
  return `/api/attachments/${id}/data`;
}

export function resolveAttachmentSrc(src: string, base: string): string {
  if (src.startsWith("/api/")) return `${base}${src}`;
  return src;
}

function escapeAltText(filename: string): string {
  const cleaned = filename.replace(/[\r\n]+/g, " ").trim();
  if (!cleaned) return "image";
  return cleaned.replace(/([[\]\\])/g, "\\$1");
}

export function attachmentMarkdown(att: UploadedAttachmentRef): string {
  return `![${escapeAltText(att.filename)}](${attachmentDataPath(att.id)})`;
}

/**
 * The body actually sent: the typed text, then one markdown image line per
 * uploaded attachment. Attachments alone (no prose) are a valid message.
 */
export function composeBodyWithAttachments(
  body: string,
  attachments: readonly UploadedAttachmentRef[]
): string {
  const text = body.trim();
  if (attachments.length === 0) return text;
  const images = attachments.map(attachmentMarkdown).join("\n\n");
  return text ? `${text}\n\n${images}` : images;
}
