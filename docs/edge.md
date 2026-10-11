# The edge Worker

`edge/` is `cygnet.lol`: a launch's page (`/silcord`), its sign in with Discord, the
invites its members give, and the list and ledger it keeps in D1. It holds no bot token,
opens nothing on foundry and never sends email.

## Getting on the list

1. `count me in` passes Turnstile (invisible unless it has doubts), then goes to Discord
   with a state only that browser holds (`cy_state`, 10 minutes).
2. Discord sends them back to `/auth/discord` with `identify email`. The Worker reads who
   they are and revokes the token at once.
3. They need a verified email on their Discord account, and an account at least
   `minAgeDays` (30) old. One place per account and per email (lowercased, `+tag`
   dropped, Gmail dots dropped).
4. Their email, connection (IPv4 address or IPv6 /64), network (/24 or /48) and area
   (country and a half degree square) are stored as HMACs under `HMAC_KEY`. A second
   person on the same connection, or the same network in the same area, gets a place
   with their invites held for an admin (`held` says why).
5. A signed cookie (`cy_silcord`, 30 days) shows them their place after that.

## Invites, and the tree they make

- **Released, not reserved.** An admin releases a contingent to the front of the line:
  `POST /admin/silcord/release {"count": 100, "invites": 3}` gives 3 each to the first
  100 people in line who have none and aren't held.
- **Issued with a proof.** `make an invite` takes a fresh Discord sign in (the same
  account, the same email as when they joined) and a proof of the email (below). Only
  then is a code made: 20 random bytes, shown once as `cygnet.lol/i/<code>`, stored as
  its SHA-256, good for 48 hours. An invite nobody took is back with its issuer when
  it runs out, or when they take it back.
- **Taken with a proof.** The person it's for signs in with Discord (verified email,
  30 days old, not on the list, not the issuer) and proves their email. Spending the
  invite and putting them on the list under its issuer are one D1 batch, so an invite
  is spent once, and only by someone who got in.
- **The tree.** Every member who came in through an invite has a `sponsor`. Leaving
  takes back a member's unspent invites but keeps whoever they brought in under them:
  the tree is the record of who vouched for whom. `GET /admin/silcord/tree` reads it.

## Proving an email without sending one

The person sends any email from their Discord email to a one time address shown on the
page (`v-<26 random characters>@cygnet.lol`, 30 minutes). Email Routing hands it to
the Worker's `email` handler, which:

1. checks DKIM itself (`src/dkim.ts`): one `From`, a signature from that address's own
   domain over `From` and the whole body (`l=` refused), verified with the key the
   domain publishes, looked up over Cloudflare's DNS over HTTPS. Cloudflare's own
   inbound check accepts SPF or DKIM for any domain, which doesn't prove the `From`;
2. compares the keyed hash of the address with the one the proof waits for;
3. marks it proven and drops the email. Nothing in it is kept.

The page refreshes itself and goes on once it's proven. Both the cookie's token and the
address's token are stored as SHA-256 digests, five proofs an hour per account.

## The ledger

Every join, release, lift, issue, revoke, redeem and leave is appended to `ledger`, in
the same batch as the change it records. Each entry's MAC is keyed over the one before
it, so an entry changed, dropped or inserted breaks the chain from there on, and
triggers refuse any `UPDATE` or `DELETE`. `GET /admin/verify` walks the chain, then
checks that each member's released invites and each issuer's invites match what the
ledger says happened, so a grant made around the code shows up too. Actors and subjects
are keyed hashes of Discord IDs.

## What is kept, and for how long

| | Kept as | Until |
|---|---|---|
| Discord ID, place, sponsor | plain | they leave (as a sponsor, in their invitees' rows) |
| email | HMAC | 30 days after the launch opens |
| connection, network, area | HMAC | 30 days after they joined |
| invite codes, proof tokens | SHA-256 | invites stay as the tree's record; proofs go with the daily cron |
| the email that proves an address | not kept | read, checked, dropped |

Worker logs are off (`observability`): a URL can hold an invite code.

## Commands

```sh
cd edge
npm ci
npm run check        # wrangler types, then tsc
npm test             # rules, store, ledger tampering and DKIM, against SQLite
npx wrangler d1 migrations apply cygnet-edge --local && npm run dev
```

For `npm run dev`, put dev values in `edge/.dev.vars` (gitignored). Turnstile's test
keys always pass: site key `1x00000000000000000000AA`, secret
`1x0000000000000000000000000000000AA`.

## Going live

On the Cloudflare account that holds `cygnet.lol` (everything here is on the free plan):

1. `npx wrangler d1 create cygnet-edge`, and put its `database_id` in `wrangler.jsonc`.
2. A Turnstile widget for `cygnet.lol` (managed); its site key goes in `wrangler.jsonc`
   as `TURNSTILE_SITEKEY`.
3. Email Routing on `cygnet.lol`, with the catch-all rule sending to the
   `cygnet-edge` Worker.
4. In the Discord developer portal, cygnet's app, OAuth2: add the redirect
   `https://cygnet.lol/auth/discord`. Its application ID goes in `wrangler.jsonc` as
   `DISCORD_CLIENT_ID`.
5. The secrets, each typed in, never in a file: `npx wrangler secret put` for
   `DISCORD_CLIENT_SECRET`, `TURNSTILE_SECRET`, `HMAC_KEY` and `ADMIN_TOKEN` (both
   `openssl rand -base64 32`). Changing `HMAC_KEY` later makes every stored hash and the
   ledger's chain unverifiable, so set it once.
6. `npm run deploy` applies the migrations and deploys to the `cygnet.lol` custom
   domain.
7. Before announcing it: one real proof each from Gmail, Outlook and a custom domain,
   since DKIM is checked here and only this proves it reads their signatures.
