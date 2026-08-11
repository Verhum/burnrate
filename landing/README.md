# burnrate landing site

Next.js marketing site for burnrate — **burnthemtokens.com**. Deployed on Vercel; also serves
the release manifest, the `.dmg` redirect, and the updater endpoint.

```bash
npm install
npm run dev         # http://localhost:3100
npm run build
npm test            # node --test against the .ts sources (no framework)
npm run typecheck   # tsc --noEmit
make deploy         # npx vercel deploy --prod
```

## Routes

| Route | Purpose |
|---|---|
| `/` | landing page |
| `GET /api/releases/latest` | 307 to the latest `.dmg`; `Accept: application/json` returns the manifest |
| `GET /api/update?current_version=&target=` | Tauri-style update feed (204 when current) |
| `POST /api/releases/upload` | register a new release manifest (Bearer `UPLOAD_SECRET`) |

Publishing a release: `./scripts/upload-release.sh VERSION "notes"` uploads the `.dmg` to Blob
and registers the manifest. Normally you don't call it directly — `make upload` at the repo
root does, for the version currently in `tauri.conf.json`.

## The release pointer

Everything the download button and the updater serve comes from one mutable blob,
`releases/latest.json`, read and written through `lib/releases/manifest.ts`. Two Vercel Blob
defaults make that pointer easy to strand, and both have already broken the site in production
(the download link served **v0.1.0** for six releases, pointing at a `.dmg` that had since been
deleted):

- **`put()` refuses to overwrite an existing pathname.** Without `allowOverwrite: true` every
  publish after the first one throws, so the pointer freezes on the first version ever uploaded.
- **Blobs are CDN-cached for 30 days.** Even once the pointer *is* rewritten, readers keep getting
  the old JSON. Writes now ask for `cacheControlMaxAge: 0` — the store floors that at `max-age=60`
  — so reads also append the blob's `uploadedAt` to the URL, giving each generation its own cache
  key. That's what makes a publish visible immediately, and it's also the only thing that gets
  past the 30-day `max-age` already baked into blobs written before this.

Three guards keep a silent failure from looking like a success:

1. `POST /api/releases/upload` HEAD-checks the `.dmg` URL before moving the pointer, so a URL that
   was never uploaded or has since been deleted is rejected (400) rather than published.
2. A failed blob write answers **502**, not an unhandled 500.
3. `upload-release.sh` reads the release back through `GET /api/releases/latest` afterwards and
   fails unless the live version matches and the `.dmg` it names returns 200.

If the download is ever stale again, the first question is what
`curl -H 'Accept: application/json' https://www.burnthemtokens.com/api/releases/latest` says
versus `desktop/src-tauri/tauri.conf.json` — re-running `make upload` republishes the pointer.

## Download analytics

Downloads leave through a **server-side redirect**, so the `<Analytics />` script in the layout
never sees them — pageviews get measured and the one number that matters doesn't. `lib/analytics/`
closes that gap with [Vercel Web Analytics custom events](https://vercel.com/docs/analytics/custom-events)
sent from the server (`@vercel/analytics/server`), so downloads land in the same dashboard, with
the same geo/referrer/device attribution, as the pageviews they follow.

- `lib/analytics/events.ts` — event names, property normalization (pure)
- `lib/analytics/classify.ts` — crawler vs scripted-client rules (pure)
- `lib/analytics/track.ts` — the side effect, fired inside `after()`

Design and rationale: [`../docs/specs/spec-x-download-analytics.md`](../docs/specs/spec-x-download-analytics.md).

### Reading the numbers

Vercel dashboard → the project → **Analytics** → the **Events** panel. Select an event name to
break it down by its properties, and use the page-level filters for country, referrer, OS and
device. There is no self-hosted dashboard and no query API — that's the trade for not owning a
storage layer.

| Event | Exactly what it means |
|---|---|
| `download` | a `.dmg` redirect served to a non-automated client |
| `download:bot` | same, but a crawler, unfurler, `curl`/`wget`-style client, or no UA at all |
| `download:no_release` | someone clicked download and there was nothing to serve — an alarm, not a metric |
| `update_check:up_to_date` | an install phoned home and was current |
| `update_check:update_available` | an install phoned home and was behind |
| `update_check:wrong_target` | an install phoned home from a platform with no build |
| `update_check:no_release` | the updater ran with no manifest published |
| `update_check:bot` | a crawler hit the updater feed |

Every event carries exactly two properties: `version` and `target`. On `download` that's the
version served; on `update_check` it's the caller's *current* version — which makes the updater
feed an installed-version and active-install signal, not just a download counter.

Counting rules that are easy to break:

- **Crawlers are excluded everywhere, scripted clients only from downloads.** A `Go-http-client`
  hitting `/api/update` is exactly the caller we want; the same UA on the `.dmg` redirect isn't a
  person. One shared rule would either inflate downloads or zero out installs.
- **HEAD is not a download.** Next serves HEAD from the GET handler, so a client that probes
  before fetching would otherwise be counted twice.
- **A JSON read of the manifest is not a download.** `Accept: application/json` returns early.
- **A download from a shared copy of the public blob URL bypasses the redirect** and can't be
  counted at all.

`version` and `target` on `/api/update` are caller-supplied query params, so anything that isn't
a plausible semver or target triple collapses to `unknown` rather than minting a new row in the
dashboard for every request.

### Plan requirements

| | Hobby | Pro | Pro + Web Analytics Plus |
|---|---|---|---|
| Custom events | ✗ **not available** | ✓ | ✓ |
| Properties per event | — | **2** | 8 |
| Reporting window | 1 month | 12 months | 24 months |
| Cost | 50k events/mo included | $0.03 / 1k events | + $10/mo per team |

Two consequences worth knowing before you read a zero:

1. **On Hobby, none of this collects.** The code runs and fails silently by design (see below);
   the events simply never appear. This needs a Pro team.
2. **Two properties is the whole budget on Pro**, which is why every other dimension is folded
   into the event *name* rather than sent as data. Adding a third property means the Plus add-on.

Update checks are metered like any other event. If the desktop updater is ever pointed at
`/api/update` on a short poll interval, that is one billed event per install per poll — at
$0.03/1k, 100 installs polling hourly is ~72k events and ~$2/month. Lengthen the interval before
adding installs, and use `ANALYTICS_DISABLED=1` as the kill switch.

### Failure behaviour

`trackEvent` never throws and never blocks. The beacon is sent inside `after()`, the
`@vercel/analytics/server` import is lazy, and the promise is `.catch`-ed — a missing plan, a
disabled Web Analytics project, or a network failure all resolve to "no event was recorded", never
to a failed download.

### Environment

| Variable | Needed for | Notes |
|---|---|---|
| `BLOB_READ_WRITE_TOKEN` | release storage | set automatically on Vercel |
| `UPLOAD_SECRET` | release upload | |
| `ANALYTICS_DISABLED` | kill switch | `1` stops all recording |
| `VERCEL_URL` | sending events | set automatically on Vercel; unset locally, which is why dev logs instead of sending |
| `VERCEL_AUTOMATION_BYPASS_SECRET` | sending events on protected deployments | only needed if Deployment Protection is enabled — otherwise `track()` gets a 401 |

### Working on it locally

Nothing to set up. Without `VERCEL_URL`, `@vercel/analytics/server` prints each event instead of
sending it, so `npm run dev` and then clicking download gives you:

```
[Vercel Web Analytics] Track "download" with data {"version":"0.1.4","target":"darwin-aarch64"}
```

That's the real code path — the classification and normalization have already run by that point.
