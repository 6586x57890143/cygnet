package provision

import (
	"strings"
	"unicode"

	"github.com/disgoorg/disgo/discord"
)

// A Layout is a server as cygnet builds it: roles top to bottom, then
// categories and their channels. Applying one only adds and changes what it
// names; anything else in the server is left alone, unless the admin asks
// for a fresh start.
type Layout struct {
	Name, About string
	Roles       []Role
	Categories  []Category
	// Reports is the text channel, one the layout makes, where cygnet posts
	// when the server drifts from it.
	Reports string
	// BotColor is every bot's role, the birds' first, so the bots read as
	// one in the member list beside the layout's own colours.
	BotColor int
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

// Channel is one channel. On Discord it is called Emoji, sep, Name
// ("📜│rules"); everywhere else, Name.
type Channel struct {
	Emoji       string
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

// Self is cygnet's own role in an Overwrite.
const Self = "cygnet"

// Birds in an Overwrite is every linked bird but cygnet, each by its own
// role, so a channel they all need lets in whichever are here.
const Birds = "birds"

// sep goes between a channel's emoji and its name. It is visible because no
// blank one survives: Discord rewrites every whitespace character in a
// channel name to a hyphen and strips the zero-width ones, while answering
// 200 for both, and U+2502 is what came back intact when weloveyou measured
// them. A separator Discord strips would leave the plan renaming the
// channel on every run.
const sep = "│"

const (
	send     = discord.PermissionSendMessages | discord.PermissionSendMessagesInThreads | discord.PermissionCreatePublicThreads
	post     = discord.PermissionSendMessages | discord.PermissionEmbedLinks | discord.PermissionAttachFiles
	moderate = discord.PermissionManageMessages | discord.PermissionManageThreads | discord.PermissionModerateMembers
	see      = discord.PermissionViewChannel
)

// layouts are compiled in, by name.
var layouts = []Layout{flock}

// flock is a small community server the birds work in together: read-only
// announcements skua's notify can still post to, a place to talk, a room to
// hang out in, and a staff channel where cygnet reports drift.
var flock = Layout{
	Name:     "flock",
	About:    "a small community: announcements, chatter, a voice room, and a nest where staff and the birds report",
	Reports:  "cygnet",
	BotColor: 0x8F96B8,
	Roles: []Role{
		{Name: "mod", Color: 0xD1A06A, Perms: moderate, Hoist: true},
		{Name: "member", Color: 0x7FA08C},
	},
	Categories: []Category{
		{Name: "welcome", Channels: []Channel{
			{Emoji: "📜", Name: "rules", Topic: "how this server works", Overwrites: []Overwrite{
				{Role: Everyone, Deny: send},
				{Role: "mod", Allow: send},
			}},
			{Emoji: "📣", Name: "announcements", Topic: "news from the server and the people it follows", Overwrites: []Overwrite{
				{Role: Everyone, Deny: send},
				{Role: "mod", Allow: send},
				{Role: "skua", Allow: post},
			}},
		}},
		{Name: "chatter", Channels: []Channel{
			{Emoji: "💬", Name: "general"},
			{Emoji: "📷", Name: "media"},
		}},
		{Name: "song", Channels: []Channel{
			{Emoji: "🔊", Name: "lounge", Voice: true},
		}},
		{Name: "nest", Channels: []Channel{
			{Emoji: "🦢", Name: "cygnet", Topic: "where the birds report on this server, and cygnet says when it drifts from its layout", Overwrites: []Overwrite{
				{Role: Everyone, Deny: see},
				{Role: "mod", Allow: see},
				{Role: Self, Allow: see | post},
				{Role: Birds, Allow: see | post},
			}},
		}},
	},
}

// archived is the category a fresh start moves the old channels into, when
// the admin keeps them. Its mark sits outside the tree.
const archived = "archive"

const archiveName = "┄ " + archived

// full is c's name on Discord.
func (c Channel) full() string {
	if c.Emoji == "" {
		return c.Name
	}
	return c.Emoji + sep + c.Name
}

// categoryName is the i-th of n categories' name on Discord: its name
// behind a mark that draws the categories down the sidebar as a tree
// ("╭ welcome", "├ chatter", "╰ nest").
func categoryName(name string, i, n int) string {
	mark := "├"
	switch {
	case n == 1:
		mark = "─"
	case i == 0:
		mark = "╭"
	case i == n-1:
		mark = "╰"
	}
	return mark + " " + name
}

// bare is a name without its decoration, whatever leads it that isn't a
// letter or a digit, so restyling a server renames its channels instead of
// making new ones beside them.
func bare(name string) string {
	return strings.TrimLeftFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
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
