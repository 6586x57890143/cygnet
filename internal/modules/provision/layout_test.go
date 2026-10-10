package provision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/internal/hub"
	"github.com/6586x57890143/cygnet/link"
)

const (
	self     = snowflake.ID(2) // cygnet's app, as coretest sends it
	skuaApp  = snowflake.ID(7)
	selfRole = snowflake.ID(30)
	skuaRole = snowflake.ID(31)
)

type fakeChan struct {
	id, parent snowflake.ID
	name       string
	typ        discord.ChannelType
	ows        []discord.RolePermissionOverwrite
}

// guildFake is a server that keeps what is written to it, so a test can
// apply a layout and plan again against the result.
type guildFake struct {
	rest.Rest
	roles  []discord.Role
	chans  []fakeChan
	me     []snowflake.ID
	next   snowflake.ID
	writes []string
	// fail makes the write of that kind fail ("role", "channel",
	// "overwrite"), or the read ("read").
	fail  map[string]error
	edits []discord.MessageUpdate
	// sent is each message posted, by channel; a drift check posts from a
	// timer, so it's read under mu.
	mu   sync.Mutex
	sent map[snowflake.ID][]discord.MessageCreate
}

func (g *guildFake) CreateMessage(channel snowflake.ID, m discord.MessageCreate, _ ...rest.RequestOpt) (*discord.Message, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.fail["send"]; err != nil {
		return nil, err
	}
	if g.sent == nil {
		g.sent = map[snowflake.ID][]discord.MessageCreate{}
	}
	g.sent[channel] = append(g.sent[channel], m)
	return &discord.Message{}, nil
}

// posts is every message posted to channel so far.
func (g *guildFake) posts(channel snowflake.ID) []discord.MessageCreate {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]discord.MessageCreate(nil), g.sent[channel]...)
}

func newGuild() *guildFake {
	bot := func(app snowflake.ID) *discord.RoleTag { return &discord.RoleTag{BotID: &app} }
	return &guildFake{
		roles: []discord.Role{
			{ID: guild, Name: "@everyone", Permissions: discord.PermissionViewChannel | discord.PermissionSendMessages},
			{ID: selfRole, Name: "cygnet", Managed: true, Tags: bot(self), Position: 10, Permissions: needs(flock)},
			{ID: skuaRole, Name: "skua", Managed: true, Tags: bot(skuaApp), Position: 5},
		},
		me:   []snowflake.ID{selfRole},
		next: 100,
		fail: map[string]error{},
	}
}

func (g *guildFake) GetRoles(snowflake.ID, ...rest.RequestOpt) ([]discord.Role, error) {
	return g.roles, g.fail["read"]
}

func (g *guildFake) GetMember(_, user snowflake.ID, _ ...rest.RequestOpt) (*discord.Member, error) {
	if user != self {
		return nil, &rest.Error{Code: rest.JSONErrorCodeUnknownMember}
	}
	return &discord.Member{RoleIDs: g.me}, g.fail["member"]
}

func (g *guildFake) GetGuildChannels(snowflake.ID, ...rest.RequestOpt) ([]discord.GuildChannel, error) {
	if err := g.fail["channels"]; err != nil {
		return nil, err
	}
	var out []discord.GuildChannel
	for _, c := range g.chans {
		out = append(out, c.guildChannel())
	}
	return out, nil
}

// guildChannel is c as Discord sends it.
func (c fakeChan) guildChannel() discord.GuildChannel {
	ows := []map[string]any{}
	for _, o := range c.ows {
		ows = append(ows, map[string]any{"id": o.RoleID.String(), "type": 0, "allow": o.Allow, "deny": o.Deny})
	}
	raw := map[string]any{"id": c.id.String(), "type": c.typ, "name": c.name, "guild_id": guild.String(), "permission_overwrites": ows}
	if c.parent != 0 {
		raw["parent_id"] = c.parent.String()
	}
	b, _ := json.Marshal(raw)
	var u discord.UnmarshalChannel
	if err := json.Unmarshal(b, &u); err != nil {
		panic(err)
	}
	return u.Channel.(discord.GuildChannel)
}

