# cygnet spec

cygnet runs Discord servers for the flock. She builds a server to a layout and keeps it
there, decides who gets in and traces every join to its cause, and runs a launch from
the first person who shows interest to the last wave let in. The public sees one quiet
page at `cygnet.lol`; everything behind it is private, and everything she decides can
be explained from its inputs.

She is built from skua's spine and keeps skua's habits: bleeding-edge Go, every
performance claim measured, safety that costs nothing measurable. She adds one more:
**nothing leaves that we did not choose to send.**

| Bird | Job |
|---|---|
| cygnet | the server's keeper: layout, membership, invites, launches |
| skua | the test bed, and the flock's memory of who is active: answers cygnet's questions about members without handing over what it knows |
| merlin | moderation; joins the hub when a rule wants its answers |
| peregrine | engagement; joins the hub when a rule wants its answers |

## What a member sees

1. **Interest.** A launch has a page at `cygnet.lol/<launch>`: the server's art, one line
   about it, and *count me in*. Signing in with Discord (and a Turnstile check nobody
   has to click) puts them on the list and shows their place in it. Nothing else is
   asked for: Discord's verified email (the `email` scope) is what makes them one
   person, kept only as a keyed hash, with their connection and rough area hashed the
   same way to hold the invites of a second sign up from one household for an admin.
   Invites are released to the front of the line in contingents; each one a member
   gives is made only after they prove their email, and taken only by someone who
   proves theirs, so every member hangs from whoever vouched for them. An email is
   proven by a link sent to it and clicked once (`docs/edge.md`).
2. **A way in that is theirs.** When their wave opens, cygnet adds them to the server
   herself: no invite link exists for anyone to forward. Someone who would rather not
   grant that gets a Discord invite made for them alone: one use, gone in minutes.
3. **Bringing friends.** In the server, `/invite` hands a member a link of their own to
   give (`cygnet.lol/i/<code>`), one person per link. It unfurls in Discord as a pixel
   card with the bird and who it's from. They see who used each one, and can take back
   any that hasn't been.
4. **Earning more.** Each server sets rules for how members earn invites: time in the
   server, being active, holding a role, turning up to an event. `/invite` shows how
   close they are to the next one (`12 of 50 messages ▰▰▱▱▱`), so nobody wonders what
   counts.

`/invite` is also a user-installed command, so a member can make a link from any DM
or server without leaving the conversation they're in.

## What an admin sees

- **`/provision`.** A layout (roles, their order, categories, channels, permission
  overwrites, onboarding) applied by diff: cygnet shows what would change, then changes
  only that. For a new server, a guild template to start from, then a live install card
  with one button per bird that ticks itself off as each one joins, with exactly the
  permissions that bird's manifest asks for.
- **`/launch`.** The server's page at `cygnet.lol` and its campaign on one panel: the
  numbers, the stage, releases to the front of the line, and the valve's waves when
  they come.
- **Drift.** A role or overwrite changed by hand shows up as a card with the diff and a
  *put it back* button. Each server picks how far cygnet goes on her own: `watch`
  (tell), `propose` (the card, the default) or `act` (put it back and say so).
  A report lets an admin leave any of its differences alone: cygnet stops managing
  that aspect there until someone forgets it on the panel, so `act` never argues
  with a change made on purpose. A fresh start forgets them all.
- **The tree.** Every member's line back to the code, rule or wave that let them in. A
  sponsor whose invitees keep getting removed earns more slowly, by a rule anyone can
  read.
- **The valve.** A launch admits waves, sized by the server's measured load: messages
  a minute, joins a minute, and, once merlin is linked, its open cases. The next wave
  waits until the last one has settled.

## Principles

1. **One binary, modules by package.** As skua: a module implements `core.Module` and is
   listed in `cmd/cygnet/main.go`. Modules never import each other.
2. **Ask Discord, don't assume.** Intents from the portal's flags, `wants ∩ granted`;
   install settings written from the running modules' `Perms`.
3. **Fail closed, cheaply.** A command with no tier fails boot. `internal/guard` caps
   every Discord write per guild and op, with a per-guild breaker. A provision applies
   no change it did not first show.
4. **Nothing pings.** Every send carries `core.NoPings()`.
5. **Smallest thing that works.** No table, cache, job or option until something needs
   it; shortcuts carry a `ponytail:` comment naming their ceiling.
