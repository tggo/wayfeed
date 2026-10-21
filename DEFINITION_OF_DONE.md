# Definition of done

Wayfeed is a product built only on the public feedql API (`https://api.feedql.com`) and "Sign in with feedql".

## Sign in with feedql
- OAuth authorization code with PKCE S256 as a public client (`wayfeed`, no secret), scope `trips:write`, resource `https://api.feedql.com/api`.
- The tokens live in a sealed (AES-256-GCM) HttpOnly, SameSite=Lax cookie; the server keeps no state and no user store. The key (`WAYFEED_COOKIE_KEY`) is generated on the server by the deploy and is not in this repository.
- An expired access token is refreshed with the refresh token; a refused refresh or a 401 clears the cookie and sends the person to sign in again.
- State-changing requests from another origin are refused (403).

## Trip
- Stops are picked on an OpenStreetMap map (Leaflet 1.9.4 from unpkg with subresource integrity) or found by name with Nominatim; each has arrive/leave dates and a radius, up to 8, reorderable.
- "Preview what will be collected" calls `POST /api/v1/trips` with `dry_run: true` and shows feedql's own words (what is collected, refresh, who sees it, when it is deleted, whether it can be undone). "Create this trip" appears only after a preview of the current stops; any edit hides it again.
- One trip at a time: feedql's 409 `trip_active` is shown in its own words with a link to the trip.
- The trip page: state, dates, what capacity allowed for the refresh, one tab per stop, the briefing of the selected stop grouped by day (translated title, "translated from X (check the original)", the original title), alerts with the overflow line, and "Archive" / "Delete" each saying what happens after it.
- Every page that shows safety-related text says it is collected from sources and is not an official warning.
- Trip pages are `noindex`, `Cache-Control: private, no-store`; the landing page is indexable.

## PWA
- Manifest, icon, service worker (static files cache-first; `/trip` network-first with an offline copy of the last page; sign-out clears the copy). Web Push is not built yet.

## Tests
- `go test ./...` (86%+ coverage): sealed cookies, the PKCE request and the token exchange against a fake feedql, refresh and sign-out, every page and POST route, error passthrough, cross-site refusal. The planner script is checked by hand in a browser (phone and desktop screenshots).

## Not yet
- Web Push subscription, a day-by-day view and buckets (wait for feedql event dates and buckets), a trip list for archived trips.
