package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/link"
	"github.com/6586x57890143/cygnet/link/perms"
)

// birdsIn is a provisioned server where skua is in with its own role, and
// cygnet knows the client to answer refusals with.
func birdsIn(t *testing.T, mode string) (*Module, *guildFake, snowflake.ID) {
	t.Helper()
	m, g, reports, _ := provisioned(t, mode)
	g.members = map[snowflake.ID][]snowflake.ID{skuaApp: {skuaRole}}
	m.grants.rest = g
	return m, g, reports
}

func chanID(g *guildFake, name string) snowflake.ID {
	for _, c := range g.chans {
		if bare(c.name) == name && c.typ != discord.ChannelTypeGuildCategory {
			return c.id
		}
	}
	return 0
}

func refused(t *testing.T, m *Module, channel snowflake.ID, op perms.Op) (map[string]string, error) {
	t.Helper()
	in := perms.Missing{Guild: guild.String(), Op: op}
	if channel != 0 {
		in.Channel = channel.String()
	}
	body, _ := json.Marshal(in)
	out, err := m.Missing(context.Background(), "skua", body)
	if err != nil {
		return nil, err
	}
	return out.(map[string]string), nil
}

func TestRefusedProposed(t *testing.T) {
	m, g, reports := birdsIn(t, Propose)
	general := chanID(g, "general")

	// Everyone may send in #general, nobody embed: skua lacks embed links
	// server wide, so its role is where the grant goes, offered.
	out, err := refused(t, m, general, perms.MessageEmbed)
	if err != nil || out["lacks"] != "16384" || out["did"] != "proposed" {
		t.Fatalf("%v, %v", out, err)
	}
	posts := g.posts(reports)
	if len(posts) != 1 {
		t.Fatalf("%d reports", len(posts))
	}
	text := say(posts[0])
	if !strings.Contains(text, "! **skua** was refused `message.embed` in <#"+general.String()+">\n-# it lacks embed links") || !strings.Contains(text, "grant gives it that on its role") {
		t.Fatal(text)
	}
	btn := buttons(posts[0].Components[0].(discord.ContainerComponent))
	if len(btn) != 1 || btn[0].CustomID != grantID+":skua:"+general.String()+":message.embed" {
		t.Fatalf("buttons %+v", btn)
	}
	// The same refusal again is answered and not reported twice.
	if out, _ := refused(t, m, general, perms.MessageEmbed); out["did"] != "" || len(g.posts(reports)) != 1 {
		t.Fatalf("again: %v", out)
	}

	// Pressed, it's worked out again and granted on skua's role.
	got, err := pressOn(t, m, g, btn[0].CustomID, "5")
	if err != nil {
		t.Fatal(err)
	}
	if text := coretest.Text("", *(*got)[0].(discord.MessageUpdate).Components); !strings.Contains(text, "so cygnet gave it embed links on its role") {
		t.Fatal(text)
	}
	if role := g.roles[2]; role.ID != skuaRole || !role.Permissions.Has(discord.PermissionEmbedLinks) {
		t.Fatalf("skua's role %+v", role)
	}
	// Pressed again, there's nothing left to grant.
	got, _ = pressOn(t, m, g, btn[0].CustomID, "5")
	if text := coretest.Text("", *(*got)[0].(discord.MessageUpdate).Components); !strings.Contains(text, "has what `message.embed` needs now") {
		t.Fatal(text)
	}
	for _, id := range []string{grantID + ":skua", grantID + ":skua:x:message.send", grantID + ":skua:0:nope"} {
		if _, err := pressOn(t, m, g, id, "5"); err == nil {
			t.Errorf("%s: no refusal", id)
		}
	}
	if _, err := pressOn(t, m, g, btn[0].CustomID, "6"); err == nil {
		t.Error("a member who isn't an admin granted it")
	}
}

