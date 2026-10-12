// Package perms is the fleet's one model of Discord permissions: the bits,
// what each thing a bird does needs, and how Discord works out what a member
// may do in a channel. Every bird decides from this, the same way, rather
// than from its own bit math; it imports no Discord library, so any bird
// can use it whatever it's built on.
//
// When Discord refuses a bird for want of a permission (error 50013, which
// doesn't say which), the bird tells cygnet what it tried with a Missing,
// and cygnet works out from the bird's roles what it lacks, with Effective.
package perms

import (
	"math/bits"
	"slices"
	"strings"
)

// Permissions is Discord's permission bitfield.
type Permissions uint64

// The bits, as Discord numbers them.
const (
	CreateInvite Permissions = 1 << iota
	KickMembers
	BanMembers
	Administrator
	ManageChannels
	ManageGuild
	AddReactions
	ViewAuditLog
	PrioritySpeaker
	Stream
	ViewChannel
	SendMessages
	SendTTS
	ManageMessages
	EmbedLinks
	AttachFiles
	ReadHistory
	MentionEveryone
	ExternalEmojis
	ViewInsights
	Connect
	Speak
	MuteMembers
	DeafenMembers
	MoveMembers
	UseVAD
	ChangeNickname
	ManageNicknames
	ManageRoles
	ManageWebhooks
	ManageExpressions
	UseCommands
	RequestToSpeak
	ManageEvents
	ManageThreads
	CreatePublicThreads
	CreatePrivateThreads
	ExternalStickers
	SendInThreads
	UseActivities
	ModerateMembers
	ViewMonetization
	UseSoundboard
	CreateExpressions
	CreateEvents
	ExternalSounds
	SendVoiceMessages
	_
	_
	SendPolls
	UseExternalApps
	PinMessages
)

// All is every permission there is.
const All = PinMessages<<1 - 1

var names = []string{
	"create invite", "kick members", "ban members", "administrator", "manage channels", "manage server",
	"add reactions", "view audit log", "priority speaker", "video", "view channel", "send messages",
	"send tts messages", "manage messages", "embed links", "attach files", "read message history",
	"mention everyone", "use external emojis", "view server insights", "connect", "speak",
	"mute members", "deafen members", "move members", "use voice activity", "change nickname",
	"manage nicknames", "manage roles", "manage webhooks", "manage expressions", "use application commands",
	"request to speak", "manage events", "manage threads", "create public threads", "create private threads",
	"use external stickers", "send messages in threads", "use activities", "timeout members",
	"view creator monetization", "use soundboard", "create expressions", "create events",
	"use external sounds", "send voice messages", "", "", "create polls", "use external apps", "pin messages",
}

// Names is p in words, lowercase, in bit order, so it reads the same every
// time: "view channel, send messages".
func (p Permissions) Names() string {
	var out []string
	for p != 0 {
		bit := bits.TrailingZeros64(uint64(p))
		if bit < len(names) && names[bit] != "" {
			out = append(out, names[bit])
		}
		p &^= 1 << bit
	}
	return strings.Join(out, ", ")
}

// Has is whether p holds all of q.
func (p Permissions) Has(q Permissions) bool { return p&q == q }

// Op is something a bird does on Discord, by the fleet's shared name.
type Op string

// Need is what an Op takes, and whether a channel's overwrites decide it or
// only the server's roles do.
type Need struct {
	Perms   Permissions
	Channel bool
}

// The operations, and what Discord asks of each.
const (
	MessageSend    Op = "message.send"
	MessageEmbed   Op = "message.embed"
	MessageAttach  Op = "message.attach"
	MessageReact   Op = "message.react"
	MessageRead    Op = "message.read"
	MessageDelete  Op = "message.delete"
	MessagePin     Op = "message.pin"
	ThreadCreate   Op = "thread.create"
	ThreadSend     Op = "thread.send"
	WebhookManage  Op = "webhook.manage"
	ChannelManage  Op = "channel.manage"
	ChannelPerms   Op = "channel.perms"
	VoiceConnect   Op = "voice.connect"
	VoiceSpeak     Op = "voice.speak"
	VoiceMove      Op = "voice.move"
	RoleManage     Op = "role.manage"
	MemberTimeout  Op = "member.timeout"
	MemberKick     Op = "member.kick"
	MemberBan      Op = "member.ban"
	MemberNickname Op = "member.nickname"
	GuildManage    Op = "guild.manage"
	GuildAudit     Op = "guild.audit"
	GuildEvents    Op = "guild.events"
	GuildEmoji     Op = "guild.emoji"
)

