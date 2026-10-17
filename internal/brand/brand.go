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
// "info": never claim success for something unrecognised.
func Embed(color int, title, description string) (discord.Embed, []*discord.File) {
	face, faceURL := Icon(color)
	rule, ruleURL := asset("cygnet_rule")
	files := []*discord.File{rule}
	if face != nil {
		files = append(files, face)
	}
	return discord.Embed{
		Title:       title,
		Description: description,
		Color:       color,
		Thumbnail:   &discord.EmbedResource{URL: faceURL},
		Image:       &discord.EmbedResource{URL: ruleURL},
	}, files
}

// inlineWidth is the longest value set in a column: past it, three abreast
// wrap into a ragged block, so a longer one takes the row.
const inlineWidth = 24

// Fields is rows as a record's summary list: a key over its value, three
// abreast where every line of the values is short, in the order given.
func Fields(rows [][2]string) []discord.EmbedField {
	out := make([]discord.EmbedField, 0, len(rows))
	for _, r := range rows {
		inline := true
		for line := range strings.SplitSeq(r[1], "\n") {
			inline = inline && utf8.RuneCountInString(line) <= inlineWidth
		}
		out = append(out, discord.EmbedField{Name: r[0], Value: r[1], Inline: &inline})
	}
	return out
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
