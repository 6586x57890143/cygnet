package provision

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

type kind uint8

const (
	roleCreate kind = iota
	roleUpdate
	// roleOrder puts the layout's roles in its order, among the positions
	// they already hold.
	roleOrder
	categoryCreate
	channelCreate
	// rename restyles a category or channel that's already here, keeping
	// its history.
	rename
	overwriteSet
	// roleColor gives a bot's role the layout's bot colour, and nothing
	// else: the rest of that role is the bot's own.
	roleColor
	// remove deletes what a fresh start doesn't keep: the role called
	// role.Name, or else the channel or category id.
	remove
	// archive moves ids, the channels a fresh start removes, into the
	// archive, where they're hidden and kept.
	archive
	// onboard writes the layout's onboarding; fields is what differs.
	onboardSet
)

// change is one write a layout needs, in the order it has to happen: roles,
// then categories, then channels, then overwrites on channels that exist,
// so whatever a change names already exists when it runs, and in a fresh
// start what goes, last.
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
	// name is what a create or a rename calls it on Discord, and typ what
	// a rename renames.
	name string
	typ  discord.ChannelType
	ow   Overwrite
	// fields are what a roleUpdate changes.
	fields []string
	// ids are the channels an archive moves.
	ids []snowflake.ID
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
	// required is the channels a community server can't be without, which
	// Discord won't let anyone delete.
	required map[snowflake.ID]bool
	// community is whether the server is one, which onboarding needs, and
	// onboarding what it has, read only for a layout that has some.
	community  bool
	onboarding *discord.GuildOnboarding
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
	changes = append(changes, s.colors(l)...)
	if c, ok := s.order(l); ok {
		changes = append(changes, c)
	}
	var later []change
	for i, cat := range l.Categories {
		name := categoryName(cat.Name, i, len(l.Categories))
		parent, ok := s.channel(cat.Name, discord.ChannelTypeGuildCategory, 0)
		switch {
		case !ok:
			changes = append(changes, change{kind: categoryCreate, category: cat.Name, name: name})
		case parent.Name() != name:
			changes = append(changes, change{kind: rename, id: parent.ID(), category: cat.Name, name: name, typ: discord.ChannelTypeGuildCategory})
		}
		for _, ch := range cat.Channels {
			ch.Overwrites = s.expand(ch.Overwrites)
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
				changes = append(changes, change{kind: channelCreate, category: cat.Name, channel: kept, name: ch.full()})
				continue
			}
			if have.Name() != ch.full() {
				changes = append(changes, change{kind: rename, id: have.ID(), channel: ch, name: ch.full(), typ: ch.typ()})
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
	changes = append(changes, later...)
	// Onboarding needs a community server. Until this is one it waits, as
	// a bird's overwrite does, rather than standing as drift nobody can
	// put back.
	switch {
	case l.Onboarding == nil:
	case !s.community:
		waiting = append(waiting, "onboarding waits for community; turn it on in server settings and check again")
	default:
		if fields := s.onboardingDiff(l); len(fields) > 0 {
			changes = append(changes, change{kind: onboardSet, fields: fields})
		}
	}
	return changes, waiting
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

// colors gives every bot's role here the layout's bot colour, the birds'
// and cygnet's own included, as far as she reaches: her own sits at her
// top, where only an admin can recolour it, so that one is left to them.
func (s state) colors(l Layout) []change {
	var out []change
	for _, r := range s.roles {
		if !r.Managed || r.Tags == nil || r.Tags.BotID == nil || r.Color == l.BotColor || r.Position >= s.top {
			continue
		}
		out = append(out, change{kind: roleColor, id: r.ID, role: Role{Name: r.Name, Color: l.BotColor}})
	}
	return out
}

// sweep is what a fresh start removes: every channel, category and role l
// doesn't name, but the archive and what's in it, bots' own roles, and what
// Discord or safety won't let go. Channels go first, then the categories
// they were in, then roles.
func sweep(l Layout, s state) []change {
	keep := map[snowflake.ID]bool{}
	for _, cat := range l.Categories {
		parent, ok := s.channel(cat.Name, discord.ChannelTypeGuildCategory, 0)
		if !ok {
			continue
		}
		keep[parent.ID()] = true
		for _, ch := range cat.Channels {
			if c, ok := s.channel(ch.Name, ch.typ(), parent.ID()); ok {
				keep[c.ID()] = true
			}
		}
	}
	var attic snowflake.ID
	if a, ok := s.channel(archived, discord.ChannelTypeGuildCategory, 0); ok {
		attic = a.ID()
		keep[attic] = true
	}
	var chans, cats, roles []change
	for _, c := range s.channels {
		if keep[c.ID()] || (attic != 0 && c.ParentID() != nil && *c.ParentID() == attic) {
			continue
		}
		rm := change{kind: remove, id: c.ID(), name: c.Name(), typ: c.Type()}
		if s.required[c.ID()] {
			rm.blocked = "discord keeps it while this is a community server"
		}
		if c.Type() == discord.ChannelTypeGuildCategory {
			cats = append(cats, rm)
		} else {
			chans = append(chans, rm)
		}
	}
	for _, r := range s.roles {
		if r.ID == s.guild || r.Managed || slices.ContainsFunc(l.Roles, func(w Role) bool { return strings.EqualFold(w.Name, r.Name) }) {
			continue
		}
		rm := change{kind: remove, id: r.ID, role: Role{Name: r.Name}}
		switch {
		case r.Permissions.Has(discord.PermissionAdministrator):
			// Whoever runs this may be an admin by this role alone.
			rm.blocked = "it holds administrator; delete it yourself if you mean to"
		case r.Position >= s.top:
			rm.blocked = "its role is above cygnet's; drag cygnet's role above it"
		}
		roles = append(roles, rm)
	}
	return slices.Concat(chans, cats, roles)
}

// archivedTo is changes with the channels they remove moved into the
// archive instead, in one move where the first removal was.
func archivedTo(changes []change) []change {
	var out []change
	mv := change{kind: archive}
	at := -1
	for _, c := range changes {
		if c.kind == remove && c.role.Name == "" && c.typ != discord.ChannelTypeGuildCategory && c.blocked == "" {
			if at == -1 {
				at = len(out)
			}
			mv.ids = append(mv.ids, c.id)
			continue
		}
		out = append(out, c)
	}
	if at == -1 {
		return out
	}
	return slices.Insert(out, at, mv)
}

// order is the change that puts l's roles in order, if they exist and
// aren't. A role the layout is about to make is made at the bottom, in the
// layout's order, so it never needs one.
func (s state) order(l Layout) (change, bool) {
	prev := -1
	var block string
	for _, want := range l.Roles {
		have, ok := s.role(want.Name)
		if !ok {
			continue
		}
		if have.Position >= s.top {
			block = "its role is above cygnet's; drag cygnet's role above it"
		}
		if prev != -1 && have.Position >= prev {
			return change{kind: roleOrder, role: Role{Name: "order"}, blocked: block}, true
		}
		prev = have.Position
	}
	return change{}, false
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
		if c.Type() == typ && p == parent && strings.EqualFold(bare(c.Name()), bare(name)) {
			return c, true
		}
	}
	return nil, false
}

// expand puts each linked bird in place of Birds, in name order.
func (s state) expand(ows []Overwrite) []Overwrite {
	var out []Overwrite
	for _, o := range ows {
		if o.Role != Birds {
			out = append(out, o)
			continue
		}
		var names []string
		for name := range s.birds {
			if name != Self {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			out = append(out, Overwrite{Role: name, Allow: o.Allow, Deny: o.Deny})
		}
	}
	return out
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
	case roleColor:
		return "! role  " + c.role.Name + ": colour"
	case remove:
		switch {
		case c.role.Name != "":
			return "- role  " + c.role.Name
		case c.typ == discord.ChannelTypeGuildCategory:
			return "- group " + c.name
		case c.typ == discord.ChannelTypeGuildVoice || c.typ == discord.ChannelTypeGuildStageVoice:
			return "- voice " + c.name
		}
		return "- text  #" + c.name
	case archive:
		return "! move  " + count(len(c.ids), "channel") + " into " + archiveName
	case onboardSet:
		return "! onboarding: " + strings.Join(c.fields, ", ")
	case roleOrder:
		return "! order roles as the layout lists them"
	case categoryCreate:
		return "+ group " + c.name
	case channelCreate:
		if c.channel.Voice {
			return "+ voice " + c.name
		}
		return "+ text  #" + c.name
	case rename:
		if c.typ == discord.ChannelTypeGuildCategory {
			return "! name  " + c.category + ": " + c.name
		}
		return "! name  #" + c.channel.Name + ": " + c.name
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
