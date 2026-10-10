# cygnet spec

cygnet is the fleet's coordinator: a private Discord bot that every other bot (skua,
merlin, peregrine) connects to so they can call each other, and that provisions
Discord servers for them. It is built from skua's spine and keeps skua's habits:
bleeding-edge Go, every performance claim measured, safety that costs nothing
measurable. It adds one more: **nothing leaves that we did not choose to send.**

The flock, each lean and kept to its own job:

| Bird | Job |
|---|---|
| peregrine | engagement |
| merlin | general purpose moderation, highly modular |
| skua | the test bed: experiments, UX, the newer and less general modules |
| cygnet | the backend: links the others, provisions servers that fit all their permission schemes |

## Principles

1. **One binary, modules by package.** As skua: a module implements `core.Module` and is
   listed in `cmd/cygnet/main.go`. Modules never import each other.
2. **Ask Discord, don't assume.** Intents from the portal's flags, `wants ∩ granted`;
   install settings written from the running modules' `Perms`.
3. **Fail closed, cheaply.** A command with no tier fails boot. `internal/guard` caps
   every Discord write per guild and op, with a per-guild breaker.
4. **Nothing pings.** Every send carries `core.NoPings()`.
5. **Smallest thing that works.** No table, cache, job or option until something needs
   it; shortcuts carry a `ponytail:` comment naming their ceiling.
6. **Private by default.** The app is not public and advertises nothing: no invite
   button, no source link. The hub listens only on a private network and never on a
   public port. The one public surface (invite codes, signups) is a Cloudflare Worker,
   so foundry opens no port and the origin is never named. It never holds the Discord
   token. The hub never logs or keeps a frame's body: it routes and forgets.
   Logs carry IDs and counts, never message content or tokens. A bot learns
   only what is addressed to it.
7. **cygnet authenticates, the callee authorizes.** Every frame's `from` is stamped by
   cygnet from the connection's token. What a bot will do for whom is its own decision.
8. **Rules, not a model.** cygnet decides with deterministic policies whose every
   outcome can be explained from its inputs: leases with a TTL, budgets, breakers,
   health from heartbeats and measured load. No LLM is in any decision path. Smart
   means it reads the fleet's state and acts on it, not that it guesses.

## Protocol

The contract every bird speaks, the same over the WebSocket and `POST /v1/call`.

- **Versioned and negotiated.** A link opens with `hello`: protocol version, the bot's
  build, the methods it serves, the events it emits and its limits. cygnet answers with
  what it accepted. An unknown frame kind or a version outside the range is refused at
  the door, never half understood.
- **Typed frames.** `{v, id, kind, from, to, method, body, err, deadline, trace}`. Kinds:
  `call`, `reply`, `event`, `lease`, `goaway`. Methods are `<bot>.<verb>`, bodies are
  JSON objects with a published shape, errors carry a code (`offline`, `timeout`,
  `denied`, `busy`, `invalid`, `internal`) and a short reason, never a stack.
- **Deadlines travel.** Every call carries an absolute deadline; past it cygnet drops the
  frame unsent. No call waits forever, and a reply after the deadline is discarded.
- **At most once, retried on purpose.** cygnet never redelivers. A caller that retries
  sends the same `id`, and a callee that changes state keeps recent ids so a retry is a
  no-op.
- **Backpressure.** Each link has an in-flight cap from its `hello`; over it, a call is
  refused `busy` at once rather than queued.
- **Draining.** `goaway` before a deploy: the bot stops taking calls, finishes what it
  holds and closes, and its leases go to whoever can take them.
- **Bots never talk to each other through Discord.** Interop is the hub's job; a bird
  ignores other bots' messages, so no two of them can loop in a channel.

## Coordination

Shared state lives in cygnet and is handed out as **leases**: a named duty or resource,
one holder, a TTL renewed by heartbeat. A holder that stops renewing loses it, and
cygnet grants it to the next bot that declared it can take it. Duties (one bot posts a
server's welcome, one runs a nightly job) and shards are both leases.

**Sharding.** Each bird is its own Discord application, so its gateway limits are its own.
cygnet holds each one's `GET /gateway/bot` answer and coordinates its processes:

- assigns shard ranges to the bot's processes as leases, sized from Discord's
  recommended count and each process's measured guild load, never more than 2500
  guilds a shard;
- grants every IDENTIFY as a slot in bucket `shard_id % max_concurrency`, one per bucket
  per 5 seconds, so no process starts a session on its own;
- guards the 1000 identifies a day: past a reserve, slots go to RESUME first and a
  process in a crash loop is held off instead of identifying, since running out resets
  the token;
- moves a shard only through drain then grant, so no shard runs twice or not at all.

A bot with one process and one shard needs none of this and asks for nothing.

## Stack

| | |
|---|---|
| Discord | `disgoorg/disgo` v0.19, zstd-stream gateway, every cache off except guilds |
| Database | Postgres 16 via `pgx/v5`, optional; migrations compiled in |
| Hub | WebSocket on the private `fleet` Docker network. An off-host bot comes in through a Cloudflare Tunnel whose hostname answers only to an Access service token; foundry opens no port |
| Edge | `cygnet.lol`: a Cloudflare Worker + D1 + Turnstile for invite codes and signups (milestones 4-5), the only public surface. Worker to cygnet goes through the same tunnel and Access, never the open internet |
| Build | Go 1.27, `CGO_ENABLED=0 -trimpath -s -w`, distroless nonroot, PGO when present |
| Deploy | as skua: CI image to GHCR, foundry pulls by SHA; `scripts/deploy.sh` by hand |

## Experiments

| # | Experiment | State |
|---|---|---|
| 1 | Hub frame encoding: JSON against a fixed binary header with a raw body, round trip and allocations per call between two `link` clients | open. JSON baseline (`go test ./internal/hub -bench Call`): 97 us and 122 allocs a call and reply, two WebSocket hops on Windows loopback |
| 2 | Hub call latency floor on one host: WebSocket against a Unix socket on a shared volume | open |
| 3 | `permessage-deflate` off against on for hub frames (expected: off wins, frames are small) | open |

## Milestones

| # | Milestone | State |
|---|---|---|
| 0 | Scaffold from skua: core, guard, intents, obs, ratelimit, store, brand, `/status` `/perf` `/help`, CI, deploy, setup | done |
| 1 | `internal/hub` and the public `link` package (its own module): hello, calls, events, deadlines, backpressure, goaway; the fleet on `/status`; skua joins first | built and tested; skua not linked yet |
| 2 | merlin and peregrine join through `link` (discordgo) on the shared `fleet` network | open |
| 3 | `/provision`: compiled-in layouts applied by diff, fleet install links from what each bot declares, new servers through a guild template (bot `POST /guilds` is deprecated; verify first) | open |
| 4 | Invite codes: `/invite` mints codes, the edge Worker redeems one for a fresh single-use Discord invite minted by cygnet, every join traced to a code | open |
| 5 | Signups: an edge page per upcoming launch, Discord OAuth2 and Turnstile, interest counted in D1; at launch cygnet adds members straight in or sends them a code | open |
| 6 | Leases: duties and failover between birds, then sharding: shard leases, identify slots per bucket, the daily identify reserve, drain-then-grant moves | open |