func TestRefusedActed(t *testing.T) {
	m, g, reports := birdsIn(t, Act)
	rules := chanID(g, "rules")
	// skua's own overwrite on #rules lets it add reactions and keeps it
	// from attaching files; a grant has to keep both.
	for i, c := range g.chans {
		if c.id == rules {
			g.chans[i].ows = append(g.chans[i].ows, discord.RolePermissionOverwrite{RoleID: skuaRole, Allow: discord.PermissionAddReactions, Deny: discord.PermissionAttachFiles | discord.PermissionSendMessages})
		}
	}
	// #rules denies everyone sending; server wide skua may, so the channel
	// is where it's given back, by an overwrite for its role.
	out, err := refused(t, m, rules, perms.MessageSend)
	if err != nil || out["did"] != "granted" {
		t.Fatalf("%v, %v", out, err)
	}
	if text := say(g.posts(reports)[0]); !strings.Contains(text, "so cygnet gave it send messages in <#"+rules.String()+">") {
		t.Fatal(text)
	}
	for _, c := range g.chans {
		if c.id != rules {
			continue
		}
		for _, o := range c.ows {
			if o.RoleID == skuaRole && (o.Allow != discord.PermissionAddReactions|discord.PermissionSendMessages || o.Deny != discord.PermissionAttachFiles) {
				t.Fatalf("skua's overwrite %+v", o)
			}
		}
	}

	// A grant Discord refuses is reported as such.
	m, g, reports = birdsIn(t, Act)
	g.fail["overwrite"] = errBoom
	if out, _ := refused(t, m, chanID(g, "rules"), perms.MessageSend); out["did"] != "reported" {
		t.Fatalf("refused grant: %v", out)
	}
	posts := g.posts(reports)
	if card := posts[len(posts)-1].Components[0].(discord.ContainerComponent); card.AccentColor != brand.ColorError {
		t.Fatal("not an error")
	}
}

var errBoom = &link.Error{Code: link.Internal}

func TestRefusedWatched(t *testing.T) {
	m, g, reports := birdsIn(t, Watch)
	out, err := refused(t, m, 0, perms.MemberTimeout)
	if err != nil || out["did"] != "reported" || out["lacks"] != "1099511627776" {
		t.Fatalf("%v, %v", out, err)
	}
	if posts := g.posts(reports); len(buttons(posts[0].Components[0].(discord.ContainerComponent))) != 0 || !strings.Contains(say(posts[0]), "refused `member.timeout`\n-# it lacks timeout members") {
		t.Fatal(say(posts[0]))
	}
	// Nothing lacking: answered, not reported.
	if out, _ := refused(t, m, chanID(g, "general"), perms.MessageSend); out["lacks"] != "0" || len(g.posts(reports)) != 1 {
		t.Fatalf("nothing lacking: %v", out)
	}
}

func TestRefusedRefusals(t *testing.T) {
	m, g, _ := birdsIn(t, Propose)
	for name, body := range map[string]string{
		"body":    `{`,
		"guild":   `{"guild":"x","op":"message.send"}`,
		"channel": `{"guild":"3","channel":"x","op":"message.send"}`,
		"op":      `{"guild":"3","op":"message.fly"}`,
		"elsewhere": func() string {
			return `{"guild":"999","channel":"` + chanID(g, "general").String() + `","op":"message.send"}`
		}(),
	} {
		if _, err := m.Missing(context.Background(), "skua", json.RawMessage(body)); err == nil {
			t.Errorf("%s: no refusal", name)
		}
	}
	if _, err := m.Missing(context.Background(), "wren", json.RawMessage(`{"guild":"3","op":"message.send"}`)); err == nil {
		t.Error("a bird with no manifest")
	}
	// A channel alone is enough: cygnet finds its server.
	if out, err := refused(t, m, chanID(g, "general"), perms.MessageEmbed); err != nil || out["lacks"] != "16384" {
		t.Fatalf("with its guild: %v, %v", out, err)
	}
	body, _ := json.Marshal(perms.Missing{Channel: chanID(g, "general").String(), Op: perms.MessageEmbed})
	if out, err := m.Missing(context.Background(), "skua", body); err != nil || out.(map[string]string)["lacks"] != "16384" {
		t.Fatalf("channel alone: %v, %v", out, err)
	}
	if _, err := m.Missing(context.Background(), "skua", json.RawMessage(`{"channel":"999999","op":"message.send"}`)); err == nil {
		t.Error("a channel cygnet can't see")
	}
	m.grants.rest = nil
	if _, err := m.Missing(context.Background(), "skua", json.RawMessage(`{"guild":"3","op":"message.send"}`)); err == nil {
		t.Error("before cygnet connected")
	}
}

