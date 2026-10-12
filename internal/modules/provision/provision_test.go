package provision

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

const guild = snowflake.ID(3)

// fakeRest is a server with an @everyone role, a "birds" role, and the
// members listed; anyone else is Discord's unknown member.
type fakeRest struct {
	rest.Rest
	roles    []discord.Role
	rolesErr error
	members  map[snowflake.ID][]snowflake.ID
}

func (f *fakeRest) GetRoles(snowflake.ID, ...rest.RequestOpt) ([]discord.Role, error) {
	return f.roles, f.rolesErr
}

func (f *fakeRest) GetMember(_, user snowflake.ID, _ ...rest.RequestOpt) (*discord.Member, error) {
	roles, ok := f.members[user]
	if !ok {
		return nil, &rest.Error{Code: rest.JSONErrorCodeUnknownMember}
	}
	return &discord.Member{RoleIDs: roles}, nil
}

type fakeFleet struct {
	birds []hub.Bird
	// answers is each bird's manifest, or the error its call fails with.
	answers map[string]any
}

func (f *fakeFleet) Fleet() []hub.Bird { return f.birds }

func (f *fakeFleet) Call(_ context.Context, method string, _, out any) error {
	name, _, _ := strings.Cut(method, ".")
	switch a := f.answers[name].(type) {
	case error:
		return a
	case link.Manifest:
		raw, _ := json.Marshal(a)
		return json.Unmarshal(raw, out)
	}
	return link.Err(link.Offline)
}

const (
	view    = discord.PermissionViewChannel
	webhook = discord.PermissionManageWebhooks
	threads = discord.PermissionManageThreads
)

func server() *fakeRest {
	return &fakeRest{
		roles: []discord.Role{
			{ID: guild, Permissions: view},
			{ID: 20, Permissions: webhook},
		},
		members: map[snowflake.ID][]snowflake.ID{2: nil},
	}
}

func TestPermsOf(t *testing.T) {
	roles := []discord.Role{{ID: guild, Permissions: view}, {ID: 20, Permissions: webhook}, {ID: 21, Permissions: discord.PermissionAdministrator}}
	if got := permsOf(guild, roles, nil); got != view {
		t.Fatalf("@everyone alone: %v", got)
	}
	if got := permsOf(guild, roles, []snowflake.ID{20}); got != view|webhook {
		t.Fatalf("with a role: %v", got)
	}
	if got := permsOf(guild, roles, []snowflake.ID{21}); got != discord.PermissionsAll {
		t.Fatalf("administrator is everything: %v", got)
	}
}

func TestRender(t *testing.T) {
	birds := []row{
		{Name: "cygnet", Build: "abc", App: 2, In: true},
		{Name: "skua", Build: "35ebc52", App: 7, In: true, Have: view, Want: view | webhook | threads},
		{Name: "merlin", App: 8, Want: view},
		{Name: "peregrine", Err: "it took too long to answer"},
	}
	card := render(guild, birds, true)
	text := coretest.Text("", []discord.LayoutComponent{card})
	for _, want := range []string{
		"**provision** · fleet",
		"✓ **cygnet** · `abc`\n-# asks for nothing and has it",
		"! **skua** · `35ebc52`\n-# missing manage webhooks, manage threads",
		"▸ **merlin**\n-# not in this server yet",
		"✗ **peregrine**\n-# it took too long to answer",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("card lacks %q:\n%s", want, text)
		}
	}
	if card.AccentColor != brand.ColorError {
		t.Errorf("one bird failed, so the card is an error: %x", card.AccentColor)
	}
	var labels, urls []string
	for _, c := range card.Components {
		if r, ok := c.(discord.ActionRowComponent); ok {
			for _, b := range r.Components {
				btn := b.(discord.ButtonComponent)
				labels, urls = append(labels, btn.Label), append(urls, btn.URL)
			}
		}
	}
	if strings.Join(labels, ",") != "grant skua,add merlin,check again,◂ back" {
		t.Fatalf("buttons: %v", labels)
	}
	if want := "client_id=7&scope=bot+applications.commands&permissions=17716741120&guild_id=3&disable_guild_select=true"; !strings.HasSuffix(urls[0], want) {
		t.Fatalf("grant link %s", urls[0])
	}
}

func TestRenderMoods(t *testing.T) {
	ok := render(guild, []row{{Name: "cygnet", In: true, Have: view, Want: view}}, true)
	if ok.AccentColor != brand.ColorOK || !strings.Contains(coretest.Text("", []discord.LayoutComponent{ok}), "has the one permission it asks for") {
		t.Fatalf("all well: %x", ok.AccentColor)
	}
	off := render(guild, []row{{Name: "cygnet", In: true}}, false)
	if off.AccentColor != brand.ColorWarn || !strings.Contains(coretest.Text("", []discord.LayoutComponent{off}), "the hub is off") {
		t.Fatal("hub off should warn and say so")
	}
	if got := holds(view | webhook); got != "has all 2 permissions it asks for" {
		t.Fatal(got)
	}
}

