# CLAUDE.md

## What this is

`cygnet` is the coordinator of the Discord bot fleet (skua, merlin, peregrine): a
private bot that the others connect to so they can call each other, and that
provisions Discord servers for them (layouts, fleet installs, invite codes, signups).
It is built from skua's spine at skua `fdcf388`: same layout, same rules, same voice.
Read [`SPEC.md`](./SPEC.md) first, then [`HANDOFF.md`](./HANDOFF.md). Anything a person
sees follows [`DESIGN.md`](./DESIGN.md), the guide every bird shares, and [`UX.md`](./UX.md),
what is cygnet's own. A lesson about how a surface reads goes in DESIGN.md's log.

- `cmd/cygnet/main.go`: wiring only.
- `internal/core`, `guard`, `intents`, `obs`, `ratelimit`, `store`, `brand`: copied from
  skua and owned here now. A fix that applies to both goes to each repo on its own.
- `internal/hub`: the bird protocol's server (SPEC.md, Protocol). `link/` is its client and
  the frame types, a module of its own (`github.com/6586x57890143/cygnet/link`) so a bird
  imports it without cygnet's dependencies; cygnet uses it through a `replace`. A change
  to the protocol changes both, and is tagged `link/vX.Y.Z` for the birds.
- `ledger/`: the log every bird keeps, a module of its own on disgo alone
  (`github.com/6586x57890143/cygnet/ledger`, tagged `ledger/vX.Y.Z`). Typed entries,
  the shared voice check, the sha256 chain, the log card and the batcher that folds a
  run of one action into one live card. Shared code, not a service: each bird stores its
  own entries (`ledger.Store`, held to `ledgertest.Run`) and posts its own cards, so
  logging never waits on the hub. Not imported by anything in `link`.
- `support/`: the supporter cards every bird posts, a module of its own on disgo alone
  (`github.com/6586x57890143/cygnet/support`, tagged `support/vX.Y.Z`): the advert that
  sends people to cygnet.lol/support and the welcome for a new supporter. It returns a
  `Page` (head, body parts, footer, whether to lead with a banner) and the bird lays it
  out in its own panel with its own art; icons and the owner's mention come in through
  a `Brand`. It names no bird, so any of them can import it.
- `internal/modules/*`: `status` (the reference), `perf`, `help`, `provision`, `launch`
  (the campaign, the Worker's verbs, sealed sign ins, the valve), `invite` (codes: the
  panel, redemption for cygnet.lol, attribution, the tree), `support` (backing the birds:
  checkouts for cygnet.lol, the chains read for their payments, who backs the birds;
  docs/support.md). `launch` lets a wave in
  through `invite.Let`, wired in `main.go`: modules still never import each other.
- `internal/chain`: the public chain reads `support` watches, and its rails' amounts.
- `tools/supportart`: the supporter's badge and marks, written to `edge/public/art/support`.
- `tools/sprites`: the art. `art/source/cygnet_avatar_source.png` is the drawing; the
  generator keeps its disc, quantises it to its own palette and computes the banner.

Adding a module is as in skua: implement `core.Module`, declare intents honestly in
`Want`, channel permissions in `Perms`, a `/help` page, a glyph in
`tools/sprites/icons.go` (rerun the generator), and append it to `all` in `main.go`.
Writes to Discord go through `guard.Allow`/`Report`; add an `Op` with its cap when a
module needs a new kind of write. CI fails any `internal/` package under 85% coverage.

**Privacy is a design rule here, not a review step** (SPEC principle 6): no public
listener but the edge Worker, no public invite or source link, no content or tokens
in logs, and a bot is told only what is addressed to it.

## Commands

```sh
go build ./... && go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...   # CI's exact linter
scripts/coverage.sh                    # race tests + 85% floor per internal/ package
scripts/prose.sh                       # no em dashes, ellipses or curly quotes
go -C link vet ./... && go -C link test -race ./...   # the client module
go -C ledger vet ./... && go -C ledger test -race ./...   # the log module
go -C support vet ./... && go -C support test -race ./... # the supporter cards module
go -C ledger test ./... -update -run TestRenderedCards     # rewrite the golden cards after a deliberate change
go test ./internal/hub -run x -bench Call             # a call's round trip through the hub
go run ./tools/hubbench                               # live: cygnet.ping through the running hub, as bird `bench`
govulncheck ./...
go run ./tools/sprites                 # regenerate internal/brand/assets and emoji
go run ./tools/sealkey                 # CYGNET_SEAL_KEY for cygnet, CYGNET_SEAL_PUBLIC for the Worker

cp .env.example .env && docker compose up --build   # local bot + Postgres
go run ./cmd/cygnet                                 # native, DB optional
go run ./tools/setup                                # go live / rotate the token
scripts/deploy.sh                                   # manual deploy of HEAD to foundry
npm -C edge ci && npm -C edge run check && npm -C edge test   # the Worker at cygnet.lol (docs/edge.md)
```

`store` reaches its coverage floor only with `CYGNET_TEST_DATABASE_URL` set, as in CI.

## Working in parallel

- **Never work in the main checkout.** `.claude/settings.json` denies edits there. Each
  branch gets a worktree: `git worktree add -b <branch> ../cygnet.worktrees/<name> main`.
  Use absolute paths in shell commands: a relative one can resolve against the main
  checkout and hit the deny rule.
- Public repo `6586x57890143/cygnet`: privacy by design, not obscurity. Keep docs sparse
  and free of personal details. `main` takes changes through PRs, squash merged, one
  change per PR, PR title reads as the commit (`feat(hub): ...`).
- One bot per token: check no other session runs cygnet before starting it live. Give
  each local Postgres its own port with `CYGNET_PG_PORT`.
- Stage paths explicitly, never `git add -A`.
