package perms

import "testing"

func TestRoute(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         Missing
		ok           bool
	}{
		{"POST", "/api/v10/channels/42/messages", Missing{Channel: "42", Op: MessageSend}, true},
		{"post", "https://discord.com/api/v10/channels/42/messages?wait=true", Missing{Channel: "42", Op: MessageSend}, true},
		{"PUT", "/channels/42/messages/7/reactions/%F0%9F%A6%A2/@me", Missing{Channel: "42", Op: MessageReact}, true},
		{"POST", "/channels/42/messages/bulk-delete", Missing{Channel: "42", Op: MessageDelete}, true},
		{"DELETE", "/channels/42/messages/7", Missing{Channel: "42", Op: MessageDelete}, true},
		{"PUT", "/channels/42/permissions/9", Missing{Channel: "42", Op: ChannelPerms}, true},
		{"PUT", "/guilds/3/members/5/roles/6", Missing{Guild: "3", Op: RoleManage}, true},
		{"DELETE", "/guilds/3/members/5", Missing{Guild: "3", Op: MemberKick}, true},
		{"PUT", "/guilds/3/members/5", Missing{Guild: "3", Op: MemberAdd}, true},
		{"GET", "/guilds/3/bans/5", Missing{Guild: "3", Op: MemberBan}, true},
		{"POST", "/channels/42/invites", Missing{Channel: "42", Op: InviteCreate}, true},
		{"PUT", "/guilds/3/onboarding", Missing{Guild: "3", Op: GuildManage}, true},
		{"GET", "/guilds/3/audit-logs?limit=1", Missing{Guild: "3", Op: GuildAudit}, true},
		// What the body decides, or the table doesn't name, isn't read.
		{"PATCH", "/guilds/3/members/5", Missing{}, false},
		{"POST", "/webhooks/1/token", Missing{}, false},
		{"GET", "/users/@me", Missing{}, false},
		// Only an ID stands for a server or a channel.
		{"PATCH", "/guilds/@me", Missing{}, false},
		{"POST", "/channels/x1/messages", Missing{}, false},
		{"POST", "/channels/123456789012345678901/messages", Missing{}, false},
		{"POST", "/channels//messages", Missing{}, false},
	} {
		got, ok := Route(c.method, c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("%s %s: %+v %v", c.method, c.path, got, ok)
		}
	}
	// Every op the table names is one the model knows.
	for _, r := range routes {
		if _, ok := Needs[r.op]; !ok {
			t.Errorf("%s %s: %s", r.method, r.path, r.op)
		}
	}
}