// Six birds to fix need two button rows: Discord takes five a row.
func TestRenderButtonRows(t *testing.T) {
	var birds []row
	for i := range 6 {
		birds = append(birds, row{Name: "b" + string(rune('a'+i)), App: snowflake.ID(i + 10)})
	}
	rows := 0
	for _, c := range render(guild, birds, true).Components {
		if r, ok := c.(discord.ActionRowComponent); ok {
			rows++
			if len(r.Components) > 5 {
				t.Fatalf("a row of %d", len(r.Components))
			}
		}
	}
	if rows != 2 {
		t.Fatalf("%d rows", rows)
	}
}

func TestReason(t *testing.T) {
	for code, want := range map[link.Code]string{
		link.Offline: "not linked", link.Timeout: "too long", link.Busy: "busy", link.Invalid: "doesn't serve", link.Internal: "something went wrong",
	} {
		if got := reason(link.Err(code)); !strings.Contains(got, want) {
			t.Errorf("%s: %q", code, got)
		}
	}
	if reason(errors.New("x")) != "something went wrong asking" {
		t.Error("a plain error")
	}
}

func module(f Fleet) *Module {
	return New(f, func() discord.Permissions { return 0 }, func(i discord.Interaction) bool {
		return i.User().ID == 5
	}, guard.New())
}

// fleet is an admin pressing fleet on the panel, against r: the card it
// draws in place.
func fleet(t *testing.T, m *Module, r rest.Rest) string {
	t.Helper()
	e, _ := coretest.Button(t, refresh, nil)
	e.Client().Rest = r
	var got discord.MessageUpdate
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = d.(discord.MessageUpdate)
		return nil
	}
	if err := m.again(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if got.Flags == nil || !got.Flags.Has(discord.MessageFlagIsComponentsV2) {
		t.Fatalf("flags %v", got.Flags)
	}
	return coretest.Text("", *got.Components)
}

func TestFleet(t *testing.T) {
	f := &fakeFleet{
		birds: []hub.Bird{{Name: "merlin"}, {Name: "peregrine"}, {Name: "skua", Build: "old"}, {Name: "wren"}, {Name: "kite"}},
		answers: map[string]any{
			"skua":      link.Manifest{Build: "35ebc52", App: "7", Perms: "1024"},
			"merlin":    link.Manifest{App: "8", Perms: "16"},
			"peregrine": link.Manifest{Perms: "1"},
			"wren":      link.Manifest{App: "9", Perms: "x"},
			"kite":      link.Err(link.Timeout),
		},
	}
	r := server()
	r.members[7] = []snowflake.ID{20}
	text := fleet(t, module(f), r)
	for _, want := range []string{
		"✓ **skua** · `35ebc52`",
		"▸ **merlin**",
		"✗ **peregrine**\n-# its build doesn't say which app it is yet",
		"✗ **wren**\n-# it sent permissions that don't read as a number",
		"✗ **kite**\n-# didn't say what it needs: it took too long to answer",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("card lacks %q:\n%s", want, text)
		}
	}
}

func TestFleetHubOff(t *testing.T) {
	if text := fleet(t, module(nil), server()); !strings.Contains(text, "✓ **cygnet**") || !strings.Contains(text, "hub is off") {
		t.Fatal(text)
	}
}

func TestFleetUnreadable(t *testing.T) {
	r := server()
	r.rolesErr = errors.New("500")
	if text := fleet(t, module(nil), r); !strings.Contains(text, "couldn't read this server's roles") {
		t.Fatal(text)
	}
	if text := fleet(t, module(nil), &failingMember{server()}); !strings.Contains(text, "couldn't look it up here") {
		t.Fatal(text)
	}
}

type failingMember struct{ *fakeRest }

func (failingMember) GetMember(snowflake.ID, snowflake.ID, ...rest.RequestOpt) (*discord.Member, error) {
	return nil, errors.New("503")
}

func TestCheckAgain(t *testing.T) {
	e, _ := coretest.Button(t, refresh, nil)
	e.Client().Rest = server()
	var updated discord.MessageUpdate
	e.Respond = func(typ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		updated = d.(discord.MessageUpdate)
		return nil
	}
	if err := module(nil).again(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if updated.Components == nil || !strings.Contains(coretest.Text("", *updated.Components), "**cygnet**") {
		t.Fatal("the card was not redrawn")
	}

	e, _ = coretest.Button(t, refresh, func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": "6", "username": "other"}
	})
	err := module(nil).again(context.Background(), e)
	if tell, ok := errors.AsType[core.Tell](err); !ok || !strings.Contains(string(tell), "admins") {
		t.Fatalf("a member who isn't an admin: %v", err)
	}
}

func TestModule(t *testing.T) {
	m := module(nil)
	if m.Name() != "provision" || m.Perms() != needs(flock) || len(m.Commands()) != 1 || m.Components()[0].ID != refresh {
		t.Fatal("contract")
	}
	if m.Commands()[0].Tier != core.Admin || m.Want().Required == 0 || m.Help().About == "" {
		t.Fatal("tier, intents or help")
	}
}
