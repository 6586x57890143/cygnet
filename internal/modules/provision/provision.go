// Package provision is how cygnet sets a server up for the flock.
// /provision fleet is every linked bird, read from its own manifest, checked
// against what it holds in this server, with the one button that fixes it.
// /provision layout is a compiled-in layout previewed as a diff against the
// server and applied by it.
package provision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/internal/hub"
	"github.com/6586x57890143/cygnet/internal/intents"
	"github.com/6586x57890143/cygnet/link"
)

// Fleet is the hub as provision uses it: who is linked, and a call to one
// of them. Nil when the hub is off.
type Fleet interface {
	Fleet() []hub.Bird
	Call(ctx context.Context, method string, in, out any) error
}

// ask bounds a bird's manifest call, inside the response window.
const ask = 1500 * time.Millisecond

const refresh = "provision-fleet"

type Module struct {
	fleet Fleet
	// want is what cygnet's own running modules declare, read on each card.
	want  func() discord.Permissions
	admin func(discord.Interaction) bool
	guard *guard.Guard
	// applying is each guild with a layout being applied, one at a time.
	applying sync.Map
	// running is the applies in flight, for a test to wait on.
	running sync.WaitGroup

	// kept is each provisioned guild's Kept; save keeps one past a restart.
	kept sync.Map
	save func(context.Context, snowflake.ID, Kept) error
	log  *slog.Logger
	// timers is each guild's pending drift check, posted the digest of the
	// drift last reported there, so one drift is reported once.
	timers, posted sync.Map
	settle         time.Duration

	// edge is the Worker launches are registered with, and launches each
	// launch server's name there.
	edge     *Edge
	launches sync.Map
}

func New(fleet Fleet, want func() discord.Permissions, admin func(discord.Interaction) bool, g *guard.Guard) *Module {
	return &Module{fleet: fleet, want: want, admin: admin, guard: g, settle: settle}
}

func (*Module) Name() string { return "provision" }

func (*Module) Want() intents.Want { return intents.Want{Required: gateway.IntentGuilds} }

// Perms is what applying every compiled-in layout takes: roles, channels
// and each permission a layout hands out, which Discord lets cygnet grant
// only if she holds it.
func (*Module) Perms() discord.Permissions {
	var p discord.Permissions
	for _, l := range layouts {
		p |= needs(l)
	}
	return p
}

func (*Module) Help() core.Help {
	return core.Help{
		Color: brand.ColorPrimary,
		Line:  "sets this server up for the flock",
		About: "/provision fleet shows every bird linked to her, whether it's in this server and whether it holds what it asks for. each one that isn't in yet or is missing something gets a button that adds it or grants the rest. what a bird asks for comes from the bird itself, so the card follows each one as it changes. /provision layout shows what a layout would change here before anything happens: roles, channels and who may post where, birds included. apply makes exactly those changes and nothing else, and leaves whatever the layout doesn't name alone. every role gets its colour: the layout's own, and one for all the bots. with fresh, whatever the layout doesn't name goes too: replace deletes it, archive keeps the old channels hidden in an archive category, and nothing is deleted before you confirm what goes. once a layout is applied she keeps an eye on it, and when the server drifts from it she says so in the layout's staff channel. /provision launch gives this server a page at cygnet.lol where people get in line: she sends the server's name and picture there herself and keeps them current. /provision mode sets how far she goes: watch only says so, propose adds a button to put it back, and act puts it back and then says what she did",
	}
}

func (m *Module) Commands() []core.Command {
	return []core.Command{{
		Create: discord.SlashCommandCreate{
			Name:        "provision",
			Description: "set this server up for the flock",
			Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionSubCommand{Name: "fleet", Description: "every bird, in this server or not, and what it's missing"},
				discord.ApplicationCommandOptionSubCommand{Name: "layout", Description: "what a layout would change here, then apply it", Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{Name: "name", Description: "which layout", Choices: choices()},
					discord.ApplicationCommandOptionBool{Name: "fresh", Description: "start over: delete or archive what the layout doesn't name, once you confirm"},
				}},
				discord.ApplicationCommandOptionSubCommand{Name: "launch", Description: "give this server a page at cygnet.lol where people get in line", Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{Name: "name", Description: "the page's name, as in cygnet.lol/name", Required: true},
				}},
				discord.ApplicationCommandOptionSubCommand{Name: "mode", Description: "how far she goes on her own when this server drifts", Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{Name: "level", Description: "watch, propose or act", Required: true, Choices: modeChoices()},
				}},
			},
		},
		Tier: core.Admin,
		Run:  m.run,
	}}
}

