# Handoff: Milestone 1 (the hub)

Written 2026-10-10. Read this with `CLAUDE.md` and `SPEC.md`.

## Where the branches are

| Branch | State |
|---|---|
| `main` | Milestone 0 and the swan art. Public at `6586x57890143/cygnet`. |

## What M1 did

`internal/hub` and the `link` client: tokens by hash, hello and version refusal,
calls routed by method with deadlines, in-flight caps, at-most-once and goaway, events
only to subscribers and stamped with the sender, `cygnet.ping` and `cygnet.fleet`, and
`POST /v1/call`. The hub starts when `CYGNET_BOTS` is set and listens on the external
`fleet` Docker network as `cygnet:7700`, never published. `/status` shows the fleet.

Not done in M1, deliberately:

- skua is not linked yet: that is a skua PR (`link.New`, `Run`, a `ping` handler).
  It needs `link` tagged (`link/v0.1.0`) so skua can require it.
- Callee-side idempotency (keeping recent IDs) is each bird's job; `link` gives
  `NewID`/`CallID` and leaves state to the callee.
- A slow callee holds up the caller's read loop for up to 5s a frame (write timeout).

## What M0 did

Imported skua's spine at `fdcf388` and cut it down to what a coordinator needs:
`core`, `guard`, `intents`, `obs`, `ratelimit`, `store`, `brand`, the `status`,
`perf` and `help` modules, `tools/setup`, `tools/sprites`, CI and deploy. Everything
named skua is cygnet, env vars included (`CYGNET_*`).

Decisions worth keeping:

- **Copied, not imported.** skua's packages are `internal/`, and the two bots will
  drift. A fix to a shared package goes to each repo on its own.
- **Private by default.** `/help` has no invite or source button, `tools/setup` prints
  Discord's own install link and warns if the app is public, and the prod compose has
  no tunnel or extra services. skua.lol's redirect Worker was not carried over.
- **Guard ops cut to the generic ones** (send, delete, channel and member edits,
  command sync, emoji). Provisioning adds its own when it lands.
- **The plan changed once already.** The public pages (invite redemption, signups)
  were going to be served by cygnet behind a tunnel; they are a Cloudflare Worker with
  D1 and Turnstile instead, so foundry opens no port and the Worker never holds the
  Discord token (SPEC principle 6).

## Not done yet, deliberately

- No GitHub secrets or variables yet, so the deploy skips (`DEPLOY_ENABLED` unset)
  until `go run ./tools/setup` is run with cygnet's token.
- The voice lines in `internal/brand/voice.go` are still skua's; cygnet needs her own.
- The hub, provisioner, invite codes and signups: milestones 1-5 in `SPEC.md`.
- Not run against Discord: needs cygnet's own application and token (`tools/setup`).
