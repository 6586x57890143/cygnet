# Backing the birds

A monthly supporter for the birds, paid in crypto into wallets only their owner holds,
for a length bought up front. cygnet.lol sells it at `/support`; cygnet quotes each
checkout, watches the chains for its payment and records what it bought; the birds ask
her who backs them and give their supporter role.

cygnet holds **no key and moves no money**. Every chain call is a public read. What a
payment buys is the role, preview builds, early access and a say: never a feature of a
bot, since Discord's monetization terms ask an app that sells its own features to sell
them through Discord too.

## What can be bought

Lengths and rates are rows in Postgres (`support_plan`, `support_rate`, migration 006),
so a new length or tier is a row, not a release.

| Length | Price at $5 |
|---|---|
| 1 month | $5 |
| 3 months | $14 |
| 6 months | $27 |
| 12 months | $50 |

A plan's price is its `worth` (in hundredths of a month) times the monthly rate. The
rates are $5, $10 and $20 a month; anything above the lowest wears the gold badge and
buys nothing else. Nothing renews: a payment buys its length, after whatever the person
already has.

## How a payment finds its checkout

A checkout asks for its price **plus change no other live checkout on its rail has**:
up to 9999 units of a 6 decimal stablecoin (under a cent), or up to 0.0001 SOL. A unique
index on `(rail, amount)` over live checkouts makes that a database fact, not a hope, and
a quote that draws a taken amount draws again. The transfer that arrives with exactly
that amount pays exactly that checkout.

- A checkout's amount holds for an hour, 20 minutes on SOL, whose price is read from
  Coinbase's public spot price when the checkout is made and believed only between $1
  and $10,000. Past that it is *lapsed*: a stablecoin checkout still takes a late
  payment and keeps its amount from anyone else for 48 hours, a SOL one for 10 minutes
  (long enough to finalize, too short to be a bet on the price), then it is *void* and
  the amount is free again. A SOL payment after that matches nothing (below).
- One person holds at most three live checkouts, lapsed ones included; asking again
  for the same thing returns the one they have.
- Each rail is read on its own 30 second clock, so a slow or flooded one never holds
  up the rest; a rail's Solana accounts share it, in an order that turns every round.
  A Solana read lists at most 10 pages of signatures and fetches at most 50
  transactions, and saves how far it got either way, so a flood of junk slows reading
  down and never stops it. What a read got before an error is kept.
- A SOL checkout past its 10 minutes of slack stops showing where to pay and says to
  start a new one, since nothing sent then pays it.
- The Solana rail remembers which wallet it watches. A different wallet is read from
  the moment it is set, never back through its history.
- Anything that arrives and matches nothing (a wrong amount, a gift) is recorded all
  the same and listed on the keeper's `/support` panel with its transaction, for the
  keeper to sort out by hand with **give backing**.
- A payment is never changed or deleted once written (a trigger refuses it), and is
  recorded at most once: `(rail, transaction, index)` is its key.

## The rails

`internal/chain`. Every contract and mint is the one merlin's tip jar checked live
against its own `symbol()` and `decimals()`.

| Rail | Read with | Final when |
|---|---|---|
| USDC, USDT on Solana; SOL | `getSignaturesForAddress` on the wallet's token accounts (or the wallet, for SOL), then `getTransaction`: what the wallet's balance of the mint gained | finalized |
| USDC on Base, Polygon | `eth_getLogs` for ERC-20 `Transfer` to the wallet, 1000 blocks a request | 30 blocks deep on Base, 128 on Polygon |
| USDT on TRON | TronGrid's `/v1/accounts/<wallet>/transactions/trc20` | `only_confirmed` |

Each watched account has a cursor (`support_cursor`): a block, a time or a signature.
The first read starts at the present and reads nothing older, so a wallet's history is
never taken for payments; a Solana token account found later (the first payment opens
it) is read from when its rail was first watched. The chains are read every minute;
what arrived is recorded first and the cursor moved after, so a crash between the two
reads the same transfers again, which are already seen. A failed read leaves its cursor
where it was and says why on the panel.

`internal/chain/live_test.go` reads the real chains through the public endpoints and
checks the readers see real transfers. Run it before changing a reader:
`CYGNET_LIVE_CHAINS=1 go test ./internal/chain -run Live -v`.

## Who asks what

