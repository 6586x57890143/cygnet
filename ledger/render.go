package ledger

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
)

// Brand is the face a bird's cards wear. The layout is the same for every
// bird; whose face, icons and colours it wears is not.
type Brand struct {
	// Name is the bird's, for an automatic actor: "merlin (automatic)".
	Name string
	// Icon is the inline app emoji for a module's icon, leading the title,
	// or "" before the emoji sync, when the title is text alone.
	Icon func(module string) string
	// Color is the card's accent for a mood, from the bird's own palette.
	Color func(Mood) int
	// Thumbnail is the URL of the bird's drawing for a mood, beside the
	// title, or "" for none. The bird uploads what it names.
	Thumbnail func(Mood) string
	// Rule is the URL of a wide divider image closing the card, or "" for
	// none. It is there for its width as much as its look: Discord sizes an
	// embed to its content, so without one every card is a different width.
	Rule string
}

// Card is a rendered log card: an embed, and the row of buttons under it
// when the card has any.
type Card struct {
	Embed   discord.Embed
	Buttons *discord.ActionRowComponent
}

// Discord's embed limits, less room for what is added around them, and the
// grid the fields are set in.
const (
	maxDescription = 4000
	maxFieldValue  = 1000
	maxFields      = 25
	// columns is how many inline fields Discord sets in a row, and it
	// divides each row by the fields in it: a row of two is split in halves
	// under a row split in thirds, and nothing lines up. So the grid is
	// padded to whole rows.
	columns = 3
	// columnWidth is the longest value, as the reader sees it, that fits a
	// third of a card beside its thumbnail (about 400px on desktop) on one
	// line. A longer one wraps into a tall strip that drags its row out of
	// line, so it goes under the grid at full width.
	columnWidth = 16
)

// Render draws entries as one log card (DESIGN.md's Log cards).
//
// An embed, not a Components V2 container, because a log card is a record
// read by its facts, and embed fields are the one thing Discord lays out as
// a description list: a short label over a short value, three abreast where
// there is room and stacked on a phone. A container can only stack lines,
// which reads as a form rather than a record.
//
//	<icon> AI moderation: message rewritten          [face]
//	> the reason, in the actor's words
//	Actor               Member        Channel
//	merlin (automatic)  @who          #where
//	Policy              Confidence
//	Hate speech         ▰▰▰▰▱ 80%
//	Message
//	Deleted → Reposted
//	[rule]
//	follows <cause>
//
// Several entries (a batch, see Batcher) are one line each under the shared
// title with their count, accented by the loudest of them, their shared
// reason quoted once.
func Render(b Brand, entries ...Entry) Card {
	if len(entries) == 0 {
		return Card{}
	}
	first := entries[0]
	mood := first.Mood
	for _, e := range entries[1:] {
		mood = louder(mood, e.Mood)
	}
	e := discord.Embed{Title: sentence(first.Summary)}
	if b.Icon != nil {
		if i := b.Icon(first.Module()); i != "" {
			e.Title = i + " " + e.Title
		}
	}
	if b.Color != nil {
		e.Color = b.Color(mood)
	}
	if b.Thumbnail != nil {
		if u := b.Thumbnail(mood); u != "" {
			e.Thumbnail = &discord.EmbedResource{URL: u}
		}
	}
	if b.Rule != "" {
		e.Image = &discord.EmbedResource{URL: b.Rule}
	}

	var card Card
	cause := first.Cause
	actor := field("actor", by(b, first.Actor))
	// Always a column, as on the old card: who did it heads every row of
	// the grid, and "merlin (automatic)" is only just over the width.
	*actor.Inline = true
	if len(entries) == 1 {
		if first.Reason != "" {
			e.Description = quote(first.Reason)
		}
		who, what := fields(first)
		e.Fields = grid(append([]discord.EmbedField{actor}, who...), what)
		card.Buttons = buttons(first.Buttons)
	} else {
		e.Title += fmt.Sprintf(" · %d", len(entries))
		e.Description, cause = batch(entries)
		e.Fields = grid([]discord.EmbedField{actor})
	}
	if cause != "" {
		e.Footer = &discord.EmbedFooter{Text: "follows " + cause}
	}
	e.Title = clip(e.Title, 256)
	e.Description = clip(e.Description, maxDescription)
	card.Embed = e
	return card
}

