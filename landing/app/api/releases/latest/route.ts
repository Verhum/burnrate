import { NextRequest, NextResponse } from "next/server";
import { readLatestManifest } from "@/lib/releases/manifest";
import { trackEvent } from "@/lib/analytics/track";

// The download button has to reflect a release published seconds ago, so this
// route is never prerendered or cached — the pointer it reads is mutable by
// design, and a cached answer here is indistinguishable from a failed publish.
export const dynamic = "force-dynamic";
export const revalidate = 0;

export async function GET(req: NextRequest) {
  const wantJson = req.headers.get("accept")?.includes("application/json");

  const manifest = await readLatestManifest();

  if (!manifest) {
    trackEvent(req, { kind: "download", outcome: "no_release" });
    return NextResponse.json(
      { error: "no releases found" },
      { status: 404, headers: { "cache-control": "no-store" } },
    );
  }

  // A JSON read is tooling asking what the latest version is, not a person
  // downloading the app — only the redirect counts as a download.
  if (wantJson) {
    return NextResponse.json(manifest, { headers: { "cache-control": "no-store" } });
  }

  trackEvent(req, {
    kind: "download",
    version: manifest.version,
    target: manifest.target,
    outcome: "served",
  });

  const res = NextResponse.redirect(manifest.url);
  res.headers.set("cache-control", "no-store");
  return res;
}
