package launch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/store"
)

// guild is coretest's guild.
const guild = snowflake.ID(3)

// worker is cygnet.lol's admin API as a test sees it: what each launch
// was last sent.
type worker struct {
	mu   sync.Mutex
	sent map[string]map[string]any
	fail int // answer every request with this status, if set
}

func (w *worker) serve(t *testing.T) *httptest.Server {
	w.sent = map[string]map[string]any{}
	s := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if r.Header.Get("authorization") != "Bearer secret" {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		if w.fail != 0 {
			rw.WriteHeader(w.fail)
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		w.sent[strings.TrimPrefix(r.URL.Path, "/admin/")] = b
		_, _ = rw.Write([]byte("{}"))
	}))
	t.Cleanup(s.Close)
	return s
}

func (w *worker) got(name, key string) any {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sent[name][key]
}

// admin is member 5.
func admin(i discord.Interaction) bool { return i.User().ID == 5 }

// owner is the server's owner, who may see its list on the web.
const owner = snowflake.ID(5)

var server = discord.Guild{ID: guild, Name: "silcord", OwnerID: owner}

func known(id snowflake.ID) (discord.Guild, bool) { return server, id == guild }

// module is a module with no database: the flock, and the refusals.
func module(e *Edge) *Module { return New(e, nil, known, admin, nil) }

var t0 = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// launches is the launch store on CYGNET_TEST_DATABASE_URL, with every
// launch these tests name cleared.
func launches(t *testing.T) *store.Launches {
	t.Helper()
	dsn := os.Getenv("CYGNET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CYGNET_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"insert into gone (launch, at) select name, now() from launch where guild_id in (3, 4) on conflict do nothing",
		"delete from ledger where launch in (select name from launch where guild_id in (3, 4))",
		"delete from gone where launch in (select name from launch where guild_id in (3, 4))",
		"delete from line where launch in (select name from launch where guild_id in (3, 4))",
		"delete from code where guild_id in (3, 4)",
		"delete from launch where guild_id in (3, 4)",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}
	return store.NewLaunches(pool, []byte("a ledger key for these tests"))
}

// running is a module with a database and a Worker, its clock at t0.
func running(t *testing.T) (*Module, *worker) {
	w := &worker{}
	s := w.serve(t)
	m := New(&Edge{URL: s.URL + "/", Token: "secret", Breakglass: 9}, launches(t), known, admin, nil)
	m.now = func() time.Time { return t0 }
	return m, w
}

func asTell(err error) (string, bool) {
	t, ok := errors.AsType[core.Tell](err)
	return string(t), ok
}

// open is /launch, its reply's text.
func open(t *testing.T, m *Module) (string, error) {
	e, sent := coretest.Event(t, "launch", nil)
	if err := m.open(context.Background(), e); err != nil {
		return "", err
	}
	return coretest.Text("", (*sent)[0].Components), nil
}

// press is user pressing customID on the panel, with every response
// recorded.
func press(t *testing.T, m *Module, customID, user string) ([]discord.InteractionResponseData, error) {
	e, _ := coretest.Button(t, customID, func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
	})
	var got []discord.InteractionResponseData
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = append(got, d)
		return nil
	}
	return got, m.click(context.Background(), e)
}

// pressed is press's redrawn panel, as text.
func pressed(t *testing.T, m *Module, customID string) string {
	t.Helper()
	got, err := press(t, m, customID, "5")
	if err != nil {
		t.Fatalf("%s: %v", customID, err)
	}
	return coretest.Text("", *got[0].(discord.MessageUpdate).Components)
}

// submit is the form sent with name, in a server cygnet sees as g.
func submit(t *testing.T, m *Module, name string, g *discord.Guild) (string, error) {
	e, _ := coretest.Modal(t, nameID, map[string]string{"name": name}, nil)
	e.Client().Caches = cache.New(cache.WithCaches(cache.FlagGuilds))
	if g != nil {
		e.Client().Caches.AddGuild(*g)
	}
	var got discord.MessageUpdate
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = d.(discord.MessageUpdate)
		return nil
	}
	if err := m.named(context.Background(), e); err != nil {
		return "", err
	}
	return coretest.Text("", *got.Components), nil
}

func TestName(t *testing.T) {
	m, w := running(t)
	hash := "abc"
	g := discord.Guild{ID: guild, Name: "silcord", Icon: &hash, OwnerID: owner}

	// No page yet: the panel offers to name one, and its button opens the
	// form.
	text, err := open(t, m)
	if err != nil || !strings.Contains(text, "no page yet") {
		t.Fatalf("%v: %s", err, text)
	}
	got, err := press(t, m, id+":name", "5")
	if err != nil || got[0].(discord.ModalCreate).CustomID != nameID {
		t.Fatalf("%v: %v", err, got)
	}
	if text := pressed(t, m, id+":stage:open"); !strings.Contains(text, "no page yet") {
		t.Fatalf("a stage before a page: %s", text)
	}

	text, err = submit(t, m, " Silcord ", &g)
	if err != nil || !strings.Contains(text, "**silcord** has its page") || !strings.Contains(text, "/silcord") || !strings.Contains(text, "ledger    ✓ 0 entries") {
		t.Fatalf("%v: %s", err, text)
	}
	if w.got("silcord", "stage") != store.StageGathering || w.got("silcord", "admins").([]any)[1] != "9" {
		t.Fatalf("sent %v", w.sent["silcord"])
	}

	// A second name for the server, or the name in another server, is
	// refused.
	if _, err := submit(t, m, "other", &g); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("a second name: %v", err)
	}
	elsewhere := discord.Guild{ID: 4, Name: "x"}
	if _, err := submit(t, m, "silcord", &elsewhere); err == nil {
		t.Fatal("another server took the name")
	}

	// A change to the server's name reaches the page; a server that isn't a
	// launch sends nothing.
	renamed := g
	renamed.Name = "silcord hq"
	m.OnEvent(&events.GuildUpdate{Guild: renamed, OldGuild: g})
	m.OnEvent(&events.GuildUpdate{Guild: g, OldGuild: g})
	m.refresh(discord.Guild{ID: 99, Name: "x"})
	for range 100 {
		if w.got("silcord", "title") == "silcord hq" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("refreshed %v", w.sent)
}

