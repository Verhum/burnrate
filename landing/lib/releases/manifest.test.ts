import assert from "node:assert/strict";
import test from "node:test";
import {
  MANIFEST_PATH,
  cacheBustedUrl,
  isTrustedArtifactURL,
  parseManifest,
  readLatestManifest,
  writeLatestManifest,
  type ManifestDeps,
} from "./manifest";

const BLOB_URL = "https://example.public.blob.vercel-storage.com/releases/latest.json";

const LIVE = {
  version: "0.1.6",
  url: "https://example.public.blob.vercel-storage.com/releases/Burnrate_0.1.6_darwin-aarch64.dmg",
  target: "darwin-aarch64",
  pub_date: "2026-07-25T12:00:00.000Z",
  notes: "faster",
  size: 16324493,
};

/** A `head` + `fetch` pair over a fixed in-memory blob. */
function stubDeps(opts: {
  body?: unknown;
  status?: number;
  uploadedAt?: Date;
  headThrows?: boolean;
}): ManifestDeps & { requested: string[] } {
  const requested: string[] = [];
  return {
    requested,
    head: (async () => {
      if (opts.headThrows) throw new Error("blob not found");
      return { url: BLOB_URL, uploadedAt: opts.uploadedAt ?? new Date(1_700_000_000_000) };
    }) as unknown as ManifestDeps["head"],
    fetch: (async (input: string | URL | Request) => {
      requested.push(String(input));
      return new Response(JSON.stringify(opts.body ?? LIVE), {
        status: opts.status ?? 200,
        headers: { "content-type": "application/json" },
      });
    }) as unknown as ManifestDeps["fetch"],
  };
}

test("a well-formed manifest round-trips, with defaults filled in", () => {
  assert.deepEqual(parseManifest(LIVE), LIVE);

  assert.deepEqual(parseManifest({ version: "1.0.0", url: "https://x/y.dmg" }), {
    version: "1.0.0",
    url: "https://x/y.dmg",
    target: "darwin-aarch64",
    pub_date: "",
    notes: "",
    size: 0,
  });
});

test("a manifest without a version or url is no manifest at all", () => {
  // Serving these would 307 the download button to `undefined`.
  for (const bad of [
    null,
    "nope",
    {},
    { version: "0.1.6" },
    { url: "https://x/y.dmg" },
    { version: "", url: "https://x/y.dmg" },
    { version: "0.1.6", url: "" },
    { version: 6, url: "https://x/y.dmg" },
  ]) {
    assert.equal(parseManifest(bad), null, JSON.stringify(bad));
  }
});

test("the read URL carries the blob's upload time so a CDN copy can't answer", () => {
  // Blobs written before this fix still carry `max-age=2592000`, and the blob
  // URL is stable across overwrites — the stamp is what changes per publish.
  assert.equal(cacheBustedUrl("https://x/latest.json", new Date(1234)), "https://x/latest.json?v=1234");
  assert.equal(cacheBustedUrl("https://x/latest.json?a=b", new Date(1234)), "https://x/latest.json?a=b&v=1234");
  assert.equal(
    cacheBustedUrl("https://x/latest.json", "2026-07-25T12:00:00.000Z"),
    `https://x/latest.json?v=${Date.parse("2026-07-25T12:00:00.000Z")}`,
  );
  assert.equal(cacheBustedUrl("https://x/latest.json", "not a date"), "https://x/latest.json?v=0");
});

test("reading fetches the stamped URL with caching off", async () => {
  const deps = stubDeps({ uploadedAt: new Date(9999) });
  const manifest = await readLatestManifest(deps);

  assert.deepEqual(manifest, LIVE);
  assert.deepEqual(deps.requested, [`${BLOB_URL}?v=9999`]);
});

test("two publishes are never confused for one", async () => {
  const first = await readLatestManifest(stubDeps({ uploadedAt: new Date(1) }));
  const second = stubDeps({
    body: { ...LIVE, version: "0.1.7" },
    uploadedAt: new Date(2),
  });

  assert.equal(first?.version, "0.1.6");
  assert.equal((await readLatestManifest(second))?.version, "0.1.7");
  assert.deepEqual(second.requested, [`${BLOB_URL}?v=2`]);
});

test("every read failure collapses to no-release rather than a broken pointer", async () => {
  assert.equal(await readLatestManifest(stubDeps({ headThrows: true })), null);
  assert.equal(await readLatestManifest(stubDeps({ status: 404 })), null);
  assert.equal(await readLatestManifest(stubDeps({ body: { notes: "hi" } })), null);
});

test("publishing overwrites the pointer and opts out of the 30-day blob cache", async () => {
  // Without allowOverwrite, @vercel/blob throws on an existing pathname — which
  // is how the pointer froze on the first version ever published.
  const calls: Array<{ path: string; body: string; opts: Record<string, unknown> }> = [];
  const put = (async (path: string, body: string, opts: Record<string, unknown>) => {
    calls.push({ path, body, opts });
    return { url: BLOB_URL };
  }) as unknown as NonNullable<Parameters<typeof writeLatestManifest>[1]>["put"];

  await writeLatestManifest(LIVE, { put });

  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, MANIFEST_PATH);
  assert.deepEqual(JSON.parse(calls[0].body), LIVE);
  assert.equal(calls[0].opts.allowOverwrite, true);
  assert.equal(calls[0].opts.addRandomSuffix, false);
  assert.equal(calls[0].opts.cacheControlMaxAge, 0);
});

test("only the release blob store can be published as the artifact host", () => {
  // The updater installs whatever this URL names and there is no checksum or
  // signature anywhere in the chain, so the host is the whole integrity story.
  for (const url of [
    "https://example.public.blob.vercel-storage.com/releases/Burnrate_0.1.6_darwin-aarch64.dmg",
    "https://blob.vercel-storage.com/releases/Burnrate.dmg",
  ]) {
    assert.equal(isTrustedArtifactURL(url), true, url);
  }

  for (const url of [
    // A plain substring check would pass this one.
    "https://evil.example.com/?x=.public.blob.vercel-storage.com",
    // ...and a naive endsWith on the raw string would pass this one.
    "https://evilpublic.blob.vercel-storage.com.evil.com/x.dmg",
    // Parsing is what rejects userinfo posing as the host.
    "https://example.public.blob.vercel-storage.com@evil.com/x.dmg",
    "http://example.public.blob.vercel-storage.com/x.dmg",
    "http://169.254.169.254/latest/meta-data/",
    "file:///etc/passwd",
    "not-a-url",
    "",
  ]) {
    assert.equal(isTrustedArtifactURL(url), false, url);
  }
});
