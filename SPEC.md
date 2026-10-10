# cygnet spec

cygnet is the fleet's coordinator: a private Discord bot that every other bot (skua,
merlin, peregrine) connects to so they can call each other, and that provisions
Discord servers for them. It is built from skua's spine and keeps skua's habits:
bleeding-edge Go, every performance claim measured, safety that costs nothing
measurable. It adds one more: **nothing leaves that we did not choose to send.**

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
   token. Logs carry IDs and counts, never message content or tokens. A bot learns
   only what is addressed to it.
7. **cygnet authenticates, the callee authorizes.** Every frame's `from` is stamped by
   cygnet from the connection's token. What a bot will do for whom is its own decision.

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
| 1 | Hub frame encoding: JSON against a fixed binary header with a raw body, round trip and allocations per call between two `link` clients | open |
| 2 | Hub call latency floor on one host: WebSocket against a Unix socket on a shared volume | open |
| 3 | `permessage-deflate` off against on for hub frames (expected: off wins, frames are small) | open |

## Milestones

| # | Milestone | State |
|---|---|---|
| 0 | Scaffold from skua: core, guard, intents, obs, ratelimit, store, brand, `/status` `/perf` `/help`, CI, deploy, setup | done |
| 1 | `internal/hub` and the public `link` package; the fleet on `/status`; skua joins first | open |
| 2 | merlin and peregrine join through `link` (discordgo) on the shared `fleet` network | open |
| 3 | `/provision`: compiled-in layouts applied by diff, fleet install links from what each bot declares, new servers through a guild template (bot `POST /guilds` is deprecated; verify first) | open |
| 4 | Invite codes: `/invite` mints codes, the edge Worker redeems one for a fresh single-use Discord invite minted by cygnet, every join traced to a code | open |
| 5 | Signups: an edge page per upcoming launch, Discord OAuth2 and Turnstile, interest counted in D1; at launch cygnet adds members straight in or sends them a code | open |
