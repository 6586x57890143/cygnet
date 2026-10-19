// Package brand is cygnet's palette, mood icons and emoji. The mood is
// derived from the embed's colour rather than passed beside it, so the two
// cannot disagree. Once Sync has run an icon is an application emoji on
// Discord's CDN and is not uploaded with a reply; before that, it is an
// attachment derived from the finished embed, so an attachment:// URL never
// points at nothing.
package brand

import (
	"bytes"
	"embed"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
)

// The palette is the avatar's: its navy field, dusk sky and the down, and
// muted signal colours that match the badge on each icon.
const (
	ColorPrimary = 0x38425F
	ColorInfo    = 0x7C889E
	ColorOK      = 0x7FA08C
	ColorWarn    = 0xD1A06A
	ColorError   = 0xC0626A
	ColorNotice  = 0x8F96B8
	ColorIdle    = 0x928688
)

//go:embed assets/*.png
var assets embed.FS

var moods = map[int]string{
	ColorPrimary: "notice",
	ColorNotice:  "notice",
	ColorInfo:    "info",
	ColorOK:      "ok",
	ColorWarn:    "warn",
	ColorError:   "error",
	ColorIdle:    "idle",
}

// Embed returns a record (DESIGN.md's A record) in color: an embed wearing
// that colour's mood as its thumbnail and the rule as its image, and the
// files to send with it. The rule is always attached, being too wide for an
// emoji; the face is too until the emoji are synced. An unknown colour gets
// "info": never claim success for something unrecognised. The title leads
// with module's icon once the emoji are synced, and the footer with its
// name; Note adds to the footer.
func Embed(color int, module, title, description string) (discord.Embed, []*discord.File) {
	face, faceURL := Icon(color)
	rule, ruleURL := asset("cygnet_rule")
	files := []*discord.File{rule}
	if face != nil {
		files = append(files, face)
	}
	if e := Mention("mod_" + module); e != "" {
		title = e + " " + title
	}
	return discord.Embed{
		Title:       title,
		Description: description,
		Color:       color,
		Thumbnail:   &discord.EmbedResource{URL: faceURL},
		Image:       &discord.EmbedResource{URL: ruleURL},
		Footer:      &discord.EmbedFooter{Text: module},
	}, files
}

// Note adds s to e's footer, after what is there.
func Note(e *discord.Embed, s string) {
	if e.Footer == nil || e.Footer.Text == "" {
		e.Footer = &discord.EmbedFooter{Text: s}
		return
	}
	e.Footer = &discord.EmbedFooter{Text: e.Footer.Text + " · " + s}
}

// The record grid, as ledger.Render lays out a log card, so cygnet's
// records and her log line up the same way.
const (
	// columns is how many inline fields Discord sets in a row. It divides
	// each row by the fields in it, so a row of two sits in halves under a
	// row of three in thirds; every row is padded to three.
	columns = 3
	// columnWidth is the longest value, as the reader sees it, that fits a
	// third of an embed beside its thumbnail on one line. A longer one wraps
	// into a tall strip that drags its row out of line.
	columnWidth = 16
	maxFields   = 25
)

// Fields is sections of rows as a record's summary list (DESIGN.md's grid):
// a key over its value, all lowercase. Each section starts a row of its
// own, its short values spread evenly over whole rows of three (four as
// two and two, five as three and two) padded with blank fields, and the
// values too wide for a column under it at full width.
func Fields(sections ...[][2]string) []discord.EmbedField {
	var out []discord.EmbedField
	for _, s := range sections {
		var cols, wide []discord.EmbedField
		for _, r := range s {
			f := field(r[0], r[1])
			if *f.Inline {
				cols = append(cols, f)
			} else {
				wide = append(wide, f)
			}
		}
		total := len(cols)
		rows := (total + columns - 1) / columns
		for r := range rows {
			// Rows before the remainder take one more: 4 is 2+2, 5 is 3+2
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

var (
	link   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markup = regexp.MustCompile(`<(@&|@!?|#)\d+>`)
)

// field is one fact, in a column when what the reader sees of its value
// fits one: a link shows as its words and a mention as a pill about a name
// wide, not as the markup behind them. The actor is always a column, as on
// a log card: who did it heads the grid, and "cygnet (automatic)" is only
// just over the width.
func field(name, value string) discord.EmbedField {
	seen := markup.ReplaceAllString(link.ReplaceAllString(value, "$1"), "@abcdefghij")
	inline := name == "actor" || utf8.RuneCountInString(seen) <= columnWidth && !strings.Contains(value, "\n")
	return discord.EmbedField{Name: name, Value: value, Inline: &inline}
}

// zwsp is a zero width space, built from its code point so the source holds
// nothing invisible.
var zwsp = string(rune(0x200b))

// blank is an empty column holding a row's place. A zero width space, since
// Discord refuses an empty name or value.
func blank() discord.EmbedField {
	inline := true
	return discord.EmbedField{Name: zwsp, Value: zwsp, Inline: &inline}
}

// Icon is color's mood icon for a component that shows it: its emoji's URL
// and a nil file once synced, otherwise the file and the attachment:// URL
// that points at it. Unknown colours get "info", as in Embed.
func Icon(color int) (*discord.File, string) {
	mood, ok := moods[color]
	if !ok {
		mood = "info"
	}
	return asset("cygnet_" + mood)
}

// Art is one of cygnet's application emoji as its PNG: the bird alone is
// "cygnet_avatar". Nil for a name that has none.
func Art(key string) []byte {
	data, _ := emojiArt.ReadFile("emoji/" + key + ".png")
	return data
}

// Avatar is the bird alone, with no badge, as Icon gives it.
func Avatar() (*discord.File, string) { return asset("cygnet_avatar") }

// ModuleIcon is module's own icon, as Icon gives it, or the mood icon of
// color when the module has none.
func ModuleIcon(module string, color int) (*discord.File, string) {
	key := "mod_" + module
	if _, err := emojiArt.Open("emoji/" + key + ".png"); err != nil {
		return Icon(color)
	}
	if url, ok := emojiURL(key); ok {
		return nil, url
	}
	data, _ := emojiArt.ReadFile("emoji/" + key + ".png")
	return discord.NewFile(key+".png", "", bytes.NewReader(data)), "attachment://" + key + ".png"
}

func asset(key string) (*discord.File, string) {
	if url, ok := emojiURL(key); ok {
		return nil, url
	}
	name := key + ".png"
	data, _ := assets.ReadFile("assets/" + name) // embedded; cannot fail for a listed name
	return discord.NewFile(name, "", bytes.NewReader(data)), "attachment://" + name
}
