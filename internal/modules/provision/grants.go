package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/link"
	"github.com/6586x57890143/cygnet/link/perms"
)

// grantID is the button on a report that grants a bird what it lacked:
// "provision-grant:<bird>:<channel or 0>:<op>".
const grantID = "provision-grant"

// again is how long one refusal is reported once: a bird retrying, or
// refused on every message in a channel, makes one report.
const again = 10 * time.Minute

// Grants is what working out and granting a bird's permissions calls on
// Discord.
type Grants interface {
	GetRoles(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.Role, error)
	GetMember(guildID, userID snowflake.ID, opts ...rest.RequestOpt) (*discord.Member, error)
	GetChannel(channelID snowflake.ID, opts ...rest.RequestOpt) (discord.Channel, error)
	GetGuildChannels(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.GuildChannel, error)
	UpdateRole(guildID, roleID snowflake.ID, roleUpdate discord.RoleUpdate, opts ...rest.RequestOpt) (*discord.Role, error)
	UpdatePermissionOverwrite(channelID, overwriteID snowflake.ID, permissionOverwrite discord.PermissionOverwriteUpdate, opts ...rest.RequestOpt) error
	CreateMessage(channelID snowflake.ID, messageCreate discord.MessageCreate, opts ...rest.RequestOpt) (*discord.Message, error)
}

// granting is what the grants keep: the REST client cygnet connected with,
// and when each refusal was last reported.
type granting struct {
	mu   sync.Mutex
	rest Grants
	seen map[string]time.Time
}

// lack is a bird's refusal worked out: what it lacks, and the shortest
// grant that fixes it: an overwrite for its role on the channel when only
// the channel takes it away, else its own role.
type lack struct {
	guild, channel snowflake.ID
	bird           string
	op             perms.Op
	lacks          perms.Permissions
	// role is the bird's own role; on it, or on the channel for it, the
	// grant lands. Zero when Discord made it none.
	role      snowflake.ID
	roleHas   discord.Permissions
	inChannel bool
	// owAllow and owDeny are the role's overwrite on the channel now: a
	// grant writes it whole, so it keeps what it had.
	owAllow, owDeny discord.Permissions
}

// Missing answers "cygnet.missing": a bird Discord refused says where and
// what it tried, and cygnet works out what it lacks, reports it in the
// layout's staff channel, and grants it as far as the server's mode says.
func (m *Module) Missing(ctx context.Context, bird string, body json.RawMessage) (any, error) {
	var in perms.Missing
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, link.Err(link.Invalid, "body")
	}
	var channel snowflake.ID
	var err error
	if in.Channel != "" {
		if channel, err = snowflake.Parse(in.Channel); err != nil {
			return nil, link.Err(link.Invalid, "channel")
		}
	}
	if _, ok := perms.Needs[in.Op]; !ok {
		return nil, link.Err(link.Invalid, "no such op")
	}
	m.grants.mu.Lock()
	r := m.grants.rest
	m.grants.mu.Unlock()
	if r == nil {
		return nil, link.Err(link.Busy, "cygnet isn't connected yet")
	}
	guild, err := snowflake.Parse(in.Guild)
	switch {
	case in.Guild == "" && channel != 0:
		// A channel's path doesn't name its server; Discord does.
		ch, err := r.GetChannel(channel, rest.WithCtx(ctx))
		gc, ok := ch.(discord.GuildChannel)
		if err != nil || !ok {
			return nil, link.Err(link.Invalid, "cygnet can't see that channel")
		}
		guild = gc.GuildID()
	case err != nil:
		return nil, link.Err(link.Invalid, "guild")
	}
	l, err := m.assess(ctx, r, guild, channel, bird, in.Op)
	if err != nil {
		return nil, err
	}
	out := map[string]string{"lacks": strconv.FormatUint(uint64(l.lacks), 10)}
	if l.lacks == 0 || !m.fresh(l, time.Now()) {
		return out, nil
	}
	out["did"] = m.answer(ctx, r, l)
	return out, nil
}

// fresh is whether l hasn't been reported within again, and marks it.
func (m *Module) fresh(l lack, now time.Time) bool {
	k := fmt.Sprint(l.guild, l.bird, l.channel, l.op)
	m.grants.mu.Lock()
	defer m.grants.mu.Unlock()
	if now.Sub(m.grants.seen[k]) < again {
		return false
	}
	if m.grants.seen == nil {
		m.grants.seen = map[string]time.Time{}
	}
	m.grants.seen[k] = now
	return true
}

