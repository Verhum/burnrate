import { NextRequest, NextResponse } from "next/server";
import {
  isTrustedArtifactURL,
  writeLatestManifest,
  type ReleaseManifest,
} from "@/lib/releases/manifest";

/**
 * Register a freshly uploaded build as the latest release.
 *
 * The .dmg is pushed straight to Vercel Blob by `scripts/upload-release.sh`;
 * this endpoint only moves the pointer at `releases/latest.json`. Two guards
 * matter here, because the cost of getting either wrong is a download button
 * that 404s:
 *
 *  - the artifact is HEAD-checked before the pointer moves, so a URL that was
 *    never uploaded (or has since been deleted) is rejected rather than
 *    published;
 *  - a failed blob write answers 502 instead of an unhandled 500, so the
 *    release script can tell "publish failed" apart from "bad request" and say
 *    so.
 */
export async function POST(req: NextRequest) {
  // Fail closed when the secret is unconfigured. Interpolating a missing env var
  // into the template yields the literal "Bearer undefined", which any caller can
  // send — so the previous form authenticated everyone on a deployment that had
  // simply forgotten to set UPLOAD_SECRET, which is the default state of a fork.
  const expected = process.env.UPLOAD_SECRET;
  if (!expected) {
    console.error("[releases] UPLOAD_SECRET is not set — refusing all uploads");
    return NextResponse.json(
      { error: "release uploads are not configured" },
      { status: 503 },
    );
  }
  const auth = req.headers.get("authorization");
  if (auth !== `Bearer ${expected}`) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  let body: unknown;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "body must be JSON" }, { status: 400 });
  }

  const { version, url, target, notes, size } = (body ?? {}) as Record<string, unknown>;

  if (typeof version !== "string" || !version || typeof url !== "string" || !url) {
    return NextResponse.json(
      { error: "version and url are required" },
      { status: 400 },
    );
  }

  // The manifest URL becomes what the macOS updater downloads and installs, and
  // nothing in the chain carries a checksum or signature — so the host it names
  // is the entire integrity guarantee. Constrain it to the blob store releases
  // are actually published to: otherwise one authenticated request repoints every
  // installed client at an attacker-hosted .dmg. This also stops probeArtifact
  // below from being an SSRF primitive, since it fetches whatever it is handed.
  if (!isTrustedArtifactURL(url)) {
    return NextResponse.json(
      { error: "url must be an https:// Vercel Blob artifact URL" },
      { status: 400 },
    );
  }

  // Publishing a pointer to a missing artifact is the exact failure this guard
  // exists for: the site spent several releases handing out a .dmg that had
  // already been deleted from the blob store.
  const artifact = await probeArtifact(url);
  if (!artifact.ok) {
    return NextResponse.json(
      { error: `artifact not reachable at ${url}`, artifactStatus: artifact.status },
      { status: 400 },
    );
  }

  const manifest: ReleaseManifest = {
    version,
    url,
    target: typeof target === "string" && target ? target : "darwin-aarch64",
    pub_date: new Date().toISOString(),
    notes: typeof notes === "string" ? notes : "",
    size: typeof size === "number" && size > 0 ? size : artifact.size,
  };

  try {
    await writeLatestManifest(manifest);
  } catch (err) {
    console.error("[releases] failed to write latest.json", err);
    return NextResponse.json(
      { error: "failed to publish release manifest", detail: String(err) },
      { status: 502 },
    );
  }

  return NextResponse.json(manifest);
}

async function probeArtifact(
  url: string,
): Promise<{ ok: boolean; status: number; size: number }> {
  try {
    const res = await fetch(url, { method: "HEAD", cache: "no-store" });
    return {
      ok: res.ok,
      status: res.status,
      size: Number(res.headers.get("content-length") ?? 0) || 0,
    };
  } catch {
    return { ok: false, status: 0, size: 0 };
  }
}
