# UX standard

Every surface cygnet has follows this: replies, embeds, webhook posts, command names and
descriptions, the setup CLI, and the art. A change to a surface is checked against it,
and a layout claim is checked by measuring or by a test, never by eye.

[`DESIGN.md`](./DESIGN.md) is the design every bird shares and wins where the two
disagree; this file adds what is cygnet's own. Surfaces still drawn the older way
(the provision module's drift reports, records drawn as containers) move to it in
their next PR.

## Voice

cygnet is a quiet, clever bird who knows more than she says and is casual about it, a
friend of peregrine and merlin (merlin's brief, `merlin/internal/voice/PERSONA.md`, is the
shared one). She talks the way a person types in a server: natural sentences, not
clipped fragments, not a character performing. The tells to avoid are the ones that
read as generated: a tag hung off a comma for effect (`takes back what you said, all of
it`), dramatic two-word fragments (`coffee's cold. trail's warm`), and chains of
semicolons. Errors stay plain: say what happened and what to do, with no wit.

Her flavour is another register: where she isn't answering anyone (her status lines,
cygnet.lol's home and launch pages and their quiet states) she sounds like the owner's
own prose, images of light, water, feathers and a borrowed sky, felt more than
explained. Anything a person acts on (a step, a button, a reply, an error) stays plain.

The rules every bird shares, which `brand/voice_test.go` holds the status lines to and
`help`'s voice test holds every page to:

- All lowercase. The exceptions are names that have to be typed exactly as written:
  environment variables (`DISCORD_BOT_TOKEN`), flags, URLs and code.
- No closing full stop. Clauses join with `; `, or with `:` when the second explains the
  first: `slowmode is on: you can send again in 20s`.
- Say what happened, then what to do: `you're sending too fast; try again in a couple of
  minutes`. A wait or a limit is stated as it is, not rounded to sound nicer.
- Plain words and contractions. No oxford commas, no exclamation marks, no emoji. The
  marks are `✓` done, `!` warning, `✗` failed, `▸` step, `·` between facts on one line.
- The prose check applies: no em dashes, ellipsis characters or curly quotes.

## Discord

- A reply is ephemeral unless being seen by the channel is the point, and every message
  carries `core.NoPings()`.
- An error a member reads is a `core.Tell`, sent as `✗ <text>`. Any other error is
  logged, and the member sees only `✗ something went wrong on cygnet's side; try again in
  a moment`. REST bodies and internal wording never reach Discord. To say something and
  keep the cause, wrap it: `fmt.Errorf("%w: %w", tell, err)` shows the `Tell` and logs
  the rest.
- A refusal is tested through `core.Router` with `coretest.Event`, checking the reply the
  member gets, not only the error the handler returns.
- **A record** (DESIGN.md's A record: `/status`, `/perf`, a report of something that
  happened) comes from `brand.Embed`: cygnet's face for its mood as the thumbnail, the
  rule (`cygnet_rule`, the bird afloat on a waterline, drawn by `tools/sprites`) as the
  image, its facts from `brand.Fields`, and what explains them in the footer. The rule
  is always attached, too wide to be an emoji; the face is too until the emoji sync.
  The title is one lowercase word.
- **A module has one command, and it opens a panel.** Everything else the module does is
  a button, a select or a form on that panel or on a card it leads to, and each of those
  has `◂ back` to the panel. A new feature is a button on a panel, not a new subcommand:
  options typed into a command are guessed at, a panel shows what is set and what each
  choice does. `/help`, `/provision` and `/launch` are panels; `/status`, `/ping` and
  `/perf` are readouts with nothing to set.
- A page built from components (`/help`) is one Components V2 container, accented in
  a brand colour, wearing its mood icon (`brand.Icon`) as a section thumbnail, with any
  footer as `-#` subtext. It points at synced app emoji, attaching only what hasn't
  synced. Only the title and its `-#` line sit beside the thumbnail; the about goes
  below at full width, because beside a thumbnail a phone gives text a narrow column.
  The about says what the module does and how to use it before anything else.
- An ASCII glyph that leads a line (`├ └ ▸`) leads only a line short enough never to
  wrap: a wrapped line leaves its text hanging under the glyph on a phone. The `/help`
  index's tree marks each module's name line; a page lists each command as a fact (its
  invocation in bold, then its options), a subcommand under its full invocation.
- **A panel's state** (`brand.Card`: a panel, or what one of its buttons turns it into)
  is DESIGN.md's A panel, accented in the mood of what it says (ok when done, warn
  while working, error when it failed, info otherwise). Its first line is the module's
  emoji, its name in bold and what this is: `<emoji> **provision** · apply`. The body is
  text: a `✓`/`!` line, facts, `-#` detail, an ASCII bar (`▰▱`) or tree (`├ └`).
  Buttons go inside the container. A panel edited in place stays a container
  (`discord.NewMessageUpdateV2`): Discord lets a message take the Components V2 flag
  on an edit but never drop it, so a record can turn into a panel and a panel cannot
  turn back. Errors stay `core.Tell`, plain text.