func (g *guildFake) CreateRole(_ snowflake.ID, c discord.RoleCreate, _ ...rest.RequestOpt) (*discord.Role, error) {
	if err := g.fail["role"]; err != nil {
		return nil, err
	}
	g.next++
	// Discord makes a role at the bottom, just above @everyone.
	for i := range g.roles {
		if g.roles[i].ID != guild {
			g.roles[i].Position++
		}
	}
	r := discord.Role{ID: g.next, Name: c.Name, Permissions: *c.Permissions, Color: c.Color, Hoist: c.Hoist, Position: 1}
	g.roles = append(g.roles, r)
	g.writes = append(g.writes, "create role "+c.Name)
	return &r, nil
}

func (g *guildFake) UpdateRole(_, id snowflake.ID, u discord.RoleUpdate, _ ...rest.RequestOpt) (*discord.Role, error) {
	for i, r := range g.roles {
		if r.ID == id {
			g.roles[i].Permissions, g.roles[i].Color, g.roles[i].Hoist = *u.Permissions, *u.Color, *u.Hoist
			g.writes = append(g.writes, "update role "+r.Name)
			return &g.roles[i], nil
		}
	}
	return nil, errors.New("no such role")
}

func (g *guildFake) UpdateRolePositions(_ snowflake.ID, moves []discord.RolePositionUpdate, _ ...rest.RequestOpt) ([]discord.Role, error) {
	for _, mv := range moves {
		for i := range g.roles {
			if g.roles[i].ID == mv.ID {
				g.roles[i].Position = *mv.Position
			}
		}
	}
	g.writes = append(g.writes, "order roles")
	return g.roles, nil
}

func (g *guildFake) CreateGuildChannel(_ snowflake.ID, c discord.GuildChannelCreate, _ ...rest.RequestOpt) (discord.GuildChannel, error) {
	if err := g.fail["channel"]; err != nil {
		return nil, err
	}
	g.next++
	ch := fakeChan{id: g.next}
	var ows []discord.PermissionOverwrite
	switch c := c.(type) {
	case discord.GuildCategoryChannelCreate:
		ch.name, ch.typ = c.Name, discord.ChannelTypeGuildCategory
	case discord.GuildTextChannelCreate:
		ch.name, ch.typ, ch.parent, ows = c.Name, discord.ChannelTypeGuildText, c.ParentID, c.PermissionOverwrites
	case discord.GuildVoiceChannelCreate:
		ch.name, ch.typ, ch.parent, ows = c.Name, discord.ChannelTypeGuildVoice, c.ParentID, c.PermissionOverwrites
	}
	for _, o := range ows {
		ch.ows = append(ch.ows, o.(discord.RolePermissionOverwrite))
	}
	g.chans = append(g.chans, ch)
	g.writes = append(g.writes, "create channel "+ch.name)
	return ch.guildChannel(), nil
}

func (g *guildFake) UpdatePermissionOverwrite(channel, role snowflake.ID, u discord.PermissionOverwriteUpdate, _ ...rest.RequestOpt) error {
	if err := g.fail["overwrite"]; err != nil {
		return err
	}
	o := u.(discord.RolePermissionOverwriteUpdate)
	for i, c := range g.chans {
		if c.id != channel {
			continue
		}
		kept := []discord.RolePermissionOverwrite{{RoleID: role, Allow: *o.Allow, Deny: *o.Deny}}
		for _, have := range c.ows {
			if have.RoleID != role {
				kept = append(kept, have)
			}
		}
		g.chans[i].ows = kept
		g.writes = append(g.writes, "overwrite "+c.name)
	}
	return nil
}

func (g *guildFake) UpdateInteractionResponse(_ snowflake.ID, _ string, u discord.MessageUpdate, _ ...rest.RequestOpt) (*discord.Message, error) {
	g.edits = append(g.edits, u)
	return nil, nil
}