// fields is an entry as a summary list (GOV.UK's summary list, Carbon's
// structured list): short keys, every key with a value, in the same order on
// every card so a reader finds a fact where it was last time (NN/g: lists
// beat cards for finding a detail because positions are fixed). Grouped by
// what the reader asks first, in two sections: to whom and where (who), then
// the facts of the action, ending with the messages it touched (what).
func fields(e Entry) (who, what []discord.EmbedField) {
	for _, k := range []struct {
		kind  RefKind
		label string
	}{{RefUser, "member"}, {RefChannel, "channel"}, {RefRole, "role"}} {
		var ms []string
		for _, r := range e.Subjects {
			if r.Label == "" && r.Kind == k.kind {
				ms = append(ms, mention(e.Guild, r, ""))
			}
		}
		if len(ms) > 0 {
			who = append(who, field(plural(k.label, len(ms)), strings.Join(ms, ", ")))
		}
	}
	var labels, messages []string
	byLabel := map[string][]string{}
	for _, r := range e.Subjects {
		switch {
		case r.Kind == RefMessage:
			messages = append(messages, mention(e.Guild, r, sentence(r.Label)))
		case r.Label != "":
			if _, ok := byLabel[r.Label]; !ok {
				labels = append(labels, r.Label)
			}
			byLabel[r.Label] = append(byLabel[r.Label], mention(e.Guild, r, ""))
		}
	}
	for _, l := range labels {
		what = append(what, field(l, strings.Join(byLabel[l], ", ")))
	}
	for _, f := range e.Facts {
		what = append(what, field(f.Label, f.Value))
	}
	for _, c := range e.Changes {
		what = append(what, field(c.Field, c.From+" → "+c.To))
	}
	// The messages last, as one field read in order: a message removed and
	// its repost are one thing that happened, so "Deleted → Reposted", each
	// word its link.
	if len(messages) > 0 {
		f := field("message", strings.Join(messages, " → "))
		*f.Inline = false // read as a sentence, never a column
		what = append(what, f)
	}
	return who, what
}

// grid lays sections out so every column lines up down the card.
//
// Discord divides each row by the inline fields in it, so a row of two is
// split in halves under a row of three split in thirds and nothing lines
// up. Each section's columns are therefore set in whole rows of three,
// padded with blank fields, and a section always starts a row of its own
// (Atlassian: space between groups, not within). Within a section the
// fields are spread evenly over its rows, four as two and two rather than
// three and one, so no row is a lone field under a full one. Fields too wide
// for a column go under their section at full width, where they read on
// one line and cannot pull the grid out of line.
func grid(sections ...[]discord.EmbedField) []discord.EmbedField {
	var out []discord.EmbedField
	for _, s := range sections {
		var cols, wide []discord.EmbedField
		for _, f := range s {
			if *f.Inline {
				cols = append(cols, f)
			} else {
				wide = append(wide, f)
			}
		}
		total := len(cols)
		rows := (total + columns - 1) / columns
		for r := range rows {
			// Rows before the remainder take one more, so 4 is 2+2, 5 is 3+2
			// and 7 is 3+2+2.
			n := total / rows
			if r < total%rows {
				n++
			}
			out = append(out, cols[:n]...)
			cols = cols[n:]
			for range columns - n {
				out = append(out, blank())
			}
		}
		out = append(out, wide...)
	}
	return out[:min(len(out), maxFields)]
}

// zwsp is a zero width space, built from its code point so the source holds
// nothing invisible.
var zwsp = string(rune(0x200b))

