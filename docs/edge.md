# The edge Worker

`edge/` is `cygnet.lol`: a launch's page (`/silcord`), its sign in with Discord, the
invites its members give and take, the way in when their turn comes, and the page behind
every link a member makes with `/invite`. It holds no bot token, opens nothing on
foundry, and emails only the one link a person asks for.

**cygnet owns the list.** A launch's line, its invites, the waves that let it in and the
ledger of all of it live in cygnet's Postgres (`internal/store`, migration 005). The
Worker asks her for them as the bird `edge`, with `POST /v1/call` on her hub through the
tunnel (below), and keeps in D1 only what a sign up needs: the keyed hashes that stop
one person signing up twice, the emailed links that prove an address, the email budget,
the birds in the footer, and what cygnet sends for each launch's page (its server's name
and picture, who may see its list, its stage and when it first opened).

| Verb | What the Worker asks |
|---|---|
| `cygnet.code` | whether a link works: a server's (its name, who it's from) or a launch's |
| `cygnet.redeem` | a server's link spent, and the person let in |
| `cygnet.join`, `cygnet.place` | onto a launch's list; where someone stands |
| `cygnet.issue`, `cygnet.revoke` | an invite kept by its hash (the Worker makes the code); taken back |
| `cygnet.vouch` | an invite spent, and its taker put on the list under its giver |
| `cygnet.seal` | a sign in sealed to cygnet for *add me when it opens*, or dropped |
| `cygnet.admit` | someone a wave let in, added or given an invite only they can take |
| `cygnet.leave`, `cygnet.mine`, `cygnet.forget` | off a list; everything kept about them; forgotten everywhere |
| `cygnet.list`, `cygnet.lift` | the list and its holds, for the server's owner and the breakglass admin |

Each refusal is `denied` with a reason the Worker says in its own words. A join goes to
cygnet first and the hashes are kept after; someone signed up here she doesn't know is
put on her list when they next sign in.

## Links into a server

`/invite` in Discord makes a link, `cygnet.lol/i/<code>`, that lets one person into a
server cygnet keeps. cygnet keeps the code (its SHA-256, in Postgres); the Worker only
asks her, as the bird `edge`, with `POST /v1/call` on her hub through the tunnel (below).

1. The page asks `cygnet.code {code}` (the hash) for whether the link still works, the
   server's name and picture, who it's from and how long it has left. A launch's invite
   says so, and goes the launch's way instead.
2. **take me in** passes Turnstile and goes to Discord with `identify guilds.join`;
   **send me an invite instead** with `identify` alone.
3. Back at `/auth/discord`, the Worker reads who they are and asks
   `cygnet.redeem {code, user, token}`, the token only if they chose to be added. cygnet
   checks the gate (an account 30 days old, not banned), spends the code and adds them,
   or makes a Discord invite only they can take, once, within five minutes. Then the
   Worker revokes their token.
4. They land in the server (`discord.com/channels/<server>`) or on their invite
   (`discord.gg/<invite>`). A refusal is said plainly: the link is gone, their account
   is too new, they're banned, or they're in as many servers as Discord allows.

Nothing about the person is stored at the edge. cygnet keeps who used which link and
when they joined and left, which is the tree.

## Getting on the list

1. `count me in` passes Turnstile (invisible unless it has doubts), then goes to Discord
   with a state only that browser holds (`cy_state`, 10 minutes).
2. Discord sends them back to `/auth/discord` with `identify email`. The Worker reads who
   they are and revokes the token at once.
3. They need a verified email on their Discord account, and an account at least
   `minAgeDays` (30) old. One place per account and per email (lowercased, `+tag`
   dropped, Gmail dots dropped).
4. cygnet gives them the next place (`cygnet.join`). Their email, connection (IPv4
   address or IPv6 /64), network (/24 or /48) and area (country and a half degree
   square) are kept in D1 as HMACs under `HMAC_KEY` (`signup`). A second person on the
   same connection, or the same network in the same area, gets a place with their
   invites held for an admin (`held` says why).
5. A signed cookie (`cy_silcord`, 30 days) shows them their place after that.

## Invites, and the tree they make

- **Released, not reserved.** An admin releases a contingent to the front of the line
  from `/launch`: 3 invites each to the next people in line who have none and aren't
  held.
- **Issued with a proof.** `make an invite` takes a fresh Discord sign in (the same
  account, the same email as when they joined) and a proof of the email (below). Only
  then is a code made: 20 random bytes, shown once as `cygnet.lol/i/<code>`. cygnet
  keeps its SHA-256 (`cygnet.issue`), good for 48 hours. An invite nobody took is back
  with its issuer when it runs out, or when they take it back.
- **Taken with a proof.** The person it's for signs in with Discord (verified email,
  30 days old, not on the list) and proves their email. Then `cygnet.vouch` spends the
  invite and puts them on the list under its issuer in one transaction, so an invite is
  spent once, and only by someone who got in. Sent again, it answers the same.
