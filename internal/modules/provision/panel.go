package provision

import (
	"context"
	"slices"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
)

// The panel's custom IDs: "provision" draws it, "provision:mode" is the
// select that sets how far cygnet goes on her own.
const (
	homeID = "provision"
	modeID = homeID + ":mode"
)

// what is what cygnet does in each mode when the server drifts.
var what = map[string]string{
	Watch:   "says so in #%s",
	Propose: "says so in #%s, with a button to put it back",
	Act:     "puts it back and says what she did in #%s",
}

// open is /provision: the panel, which reads nothing from Discord, so it
// opens at once.
func (m *Module) open(_ context.Context, e *events.ApplicationCommandInteractionCreate) error {
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{m.panel(*e.GuildID(), "")},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// home answers the back button and the mode select. A card posted in a
// channel, such as a drift report, stays as it is: whoever pressed it gets
// their own panel.
func (m *Module) home(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if e.GuildID() == nil || !m.admin(e) {
		return core.Tell("only this server's admins can use /provision")
	}
	guild := *e.GuildID()
	note := ""
	if e.Data.CustomID() == modeID {
		v := e.StringSelectMenuInteractionData().Values
		kept, ok := m.kept.Load(guild)
		switch {
		case !ok:
			return core.Tell("apply a layout here first; cygnet watches only what she set up")
		case len(v) != 1 || !slices.Contains(modes, v[0]):
			return core.Tell("pick watch, propose or act")
		}
		k := kept.(Kept)
		k.Mode = v[0]
		m.store(ctx, guild, k)
		note = "✓ saved"
	}
	card := m.panel(guild, note)
	if !e.Message.Flags.Has(discord.MessageFlagEphemeral) {
		return e.CreateMessage(discord.MessageCreate{
			Components:      []discord.LayoutComponent{card},
			Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
			AllowedMentions: core.NoPings(),
		})
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

// panel is the server as cygnet keeps it, and the way to everything else:
// the fleet, a layout's preview, a fresh start and how far she goes when
// it drifts.
func (m *Module) panel(guild snowflake.ID, note string) discord.ContainerComponent {
	l, k, kept := layouts[0], Kept{}, false
	if v, ok := m.kept.Load(guild); ok {
		k, kept = v.(Kept), true
		if have, ok := layout(k.Layout); ok {
			l = have
		}
	}
	var b strings.Builder
	if note != "" {
		b.WriteString(note + "\n")
	}
	if kept {
		b.WriteString(core.Readout([][2]string{{"layout", l.Name}, {"drift", k.Mode}}))
		b.WriteString("-# when it drifts from " + l.Name + ", cygnet " + strings.Replace(what[k.Mode], "%s", l.Reports, 1))
	} else {
		b.WriteString(core.Readout([][2]string{{"layout", "none yet"}}))
		b.WriteString("-# once a layout is applied here, cygnet keeps the server to it")
	}
	b.WriteString("\n-# fleet is every bird and what it's missing; layout shows what " + l.Name + " would change before anything does; start fresh also clears out what it doesn't name")
	// ponytail: one layout, so one button; a select once there are more.
	buttons := discord.NewActionRow(
		discord.NewPrimaryButton("layout", previewID+":"+l.Name),
		discord.NewSecondaryButton("fleet", refresh),
		discord.NewSecondaryButton("start fresh", previewID+":"+l.Name+":fresh"),
	)
	extra := []discord.ContainerSubComponent{discord.NewSeparator(discord.SeparatorSpacingSizeSmall), buttons}
	if kept {
		opts := make([]discord.StringSelectMenuOption, len(modes))
		for i, md := range modes {
			opts[i] = discord.StringSelectMenuOption{Label: md, Value: md, Description: strings.Replace(what[md], "%s", l.Reports, 1), Default: md == k.Mode}
		}
		extra = append(extra, discord.NewActionRow(discord.NewStringSelectMenu(modeID, "when it drifts", opts...)))
	}
	return brand.Card(brand.ColorInfo, "provision", "", b.String(), extra...)
}

// withBack puts the button back to the panel on card, beside its last
// buttons when there's room.
func withBack(card discord.ContainerComponent) discord.ContainerComponent {
	back := discord.NewSecondaryButton("◂ back", homeID)
	if n := len(card.Components); n > 0 {
		if row, ok := card.Components[n-1].(discord.ActionRowComponent); ok && len(row.Components) < 5 {
			if _, menu := row.Components[0].(discord.StringSelectMenuComponent); !menu {
				row.Components = append(row.Components, back)
				card.Components[n-1] = row
				return card
			}
		}
	}
	card.Components = append(card.Components, discord.NewActionRow(back))
	return card
}
