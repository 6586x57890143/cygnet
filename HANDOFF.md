# Handoff: Milestone 2 (`/provision`), in progress

Written 2026-10-11. Read this with `CLAUDE.md` and `SPEC.md`.

## Where the branches are

| Branch | State |
|---|---|
| `main` | M0, M1, the rewritten spec and M2 so far (#8 to #12). Deployed to foundry; skua is linked in production |
| `feat/panels` | one command a module, each a panel: `/provision`, and launches moved to `/launch` |

## What M2 has so far

- `Hub.Call`: cygnet calls a bird herself, routed like any bird's call, from `cygnet`.
- `link.Manifest.App`: the bird's application ID, which is its bot user's. Needs
  `link/v0.3.0` tagged once merged, then a skua PR that fills it in. Until then skua's
  row on the card says its build doesn't say which app it is.
- **One command a module, each a panel** (UX.md). `/provision` opens a panel that
  reads nothing from Discord: the layout the server keeps, the drift mode as a select,
  and buttons to the layout preview, the fleet card and a fresh start. Every card on
  the way has `◂ back`; on a card posted in a channel (a drift report) it gives the
  admin their own panel and leaves the card alone. `/launch` (its own module now, with
  a flag glyph) is the campaign card, or, before a page exists, a button that opens a
  form for its name. `/hjælp` is gone: it's skua's.
- `internal/modules/provision`: the fleet card, an admin card with a line for cygnet
  and each linked bird: in the server or not, and the permissions it declares that its
  roles don't give it, server wide. A bird that isn't in, or is missing something, gets
  its own install link for this server (`guild_id`, `disable_guild_select`), which for
  a bird already in updates its role. `check again` redraws the card in place.
  Roles are read once and every bird is asked at once, inside the response window.
- the layout preview: the compiled-in `flock` layout (`layout.go`) planned against the
  server (`plan.go`, pure) as a diff card, every overwrite spelled out, and an apply
  button that carries the plan's digest: if the server changed since the preview, apply
  shows the new plan instead of running. `apply.go` runs the writes in order off the
  gateway goroutine, one apply per server, each through `guard` (`RoleEdit` is new),
  redrawing a progress bar at most once a second, and stops at the first refusal.
  Role order is one change: the layout's roles swap the positions they already hold,
  so they never climb past the server's other roles.
- Drift (`drift.go`): a server is kept to its layout once one is applied there
  (`provision` table, migration 002). Role and channel updates and deletes, which come
  with `GUILDS`, start a check once the server has been quiet for 5s; drift is
  reported in the layout's `Reports` channel (`#cygnet`, staff only), once per distinct
  drift. The panel's mode: `watch` reports, `propose` (the default) adds the apply
  button, `act` puts it back first and then says what it did.

Decisions worth keeping:

- **The card reads Discord, not a cache.** cygnet caches only guilds, so roles and
  members come from REST on each card. Fine for an admin command; a live card that
  ticks itself would need `GUILD_MEMBERS` and is left for later.
- **Server wide only.** A channel's overwrites can still take a permission away. The
  per-channel check comes with layouts, which know the channels (`ponytail:` in
  `permsOf`).
- **Only what a layout names, unless asked.** A plan adds and changes; it never deletes,
  and leaves every role, channel and overwrite the layout doesn't name alone. A role
  above cygnet's is shown as blocked, not attempted.
- **A fresh start asks twice.** **start fresh** plans the removal of
  every channel, category and role the layout doesn't name, after its own changes.
  The preview offers `archive old` (the old channels move into `┄ archive`, hidden from
  @everyone, in one `lock_permissions` move; categories and roles are deleted) or
  `replace` (all deleted). Either opens a confirmation that counts what goes before
  the button that does it. It keeps the archive and what's in it, bots' roles, roles
  holding Administrator (whoever runs it may be an admin by that role alone), and a
  community server's required channels. If the card's channel went with the old
  layout, the result is posted in the nest.
- **Onboarding is part of the layout.** `flock` asks one question, "what do you come
  here for", whose answers open `#studio`, `#play` and `#general`, over five default
  channels, in advanced mode, so Discord counts 7 channels and 5 to talk in. The plan
  compares on, mode, default channels and the question, and is one change
  (`! onboarding: ...`). It writes after the channels, reading them again so new ones
  have IDs, keeps questions an admin added and the IDs of its own, so members'
  answers survive a reapply. On a server that isn't a community it waits, as a bird's
  overwrite does, instead of standing as drift. It costs cygnet Manage Server, which
  `needs` asks for only for a layout that has onboarding; `guard.GuildEdit` caps it.
- **Left alone is per aspect, not per value.** A drift report's select stores each
  picked change by its preview line (`! perms #rules: @everyone`) in the server's row
  (`leave`, migration 003, at most 100). Every plan but a fresh start's drops those
  lines, so cygnet stops managing that aspect whatever its value; in `act` she has
  already put it back, so the admin makes their change again. **forget them** on
  the panel clears the list, and so does a fresh start.
- **Every role is coloured.** The layout's roles carry their colours, and every bot's
  role takes `BotColor`, linked bird or not, except cygnet's own at her top, which she
  can't reach. A bot joining (`RoleCreate`) starts a drift check, so in `act` it is
  coloured and let into the nest on its own.
- **cygnet's install follows the layouts.** Discord lets a bot grant only what it holds,
  so `Perms` is roles, channels and every permission a layout hands out, and the
  fleet card offers to grant cygnet exactly that.
- **A bird's overwrite waits for the bird.** It is named by the bird and resolved to the
  role Discord made for it (`Tags.BotID`), so skua's overwrite in `#announcements`
  lands once skua is in, on the next apply.
- **Drift reports go to a channel the layout makes.** `#cygnet` is hidden from
  everyone but mods and cygnet, whose own overwrite (`Self`) lets her post there. A
  server whose report channel is gone gets nothing but a log line.
- **Names are styled, matched bare.** A channel is `emoji` + U+2502 + name on Discord
  (weloveyou's measured separator: whitespace becomes a hyphen and zero-width
  characters are stripped, both with a 200), and categories draw a tree down the
  sidebar (`╭ welcome`, `├ chatter`, `├ song`, `╰ nest`). Matching strips the
  decoration (`bare`), so restyling a server renames its channels and keeps their
  history. Discord allows two renames a channel per ten minutes; the rate limiter waits.
- **`birds` in an overwrite is every linked bird but cygnet**, each by its own role, so
  the nest lets in whichever birds are linked and in the server.
- **The glyph is drawn here**, three sliders on pixelarticons' grid, not traced from a
  pixelarticons icon like the others.

## The edge (M4, started)

Since #23 the copy is cygnet's flavour (UX.md, Voice), and every page carries the
home page's footer: the flock, live from the hub (`launch/flock.go` pushes each linked
bird's avatar, `PUT /admin/bird/<name>`), and a link to `/privacy`, whose terms are
centred. The Worker is deployed by hand (`npm -C edge run deploy`, which applies D1
migrations first); CI only tests it.


`edge/` is the Worker at `cygnet.lol` (`docs/edge.md`): `/silcord` takes sign ups
through Turnstile and Discord (`identify email`), one place per Discord account and per
verified email. Admins release invites to the front of the line; a member makes one
after a fresh Discord sign in and an email proof, and the person it's for takes it with
their own, which links them to their sponsor in one D1 batch. An email is proven by a
link emailed to it (Scaleway Transactional Email, pay per use), opened on any device and
acted on only in the browser that asked; a page that confirms itself keeps mail scanners
from clicking it for them. Sends are capped at 2,900 in any 31 days, about €0.65 at
worst, and the rest runs on Cloudflare's free plan.
Every change lands in a keyed, chained, append only ledger in the same batch, and
`/admin/verify` checks it. Identifiers are HMACs under `HMAC_KEY`, forgotten by a daily
cron. The copy is a person texting: lowercase, no punctuation, one thought a line, and
a test fails any page with punctuation or a capital. Tested with `node --test` against
`node:sqlite`, including tampering, typed with `wrangler types`, in CI's `edge` job.
Not live until the steps in `docs/edge.md` are done.

## Not done yet, deliberately

- More layouts than `flock`, and a way to choose channel positions.
- The live install card: needs `GUILD_MEMBERS`.
- M3 to M5 in `SPEC.md`. For M4: sealed tokens, the valve, standing for sponsors whose
  invitees are removed, and how cygnet reads the list and the tree (through the tunnel,
  as the spec says; the admin endpoints are a bearer token until then).

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
