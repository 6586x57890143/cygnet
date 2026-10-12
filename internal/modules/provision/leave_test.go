package provision

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
)

// choose is user picking values on the select customID, with every
// response recorded.
func choose(t *testing.T, customID, user string, values ...string) (*events.ComponentInteractionCreate, *[]discord.InteractionResponseData) {
	t.Helper()
	e, _ := coretest.Button(t, customID, func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
		vs := make([]any, len(values))
		for i, v := range values {
			vs[i] = v
		}
		p["data"] = map[string]any{"custom_id": customID, "component_type": 3, "values": vs}
	})
	var got []discord.InteractionResponseData
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = append(got, d)
		return nil
	}
	return e, &got
}

// options is the values of the leave select on a report.
func options(card discord.ContainerComponent) []string {
	var out []string
	for _, c := range card.Components {
		if row, ok := c.(discord.ActionRowComponent); ok {
			if menu, ok := row.Components[0].(discord.StringSelectMenuComponent); ok && menu.CustomID == leaveID {
				for _, o := range menu.Options {
					out = append(out, o.Value)
				}
			}
		}
	}
	return out
}

func TestLeave(t *testing.T) {
	m, g, reports, saved := provisioned(t, Act)
	ctx := context.Background()

	// In act, the report says what she put back, and offers each change
	// to leave alone from now on.
	drift(g)
	m.check(ctx, g, guild, self)
	card := g.posts(reports)[0].Components[0].(discord.ContainerComponent)
	opts := options(card)
	if len(opts) != 2 || !strings.Contains(say(g.posts(reports)[0]), "make the change again") {
		t.Fatalf("options %v\n%s", opts, say(g.posts(reports)[0]))
	}

	leave := func(user string, values ...string) ([]discord.InteractionResponseData, error) {
		e, got := choose(t, leaveID, user, values...)
		err := m.leavePressed(ctx, e)
		return *got, err
	}
	got, err := leave("5", opts[0])
	if err != nil {
		t.Fatal(err)
	}
	reply := got[0].(discord.MessageCreate)
	if reply.Flags&discord.MessageFlagEphemeral == 0 || !strings.Contains(say(reply), "leaves 1 change alone") {
		t.Fatal(say(reply))
	}
	if last := (*saved)[len(*saved)-1]; !slices.Equal(last.Leave, opts[:1]) {
		t.Fatalf("saved %v", last)
	}
	// Picking it again doesn't count it twice.
	if _, err := leave("5", opts[0]); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.kept.Load(guild); len(v.(Kept).Leave) != 1 {
		t.Fatalf("left %v", v.(Kept).Leave)
	}

	// The same drift again: only what wasn't left alone is put back.
	drift(g)
	m.check(ctx, g, guild, self)
	if text := say(g.posts(reports)[1]); !strings.Contains(text, "put back 1 change") || strings.Contains(text, opts[0]) {
		t.Fatal(text)
	}

	// The panel lists it, and forgets it.
	if text := coretest.Text("", []discord.LayoutComponent{m.panel(guild, "")}); !strings.Contains(text, "1 change left alone here") || !strings.Contains(text, opts[0]) {
		t.Fatal(text)
	}
	if !strings.Contains(customIDs([]discord.LayoutComponent{m.panel(guild, "")}), forgetID+" ") {
		t.Fatal("no forget button")
	}
	pr, err := pressOn(t, m, g, forgetID, "5")
	if err != nil || !strings.Contains(coretest.Text("", *(*pr)[0].(discord.MessageUpdate).Components), "keeps everything flock names again") {
		t.Fatalf("%v", err)
	}
	if v, _ := m.kept.Load(guild); v.(Kept).Leave != nil {
		t.Fatal("not forgotten")
	}

	// Refusals: not an admin, a value too long to be a line, too many.
	for _, c := range []struct {
		user   string
		values []string
		want   string
	}{
		{"6", opts[:1], "admins"},
		{"5", []string{strings.Repeat("x", 101)}, "older cygnet"},
	} {
		if _, err := leave(c.user, c.values...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.values, err)
		}
	}
	full := make([]string, most)
	for i := range full {
		full[i] = string(rune('a'+i%26)) + strings.Repeat("y", i/26)
	}
	m.kept.Store(guild, Kept{Layout: "flock", Mode: Act, Leave: full})
	if _, err := leave("5", "one more"); err == nil || !strings.Contains(err.Error(), "the most it can") {
		t.Errorf("over the cap: %v", err)
	}
	m.kept.Delete(guild)
	if _, err := leave("5", opts[0]); err == nil || !strings.Contains(err.Error(), "doesn't keep") {
		t.Errorf("unkept: %v", err)
	}
}

// A report with nothing to leave alone, such as a fresh start's removals,
// gets no select.
func TestLeavableNothing(t *testing.T) {
	p := planned{l: flock, changes: []change{{kind: remove, id: snowflake.ID(1), name: "old"}}}
	card := p.leavable(p.card("", false), "why")
	if len(options(card)) != 0 || strings.Contains(coretest.Text("", []discord.LayoutComponent{card}), "why") {
		t.Fatal("a removal was offered")
	}
}
