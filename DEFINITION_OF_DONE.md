# Definition of done

Wayfeed is a product built only on the public feedql API (`https://api.feedql.com`) and "Sign in with feedql".

## Sign in with feedql
- OAuth authorization code with PKCE S256 as a public client (`wayfeed`, no secret), scope `trips:write`, resource `https://api.feedql.com/api`.
- The tokens live in a sealed (AES-256-GCM) HttpOnly, SameSite=Lax cookie; the server keeps no state and no user store. The key (`WAYFEED_COOKIE_KEY`) is generated on the server by the deploy and is not in this repository.
- An expired access token is refreshed with the refresh token; a refused refresh or a 401 clears the cookie and sends the person to sign in again.
- State-changing requests from another origin are refused (403).

## Public example routes (no account)
- The landing page shows the three pilot routes (Sri Lanka, Georgia, US west) as cards from feedql's public demo endpoint (`GET /p/trips`), with a fallback list when feedql cannot be reached, and "Build your own trip" (Sign in with feedql).
- `/demo/<slug>` is the same trip page, read only, with no sign-in: stop tabs, the newest stories near the stop grouped by day with the translated title and the original, the alerts, the disclaimer, no archive, delete or push controls. Data comes from `GET /p/trips/<slug>` (the routes are kept by feedql's house account and their dates roll every day), kept five minutes here; an older copy is shown if feedql is down; an unknown slug is a 404 page.
- The demo pages are indexable (Open Graph, canonical, sitemap); trip pages of people stay `noindex`.

## Trip
- Stops are picked on an OpenStreetMap map (Leaflet 1.9.4 from unpkg with subresource integrity) or found by name with Nominatim; each has arrive/leave dates and a radius, up to 8, reorderable.
- "Preview what will be collected" calls `POST /api/v1/trips` with `dry_run: true` and shows feedql's own words (what is collected, refresh, who sees it, when it is deleted, whether it can be undone). "Create this trip" appears only after a preview of the current stops; any edit hides it again.
- One trip at a time: feedql's 409 `trip_active` is shown in its own words with a link to the trip.
- The trip page: state, dates, what capacity allowed for the refresh, one tab per stop, the briefing of the selected stop (the same block on the demo and the private page): "Right now" (what matters today) and "Day by day" (one fold per day of the stop), each in four groups: Safety and Roads and transport (the most serious first, with Serious / Notable / Minor and a "official source" badge), Events (the day or days they take place, "Cancelled" and "Dates changed" badges) and Background; translated title, "translated from X (check the original)", the original title; alerts with the overflow line; and "Archive" / "Delete" each saying what happens after it.
- Every page that shows safety-related text says it is collected from sources and is not an official warning.
- Trip pages are `noindex`, `Cache-Control: private, no-store`; the landing page is indexable.

## PWA
- Manifest, icon, service worker (static files cache-first; `/trip` network-first with an offline copy of the last page; sign-out clears the copy).
- Web Push: the trip page has "Alerts on this device" (turn on, turn off, show a test notification, and a sentence saying what each does). Turning on asks the browser for permission, subscribes with feedql's public VAPID key (`GET /api/v1/trips/push`), and gives the subscription to feedql (`POST /api/v1/trips/push`, scope `trips:write`); turning off calls `DELETE` and unsubscribes. feedql sends the alerts; the service worker shows `{title, body}` and opens `/trip` on a tap. The device receives trip alerts only. A browser without push (iPhone outside the Home Screen app) is told so. The test notification is shown by the page itself and sends nothing.

## Tests
- `go test ./...` (86%+ coverage): sealed cookies, the PKCE request and the token exchange against a fake feedql, refresh and sign-out, every page and POST route, error passthrough, cross-site refusal. The planner script is checked by hand in a browser (phone and desktop screenshots).

## Not yet
- A day-by-day view and buckets (wait for feedql event dates and buckets), a trip list for archived trips.

## Demo pages on the generic API (Story 40.10, 2026-10-21, owner screenshot)

- **Why:** the demo showed titles only, Reddit chatter in Background and a wrong "Dates changed" badge, and it read feedql's trip-only endpoint.
- **Expected:** `/demo/<slug>` reads the demo's `feed_url` (the channel's `fqp_` feed, listed by `GET /p/trips`) with `lang=en&summary=1&group=topic,day,bucket&order=severity&collapse=event&tz=<zone>`: a one or two sentence summary under every headline; only stories with a bucket; Background capped at 3 a stop here (a view choice); a headline still being translated is counted, not shown. Stops are the channel's topics named like the stop; until the channel has them (Story 40.9) the whole channel is one list grouped by day and bucket. Without a `feed_url` or when the feed fails it falls back to the trip endpoint.
- **Display guard:** "Dates changed" shows only when `when.from` is set (the status came from a newer report of the same event). The Tamil congress announcement had a status of "changed" from the date extractor alone; the extractor is fixed in the 39.2 queue.
- **Open:** excluding Reddit chat and requiring a subject place belongs in the stop topics' filters (40.9, lane-40-C), not here.
- **Tests:** `TestDemoPageReadsThePublicFeedWithSummariesAndCapsBackground`, `TestDatesChangedBadgeNeedsANewerReportOfTheEvent`.