// planOn reads g as the module would and plans flock against it.
func planOn(t *testing.T, g *guildFake, birds map[string]snowflake.ID) planned {
	t.Helper()
	m := module(nil)
	if birds != nil {
		answers := map[string]any{}
		var fleet []hub.Bird
		for name, app := range birds {
			fleet = append(fleet, hub.Bird{Name: name})
			answers[name] = link.Manifest{App: app.String(), Perms: "0"}
		}
		m.fleet = &fakeFleet{birds: fleet, answers: answers}
	}
	p, err := m.read(context.Background(), g, guild, self, flock)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func lines(changes []change) string {
	var out []string
	for _, c := range changes {
		out = append(out, c.line())
	}
	return strings.Join(out, "\n")
}

// An empty server gets the whole layout, and applying it leaves nothing
// left to do: a second plan is empty.
func TestApplyThenNothingLeft(t *testing.T) {
	g := newGuild()
	p := planOn(t, g, map[string]snowflake.ID{"skua": skuaApp})
	want := strings.Join([]string{
		"+ role  mod", "+ role  member",
		"+ group welcome", "+ text  #rules", "+ text  #announcements",
		"+ group talk", "+ text  #general", "+ text  #media",
		"+ group voice", "+ voice lounge",
		"+ group staff", "+ text  #cygnet",
	}, "\n")
	if got := lines(p.changes); got != want {
		t.Fatalf("plan:\n%s", got)
	}
	if len(p.waiting) != 0 || p.short != 0 {
		t.Fatalf("waiting %v, short %v", p.waiting, p.short)
	}
	var seen []int
	done, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(n int) { seen = append(seen, n) })
	if err != nil || done != 12 || len(seen) != 12 {
		t.Fatalf("applied %d, %v", done, err)
	}
	// #announcements was made with skua's overwrite, by its role's ID.
	for _, c := range g.chans {
		if c.name == "announcements" {
			found := false
			for _, o := range c.ows {
				found = found || (o.RoleID == skuaRole && o.Allow == post)
			}
			if !found {
				t.Fatalf("announcements overwrites %+v", c.ows)
			}
		}
	}
	if again := planOn(t, g, map[string]snowflake.ID{"skua": skuaApp}); len(again.changes) != 0 {
		t.Fatalf("a second plan still has:\n%s", lines(again.changes))
	}
}