// assess works out what bird lacks for op in guild, and in channel if op
// is one a channel decides, from its roles and the channel's overwrites,
// with the fleet's one model.
func (m *Module) assess(ctx context.Context, r Grants, guild, channel snowflake.ID, bird string, op perms.Op) (lack, error) {
	opts := rest.WithCtx(ctx)
	b := row{Name: bird}
	b.manifest(ctx, m.fleet)
	if b.Err != "" {
		return lack{}, link.Err(link.Invalid, "cygnet couldn't read your manifest")
	}
	roles, err := r.GetRoles(guild, opts)
	if err != nil {
		return lack{}, err
	}
	mem, err := r.GetMember(guild, b.App, opts)
	if err != nil {
		return lack{}, err
	}
	l := lack{guild: guild, bird: bird, op: op}
	s := perms.Server{ID: guild.String()}
	for _, role := range roles {
		s.Roles = append(s.Roles, perms.Role{ID: role.ID.String(), Perms: perms.Permissions(role.Permissions)})
		if role.Managed && role.Tags != nil && role.Tags.BotID != nil && *role.Tags.BotID == b.App {
			l.role, l.roleHas = role.ID, role.Permissions
		}
	}
	who := perms.Member{ID: b.App.String(), TimedOut: mem.CommunicationDisabledUntil != nil && mem.CommunicationDisabledUntil.After(time.Now())}
	for _, id := range mem.RoleIDs {
		who.Roles = append(who.Roles, id.String())
	}
	var ows []perms.Overwrite
	if perms.Needs[op].Channel && channel != 0 {
		ch, err := r.GetChannel(channel, opts)
		if err != nil {
			return lack{}, err
		}
		gc, ok := ch.(discord.GuildChannel)
		if !ok || gc.GuildID() != guild {
			return lack{}, link.Err(link.Invalid, "that channel isn't in that server")
		}
		l.channel = channel
		ows = []perms.Overwrite{}
		for _, o := range gc.PermissionOverwrites() {
			switch o := o.(type) {
			case discord.RolePermissionOverwrite:
				ows = append(ows, perms.Overwrite{ID: o.RoleID.String(), Allow: perms.Permissions(o.Allow), Deny: perms.Permissions(o.Deny)})
				if o.RoleID == l.role {
					l.owAllow, l.owDeny = o.Allow, o.Deny
				}
			case discord.MemberPermissionOverwrite:
				ows = append(ows, perms.Overwrite{ID: o.UserID.String(), Member: true, Allow: perms.Permissions(o.Allow), Deny: perms.Permissions(o.Deny)})
			}
		}
	}
	l.lacks = perms.Lacks(op, s, who, ows)
	// Server wide the bird has it and the channel takes it away: the
	// channel is where to give it back.
	l.inChannel = ows != nil && perms.Effective(s, who, nil).Has(l.lacks)
	return l, nil
}

// answer reports l in the layout's staff channel, granting it first in act
// and offering to in propose, and says which it did.
func (m *Module) answer(ctx context.Context, r Grants, l lack) string {
	mode, reports := Propose, "cygnet"
	if v, ok := m.kept.Load(l.guild); ok {
		k := v.(Kept)
		mode = k.Mode
		if lay, ok := layout(k.Layout); ok {
			reports = lay.Reports
		}
	}
	did := "reported"
	var card discord.ContainerComponent
	switch {
	case mode == Act && l.role != 0:
		if err := m.grant(ctx, r, l); err != nil {
			m.warn("granting a bird", l.guild, err)
			card = l.card("✗ cygnet couldn't grant it: "+refusal(err), brand.ColorError, false)
		} else {
			did = "granted"
			card = l.granted()
		}
	default:
		card = l.card("", brand.ColorWarn, mode == Propose && l.role != 0)
		if mode == Propose {
			did = "proposed"
		}
	}
	channels, err := r.GetGuildChannels(l.guild, rest.WithCtx(ctx))
	if err != nil {
		m.warn("reporting a refusal", l.guild, err)
		return did
	}
	at, ok := state{channels: channels}.named(reports)
	if !ok {
		m.warn("reporting a refusal", l.guild, errNoReports)
		return did
	}
	if err := m.post(ctx, r, l.guild, at, card); err != nil {
		m.warn("reporting a refusal", l.guild, err)
	}
	return did
}