- A message cygnet posts as someone else (a bridge or a webhook) is their text. What
  cygnet adds to it, such as who it replies to, is a `-#` subtext line below it, never an
  embed: a webhook can't make a real Discord reply, and a box outweighs the message it
  annotates. When cygnet needs more than a line, it borrows `/help`'s colour identity and
  layout rather than inventing a new one.
- Facts are fields in a record and bold-label lines in a panel (`core.Readout`), and a
  code block holds only what somebody copies: DESIGN.md's Facts.
- **A log card** is drawn by `ledger.Render` and laid out as DESIGN.md's Log cards.
- Numbers: `42 ms` with a space, one decimal under 10 ms. Durations use the two largest
  units: `45s`, `12m`, `3h 12m`, `2d 4h`. A value not measured yet says so, never `0`.
- Command and option descriptions: lowercase, no full stop, what it does in a few words.

## CLI

`tools/setup` prints on four columns:

```
▸ step title              ▸ in column 0, the title in 2
    ✓ result              marker in column 4, text in 6
      note                plain text in 6
✗ error                   ✗ in column 0, the text in 2
  continued               further lines of an error in 2
```

A blank line comes before every step and before the error. Use `step`, `ok`, `warn` and
`note`. The one bare `fmt.Print` is the token prompt, which has to leave the cursor on
its line. A value the tool did not ask for, such as a token
taken from the environment, is said out loud before it is used.

## Art

- Pixel art on an exact grid. Every cell is one flat colour and every cell is the same
  size: the avatar is 128 cells, so the profile picture is 8 px cells (1024) and the
  mood icons 2 px cells (256). The banner is 320 by 110 at 4 px (1280 by 440).
  `internal/brand` fails if an icon has a cell that is not flat.
- Resample by majority per cell (`cells`), never one sample per cell, then reduce to
  the drawing's own 40 colours (`quantise`). Every colour in the brand comes from the
  avatar: navy field, dusk sky and water, cream down, slate bill, reeds.
- Centre on the grid: the disc (the cygnet in its water and reeds) is
  centred in its icon to the nearest cell, on the flat navy field in the profile picture.
- A badge glyph is 6 by 6 with equal margins on opposite sides, so it sits one 8 px cell
  inside the badge's interior on every side. `tools/sprites` refuses a glyph that is
  not.
- Every module has an icon, the one `/help` and its cards wear: a pixelarticons glyph
  (MIT) traced onto the 24 cell grid in `tools/sprites/icons.go`, one cell per unit of
  the icon's 24 unit viewBox, with the source icon named beside it. A module ships
  with its glyph; `internal/brand` fails any module whose `Name()` has no
  `mod_<name>.png`.
- Edit `art/source` or `tools/sprites` and rerun the generator, never the output PNGs.
