package perms

import "testing"

// Discord's own numbers for a few bits, so a slip in the list shows.
func TestBits(t *testing.T) {
	for p, want := range map[Permissions]uint64{
		Administrator: 8, ManageGuild: 32, ViewChannel: 1024, SendMessages: 2048,
		ManageRoles: 268435456, ModerateMembers: 1099511627776, SendPolls: 562949953421312, PinMessages: 2251799813685248,
	} {
		if uint64(p) != want {
			t.Errorf("%s is %d, want %d", p.Names(), uint64(p), want)
		}
	}
	if (ViewChannel|SendMessages).Names() != "view channel, send messages" || Permissions(1<<47).Names() != "" || All.Names() == "" {
		t.Error("names")
	}
}

const guild = "1"

func server() Server {
	return Server{ID: guild, Owner: "9", Roles: []Role{
		{ID: guild, Perms: ViewChannel | SendMessages | ReadHistory | EmbedLinks},
		{ID: "mod", Perms: ManageMessages | ModerateMembers},
		{ID: "admin", Perms: Administrator},
	}}
}

func TestEffective(t *testing.T) {
	s := server()
	member := Member{ID: "5"}
	mod := Member{ID: "6", Roles: []string{"mod"}}
	for _, c := range []struct {
		name string
		m    Member
		ows  []Overwrite
		want Permissions
	}{
		{"everyone, server wide", member, nil, ViewChannel | SendMessages | ReadHistory | EmbedLinks},
		{"a role adds", mod, nil, ViewChannel | SendMessages | ReadHistory | EmbedLinks | ManageMessages | ModerateMembers},
		{"the owner is everything", Member{ID: "9"}, []Overwrite{{ID: guild, Deny: ViewChannel}}, All},
		{"administrator is everything", Member{ID: "7", Roles: []string{"admin"}}, []Overwrite{{ID: guild, Deny: ViewChannel}}, All},
		{"everyone's overwrite takes away", member, []Overwrite{{ID: guild, Deny: EmbedLinks}}, ViewChannel | SendMessages | ReadHistory},
		// Roles' overwrites count together, allow over deny, after
		// everyone's: mod gets back what everyone lost here.
		{"a role's allow beats everyone's deny", mod, []Overwrite{{ID: guild, Deny: SendMessages}, {ID: "mod", Allow: SendMessages}}, ViewChannel | SendMessages | ReadHistory | EmbedLinks | ManageMessages | ModerateMembers},
		{"one role's allow beats another's deny", Member{ID: "6", Roles: []string{"mod", "x"}}, []Overwrite{{ID: "x", Deny: EmbedLinks}, {ID: "mod", Allow: EmbedLinks}}, ViewChannel | SendMessages | ReadHistory | EmbedLinks | ManageMessages | ModerateMembers},
		{"the member's own comes last", mod, []Overwrite{{ID: "mod", Allow: AttachFiles}, {ID: "6", Member: true, Deny: AttachFiles | ManageMessages}}, ViewChannel | SendMessages | ReadHistory | EmbedLinks | ModerateMembers},
		{"another member's is theirs", member, []Overwrite{{ID: "6", Member: true, Deny: ViewChannel}}, ViewChannel | SendMessages | ReadHistory | EmbedLinks},
		{"unseen is nothing", member, []Overwrite{{ID: guild, Deny: ViewChannel}}, 0},
		{"unsendable can't embed", member, []Overwrite{{ID: guild, Deny: SendMessages}}, ViewChannel | ReadHistory},
		{"timed out sees and reads", Member{ID: "5", TimedOut: true}, []Overwrite{}, ViewChannel | ReadHistory},
	} {
		if got := Effective(s, c.m, c.ows); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got.Names(), c.want.Names())
		}
	}
}

func TestLacks(t *testing.T) {
	s := server()
	m := Member{ID: "5"}
	// A channel takes it away: the channel decides.
	if got := Lacks(MessageSend, s, m, []Overwrite{{ID: guild, Deny: SendMessages}}); got != SendMessages {
		t.Errorf("send: %s", got.Names())
	}
	// Server wide, a channel's overwrites don't count.
	if got := Lacks(MemberTimeout, s, m, []Overwrite{{ID: "5", Member: true, Allow: ModerateMembers}}); got != ModerateMembers {
		t.Errorf("timeout: %s", got.Names())
	}
	// A channel op with no overwrites is still a channel's: what can't be
	// sent can't be embedded.
	if got := Lacks(MessageAttach, s, m, nil); got != AttachFiles {
		t.Errorf("attach: %s", got.Names())
	}
	if Lacks(MessageEmbed, s, Member{ID: "6", Roles: []string{"mod"}}, nil) != 0 {
		t.Error("mod can embed")
	}
	for op, n := range Needs {
		if n.Perms == 0 || (n.Channel && !n.Perms.Has(ViewChannel)) {
			t.Errorf("%s: %s", op, n.Perms.Names())
		}
	}
}