// A bird that isn't in the server yet: its overwrite waits and says so.
func TestBirdWaits(t *testing.T) {
	g := newGuild()
	p := planOn(t, g, nil)
	if len(p.waiting) != 1 || !strings.Contains(p.waiting[0], "skua isn't in this server yet, so #announcements") {
		t.Fatalf("waiting %v", p.waiting)
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	// Once skua is here, the overwrite it missed is the only change left.
	p = planOn(t, g, map[string]snowflake.ID{"skua": skuaApp})
	if got := lines(p.changes); got != "! perms #announcements: skua" {
		t.Fatalf("plan:\n%s", got)
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	if again := planOn(t, g, map[string]snowflake.ID{"skua": skuaApp}); len(again.changes) != 0 {
		t.Fatalf("left:\n%s", lines(again.changes))
	}
}

func TestRoleUpdateAndBlocked(t *testing.T) {
	g := newGuild()
	g.roles = append(g.roles,
		discord.Role{ID: 40, Name: "Mod", Position: 3, Color: 1},
		discord.Role{ID: 41, Name: "member", Position: 20, Hoist: true},
	)
	p := planOn(t, g, nil)
	var update, blocked *change
	for i, c := range p.changes {
		if c.kind == roleUpdate && c.role.Name == "mod" {
			update = &p.changes[i]
		}
		if c.blocked != "" && c.kind == roleUpdate {
			blocked = &p.changes[i]
		}
	}
	if update == nil || update.line() != "! role  mod: permissions, colour, shown apart" || update.blocked != "" {
		t.Fatalf("mod: %+v", update)
	}
	if blocked == nil || blocked.role.Name != "member" {
		t.Fatalf("member is above cygnet and should be blocked: %+v", blocked)
	}
	card := coretest.Text("", []discord.LayoutComponent{p.preview("")})
	if !strings.Contains(card, "! can't change **member**: its role is above cygnet's") {
		t.Fatal(card)
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(g.writes, ","), "update role member") || !strings.Contains(strings.Join(g.writes, ","), "update role Mod") {
		t.Fatalf("writes %v", g.writes)
	}
}

func TestPreview(t *testing.T) {
	g := newGuild()
	p := planOn(t, g, map[string]snowflake.ID{"skua": skuaApp})
	card := p.preview("")
	text := coretest.Text("", []discord.LayoutComponent{card})
	for _, want := range []string{"**provision** · flock", "▸ 12 changes to make it match", "```diff\n+ role  mod", "+ text  #cygnet\n```",
		"-# #rules: @everyone can't send messages", "-# #announcements: skua can send messages, embed links, attach files"} {
		if !strings.Contains(text, want) {
			t.Errorf("preview lacks %q:\n%s", want, text)
		}
	}
	apply := buttons(card)[0]
	if apply.Label != "apply 12 changes" || apply.Disabled || apply.CustomID != applyID+":flock:"+digest(p.changes) {
		t.Fatalf("apply button %+v", apply)
	}

	// Short of a permission: say which, offer the grant, hold apply.
	g.roles[1].Permissions = discord.PermissionManageChannels
	p = planOn(t, g, nil)
	card = p.preview("")
	text = coretest.Text("", []discord.LayoutComponent{card})
	if !strings.Contains(text, "! cygnet needs ") || !strings.Contains(text, "manage roles") || card.AccentColor != brand.ColorWarn {
		t.Fatal(text)
	}
	b := buttons(card)
	if b[0].Label != "grant cygnet" || !strings.Contains(b[0].URL, "client_id=2&") || !b[1].Disabled {
		t.Fatalf("buttons %+v", b)
	}

	// A server that matches says so and has nothing to apply.
	done := planned{l: flock}
	text = coretest.Text("", []discord.LayoutComponent{done.preview("")})
	if !strings.Contains(text, "✓ this server already matches **flock**") {
		t.Fatal(text)
	}
}

func TestOverwriteSaid(t *testing.T) {
	g := newGuild()
	p := planOn(t, g, map[string]snowflake.ID{"skua": skuaApp})
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	for i, c := range g.chans {
		if c.name == "rules" {
			g.chans[i].ows = nil
		}
	}
	p = planOn(t, g, map[string]snowflake.ID{"skua": skuaApp})
	text := coretest.Text("", []discord.LayoutComponent{p.preview("")})
	for _, want := range []string{
		"! perms #rules: @everyone", "! perms #rules: mod",
		"-# #rules: @everyone can't send messages, create public threads, send messages in threads",
		"-# #rules: mod can send messages, create public threads, send messages in threads",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
	if (Overwrite{Role: "x", Allow: view, Deny: webhook}).says() != "x can view channel; can't manage webhooks" {
		t.Error("both ways")
	}
}

func buttons(card discord.ContainerComponent) []discord.ButtonComponent {
	var out []discord.ButtonComponent
	for _, c := range card.Components {
		if r, ok := c.(discord.ActionRowComponent); ok {
			for _, b := range r.Components {
				out = append(out, b.(discord.ButtonComponent))
			}
		}
	}
	return out
}

func TestApplyStops(t *testing.T) {
	g := newGuild()
	p := planOn(t, g, nil)
	g.fail["channel"] = &rest.Error{Code: rest.JSONErrorCodeLackPermissionsToPerformAction, Response: &http.Response{StatusCode: 403}}
	done, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {})
	if done != 2 || err == nil || !strings.HasPrefix(err.Error(), "+ group welcome: ") {
		t.Fatalf("done %d, %v", done, err)
	}
	r := p.result(done, 10, err)
	text := coretest.Text("", []discord.LayoutComponent{r})
	if r.AccentColor != brand.ColorError || !strings.Contains(text, "✗ stopped after 2 of 10\n▰▰▱▱▱▱▱▱▱▱") || !strings.Contains(text, "missing a permission") {
		t.Fatal(text)
	}

	// The guard's hourly cap stops it with a Tell, before Discord is asked.
	g = newGuild()
	p = planOn(t, g, nil)
	gd := guard.New()
	for gd.Allow(guild, guard.RoleEdit) == nil {
	}
	done, err = apply(context.Background(), g, gd, p.s, flock, p.changes, func(int) {})
	if done != 0 || len(g.writes) != 0 {
		t.Fatalf("done %d, writes %v", done, g.writes)
	}
	text = coretest.Text("", []discord.LayoutComponent{p.result(done, 10, err)})
	if !strings.Contains(text, "hourly limit") {
		t.Fatal(text)
	}
}

func TestRefusal(t *testing.T) {
	for err, want := range map[error]string{
		&rest.Error{Code: rest.JSONErrorCodeMaximumGuildRolesReached}:    "as many roles",
		&rest.Error{Code: rest.JSONErrorCodeMaximumGuildChannelsReached}: "as many channels",
		context.DeadlineExceeded: "took too long",
		errors.New("x"):          "didn't take it",
		core.Tell("t"):           "",
	} {
		if got := refusal(err); want == "" && got != "" || !strings.Contains(got, want) {
			t.Errorf("%v: %q", err, got)
		}
	}
	if !struggling(&rest.Error{Response: &http.Response{StatusCode: 429}}) || struggling(&rest.Error{Response: &http.Response{StatusCode: 403}}) || struggling(nil) {
		t.Error("struggling")
	}
	if bar(0, 0) != "▱▱▱▱▱▱▱▱▱▱" || count(1, "change") != "1 change" {
		t.Error("bar or count")
	}
}

func layoutEvent(t *testing.T, g *guildFake) (*[]discord.MessageCreate, error) {
	e, sent := coretest.Event(t, "provision", func(p map[string]any) {
		p["data"] = map[string]any{"id": "6", "name": "provision", "type": 1, "options": []any{
			map[string]any{"name": "layout", "type": 1, "options": []any{map[string]any{"name": "name", "type": 3, "value": "flock"}}},
		}}
	})
	e.Client().Rest = g
	return sent, module(nil).run(context.Background(), e)
}

func TestLayoutCommand(t *testing.T) {
	g := newGuild()
	sent, err := layoutEvent(t, g)
	if err != nil {
		t.Fatal(err)
	}
	if text := coretest.Text("", (*sent)[0].Components); !strings.Contains(text, "**provision** · flock") {
		t.Fatal(text)
	}
	g.fail["read"] = errors.New("500")
	_, err = layoutEvent(t, g)
	if _, ok := asTell(err); !ok {
		t.Fatalf("a failed read should tell: %v", err)
	}
}

// press is an admin pressing customID, with every response recorded.
func press(t *testing.T, g *guildFake, customID, user string) (*[]discord.InteractionResponseData, error) {
	return pressOn(t, module(nil), g, customID, user)
}

func pressOn(t *testing.T, m *Module, g *guildFake, customID, user string) (*[]discord.InteractionResponseData, error) {
	e, _ := coretest.Button(t, customID, func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
	})
	e.Client().Rest = g
	var got []discord.InteractionResponseData
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = append(got, d)
		return nil
	}
	if strings.HasPrefix(customID, previewID) {
		return &got, m.previewAgain(context.Background(), e)
	}
	err := m.applyPressed(context.Background(), e)
	m.running.Wait()
	return &got, err
}