// Needs is the table: what each Op takes. A channel's own Manage Permissions
// is Discord's Manage Roles bit, set on the channel.
var Needs = map[Op]Need{
	MessageSend:    {ViewChannel | SendMessages, true},
	MessageEmbed:   {ViewChannel | SendMessages | EmbedLinks, true},
	MessageAttach:  {ViewChannel | SendMessages | AttachFiles, true},
	MessageReact:   {ViewChannel | ReadHistory | AddReactions, true},
	MessageRead:    {ViewChannel | ReadHistory, true},
	MessageDelete:  {ViewChannel | ReadHistory | ManageMessages, true},
	MessagePin:     {ViewChannel | ReadHistory | PinMessages, true},
	ThreadCreate:   {ViewChannel | CreatePublicThreads, true},
	ThreadSend:     {ViewChannel | SendInThreads, true},
	WebhookManage:  {ViewChannel | ManageWebhooks, true},
	ChannelManage:  {ViewChannel | ManageChannels, true},
	ChannelPerms:   {ViewChannel | ManageRoles, true},
	VoiceConnect:   {ViewChannel | Connect, true},
	VoiceSpeak:     {ViewChannel | Connect | Speak, true},
	VoiceMove:      {ViewChannel | Connect | MoveMembers, true},
	RoleManage:     {ManageRoles, false},
	MemberTimeout:  {ModerateMembers, false},
	MemberKick:     {KickMembers, false},
	MemberBan:      {BanMembers, false},
	MemberNickname: {ManageNicknames, false},
	GuildManage:    {ManageGuild, false},
	GuildAudit:     {ViewAuditLog, false},
	GuildEvents:    {ManageEvents, false},
	GuildEmoji:     {ManageExpressions, false},
}

// Role is a role as Effective reads it. The @everyone role's ID is the
// server's.
type Role struct {
	ID    string
	Perms Permissions
}

// Overwrite is a channel's override for one role or one member.
type Overwrite struct {
	ID          string
	Member      bool
	Allow, Deny Permissions
}

// Server is what Effective needs of a server: its ID, which is @everyone's,
// its owner and its roles.
type Server struct {
	ID, Owner string
	Roles     []Role
}

// Member is who is asking: their ID, roles, and whether they're timed out.
type Member struct {
	ID       string
	Roles    []string
	TimedOut bool
}

// Effective is what m may do in s, in a channel with overwrites ows, or
// server wide when ows is nil, worked out as Discord does: the owner may do
// anything; then @everyone and every role m holds; Administrator is
// everything; then the channel's @everyone overwrite, its roles' together,
// and m's own; a timeout leaves only seeing and reading; and in a channel,
// what can't be seen can't be done, and what can't be sent can't carry
// embeds, files or mentions.
func Effective(s Server, m Member, ows []Overwrite) Permissions {
	if m.ID != "" && m.ID == s.Owner {
		return All
	}
	var p Permissions
	for _, r := range s.Roles {
		if r.ID == s.ID || slices.Contains(m.Roles, r.ID) {
			p |= r.Perms
		}
	}
	if p.Has(Administrator) {
		return All
	}
	if ows != nil {
		var allow, deny Permissions
		for _, o := range ows {
			switch {
			case !o.Member && o.ID == s.ID:
				p = p&^o.Deny | o.Allow
			case !o.Member && slices.Contains(m.Roles, o.ID):
				allow, deny = allow|o.Allow, deny|o.Deny
			}
		}
		p = p&^deny | allow
		for _, o := range ows {
			if o.Member && o.ID == m.ID {
				p = p&^o.Deny | o.Allow
			}
		}
	}
	if m.TimedOut {
		p &= ViewChannel | ReadHistory
	}
	if ows != nil {
		if !p.Has(ViewChannel) {
			return 0
		}
		if !p.Has(SendMessages) {
			p &^= SendTTS | EmbedLinks | AttachFiles | MentionEveryone
		}
	}
	return p
}

// Lacks is what op needs that m doesn't have, in s and, for an op a channel
// decides, in the channel with overwrites ows.
func Lacks(op Op, s Server, m Member, ows []Overwrite) Permissions {
	n := Needs[op]
	if !n.Channel {
		ows = nil
	} else if ows == nil {
		ows = []Overwrite{}
	}
	return n.Perms &^ Effective(s, m, ows)
}

// MissingMethod is what a bird calls to tell cygnet it was refused:
// "cygnet.missing".
const MissingMethod = "cygnet.missing"

// Missing is a bird telling cygnet Discord refused it: where, and what it
// was doing. cygnet works out what it lacks from its roles there, so a bird
// sends only what it knows. Channel is empty for an op the server decides.
type Missing struct {
	Guild   string `json:"guild"`
	Channel string `json:"channel,omitempty"`
	Op      Op     `json:"op"`
}
