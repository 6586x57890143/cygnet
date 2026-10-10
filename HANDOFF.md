# Handoff: Milestone 2 (`/provision`), in progress

Written 2026-10-11. Read this with `CLAUDE.md` and `SPEC.md`.

## Where the branches are

| Branch | State |
|---|---|
| `main` | M0, M1 and the rewritten spec. Deployed to foundry; skua is linked in production |
| `feat/provision` | `/provision fleet`, `Hub.Call` and `Manifest.App` |

## What M2 has so far

- `Hub.Call`: cygnet calls a bird herself, routed like any bird's call, from `cygnet`.
- `link.Manifest.App`: the bird's application ID, which is its bot user's. Needs
  `link/v0.3.0` tagged once merged, then a skua PR that fills it in. Until then skua's
  row on the card says its build doesn't say which app it is.
- `internal/modules/provision`: `/provision fleet`, an admin card with a line for cygnet
  and each linked bird: in the server or not, and the permissions it declares that its
  roles don't give it, server wide. A bird that isn't in, or is missing something, gets
  its own install link for this server (`guild_id`, `disable_guild_select`), which for
  a bird already in updates its role. `check again` redraws the card in place.
  Roles are read once and every bird is asked at once, inside the response window.

Decisions worth keeping:

- **The card reads Discord, not a cache.** cygnet caches only guilds, so roles and
  members come from REST on each card. Fine for an admin command; a live card that
  ticks itself would need `GUILD_MEMBERS` and is left for later.
- **Server wide only.** A channel's overwrites can still take a permission away. The
  per-channel check comes with layouts, which know the channels (`ponytail:` in
  `permsOf`).
- **The glyph is drawn here**, three sliders on pixelarticons' grid, not traced from a
  pixelarticons icon like the others.

## Not done yet, deliberately

- Layouts: compiled in, a diff preview, apply by diff with new guard ops, role order,
  onboarding.
- Drift cards at `watch`, `propose` or `act`, and the intents they need.
- The live install card: needs `GUILD_MEMBERS`.
- M3 to M5 in `SPEC.md`.

## From M0 and M1, still true

- **Copied, not imported.** skua's packages are `internal/`; a fix to a shared
  package goes to each repo on its own.
- **Private by default.** `/help` has no invite or source button, `tools/setup` warns
  if the app is public, and the prod compose publishes nothing. The hub listens only
  on the `fleet` network.
- Callee-side idempotency is each bird's job; `link` gives `NewID`/`CallID`.
- The voice lines in `internal/brand/voice.go` are still skua's.
- The local `golangci-lint` predates Go 1.27's export data and can't typecheck; CI's is
  current.