func TestApplyButton(t *testing.T) {
	g := newGuild()
	p := planOn(t, g, nil)
	id := applyID + ":flock:" + digest(p.changes)

	// A plan that no longer matches the server shows the new one instead.
	got, err := press(t, g, applyID+":flock:stale", "5")
	if err != nil || len(g.writes) != 0 {
		t.Fatalf("%v, writes %v", err, g.writes)
	}
	if u := (*got)[0].(discord.MessageUpdate); !strings.Contains(coretest.Text("", *u.Components), "the server changed since that preview") {
		t.Fatal("no note")
	}

	got, err = press(t, g, id, "5")
	if err != nil || (*got)[0] != nil || len(g.writes) != 12 {
		t.Fatalf("%v, %v, writes %v", err, *got, g.writes)
	}
	last := g.edits[len(g.edits)-1]
	if text := coretest.Text("", *last.Components); !strings.Contains(text, "✓ applied 12 changes; this server matches **flock**") {
		t.Fatal(text)
	}

	// One apply at a time in a server.
	m := module(nil)
	m.applying.Store(guild, true)
	p = planOn(t, newGuild(), nil)
	_, err = pressOn(t, m, newGuild(), applyID+":flock:"+digest(p.changes), "5")
	if tell, ok := asTell(err); !ok || !strings.Contains(tell, "already being applied") {
		t.Fatalf("a second apply: %v", err)
	}

	// check again redraws the preview from the server as it is now.
	got, err = press(t, g, previewID+":flock", "5")
	if err != nil {
		t.Fatal(err)
	}
	if u := (*got)[0].(discord.MessageUpdate); !strings.Contains(coretest.Text("", *u.Components), "already matches") {
		t.Fatal("check again")
	}
}

