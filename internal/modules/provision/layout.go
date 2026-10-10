package provision

import "github.com/disgoorg/disgo/discord"

// A Layout is a server as cygnet builds it: roles top to bottom, then
// categories and their channels. Applying one only adds and changes what it
// names; anything else in the server is left alone.
type Layout struct {
	Name, About string
	Roles       []Role
	Categories  []Category
}

// Role is one role the layout keeps. Perms are its server-wide permissions.
type Role struct {
	Name  string
	Color int
	Perms discord.Permissions
	Hoist bool
}

type Category struct {
	Name     string
	Channels []Channel
}

type Channel struct {
	Name, Topic string
	Voice       bool
	Overwrites  []Overwrite
}

// Overwrite is a channel's override for one role: "@everyone", a role of
// the layout, or a bird by name, which is that bird's own role.
type Overwrite struct {
	Role        string
	Allow, Deny discord.Permissions
}

// Everyone is the @everyone role in an Overwrite.
const Everyone = "@everyone"

const (
	send     = discord.PermissionSendMessages | discord.PermissionSendMessagesInThreads | discord.PermissionCreatePublicThreads
	post     = discord.PermissionSendMessages | discord.PermissionEmbedLinks | discord.PermissionAttachFiles
	moderate = discord.PermissionManageMessages | discord.PermissionManageThreads | discord.PermissionModerateMembers
)

// layouts are compiled in, by name.
var layouts = []Layout{flock}

// flock is a small community server the birds work in together: read-only
// announcements skua's notify can still post to, a place to talk, a room to
// hang out in.
var flock = Layout{
	Name:  "flock",
	About: "a small community: announcements, a place to talk, a voice room",
	Roles: []Role{
		{Name: "mod", Color: 0xD1A06A, Perms: moderate, Hoist: true},
		{Name: "member", Color: 0x7FA08C},
	},
	Categories: []Category{
		{Name: "welcome", Channels: []Channel{
			{Name: "rules", Topic: "how this server works", Overwrites: []Overwrite{
				{Role: Everyone, Deny: send},
				{Role: "mod", Allow: send},
			}},
			{Name: "announcements", Topic: "news from the server and the people it follows", Overwrites: []Overwrite{
				{Role: Everyone, Deny: send},
				{Role: "mod", Allow: send},
				{Role: "skua", Allow: post},
			}},
		}},
		{Name: "talk", Channels: []Channel{
			{Name: "general"},
			{Name: "media"},
		}},
		{Name: "voice", Channels: []Channel{
			{Name: "lounge", Voice: true},
		}},
	},
}

// layout is the compiled-in layout called name.
func layout(name string) (Layout, bool) {
	for _, l := range layouts {
		if l.Name == name {
			return l, true
		}
	}
	return Layout{}, false
}

// Grants is every permission l hands out, in a role or an overwrite either
// way. Discord lets a bot hand out only what it holds itself, so cygnet asks
// for these on install.
func (l Layout) Grants() discord.Permissions {
	var p discord.Permissions
	for _, r := range l.Roles {
		p |= r.Perms
	}
	for _, c := range l.Categories {
		for _, ch := range c.Channels {
			for _, o := range ch.Overwrites {
				p |= o.Allow | o.Deny
			}
		}
	}
	return p
}