6. **Private by default.** The app is not public and advertises nothing. The hub listens
   only on the private `fleet` network. The one public surface is the edge Worker, so
   foundry opens no port and the origin is never named; the Worker never holds the bot
   token. Logs carry IDs and counts, never message content or tokens.
7. **cygnet reads no messages.** She identifies without message intents. Anything
   about what members say comes from a bird that already sees it, as an answer to a
   question, never as data.
8. **Questions, not data.** A bird answers `<bot>.check` with whether a member meets a
   rule and how far along they are (`{met, have, need}`), never the messages, counts
   by channel or history behind it. cygnet keeps the answer, not the inputs.
9. **Bearer secrets are kept as hashes.** Invite codes, hub tokens: cygnet stores
   their SHA-256 and compares in constant time. A member's Discord OAuth token is used
   once and dropped, unless they chose to be added at launch, and then it is sealed
   (below).
10. **Rules, not a model.** Every decision (who gets in, who earns an invite, how big a
    wave is) comes from a deterministic rule whose outcome can be explained from its
    inputs, and `/invite` and the tree show that explanation. No LLM is in any
    decision path.
11. **Every join has a cause.** A member who joined through cygnet is traced to a code,
    a rule or a wave. A join cygnet cannot trace is shown as such, never guessed.

## Membership

**Codes.** 20 random bytes, base32, shown once; cygnet stores the hash, the owner, the
launch or server and whether it is spent. Spending is one conditional `UPDATE` in
Postgres, so a code is spent once however many tabs race for it.

**Redemption.** The Worker serves the page and the Discord sign-in (`identify` and
`guilds.join`), then hands cygnet the code, the user's ID and their access token over
the tunnel. cygnet spends the code, checks the server's gate (not banned, account age,
whatever the layout asks) and adds the member with `PUT /guilds/{guild}/members/{user}`.
The token is dropped as soon as that returns. Without `guilds.join` she mints a Discord
invite instead (`max_uses: 1`, `max_age: 300`, `unique: true`), limited by Discord to
them alone (`target_users_file`), and the page sends them straight to it. Either way the
code is spent before anyone is let in, and someone holds one code into a server at a
time.

**Attribution.** A direct add is attributed when cygnet makes it. An invite join is the
member's own: only they can take the invite, so `GUILD_MEMBER_ADD` for them is the join,
traced to the code they spent. Joins missed while cygnet was away are found by a sweep
once she is back. A code whose owner has left the server is gone with them.

**Sealed until launch.** Someone who signs up early and ticks *add me when it opens*
leaves a refresh token that the Worker encrypts to cygnet's public key and cannot
decrypt itself. At their wave cygnet opens it, refreshes it, adds them and forgets it.
Anyone who changes their mind deletes it from the page. A list with no sealed tokens
works the same: the page tells them their way in is ready the next time they sign in.

**Earning.** A rule is a small expression the layout holds, checked when its inputs
change and swept daily:

```
earn 1 when tenure >= 14d and skua.active(30d) >= 50 and not merlin.actioned(30d)
hold at most 3
```

Inputs cygnet sees herself: tenure, roles, scheduled event attendance (the event's
voice channel). Inputs from a bird come from `<bot>.check`. A rule naming a bird that
isn't linked fails at load, not at night.

## Protocol

The contract every bird speaks, the same over the WebSocket and `POST /v1/call`.

- **Versioned and negotiated.** A link opens with `hello`: protocol version, the bot's
  build, the methods it serves, the events it emits and its limits. cygnet answers with
  what it accepted. An unknown frame kind or a version outside the range is refused at
  the door, never half understood.
- **Typed frames.** `{v, id, kind, from, to, method, body, err, deadline}`. Kinds:
  `hello`, `welcome`, `call`, `reply`, `event`, `goaway`. Methods are `<bot>.<verb>`,
  bodies are JSON objects with a published shape, errors carry a code (`offline`,
  `timeout`, `denied`, `busy`, `invalid`, `internal`) and a short reason, never a stack.
- **Deadlines travel.** Every call carries an absolute deadline; past it cygnet drops the
  frame unsent. No call waits forever, and a reply after the deadline is discarded.
- **At most once, retried on purpose.** cygnet never redelivers. A caller that retries
  sends the same `id`, and a callee that changes state keeps recent ids so a retry is a
  no-op.
- **Backpressure.** Each link has an in-flight cap from its `hello`; over it, a call is
  refused `busy` at once rather than queued.
