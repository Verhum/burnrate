/**
 * Mapping from "something happened on the server" to a Vercel Web Analytics
 * custom event. Pure and dependency-free, so the naming and cardinality rules
 * are testable without a network or a deployment.
 *
 * Two constraints from Vercel shape everything here:
 *
 *  1. **Two custom properties per event** on Pro (eight with the Web Analytics
 *     Plus add-on). So only the two high-cardinality dimensions — `version` and
 *     `target` — get to be properties. Every low-cardinality dimension (which
 *     endpoint, what happened, was it a bot) is folded into the *event name*
 *     instead, where it costs nothing and still groups in the Events panel.
 *     Country, referrer, device and OS are derived by Vercel from the request
 *     headers, so they never need to be sent.
 *
 *  2. **Property values are rendered as a list in the dashboard**, so unbounded
 *     values are a usability problem as well as a cost. `current_version` and
 *     `target` arrive as query params on `/api/update` — caller-controlled.
 *     Anything that isn't a plausible version or target collapses to a single
 *     bucket rather than minting a new row per request.
 */

export type EventKind = "download" | "update_check";

export type Outcome =
  | "served" // download: redirected to the .dmg
  | "no_release" // manifest missing or unreadable
  | "update_available" // update_check: caller is behind
  | "up_to_date" // update_check: caller is current
  | "wrong_target"; // update_check: no build for the caller's platform

export interface TrackInput {
  kind: EventKind;
  /** version served (download) or the caller's current version (update_check) */
  version?: string | null;
  target?: string | null;
  outcome: Outcome;
}

/** Bucket for a missing or implausible value. */
export const UNKNOWN = "unknown";

/** Semver, with an optional `v` prefix and a bounded pre-release/build suffix. */
const VERSION = /^v?\d{1,4}\.\d{1,4}\.\d{1,4}(?:[-+][0-9A-Za-z][0-9A-Za-z.-]{0,19})?$/;

/** Tauri-style target triple: `darwin-aarch64`, `linux-x86_64`, … */
const TARGET = /^[a-z0-9]{1,16}-[a-z0-9_]{1,16}$/;

export function normalizeVersion(value: string | null | undefined): string {
  if (!value || !VERSION.test(value)) return UNKNOWN;
  return value.replace(/^v/, "");
}

export function normalizeTarget(value: string | null | undefined): string {
  if (!value || !TARGET.test(value)) return UNKNOWN;
  return value;
}

/**
 * The name the event appears under in the Vercel Events panel.
 *
 * `download` is deliberately the bare kind, so the headline number is one row you
 * can read at a glance; everything that is *not* a person successfully getting the
 * app is a suffixed sibling sorted next to it.
 */
export function eventName(kind: EventKind, outcome: Outcome, automated: boolean): string {
  if (automated) return `${kind}:bot`;
  return outcome === "served" ? kind : `${kind}:${outcome}`;
}

/** Exactly two properties — see the plan limit above. */
export function eventProperties(input: TrackInput): { version: string; target: string } {
  return {
    version: normalizeVersion(input.version),
    target: normalizeTarget(input.target),
  };
}
