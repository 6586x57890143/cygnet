package launch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
)

// guild is coretest's guild.
const guild = snowflake.ID(3)

// worker is cygnet.lol's admin API as a test sees it: launches by name
// and server, each one's stage, and what it was sent.
type worker struct {
	mu       sync.Mutex
	guilds   map[string]string // guild to launch
	titles   map[string]string
	stage    string
	released int
	fail     int // answer every request with this status, if set
}

func (w *worker) serve(t *testing.T) *httptest.Server {
	w.guilds, w.titles, w.stage = map[string]string{}, map[string]string{}, "gathering"
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
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/"), "/")
		answer := func(v any) { _ = json.NewEncoder(rw).Encode(v) }
		switch {
		case r.Method == http.MethodPut && len(parts) == 1:
			w.guilds[b["guild"].(string)], w.titles[parts[0]] = parts[0], b["title"].(string)
			answer(map[string]string{"launch": parts[0]})
		case parts[0] == "guild":
			name, ok := w.guilds[parts[1]]
			if !ok {
				rw.WriteHeader(http.StatusNotFound)
				return
			}
			answer(map[string]string{"launch": name})
		case r.Method == http.MethodGet && len(parts) == 1:
			answer(map[string]any{"launch": parts[0], "stage": w.stage, "opened": "2026-10-11T00:00:00Z", "total": 42, "held": 2, "in_line": 40, "waiting": 30, "released_to": w.released})
		case parts[1] == "stage":
			w.stage = b["stage"].(string)
			answer(map[string]string{})
		case parts[1] == "release":
			w.released += int(b["count"].(float64))
			answer(map[string]int{"released": int(b["count"].(float64))})
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// admin is member 5.
func admin(i discord.Interaction) bool { return i.User().ID == 5 }

func module(e *Edge) *Module { return New(e, admin, nil) }

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
	w := &worker{}
	s := w.serve(t)
	m := module(&Edge{URL: s.URL + "/", Token: "secret"})
	hash := "abc"
	g := discord.Guild{ID: guild, Name: "silcord", Icon: &hash}

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

	text, err = submit(t, m, " Silcord ", &g)
	if err != nil || !strings.Contains(text, "**silcord** has its page") || !strings.Contains(text, s.URL+"/silcord") {
		t.Fatalf("%v: %s", err, text)
	}

	// A change to the server's name reaches the page, found through the
	// Worker, so it holds across a restart; a server that isn't a launch
	// sends nothing.
	renamed := g
	renamed.Name = "silcord hq"
	m.OnEvent(&events.GuildUpdate{Guild: renamed, OldGuild: g})
	m.OnEvent(&events.GuildUpdate{Guild: g, OldGuild: g})
	m.refresh(discord.Guild{ID: 99, Name: "x"})
	for range 100 {
		w.mu.Lock()
		done := w.titles["silcord"] == "silcord hq"
		w.mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("refreshed %v", w.titles)
}

func TestCampaign(t *testing.T) {
	w := &worker{}
	s := w.serve(t)
	m := module(&Edge{URL: s.URL, Token: "secret"})

	// A stage pressed on a server with no launch only redraws the panel.
	if got, err := press(t, m, id+":stage:open", "5"); err != nil || !strings.Contains(coretest.Text("", *got[0].(discord.MessageUpdate).Components), "no page yet") {
		t.Fatalf("no launch: %v", err)
	}

	w.guilds[guild.String()] = "silcord"
	text, err := open(t, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**launch** · silcord", "stage     gathering", "on list   42 · 2 held", "in line   40 · 30 waiting", "opened 2026-10-11"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in\n%s", want, text)
		}
	}

	// The buttons: a stage, a release, a fresh look, each redrawing it.
	for _, c := range []struct{ id, want string }{
		{id + ":stage:closed", "the campaign is closed now"},
		{id + ":release:10", "released 3 invites each to 10 people"},
		{id, "stage     closed"},
	} {
		got, err := press(t, m, c.id, "5")
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if text := coretest.Text("", *got[0].(discord.MessageUpdate).Components); !strings.Contains(text, c.want) {
			t.Fatalf("%s: %s", c.id, text)
		}
	}
	if w.stage != "closed" || w.released != 10 {
		t.Fatalf("stage %s released %d", w.stage, w.released)
	}
	if _, err := press(t, m, id+":stage:open", "6"); err == nil {
		t.Fatal("a member who isn't an admin pressed it")
	}
	w.fail = http.StatusBadGateway
	if _, err := press(t, m, id+":release:50", "5"); err == nil || !strings.Contains(err.Error(), "didn't answer") {
		t.Fatalf("down: %v", err)
	}
	if _, err := open(t, m); err == nil || !strings.Contains(err.Error(), "didn't answer") {
		t.Fatalf("down: %v", err)
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

	w := &worker{}
	s := w.serve(t)
	m := module(&Edge{URL: s.URL, Token: "secret"})
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

	for _, c := range []struct {
		m    *Module
		fail int
		want string
	}{
		{m, http.StatusConflict, "another server's"},
		{module(&Edge{URL: s.URL, Token: "wrong"}), 0, "ADMIN_TOKEN"},
		{module(&Edge{URL: "http://127.0.0.1:1", Token: "secret"}), 0, "didn't answer"},
	} {
		w.fail = c.fail
		if _, err := submit(t, c.m, "silcord", &g); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.want, err)
		}
	}
	if err := (&Edge{URL: "://bad"}).push(context.Background(), "x", g); err == nil {
		t.Error("a bad URL should fail")
	}
	if icon(guild, nil) != "" || people(1) != "1 person" {
		t.Error("icon or people")
	}
	module(&Edge{URL: "http://127.0.0.1:1", Token: "secret"}).refresh(g) // logs, doesn't panic
	off.refresh(g)
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