- **Draining.** `goaway` before a deploy: the bot stops taking calls, finishes what it
  holds and closes.
- **Every bird has a manifest.** `<bot>.manifest` answers `link.Manifest`: build, the
  union of its running modules' permissions, its intents and each command with its tier.
  `/provision` reads these, never a copy.
- **Checks.** `<bot>.check {guild, user, rule}` answers `{met, have, need}`. A bird
  lists the rules it can answer in its manifest; cygnet asks only those.
- **One permission model.** `link/perms` is Discord's permissions as data, shared by
  every bird: the bits, what each operation needs (`message.send`, `member.timeout`...)
  and Discord's own algorithm for what a member may do in a channel. A bird Discord
  refuses for want of a permission calls `cygnet.missing {guild, channel, op}`, read from
  the request by `perms.Route` under `perms.Watch`, one wrapper on its HTTP client; cygnet
  works out from its roles what it lacks and grants it, by the server's drift mode, on
  the bird's role or, when only a channel takes it away, on that channel. cygnet's own
  client is watched the same way: what a channel takes from her she gives back herself,
  and what she lacks server wide is reported with her install link, since Discord lets
  only a person raise a bot's role. Each manifest
  says the intents a bird wants beside those it was granted, and the fleet card links
  a gap to that bird's portal.
- **cygnet authenticates, the callee authorizes.** Every frame's `from` is stamped by
  cygnet from the connection's token. What a bot will do for whom is its own decision.
- **A bird proves its build.** A token says who a bird is, not what it runs. Each bird
  has a release key that lives only in its CI's deploy job on `main`; CI signs the bird,
  commit and image (`link.Attestation`, `link/cmd/attest`) and the deploy hands it to
  the bird for its hello. cygnet pins each bird's public key (`CYGNET_BIRD_KEYS`) and
  refuses a pinned bird's link without a good attestation, so a fork or an altered
  build holding the token is turned away. Every frame carries the caller's `trust`,
  stamped like `from`: untrusted (token only), signed, or attested (signed, and cygnet
  saw the signed image running the link, on her own host). Once any key is pinned,
  cygnet grants permissions only to signed birds, and a callee may take trust into
  its decision. This proves what CI built, not what runs on a host cygnet can't see:
  the trust boundary is her host and each bird's CI secrets.
  A bird that would do more for an attested caller where cygnet keeps watch asks her
  `cygnet.here`: whether she is a member of that server, yes or no.
- **Bots never talk to each other through Discord.** A bird ignores other bots'
  messages, so no two of them can loop in a channel.

## skua and cygnet

skua is where a surface is tried before cygnet keeps it, and the one bird cygnet asks
about activity.

- **Activity checks.** skua's purge index already holds who wrote which message (IDs and
  authors, never content). `skua.check` answers `active(window) >= n` from it, so
  cygnet gets a yes, a no and a count, and no new copy of anything exists. A server
  opts in on skua's side; without it the check answers `denied`.
- **Tried on skua first.** The invite card, the pixel unfurl and the progress bars go
  out as a skua experiment in one server, are measured, and move to cygnet when the
  numbers say so. A surface cygnet ships has a row in skua's experiment log.
- **notify meets launches.** A launch page can carry a creator's live state from
  skua's notify (live, how many watching), and a code minted for that creator traces
  every join from their audience back to the stream.
- **Shared art, copied.** skua's stream pictures and cygnet's cards come from the same
  drawing rules (`UX.md`, Art). The code is copied, not imported, as with every shared
  package.

## Stack