- **The tree.** Every member who came in through an invite hangs from whoever gave it.
  Leaving takes back a member's unspent invites but keeps whoever they brought in under
  them, under a name that isn't theirs: the tree is the record of who vouched for whom.

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

In the launch's server, an admin runs `/launch`. The first time, they name its page, and
cygnet registers the launch in Postgres and sends the Worker what the page shows. After
that the panel is the campaign: how many are on the list and held, in line and waiting,
how many have invites and how many invites were made, used and still open, and
`ledger ✓ n entries` from a walk of the ledger.

- **Stages**, one button each: **gathering** (anyone can get on the list), **closed** (no
  new sign ups; invites already out still work) and **open** (the valve lets the line
  in; invites still bring friends in). The first time it opens starts the 12 month
  clock on deleting the launch's list.
- **Releases**: *release to next 10* and *next 50* give 3 invites each to the next people
  in line who have none and aren't held.
- **The valve**: a ceiling (5 to 100 joins a minute) and a pause. Once open, each minute
  cygnet lets in whoever followed someone already in, and once the last wave has settled
  opens the next: the next people waiting on their own, by place, with everyone they
  brought in. The first wave is five minutes of joins at the ceiling; then it doubles
  when joins stayed under half the ceiling and skua says the server isn't busier than
  twice usual, halves when joins went over, and holds otherwise (SPEC.md, experiment 6).
  The panel shows the last wave and why it was that size.
- **Their way in.** Someone who asked to be added when it opens is added by cygnet with
  the sign in they sealed to her (below). Anyone else finds their way in on their page
  when their wave comes: **take me in** (cygnet adds them with a fresh sign in) or **send
  me an invite instead** (one only they can use, for five minutes).

The list itself is on the web at `/<launch>/list`, behind a Discord sign in, for the
server's owner and cygnet's breakglass admin, who can lift a hold there.

The Worker's admin API, with `ADMIN_TOKEN` as a bearer, is now only what cygnet sends it:
`PUT /admin/<launch>` (the page) and `PUT /admin/bird/<name>` (the footer).

## The flock in the footer

Every page ends with one line: the birds that keep the site, each as its own pixel
picture, then `kept by cygnet with skua` and a link to the full notice at `/privacy`.
The notice is linked from every page, not repeated under the card.

The birds come from the hub. Once she is connected, cygnet sends herself and every bird
linked to her, each the bird's Discord avatar resampled by majority to 32 by 32 cells:
`PUT /admin/bird/<name> {"png": "<base64>"}`, again each hour while it stays linked. The
Worker keeps the last one per bird (`bird`, migration 0003) and serves it at
`/bird/<name>.png?v=<version>`, so visitors never load anything from Discord for it. A
bird seen in the last day is shown, cygnet first and the rest in the order they first
linked, so a deploy doesn't make one blink out. When merlin links, merlin appears.

## Add me when it opens

Someone on a launch's list can ask to be added when it opens, when they join or later
from their page. Their sign in then asks Discord for `guilds.join` too, and the Worker
seals the refresh token to cygnet's public key (`CYGNET_SEAL_PUBLIC`): HPKE, RFC 9180 base
mode with DHKEM(X25519, HKDF-SHA256), HKDF-SHA256 and AES-128-GCM (`src/seal.ts`, held to
the RFC's own vectors). The Worker can't open it again; cygnet keeps it sealed, opens it
once at their wave, refreshes it with the app's secret, adds them, and gives the grant
back. **never mind and drop it** on their page forgets it at once. A sign in that no
longer adds them is dropped, and their page shows their way in instead.

## GDPR, by design

- **Notice:** a compact one under every card, and the full one at `/privacy`: who's
  responsible, what's kept and why (taking part in the list, and the legitimate interest
  in stopping double sign ups), who processes it (Cloudflare, Discord, Scaleway), for how
  long, the rights, and that nothing is decided by profiling. The voice test holds it to
  the page's voice.
- **Access and portability:** `/<launch>/mine` downloads everything kept about the person
  signed in, as JSON: cygnet's record (`cygnet.mine`, ledger entries included) and which
  hashes the Worker keeps.
- **Erasure:** leaving takes them off cygnet's list and deletes their sign up here.
  Wherever their Discord ID stood in someone else's record (an invite, a sponsor) it
  becomes their pseudonym, a negative ID derived from the keyed hash the ledger knows
  them by. **forget me everywhere** on `/privacy` does the same across every launch and
  every server cygnet keeps (`cygnet.forget`): their links, their joins and who they
  brought in.
- **Storage limitation:** network and area hashes go after 30 days, email hashes 30 days
  after launch, and a launch's whole list, unused invites and ledger 12 months after it
  opens: cygnet's daily sweep (`store.Expire`) and the Worker's cron.
- **Cookies:** only the ones a sign in and a step in progress need, and local storage for
  the theme and the swan: nothing to consent to, so no banner.
