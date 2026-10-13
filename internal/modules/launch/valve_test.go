package launch

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/store"
)

func TestNext(t *testing.T) {
	last := store.Wave{ID: 1, Size: 40}
	for _, c := range []struct {
		name string
		r    reading
		size int
		why  string
	}{
		{"the first", reading{ceiling: 10}, 50, "the first wave is 50: 5 minutes of joins at the ceiling of 10 a minute"},
		{"calm", reading{ceiling: 10, last: last, joins: 4}, 80, "doubled to 80: joins peaked at 4.0 a minute against a ceiling of 10; skua didn't say how busy the server is"},
		{"calm, and quiet", reading{ceiling: 10, last: last, joins: 4, pace: &pace{Now: 30, Usual: 20}}, 80, "messages ran at 30 a minute against 20 usual"},
		{"calm, but loud", reading{ceiling: 10, last: last, joins: 4, pace: &pace{Now: 50, Usual: 20}}, 40, "held at 40"},
		{"busy", reading{ceiling: 10, last: last, joins: 7}, 40, "held at 40"},
		{"over", reading{ceiling: 10, last: last, joins: 12}, 20, "halved to 20: joins peaked at 12.0"},
		{"at the most", reading{ceiling: 10, last: store.Wave{ID: 1, Size: 150}, joins: 1}, 200, "doubled to 200"},
		{"at the least", reading{ceiling: 10, last: store.Wave{ID: 1, Size: 1}, joins: 20}, 1, "halved to 1"},
	} {
		size, why := next(c.r)
		if size != c.size || !strings.Contains(why, c.why) {
			t.Errorf("%s: %d %q, want %d %q", c.name, size, why, c.size, c.why)
		}
	}
}

func TestSettled(t *testing.T) {
	w := store.Wave{ID: 1, Admitted: 10, Arrived: 8, Opened: t0}
	if settled(w, 9, 10, t0.Add(time.Minute)) {
		t.Fatal("8 of 10 in, settled")
	}
	if w.Arrived = 9; !settled(w, 9, 10, t0.Add(time.Minute)) {
		t.Fatal("9 of 10 in, not settled")
	}
	w.Arrived = 0
	if settled(w, 4, 10, t0.Add(4*time.Minute)) {
		t.Fatal("quiet before a window passed, settled")
	}
	if !settled(w, 4, 10, t0.Add(window)) {
		t.Fatal("quiet after a window, not settled")
	}
	if !settled(w, 9, 10, t0.Add(settleBy)) {
		t.Fatal("15 minutes on, not settled")
	}
}

func TestJoinsRate(t *testing.T) {
	var j joins
	if j.peak(1, t0, t0) != 0 {
		t.Fatal("no joins yet")
	}
	// A burst of 10 in the second minute, then one a minute.
	for range 10 {
		j.add(1, t0.Add(90*time.Second))
	}
	for i := 2; i < 12; i++ {
		j.add(1, t0.Add(time.Duration(i)*time.Minute))
	}
	if p := j.peak(1, t0, t0.Add(12*time.Minute)); p != 15.0/5 {
		t.Fatalf("peak %v, want the burst and five more in 5 minutes", p)
	}
	if p := j.peak(1, t0.Add(8*time.Minute), t0.Add(12*time.Minute)); p != 6.0/5 {
		t.Fatalf("peak since minute 8 %v, want 6 in the 5 minutes to minute 9", p)
	}
	if p := j.peak(1, t0, t0.Add(time.Hour)); p != 0 {
		t.Fatalf("an hour on, kept %v", p)
	}
	m := module(nil)
	m.now = func() time.Time { return t0 }
	m.OnEvent(&events.GuildMemberJoin{GenericGuildMember: &events.GenericGuildMember{GuildID: 7}})
	if m.joins.peak(7, t0.Add(-time.Minute), t0) != 0.2 {
		t.Fatal("a join event wasn't counted")
	}
}

