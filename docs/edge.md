# The edge Worker

`edge/` is `cygnet.lol`: a launch's page (`/silcord`), its sign in with Discord and the
list it keeps in D1. It holds no bot token and opens nothing on foundry.

## How someone gets in line

1. `count me in` passes Turnstile (invisible unless it has doubts), then goes to Discord
   with a state only that browser holds (`cy_state`, 10 minutes).
2. Discord sends them back to `/auth/discord` with `identify email`. The Worker reads
   who they are and revokes the token at once.
3. Someone already in line just gets their page. Anyone else needs a verified email on
   their Discord account and an account at least `minAgeDays` old (30).
4. They get the next place. Their email (lowercased, `+tag` dropped, Gmail dots dropped),
   connection (IPv4 address or IPv6 /64), network (/24 or /48) and area (country and a
   half degree square) are stored as HMACs under `HMAC_KEY`, never the values.
5. The same account or the same email gets no second place. The same connection, or
   the same network in the same area, as someone already in line still gets a place,
   with its invites held (`held` says why) for an admin to look at.
6. A signed cookie (`cy_silcord`, 30 days) shows them their place after that, and
   `leave the line` deletes their row.

A daily cron forgets connection, network and area hashes 30 days after each sign up,
and a launch's email hashes 30 days after it opens (`opens` in `src/signup.ts`).

## Commands

```sh
cd edge
npm ci
npm run check        # wrangler types, then tsc
npm test             # the rules, and the SQL against SQLite
npx wrangler d1 migrations apply cygnet-edge --local && npm run dev
```

For `npm run dev`, put dev values in `edge/.dev.vars` (gitignored). Turnstile's test
keys always pass: site key `1x00000000000000000000AA`, secret
`1x0000000000000000000000000000000AA`.

## Going live

On the Cloudflare account that holds `cygnet.lol`:

1. `npx wrangler d1 create cygnet-edge`, and put its `database_id` in `wrangler.jsonc`.
2. A Turnstile widget for `cygnet.lol` (managed); its site key goes in `wrangler.jsonc`
   as `TURNSTILE_SITEKEY`.
3. In the Discord developer portal, cygnet's app, OAuth2: add the redirect
   `https://cygnet.lol/auth/discord`. Its application ID goes in `wrangler.jsonc` as
   `DISCORD_CLIENT_ID`.
4. The secrets, each typed in, never in a file:
   `npx wrangler secret put DISCORD_CLIENT_SECRET`, `TURNSTILE_SECRET`, and `HMAC_KEY`
   (`openssl rand -base64 32`). Changing `HMAC_KEY` later makes every stored hash
   unmatchable, so set it once.
5. `npm run deploy` applies the migrations and deploys to the `cygnet.lol` custom domain.