func (m *Module) Components() []core.Component {
	return []core.Component{{ID: refresh, Run: m.again}, {ID: previewID, Run: m.previewAgain}, {ID: applyID, Run: m.applyPressed}, {ID: confirmID, Run: m.confirmPressed}}
}

func choices() []discord.ApplicationCommandOptionChoiceString {
	out := make([]discord.ApplicationCommandOptionChoiceString, len(layouts))
	for i, l := range layouts {
		out[i] = discord.ApplicationCommandOptionChoiceString{Name: l.Name, Value: l.Name}
	}
	return out
}

func modeChoices() []discord.ApplicationCommandOptionChoiceString {
	out := make([]discord.ApplicationCommandOptionChoiceString, len(modes))
	for i, md := range modes {
		out[i] = discord.ApplicationCommandOptionChoiceString{Name: md, Value: md}
	}
	return out
}

func (m *Module) run(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	if sub := e.SlashCommandInteractionData().SubCommandName; sub != nil {
		switch *sub {
		case "layout":
			return m.layoutCmd(ctx, e)
		case "mode":
			return m.modeCmd(ctx, e)
		case "launch":
			return m.launchCmd(ctx, e)
		}
	}
	card := m.card(ctx, e.Client().Rest, *e.GuildID(), e.ApplicationID())
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// again redraws the card in place, for whoever may run /provision.
func (m *Module) again(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if e.GuildID() == nil || !m.admin(e) {
		return core.Tell("only this server's admins can use /provision")
	}
	card := m.card(ctx, e.Client().Rest, *e.GuildID(), e.ApplicationID())
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

// Rest is what the card reads from Discord.
type Rest interface {
	GetRoles(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.Role, error)
	GetMember(guildID, userID snowflake.ID, opts ...rest.RequestOpt) (*discord.Member, error)
}

// card reads cygnet and every linked bird as guild sees them, at once.
func (m *Module) card(ctx context.Context, r Rest, guild, self snowflake.ID) discord.ContainerComponent {
	roles, rolesErr := r.GetRoles(guild, rest.WithCtx(ctx))
	birds := []row{{Name: "cygnet", Build: core.Revision(), App: self, Want: m.want()}}
	if m.fleet != nil {
		for _, b := range m.fleet.Fleet() {
			birds = append(birds, row{Name: b.Name, Build: b.Build})
		}
	}
	var wg sync.WaitGroup
	for i := range birds {
		wg.Go(func() {
			b := &birds[i]
			if i > 0 {
				b.manifest(ctx, m.fleet)
			}
			if b.Err == "" && rolesErr == nil {
				b.member(ctx, r, guild, roles)
			}
		})
	}
	wg.Wait()
	if rolesErr != nil {
		for i := range birds {
			if birds[i].Err == "" {
				birds[i].Err = "couldn't read this server's roles"
			}
		}
	}
	return render(guild, birds, m.fleet != nil)
}

// row is one bird as a server sees it.
type row struct {
	Name, Build string
	App         snowflake.ID
	In          bool
	Have, Want  discord.Permissions
	// Err is why it couldn't be read, in a member's words.
	Err string
}

func (b *row) manifest(ctx context.Context, f Fleet) {
	ctx, cancel := context.WithTimeout(ctx, ask)
	defer cancel()
	var mf link.Manifest
	if err := f.Call(ctx, b.Name+"."+link.ManifestMethod, nil, &mf); err != nil {
		b.Err = "didn't say what it needs: " + reason(err)
		return
	}
	app, err := snowflake.Parse(mf.App)
	if err != nil {
		b.Err = "its build doesn't say which app it is yet"
		return
	}
	want, err := strconv.ParseInt(mf.Perms, 10, 64)
	if err != nil {
		b.Err = "it sent permissions that don't read as a number"
		return
	}
	b.App, b.Want = app, discord.Permissions(want)
	if mf.Build != "" {
		b.Build = mf.Build
	}
}

func (b *row) member(ctx context.Context, r Rest, guild snowflake.ID, roles []discord.Role) {
	mem, err := r.GetMember(guild, b.App, rest.WithCtx(ctx))
	switch {
	case isUnknownMember(err):
		return
	case err != nil:
		b.Err = "couldn't look it up here"
		return
	}
	b.In, b.Have = true, permsOf(guild, roles, mem.RoleIDs)
}

func isUnknownMember(err error) bool {
	re, ok := errors.AsType[*rest.Error](err)
	return ok && re.Code == rest.JSONErrorCodeUnknownMember
}

// reason is a call's error code as a member reads it.
func reason(err error) string {
	var le *link.Error
	if errors.As(err, &le) {
		switch le.Code {
		case link.Offline:
			return "it's not linked right now"
		case link.Timeout:
			return "it took too long to answer"
		case link.Busy:
			return "it's busy or still starting"
		case link.Invalid:
			return "its build doesn't serve a manifest yet"
		}
	}
	return "something went wrong asking"
}

// permsOf is what a member holds server wide: @everyone's (the role whose
// ID is the guild's) and each of its roles', all of them under
// Administrator.
//
// ponytail: server wide only; a channel's overwrites can still take one
// away. Per-channel checks come with layouts, which know the channels.
func permsOf(guild snowflake.ID, roles []discord.Role, has []snowflake.ID) discord.Permissions {
	var p discord.Permissions
	for _, r := range roles {
		if r.ID == guild || slices.Contains(has, r.ID) {
			p |= r.Permissions
		}
	}
	if p.Has(discord.PermissionAdministrator) {
		return discord.PermissionsAll
	}
	return p
}

// render is the card: one line a bird, the fix for each as a button.
func render(guild snowflake.ID, birds []row, hubOn bool) discord.ContainerComponent {
	color := brand.ColorOK
	var lines []string
	var fix []discord.InteractiveComponent
	for _, b := range birds {
		head := "**" + b.Name + "**"
		if b.Build != "" {
			head += " · `" + b.Build + "`"
		}
		missing := b.Want &^ b.Have
		switch {
		case b.Err != "":
			color = brand.ColorError
			lines = append(lines, "✗ "+head+"\n-# "+b.Err)
		case !b.In:
			color = worse(color, brand.ColorWarn)
			lines = append(lines, "▸ "+head+"\n-# not in this server yet")
			fix = append(fix, discord.NewLinkButton("add "+b.Name, install(b.App, b.Want, guild)))
		case missing != 0:
			color = worse(color, brand.ColorWarn)
			lines = append(lines, "! "+head+"\n-# missing "+names(missing))
			fix = append(fix, discord.NewLinkButton("grant "+b.Name, install(b.App, b.Want, guild)))
		default:
			lines = append(lines, "✓ "+head+"\n-# "+holds(b.Want))
		}
	}
	if !hubOn {
		color = worse(color, brand.ColorWarn)
		lines = append(lines, "-# the hub is off, so no other bird can show up here")
	}
	body := strings.Join(lines, "\n")
	extra := []discord.ContainerSubComponent{discord.NewSeparator(discord.SeparatorSpacingSizeSmall)}
	for len(fix) > 4 {
		extra = append(extra, discord.NewActionRow(fix[:5]...))
		fix = fix[5:]
	}
	extra = append(extra, discord.NewActionRow(append(fix, discord.NewSecondaryButton("check again", refresh))...))
	return brand.Card(color, "provision", "fleet", body, extra...)
}

// worse keeps whichever mood says more is wrong.
func worse(a, b int) int {
	rank := map[int]int{brand.ColorOK: 0, brand.ColorWarn: 1, brand.ColorError: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func holds(want discord.Permissions) string {
	if want == 0 {
		return "asks for nothing and has it"
	}
	n := 0
	for bit := range 64 {
		if want&(1<<bit) != 0 {
			n++
		}
	}
	if n == 1 {
		return "has the one permission it asks for"
	}
	return fmt.Sprintf("has all %d permissions it asks for", n)
}

// names lists permissions lowercase, in bit order, so a card reads the same
// every time.
func names(p discord.Permissions) string {
	var out []string
	for bit := range 64 {
		one := discord.Permissions(1) << bit
		if p&one != 0 {
			out = append(out, strings.ToLower(one.String()))
		}
	}
	return strings.Join(out, ", ")
}

// install is the bird's own install link for this server, asking for what
// it declares. For a bird already here, Discord applies the new permissions
// to its role.
func install(app snowflake.ID, want discord.Permissions, guild snowflake.ID) string {
	return fmt.Sprintf("https://discord.com/oauth2/authorize?client_id=%s&scope=bot+applications.commands&permissions=%d&guild_id=%s&disable_guild_select=true", app, want, guild)
}
