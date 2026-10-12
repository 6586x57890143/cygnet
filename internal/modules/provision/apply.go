package provision

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/guard"
)

// Writes is what applying a layout calls on Discord.
type Writes interface {
	GetRoles(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.Role, error)
	UpdateRolePositions(guildID snowflake.ID, rolePositionUpdates []discord.RolePositionUpdate, opts ...rest.RequestOpt) ([]discord.Role, error)
	CreateRole(guildID snowflake.ID, createRole discord.RoleCreate, opts ...rest.RequestOpt) (*discord.Role, error)
	UpdateRole(guildID, roleID snowflake.ID, roleUpdate discord.RoleUpdate, opts ...rest.RequestOpt) (*discord.Role, error)
	CreateGuildChannel(guildID snowflake.ID, guildChannelCreate discord.GuildChannelCreate, opts ...rest.RequestOpt) (discord.GuildChannel, error)
	UpdatePermissionOverwrite(channelID, overwriteID snowflake.ID, permissionOverwrite discord.PermissionOverwriteUpdate, opts ...rest.RequestOpt) error
	UpdateChannel(channelID snowflake.ID, channelUpdate discord.ChannelUpdate, opts ...rest.RequestOpt) (discord.Channel, error)
	UpdateChannelPositions(guildID snowflake.ID, guildChannelPositionUpdates []discord.GuildChannelPositionUpdate, opts ...rest.RequestOpt) error
	DeleteChannel(channelID snowflake.ID, opts ...rest.RequestOpt) error
	DeleteRole(guildID snowflake.ID, roleID snowflake.ID, opts ...rest.RequestOpt) error
}

// apply makes changes in order, skipping the blocked ones, and stops at the
// first that fails. progress hears how many are done after each.
func apply(ctx context.Context, w Writes, g *guard.Guard, s state, l Layout, changes []change, progress func(done int)) (done int, err error) {
	roles := map[string]snowflake.ID{}
	cats := map[string]snowflake.ID{}
	for _, cat := range l.Categories {
		if c, ok := s.channel(cat.Name, discord.ChannelTypeGuildCategory, 0); ok {
			cats[cat.Name] = c.ID()
		}
	}
	if c, ok := s.channel(archived, discord.ChannelTypeGuildCategory, 0); ok {
		cats[archived] = c.ID()
	}
	target := func(name string) (snowflake.ID, bool) {
		if id, ok := roles[name]; ok {
			return id, true
		}
		return s.target(name)
	}
	opts := []rest.RequestOpt{rest.WithCtx(ctx), rest.WithReason("cygnet: layout " + l.Name)}
	for _, c := range changes {
		if c.blocked != "" {
			continue
		}
		op := guard.ChannelEdit
		if c.kind == roleCreate || c.kind == roleUpdate || c.kind == roleOrder || c.kind == roleColor || (c.kind == remove && c.role.Name != "") {
			op = guard.RoleEdit
		}
		if err := g.Allow(s.guild, op); err != nil {
			return done, fmt.Errorf("%w: %w", core.Tell("stopped at cygnet's hourly limit for this server; check again in a while and apply the rest"), err)
		}
		err := c.do(s.guild, w, opts, l, roles, cats, target)
		g.Report(s.guild, struggling(err))
		if err != nil {
			return done, fmt.Errorf("%s: %w", c.line(), err)
		}
		done++
		progress(done)
	}
	return done, nil
}