func TestCampaign(t *testing.T) {
	m, w := running(t)
	ctx := context.Background()
	if err := m.store.Register(ctx, "silcord", guild, t0); err != nil {
		t.Fatal(err)
	}
	for u := range snowflake.ID(12) {
		if _, err := m.store.Join(ctx, "silcord", 100+u, "", nil, t0); err != nil {
			t.Fatal(err)
		}
	}
	text, err := open(t, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**launch** · silcord", "stage     gathering", "on list   12 · 0 held", "in line   12 · 12 waiting", "ledger    ✓ 12 entries"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in\n%s", want, text)
		}
	}

	// The buttons: a stage, a release, a fresh look, each redrawing it.
	for _, c := range []struct{ id, want string }{
		{id + ":stage:open", "the campaign is open now"},
		{id + ":release:10", "released 3 invites each to 10 people"},
		{id + ":release:50", "released 3 invites each to 2 people"},
		{id, "stage     open"},
	} {
		if text := pressed(t, m, c.id); !strings.Contains(text, c.want) {
			t.Fatalf("%s: %s", c.id, text)
		}
	}
	if text := pressed(t, m, id); !strings.Contains(text, "opened 2026-10-13") || !strings.Contains(text, "ledger    ✓ 25 entries") {
		t.Fatalf("after: %s", text)
	}
	if w.got("silcord", "stage") != store.StageOpen || w.got("silcord", "opens") != float64(t0.UnixMilli()) {
		t.Fatalf("the page wasn't told: %v", w.sent)
	}
	for _, bad := range []string{id + ":release:x", id + ":release:1000"} {
		if _, err := press(t, m, bad, "5"); err == nil {
			t.Fatalf("%s pressed", bad)
		}
	}
	if _, err := press(t, m, id+":stage:open", "6"); err == nil {
		t.Fatal("a member who isn't an admin pressed it")
	}

	// The Worker down: the change is kept, and the panel says the page
	// hasn't heard.
	w.fail = http.StatusBadGateway
	if text := pressed(t, m, id+":stage:closed"); !strings.Contains(text, "the campaign is closed now") || !strings.Contains(text, "didn't answer") {
		t.Fatalf("down: %s", text)
	}
	if text := pressed(t, m, id); !strings.Contains(text, "didn't answer") {
		t.Fatalf("check again while down: %s", text)
	}
}

func TestRefusals(t *testing.T) {
	g := discord.Guild{ID: guild, Name: "s"}
	off := module(nil)
	if _, err := open(t, off); !errors.Is(err, errOffline) {
		t.Fatalf("open with no edge: %v", err)
	}
	if _, err := press(t, off, id, "5"); !errors.Is(err, errOffline) {
		t.Fatalf("press with no edge: %v", err)
	}
	if _, err := submit(t, off, "silcord", &g); !errors.Is(err, errOffline) {
		t.Fatalf("submit with no edge: %v", err)
	}
	if _, err := open(t, module(&Edge{URL: "http://x"})); !errors.Is(err, errNoStore) {
		t.Fatalf("open with no database: %v", err)
	}
	off.refresh(g)
	off.expire()

	m, w := running(t)
	for _, c := range []struct {
		name  string
		guild *discord.Guild
		want  string
	}{
		{"Not A Slug!", &g, "lowercase letters"},
		{"silcord", nil, "can't see this server"},
	} {
		if _, err := submit(t, m, c.name, c.guild); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	e, _ := coretest.Modal(t, nameID, map[string]string{"name": "silcord"}, func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": "6", "username": "u"}
	})
	if tell, _ := asTell(m.named(context.Background(), e)); !strings.Contains(tell, "admins") {
		t.Errorf("a member who isn't an admin: %v", tell)
	}
	w.fail = http.StatusConflict
	if _, err := submit(t, m, "silcord", &g); err == nil || !strings.Contains(err.Error(), "another server's") {
		t.Errorf("the Worker refusing the name: %v", err)
	}
	w.fail = 0
	if err := (&Edge{URL: "://bad"}).push(context.Background(), store.Launch{Name: "x"}, g); err == nil {
		t.Error("a bad URL should fail")
	}
	if !strings.Contains(refusalEdge(edgeStatus(http.StatusNotFound)), "ADMIN_TOKEN") {
		t.Error("a wrong token")
	}
	if icon(guild, nil) != "" || people(1) != "1 person" {
		t.Error("icon or people")
	}
	if ledger(store.Verified{Broken: 4}) != "✗ broken at entry 4" || ledger(store.Verified{Mismatches: []string{"x"}}) != "✗ 1 counts it doesn't account for" {
		t.Error("ledger")
	}
}

func TestModule(t *testing.T) {
	m := module(nil)
	if m.Name() != "launch" || m.Perms() != 0 || len(m.Commands()) != 1 || m.Commands()[0].Tier != core.Admin {
		t.Fatal("contract")
	}
	if m.Want().Required == 0 || m.Help().About == "" || m.Components()[0].ID != id || m.Modals()[0].ID != nameID {
		t.Fatal("intents, help or IDs")
	}
}
