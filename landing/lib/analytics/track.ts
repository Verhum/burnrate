/**
 * Request-side entry point: turn an incoming request into a Vercel Web Analytics
 * custom event.
 *
 * A `.dmg` download leaves through a server-side 307, so the `<Analytics />` script
 * in the layout never sees it — pageviews get measured and the one number that
 * matters doesn't. `@vercel/analytics/server` closes that gap: same product, same
 * dashboard, same session and geo attribution as the client-side pageviews, and no
 * storage of our own to build or maintain.
 *
 * Recording happens in `after()`, so nothing here delays the download redirect or
 * the updater's response, and every failure path is swallowed — analytics must never
 * be the reason a release can't be downloaded.
 */

import { after } from "next/server";
import { isAutomated } from "./classify";
import { eventName, eventProperties, type TrackInput } from "./events";

export type { TrackInput };

/**
 * Resolve a request to the event it should be filed under. Split out from the
 * side effect so the naming rules are testable with a plain `Request`.
 */
export function resolveEvent(
  headers: Headers,
  input: TrackInput,
): { name: string; properties: { version: string; target: string } } {
  const automated = isAutomated(input.kind, headers.get("user-agent"));
  return {
    name: eventName(input.kind, input.outcome, automated),
    properties: eventProperties(input),
  };
}

/**
 * Should this request produce an event at all?
 *
 * Next.js serves HEAD from the GET handler, so counting both would double every
 * download made by a client that probes before fetching. `ANALYTICS_DISABLED` is
 * the kill switch — custom events are metered, so there has to be a way to stop.
 */
export function shouldRecord(req: Request): boolean {
  if (req.method === "HEAD") return false;
  return process.env.ANALYTICS_DISABLED !== "1";
}

/**
 * Record one event. Fire-and-forget: returns immediately, the beacon lands after
 * the response is flushed.
 */
export function trackEvent(req: Request, input: TrackInput): void {
  if (!shouldRecord(req)) return;

  // Snapshot the headers: `after()` runs once the response is done with, and
  // `track` needs the user agent (device/OS) and x-forwarded-for (country) to
  // attribute the event the same way a pageview would be.
  const headers = new Headers(req.headers);
  const { name, properties } = resolveEvent(headers, input);

  const send = async () => {
    // Imported lazily so a missing or disabled analytics package can't break a route.
    const { track } = await import("@vercel/analytics/server");
    await track(name, properties, { headers });
  };

  const guarded = send().catch((err: unknown) => {
    console.error("[analytics] failed to record event", name, err);
  });

  try {
    after(guarded);
  } catch {
    // Outside a request scope (unit tests, scripts) `after` is unavailable; the
    // promise above is already running, so there is nothing else to do.
  }
}
