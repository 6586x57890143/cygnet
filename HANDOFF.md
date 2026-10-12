# Handoff: Milestone 3 (codes), built

Written 2026-10-13. Read this with `CLAUDE.md` and `SPEC.md`.

## Where the branches are

| Branch | State |
|---|---|
| `main` | M0 to M2, the edge's launch list (M4, started), and M3's store (#31) and user installed commands (#32). Deployed to foundry |
| `feat/invite` (#33) | `/invite`, the panel |
| `feat/redeem` (#34) | cygnet's side of redemption, attribution and the sweep; on `feat/invite` |
| `feat/edge-redeem` (#35) | the Worker's `/i/<code>` for server codes, and the tunnel; on `feat/redeem` |
| `feat/tree` | the tree, and these docs; on `feat/edge-redeem` |

The four go in together, in that order: alone, `/invite` would hand out links
cygnet.lol can't take yet.

## What M3 changed

- **Postgres owns codes and joins** (`internal/store/codes.go`, migration 004). A code
  is its SHA-256 with its server, owner, expiry, spender and arrival. `Spend` is one
  conditional `UPDATE`, and spending a code you already spent returns it (the
  redemption's idempotency key); someone holds one code into a server at a time (a
  partial unique index). `Arrive` records a join with the code its member spent there or
  none, under an advisory lock per member, so a direct add and its gateway event make
  one join. `invites` holds each server's allowance.
- **User installed commands** (`core`): one whose integration types include user
  install is registered globally, and has to be `Public`; the install settings turn user
  install on exactly when one runs.
- **`/invite`** (`internal/modules/invite`): the panel lists your links and what became
  of each, makes one (shown once), takes one back. From a DM or another server it offers
  the servers cygnet is in where you're a member and links are allowed. Admins set the
  allowance from inside (none until they do) and have no limit; the owner has none
  from anywhere. A server with the module off has it off from DMs too (`core.Gated`).
- **Redemption** (`redeem.go`): cygnet.lol's Worker is the bird `edge`, calling
  `cygnet.code` and `cygnet.redeem` over `POST /v1/call`. Gate (30 day account, not
  banned), spend, then in: added with their `guilds.join` token, or a **targeted
  invite** (Discord's `target_users_file`: theirs alone, one use, 300 s, returned once
  Discord's job finishes). Already in spends nothing. A code whose owner left is gone.
- **The token** goes through a REST client with a discard logger (disgo logs request
  bodies at debug) and the error kept from the add is rebuilt without the request.
- **Attribution** (`joins.go`): member add and remove events (Server Members, optional),
  and a sweep on `GuildsReady` over codes spent in the last 7 days.
- **The Worker** (`edge/src/hub.ts`, `index.ts`): a code D1 doesn't know is a server's.
  Its page has two ways in behind one Turnstile form, the callback sends the token only
  for an add and revokes it after, and refusals are said plainly. The privacy notice says
  what a server's link keeps.
- **The tunnel**: cloudflared in the prod compose behind the `tunnel` profile, on the
  fleet network only. Setup is in `docs/edge.md`, The tunnel.
- **The tree**: on an admin's `/invite` panel, one line a sponsor ("brought ..."), the
  untraced on their own line, leavers struck through, cut at a card's room.

Decisions worth keeping:

- **SPEC experiment 4 is answered**: Discord restricts an invite to chosen users itself,
  so the fallback needs no binding of cygnet's and its join is exact.
- **The launch list's D1 invites stay where they are** until M4's hand-off moves them to
  Postgres (the "Postgres owns membership" decision); nothing new was put in D1.
- **No ledger table in Postgres yet**: `code` and `joined` are the record. The edge's
  chained ledger moves with the launch hand-off.
- **Pages were checked by rendering `page()`**: `wrangler dev`'s workerd couldn't reach
  a local fake hub from the sandbox, so the server views were looked at in Chrome from a
  node server with the same assets.

## Not done yet, deliberately (M3)

- Going live: the tunnel, Access, `edge=` in `CYGNET_BOTS` and the Worker's secrets
  (`docs/edge.md`, The tunnel), then a real link end to end.
- The pixel unfurl for a link (SPEC experiment 5) and earning links by rules (M5): the
  allowance is a number an admin sets.
- Erasure for `joined`: the notice says to write in; there's no button.
- The tree past one card: the first lines are shown, then a count.

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
- **The birds' refusals are cygnet's to fix.** `hub.Serve` lets a module answer a call
  to cygnet herself, off the link's reader and inside the call's deadline;
  `cygnet.missing` goes to `provision.Missing`, through an atomic pointer because the
  hub listens before the modules exist. cygnet reads the bird's roles and the channel,
  runs `perms.Lacks`, and grants on the bird's managed role, or on the channel for that
  role when the server would allow it, writing the overwrite whole so it keeps what it
  had. watch reports in the layout's staff channel, propose adds a grant button that
  works it out again when pressed, act grants and says so. One refusal is reported once
  in ten minutes. Permissions cygnet lacks herself are a grant to make, never a reason
  to design around (she runs these servers).
- **The fleet card ticks itself off.** With the server members intent identified (an
  optional `Want`, so cygnet runs without it), a fleet card opened from the panel is
  kept with its interaction's token for 14 minutes and redrawn when a bot joins the
  server or a role there changes, which is how a re-authorised bird's permissions land.
  A burst of events settles for a second into one redraw. Without the intent the card
  says so, with a link to cygnet's bot page in the portal: **switch Server Members
  Intent on there** and cygnet restarts onto it within ten minutes.
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
