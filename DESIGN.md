# Design guide

The shared design for every bird's Discord surfaces: replies, panels, help pages, log
cards. One guide so they converge on one best version instead of drifting into one
dialect each. It names no bird, so any bird carries a copy of it as written.

A bird's own guide (`UX.md`) adds what is its own (its art, its voice's flavour, its
CLI) and never contradicts this one. Where the two disagree, this one is right and the
bird's guide is stale.

## How it gets better

- **A rule changes on evidence, not taste.** Something a person saw read wrong (a
  screenshot, a complaint, a misread), or something measured. The change says what it
  was, in the log at the bottom, so the reason travels with the rule.
- **The guide changes first, then the surfaces.** The change lands here; every bird's
  next PR that touches a surface brings it in line and copies the new guide over its
  own copy. A bird is never left on an old rule because nobody told it: whoever lands a
  change here tells the others.
- **Compare against what it replaces.** A new layout is checked against a screenshot of
  the old one before it ships. If the old one read better somewhere, keep that part.
- **Test what can be tested.** A layout rule a test can hold (a limit, an order, the
  voice) gets one, so it holds without anyone remembering it.

## Voice

- All lowercase, except what has to be typed exactly: names in backticks, URLs, code.
- No closing full stop. Clauses join with `; `, or with `:` when the second explains
  the first.
- Say what happened, then what to do. Errors stay plain, with no wit.
- Plain words and contractions. No exclamation marks, no emoji in text. The marks are
  `✓` done, `!` warning, `✗` failed, `▸` step, `·` between facts on one line.
- No em dashes, ellipsis characters or curly quotes.

## A message

Every message a bird posts is one Components V2 container, accented in the colour of
its mood, laid out top to bottom:

1. **The head, beside the bird's face.** A bold title (the module's icon as an inline
   app emoji, then what this is) and one `-#` line under it (who did it, which module).
   The face is the bird's drawing for the mood, as the section thumbnail. Only these
   two lines sit beside it: beside a thumbnail a phone gives text a narrow column.
2. **The body, at full width.** Somebody's own words (a reason, a quoted message) as a
   `>` quote first, so they read as theirs. Then who and where as mentions, then facts.
3. **The rule.** The bird's wide divider image. It closes the body and divides off the
   footnote, never sits between the head and the body. It is there for its width as
   much as its look: a container is sized to its content, so without it every message
   is a different width and a channel of them reads ragged. On a panel the controls
   are the body, so the rule sits below them.
4. **The footnote**, as `-#` subtext: provenance, what this follows from, a hint.
5. **Buttons**, inside the container, last.

## Facts

- A fact is one line, its label in bold: `**policy** hate speech`. Plain text, so it
  wraps the way any line does on whatever is reading it, and a mention or a link in it
  works.
- Labels are short (10 characters at most) and lowercase. A label shared by several
  values takes them all on one line, comma separated: `**kept** @booster, @staff`.
- A change reads `**field** from → to`.
- **A code block is for what somebody copies**: an ID, a command, a token, a config
  line. It is not a layout tool. Monospace grids line up, but they render mentions as
  raw IDs, cut off on phones, and read as a terminal, not a message.
- A bar is `▰▰▰▱▱ 60%`. A tree (`├ └`) leads only a line short enough never to wrap.
- Numbers: `42 ms` with a space. Durations use the two largest units: `45s`, `12m`,
  `3h 12m`, `2d 4h`. A value not measured yet says so, never `0`.

## Log cards

What a bird posts to a server's log channel about something it did, drawn by
`ledger.Render`, so a channel holding several birds' cards reads as one log. It is a
message as above: the summary as the bold title beside the face, `by @actor` or
`automatic` and the module under it; the reason quoted; who and where; the facts;
the rule; what it follows from; its buttons. A run of the same action by the same
actor is one card that edits itself as it grows, its count in the title and one line
per entry. Moods are `ok`, `info`, `warn`, `error`, `idle` and `danger` (what cannot be
undone). Every entry passes `ledger.Validate` in the bird's tests.

## Log

- **2026-10-17** Code block grids are for copyable text only; facts are bold-label
  lines. Log cards in a grid read as a terminal next to the embeds they replaced, and
  the grid was the central element of every card.
- **2026-10-17** The rule closes the body, never splits the head from it. Between the
  two it cut every card in half under a near empty head.
- **2026-10-17** Every message wears the bird's face and the rule. Cards without them
  had no identity and shrank to their content, a different width for every card.
