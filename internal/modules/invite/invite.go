// Package invite is /invite: the links a member makes to bring someone into
// their server, one person per link (SPEC.md, What a member sees). The
// command opens one panel, in the server or, installed by the member, from
// any DM or other server: the links they made, who used each, a button for
// a new one and a select to take one back. A server's admins set on the
// same panel how many links each member may have.
package invite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
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
	"github.com/6586x57890143/cygnet/internal/intents"
	"github.com/6586x57890143/cygnet/internal/store"
)

// The panel's custom IDs, each naming the server it is for, since the
// panel can be opened from anywhere: "invite:<guild>" redraws it,
// "invite:make:<guild>" makes a link, "invite:back:<guild>" takes one back
// and "invite:each:<guild>" sets the allowance. "invite:pick" chooses the
// server when the member is in more than one.
const (
	id     = "invite"
	pickID = id + ":pick"
	// life is how long a link works; shown, how many the panel lists.
	life  = 48 * time.Hour
	shown = 10
)

// eachChoices is what an admin can set a member's allowance to.
var eachChoices = []int{0, 1, 2, 3, 5, 10}

// Store is the codes as /invite keeps them: store.Codes in production.
type Store interface {
	Mint(ctx context.Context, hash []byte, guild, owner snowflake.ID, now, expires time.Time, limit int) error
	Left(ctx context.Context, guild, owner snowflake.ID, now time.Time, limit int) (int, error)
	Mine(ctx context.Context, guild, owner snowflake.ID, n int) ([]store.Code, error)
	Revoke(ctx context.Context, hash []byte, owner snowflake.ID, now time.Time) (bool, error)
	Each(ctx context.Context, guild snowflake.ID) (int, error)
	SetEach(ctx context.Context, guild snowflake.ID, n int) error
	Find(ctx context.Context, hash []byte) (store.Code, error)
	Spend(ctx context.Context, hash []byte, user snowflake.ID, now time.Time) (store.Code, error)
	Arrive(ctx context.Context, guild, user snowflake.ID, at time.Time) (snowflake.ID, error)
	Leave(ctx context.Context, guild, user snowflake.ID, at time.Time) error
	Pending(ctx context.Context, since time.Time) ([]store.Code, error)
	Tree(ctx context.Context, guild snowflake.ID) ([]store.Join, error)
}

// Discord is cygnet's Discord as /invite reaches it: the servers in her
// guild cache, and REST.
type Discord interface {
	Guilds() []discord.Guild
	Rest() rest.Rest
	// Quiet is REST that logs nothing, whatever the log level, for the one
	// request that carries a member's own token.
	Quiet() rest.Client
}

type Module struct {
	store   Store
	discord Discord
	guard   *guard.Guard
	log     *slog.Logger
	// edge is cygnet.lol's address, where links point; "" when unset.
	edge  string
	admin func(discord.Interaction) bool
	now   func() time.Time
	// on is whether a server has /invite on. The router refuses the
	// command inside a server that turned it off; this keeps it off from
	// a DM or another server too.
	on func(guild snowflake.ID) bool
}

func New(s Store, d Discord, edge string, admin func(discord.Interaction) bool, g *guard.Guard, log *slog.Logger) *Module {
	return &Module{
		store: s, discord: d, guard: g, log: log, edge: strings.TrimRight(edge, "/"), admin: admin,
		now: time.Now, on: func(snowflake.ID) bool { return true },
	}
}

// Gate is core.Gated: on says which servers have /invite on.
func (m *Module) Gate(on func(guild snowflake.ID) bool) { m.on = on }

func (*Module) Name() string { return "invite" }

// Want is guilds, the servers a member can make links for, and, when the
// portal grants it, server members: who joins, to trace them to a code.
// Without it a join is traced only when cygnet adds the member herself or
// her sweep finds them.
func (*Module) Want() intents.Want {
	return intents.Want{Required: gateway.IntentGuilds, Optional: gateway.IntentGuildMembers}
}

// Perms is what a redemption takes: adding a member needs Create Invite, a
// banned check Ban Members, and an invite only one person can take Manage
// Server as well.
func (*Module) Perms() discord.Permissions {
	return discord.PermissionCreateInstantInvite | discord.PermissionBanMembers | discord.PermissionManageGuild
}

