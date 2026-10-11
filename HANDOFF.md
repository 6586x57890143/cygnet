# Handoff: Milestone 2 (`/provision`), in progress

Written 2026-10-11. Read this with `CLAUDE.md` and `SPEC.md`.

## Where the branches are

| Branch | State |
|---|---|
| `main` | M0, M1, the rewritten spec and M2 so far (#8 to #12). Deployed to foundry; skua is linked in production |
| `feat/provision-fresh` | role colours and the fresh start |

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
- `/provision layout`: the compiled-in `flock` layout (`layout.go`) planned against the
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
  drift. `/provision mode`: `watch` reports, `propose` (the default) adds the apply
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
- **A fresh start asks twice.** `/provision layout fresh:true` plans the removal of
  every channel, category and role the layout doesn't name, after its own changes.
  The preview offers `archive old` (the old channels move into `┄ archive`, hidden from
  @everyone, in one `lock_permissions` move; categories and roles are deleted) or
  `replace` (all deleted). Either opens a confirmation that counts what goes before
  the button that does it. It keeps the archive and what's in it, bots' roles, roles
  holding Administrator (whoever runs it may be an admin by that role alone), and a
  community server's required channels. If the card's channel went with the old
  layout, the result is posted in the nest.
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

`edge/` is the Worker at `cygnet.lol` (`docs/edge.md`): `/silcord` takes sign ups
through Turnstile and Discord (`identify email`), one place per Discord account and per
verified email. Admins release invites to the front of the line; a member makes one
after a fresh Discord sign in and an email proof, and the person it's for takes it with
their own, which links them to their sponsor in one D1 batch. An email is proven by the
person sending one to a one time address: the Worker's `email` handler checks its DKIM
itself (`dkim.ts`), because Email Routing's own check doesn't tie a signature to `From`.
Every change lands in a keyed, chained, append only ledger in the same batch, and
`/admin/verify` checks it. Identifiers are HMACs under `HMAC_KEY`, forgotten by a daily
cron. Tested with `node --test` against `node:sqlite`, including tampering and spoofed
mail, typed with `wrangler types`, in CI's `edge` job. Free plan only: nothing is sent.
Not live until the steps in `docs/edge.md` are done.

## Not done yet, deliberately

- Onboarding in layouts.
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
