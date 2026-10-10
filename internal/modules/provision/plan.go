package provision

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

type kind uint8

const (
	roleCreate kind = iota
	roleUpdate
	categoryCreate
	channelCreate
	overwriteSet
)

// change is one write a layout needs, in the order it has to happen: roles,
// then categories, then channels, then overwrites on channels that exist,
// so whatever a change names already exists when it runs.
type change struct {
	kind kind
	role Role
	// id is the role a roleUpdate changes, or the channel an overwriteSet
	// is on.
	id snowflake.ID
	// category is a categoryCreate's name, or the category a channelCreate
	// goes in.
	category string
	channel  Channel
	ow       Overwrite
	// fields are what a roleUpdate changes.
	fields []string
	// blocked is why cygnet can't make this change, or "".
	blocked string
}

// state is a server as a layout is planned against it.
type state struct {
	guild    snowflake.ID
	roles    []discord.Role
	channels []discord.GuildChannel
	// birds is each linked bird's application ID, which is its bot user's.
	birds map[string]snowflake.ID
	// top is the position of cygnet's highest role: she can change only
	// roles below it.
	top int
}

// plan is what l needs done to s, and the overwrites that wait for a bird
// that isn't in the server yet.
func plan(l Layout, s state) (changes []change, waiting []string) {
	roles := map[string]bool{}
	for _, want := range l.Roles {
		roles[want.Name] = true
		have, ok := s.role(want.Name)
		if !ok {
			changes = append(changes, change{kind: roleCreate, role: want})
			continue
		}
		if fields := differs(want, have); len(fields) > 0 {
			c := change{kind: roleUpdate, role: want, id: have.ID, fields: fields}
			if have.Position >= s.top {
				c.blocked = "its role is above cygnet's; drag cygnet's role above it"
			}
			changes = append(changes, c)
		}
	}
	var later []change
	for _, cat := range l.Categories {
		parent, ok := s.channel(cat.Name, discord.ChannelTypeGuildCategory, 0)
		if !ok {
			changes = append(changes, change{kind: categoryCreate, category: cat.Name})
		}
		for _, ch := range cat.Channels {
			var have discord.GuildChannel
			found := false
			if ok {
				have, found = s.channel(ch.Name, ch.typ(), parent.ID())
			}
			if !found {
				kept := ch
				kept.Overwrites = nil
				for _, o := range ch.Overwrites {
					if s.waits(o, roles) {
						waiting = append(waiting, wait(o.Role, ch.Name))
						continue
					}
					kept.Overwrites = append(kept.Overwrites, o)
				}
				changes = append(changes, change{kind: channelCreate, category: cat.Name, channel: kept})
				continue
			}
			for _, o := range ch.Overwrites {
				if s.waits(o, roles) {
					waiting = append(waiting, wait(o.Role, ch.Name))
					continue
				}
				id, exists := s.target(o.Role)
				if exists {
					if cur, ok := have.PermissionOverwrites().Role(id); ok && cur.Allow == o.Allow && cur.Deny == o.Deny {
						continue
					}
				}
				later = append(later, change{kind: overwriteSet, id: have.ID(), channel: ch, ow: o})
			}
		}
	}
	return append(changes, later...), waiting
}

// role is the role of the layout called name, never a bot's own.
func (s state) role(name string) (discord.Role, bool) {
	for _, r := range s.roles {
		if !r.Managed && r.ID != s.guild && strings.EqualFold(r.Name, name) {
			return r, true
		}
	}
	return discord.Role{}, false
}

// birdRole is the role Discord made for a bird when it was added.
func (s state) birdRole(name string) (snowflake.ID, bool) {
	app, ok := s.birds[name]
	if !ok {
		return 0, false
	}
	for _, r := range s.roles {
		if r.Managed && r.Tags != nil && r.Tags.BotID != nil && *r.Tags.BotID == app {
			return r.ID, true
		}
	}
	return 0, false
}

// target is the role an overwrite names, if it exists yet.
func (s state) target(name string) (snowflake.ID, bool) {
	if name == Everyone {
		return s.guild, true
	}
	if r, ok := s.role(name); ok {
		return r.ID, true
	}
	return s.birdRole(name)
}

// waits is whether o names a bird with no role here yet: neither @everyone
// nor a role the layout makes.
func (s state) waits(o Overwrite, layoutRoles map[string]bool) bool {
	if o.Role == Everyone || layoutRoles[o.Role] {
		return false
	}
	_, ok := s.birdRole(o.Role)
	return !ok
}

func (s state) channel(name string, typ discord.ChannelType, parent snowflake.ID) (discord.GuildChannel, bool) {
	for _, c := range s.channels {
		p := snowflake.ID(0)
		if c.ParentID() != nil {
			p = *c.ParentID()
		}
		if c.Type() == typ && p == parent && strings.EqualFold(c.Name(), name) {
			return c, true
		}
	}
	return nil, false
}

func (c Channel) typ() discord.ChannelType {
	if c.Voice {
		return discord.ChannelTypeGuildVoice
	}
	return discord.ChannelTypeGuildText
}

func wait(bird, channel string) string {
	return fmt.Sprintf("%s isn't in this server yet, so #%s gets its overwrite once it's added", bird, channel)
}

// differs is what about have a layout's role would change.
func differs(want Role, have discord.Role) []string {
	var out []string
	if have.Permissions != want.Perms {
		out = append(out, "permissions")
	}
	if have.Color != want.Color {
		out = append(out, "colour")
	}
	if have.Hoist != want.Hoist {
		out = append(out, "shown apart")
	}
	return out
}

// line is the change in the preview's diff block: short, so it doesn't
// wrap on a phone.
func (c change) line() string {
	switch c.kind {
	case roleCreate:
		return "+ role  " + c.role.Name
	case roleUpdate:
		return "! role  " + c.role.Name + ": " + strings.Join(c.fields, ", ")
	case categoryCreate:
		return "+ group " + c.category
	case channelCreate:
		if c.channel.Voice {
			return "+ voice " + c.channel.Name
		}
		return "+ text  #" + c.channel.Name
	}
	return "! perms #" + c.channel.Name + ": " + c.ow.Role
}

// says is an overwrite in words: "skua can send messages; can't add
// reactions".
func (o Overwrite) says() string {
	var parts []string
	if o.Allow != 0 {
		parts = append(parts, "can "+names(o.Allow))
	}
	if o.Deny != 0 {
		parts = append(parts, "can't "+names(o.Deny))
	}
	return o.Role + " " + strings.Join(parts, "; ")
}

// digest names a plan, so apply runs only the plan that was shown.
func digest(changes []change) string {
	h := fnv.New32a()
	for _, c := range changes {
		_, _ = fmt.Fprintf(h, "%s|%s|%d|%d\n", c.line(), c.ow.says(), c.role.Perms, c.id)
	}
	return fmt.Sprintf("%08x", h.Sum32())
}
