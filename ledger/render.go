package ledger

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
)

// Brand is the face a bird's cards wear. The layout is the same for every
// bird; who it comes from and in what colours is not.
type Brand struct {
	// Name is the bird's, in the card's head.
	Name string
	// Emoji is the inline app emoji for a module's icon, or "" before the
	// emoji sync, when the head is text alone.
	Emoji func(module string) string
	// Color is the card's accent for a mood, from the bird's own palette.
	Color func(Mood) int
}

// maxText is Discord's limit on a text display's content, less room for
// the head.
const maxText = 3900

// Render draws entries as one card in the UX.md card form: one Components
// V2 container accented by mood, its head the module's emoji, the bird's
// name in bold and the module. One entry is the full card: summary, who and
// where as mentions, a grid of facts and changes, then who did it, why and
// what it followed from as subtext, then its buttons. Several entries (a
// batch, see Batcher) are one line each under a shared head, accented by the
// loudest of them, with their common actor and reason once below.
func Render(b Brand, entries ...Entry) discord.ContainerComponent {
	if len(entries) == 0 {
		return discord.NewContainer()
	}
	first := entries[0]
	head := "**" + b.Name + "** · " + first.Module()
	if b.Emoji != nil {
		if e := b.Emoji(first.Module()); e != "" {
			head = e + " " + head
		}
	}
	mood := first.Mood
	var body string
	var extra []discord.ContainerSubComponent
	if len(entries) == 1 {
		body = single(first)
		if row := buttons(first.Buttons); row != nil {
			extra = append(extra, *row)
		}
	} else {
		head += fmt.Sprintf(" · %d", len(entries))
		body = batch(entries)
		for _, e := range entries[1:] {
			mood = louder(mood, e.Mood)
		}
	}
	color := 0
	if b.Color != nil {
		color = b.Color(mood)
	}
	parts := append([]discord.ContainerSubComponent{discord.NewTextDisplay(clipText(head + "\n" + body))}, extra...)
	return discord.NewContainer(parts...).WithAccentColor(color)
}

func single(e Entry) string {
	lines := []string{e.Summary}
	if s := subjects(e.Guild, e.Subjects); s != "" {
		lines = append(lines, s)
	}
	if g := grid(e.Facts, e.Changes); g != "" {
		lines = append(lines, "```\n"+g+"```")
	}
	return strings.Join(append(lines, footer(e.Actor, e.Reason, e.Cause)...), "\n")
}

// batch is one line per entry, then whatever they share. The Batcher only
// batches entries with the same action and actor; a reason or cause they do
// not all share stays on each entry's row in the store.
func batch(entries []Entry) string {
	lines := make([]string, 0, len(entries)+2)
	reason, cause := entries[0].Reason, entries[0].Cause
	for _, e := range entries {
		line := e.Summary
		// One line per entry: only its who and where, not its labelled refs.
		if s := subjects(e.Guild, unlabelled(e.Subjects)); s != "" {
			line = s + " · " + line
		}
		lines = append(lines, line)
		if e.Reason != reason {
			reason = ""
		}
		if e.Cause != cause {
			cause = ""
		}
	}
	return strings.Join(append(lines, footer(entries[0].Actor, reason, cause)...), "\n")
}

func footer(a Actor, reason, cause string) []string {
	by := "-# automatic"
	if a.Kind == ActorMember {
		by = "-# by " + mention("", User(a.ID))
	}
	out := []string{by}
	if reason != "" {
		out = append(out, "-# reason: "+oneLine(reason))
	}
	if cause != "" {
		out = append(out, "-# follows "+cause)
	}
	return out
}

// subjects is the refs as lines: the unlabelled ones together, then one
// line per label in the order labels first appear.
func subjects(guild string, refs []Ref) string {
	var plain []string
	var labels []string
	byLabel := map[string][]string{}
	for _, r := range refs {
		m := mention(guild, r)
		switch {
		case m == "":
		case r.Label == "":
			plain = append(plain, m)
		default:
			if _, ok := byLabel[r.Label]; !ok {
				labels = append(labels, r.Label)
			}
			byLabel[r.Label] = append(byLabel[r.Label], m)
		}
	}
	var lines []string
	if len(plain) > 0 {
		lines = append(lines, strings.Join(plain, " · "))
	}
	for _, l := range labels {
		lines = append(lines, l+": "+strings.Join(byLabel[l], ", "))
	}
	return strings.Join(lines, "\n")
}

// mention is how a Ref reads in Discord, or "" for one it cannot draw. A
// message is a link, which needs the guild it is in.
func mention(guild string, r Ref) string {
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
			return "[message](https://discord.com/channels/" + guild + "/" + ch + "/" + msg + ")"
		}
	}
	return ""
}

// grid is the facts and changes as UX.md's code block grid: the label
// column the longest label plus two spaces, each value wrapped to fit the
// rest of gridWidth, continuation lines indented under the value.
func grid(facts []Fact, changes []Change) string {
	type row struct{ label, value string }
	rows := make([]row, 0, len(facts)+len(changes))
	for _, f := range facts {
		rows = append(rows, row{f.Label, f.Value})
	}
	for _, c := range changes {
		rows = append(rows, row{c.Field, c.From + " → " + c.To})
	}
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, r := range rows {
		width = max(width, utf8.RuneCountInString(r.label))
	}
	width += 2
	var b strings.Builder
	for _, r := range rows {
		for i, line := range wrap(r.value, gridWidth-width) {
			label := ""
			if i == 0 {
				label = r.label
			}
			b.WriteString(label + strings.Repeat(" ", width-utf8.RuneCountInString(label)) + line + "\n")
		}
	}
	return b.String()
}

// wrap breaks s into lines of at most n runes: whole comma separated items
// packed onto a line while they fit, breaking after the comma, the way a
// list reads. An item longer than a line is itself packed by words, and a
// word longer than a line is cut, so nothing ever runs past the grid.
func wrap(s string, width int) []string {
	// One column is kept for the comma a break leaves at the end of a line.
	n := width - 1
	var lines []string
	line := ""
	put := func(piece, sep string) {
		switch {
		case line == "":
			line = piece
		case utf8.RuneCountInString(line+sep+piece) <= n:
			line += sep + piece
		default:
			if sep == ", " {
				line += ","
			}
			lines = append(lines, line)
			line = piece
		}
	}
	for i, item := range strings.Split(s, ", ") {
		sep := ", "
		if i == 0 {
			sep = ""
		}
		if utf8.RuneCountInString(item) <= n {
			put(item, sep)
			continue
		}
		for _, word := range strings.Fields(item) {
			for utf8.RuneCountInString(word) > n {
				r := []rune(word)
				put(string(r[:n]), sep)
				word, sep = string(r[n:]), " "
			}
			put(word, sep)
			sep = " "
		}
	}
	return append(lines, line)
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

// clipText keeps a card under Discord's limit. Only a batch at its cap with
// long lines gets near it.
func clipText(s string) string { return clip(s, maxText) }

func unlabelled(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		if r.Label == "" {
			out = append(out, r)
		}
	}
	return out
}