// discordOAuth is Discord's OAuth as a test sees it: a refresh answers
// with an access token, a revoke with nothing, and both are recorded.
func discordOAuth(t *testing.T) *[]string {
	var mu sync.Mutex
	var got []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		got = append(got, r.URL.Path+" "+r.Form.Get("grant_type")+r.Form.Get("refresh_token")+r.Form.Get("token"))
		mu.Unlock()
		if user, pass, _ := r.BasicAuth(); user != "2" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"access"}`))
	}))
	t.Cleanup(s.Close)
	old := discordAPI
	discordAPI = s.URL
	t.Cleanup(func() { discordAPI = old })
	return &got
}

// door is a Door that lets everyone in, added when given a token, and
// records who.
type door struct {
	mu   sync.Mutex
	in   map[snowflake.ID]string
	fail error
}

func (d *door) let(_ context.Context, _, user snowflake.ID, token string) (bool, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail != nil {
		return false, "", d.fail
	}
	if d.in == nil {
		d.in = map[snowflake.ID]string{}
	}
	d.in[user] = token
	if token == "" {
		return false, "onlyyou", nil
	}
	return true, "", nil
}

func TestValveLetsTheLineIn(t *testing.T) {
	m, _ := running(t)
	ctx := context.Background()
	calls := discordOAuth(t)
	d := &door{}
	m.Seal(opener(t)).Let(d.let, "secret")
	m.app = 2
	if err := m.store.Register(ctx, "silcord", guild, t0); err != nil {
		t.Fatal(err)
	}
	for u := range snowflake.ID(3) {
		if _, err := m.store.Join(ctx, "silcord", 200+u, "", nil, t0); err != nil {
			t.Fatal(err)
		}
	}
	sealed, _ := base64.RawURLEncoding.DecodeString(workerSealed)
	if err := m.store.Seal(ctx, "silcord", 201, sealed, t0); err != nil {
		t.Fatal(err)
	}
	if err := m.store.Valve(ctx, "silcord", 5, false, owner, t0); err != nil {
		t.Fatal(err)
	}

	// Gathering: the valve waits.
	m.tick(ctx, t0)
	if w, _ := m.store.Last(ctx, "silcord"); w.ID != 0 {
		t.Fatalf("a wave before it opened: %+v", w)
	}
	if err := m.store.SetStage(ctx, "silcord", store.StageOpen, owner, t0); err != nil {
		t.Fatal(err)
	}
	m.tick(ctx, t0)
	w, _ := m.store.Last(ctx, "silcord")
	if w.ID == 0 || w.Size != 3 || !strings.Contains(w.Why, "the first wave is 25") {
		t.Fatalf("the first wave: %+v", w)
	}
	// The sealed one was added with their refreshed sign in, which was then
	// given back; the others come for their way in.
	if d.in[201] != "access" || len(d.in) != 1 {
		t.Fatalf("let in %v", d.in)
	}
	if strings.Join(*calls, ",") != "/oauth2/token refresh_tokena refresh token from discord,/oauth2/token/revoke access" {
		t.Fatalf("oauth %v", *calls)
	}
	if got, _ := m.store.Sealed(ctx, "silcord", 10); len(got) != 0 {
		t.Fatalf("still sealed: %v", got)
	}

	// The next waits for the last to settle.
	m.tick(ctx, t0.Add(time.Minute))
	if w2, _ := m.store.Last(ctx, "silcord"); w2.ID != w.ID {
		t.Fatalf("a second wave before the first settled: %+v", w2)
	}
	if _, err := m.store.Join(ctx, "silcord", 210, "", nil, t0); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("joining an open launch: %v", err)
	}

	// Its way in, for someone the wave let in without a seal.
	got, err := ask(t, m, "admit", map[string]any{"launch": "silcord", "user": "200", "token": "theirs"})
	if err != nil || got["added"] != true || got["guild"] != "3" || d.in[200] != "theirs" {
		t.Fatalf("admit: %v %v", got, err)
	}
	if got, _ := ask(t, m, "admit", map[string]any{"launch": "silcord", "user": "202"}); got["invite"] != "onlyyou" {
		t.Fatalf("admit without a token: %v", got)
	}
	if _, err := ask(t, m, "admit", map[string]any{"launch": "silcord", "user": "299"}); !denied(err, NotOnList) {
		t.Fatalf("admit for someone not on it: %v", err)
	}
	if _, err := ask(t, m, "join", map[string]any{"launch": "silcord", "user": "203"}); !denied(err, Closed) {
		t.Fatalf("a join after it opened: %v", err)
	}

	// A sealed sign in that no longer adds anyone is dropped; they get their
	// way in on their page.
	if err := m.store.SetStage(ctx, "silcord", store.StageGathering, owner, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.Join(ctx, "silcord", 204, "", sealed, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(t, m, "admit", map[string]any{"launch": "silcord", "user": "204"}); !denied(err, NotYet) {
		t.Fatalf("admit before their wave: %v", err)
	}
	_ = m.store.SetStage(ctx, "silcord", store.StageOpen, owner, t0)
	d.fail = errors.New("discord says no")
	m.tick(ctx, t0.Add(settleBy))
	if got, _ := m.store.Sealed(ctx, "silcord", 10); len(got) != 0 {
		t.Fatalf("a failed sealed add kept its seal: %v", got)
	}
	if w3, _ := m.store.Last(ctx, "silcord"); w3.ID == w.ID {
		t.Fatal("no second wave once the first settled")
	}
	if _, err := ask(t, m, "admit", map[string]any{"launch": "silcord", "user": "204"}); err == nil {
		t.Fatal("the door failing, admitted")
	}
}

func TestValvePanel(t *testing.T) {
	m, _ := running(t)
	ctx := context.Background()
	if err := m.store.Register(ctx, "silcord", guild, t0); err != nil {
		t.Fatal(err)
	}
	text, _ := open(t, m)
	if !strings.Contains(text, "the valve runs once the launch is open, under 10 joins a minute") || !strings.Contains(text, "no wave yet") {
		t.Fatalf("panel %s", text)
	}
	if text := pressed(t, m, id+":valve:pause"); !strings.Contains(text, "the valve is paused") {
		t.Fatalf("pause: %s", text)
	}
	e, _ := coretest.Button(t, id+":ceiling", func(p map[string]any) {
		p["data"] = map[string]any{"custom_id": id + ":ceiling", "component_type": 3, "values": []string{"20"}}
	})
	var got []discord.InteractionResponseData
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = append(got, d)
		return nil
	}
	if err := m.click(ctx, e); err != nil {
		t.Fatal(err)
	}
	if text := coretest.Text("", *got[0].(discord.MessageUpdate).Components); !strings.Contains(text, "under 20 joins a minute") || !strings.Contains(text, "the valve paused") {
		t.Fatalf("ceiling: %s", text)
	}
	e, _ = coretest.Button(t, id+":ceiling", func(p map[string]any) {
		p["data"] = map[string]any{"custom_id": id + ":ceiling", "component_type": 3, "values": []string{"7"}}
	})
	if err := m.click(ctx, e); err == nil {
		t.Fatal("a ceiling the panel doesn't offer")
	}
	if text := pressed(t, m, id+":valve:run"); !strings.Contains(text, "the valve runs") {
		t.Fatalf("run: %s", text)
	}
	_ = m.store.SetStage(ctx, "silcord", store.StageOpen, owner, t0)
	if _, err := m.store.Join(ctx, "silcord", 300, "", nil, t0); err == nil {
		t.Fatal("joined an open launch")
	}
	_ = m.store.SetStage(ctx, "silcord", store.StageGathering, owner, t0)
	_, _ = m.store.Join(ctx, "silcord", 300, "", nil, t0)
	_ = m.store.SetStage(ctx, "silcord", store.StageOpen, owner, t0)
	m.tick(ctx, t0)
	if text := pressed(t, m, id); !strings.Contains(text, "wave") || !strings.Contains(text, "let in 1 person, 0 of them here") {
		t.Fatalf("after a wave: %s", text)
	}
}
