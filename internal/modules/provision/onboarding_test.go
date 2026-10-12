package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/guard"
)

func TestOnboarding(t *testing.T) {
	g := newGuild()
	g.community = true
	birds := map[string]snowflake.ID{"skua": skuaApp}

	// A community server gets flock's onboarding after its channels, all
	// of it, and nothing waits.
	p := planOn(t, g, birds)
	if last := p.changes[len(p.changes)-1].line(); last != "! onboarding: on, mode, channels, question" {
		t.Fatalf("last change %q", last)
	}
	if len(p.waiting) != 0 {
		t.Fatalf("waiting %v", p.waiting)
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	o := g.onboarding
	names := func(ids []snowflake.ID) string {
		var out []string
		for _, id := range ids {
			for _, c := range g.chans {
				if c.id == id {
					out = append(out, bare(c.name))
				}
			}
		}
		return strings.Join(out, " ")
	}
	if !o.Enabled || o.Mode != discord.GuildOnboardingModeAdvanced || names(o.DefaultChannelIDs) != "rules announcements general media lounge" {
		t.Fatalf("onboarding %+v: %s", o, names(o.DefaultChannelIDs))
	}
	q := o.Prompts[0]
	if len(o.Prompts) != 1 || q.Title != "what do you come here for" || q.SingleSelect || q.Required || !q.InOnboarding || len(q.Options) != 3 {
		t.Fatalf("prompts %+v", o.Prompts)
	}
	if names(q.Options[0].ChannelIDs) != "studio" || names(q.Options[1].ChannelIDs) != "play" || *q.Options[2].Emoji.Name != "💬" {
		t.Fatalf("answers %+v", q.Options)
	}
	if again := planOn(t, g, birds); len(again.changes) != 0 {
		t.Fatalf("left:\n%s", lines(again.changes))
	}

	// An admin's own question is kept, and a reapply keeps the IDs of the
	// layout's question and answers, so members' answers hold.
	ids := []snowflake.ID{q.ID, q.Options[0].ID}
	g.onboarding.Prompts = append(g.onboarding.Prompts, discord.GuildOnboardingPrompt{ID: 77, Title: "pronouns"})
	g.onboarding.Mode = discord.GuildOnboardingModeDefault
	p = planOn(t, g, birds)
	if got := lines(p.changes); got != "! onboarding: mode" {
		t.Fatalf("plan %s", got)
	}
	if _, err := apply(context.Background(), g, guard.New(), p.s, flock, p.changes, func(int) {}); err != nil {
		t.Fatal(err)
	}
	ps := g.onboarding.Prompts
	if len(ps) != 2 || ps[0].ID != 77 || ps[1].ID != ids[0] || ps[1].Options[0].ID != ids[1] {
		t.Fatalf("prompts %+v", ps)
	}

	// A question answered differently, or gone, is a change.
	g.onboarding.Prompts[1].Options[0].ChannelIDs = nil
	if got := lines(planOn(t, g, birds).changes); got != "! onboarding: question" {
		t.Fatalf("plan %s", got)
	}
	g.onboarding.Prompts = g.onboarding.Prompts[:1]
	if got := lines(planOn(t, g, birds).changes); got != "! onboarding: question" {
		t.Fatalf("plan %s", got)
	}

	// Reading or writing it can fail like anything else.
	g.fail["onboarding"] = errors.New("500")
	if _, err := module(nil).read(context.Background(), g, guild, self, flock, false); err == nil {
		t.Fatal("a failed read")
	}
	if err := onboard(guild, g, nil, flock); err == nil {
		t.Fatal("a failed read before the write")
	}
	g.fail = map[string]error{"onboard": errors.New("500")}
	if err := onboard(guild, g, nil, flock); err == nil {
		t.Fatal("a failed write")
	}
	g.fail = map[string]error{"channels": errors.New("500")}
	if err := onboard(guild, g, nil, flock); err == nil {
		t.Fatal("a failed channel read")
	}
}

// Only a layout with onboarding asks for Manage Server.
func TestOnboardingAsks(t *testing.T) {
	if !needs(flock).Has(discord.PermissionManageGuild) || needs(Layout{}).Has(discord.PermissionManageGuild) {
		t.Fatal("manage server")
	}
	if (state{}).asked(flock.Onboarding.Questions[0], []discord.GuildOnboardingPrompt{{Title: "what do you come here for", InOnboarding: true}}) {
		t.Fatal("a question with no answers matched")
	}
}
