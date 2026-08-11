import { NextRequest, NextResponse } from "next/server";
import { readLatestManifest } from "@/lib/releases/manifest";
import { trackEvent } from "@/lib/analytics/track";

// An installed app polling for updates must not be answered from a cache that
// outlives the release it was built from.
export const dynamic = "force-dynamic";
export const revalidate = 0;

export async function GET(req: NextRequest) {
  const currentVersion = req.nextUrl.searchParams.get("current_version");
  const target = req.nextUrl.searchParams.get("target") || "darwin-aarch64";

  if (!currentVersion) {
    return NextResponse.json(
      { error: "current_version query param required" },
      { status: 400 },
    );
  }

  // Every check is one install phoning home, so the outcome is recorded on all
  // paths — that's what turns this endpoint into an active-installs signal.
  const track = (outcome: "up_to_date" | "update_available" | "wrong_target" | "no_release") =>
    trackEvent(req, { kind: "update_check", version: currentVersion, target, outcome });

  const manifest = await readLatestManifest();

  if (!manifest) {
    track("no_release");
    return new NextResponse(null, { status: 204 });
  }

  if (manifest.version === currentVersion) {
    track("up_to_date");
    return new NextResponse(null, { status: 204 });
  }

  if (manifest.target !== target) {
    track("wrong_target");
    return new NextResponse(null, { status: 204 });
  }

  track("update_available");
  return NextResponse.json(
    {
      version: manifest.version,
      url: manifest.url,
      pub_date: manifest.pub_date,
      notes: manifest.notes,
    },
    { headers: { "cache-control": "no-store" } },
  );
}
