package provision

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

	"github.com/6586x57890143/cygnet/internal/core/coretest"
)

// worker is cygnet.lol's admin endpoint as a test sees it: what it was
// sent, and what it answers.
type worker struct {
	mu     sync.Mutex
	got    []map[string]string
	paths  []string
	status int
}

func (w *worker) serve(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.Header.Get("authorization") != "Bearer secret" {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		w.mu.Lock()
		w.got, w.paths = append(w.got, b), append(w.paths, r.URL.Path)
		status := w.status
		w.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		rw.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	return s
}

func launchEvent(t *testing.T, name string, guild *discord.Guild) (*events.ApplicationCommandInteractionCreate, *[]discord.MessageCreate) {
	e, sent := coretest.Event(t, "provision", func(p map[string]any) {
		p["data"] = map[string]any{"id": "6", "name": "provision", "type": 1, "options": []any{
			map[string]any{"name": "launch", "type": 1, "options": []any{map[string]any{"name": "name", "type": 3, "value": name}}},
		}}
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

	e, sent := launchEvent(t, "silcord", &g)
	if err := m.run(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if text := coretest.Text("", (*sent)[0].Components); !strings.Contains(text, "**silcord** has its page at "+s.URL+"/silcord") {
		t.Fatal(text)
	}
	if w.paths[0] != "/admin/silcord" || w.got[0]["guild"] != guild.String() || w.got[0]["icon"] != "https://cdn.discordapp.com/icons/3/abc.webp?size=256" {
		t.Fatalf("sent %v to %v", w.got, w.paths)
	}

	// A change to the server's name or picture reaches the page; anything
	// else, or a server that isn't a launch, doesn't.
	renamed := g
	renamed.Name = "silcord hq"
	m.OnEvent(&events.GuildUpdate{Guild: renamed, OldGuild: g})
	m.OnEvent(&events.GuildUpdate{Guild: g, OldGuild: g})
	other := discord.Guild{ID: 99, Name: "x"}
	m.refresh(other)
	for range 50 {
		w.mu.Lock()
		n := len(w.got)
		w.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.mu.Lock()
	if len(w.got) != 2 || w.got[1]["title"] != "silcord hq" {
		t.Fatalf("refreshed %v", w.got)
	}
	w.mu.Unlock()
}

func TestLaunchRefusals(t *testing.T) {
	g := discord.Guild{ID: guild, Name: "s"}
	e, _ := launchEvent(t, "silcord", &g)
	if tell, ok := asTell(module(nil).run(context.Background(), e)); !ok || !strings.Contains(tell, "isn't connected") {
		t.Fatalf("no edge: %v", tell)
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
		e, _ := launchEvent(t, c.name, c.guild)
		if tell, ok := asTell(m.run(context.Background(), e)); !ok || !strings.Contains(tell, c.want) {
			t.Errorf("%s: %v", c.name, tell)
		}
	}

	w.status = http.StatusConflict
	e, _ = launchEvent(t, "silcord", &g)
	if tell, _ := asTell(m.run(context.Background(), e)); !strings.Contains(tell, "another server's") {
		t.Errorf("conflict: %v", tell)
	}
	bad := module(nil).Launch(&Edge{URL: s.URL, Token: "wrong"})
	e, _ = launchEvent(t, "silcord", &g)
	if tell, _ := asTell(bad.run(context.Background(), e)); !strings.Contains(tell, "ADMIN_TOKEN") {
		t.Errorf("token: %v", tell)
	}
	gone := module(nil).Launch(&Edge{URL: "http://127.0.0.1:1", Token: "secret"})
	e, _ = launchEvent(t, "silcord", &g)
	if tell, _ := asTell(gone.run(context.Background(), e)); !strings.Contains(tell, "didn't answer") {
		t.Errorf("down: %v", tell)
	}
	if err := (&Edge{URL: "://bad"}).push(context.Background(), "x", g); err == nil {
		t.Error("a bad URL should fail")
	}
	if icon(guild, nil) != "" || errors.Is(edgeStatus(500), edgeStatus(404)) {
		t.Error("icon or status")
	}
	gone.launches.Store(guild, "silcord")
	gone.refresh(g) // logs, doesn't panic
}