func TestButtonRefusals(t *testing.T) {
	g := newGuild()
	for _, c := range []struct{ id, user, want string }{
		{applyID + ":flock:x", "6", "admins"},
		{applyID, "5", "older cygnet"},
		{applyID + ":gone:x", "5", "isn't in this build"},
		{previewID + ":flock", "6", "admins"},
	} {
		_, err := press(t, g, c.id, c.user)
		if tell, ok := asTell(err); !ok || !strings.Contains(tell, c.want) {
			t.Errorf("%s by %s: %v", c.id, c.user, err)
		}
	}
	g.fail["channels"] = errors.New("500")
	for _, id := range []string{applyID + ":flock:x", previewID + ":flock"} {
		if _, err := press(t, g, id, "5"); err == nil {
			t.Errorf("%s: a failed read should fail", id)
		}
	}
	g.fail = map[string]error{"member": errors.New("500")}
	if _, err := press(t, g, previewID+":flock", "5"); err == nil {
		t.Error("a failed member read should fail")
	}
}

func TestProgress(t *testing.T) {
	text := coretest.Text("", []discord.LayoutComponent{planned{l: flock}.progress(3, 10)})
	if !strings.Contains(text, "▸ applying · 3 of 10\n▰▰▰▱▱▱▱▱▱▱") {
		t.Fatal(text)
	}
	r := planned{l: flock, waiting: []string{"skua waits"}}.result(1, 1, nil)
	if !strings.Contains(coretest.Text("", []discord.LayoutComponent{r}), "-# skua waits") {
		t.Fatal("waiting on the result")
	}
}

// Roles in the wrong order swap the positions they hold, and no more.
func TestRoleOrder(t *testing.T) {
	g := newGuild()
	g.roles = append(g.roles,
		discord.Role{ID: 40, Name: "mod", Position: 3, Color: 0xD1A06A, Hoist: true, Permissions: moderate},
		discord.Role{ID: 41, Name: "member", Position: 4, Color: 0x7FA08C},
	)
	p := planOn(t, g, nil)
	if p.changes[0].line() != "! order roles as the layout lists them" {
		t.Fatalf("plan:\n%s", lines(p.changes))
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for _, r := range g.roles {
		pos[r.Name] = r.Position
	}
	if pos["mod"] != 4 || pos["member"] != 3 || pos["cygnet"] != 10 {
		t.Fatalf("positions %v", pos)
	}
	if again := planOn(t, g, nil); len(again.changes) != 0 {
		t.Fatalf("left:\n%s", lines(again.changes))
	}

	// Above cygnet, she can't move them, and says so.
	g.roles[3].Position, g.roles[4].Position = 12, 13
	p = planOn(t, g, nil)
	text := coretest.Text("", []discord.LayoutComponent{p.preview("")})
	if !strings.Contains(text, "! can't reorder the roles: its role is above cygnet's") {
		t.Fatal(text)
	}

	g.fail["read"] = errors.New("500")
	if err := order(guild, g, nil, flock); err == nil {
		t.Fatal("a failed read should fail")
	}
}