// A bird waiting on its portal for an intent says so, with the link there.
func TestFleetIntents(t *testing.T) {
	card := render(guild, []row{{Name: "skua", App: skuaApp, In: true, Wants: gateway.IntentGuildMembers | gateway.IntentGuilds, Intents: gateway.IntentGuilds}}, true)
	text := coretest.Text("", []discord.LayoutComponent{card})
	if !strings.Contains(text, "-# its portal hasn't granted server members") || card.AccentColor != brand.ColorWarn {
		t.Fatal(text)
	}
	if b := buttons(card); b[0].Label != "skua portal" || b[0].URL != "https://discord.com/developers/applications/7/bot" {
		t.Fatalf("buttons %+v", b)
	}
	if intentNames(gateway.IntentGuilds) != "an intent it asks for" || intentNames(gateway.IntentMessageContent|gateway.IntentGuildPresences) != "presence, message content" {
		t.Error("names")
	}
}

// cygnet's own refusals come in as the bird cygnet. Server wide, Discord
// lets only a person give her more: the report carries her install link
// for this server, asking for all she needs here. When only a channel takes
// it away, she gives it back herself, as she would a bird.
func TestRefusedCygnet(t *testing.T) {
	m, g, reports := birdsIn(t, Act)
	m.want = func() discord.Permissions { return discord.PermissionCreateInstantInvite }
	m.grants.self = self
	body, _ := json.Marshal(perms.Missing{Guild: guild.String(), Op: perms.MemberBan})
	out, err := m.Missing(context.Background(), cygnet, body)
	if err != nil || out.(map[string]string)["did"] != "reported" {
		t.Fatalf("%v, %v", out, err)
	}
	post := g.posts(reports)[0]
	if text := say(post); !strings.Contains(text, "! **cygnet** was refused `member.ban`\n-# she lacks ban members; discord lets only a person give a bot more") {
		t.Fatal(text)
	}
	var url string
	for _, c := range post.Components[0].(discord.ContainerComponent).Components {
		if row, ok := c.(discord.ActionRowComponent); ok {
			url = row.Components[0].(discord.ButtonComponent).URL
		}
	}
	want := needs(flock) | discord.PermissionCreateInstantInvite | discord.PermissionBanMembers
	if !strings.HasSuffix(url, fmt.Sprintf("client_id=%d&scope=bot+applications.commands&permissions=%d&guild_id=%d&disable_guild_select=true", self, want, guild)) {
		t.Fatalf("install link %q", url)
	}
	if g.roles[1].ID != selfRole || g.roles[1].Permissions != needs(flock) {
		t.Fatalf("cygnet's role was touched: %+v", g.roles[1])
	}

	// #rules denies her sending, which she has server wide: given back on
	// the channel, for her role.
	rules := chanID(g, "rules")
	for i, c := range g.chans {
		if c.id == rules {
			g.chans[i].ows = append(g.chans[i].ows, discord.RolePermissionOverwrite{RoleID: selfRole, Deny: discord.PermissionSendMessages})
		}
	}
	body, _ = json.Marshal(perms.Missing{Guild: guild.String(), Channel: rules.String(), Op: perms.MessageSend})
	if out, err := m.Missing(context.Background(), cygnet, body); err != nil || out.(map[string]string)["did"] != "granted" {
		t.Fatalf("in a channel: %v, %v", out, err)
	}
	for _, c := range g.chans {
		for _, o := range c.ows {
			if c.id == rules && o.RoleID == selfRole && o.Deny&discord.PermissionSendMessages != 0 {
				t.Fatalf("still denied: %+v", o)
			}
		}
	}
}