func (*Module) Help() core.Help {
	return core.Help{
		Color: brand.ColorPrimary,
		Line:  "links that bring one person each into the server",
		About: "/invite shows the links you made for this server and who used each, makes a new one and takes back one nobody used. a link lets one person in and works for 48h; one taken back or run out unused is yours to make again. install cygnet on your account and /invite works from any dm or server. admins set how many links each member may have on the same panel, and have no limit themselves. the tree shows them everyone who joined since cygnet watched and whose link brought them in",
	}
}

func (m *Module) Commands() []core.Command {
	return []core.Command{{
		Create: discord.SlashCommandCreate{
			Name:        "invite",
			Description: "your links to bring someone in, one person each",
			IntegrationTypes: []discord.ApplicationIntegrationType{
				discord.ApplicationIntegrationTypeGuildInstall, discord.ApplicationIntegrationTypeUserInstall,
			},
			Contexts: []discord.InteractionContextType{
				discord.InteractionContextTypeGuild, discord.InteractionContextTypeBotDM, discord.InteractionContextTypePrivateChannel,
			},
		},
		Tier: core.Public,
		Run:  m.open,
	}}
}

func (m *Module) Components() []core.Component {
	return []core.Component{{ID: id, Run: m.click}}
}

var errOffline = core.Tell("cygnet isn't connected to cygnet.lol yet, so links have nowhere to go; ask an admin")

