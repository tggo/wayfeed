# wayfeed — repo guide

Public repo (github.com/tggo/wayfeed). A travel-briefing PWA built on feedql: the demo of "a product on the
feedql engine". Product spec and pitfalls: `../docs/ideas/wayfeed-trips.md`; pre-launch checklist:
`../docs/ideas/wayfeed-launch-problems.md`.

## Rules

- **Public repo.** Never commit secrets, tokens, internal hostnames/ports, server IPs or customer data.
  Configuration only through env vars; `.env*` is git-ignored.
- **Only the public feedql API.** No imports from `../backend`, no direct Mongo/Redis. If something is missing,
  it becomes a feedql API story first — that gap is the finding.
- Sign-in only via "Sign in with feedql" (OAuth + PKCE, public client). No own user store.
- Owner decisions (2026-10-20): free, one trip per feedql user; pilots Sri Lanka, Georgia, USA (domestic
  multi-stop routes are first-class).
- Stack follows the workspace: Go + Fiber, templ, htmx, plain CSS, SSR; PWA manifest + service worker + Web Push.
- English for code and UI; talk with the owner in Ukrainian.
- Commit messages never mention Claude or AI; no Co-Authored-By lines.
- Keep `DEFINITION_OF_DONE.md` as the spec once code starts.
