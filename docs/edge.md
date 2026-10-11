# The edge Worker

`edge/` is `cygnet.lol`: a launch's page (`/silcord`), its sign in with Discord, the
invites its members give, and the list and ledger it keeps in D1. It holds no bot token,
opens nothing on foundry, and emails only the one link a person asks for.

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

## Proving an email with a link

Making or taking an invite emails a link to the address on the person's Discord account
(`cygnet@cygnet.lol`, through Scaleway's Transactional Email in Paris). The address is used for that send and
never stored; the page shows it masked (`st***@gm***.com`) from a signed cookie.

- The link is `cygnet.lol/v/<32 random bytes>`, good once, for 30 minutes. Its token and
  the browser's cookie token are stored as SHA-256 digests.
- Opening it shows a page with one button that `app.js` presses, so a person clicks
  once while a mail scanner that only fetches links confirms nothing.
- It can be opened on any device. What it was for (minting the invite, or taking it)
  happens only in the browser that started it, which waits on `/silcord/wait` and moves
  on by itself.
- Five links an hour per Discord account, each behind Turnstile and a fresh Discord
  sign in.

**What it costs.** Scaleway sends 300 emails a month free, then €0.25 per 1,000, with no
plan to buy. Every send first takes a slot in `sends`, one atomic statement: no more
than 2,900 go out in any 31 days, so the worst month costs about €0.65 whatever anyone
tries. Past it the page says so and nothing is sent. Everything else (Workers, D1,
Turnstile, static assets) runs on Cloudflare's free plan.

## Running a campaign

In the launch's server, an admin runs `/provision campaign`. cygnet asks the Worker which
launch the server has and shows its card: how many are on the list and held, in line and
waiting, how many have invites and how many invites were made, used and still open.

- **Stages**, one button each: **gathering** (anyone can get on the list), **closed** (no
  new sign ups; invites already out still work) and **open** (the server is open; the
  page says so and invites still bring friends in). The first time it opens starts the
  12 month clock on deleting the launch's data.
- **Releases**: *release to next 10* and *next 50* give 3 invites each to the next people
  in line who have none and aren't held.

The same lives on the Worker's admin API, with `ADMIN_TOKEN` as a bearer:
`GET /admin/<launch>` (the numbers), `POST /admin/<launch>/stage {"stage": "closed"}`,
`POST /admin/<launch>/release {"count": 10, "invites": 3}`, `POST /admin/<launch>/lift
{"user": "<discord id>"}`, `GET /admin/<launch>/tree` and `GET /admin/verify`.

## GDPR, by design

- **Notice:** a compact one under every card, and the full one at `/privacy`: who's
  responsible, what's kept and why (taking part in the list, and the legitimate interest
  in stopping double sign ups), who processes it (Cloudflare, Discord, Scaleway), for how
  long, the rights, and that nothing is decided by profiling. The voice test holds it to
  the page's voice.
- **Access and portability:** `/<launch>/mine` downloads everything kept about the person
  signed in, as JSON, ledger entries included.
- **Erasure:** leaving deletes their row at once, and wherever their Discord ID stood in
  someone else's record (an invite, a sponsor) it becomes the keyed hash the ledger knows
  them by, `h:`. The ledger keeps that hash, for the tree's tamper evidence, until the
  launch's data goes.
- **Storage limitation:** network and area hashes go after 30 days, email hashes 30 days
  after launch, and a launch's whole list, invites and ledger 12 months after it opens
  (`keepList`); each launch's ledger chains on its own so it can go alone.
- **Cookies:** only the ones a sign in and a step in progress need, and local storage for
  the theme and the swan: nothing to consent to, so no banner.
- **Who:** `CONTROLLER` and `CONTACT` are set as secrets at deploy, so no name is in the
  code; unset, the notice names the people running cygnet.lol and
  `privacy@cygnet.lol`, which Email Routing can forward to a real inbox for free.

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
| the address a link is sent to | not kept | used for the send, shown masked from a cookie |

Worker logs are off (`observability`): a URL can hold an invite code.

## Commands

```sh
cd edge
npm ci
npm run check        # wrangler types, then tsc
npm test             # rules, store, ledger tampering, the email budget, every page
npx wrangler d1 migrations apply cygnet-edge --local && npm run dev
```

For `npm run dev`, put dev values in `edge/.dev.vars` (gitignored). Turnstile's test
keys always pass: site key `1x00000000000000000000AA`, secret
`1x0000000000000000000000000000000AA`.

## Going live

On the Cloudflare account that holds `cygnet.lol` (the free plan is enough):

1. `npx wrangler d1 create cygnet-edge`, and put its `database_id` in `wrangler.jsonc`.
2. A Turnstile widget for `cygnet.lol` (managed); its site key goes in `wrangler.jsonc`
   as `TURNSTILE_SITEKEY`.
3. Scaleway, console, Transactional Email: add the domain `cygnet.lol` in Paris and add
   the SPF, DKIM and DMARC records it lists to Cloudflare DNS, then wait for it to check
   out. Its project ID goes in `wrangler.jsonc` as `SCW_PROJECT_ID`; an IAM API key whose
   policy allows only Transactional Email in that project is `SCW_SECRET_KEY`.
   Do this well before invites go out: a new domain sends slowly at first.
   **The key expires a year after it's made** (the first one, made 2026-10-11, runs out
   around 2027-10-11). Before then, generate a new one on the same IAM application and
   `npx wrangler secret put SCW_SECRET_KEY` again; until it's replaced, link emails fail
   and the page says it couldn't send.
4. In the Discord developer portal, cygnet's app, OAuth2: add the redirect
   `https://cygnet.lol/auth/discord`. Its application ID goes in `wrangler.jsonc` as
   `DISCORD_CLIENT_ID`.
5. The secrets, each typed in, never in a file: `npx wrangler secret put` for
   `DISCORD_CLIENT_SECRET`, `TURNSTILE_SECRET`, `SCW_SECRET_KEY`, `HMAC_KEY` and `ADMIN_TOKEN` (the last two
   `openssl rand -base64 32`). Optionally `CONTROLLER` (who's responsible, as the privacy notice names
   them) and `CONTACT` (their address). Changing `HMAC_KEY` later makes every stored hash and the
   ledger's chain unverifiable, so set it once.
6. `npm run deploy` applies the migrations and deploys to the `cygnet.lol` custom
   domain.
7. On foundry, in `~/cygnet/.env`: `CYGNET_EDGE_URL=https://cygnet.lol` and
   `CYGNET_EDGE_TOKEN` set to the same value as the Worker's `ADMIN_TOKEN`, then restart
   cygnet. In the launch's server, run `/provision launch name:silcord`: cygnet reads the
   server's name and picture with its own token and registers them, and keeps them current.
   Any server can launch this way; a name the code doesn't know gets the default line.
8. Before announcing it: one real link each to Gmail, Outlook and a custom domain, to
   see it lands in the inbox and not spam.
