package provision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
)

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

func launchEvent(t *testing.T, sub, name string, guild *discord.Guild) (*events.ApplicationCommandInteractionCreate, *[]discord.MessageCreate) {
	e, sent := coretest.Event(t, "provision", func(p map[string]any) {
		opt := map[string]any{"name": sub, "type": 1}
		if name != "" {
			opt["options"] = []any{map[string]any{"name": "name", "type": 3, "value": name}}
		}
		p["data"] = map[string]any{"id": "6", "name": "provision", "type": 1, "options": []any{opt}}
	})
	e.Client().Caches = cache.New(cache.WithCaches(cache.FlagGuilds))
	if guild != nil {
		e.Client().Caches.AddGuild(*guild)
	}
	return e, sent
}

func TestLaunch(t *testing.T) {
	w := &worker{}
	s := w.serve(t)
	m := module(nil).Launch(&Edge{URL: s.URL + "/", Token: "secret"})
	hash := "abc"
	g := discord.Guild{ID: guild, Name: "silcord", Icon: &hash}

	e, sent := launchEvent(t, "launch", "silcord", &g)
	if err := m.run(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if text := coretest.Text("", (*sent)[0].Components); !strings.Contains(text, "**silcord** has its page at "+s.URL+"/silcord") {
		t.Fatal(text)
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
	m := module(nil).Launch(&Edge{URL: s.URL, Token: "secret"})
	g := discord.Guild{ID: guild, Name: "silcord"}

	// No launch here yet.
	e, _ := launchEvent(t, "campaign", "", &g)
	if tell, ok := asTell(m.run(context.Background(), e)); !ok || !strings.Contains(tell, "no launch yet") {
		t.Fatalf("no launch: %v", tell)
	}
	if _, err := pressOn(t, m, nil, campaignID, "5"); err == nil {
		t.Fatal("a press with no launch")
	}

	w.guilds[guild.String()] = "silcord"
	e, sent := launchEvent(t, "campaign", "", &g)
	if err := m.run(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	text := coretest.Text("", (*sent)[0].Components)
	for _, want := range []string{"stage     gathering", "on list   42 · 2 held", "in line   40 · 30 waiting", "opened 2026-10-11"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in\n%s", want, text)
		}
	}

	// The buttons: a stage, a release, a fresh look, each redrawing it.
	for _, c := range []struct{ id, want string }{
		{campaignID + ":stage:closed", "the campaign is closed now"},
		{campaignID + ":release:10", "released 3 invites each to 10 people"},
		{campaignID, "stage     closed"},
	} {
		got, err := pressOn(t, m, nil, c.id, "5")
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if text := coretest.Text("", *(*got)[0].(discord.MessageUpdate).Components); !strings.Contains(text, c.want) {
			t.Fatalf("%s: %s", c.id, text)
		}
	}
	if w.stage != "closed" || w.released != 10 {
		t.Fatalf("stage %s released %d", w.stage, w.released)
	}
	if _, err := pressOn(t, m, nil, campaignID+":stage:open", "6"); err == nil {
		t.Fatal("a member who isn't an admin pressed it")
	}
	w.fail = http.StatusBadGateway
	_, err := pressOn(t, m, nil, campaignID+":release:50", "5")
	if tell, _ := asTell(err); !strings.Contains(tell, "didn't answer") {
		t.Fatalf("down: %v", err)
	}
	e, _ = launchEvent(t, "campaign", "", &g)
	if tell, _ := asTell(m.run(context.Background(), e)); !strings.Contains(tell, "didn't answer") {
		t.Fatalf("down: %v", tell)
	}
}

func TestLaunchRefusals(t *testing.T) {
	g := discord.Guild{ID: guild, Name: "s"}
	for _, sub := range []string{"launch", "campaign"} {
		e, _ := launchEvent(t, sub, "silcord", &g)
		if tell, ok := asTell(module(nil).run(context.Background(), e)); !ok || !strings.Contains(tell, "isn't connected") {
			t.Fatalf("%s with no edge: %v", sub, tell)
		}
	}
	if _, err := pressOn(t, module(nil), nil, campaignID, "5"); err == nil {
		t.Fatal("a press with no edge")
	}

	w := &worker{}
	s := w.serve(t)
	m := module(nil).Launch(&Edge{URL: s.URL, Token: "secret"})
	for _, c := range []struct {
		name  string
		guild *discord.Guild
		want  string
	}{
		{"Not A Slug!", &g, "lowercase letters"},
		{"silcord", nil, "can't see this server"},
	} {
		e, _ := launchEvent(t, "launch", c.name, c.guild)
		if tell, ok := asTell(m.run(context.Background(), e)); !ok || !strings.Contains(tell, c.want) {
			t.Errorf("%s: %v", c.name, tell)
		}
	}

	w.fail = http.StatusConflict
	e, _ := launchEvent(t, "launch", "silcord", &g)
	if tell, _ := asTell(m.run(context.Background(), e)); !strings.Contains(tell, "another server's") {
		t.Errorf("conflict: %v", tell)
	}
	bad := module(nil).Launch(&Edge{URL: s.URL, Token: "wrong"})
	e, _ = launchEvent(t, "launch", "silcord", &g)
	if tell, _ := asTell(bad.run(context.Background(), e)); !strings.Contains(tell, "ADMIN_TOKEN") {
		t.Errorf("token: %v", tell)
	}
	gone := module(nil).Launch(&Edge{URL: "http://127.0.0.1:1", Token: "secret"})
	e, _ = launchEvent(t, "launch", "silcord", &g)
	if tell, _ := asTell(gone.run(context.Background(), e)); !strings.Contains(tell, "didn't answer") {
		t.Errorf("down: %v", tell)
	}
	if err := (&Edge{URL: "://bad"}).push(context.Background(), "x", g); err == nil {
		t.Error("a bad URL should fail")
	}
	if icon(guild, nil) != "" {
		t.Error("icon")
	}
	gone.refresh(g) // logs, doesn't panic
	module(nil).refresh(g)
}
