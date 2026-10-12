package provision

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
)

func TestLiveCard(t *testing.T) {
	g := newGuild()
	now := time.Now()

	// Without the server members intent a card doesn't tick, and says how
	// to make it.
	m := module(nil)
	text := coretest.Text("", []discord.LayoutComponent{m.card(context.Background(), g, guild, self)})
	if !strings.Contains(text, "turn on the server members intent") {
		t.Fatal(text)
	}
	m.live.open(guild, self, "t1", now)
	m.tick(context.Background(), g, guild, now)
	if len(g.edits) != 0 {
		t.Fatal("a card ticked without the intent")
	}

	// With it, each open card is redrawn once a bot joins, until its token
	// runs out.
	m.Live(func() bool { return true })
	if text := coretest.Text("", []discord.LayoutComponent{m.card(context.Background(), g, guild, self)}); !strings.Contains(text, "ticks itself off as each bird joins") {
		t.Fatal(text)
	}
	m.live.open(guild, self, "t1", now)
	m.live.open(guild, self, "t2", now.Add(time.Minute))
	m.tick(context.Background(), g, guild, now.Add(2*time.Minute))
	if len(g.edits) != 2 {
		t.Fatalf("%d edits", len(g.edits))
	}
	m.tick(context.Background(), g, guild, now.Add(lasts+30*time.Second))
	if len(g.edits) != 3 {
		t.Fatalf("%d edits after the first ran out", len(g.edits))
	}
	m.tick(context.Background(), g, guild, now.Add(time.Hour))
	if len(g.edits) != 3 || len(m.live.cards) != 0 {
		t.Fatalf("%d edits, %d guilds once both ran out", len(g.edits), len(m.live.cards))
	}
	// A failed edit is logged, and the next card still gets its own.
	m.live.open(guild, self, "t3", now)
	m.live.open(guild, self, "t4", now)
	g.fail["edit"] = errors.New("500")
	m.tick(context.Background(), g, guild, now)
	if len(g.edits) != 5 {
		t.Fatalf("%d edits", len(g.edits))
	}
}

func TestHeard(t *testing.T) {
	c := &bot.Client{}
	member := func(isBot bool) *events.GuildMemberJoin {
		return &events.GuildMemberJoin{GenericGuildMember: &events.GenericGuildMember{GenericEvent: events.NewGenericEvent(c, 0, 0), GuildID: guild, Member: discord.Member{User: discord.User{Bot: isBot}}}}
	}
	for _, x := range []struct {
		ev   bot.Event
		want bool
	}{
		{member(true), true},
		{member(false), false},
		{&events.RoleUpdate{GenericRole: &events.GenericRole{GenericEvent: events.NewGenericEvent(c, 0, 0), GuildID: guild}}, true},
		{&events.Ready{GenericEvent: events.NewGenericEvent(c, 0, 0)}, false},
	} {
		if g, _, ok := heard(x.ev); ok != x.want || (ok && g != guild) {
			t.Errorf("%T: %v", x.ev, ok)
		}
	}
}

// The fleet button opens a card that ticks, once the intent is on.
func TestFleetOpensLive(t *testing.T) {
	m := module(nil).Live(func() bool { return true })
	e, _ := coretest.Button(t, refresh, nil)
	e.Client().Rest = server()
	e.Respond = func(discord.InteractionResponseType, discord.InteractionResponseData, ...rest.RequestOpt) error {
		return nil
	}
	if err := m.again(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(m.live.cards[guild]) != 1 {
		t.Fatalf("cards %v", m.live.cards)
	}
	// OnEvent hears a run of bots joining and redraws once they settle,
	// off the gateway's goroutine.
	g := newGuild()
	join := &events.GuildMemberJoin{GenericGuildMember: &events.GenericGuildMember{GenericEvent: events.NewGenericEvent(&bot.Client{Rest: g}, 0, 0), GuildID: guild, Member: discord.Member{User: discord.User{Bot: true}}}}
	m.OnEvent(join)
	m.OnEvent(join)
	m.OnEvent(join)
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.live.mu.Lock()
		_, waiting := m.live.pending[guild]
		m.live.mu.Unlock()
		g.mu.Lock()
		n := len(g.edits)
		g.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			if n != 1 || !waiting {
				t.Fatalf("%d redraws for a burst of 3", n)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A guild with no card open starts nothing.
	m.live.soon(99, func() { t.Error("redrew a guild with no card") })
}