// grant gives the bird what it lacks: on the channel for its role, or on
// its role, through the guard like every write.
func (m *Module) grant(ctx context.Context, r Grants, l lack) error {
	opts := []rest.RequestOpt{rest.WithCtx(ctx), rest.WithReason("cygnet: " + l.bird + " was refused " + string(l.op))}
	if l.inChannel {
		if err := m.guard.Allow(l.guild, guard.ChannelEdit); err != nil {
			return err
		}
		// The overwrite is written whole: it keeps what it allowed and
		// denied, allows what was lacking, and denies it no more.
		allow, deny := l.owAllow|discord.Permissions(l.lacks), l.owDeny&^discord.Permissions(l.lacks)
		err := r.UpdatePermissionOverwrite(l.channel, l.role, discord.RolePermissionOverwriteUpdate{Allow: &allow, Deny: &deny}, opts...)
		m.guard.Report(l.guild, struggling(err))
		return err
	}
	if err := m.guard.Allow(l.guild, guard.RoleEdit); err != nil {
		return err
	}
	p := l.roleHas | discord.Permissions(l.lacks)
	_, err := r.UpdateRole(l.guild, l.role, discord.RoleUpdate{Permissions: &p}, opts...)
	m.guard.Report(l.guild, struggling(err))
	return err
}

// where is the grant's place in words.
func (l lack) where() string {
	if l.inChannel {
		return "in <#" + l.channel.String() + ">"
	}
	return "on its role"
}

// card is the report: who was refused what, where, and what it lacks,
// with the grant button when offered.
func (l lack) card(note string, color int, offer bool) discord.ContainerComponent {
	in := ""
	if l.channel != 0 {
		in = " in <#" + l.channel.String() + ">"
	}
	body := "! **" + l.bird + "** was refused `" + string(l.op) + "`" + in + "\n-# it lacks " + l.lacks.Names()
	if note != "" {
		body += "\n" + note
	}
	if l.role == 0 {
		body += "\n-# discord made it no role here; add it again from the fleet card"
	}
	if !offer {
		return brand.Card(color, "provision", "grant", body)
	}
	id := fmt.Sprintf("%s:%s:%d:%s", grantID, l.bird, l.channel, l.op)
	return brand.Card(color, "provision", "grant", body+"\n-# grant gives it that "+l.where(),
		discord.NewSeparator(discord.SeparatorSpacingSizeSmall),
		discord.NewActionRow(discord.NewSuccessButton("grant "+l.bird, id)))
}

// granted is the report once cygnet granted it.
func (l lack) granted() discord.ContainerComponent {
	return brand.Card(brand.ColorOK, "provision", "grant", "✓ **"+l.bird+"** was refused `"+string(l.op)+"`, so cygnet gave it "+l.lacks.Names()+" "+l.where())
}

// grantPressed grants what a report offered, worked out again from the
// server as it is now: the custom ID came from the member's client.
func (m *Module) grantPressed(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if e.GuildID() == nil || !m.admin(e) {
		return core.Tell("only this server's admins can grant a bird permissions")
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) != 4 {
		return core.Tell("that button is from an older cygnet; check the fleet card")
	}
	channel, err := snowflake.Parse(parts[2])
	if err != nil && parts[2] != "0" {
		return core.Tell("that button is from an older cygnet; check the fleet card")
	}
	op := perms.Op(parts[3])
	if _, ok := perms.Needs[op]; !ok {
		return core.Tell("that button is from an older cygnet; check the fleet card")
	}
	r := e.Client().Rest
	l, err := m.assess(ctx, r, *e.GuildID(), channel, parts[1], op)
	if err != nil {
		return fmt.Errorf("%w: %w", core.Tell("couldn't read "+parts[1]+" here; try again in a moment"), err)
	}
	card := brand.Card(brand.ColorOK, "provision", "grant", "✓ **"+l.bird+"** has what `"+string(op)+"` needs now")
	if l.lacks != 0 {
		if l.role == 0 {
			return core.Tell(l.bird + " has no role here; add it again from the fleet card")
		}
		if err := m.grant(ctx, r, l); err != nil {
			return fmt.Errorf("%w: %w", core.Tell("couldn't grant it: "+refusal(err)), err)
		}
		card = l.granted()
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}
