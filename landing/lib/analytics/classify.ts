/**
 * Traffic classification. Pure, so the rules are testable and reviewable in one place.
 *
 * The point of splitting crawlers from scripts is that they mean opposite things
 * depending on the endpoint: a `Go-http-client` hitting /api/update is exactly the
 * caller we want to count, while the same UA hitting the .dmg redirect is not a
 * person downloading the app.
 */

import type { EventKind } from "./events";

/** Search engines, link unfurlers, uptime pingers — never a real download. */
const CRAWLER =
  /(bot\b|bot\/|crawler|crawl\b|spider|slurp|bingpreview|facebookexternalhit|slackbot|discordbot|telegrambot|whatsapp|twitterbot|linkedinbot|embedly|quora link preview|preview|pingdom|uptimerobot|statuscake|monitoring|headless)/i;

/** Programmatic HTTP clients. Legitimate for update checks, automated for downloads. */
const SCRIPT =
  /(curl\/|wget|python-requests|urllib|go-http-client|okhttp|axios|node-fetch|got\/|libwww|scrapy|httpie|powershell|java\/|ruby|php\/)/i;

export function isCrawler(userAgent: string | null | undefined): boolean {
  return Boolean(userAgent && CRAWLER.test(userAgent));
}

export function isScriptedClient(userAgent: string | null | undefined): boolean {
  return Boolean(userAgent && SCRIPT.test(userAgent));
}

/**
 * Should this request land under a `:bot` event name rather than the headline one?
 *
 * - `download` — a person clicking the button in a browser. Crawlers, scripted
 *   fetches and requests with no UA at all are all automated.
 * - `update_check` — the desktop app phoning home. Native HTTP clients send a
 *   scripted-looking UA or none at all, so only crawlers are excluded.
 */
export function isAutomated(kind: EventKind, userAgent: string | null | undefined): boolean {
  if (isCrawler(userAgent)) return true;
  if (kind === "update_check") return false;
  return !userAgent || isScriptedClient(userAgent);
}
