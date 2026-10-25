# Handoff: Milestone 4 deployed, its last checks open

Written 2026-10-13. Read this with `CLAUDE.md` and `SPEC.md`.

## Where the branches are

| Branch | State |
|---|---|
| `main` | M0 to M4. M4 is #41 to #47, merged 2026-10-13; cygnet deploys from CI, the Worker by hand |
| `feat/support` | M6, supporters: backing the birds in crypto (docs/support.md). Open as a PR |

Other worktrees under `cygnet.worktrees/` are from merged work and can go.

## M6: supporters (feat/support)

What it adds: `/support` at cygnet.lol (the owner's letter, a Discord sign in, the
choices, a checkout that checks itself), the `support` module (the hub verbs, the
chains read every minute, `/support` with the keeper's takings and **give backing**),
`internal/chain` (the rails and their readers), `store.Support` (migration 006) and
`tools/supportart` (the badge and marks). Launch names that are the site's own pages
(`support`, `privacy`, `admin`, `auth`, `bird`) are refused on both sides now.

Decisions worth keeping:

- **Matched by exact amount**, not by a reference: most rails carry none, and one typed
  into a wallet is a step people skip. A unique index over live checkouts makes the
  amount name its checkout. A late payment still counts for 48 hours.
- **Read from now, never from history**: each watched account's cursor starts at the
  present, and a Solana token account found later starts from when its rail did.
- **Base's own public node refuses log reads** (429), so the default is publicnode's;
  docs/support.md says to set keyed nodes for real use. Checked live with
  `CYGNET_LIVE_CHAINS=1`.
- **The page is the owner's voice**, punctuation and all, held by its own test; the
  rest of the site stays cygnet's.

Not done yet, deliberately:

- **The birds' side.** merlin gives the supporter role from `cygnet.supporters` and
  shows the card, with the reminder before it runs out; peregrine mirrors the card.
  The page promises the role, so it isn't announced before merlin's PR lands.
- **The Melting Pot's tip jar** pointing at `/support`: merlin's PR too.
- **Going live**: wallets, keyed nodes and a small real payment per rail
  (docs/support.md, Going live).

## Next: M4's last checks

M4 is deployed and was run for real on 2026-10-13. silcord moved over: the two people
on its old list were put on cygnet's by `cygnet.join` and their places set to the old
order (one hand transaction on `line.place`, which the ledger doesn't hold), so the
line stays first come first served. Then the launch was opened: a hold lifted, a
release of 3 each, and wave 1 admitted all three. One of them had sealed a sign in,
and cygnet refreshed it with the app's secret (reset that day, set on both sides),
added them, and the join was traced to wave 1. Their page now says they're in.

Still open:

1. **An invite made and taken** on silcord, from a second account, to see `vouch`
   and the invitee following its giver into a wave live.
2. **skua.pace switched on** in silcord (skua's `/help`, pace). Until then the valve
   is told `denied` and each wave's reason says skua didn't say. skua serves it and
   lists it in its manifest (`Serves`, link v0.8.0).
3. **Experiment 6 against a recorded launch**, and experiment 2.

## What M4 changed

- **Postgres owns launches** (`internal/store/launch.go`, `wave.go`, migration 005). A
  launch's line (place, hold, invites released, a sealed sign in, the wave that let
  them in), its invites (codes with a `launch` column, so a spender stays pending until
  their wave and `Arrive` attributes them), its waves, and a ledger: every change and
  its entry in one transaction under the launch's chain, each entry's HMAC keyed over
  the one before, a trigger refusing update and delete until the list is gone.
  `Verify` walks it on every `/launch`.
- **Pseudonyms, not new column types.** Someone who leaves or is forgotten becomes a
  negative ID derived from the ledger's keyed name for them (`store.Pseudonym`), so
  `code`, `line` and `joined` keep their bigints and the tree keeps its shape.
- **The Worker asks** (`edge/src/index.ts`): `cygnet.join`, `place`, `issue`, `revoke`,
  `vouch`, `leave`, `mine`, `list`, `lift`, `seal`, `admit`, `forget`; `cygnet.code`
  says whether a code is a launch's. D1 keeps `signup` (the hashes), `challenge`,
  `sends`, `bird`, and `launch` as a read model cygnet pushes (now with stage and
  opens). The admin bearer covers only those pushes.
- **Sealed sign ins**: HPKE (RFC 9180 base mode, X25519, HKDF-SHA256, AES-128-GCM),
  written on WebCrypto in the Worker and held to the RFC's vectors, opened with Go's
  `crypto/hpke`; a Go test opens a token the Worker sealed.
- **The valve** (`internal/modules/launch/valve.go`): the rule is `next`, every wave
  keeps its why, the panel sets the ceiling and pauses it. Sealed people are added a
  ceiling's worth a minute; anyone else's way in is on their page. Both go through
  `invite.Let`, wired in `main` as the launch's `Door`. A join traced to no code but a
  wave is recorded with `joined.wave`. The launch module requires Server Members.
- **Forget me everywhere** on `/privacy`: off every list, every link and join
  pseudonymised.

Decisions worth keeping:

- **The line is cygnet's**, not the Worker's (decided 2026-10-13): the valve picks
  waves from it, so every input to a wave is in one store.
- **Waves double when calm** (decided): read as the last wave's *peak* joins a minute,
  and a wave counts as settled once its rush has passed (joins under half the ceiling
  a window after it opened), or 9 in 10 are in, or 15 minutes. Experiment 6 is why: read
  as the trailing rate, the rule peaked at 28 a minute against a ceiling of 10.
- **The Worker makes invite codes**; cygnet keeps only their hashes, as with `/invite`.
- **Revoking either OAuth token ends the grant**, so a sealed sign in's grant isn't
  revoked at sign up; every refusal along the way revokes it, and cygnet gives it back
  after using it.

## Not done yet, deliberately (M4)

- **`skua.pace`**: the valve asks it and words the answer's absence; skua doesn't answer
  it yet. A skua PR: messages a minute now and as usual, from its purge index, opt in
  per server like `skua.check`.
- **Experiment 6 against a recorded launch**: the replay uses a modelled arrival curve.
  Record silcord's opening and replay it.
- **Experiment 2** (redemption round trip, p50 and p99 from the Worker): still open.
- **Sponsor standing** and the audit log read: M5, with earning (decided).
- A hold isn't carried across the D1 move for the 2 people on silcord's list.

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
- **cygnet's own refusals** (after M3's first live link, refused for want of Ban
  Members): her REST clients run under `perms.Watch`, and a refusal goes to provision as
  the bird `cygnet`. A channel that takes a permission from her is fixed on the channel
  by the drift mode; one she lacks server wide is a `#cygnet` card with her install
  link, asking for everything her modules and the layout need. A redemption refused so
  tells its person the server isn't ready yet (`unready`). `link/perms` gained
  `member.add` and `invite.create`, and the ban read.
- **The tree**: on an admin's `/invite` panel, one line a sponsor ("brought ..."), the
  untraced on their own line, leavers struck through, cut at a card's room.

Decisions worth keeping:

- **SPEC experiment 4 is answered**: Discord restricts an invite to chosen users itself,
  so the fallback needs no binding of cygnet's and its join is exact.
- **The launch list's D1 invites stay where they are** until M4's hand-off moves them to
  Postgres (the "Postgres owns membership" decision); nothing new was put in D1.
- **No ledger table in Postgres yet**: `code` and `joined` are the record. The edge's
  chained ledger moves with the launch hand-off.
- **Intents, like permissions, are taken, not designed around**: cygnet identifies
  with every intent a feature needs, within reason (Server Members is on in
  production). New work declares what it needs and builds no "without it" path. Message
  Content stays off: cygnet reads no messages (SPEC principle 7).
- **A manual restart on foundry loads `deployed-tag.env` first**, or compose runs the
  stale `:latest`:
  `set -a && . ./deployed-tag.env && set +a && docker compose -f docker-compose.prod.yml up -d`.
- **Pages were checked by rendering `page()`**: `wrangler dev`'s workerd couldn't reach
  a local fake hub from the sandbox, so the server views were looked at in Chrome from a
  node server with the same assets.

Closed at the end (#40): SPEC experiment 3 measured (1000 racing spends, one winner;
a spend's `UPDATE` and commit about 1.6 ms), and the tree goes on over pages, a sponsor
who brought many over several lines.

## Not done yet, deliberately (M3)

- An admin's view of other members' links: not in the spec; the tree shows who brought
  whom, a member sees their own links open and used, an admin all of their own.
- The pixel unfurl for a link: SPEC says it goes out as a skua experiment first
  (experiment 5) and moves to cygnet when the numbers say so.
- Earning links by rules (M5): the allowance is a number an admin sets.
- Erasure for `joined`: done in M4 (forget me everywhere).

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

## Not done yet, deliberately

- More layouts than `flock`, and a way to choose channel positions.
- M5 in `SPEC.md`.

## From M0 and M1, still true

- **Copied, not imported.** skua's packages are `internal/`; a fix to a shared
  package goes to each repo on its own.
- **Private by default.** `/help` has no invite or source button, `tools/setup` warns
  if the app is public, and the prod compose publishes nothing. The hub listens only
  on the `fleet` network.
- Callee-side idempotency is each bird's job; `link` gives `NewID`/`CallID`.
- The voice lines in `internal/brand/voice.go` are still skua's.
- The installed `golangci-lint` predates Go 1.27's export data and can't typecheck;
  `go run ...golangci-lint@v2.14.0 run ./...` (CLAUDE.md) is CI's exact one.
