package support

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
)

// button is the page's one link button.
func button(t *testing.T, p Page) discord.ButtonComponent {
	t.Helper()
	for _, part := range p.Parts {
		if row, ok := part.(discord.ActionRowComponent); ok {
			for _, c := range row.Components {
				if b, ok := c.(discord.ButtonComponent); ok && b.Style == discord.ButtonStyleLink {
					return b
				}
			}
		}
	}
	t.Fatalf("%q has no link button", p.Title)
	return discord.ButtonComponent{}
}

func body(p Page) string {
	var out []string
	for _, part := range p.Parts {
		if t, ok := part.(discord.TextDisplayComponent); ok {
			out = append(out, t.Content)
		}
	}
	return strings.Join(out, "\n")
}

// icons answers every key with a marker naming it, so a test can see
// which icon went where.
var icons = Brand{
	Inline: func(key string) string { return "<:" + key + ":1>" },
	Button: func(key string) *discord.ComponentEmoji { return &discord.ComponentEmoji{Name: key} },
	Owner:  "175928847299117063",
}

func TestTheAdvertListsEveryPerkAndTheWayThere(t *testing.T) {
	a := Advert(icons)
	if !a.Hero || a.Line == "" || button(t, a).URL != URL || button(t, a).Emoji.Name != IconBadge {
		t.Fatalf("advert %+v", a)
	}
	got := body(a)
	for _, p := range Perks {
		if !strings.Contains(got, "<:"+p.Icon+":1> **"+p.Name+"** "+p.Line) {
			t.Errorf("the advert leaves out %q or its icon", p.Name)
		}
	}
	for _, want := range []string{"<:usdc:1> usdc · <:usdt:1> usdt · <:sol:1> sol", "nothing renews by itself", "one person, <@175928847299117063>,"} {
		if !strings.Contains(got, want) {
			t.Errorf("the advert lacks %q", want)
		}
	}
}

// Before a bird's emoji sync, or for a bird with none, the words stand
// alone with no stray spaces, and nobody is named who wasn't given.
func TestTheAdvertReadsWithoutIcons(t *testing.T) {
	a := Advert(Brand{})
	got := body(a)
	if a.Title != "back the birds" || !strings.Contains(got, "**the role** a supporter") || !strings.Contains(got, "usdc · usdt · sol") || !strings.Contains(got, "by one person in") {
		t.Fatalf("without icons: %q / %q", a.Title, got)
	}
	if button(t, a).Emoji != nil {
		t.Fatal("a button emoji without a brand")
	}
}

func TestTheWelcomeThanksAndOffersTheWayIn(t *testing.T) {
	w := Welcome(icons, "thank you, <@1>")
	if w.Hero || !strings.Contains(body(w), "thank you, <@1>") || button(t, w).URL != URL {
		t.Fatalf("welcome %+v", w)
	}
}

// Every bird imports this, so nothing in it may name one, and the words
// keep the house rules: lowercase, plain punctuation.
func TestNothingNamesABirdOrBreaksTheHouseStyle(t *testing.T) {
	for _, p := range []Page{Advert(Brand{}), Welcome(Brand{}, "")} {
		all := strings.Join([]string{p.Title, p.Line, body(p), p.Footer, button(t, p).Label}, "\n")
		for _, bird := range []string{"merlin", "peregrine", "skua", "cygnet "} {
			if strings.Contains(strings.ToLower(all), bird) {
				t.Errorf("%q names %s", p.Title, bird)
			}
		}
		if strings.ContainsAny(all, "\u2014\u2026\u2018\u2019\u201c\u201d") {
			t.Errorf("%q has an em dash, ellipsis or curly quote", p.Title)
		}
		if strings.ToLower(all) != all {
			t.Errorf("%q isn't lowercase", p.Title)
		}
	}
}

func TestEveryPerkIconIsListed(t *testing.T) {
	for _, p := range Perks {
		found := false
		for _, k := range Icons {
			found = found || k == p.Icon
		}
		if !found {
			t.Errorf("%q's icon %q isn't in Icons", p.Name, p.Icon)
		}
	}
}

func TestTheAdvertUsesOnlyTheCurrentPaymentMethods(t *testing.T) {
	b := icons
	b.Methods = []Method{{Key: "base:ETH"}, {Key: "ethereum:ETH"}, {Key: "monero:XMR"}, {Key: "bad:<@everyone>"}, {Key: "invalid"}}
	got := body(Advert(b))
	if !strings.Contains(got, "<:eth:1> eth · <:xmr:1> xmr") || strings.Contains(got, "<:usdc:") || strings.Contains(got, "@everyone") {
		t.Fatalf("wrong payment choices: %s", got)
	}
	b.Methods = []Method{}
	if !strings.Contains(body(Advert(b)), "see the checkout for available currencies") {
		t.Fatal("empty offer advertised a payment method")
	}
}
