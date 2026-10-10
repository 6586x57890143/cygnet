# cygnet

The coordinator of a small Discord bot fleet, in Go. The other bots connect to it to
call each other, and it provisions Discord servers for them: layouts, installs,
tracked invite codes and signups. Built on [disgo](https://github.com/disgoorg/disgo)
and [pgx v5](https://github.com/jackc/pgx), from the same spine as skua.

[`SPEC.md`](SPEC.md) is the one-page design and the experiment log.

## Docs

- [Run](docs/run.md) it locally
- [Go live](docs/go-live.md): the token and the profile
- [Deploy](docs/deploy.md): what a merge to `main` does