func (c change) do(guild snowflake.ID, w Writes, opts []rest.RequestOpt, l Layout, roles, cats map[string]snowflake.ID, target func(string) (snowflake.ID, bool)) error {
	switch c.kind {
	case roleCreate:
		perms := c.role.Perms
		r, err := w.CreateRole(guild, discord.RoleCreate{Name: c.role.Name, Permissions: &perms, Color: c.role.Color, Hoist: c.role.Hoist}, opts...)
		if err == nil {
			roles[c.role.Name] = r.ID
		}
		return err
	case roleUpdate:
		perms, color, hoist := c.role.Perms, c.role.Color, c.role.Hoist
		_, err := w.UpdateRole(guild, c.id, discord.RoleUpdate{Permissions: &perms, Color: &color, Hoist: &hoist}, opts...)
		return err
	case roleOrder:
		return order(guild, w, opts, l)
	case roleColor:
		color := c.role.Color
		_, err := w.UpdateRole(guild, c.id, discord.RoleUpdate{Color: &color}, opts...)
		return err
	case remove:
		if c.role.Name != "" {
			return w.DeleteRole(guild, c.id, opts...)
		}
		return w.DeleteChannel(c.id, opts...)
	case archive:
		return moveToArchive(guild, w, opts, c.ids, cats)
	case categoryCreate:
		ch, err := w.CreateGuildChannel(guild, discord.GuildCategoryChannelCreate{Name: c.name}, opts...)
		if err == nil {
			cats[c.category] = ch.ID()
		}
		return err
	case channelCreate:
		var ows []discord.PermissionOverwrite
		for _, o := range c.channel.Overwrites {
			id, ok := target(o.Role)
			if !ok {
				continue
			}
			ows = append(ows, discord.RolePermissionOverwrite{RoleID: id, Allow: o.Allow, Deny: o.Deny})
		}
		var create discord.GuildChannelCreate = discord.GuildTextChannelCreate{Name: c.name, Topic: c.channel.Topic, ParentID: cats[c.category], PermissionOverwrites: ows}
		if c.channel.Voice {
			create = discord.GuildVoiceChannelCreate{Name: c.name, ParentID: cats[c.category], PermissionOverwrites: ows}
		}
		_, err := w.CreateGuildChannel(guild, create, opts...)
		return err
	case rename:
		// ponytail: Discord allows two renames a channel per ten minutes,
		// and the rate limiter waits that out inside the apply; fine for a
		// restyle, a per-channel queue if renames ever come in sweeps.
		name := c.name
		var update discord.ChannelUpdate = discord.GuildTextChannelUpdate{Name: &name}
		switch c.typ {
		case discord.ChannelTypeGuildCategory:
			update = discord.GuildCategoryChannelUpdate{Name: &name}
		case discord.ChannelTypeGuildVoice:
			update = discord.GuildVoiceChannelUpdate{Name: &name}
		}
		_, err := w.UpdateChannel(c.id, update, opts...)
		return err
	}
	id, ok := target(c.ow.Role)
	if !ok {
		return nil
	}
	allow, deny := c.ow.Allow, c.ow.Deny
	return w.UpdatePermissionOverwrite(c.id, id, discord.RolePermissionOverwriteUpdate{Allow: &allow, Deny: &deny}, opts...)
}

// moveToArchive moves ids into the archive in one write, making it first if
// it isn't here, hidden from @everyone. Each channel takes the archive's
// permissions, so what's in it is hidden too and kept for the admins.
//
// ponytail: Discord holds 50 channels a category; a server with more to
// archive stops there, a second archive when one comes up.
func moveToArchive(guild snowflake.ID, w Writes, opts []rest.RequestOpt, ids []snowflake.ID, cats map[string]snowflake.ID) error {
	attic, ok := cats[archived]
	if !ok {
		hide := []discord.PermissionOverwrite{discord.RolePermissionOverwrite{RoleID: guild, Deny: see}}
		ch, err := w.CreateGuildChannel(guild, discord.GuildCategoryChannelCreate{Name: archiveName, PermissionOverwrites: hide}, opts...)
		if err != nil {
			return err
		}
		attic = ch.ID()
		cats[archived] = attic
	}
	moves := make([]discord.GuildChannelPositionUpdate, len(ids))
	for i, id := range ids {
		moves[i] = discord.GuildChannelPositionUpdate{ID: id, ParentID: &attic, LockPermissions: omit.NewPtr(true)}
	}
	return w.UpdateChannelPositions(guild, moves, opts...)
}

// order hands l's roles the positions they hold between them, highest to
// the first, so they keep their place among the server's other roles.
func order(guild snowflake.ID, w Writes, opts []rest.RequestOpt, l Layout) error {
	have, err := w.GetRoles(guild, opts...)
	if err != nil {
		return err
	}
	s := state{guild: guild, roles: have}
	var ids []snowflake.ID
	var slots []int
	for _, want := range l.Roles {
		if r, ok := s.role(want.Name); ok {
			ids, slots = append(ids, r.ID), append(slots, r.Position)
		}
	}
	slices.Sort(slots)
	slices.Reverse(slots)
	moves := make([]discord.RolePositionUpdate, len(ids))
	for i, id := range ids {
		moves[i] = discord.RolePositionUpdate{ID: id, Position: &slots[i]}
	}
	_, err = w.UpdateRolePositions(guild, moves, opts...)
	return err
}

// struggling is whether Discord's answer says it is struggling (429, 5xx),
// which is all the guard's breaker counts.
func struggling(err error) bool {
	re, ok := errors.AsType[*rest.Error](err)
	return ok && re.Response != nil && (re.Response.StatusCode == http.StatusTooManyRequests || re.Response.StatusCode >= 500)
}

// refusal is why Discord turned a write down, in a member's words.
func refusal(err error) string {
	if _, ok := errors.AsType[core.Tell](err); ok {
		return ""
	}
	re, ok := errors.AsType[*rest.Error](err)
	switch {
	case ok && re.Code == rest.JSONErrorCodeLackPermissionsToPerformAction:
		return "discord says cygnet is missing a permission for it"
	case ok && re.Code == rest.JSONErrorCodeMaximumGuildRolesReached:
		return "the server has as many roles as discord allows"
	case ok && re.Code == rest.JSONErrorCodeMaximumGuildChannelsReached:
		return "the server has as many channels as discord allows"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "it took too long; check again and apply the rest"
	}
	return "discord didn't take it; check again in a moment and apply the rest"
}
