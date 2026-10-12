package invite

import (
	"context"
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/store"
)

// treeRoom is how much of a card the tree's lines may take: Discord
// allows 4000 characters of text in a message, and the head and counts
// need the rest.
const treeRoom = 3400

// tree is the tree card (SPEC.md, What an admin sees): every join cygnet
// saw in g and its cause. One line a member who brought someone in, naming
// whoever they brought, so each member's line back is there to follow
// without a glyph that wraps on a phone. A join she can't trace is shown
// as such, never guessed.
func (m *Module) tree(ctx context.Context, g discord.Guild) (discord.ContainerComponent, error) {
	joins, err := m.store.Tree(ctx, g.ID)
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	by := map[snowflake.ID][]store.Join{}
	var order []snowflake.ID
	var untraced []store.Join
	traced, gone := 0, 0
	for _, j := range joins {
		if !j.Left.IsZero() {
			gone++
		}
		if j.Sponsor == 0 {
			untraced = append(untraced, j)
			continue
		}
		traced++
		if _, seen := by[j.Sponsor]; !seen {
			order = append(order, j.Sponsor)
		}
		by[j.Sponsor] = append(by[j.Sponsor], j)
	}
	var b strings.Builder
	b.WriteString(core.Readout([][2]string{
		{"joined", fmt.Sprintf("%d since cygnet watched", len(joins))},
		{"by a link", fmt.Sprint(traced)},
		{"untraced", fmt.Sprint(len(untraced))},
		{"left since", fmt.Sprint(gone)},
	}))
	var lines []string
	for _, s := range order {
		lines = append(lines, fmt.Sprintf("<@%d> brought %s", s, names(by[s])))
	}
	if len(untraced) > 0 {
		lines = append(lines, "-# untraced: "+names(untraced))
	}
	if len(joins) == 0 {
		lines = append(lines, "-# nobody has joined since cygnet started watching; members from before have no line")
	}
	// ponytail: cut at the card's room, so a server with hundreds of joins
	// shows its first; pages, or the tree on the web, when one needs more.
	used := 0
	for i, l := range lines {
		if used+len(l) > treeRoom {
			fmt.Fprintf(&b, "-# and %d more lines\n", len(lines)-i)
			break
		}
		b.WriteString(l + "\n")
		used += len(l) + 1
	}
	b.WriteString("-# a member brought in by a link hangs from whoever made it; one who left is struck through")
	back := discord.NewActionRow(discord.NewSecondaryButton("◂ back", fmt.Sprintf("%s:%s", id, g.ID)))
	return brand.Card(brand.ColorInfo, "invite", "the tree", b.String(), discord.NewSeparator(discord.SeparatorSpacingSizeSmall), back), nil
}

// names is who joined, as mentions, in the order they did; one who left
// struck through.
func names(js []store.Join) string {
	out := make([]string, len(js))
	for i, j := range js {
		out[i] = fmt.Sprintf("<@%d>", j.User)
		if !j.Left.IsZero() {
			out[i] = "~~" + out[i] + "~~"
		}
	}
	return strings.Join(out, " · ")
}
