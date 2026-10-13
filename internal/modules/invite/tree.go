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

const (
	// treeRoom is how much of a card one page of the tree's lines may take:
	// Discord allows 4000 characters of text in a message, and the head,
	// the counts and the footer need the rest.
	treeRoom = 3400
	// perLine is the most members one line names; one who brought more
	// goes on over more lines, so no line outgrows a page.
	perLine = 60
)

// tree is the tree card (SPEC.md, What an admin sees): every join cygnet
// saw in g and its cause. One line a member who brought someone in, naming
// whoever they brought, so each member's line back is there to follow
// without a glyph that wraps on a phone. A join she can't trace is shown
// as such, never guessed. Past one card it goes on over pages, page
// counted from 0.
func (m *Module) tree(ctx context.Context, g discord.Guild, page int) (discord.ContainerComponent, error) {
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
		for i, chunk := range chunks(by[s]) {
			verb := "brought"
			if i > 0 {
				verb = "also brought"
			}
			lines = append(lines, fmt.Sprintf("<@%d> %s %s", s, verb, names(chunk)))
		}
	}
	for i, chunk := range chunks(untraced) {
		lead := "-# untraced: "
		if i > 0 {
			lead = "-# untraced, more: "
		}
		lines = append(lines, lead+names(chunk))
	}
	if len(joins) == 0 {
		lines = append(lines, "-# nobody has joined since cygnet started watching; members from before have no line")
	}
	pages := paginate(lines)
	page = min(max(page, 0), len(pages)-1)
	for _, l := range pages[page] {
		b.WriteString(l + "\n")
	}
	if len(pages) > 1 {
		fmt.Fprintf(&b, "-# page %d of %d\n", page+1, len(pages))
	}
	b.WriteString("-# a member brought in by a link hangs from whoever made it; one who left is struck through")
	at := func(p int) string { return fmt.Sprintf("%s:tree:%d:%s", id, p, g.ID) }
	row := discord.NewActionRow(discord.NewSecondaryButton("◂ back", fmt.Sprintf("%s:%s", id, g.ID)))
	if len(pages) > 1 {
		prev, next := discord.NewSecondaryButton("◂ earlier", at(page-1)), discord.NewSecondaryButton("later ▸", at(page+1))
		if page == 0 {
			prev = prev.AsDisabled()
		}
		if page == len(pages)-1 {
			next = next.AsDisabled()
		}
		row = discord.NewActionRow(prev, next, row.Components[0])
	}
	return brand.Card(brand.ColorInfo, "invite", "the tree", b.String(), discord.NewSeparator(discord.SeparatorSpacingSizeSmall), row), nil
}

// chunks is js in runs of at most perLine.
func chunks(js []store.Join) [][]store.Join {
	var out [][]store.Join
	for len(js) > perLine {
		out = append(out, js[:perLine])
		js = js[perLine:]
	}
	if len(js) > 0 {
		out = append(out, js)
	}
	return out
}

// paginate packs lines, in order, into pages of at most treeRoom
// characters each; there is always at least one page.
func paginate(lines []string) [][]string {
	pages := [][]string{nil}
	used := 0
	for _, l := range lines {
		if used > 0 && used+len(l)+1 > treeRoom {
			pages, used = append(pages, nil), 0
		}
		pages[len(pages)-1] = append(pages[len(pages)-1], l)
		used += len(l) + 1
	}
	return pages
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
