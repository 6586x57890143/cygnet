package perms

import "strings"

// A route is one Discord API request as the table names it: a method and
// a path whose {g} is a server's ID, {c} a channel's and * anything.
type route struct {
	method, path string
	op           Op
}

// routes is what each request Discord can refuse for want of a permission
// needs, by the API's own paths, so a bird on any library reads its own
// refusals the same way. A request whose need depends on its body (editing
// a member can be a nickname, a timeout or roles) isn't here.
var routes = []route{
	{"POST", "/channels/{c}/messages", MessageSend},
	{"GET", "/channels/{c}/messages", MessageRead},
	{"GET", "/channels/{c}/messages/*", MessageRead},
	{"DELETE", "/channels/{c}/messages/*", MessageDelete},
	{"POST", "/channels/{c}/messages/bulk-delete", MessageDelete},
	{"PUT", "/channels/{c}/messages/*/reactions/*/@me", MessageReact},
	{"PUT", "/channels/{c}/pins/*", MessagePin},
	{"DELETE", "/channels/{c}/pins/*", MessagePin},
	{"PUT", "/channels/{c}/messages/pins/*", MessagePin},
	{"DELETE", "/channels/{c}/messages/pins/*", MessagePin},
	{"POST", "/channels/{c}/threads", ThreadCreate},
	{"POST", "/channels/{c}/messages/*/threads", ThreadCreate},
	{"GET", "/channels/{c}/webhooks", WebhookManage},
	{"POST", "/channels/{c}/webhooks", WebhookManage},
	{"PATCH", "/channels/{c}", ChannelManage},
	{"DELETE", "/channels/{c}", ChannelManage},
	{"PUT", "/channels/{c}/permissions/*", ChannelPerms},
	{"DELETE", "/channels/{c}/permissions/*", ChannelPerms},
	{"POST", "/guilds/{g}/channels", ChannelManage},
	{"PATCH", "/guilds/{g}/channels", ChannelManage},
	{"POST", "/guilds/{g}/roles", RoleManage},
	{"PATCH", "/guilds/{g}/roles", RoleManage},
	{"PATCH", "/guilds/{g}/roles/*", RoleManage},
	{"DELETE", "/guilds/{g}/roles/*", RoleManage},
	{"PUT", "/guilds/{g}/members/*/roles/*", RoleManage},
	{"DELETE", "/guilds/{g}/members/*/roles/*", RoleManage},
	{"DELETE", "/guilds/{g}/members/*", MemberKick},
	{"PUT", "/guilds/{g}/bans/*", MemberBan},
	{"DELETE", "/guilds/{g}/bans/*", MemberBan},
	{"POST", "/guilds/{g}/bulk-ban", MemberBan},
	{"PATCH", "/guilds/{g}", GuildManage},
	{"PUT", "/guilds/{g}/onboarding", GuildManage},
	{"GET", "/guilds/{g}/audit-logs", GuildAudit},
	{"POST", "/guilds/{g}/scheduled-events", GuildEvents},
	{"PATCH", "/guilds/{g}/scheduled-events/*", GuildEvents},
	{"DELETE", "/guilds/{g}/scheduled-events/*", GuildEvents},
	{"POST", "/guilds/{g}/emojis", GuildEmoji},
	{"PATCH", "/guilds/{g}/emojis/*", GuildEmoji},
	{"DELETE", "/guilds/{g}/emojis/*", GuildEmoji},
}

// Route is what a Discord API request needs, read from its method and path
// (with or without the "/api/v10" before it and a query after), and the
// server and channel the path names. False for a request the table doesn't
// name. A bird refused with 50013 sends what this gives it as a Missing.
func Route(method, path string) (m Missing, ok bool) {
	path, _, _ = strings.Cut(path, "?")
	if i := strings.Index(path, "/channels/"); i >= 0 {
		path = path[i:]
	} else if i := strings.Index(path, "/guilds/"); i >= 0 {
		path = path[i:]
	}
	got := strings.Split(strings.Trim(path, "/"), "/")
	for _, r := range routes {
		if r.method != strings.ToUpper(method) {
			continue
		}
		want := strings.Split(strings.Trim(r.path, "/"), "/")
		if len(want) != len(got) {
			continue
		}
		m = Missing{Op: r.op}
		matched := true
		for i, w := range want {
			switch w {
			case "{g}":
				m.Guild, matched = got[i], matched && id(got[i])
			case "{c}":
				m.Channel, matched = got[i], matched && id(got[i])
			case "*":
			default:
				matched = matched && w == got[i]
			}
		}
		if matched {
			return m, true
		}
	}
	return Missing{}, false
}

// id is whether s reads as a Discord ID: digits, at most a uint64's.
func id(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
