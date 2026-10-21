# Wayfeed

A travel briefing for a country whose language you don't speak. Mark your route and dates on a map; Wayfeed
tells you, for each stop and day, what matters there: safety (protests, strikes, curfews, disasters), roads and
transport (closures, strikes), events that fit you, and the local background — translated, de-duplicated, with
a link to every original source.

Wayfeed is a demo of building a product on [feedql.com](https://feedql.com): it has no collector, no
database of news and no user store of its own. It signs you in with feedql (OAuth, PKCE) and reads and writes
through the public feedql API. Fork it to see how far the API takes you.

Status: first slice live at [wayfeed.app](https://wayfeed.app): sign in with feedql, plan a trip on a map with a preview, the briefing per stop and the alerts. Run it: `WAYFEED_COOKIE_KEY=$(openssl rand -hex 32) WAYFEED_BASE_URL=http://localhost:8787 LISTEN_ADDR=127.0.0.1:8787 go run .` (register an OAuth client with feedql first; the redirect address is `<base>/auth/callback`).

## Principles

- Talks to feedql only through the public API and OAuth — nothing internal.
- No secrets in this repo: a public PKCE OAuth client, configuration through environment variables.
- Aggregates, never advises: every safety item shows its source and is not an official warning.
- Trips are private by default and deleted after the trip.

## Licence

MIT — see [LICENSE](LICENSE).
