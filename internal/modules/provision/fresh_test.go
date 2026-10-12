package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/guard"
)

// Every bot's role takes the layout's bot colour, linked bird or not, but
// cygnet's own at her top, which she can't reach; the colour is all that
// changes.
func TestColors(t *testing.T) {
	g := newGuild()
	other := snowflake.ID(99)
	g.roles[2].Color = 0
	g.roles = append(g.roles, discord.Role{ID: 50, Name: "dyno", Managed: true, Tags: &discord.RoleTag{BotID: &other}, Position: 3, Permissions: discord.PermissionKickMembers})
	p := planOn(t, g, nil)
	got := lines(p.changes)
	if !strings.Contains(got, "! role  skua: colour") || !strings.Contains(got, "! role  dyno: colour") || strings.Contains(got, "cygnet: colour") {
		t.Fatalf("plan:\n%s", got)
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	if r := g.roles[3]; r.Color != flock.BotColor || r.Permissions != discord.PermissionKickMembers {
		t.Fatalf("dyno %+v", r)
	}
	if again := lines(planOn(t, g, nil).changes); strings.Contains(again, "colour") {
		t.Fatalf("left:\n%s", again)
	}
}

// old is a server laid out before cygnet: a category of its own, a stray
// channel, a role she can delete and one she won't, and the rules channel
// a community server keeps.
func old() *guildFake {
	g := newGuild()
	g.chans = []fakeChan{
		{id: 60, name: "old", typ: discord.ChannelTypeGuildCategory},
		{id: 61, parent: 60, name: "chat", typ: discord.ChannelTypeGuildText},
		{id: 62, parent: 60, name: "vc", typ: discord.ChannelTypeGuildVoice},
		{id: 63, name: "random", typ: discord.ChannelTypeGuildText},
		{id: 64, name: "rules", typ: discord.ChannelTypeGuildText},
	}
	g.rules = 64
	g.roles = append(g.roles,
		discord.Role{ID: 70, Name: "veteran", Position: 2},
		discord.Role{ID: 71, Name: "boss", Position: 3, Permissions: discord.PermissionAdministrator},
	)
	return g
}

func TestFreshReplace(t *testing.T) {
	g := old()
	p := planWith(t, g, nil, true)
	got := lines(p.changes)
	if !strings.HasSuffix(got, "- text  #chat\n- voice vc\n- text  #random\n- text  #rules\n- group old\n- role  veteran\n- role  boss") {
		t.Fatalf("plan:\n%s", got)
	}
	text := coretest.Text("", []discord.LayoutComponent{p.preview("")})
	for _, want := range []string{
		"! can't remove **rules**: discord keeps it while this is a community server",
		"! can't remove **boss**: it holds administrator",
		"what's marked - goes",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in\n%s", want, text)
		}
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	for _, c := range g.chans {
		if c.id >= 60 && c.id <= 63 {
			t.Fatalf("%s is still here", c.name)
		}
	}
	// What's left is what she won't remove, and nothing else.
	if chans, cats, roles := planWith(t, g, nil, true).removals(); chans+cats+roles != 0 {
		t.Fatalf("left %d %d %d", chans, cats, roles)
	}
}

func TestFreshArchive(t *testing.T) {
	g := old()
	p := planWith(t, g, nil, true)
	changes := archivedTo(p.changes)
	got := lines(changes)
	if !strings.Contains(got, "! move  3 channels into ┄ archive\n- text  #rules\n- group old\n- role  veteran") {
		t.Fatalf("plan:\n%s", got)
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	var attic fakeChan
	for _, c := range g.chans {
		if c.name == archiveName {
			attic = c
		}
	}
	if len(attic.ows) != 1 || attic.ows[0].RoleID != guild || attic.ows[0].Deny != see {
		t.Fatalf("archive %+v", attic)
	}
	for _, c := range g.chans {
		if (c.id == 61 || c.id == 62 || c.id == 63) && (c.parent != attic.id || len(c.ows) != 1) {
			t.Fatalf("%s wasn't archived: %+v", c.name, c)
		}
	}
	// The archive and what's in it stay on the next fresh start, and a
	// second archive goes into the same one.
	if chans, cats, roles := planWith(t, g, nil, true).removals(); chans+cats+roles != 0 {
		t.Fatalf("left %d %d %d", chans, cats, roles)
	}
	g.chans = append(g.chans, fakeChan{id: 80, name: "late", typ: discord.ChannelTypeGuildText})
	p = planWith(t, g, nil, true)
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, archivedTo(p.changes), func(int) {}); err != nil {
		t.Fatal(err)
	}
	if g.chans[len(g.chans)-1].parent != attic.id {
		t.Fatal("a second archive made a second category")
	}
	if archivedTo(nil) != nil {
		t.Fatal("nothing to archive")
	}

	g = old()
	g.fail["channel"] = errors.New("500")
	p = planWith(t, g, nil, true)
	if err := moveToArchive(guild, g, nil, []snowflake.ID{61}, map[string]snowflake.ID{}); err == nil {
		t.Fatal("a refused archive should fail")
	}
}

// The buttons: a choice, then a confirmation naming what goes, then the
// apply; and the result where staff look if the card's channel is gone.
func TestFreshButtons(t *testing.T) {
	g := old()
	p := planWith(t, g, nil, true)
	var ids []string
	for _, b := range buttons(p.preview("")) {
		ids = append(ids, b.Label+"="+b.CustomID)
	}
	sum := digest(p.changes)
	want := "archive old=" + confirmID + ":flock:" + sum + ":archive, replace=" + confirmID + ":flock:" + sum + ":replace, check again=" + previewID + ":flock:fresh, ◂ back=" + homeID
	if strings.Join(ids, ", ") != want {
		t.Fatalf("buttons %v", ids)
	}

	got, err := press(t, g, confirmID+":flock:"+sum+":replace", "5")
	if err != nil {
		t.Fatal(err)
	}
	text := coretest.Text("", *(*got)[0].(discord.MessageUpdate).Components)
	if !strings.Contains(text, "! this can't be undone\n▸ cygnet deletes 3 channels with every message in them, 1 category, 1 role\n-# and makes the other 14 changes") || len(g.writes) != 0 {
		t.Fatal(text)
	}
	got, _ = press(t, g, confirmID+":flock:"+sum+":archive", "5")
	text = coretest.Text("", *(*got)[0].(discord.MessageUpdate).Components)
	if !strings.Contains(text, "▸ cygnet moves 3 channels into ┄ archive, hidden from everyone but admins\n▸ cygnet deletes 1 category, 1 role") {
		t.Fatal(text)
	}
	for _, id := range []string{confirmID + ":flock:stale:replace", confirmID + ":flock:" + sum + ":burn"} {
		got, _ = press(t, g, id, "5")
		if text := coretest.Text("", *(*got)[0].(discord.MessageUpdate).Components); !strings.Contains(text, "the server changed") {
			t.Fatalf("%s: %s", id, text)
		}
	}

	// The card's channel went with the old layout: the result is posted in
	// the nest instead.
	g.fail["edit"] = errors.New("unknown channel")
	if _, err := press(t, g, applyID+":flock:"+sum+":archive", "5"); err != nil {
		t.Fatal(err)
	}
	nest := snowflake.ID(0)
	for _, c := range g.chans {
		if bare(c.name) == "cygnet" {
			nest = c.id
		}
	}
	if posts := g.posts(nest); len(posts) != 1 || !strings.Contains(coretest.Text("", posts[0].Components), "✓ applied") {
		t.Fatalf("posts %v", posts)
	}

	// Nothing left to remove: a plain apply, still fresh.
	p = planWith(t, g, nil, true)
	if b := buttons(p.preview("")); b[0].Label != "apply 0 changes" || !strings.HasSuffix(b[0].CustomID, ":replace") {
		t.Fatalf("buttons %+v", b)
	}
	got, _ = press(t, g, previewID+":flock:fresh", "5")
	if len(*got) != 1 {
		t.Fatal("check again")
	}

	g.fail["guild"] = errors.New("500")
	if _, err := press(t, g, previewID+":flock:fresh", "5"); err == nil {
		t.Fatal("a failed guild read should fail")
	}
}

func TestReportFallback(t *testing.T) {
	m := module(nil)
	g := newGuild()
	card := planned{l: flock}.progress(1, 2)
	m.report(context.Background(), g, guild, flock, card) // no nest yet
	g.fail["channels"] = errors.New("500")
	m.report(context.Background(), g, guild, flock, card)
	g.fail = map[string]error{"send": errors.New("500")}
	g.chans = []fakeChan{{id: 90, name: "🦢│cygnet", typ: discord.ChannelTypeGuildText}}
	m.report(context.Background(), g, guild, flock, card)
	if len(g.posts(90)) != 0 {
		t.Fatal("posted")
	}
}
