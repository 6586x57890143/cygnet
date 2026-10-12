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
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/internal/hub"
	"github.com/6586x57890143/cygnet/link"
)

// provisioned is a server with flock applied and skua in it, kept in mode,
// and the module keeping it; saved collects what the module saves.
func provisioned(t *testing.T, mode string) (*Module, *guildFake, snowflake.ID, *[]Kept) {
	t.Helper()
	g := newGuild()
	birds := map[string]snowflake.ID{"skua": skuaApp}
	p := planOn(t, g, birds)
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	m := module(&fakeFleet{birds: []hub.Bird{{Name: "skua"}}, answers: map[string]any{"skua": link.Manifest{App: skuaApp.String(), Perms: "0"}}})
	m.settle = time.Millisecond
	var saved []Kept
	m.Keep(map[snowflake.ID]Kept{guild: {Layout: "flock", Mode: mode}}, func(_ context.Context, _ snowflake.ID, k Kept) error {
		saved = append(saved, k)
		return nil
	}, nil)
	reports, ok := (state{channels: mustChannels(t, g)}).named("cygnet")
	if !ok {
		t.Fatal("no #cygnet")
	}
	return m, g, reports, &saved
}

func mustChannels(t *testing.T, g *guildFake) []discord.GuildChannel {
	t.Helper()
	ch, err := g.GetGuildChannels(guild)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// drift takes #rules' overwrites away, as an admin might.
func drift(g *guildFake) {
	for i, c := range g.chans {
		if bare(c.name) == "rules" {
			g.chans[i].ows = nil
		}
	}
}

func say(m discord.MessageCreate) string { return coretest.Text("", m.Components) }

func TestDriftProposed(t *testing.T) {
	m, g, reports, _ := provisioned(t, Propose)
	ctx := context.Background()

	m.check(ctx, g, guild, self)
	if len(g.posts(reports)) != 0 {
		t.Fatal("a server that matches got a report")
	}

	drift(g)
	m.check(ctx, g, guild, self)
	posts := g.posts(reports)
	if len(posts) != 1 {
		t.Fatalf("%d reports", len(posts))
	}
	text := say(posts[0])
	if !strings.Contains(text, "! this server drifted from **flock**; here's what would put it back") || !strings.Contains(text, "! perms #rules: @everyone") {
		t.Fatal(text)
	}
	if posts[0].Flags&discord.MessageFlagEphemeral != 0 || len(buttons(posts[0].Components[0].(discord.ContainerComponent))) != 2 {
		t.Fatal("a report goes in the channel, with apply and check again")
	}

	// The same drift is reported once, and again once it changes.
	m.check(ctx, g, guild, self)
	if len(g.posts(reports)) != 1 {
		t.Fatal("the same drift was reported twice")
	}
	for i, c := range g.chans {
		if bare(c.name) == "media" {
			g.chans[i].ows = []discord.RolePermissionOverwrite{}
		}
	}
	g.roles[len(g.roles)-1].Color = 1
	m.check(ctx, g, guild, self)
	if len(g.posts(reports)) != 2 {
		t.Fatal("a new drift went unreported")
	}
}

func TestDriftWatched(t *testing.T) {
	m, g, reports, _ := provisioned(t, Watch)
	drift(g)
	m.check(context.Background(), g, guild, self)
	posts := g.posts(reports)
	if len(posts) != 1 || len(buttons(posts[0].Components[0].(discord.ContainerComponent))) != 0 {
		t.Fatal("watch reports without buttons")
	}
}

func TestDriftActedOn(t *testing.T) {
	m, g, reports, _ := provisioned(t, Act)
	drift(g)
	m.check(context.Background(), g, guild, self)
	posts := g.posts(reports)
	if len(posts) != 1 {
		t.Fatalf("%d reports", len(posts))
	}
	card := posts[0].Components[0].(discord.ContainerComponent)
	if text := say(posts[0]); !strings.Contains(text, "✓ this server drifted from **flock**, so cygnet put back 2 changes") || card.AccentColor != brand.ColorOK {
		t.Fatal(text)
	}
	if again := planOn(t, g, map[string]snowflake.ID{"skua": skuaApp}); len(again.changes) != 0 {
		t.Fatalf("left:\n%s", lines(again.changes))
	}

	// One that Discord refuses is reported as stopped.
	drift(g)
	g.fail["overwrite"] = errors.New("500")
	m.check(context.Background(), g, guild, self)
	if text := say(g.posts(reports)[1]); !strings.Contains(text, "✗ stopped after 0 of 2") {
		t.Fatal(text)
	}

	// An apply already running leaves the check to it.
	g.fail = map[string]error{}
	m.posted.Delete(guild)
	m.applying.Store(guild, true)
	m.check(context.Background(), g, guild, self)
	if len(g.posts(reports)) != 2 {
		t.Fatal("a check reported over a running apply")
	}
}

func TestDriftQuietCases(t *testing.T) {
	ctx := context.Background()
	m, g, reports, _ := provisioned(t, Propose)
	drift(g)

	// Not provisioned, or a layout this build doesn't have.
	m.check(ctx, g, 99, self)
	m.kept.Store(guild, Kept{Layout: "gone", Mode: Propose})
	m.check(ctx, g, guild, self)
	m.kept.Store(guild, Kept{Layout: "flock", Mode: Propose})

	// Discord won't say what the server looks like.
	g.fail["read"] = errors.New("500")
	m.check(ctx, g, guild, self)
	delete(g.fail, "read")

	// Over the hourly cap, or Discord turns the post down.
	for m.guard.Allow(guild, guard.MessageSend) == nil {
	}
	m.check(ctx, g, guild, self)
	m.guard = guard.New()
	m.posted.Delete(guild)
	g.fail["send"] = errors.New("403")
	m.check(ctx, g, guild, self)
	if len(g.posts(reports)) != 0 {
		t.Fatal("reported anyway")
	}

	// The report channel is gone.
	delete(g.fail, "send")
	m.posted.Delete(guild)
	var kept []fakeChan
	for _, c := range g.chans {
		if bare(c.name) != "cygnet" {
			kept = append(kept, c)
		}
	}
	g.chans = kept
	m.check(ctx, g, guild, self)
	if _, ok := m.posted.Load(guild); ok {
		t.Fatal("a report with nowhere to go counted as made")
	}
}

func TestDriftFromEvents(t *testing.T) {
	m, g, reports, _ := provisioned(t, Propose)
	drift(g)
	client := &bot.Client{Rest: g, ApplicationID: self}
	ge := events.NewGenericEvent(client, 0, 0)
	role := &events.GenericRole{GenericEvent: ge, GuildID: guild}
	ch := &events.GenericGuildChannel{GenericEvent: ge, GuildID: guild}
	for _, ev := range []bot.Event{
		&events.RoleUpdate{GenericRole: role}, &events.RoleDelete{GenericRole: role},
		&events.GuildChannelUpdate{GenericGuildChannel: ch}, &events.GuildChannelDelete{GenericGuildChannel: ch},
		&events.Ready{GenericEvent: ge},
		&events.RoleUpdate{GenericRole: &events.GenericRole{GenericEvent: ge, GuildID: 99}},
	} {
		m.OnEvent(ev)
	}
	for deadline := time.Now().Add(5 * time.Second); len(g.posts(reports)) == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the run of events never led to a report")
		}
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(g.posts(reports)); n != 1 {
		t.Fatalf("four events made %d reports", n)
	}
}

// Applying a layout keeps the server to it, in propose unless it had a mode.
func TestApplyKeeps(t *testing.T) {
	g := newGuild()
	p := planOn(t, g, nil)
	m := module(nil)
	if _, err := pressOn(t, m, g, applyID+":flock:"+digest(p.changes), "5"); err != nil {
		t.Fatal(err)
	}
	if v, ok := m.kept.Load(guild); !ok || v.(Kept) != (Kept{Layout: "flock", Mode: Propose}) {
		t.Fatalf("kept %v", v)
	}
	m.kept.Store(guild, Kept{Layout: "flock", Mode: Act})
	m.keep(context.Background(), guild, "flock")
	if v, _ := m.kept.Load(guild); v.(Kept).Mode != Act {
		t.Fatal("a reapply reset the mode")
	}
}