// open is /invite: the panel for the server it was run in, or, from
// anywhere else, for the one server the member can make links for, or a
// choice between them.
func (m *Module) open(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	if m.edge == "" {
		return errOffline
	}
	user := e.User().ID
	var card discord.ContainerComponent
	var err error
	if g, ok := m.here(e); ok {
		card, err = m.panel(ctx, g, user, m.isAdmin(e, g), m.canSet(e, g), "")
	} else {
		card, err = m.choose(ctx, user)
	}
	if err != nil {
		return err
	}
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// here is the server the interaction came from, if cygnet is in it.
func (m *Module) here(i discord.Interaction) (discord.Guild, bool) {
	if g := i.GuildID(); g != nil {
		return m.guild(*g)
	}
	return discord.Guild{}, false
}

func (m *Module) guild(id snowflake.ID) (discord.Guild, bool) {
	all := m.discord.Guilds()
	i := slices.IndexFunc(all, func(g discord.Guild) bool { return g.ID == id })
	if i < 0 {
		return discord.Guild{}, false
	}
	return all[i], true
}

// member is user in guild as Discord has them, or nil if they aren't in it.
func (m *Module) member(ctx context.Context, guild, user snowflake.ID) (*discord.Member, error) {
	mem, err := m.discord.Rest().GetMember(guild, user, rest.WithCtx(ctx))
	if rest.IsJSONErrorCode(err, rest.JSONErrorCodeUnknownMember) {
		return nil, nil
	}
	return mem, err
}

// isAdmin is whether whoever sent i is an admin of g: by their roles when
// they are in g, and as its owner from anywhere.
func (m *Module) isAdmin(i discord.Interaction, g discord.Guild) bool {
	if gid := i.GuildID(); gid != nil && *gid == g.ID && m.admin(i) {
		return true
	}
	return g.OwnerID == i.User().ID
}

// canSet is whether whoever sent i may set g's allowance: an admin, from
// inside g, where their roles say so.
func (m *Module) canSet(i discord.Interaction, g discord.Guild) bool {
	gid := i.GuildID()
	return gid != nil && *gid == g.ID && m.admin(i)
}

// choose is the servers user can make links for from outside them: those
// cygnet is in, whose admins let members make links, that user is in.
func (m *Module) choose(ctx context.Context, user snowflake.ID) (discord.ContainerComponent, error) {
	var mu sync.Mutex
	var theirs []discord.Guild
	var failed error
	var wg sync.WaitGroup
	for _, g := range m.discord.Guilds() {
		if !m.on(g.ID) {
			continue
		}
		wg.Go(func() {
			each, err := m.store.Each(ctx, g.ID)
			in := false
			if err == nil && (each > 0 || g.OwnerID == user) {
				var mem *discord.Member
				mem, err = m.member(ctx, g.ID, user)
				in = mem != nil
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				failed = err
			case in:
				theirs = append(theirs, g)
			}
		})
	}
	wg.Wait()
	slices.SortFunc(theirs, func(a, b discord.Guild) int { return strings.Compare(a.Name, b.Name) })
	switch {
	case len(theirs) == 1:
		return m.panel(ctx, theirs[0], user, theirs[0].OwnerID == user, false, "")
	case len(theirs) > 1:
		opts := make([]discord.StringSelectMenuOption, 0, min(len(theirs), 25))
		for _, g := range theirs[:min(len(theirs), 25)] {
			opts = append(opts, discord.StringSelectMenuOption{Label: g.Name, Value: g.ID.String()})
		}
		return brand.Card(brand.ColorInfo, "invite", "", "▸ which server is it for",
			discord.NewActionRow(discord.NewStringSelectMenu(pickID, "pick a server", opts...))), nil
	case failed != nil:
		return discord.ContainerComponent{}, failed
	}
	return discord.ContainerComponent{}, core.Tell("you're not in a server where cygnet lets members make links yet")
}

// click runs the panel: a fresh look, a new link, one taken back, the
// allowance, or the server picked. The custom ID came from the member's
// client, so the server, their place in it and their role are checked
// again here.
func (m *Module) click(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if m.edge == "" {
		return errOffline
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	var values []string
	if d, ok := e.Data.(discord.StringSelectMenuInteractionData); ok {
		values = d.Values
	}
	raw := parts[len(parts)-1]
	if e.Data.CustomID() == pickID {
		if len(values) != 1 {
			return core.Tell("pick a server")
		}
		raw = values[0]
	}
	gid, err := snowflake.Parse(raw)
	if err != nil {
		return core.Tell("that panel is out of date; run /invite again")
	}
	g, ok := m.guild(gid)
	switch {
	case !ok:
		return core.Tell("cygnet isn't in that server anymore")
	case !m.on(g.ID):
		return core.Tell("invite is off in that server")
	}
	user := e.User().ID
	if here := e.GuildID(); here == nil || *here != g.ID {
		mem, err := m.member(ctx, g.ID, user)
		if err != nil {
			return err
		}
		if mem == nil {
			return core.Tell("you're not in that server")
		}
	}
	admin := m.isAdmin(e, g)
	if len(parts) == 3 && parts[1] == "tree" {
		if !m.canSet(e, g) {
			return core.Tell("only this server's admins can see its tree, from inside it")
		}
		card, err := m.tree(ctx, g)
		if err != nil {
			return err
		}
		return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
	}
	note := ""
	switch {
	case len(parts) == 3 && parts[1] == "make":
		note, err = m.make(ctx, g, user, admin)
	case len(parts) == 3 && parts[1] == "back":
		note, err = m.takeBack(ctx, user, values)
	case len(parts) == 3 && parts[1] == "each":
		note, err = m.setEach(ctx, e, g, values)
	}
	if err != nil {
		return err
	}
	card, err := m.panel(ctx, g, user, admin, m.canSet(e, g), note)
	if err != nil {
		return err
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

// make mints a link for user in g and says it, the only time it is shown.
func (m *Module) make(ctx context.Context, g discord.Guild, user snowflake.ID, admin bool) (string, error) {
	limit, err := m.limit(ctx, g.ID, admin)
	if err != nil {
		return "", err
	}
	code, hash := newCode()
	now := m.now()
	err = m.store.Mint(ctx, hash, g.ID, user, now, now.Add(life), limit)
	if errors.Is(err, store.ErrNoneLeft) {
		return "", core.Tell("you have no links left to make; one comes back when a link of yours runs out unused or you take it back")
	}
	if err != nil {
		return "", err
	}
	return "✓ here's your link, shown this once: send it to the one person it's for\n" + m.edge + "/i/" + code + "\n", nil
}

func (m *Module) takeBack(ctx context.Context, user snowflake.ID, values []string) (string, error) {
	hash, err := hex.DecodeString(strings.Join(values, ""))
	if len(values) != 1 || err != nil || len(hash) != sha256.Size {
		return "", core.Tell("pick a link to take back")
	}
	ok, err := m.store.Revoke(ctx, hash, user, m.now())
	switch {
	case err != nil:
		return "", err
	case !ok:
		return "", core.Tell("that link was used or taken back already")
	}
	return "✓ taken back; nobody can use it now\n", nil
}

func (m *Module) setEach(ctx context.Context, e *events.ComponentInteractionCreate, g discord.Guild, values []string) (string, error) {
	if !m.canSet(e, g) {
		return "", core.Tell("only this server's admins can set that, from inside it")
	}
	n, err := strconv.Atoi(strings.Join(values, ""))
	if len(values) != 1 || err != nil || !slices.Contains(eachChoices, n) {
		return "", core.Tell("pick how many links each member may have")
	}
	if err := m.store.SetEach(ctx, g.ID, n); err != nil {
		return "", err
	}
	if n == 0 {
		return "✓ members can't make links now; links already out still work\n", nil
	}
	return fmt.Sprintf("✓ each member may have %s out or used now\n", links(n)), nil
}

// limit is how many links a member may have in guild: the server's
// allowance, or no limit for an admin.
func (m *Module) limit(ctx context.Context, guild snowflake.ID, admin bool) (int, error) {
	if admin {
		return -1, nil
	}
	return m.store.Each(ctx, guild)
}

// panel is user's links in g: how many they have left, the last few they
// made and what became of each, and the buttons; with set, the allowance.
func (m *Module) panel(ctx context.Context, g discord.Guild, user snowflake.ID, admin, set bool, note string) (discord.ContainerComponent, error) {
	each, err := m.store.Each(ctx, g.ID)
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	limit := each
	if admin {
		limit = -1
	}
	now := m.now()
	left, err := m.store.Left(ctx, g.ID, user, now, limit)
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	mine, err := m.store.Mine(ctx, g.ID, user, shown)
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	var b strings.Builder
	b.WriteString(note)
	switch {
	case admin:
		b.WriteString("▸ you're an admin here, so your links have no limit\n")
	case each == 0:
		b.WriteString("▸ this server's admins haven't let members make links yet\n")
	default:
		fmt.Fprintf(&b, "▸ %d of %s left to make\n", left, links(each))
	}
	var open []discord.StringSelectMenuOption
	for _, c := range mine {
		label := hex.EncodeToString(c.Hash)[:4]
		state := status(c, now)
		fmt.Fprintf(&b, "`%s` %s\n", label, state)
		if c.Open(now) {
			open = append(open, discord.StringSelectMenuOption{Label: label, Value: hex.EncodeToString(c.Hash), Description: state})
		}
	}
	b.WriteString("-# a link lets one person in and works for 48h; one taken back, or run out unused, is yours to make again")
	makeBtn := discord.NewPrimaryButton("make a link", fmt.Sprintf("%s:make:%s", id, g.ID))
	if left == 0 {
		makeBtn = makeBtn.AsDisabled()
	}
	buttons := discord.NewActionRow(makeBtn, discord.NewSecondaryButton("check again", fmt.Sprintf("%s:%s", id, g.ID)))
	if set {
		buttons = buttons.AddComponents(discord.NewSecondaryButton("the tree", fmt.Sprintf("%s:tree:%s", id, g.ID)))
	}
	extra := []discord.ContainerSubComponent{discord.NewSeparator(discord.SeparatorSpacingSizeSmall), buttons}
	if len(open) > 0 {
		extra = append(extra, discord.NewActionRow(discord.NewStringSelectMenu(fmt.Sprintf("%s:back:%s", id, g.ID), "take a link back", open...)))
	}
	if set {
		opts := make([]discord.StringSelectMenuOption, len(eachChoices))
		for i, n := range eachChoices {
			label := links(n) + " each"
			if n == 0 {
				label = "members make none"
			}
			opts[i] = discord.StringSelectMenuOption{Label: label, Value: strconv.Itoa(n), Default: n == each}
		}
		extra = append(extra, discord.NewActionRow(discord.NewStringSelectMenu(fmt.Sprintf("%s:each:%s", id, g.ID), "links each member may have", opts...)))
	}
	return brand.Card(brand.ColorInfo, "invite", g.Name, b.String(), extra...), nil
}

// status is what became of a link, as of now.
func status(c store.Code, now time.Time) string {
	switch {
	case c.SpentBy != 0:
		return fmt.Sprintf("used by <@%d> %s ago", c.SpentBy, core.Duration(now.Sub(c.SpentAt)))
	case !c.Revoked.IsZero():
		return "taken back"
	case !now.Before(c.Expires):
		return "ran out"
	}
	return "open · " + core.Duration(c.Expires.Sub(now)) + " left"
}

// newCode is a new link's code, 20 random bytes as lowercase base32 (as
// cygnet.lol makes them), and its SHA-256, which is all that's kept.
func newCode() (string, []byte) {
	b := make([]byte, 20)
	_, _ = rand.Read(b) // never fails: crypto/rand panics instead
	code := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	h := sha256.Sum256([]byte(code))
	return code, h[:]
}

// links is n links, or 1 link.
func links(n int) string {
	if n == 1 {
		return "1 link"
	}
	return fmt.Sprintf("%d links", n)
}