| | |
|---|---|
| Discord | `disgoorg/disgo` v0.19, zstd-stream gateway, every cache off except guilds; intents `GUILDS`, `GUILD_MEMBERS`, `GUILD_INVITES`, `GUILD_SCHEDULED_EVENTS`, `GUILD_VOICE_STATES` |
| Database | Postgres 18 via `pgx/v5`; migrations compiled in. Required once membership lands |
| Hub | WebSocket on the private `fleet` Docker network. An off-host bot comes in through a Cloudflare Tunnel whose hostname answers only to an Access service token; foundry opens no port |
| Edge | `cygnet.lol`: a Cloudflare Worker with D1 (sign up hashes, email proofs, a read model of each launch's page) and Turnstile, OG card images drawn at the edge. cygnet's Postgres owns the lists; Worker to cygnet goes through the same tunnel and Access |
| Build | Go 1.27.2, `CGO_ENABLED=0 -trimpath -s -w`, distroless nonroot, PGO when present |
| Deploy | as skua: CI image to GHCR, foundry pulls by SHA; `scripts/deploy.sh` by hand |

## Experiments

| # | Experiment | State |
|---|---|---|
| 1 | Hub frame encoding: JSON against a fixed binary header with a raw body, round trip and allocations per call between two `link` clients | open. JSON baseline (`go test ./internal/hub -bench Call`): 97 us and 122 allocs a call and reply, two WebSocket hops on Windows loopback. Live on foundry (1 vCPU, `tools/hubbench`, one hop to the hub and back): p50 123 us, p99 854 us one at a time; 13k calls/s at 8 in flight, p50 617 us, queueing on the one core |
| 2 | Redemption round trip, sign-in return to member added, p50 and p99 from the Worker's clock: direct add against minted invite | open |
| 3 | Code spend under a race: 1000 concurrent redemptions of one code, exactly one wins, and the cost of the conditional `UPDATE` | done: 1000 concurrent spends of one code, one wins, all answered in 226 ms through a 12 connection pool (`TestSpendRaceHasOneWinner`). A winning spend, one conditional `UPDATE` and its commit, takes 1.6 ms; a losing one, which reads why, 1.8 ms (`go test ./internal/store -bench Spend`). Postgres 18 in Docker Desktop on Windows, where the commit's WAL flush is most of it |
| 4 | Invites restricted to chosen users by Discord itself, if the API offers it, against cygnet's own one-use binding | answered: the API restricts an invite to listed users (`target_users_file`, after a job cygnet waits on), so the fallback invite is theirs alone and attribution needs no binding of cygnet's |
| 5 | The unfurl: an OG card drawn in the Worker against one pre-drawn per code, time to first byte and Discord's cache behaviour | open |
| 6 | Wave sizing: the valve's rule replayed against a recorded launch, joins a minute held under a ceiling with no idle gap | rule built, replayed against a modelled launch (`replay_test.go`: 2000 in line, ceiling 10). Read as the trailing rate when a wave settled, joins doubled too eagerly and peaked at 28 a minute; read as the wave's peak, with a wave settled once its rush passed, everyone was in by minute 311, never past 8 a minute, waves at most 7 minutes apart. A recorded launch is still to replay |

## Milestones

| # | Milestone | State |
|---|---|---|
| 0 | Scaffold from skua: core, guard, intents, obs, ratelimit, store, brand, `/status` `/perf` `/help`, CI, deploy, setup | done |
| 1 | `internal/hub` and the public `link` module: hello, calls, events, deadlines, backpressure, goaway, manifests; the fleet on `/status`; skua linked | done; skua linked in production |
| 2 | `/provision`: compiled-in layouts applied by diff with a preview, the live install card from each bird's manifest, role order, onboarding; drift cards at `watch`, `propose` or `act` | done: the `/provision` panel, with the live fleet card, the layout preview (role colours, a confirmed fresh start, onboarding) and the drift mode, with drift reports |
| 3 | Codes: `/invite` (in the server and user-installed), the Worker's redemption page, direct add with the one-use invite fallback, attribution, the tree | done and live: `/invite` (in the server and from a DM) and its allowance, `cygnet.lol/i/<code>` through the tunnel past Access, the direct add and the targeted invite both used for real, attribution with a sweep, the tree on the admin's panel, and cygnet's own refusals fixed or reported |
| 4 | Launches: interest pages, places in line, sealed tokens, the valve and its waves | done and live: silcord opened, wave 1 let its line in and added a sealed sign in; the line, its invites, waves and chained ledger in Postgres, the Worker asking cygnet over the hub; sealed sign ins (HPKE); the valve's waves, a sealed person added by cygnet and anyone else's way in on their page; forget me everywhere |
| 5 | Earning: rules in the layout, `skua.check` for activity, progress on `/invite`, sponsor standing | open |
| 6 | Supporters: backing the birds in crypto at `support.cygnet.lol`, checkouts matched by exact amount, the chains watched, `cygnet.supporters` for the birds' roles (docs/support.md) | built; not live until a wallet is set, and the birds' roles are their own PRs |

Not planned: leases, failover between birds and sharding. The birds do different jobs
as different apps, so none can take over another's duty, and each runs as one
process. merlin and peregrine join the hub when a rule wants their answers, not before.
