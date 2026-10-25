// Package support is what every bird says about backing the birds: the
// card that sends people to cygnet.lol/support, and the one that thanks a
// new supporter. It holds the words and the controls, never the layout: a
// bird lays a Page out in its own panel (its head, its art, its rule), so
// the cards look like the rest of that bird. Nothing here names a bird,
// so any of them can import it.
package support

import (
	"strings"

	"github.com/disgoorg/disgo/discord"
)

// URL is where somebody backs the birds.
const URL = "https://cygnet.lol/support"

// The icons a card asks its bird for, by key: one per perk and one per coin.
const (
	IconBadge    = "badge"
	IconPreviews = "previews"
	IconEarly    = "early"
	IconSay      = "say"
	IconUSDC     = "usdc"
	IconUSDT     = "usdt"
	IconSOL      = "sol"
)

// Icons is every key a card asks for, for a bird to check it has them all.
var Icons = []string{IconBadge, IconPreviews, IconEarly, IconSay, IconUSDC, IconUSDT, IconSOL}

// Brand is what a bird lends the words: its icons and whose work it is.
type Brand struct {
	// Inline is an icon written inline, <:name:id>, or "" while the bird
	// has none for key (before its emoji sync), when the line goes without.
	Inline func(key string) string
	// Button is the same icon for a button, or nil for none.
	Button func(key string) *discord.ComponentEmoji
	// Owner is the Discord user ID of whoever builds the birds, named on
	// the advert as a mention; "" leaves them as "one person".
	Owner string
}

func (b Brand) inline(key string) string {
	if b.Inline == nil {
		return ""
	}
	if e := b.Inline(key); e != "" {
		return e + " "
	}
	return ""
}

func (b Brand) button(key string) *discord.ComponentEmoji {
	if b.Button == nil {
		return nil
	}
	return b.Button(key)
}

// Page is one card's content, for a bird's own panel: Title and Line make
// the head, Parts is the page, Footer the closing line. Hero asks the bird
// to lead with its wide banner in place of the face beside the head.
type Page struct {
	Title, Line string
	Hero        bool
	Parts       []discord.ContainerSubComponent
	Footer      string
}

// Perk is one thing backing buys, as the advert lists it.
type Perk struct{ Icon, Name, Line string }

// Perks is what a supporter gets, the same four cygnet.lol/support lists.
// None of them is a bot feature: nothing a bird does is ever locked behind
// paying.
var Perks = []Perk{
	{IconBadge, "the role", "a supporter role and its badge in the birds' servers"},
	{IconPreviews, "previews", "preview builds of the bots before they ship"},
	{IconEarly, "early access", "to the alpha projects"},
	{IconSay, "a say", "in what gets built next"},
}

// fact is one subtext line: a bold key, then its values joined by " · ".
func fact(key string, values ...string) string {
	return "-# **" + key + "** " + strings.Join(values, " · ")
}

func text(s string) discord.TextDisplayComponent { return discord.TextDisplayComponent{Content: s} }

// Advert is the card that sends people to back the birds: who builds them,
// what backing buys, how it is paid, and the way there. It leads with the
// bird's banner.
func Advert(b Brand) Page {
	who := "one person"
	if b.Owner != "" {
		who = "one person, <@" + b.Owner + ">,"
	}
	perks := make([]string, len(Perks))
	for i, p := range Perks {
		perks[i] = b.inline(p.Icon) + "**" + p.Name + "** " + p.Line
	}
	coin := func(key string) string { return b.inline(key) + key }
	return Page{
		Title: b.inline(IconBadge) + "back the birds",
		Line:  "keep the bird bots flying",
		Hero:  true,
		Parts: []discord.ContainerSubComponent{
			text("the bird bots are built and run by " + who + " in their own time and out of their own pocket. backing them pays for that, and buys you:"),
			discord.NewLargeSeparator(),
			text(strings.Join(perks, "\n")),
			discord.NewLargeSeparator(),
			text(strings.Join([]string{
				fact("pay with", coin(IconUSDC), coin(IconUSDT), coin(IconSOL)),
				fact("terms", "a month or more, paid up front", "nothing renews by itself"),
				fact("promise", "nothing the bots do is ever locked behind it"),
			}, "\n")),
			discord.ActionRowComponent{Components: []discord.InteractiveComponent{link("back the birds", b.button(IconBadge))}},
		},
		Footer: "cygnet.lol/support",
	}
}

// Welcome is the card a bird posts when people start backing the birds:
// thanks is its own words for them, naming them, and the card offers the
// same way in to everybody reading.
func Welcome(b Brand, thanks string) Page {
	return Page{
		Title: b.inline(IconBadge) + "thank you",
		Line:  "a new supporter for the birds",
		Parts: []discord.ContainerSubComponent{
			text(thanks),
			discord.NewLargeSeparator(),
			text(strings.Join([]string{
				fact("perks", "a role", "preview builds", "a say"),
				fact("terms", "from a month, paid up front", "nothing renews by itself"),
			}, "\n")),
			discord.ActionRowComponent{Components: []discord.InteractiveComponent{link("back the birds too", b.button(IconBadge))}},
		},
		Footer: "cygnet.lol/support",
	}
}

func link(label string, emoji *discord.ComponentEmoji) discord.ButtonComponent {
	btn := discord.NewLinkButton(label, URL)
	btn.Emoji = emoji
	return btn
}