// blank is an empty column, holding a row's place so the next one starts
// under the first column. A zero width space, since Discord refuses an
// empty name or value.
func blank() discord.EmbedField {
	inline := true
	return discord.EmbedField{Name: zwsp, Value: zwsp, Inline: &inline}
}

var (
	link   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markup = regexp.MustCompile(`<(@&|@!?|#)\d+>`)
)

// field is one entry of the list, its key in sentence case, in a column
// when what the reader sees of its value fits one: a link shows as its
// words and a mention as a pill about a name wide, not as the markup behind
// them.
func field(name, value string) discord.EmbedField {
	value = clip(value, maxFieldValue)
	seen := markup.ReplaceAllString(link.ReplaceAllString(value, "$1"), "@abcdefghij")
	inline := utf8.RuneCountInString(seen) <= columnWidth && !strings.Contains(value, "\n")
	return discord.EmbedField{Name: sentence(name), Value: value, Inline: &inline}
}

// sentence is s with its first letter capitalised: titles and keys are in
// sentence case (Carbon), whatever case the entry wrote them in, and
// anything already capitalised ("AI moderation") is left as written.
func sentence(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// batch is the shared reason, then one line per entry: its who and where,
// and its summary only where it says something the title does not. The
// Batcher only batches entries with the same action and actor; a reason or
// cause they do not all share stays on each entry's row in the store.
func batch(entries []Entry) (desc, cause string) {
	reason, cause := entries[0].Reason, entries[0].Cause
	var lines []string
	for _, e := range entries {
		var who []string
		for _, r := range e.Subjects {
			if r.Label == "" {
				who = append(who, mention(e.Guild, r, ""))
			}
		}
		line := strings.Join(who, " · ")
		if e.Summary != entries[0].Summary || line == "" {
			line = strings.TrimPrefix(line+" · "+e.Summary, " · ")
		}
		lines = append(lines, line)
		if e.Reason != reason {
			reason = ""
		}
		if e.Cause != cause {
			cause = ""
		}
	}
	if reason != "" {
		lines = append([]string{quote(reason), ""}, lines...)
	}
	return strings.Join(lines, "\n"), cause
}

// by is who did it: the member, or the bird itself, named so a channel
// holding several birds' cards says which one acted.
func by(b Brand, a Actor) string {
	if a.Kind == ActorMember {
		return mention("", User(a.ID), "")
	}
	if b.Name == "" {
		return "automatic"
	}
	return b.Name + " (automatic)"
}

func plural(s string, n int) string {
	if n > 1 {
		return s + "s"
	}
	return s
}

// quote is somebody's own words as a block quote, set apart from what the
// bird says: on one line, since a quote's every line needs its own marker.
func quote(s string) string { return "> " + oneLine(s) }

// mention is how a Ref reads in Discord, or "" for one it cannot draw. A
// message is a link named by word ("message" by default), which needs the
// guild it is in.
func mention(guild string, r Ref, word string) string {
	if r.ID == "" {
		return ""
	}
	switch r.Kind {
	case RefUser:
		return "<@" + r.ID + ">"
	case RefChannel:
		return "<#" + r.ID + ">"
	case RefRole:
		return "<@&" + r.ID + ">"
	case RefMessage:
		if ch, msg, ok := strings.Cut(r.ID, "/"); ok {
			if word == "" {
				word = "message"
			}
			return "[" + word + "](https://discord.com/channels/" + guild + "/" + ch + "/" + msg + ")"
		}
	}
	return ""
}

func buttons(bs []Button) *discord.ActionRowComponent {
	if len(bs) == 0 {
		return nil
	}
	items := make([]discord.InteractiveComponent, 0, len(bs))
	for _, b := range bs {
		switch {
		case b.URL != "":
			items = append(items, discord.NewLinkButton(b.Label, b.URL))
		case b.Danger:
			items = append(items, discord.NewDangerButton(b.Label, b.CustomID))
		default:
			items = append(items, discord.NewSecondaryButton(b.Label, b.CustomID))
		}
	}
	row := discord.NewActionRow(items...)
	return &row
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
