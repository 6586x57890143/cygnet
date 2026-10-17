package ledger

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
)

// Brand is the face a bird's cards wear. The layout is the same for every
// bird; whose face and colours it wears is not. There is no name: Discord
// already names the bird above every message it sends.
type Brand struct {
	// Emoji is the inline app emoji for a module's icon, or "" before the
	// emoji sync, when the title is text alone.
	Emoji func(module string) string
	// Color is the card's accent for a mood, from the bird's own palette.
	Color func(Mood) int
	// Thumbnail is the URL of the bird's drawing for a mood, shown beside
	// the title, or "" for none. The bird uploads what it names.
	Thumbnail func(Mood) string
	// Rule is the URL of a wide divider image closing the card, or "" for
	// none. It is there for its width as much as its look: a container is
	// sized to its content, so without one a two line card and a long one
	// render as boxes of every width down one channel.
	Rule string
}

// maxText is Discord's limit on a message's text, less room for the rest.
const maxText = 3900

// Render draws entries as one card, the log card in DESIGN.md:
//
//	<emoji> **summary**                 [face]
//	-# by @actor · module
//	> the reason, in the actor's words
//	@who · #where
//	**label** labelled refs
//	**label** a fact
//	**field** from → to
//	[rule]
//	-# follows <cause>
//	[buttons]
//
// Several entries (a batch, see Batcher) are one line each under the
// shared title with their count, accented by the loudest of them, their
// shared reason quoted once.
func Render(b Brand, entries ...Entry) discord.ContainerComponent {
	if len(entries) == 0 {
		return discord.NewContainer()
	}
	first := entries[0]
	mood := first.Mood
	for _, e := range entries[1:] {
		mood = louder(mood, e.Mood)
	}
	title := first.Summary
	if len(entries) > 1 {
		title += fmt.Sprintf(" · %d", len(entries))
	}
	title = "**" + title + "**"
	if b.Emoji != nil {
		if e := b.Emoji(first.Module()); e != "" {
			title = e + " " + title
		}
	}
	head := title + "\n-# " + by(first.Actor) + " · " + first.Module()

	var body, foot string
	if len(entries) == 1 {
		body = single(first)
		if first.Cause != "" {
			foot = "-# follows " + first.Cause
		}
	} else {
		body, foot = batch(entries)
	}

	thumb := ""
	if b.Thumbnail != nil {
		thumb = b.Thumbnail(mood)
	}
	parts := []discord.ContainerSubComponent{discord.NewTextDisplay(head)}
	if thumb != "" {
		parts[0] = discord.NewSection(discord.NewTextDisplay(head)).WithAccessory(discord.NewThumbnail(thumb))
	}
	if body != "" {
		parts = append(parts, discord.NewTextDisplay(clip(body, maxText-len(head)-len(foot))))
	}
	if b.Rule != "" {
		parts = append(parts, discord.NewMediaGallery(discord.MediaGalleryItem{Media: discord.UnfurledMediaItem{URL: b.Rule}}))
	}
	if foot != "" {
		parts = append(parts, discord.NewTextDisplay(foot))
	}
	if row := buttons(first.Buttons); row != nil && len(entries) == 1 {
		parts = append(parts, *row)
	}
	color := 0
	if b.Color != nil {
		color = b.Color(mood)
	}
	return discord.NewContainer(parts...).WithAccentColor(color)
}

func single(e Entry) string {
	var lines []string
	if e.Reason != "" {
		lines = append(lines, quote(e.Reason))
	}
	if s := subjects(e.Guild, e.Subjects); s != "" {
		lines = append(lines, s)
	}
	return strings.Join(append(lines, facts(e.Facts, e.Changes)...), "\n")
}

// batch is the shared reason, then one line per entry: its who and where,
// and its summary only where it says something the title does not. The
// Batcher only batches entries with the same action and actor; a reason or
// cause they do not all share stays on each entry's row in the store.
func batch(entries []Entry) (body, foot string) {
	reason, cause := entries[0].Reason, entries[0].Cause
	var lines []string
	for _, e := range entries {
		line := subjects(e.Guild, unlabelled(e.Subjects))
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
		lines = append([]string{quote(reason)}, lines...)
	}
	if cause != "" {
		foot = "-# follows " + cause
	}
	return strings.Join(lines, "\n"), foot
}

func by(a Actor) string {
	if a.Kind == ActorMember {
		return "by " + mention("", User(a.ID))
	}
	return "automatic"
}

// quote is somebody's own words as a block quote, the way the embeds this
// replaced set a reason apart: on one line, since a quote's every line
// needs its own marker.
func quote(s string) string { return "> " + oneLine(s) }

// subjects is the refs as lines: the unlabelled ones together, then one
// line per label in the order labels first appear, labelled the way a fact
// is.
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
		lines = append(lines, "**"+l+"** "+strings.Join(byLabel[l], ", "))
	}
	return strings.Join(lines, "\n")
}

// facts is one line a fact, its label in bold: plain text, so a value wraps
// the way any line does on whatever is reading it and a mention or a link
// in it works. Not a code block, which is for what somebody copies.
func facts(fs []Fact, cs []Change) []string {
	lines := make([]string, 0, len(fs)+len(cs))
	for _, f := range fs {
		lines = append(lines, "**"+f.Label+"** "+f.Value)
	}
	for _, c := range cs {
		lines = append(lines, "**"+c.Field+"** "+c.From+" → "+c.To)
	}
	return lines
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

func unlabelled(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		if r.Label == "" {
			out = append(out, r)
		}
	}
	return out
}