- **Who:** `CONTROLLER` and `CONTACT` are set as secrets at deploy, so no name is in the
  code; unset, the notice names the people running cygnet.lol and
  `privacy@cygnet.lol`, which Email Routing can forward to a real inbox for free.

## The ledger

Every join, stage, release, lift, issue, revoke, redemption, seal, admission and leave is
appended to cygnet's `ledger` in the same transaction as the change it records. Each
entry's MAC (HMAC-SHA256 under `CYGNET_LEDGER_KEY`) is keyed over the one before it in its
launch, so an entry changed, dropped or inserted breaks the chain from there on, and a
trigger refuses any `UPDATE` or `DELETE` until the launch's list is gone. `/launch` walks
the chain each time it opens, then checks that each person's released invites and each
issuer's invites match what the ledger says happened, so a grant made around the code
shows up too. Actors and subjects are keyed hashes of Discord IDs.

## What is kept, and for how long

| | Kept as | Where | Until |
|---|---|---|---|
| Discord ID, place, who brought them in, invites released | plain | cygnet | they leave (as a sponsor, as a pseudonym) |
| a sealed sign in | HPKE ciphertext | cygnet | their wave, or never mind |
| email | HMAC | the Worker | 30 days after the launch opens |
| connection, network, area | HMAC | the Worker | 30 days after they joined |
| invite codes | SHA-256 | cygnet | the launch's list goes; the ones someone came in by stay as the server's tree |
| proof tokens | SHA-256 | the Worker | the daily cron |
| the address a link is sent to | not kept | | used for the send, shown masked from a cookie |

Worker logs are off (`observability`): a URL can hold an invite code.

## Commands

```sh
cd edge
npm ci
npm run check        # wrangler types, then tsc
npm test             # rules, store, ledger tampering, the email budget, every page
npx wrangler d1 migrations apply cygnet-edge --local && npm run dev
node tools/og.mjs    # redraw public/art/og.gif, the link preview, from the live page
```

A link to any page unfurls as `public/art/og.gif`: the swan on the water at night,
looping. Discord plays it; apps that don't animate show its first frame. It is drawn
from what's deployed, so redraw it after the scene changes and deploy again. Another
scene is `node tools/og.mjs evening`.

Discord keeps a picture by its URL, whatever page it came from, so a redrawn GIF needs
the `?v=` on `og:image` in `src/page.ts` bumped, or Discord shows the old one.

The name is sized for the preview Discord shows, about half the GIF's width. A
GIF's compressed bytes can spell a word by chance, so check it with the pre-push grep
and redraw at a different palette size if it matches.

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
7. On foundry, in `~/cygnet/.env`: `CYGNET_EDGE_URL=https://cygnet.lol`,
   `CYGNET_EDGE_TOKEN` set to the same value as the Worker's `ADMIN_TOKEN`,
   `CYGNET_LEDGER_KEY` (`openssl rand -base64 32`, set once), `CYGNET_CLIENT_SECRET` (the
   Worker's `DISCORD_CLIENT_SECRET`), and `CYGNET_SEAL_KEY` from `go run ./tools/sealkey`,
   whose `CYGNET_SEAL_PUBLIC` goes in `wrangler.jsonc`'s `vars`. Then restart cygnet. In the launch's server, run `/launch` and name its page `silcord`: cygnet reads the
   server's name and picture with its own token and registers them, and keeps them current.
   Any server can launch this way; a name the code doesn't know gets the default line.
8. Before announcing it: one real link each to Gmail, Outlook and a custom domain, to
   see it lands in the inbox and not spam.

### The tunnel

What `/invite`'s links need: a way from the Worker to cygnet's hub that opens no port.

1. Zero Trust, Networks, Tunnels: create a tunnel (cloudflared) and copy its token. Add
   a public hostname for it on `cygnet.lol` (say `hub.cygnet.lol`) whose service is
   `http://cygnet:7700`.
2. Zero Trust, Access: a service token for the Worker, and a self-hosted application on
   that hostname whose one policy is **Service Auth** for that token. Without the token,
   Cloudflare answers and cygnet never sees the request.
3. On foundry, in `~/cygnet/.env`: `COMPOSE_PROFILES=tunnel`, `TUNNEL_TOKEN=<the
   tunnel's token>`, and `edge=<openssl rand -hex 32>` added to `CYGNET_BOTS`. Deploy, or
   `docker compose -f docker-compose.prod.yml up -d`.
4. The Worker's secrets, each with `npx wrangler secret put`: `CYGNET_HUB_URL`
   (`https://hub.cygnet.lol`), `CYGNET_HUB_TOKEN` (the `edge=` token),
   `CF_ACCESS_CLIENT_ID` and `CF_ACCESS_CLIENT_SECRET` (the service token). Then
   `npm run deploy`.
5. In a server cygnet keeps: `/invite`, set how many links members may have, make one,
   and open it signed in as another account.