| Verb | Who | What |
|---|---|---|
| `cygnet.offer` | the Worker | the lengths, rates and rails on offer, and the owner's name and picture |
| `cygnet.quote` | the Worker | a checkout for whoever signed in |
| `cygnet.checkout` | the Worker | a checkout, for the person who opened it |
| `cygnet.supporter` | any bird | someone's backing: active, gold, since, until |
| `cygnet.supporters` | any bird | everyone backing the birds now, for a role |

The Worker says who signed in, and only the Worker is trusted to: its verbs refuse any
other caller. The sign in asks Discord for `identify` alone, and the token is given back
as soon as the ID is read. A cookie (`cy_support`, signed, a week, `SameSite=Lax`)
remembers who signed in.

## What is kept

| | Where | Until |
|---|---|---|
| who opened a checkout, what it was for, its amount | cygnet | as long as payment records must be kept |
| every transfer seen (transaction, amount, sender address) | cygnet | the same |
| what a payment or a gift bought | cygnet | the same |

These are payment records, so **forget me everywhere** leaves them; the privacy notice
says so. The Worker keeps nothing about a supporter.

## The page

`edge/src/support.ts` draws every `/support` page; `edge/public/support.css` and
`support.js` are its look and its moving parts. It is the birds' and the owner's page,
not cygnet's: the swan page's sky in glyphs across the whole page, with a branch in it
instead of water and a merlin on the branch.

- **The merlin** (`edge/public/merlin.js`) is a point cloud drawn as ASCII the way
  `swan.js` draws the swan, and nothing in it is a guessed shape. Her profile, her width,
  her eye, beak, tail, legs and toes are measured off two public domain photographs,
  kept without metadata in `art/source`: `merlin_side.jpg` ("Merlin" 54309288128, US
  Fish and Wildlife Service) and `merlin_front.jpg` ("Merlin, January 27 2021", Great
  Smoky Mountains National Park). Her colours are those photos', sampled into
  `merlin-skin.js` by `go run ./tools/supportart`. She is rigged about measured pivots:
  her head turns, her tail flicks, she breathes and blinks, and looks toward a pointer
  near her.
- **The light** is the swan page's day going round (night, morning, day, evening), and
  she is painted by it as that page paints the swan: lit planes in the warm key, shadows
  in the sky's cool fill, an outline catching the light. By day the key is set in front
  of her, since the noon sun behind her would leave everything you see in shade.
- **The marks** (`tools/supportart`): the badge, the perks and every coin and chain the
  page names, each an 11 by 11 glyph on merlin's tile, in a dusk and a paper drawing that
  follow the theme. Coins and chains wear their makers' colours warmed toward merlin's
  cream. Inline they are 16 px, an eighth of the tile, so they stay sharp.
- **The checkout's choices** are laid out as the good checkouts do: the length a segmented
  row with its saving, the amounts cards, the ways to pay a grid of coin-on-chain tiles,
  a summary of what is about to be paid and the total on the button.
- **Without script** every choice is still a radio in its label and the page works; the
  canvas is replaced by merlin as one frame of text. **With reduced motion** nothing
  moves: the scene is one still frame, and the page's own motion (choices easing, the
  summary settling, how it works opening, the morph between pages) is off.

## Going live

Nothing is offered until a wallet is set: `/support` says backing opens soon.

1. On foundry, in `~/cygnet/.env`, the wallets, each optional: `CYGNET_SUPPORT_EVM`
   (one `0x` address takes Base and Polygon), `CYGNET_SUPPORT_SOLANA`,
   `CYGNET_SUPPORT_TRON`. A malformed one stops cygnet at boot rather than publishing
   somewhere nobody can spend from. They are whose `CYGNET_SUPPORT_OWNER` names (the
   bootstrap admin when unset), whose name and picture the page shows.
2. Public nodes rate limit: Base's own refuses log reads outright, so the defaults are
   publicnode's. For steady reads set a keyed endpoint per chain, `CYGNET_RPC_BASE`,
   `_POLYGON`, `_SOLANA`, and a TronGrid key, `CYGNET_TRONGRID_KEY`.
3. Restart cygnet. Its log says which rails it takes; `/support` in a server shows the
   keeper each rail's last read.
4. Send a small real payment on each rail you offer and watch the panel see it. A
   wallet that never held USDT or USDC on Solana has no account for it until the first
   payment, which is fine: it is found on the next read.
5. Before announcing it, the birds that give the supporter role read
   `cygnet.supporters` (merlin's side, its own PR).
