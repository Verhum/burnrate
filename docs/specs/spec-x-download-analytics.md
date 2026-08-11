# Spec X — download analytics for the landing site

`burnthemtokens.com` ships the `.dmg` and has had `<Analytics />` (Vercel Web Analytics) since
#46, so pageviews are visible. **Downloads are not.** The download button points at
`/api/releases/latest`, a route handler that looks up `releases/latest.json` in Vercel Blob and
answers with a 307 to the blob URL. A server-side redirect never runs client JS, so the one
number that matters — how many people actually took the app — was unmeasured.

Requirement: know how many downloads happen, when, of what version, and from where; and know how
many installs are out there.

## Approach: server-side custom events

Vercel Web Analytics supports [custom events](https://vercel.com/docs/analytics/custom-events),
and `@vercel/analytics/server` exposes a `track()` that works from a route handler. Given the
package is already a dependency and the pageviews are already there, the download belongs in the
same product as the pageview that preceded it — same session, same geo, same referrer, same
funnel, nothing new to operate.

The alternative considered and rejected was a self-hosted store on Vercel Blob (one blob per
event, dimensions encoded in the pathname, plus a private `/analytics` dashboard). It works
without a Pro plan and gives a queryable history, but it is ~1400 lines of storage, aggregation
and dashboard code to own for a number that Vercel will render for free. The trade below is
worth it.

### What we give up

- **Reading is the Vercel dashboard only.** There is no public query API for Web Analytics, so
  there is no self-hosted `/analytics` page and no JSON endpoint.
- **Custom events are Pro-only.** On Hobby the code runs and records nothing.
- **Retention is the plan's reporting window** — 12 months on Pro, 1 month on Hobby — not
  forever.
- **Events are metered**: $0.03 per 1k on Pro, above the plan credit.

## What is recorded

Two event kinds, both server-side, at the moment the thing actually happens:

| Kind | Source | Means |
|---|---|---|
| `download` | `GET /api/releases/latest` (the redirect branch) | a person took the `.dmg` |
| `update_check` | `GET /api/update` | one install phoned home |

`update_check` is recorded on **every** branch — `up_to_date`, `update_available`,
`wrong_target`, `no_release` — which is what turns the updater endpoint into an active-installs
and installed-version signal rather than just an update feed. (No client calls it yet; the Tauri
updater is not wired up, so expect zero until it is.)

## Event shape

Vercel allows **2 custom properties per event** on Pro (8 with the Web Analytics Plus add-on).
That budget is the single biggest constraint on the design, and it resolves cleanly:

- The two **high-cardinality** dimensions become the properties: `version` and `target`.
- Every **low-cardinality** dimension — which endpoint, what happened, was it automated — is
  folded into the **event name**, where it costs nothing and still groups in the Events panel.
- Country, referrer, OS and device are **derived by Vercel** from the request headers, so they
  never need to be sent at all. `track()` is given the request's headers so this attribution
  matches what a pageview from the same client would produce.

| Event name | Meaning |
|---|---|
| `download` | `.dmg` redirect served to a non-automated client |
| `download:bot` | same, but automated |
| `download:no_release` | download clicked, nothing to serve — an alarm, not a metric |
| `update_check:up_to_date` | install phoned home, was current |
| `update_check:update_available` | install phoned home, was behind |
| `update_check:wrong_target` | install phoned home from a platform with no build |
| `update_check:no_release` | updater ran with no manifest published |
| `update_check:bot` | crawler hit the updater feed |

`download` is deliberately the bare kind so the headline number is one row you can read at a
glance, with every not-a-successful-human-download case as a suffixed sibling beside it.

On `download`, `version`/`target` describe what was served. On `update_check` they describe the
*caller's current* version — that's the installed-version distribution.

## Counting rules

These are the rules that are easy to get wrong, and the reason `classify.ts` and `events.ts` are
pure and tested.

- **Crawlers are excluded everywhere; scripted clients only from downloads.** A `Go-http-client`
  hitting `/api/update` is exactly the caller we want to count. The same UA on the `.dmg`
  redirect is not a person. A single shared rule would either inflate downloads or zero out
  installs, so `isAutomated` takes the event kind.
- **A request with no user agent** is a script when downloading, and a plausible native updater
  when checking for updates.
- **HEAD is not a download.** Next.js serves HEAD from the GET handler, so counting both would
  double every download from a client that probes before fetching.
- **A JSON read of the manifest is not a download.** `Accept: application/json` returns the
  manifest and records nothing — that's tooling asking what the latest version is.
- **A download from a shared copy of the public blob URL bypasses the redirect** and cannot be
  counted. Accepted: the button is the supported path.

## Cardinality safety

`current_version` and `target` arrive as query params on `/api/update`, so they are
caller-controlled. Unbounded property values would mint a new dashboard row per request and
bill for the privilege. `normalizeVersion` accepts semver (optional `v` prefix, bounded
pre-release suffix) and `normalizeTarget` accepts a target triple; everything else collapses to
`unknown`.

## Failure behaviour

Analytics must never be the reason a release can't be downloaded. `trackEvent` returns
immediately and:

- sends inside `after()`, so nothing is on the response path;
- imports `@vercel/analytics/server` lazily, so a missing or broken package can't break a route;
- `.catch`-es the promise, so a plan without custom events, a disabled Web Analytics project,
  or a network failure all resolve to "no event recorded".

`ANALYTICS_DISABLED=1` is the kill switch. It matters more than it would with self-hosted
storage, because events are metered: if the desktop updater is pointed at `/api/update` on a
short poll interval, that is one billed event per install per poll.

## Local development

`@vercel/analytics/server` needs `VERCEL_URL`, which is only set on a deployment. Without it,
`track()` prints the event instead of sending it:

```
[Vercel Web Analytics] Track "download" with data {"version":"0.1.4","target":"darwin-aarch64"}
```

That is the real code path — classification and normalization have already run — so `npm run dev`
plus a click on the download button is a complete end-to-end check with no seeding, no local
store, and no token.

If Deployment Protection is ever enabled on the project, server-side `track()` starts returning
401 until a Protection Bypass for Automation secret exists; `@vercel/analytics/server` picks up
`VERCEL_AUTOMATION_BYPASS_SECRET` automatically once it does.

## Files

| File | Role |
|---|---|
| `landing/lib/analytics/events.ts` | event names, property normalization — pure |
| `landing/lib/analytics/classify.ts` | crawler vs scripted-client rules — pure |
| `landing/lib/analytics/track.ts` | the side effect, fired in `after()` |
| `landing/app/api/releases/latest/route.ts` | records `download` on the redirect branch |
| `landing/app/api/update/route.ts` | records `update_check` on every branch |

Tests run on Node's built-in runner directly against the `.ts` sources — no framework, no
transpiler, no bundler (`landing/scripts/ts-resolve-hook.mjs` supplies extensionless imports and
the `@/` alias).
