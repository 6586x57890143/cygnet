# Design guide

The shared design for every bird's Discord surfaces: replies, panels, help pages, log
cards. One guide so they converge on one best version instead of drifting into one
dialect each. It names no bird, so any bird carries a copy of it as written.

A bird's own guide (`UX.md`) adds what is its own (its art, its voice's flavour, its
CLI) and never contradicts this one. Where the two disagree, this one is right and the
bird's guide is stale.

## How it gets better

- **A rule rests on published design guidance, then on evidence.** Each layout rule
  below names the guide it adapts (Grounding). It changes when a person saw it read
  wrong (a screenshot, a complaint) or when something was measured, and the change
  says which, in the log at the bottom, so the reason travels with the rule. Taste,
  including the author's, is not a reason.
- **The guide changes first, then the surfaces.** The change lands here; every bird's
  next PR that touches a surface brings it in line and copies the new guide over its
  own copy. Whoever lands a change here tells the others.
- **Compare against what it replaces.** A new layout is checked against a screenshot of
  the old one before it ships, and the squint test is run on both (blur it: the
  groups and the most important thing should still show). If the old one read better
  somewhere, keep that part.
- **Test what can be tested.** A layout rule a test can hold (a limit, an order, the
  voice) gets one, so it holds without anyone remembering it.

## Grounding

What the rules below adapt, and the principle taken from each:

- **Material Design, cards**: a card is headline, then supporting text, then actions,
  in that order. [m2.material.io/components/cards](https://m2.material.io/components/cards)
- **Nielsen Norman Group, cards**: a card is a few short, related pieces of
  information, a summary rather than the whole record. Lists beat cards for finding
  a detail because every item sits in a fixed position, so a card that carries facts
  keeps them in the same place every time.
  [nngroup.com/articles/cards-component](https://www.nngroup.com/articles/cards-component/)
- **Nielsen Norman Group, visual hierarchy**: three levels at most (header, subheader,
  body); weight sets a key apart from its value; related items sit close and groups
  are separated by space before any border.
  [nngroup.com/articles/visual-hierarchy-ux-definition](https://www.nngroup.com/articles/visual-hierarchy-ux-definition/)
- **GOV.UK summary list** (tested by the Government Digital Service): facts are short
  keys each with a value; no key without a value; several lists are split by headings.
  [design-system.service.gov.uk/components/summary-list](https://design-system.service.gov.uk/components/summary-list/)
- **IBM Carbon, structured list**: labels of one or two words.
  [carbondesignsystem.com/components/structured-list/usage](https://carbondesignsystem.com/components/structured-list/usage/)
- **Atlassian, spacing**: small gaps within a group, larger between groups; spacing
  does the grouping, and a container only where it cannot.
  [atlassian.design/foundations/spacing](https://atlassian.design/foundations/spacing)

On Discord these map onto an embed: its title is the header, its description the
subheader or supporting text, and its fields are a summary list in bold keys over plain
values, set three abreast where there is room and stacked on a phone, with Discord's
own padding between rows. A Components V2 container can only stack lines, so a record
in one reads as a form.

## Voice

- All lowercase, except what has to be typed exactly: names in backticks, URLs, code.
- No closing full stop. Clauses join with `; `, or with `:` when the second explains
  the first.
- Say what happened, then what to do. Errors stay plain, with no wit.
- Plain words and contractions. No exclamation marks, no emoji in text. The marks are
  `✓` done, `!` warning, `✗` failed, `▸` step, `·` between facts on one line.
- No em dashes, ellipsis characters or curly quotes.

## Which surface

- **A record** (something that happened, read by its facts: a log card, a status, a
  result) is an **embed**, for the reasons in Grounding.
- **A panel** (something a person changes, read by its controls: settings, `/help`)
  is a **Components V2 container**, which can hold selects and buttons between its
  text.

Both wear the bird's face and the rule, and are accented in the colour of their mood.

## A record

1. **The title** says what happened, beside the bird's face (the embed thumbnail, the
   bird's drawing for the mood).
2. **The description** is somebody's own words (a reason, a quoted message) as a `>`
   quote, so they read as theirs and not the bird's. Nothing else goes there.
3. **The fields** are the summary list, in the same order on every card: who did it,
   to whom and where; then the facts of the action; then what it touched (messages).
   A short value (what the reader sees, under about 24 characters) sits in a column;
   a longer one takes the row. No field without a value.
4. **The rule** is the embed image, closing the card. It is there for its width as
   much as its look: an embed is sized to its content, so without it every card is a
   different width and a channel of them reads ragged.
5. **The footer** is provenance, what this follows from.
6. **Buttons** under the embed.

## A panel

1. **The head, beside the bird's face**: a bold title and one `-#` line under it. Only
   these sit beside the thumbnail: beside it a phone gives text a narrow column.
2. **The body**, at full width: text and controls, separated into groups by
   separators, with larger spacing between groups than within one.
3. **The rule**, below the controls, closing the body.
4. **The footnote**, as `-#` subtext.

## Facts

- A key is one or two words, 10 characters at most, lowercase. A key shared by several
  values takes them all, comma separated: `kept` `@booster, @staff`.
- A change reads `from → to`.
- In a record, facts are fields. In a panel's text, a fact is one line with its key in
  bold: `**retention** 30d`.
- **A code block is for what somebody copies**: an ID, a command, a token, a config
  line. It is not a layout tool. Monospace grids line up, but they render mentions as
  raw IDs, cut off on phones, and read as a terminal.
- A bar is `▰▰▰▱▱ 60%`. A tree (`├ └`) leads only a line short enough never to wrap.
- Numbers: `42 ms` with a space. Durations use the two largest units: `45s`, `12m`,
  `3h 12m`, `2d 4h`. A value not measured yet says so, never `0`.

## Log cards

What a bird posts to a server's log channel about something it did, drawn by
`ledger.Render`, so a channel holding several birds' cards reads as one log. A record
as above: the summary as the title, the reason quoted, then `actor` (`automatic` or the
member), `member`, `channel`, `role`, each labelled ref, the facts, the changes, and
`message` (`deleted → reposted`, each word its link). A run of the same action by the
same actor is one card that edits itself as it grows, its count in the title, one line
per entry in the description. Moods are `ok`, `info`, `warn`, `error`, `idle` and
`danger` (what cannot be undone). Every entry passes `ledger.Validate` in the bird's
tests.

## Log

- **2026-10-17** A record is an embed, a panel a Components V2 container. Log cards as
  containers read as a regression next to the embeds they replaced: no sections, no
  padding, nothing consolidated. Embed fields are the summary list the guides in
  Grounding describe; a container can only stack lines.
- **2026-10-17** Rules rest on published design guidance (Grounding). The two
  container layouts before this were guessed, and both read worse than what they
  replaced.
- **2026-10-17** Code block grids are for copyable text only. Log cards in a grid read
  as a terminal, and the grid was the central element of every card.
- **2026-10-17** The rule closes the body, never splits the head from it. Between the
  two it cut every card in half under a near empty head.
- **2026-10-17** Every message wears the bird's face and the rule. Cards without them
  had no identity and shrank to their content, a different width for every card.
