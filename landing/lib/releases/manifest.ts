/**
 * The release pointer: one blob at `releases/latest.json` that says which build
 * the download button and the desktop updater should hand out.
 *
 * Two things about Vercel Blob make that pointer easy to get wrong, and both of
 * them have already bitten this site:
 *
 *  1. `put()` refuses to overwrite an existing pathname unless `allowOverwrite`
 *     is set. Without it every publish after the first one throws, so the
 *     pointer freezes on the first version ever uploaded while the release
 *     script cheerfully moves on.
 *  2. Blobs are served from a CDN with a 30-day `max-age` by default. Even once
 *     the pointer *is* rewritten, readers keep getting the old JSON until that
 *     expires — a stale pointer that looks identical to a failed publish.
 *
 * So writes go through `writeLatestManifest` (overwrite allowed, cache TTL as
 * low as the store permits) and reads go through `readLatestManifest`, which
 * varies the URL per upload. The cache-buster is not belt-and-braces: Blob
 * floors `cacheControlMaxAge: 0` at `max-age=60`, and blobs written before this
 * fix still carry the 30-day default, so a fresh cache key is the only thing
 * that actually guarantees a publish is visible immediately.
 */

import { head, put } from "@vercel/blob";

export const MANIFEST_PATH = "releases/latest.json";

/**
 * Whether a URL may be published as the release artifact.
 *
 * The manifest URL is what the macOS updater downloads and installs, and nothing
 * in the chain carries a checksum or a signature — so the host it names is the
 * entire integrity guarantee. Restricting it to the blob store releases are
 * actually published to means a leaked upload secret repoints the pointer within
 * your own store instead of at an attacker's .dmg, and it keeps the upload
 * route's reachability probe from fetching arbitrary hosts on request.
 *
 * Matched against the parsed hostname, never as a substring of the raw string:
 * `raw.includes(".public.blob.vercel-storage.com")` would accept
 * `https://evil.example.com/?x=.public.blob.vercel-storage.com`, and parsing also
 * rejects the `https://trusted@evil.com/` userinfo trick for free.
 */
export function isTrustedArtifactURL(raw: string): boolean {
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    return false;
  }
  if (u.protocol !== "https:") return false;
  return (
    u.hostname === "blob.vercel-storage.com" ||
    u.hostname.endsWith(".public.blob.vercel-storage.com")
  );
}

export type ReleaseManifest = {
  version: string;
  url: string;
  target: string;
  pub_date: string;
  notes: string;
  size: number;
};

/**
 * Reject anything that isn't a usable pointer.
 *
 * A manifest missing its `version` or `url` is worse than no manifest at all:
 * the download route would 307 to `undefined` and the updater would compare
 * against nothing. Better to treat it as "no release".
 */
export function parseManifest(raw: unknown): ReleaseManifest | null {
  if (typeof raw !== "object" || raw === null) return null;
  const m = raw as Record<string, unknown>;
  if (typeof m.version !== "string" || m.version === "") return null;
  if (typeof m.url !== "string" || m.url === "") return null;

  return {
    version: m.version,
    url: m.url,
    target: typeof m.target === "string" && m.target !== "" ? m.target : "darwin-aarch64",
    pub_date: typeof m.pub_date === "string" ? m.pub_date : "",
    notes: typeof m.notes === "string" ? m.notes : "",
    size: typeof m.size === "number" ? m.size : 0,
  };
}

/**
 * Append the blob's upload time to its URL.
 *
 * `head()` is a live API call, so it always reports the current upload — but the
 * URL it returns is stable across overwrites, which is exactly what lets a CDN
 * copy of the *previous* manifest keep answering. Varying the query string per
 * upload gives each generation its own cache key.
 */
export function cacheBustedUrl(url: string, uploadedAt: Date | string | number): string {
  const stamp =
    uploadedAt instanceof Date
      ? uploadedAt.getTime()
      : typeof uploadedAt === "number"
        ? uploadedAt
        : Date.parse(uploadedAt);
  const suffix = Number.isFinite(stamp) ? String(stamp) : "0";
  return url.includes("?") ? `${url}&v=${suffix}` : `${url}?v=${suffix}`;
}

/** Injectable so the read path is testable without a blob store or a network. */
export type ManifestDeps = {
  head: typeof head;
  fetch: typeof fetch;
};

const defaultDeps: ManifestDeps = { head, fetch: (...args) => fetch(...args) };

/**
 * Read the current pointer, or `null` when there is no usable release.
 *
 * Every failure — no blob, unreachable CDN, malformed JSON — collapses to
 * `null`, because the callers all have the same "nothing to serve" branch.
 */
export async function readLatestManifest(
  deps: ManifestDeps = defaultDeps,
): Promise<ReleaseManifest | null> {
  try {
    const meta = await deps.head(MANIFEST_PATH);
    const res = await deps.fetch(cacheBustedUrl(meta.url, meta.uploadedAt), {
      cache: "no-store",
    });
    if (!res.ok) return null;
    return parseManifest(await res.json());
  } catch {
    return null;
  }
}

/**
 * Publish a new pointer.
 *
 * `allowOverwrite` is the whole point — see the note at the top of this file.
 * `cacheControlMaxAge: 0` asks for no CDN caching and gets `max-age=60`, which
 * is why the read side cache-busts rather than trusting this.
 */
export async function writeLatestManifest(
  manifest: ReleaseManifest,
  deps: { put: typeof put } = { put },
): Promise<void> {
  await deps.put(MANIFEST_PATH, JSON.stringify(manifest), {
    access: "public",
    contentType: "application/json",
    addRandomSuffix: false,
    allowOverwrite: true,
    cacheControlMaxAge: 0,
  });
}
