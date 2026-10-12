# CLAUDE.md

## What this is

`cygnet` is the coordinator of the Discord bot fleet (skua, merlin, peregrine): a
private bot that the others connect to so they can call each other, and that
provisions Discord servers for them (layouts, fleet installs, invite codes, signups).
It is built from skua's spine at skua `fdcf388`: same layout, same rules, same voice.
Read [`SPEC.md`](./SPEC.md) first, then [`HANDOFF.md`](./HANDOFF.md). Anything a person
sees follows [`UX.md`](./UX.md).

- `cmd/cygnet/main.go`: wiring only.
- `internal/core`, `guard`, `intents`, `obs`, `ratelimit`, `store`, `brand`: copied from
  skua and owned here now. A fix that applies to both goes to each repo on its own.
- `internal/hub`: the bird protocol's server (SPEC.md, Protocol). `link/` is its client and
  the frame types, a module of its own (`github.com/6586x57890143/cygnet/link`) so a bird
  imports it without cygnet's dependencies; cygnet uses it through a `replace`. A change
  to the protocol changes both, and is tagged `link/vX.Y.Z` for the birds.
- `internal/modules/*`: `status` (the reference), `perf`, `help`, `provision`, `launch`.
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
golangci-lint run
scripts/coverage.sh                    # race tests + 85% floor per internal/ package
scripts/prose.sh                       # no em dashes, ellipses or curly quotes
go -C link vet ./... && go -C link test -race ./...   # the client module
go test ./internal/hub -run x -bench Call             # a call's round trip through the hub
go run ./tools/hubbench                               # live: cygnet.ping through the running hub, as bird `bench`
govulncheck ./...
go run ./tools/sprites                 # regenerate internal/brand/assets and emoji

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
