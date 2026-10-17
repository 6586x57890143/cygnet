package ledger

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
)

// Brand is the face a bird's cards wear. The layout is the same for every
// bird; whose face and colours it wears is not.
type Brand struct {
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
// width a value can have and still read as a column.
const (
	maxDescription = 4000
	maxFieldValue  = 1000
	maxFields      = 25
	// inlineWidth is the longest value set in a column: past it, three
	// abreast wrap into a ragged block, so a longer one gets the row.
	inlineWidth = 24
)

// Render draws entries as one log card (DESIGN.md's Log cards).
//
// An embed, not a Components V2 container, because a log card is a record
// read by its facts, and embed fields are the one thing Discord lays out as
// a description list: a short label over a short value, three abreast where
// there is room and stacked on a phone. A container can only stack lines,
// which reads as a form rather than a record.
//
//	**summary**                                 [face]
//	> the reason, in the actor's words
//	actor        member        channel
//	automatic    @who          #where
//	policy       confidence
//	hate speech  ▰▰▰▰▱ 80%
//	message
//	deleted → reposted
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
	e := discord.Embed{Title: first.Summary}
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
	if len(entries) == 1 {
		if first.Reason != "" {
			e.Description = quote(first.Reason)
		}
		e.Fields = fields(first)
		card.Buttons = buttons(first.Buttons)
	} else {
		e.Title += fmt.Sprintf(" · %d", len(entries))
		e.Description, cause = batch(entries)
		e.Fields = []discord.EmbedField{field("actor", by(first.Actor))}
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
// what the reader asks first: who did it and to whom and where, then the
// facts of the action, then the messages it touched.
func fields(e Entry) []discord.EmbedField {
	out := []discord.EmbedField{field("actor", by(e.Actor))}
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
			out = append(out, field(plural(k.label, len(ms)), strings.Join(ms, ", ")))
		}
	}
	var labels, messages []string
	byLabel := map[string][]string{}
	for _, r := range e.Subjects {
		switch {
		case r.Kind == RefMessage:
			messages = append(messages, mention(e.Guild, r, r.Label))
		case r.Label != "":
			if _, ok := byLabel[r.Label]; !ok {
				labels = append(labels, r.Label)
			}
			byLabel[r.Label] = append(byLabel[r.Label], mention(e.Guild, r, ""))
		}
	}
	for _, l := range labels {
		out = append(out, field(l, strings.Join(byLabel[l], ", ")))
	}
	for _, f := range e.Facts {
		out = append(out, field(f.Label, f.Value))
	}
	for _, c := range e.Changes {
		out = append(out, field(c.Field, c.From+" → "+c.To))
	}
	// The messages last, as one field read in order: a message removed and
	// its repost are one thing that happened, so "deleted → reposted", each
	// word its link.
	if len(messages) > 0 {
		out = append(out, field("message", strings.Join(messages, " → ")))
	}
	return out[:min(len(out), maxFields)]
}

var (
	link   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markup = regexp.MustCompile(`<(@&|@!?|#)\d+>`)
)

// field is one entry of the list, in a column when what the reader sees of
// its value is short enough to read as one: a link shows as its words and a
// mention as a name, not as the markup behind them.
func field(name, value string) discord.EmbedField {
	value = clip(value, maxFieldValue)
	seen := markup.ReplaceAllString(link.ReplaceAllString(value, "$1"), "@a member name")
	inline := utf8.RuneCountInString(seen) <= inlineWidth
	return discord.EmbedField{Name: name, Value: value, Inline: &inline}
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

func by(a Actor) string {
	if a.Kind == ActorMember {
		return mention("", User(a.ID), "")
	}
	return "automatic"
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
