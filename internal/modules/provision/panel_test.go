package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
)

func TestPanel(t *testing.T) {
	// /provision opens the panel at once, before any layout is applied:
	// the way to the layout, the fleet and a fresh start, and no mode.
	m := module(nil)
	e, sent := coretest.Event(t, "provision", nil)
	if err := m.open(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if (*sent)[0].Flags&discord.MessageFlagEphemeral == 0 {
		t.Fatal("the panel is for the admin alone")
	}
	text := coretest.Text("", (*sent)[0].Components)
	for _, want := range []string{"**provision**", "**layout** none yet", "once a layout is applied"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in\n%s", want, text)
		}
	}
	ids := customIDs((*sent)[0].Components)
	for _, want := range []string{previewID + ":flock", refresh, previewID + ":flock:fresh"} {
		if !strings.Contains(ids, want+" ") {
			t.Fatalf("no %s in %s", want, ids)
		}
	}
	if strings.Contains(ids, modeID) {
		t.Fatal("a mode for a server cygnet doesn't keep")
	}
	if _, err := pick(t, m, "5", Act); !strings.Contains(err.Error(), "apply a layout here first") {
		t.Fatalf("an unprovisioned server: %v", err)
	}

	// Kept, it says how, and the select sets how far she goes.
	m, _, _, saved := provisioned(t, Propose)
	got, err := pick(t, m, "5", Act)
	if err != nil {
		t.Fatal(err)
	}
	text = coretest.Text("", *got.(discord.MessageUpdate).Components)
	for _, want := range []string{"✓ saved", "**layout** flock", "**drift** act", "puts it back and says what she did in #cygnet"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in\n%s", want, text)
		}
	}
	if len(*saved) != 1 || (*saved)[0].Mode != Act {
		t.Fatalf("saved %v", *saved)
	}
	if _, err := pick(t, m, "5", "everything"); err == nil {
		t.Fatal("a mode that isn't one")
	}
	if _, err := pick(t, m, "6", Watch); err == nil {
		t.Fatal("a member who isn't an admin set the mode")
	}

	// A save that fails keeps the mode in memory.
	m.save = func(context.Context, snowflake.ID, Kept) error { return errors.New("db down") }
	if _, err := pick(t, m, "5", Watch); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.kept.Load(guild); v.(Kept).Mode != Watch {
		t.Fatal("the mode didn't hold")
	}
}

// pick is user choosing mode on the panel's select.
func pick(t *testing.T, m *Module, user, mode string) (discord.InteractionResponseData, error) {
	e, _ := coretest.Button(t, modeID, func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
		p["data"] = map[string]any{"custom_id": modeID, "component_type": 3, "values": []any{mode}}
	})
	var got discord.InteractionResponseData
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = d
		return nil
	}
	return got, m.home(context.Background(), e)
}

func TestBack(t *testing.T) {
	// Back on the admin's own card turns it into the panel in place.
	got, err := press(t, newGuild(), homeID, "5")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)[0].(discord.MessageUpdate); !ok {
		t.Fatalf("not in place: %T", (*got)[0])
	}
	// On a card in a channel, such as a drift report, it leaves the card
	// and gives whoever pressed it their own panel.
	e, _ := coretest.Button(t, homeID, func(p map[string]any) { p["message"].(map[string]any)["flags"] = 0 })
	var reply discord.InteractionResponseData
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		reply = d
		return nil
	}
	if err := module(nil).home(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if c, ok := reply.(discord.MessageCreate); !ok || c.Flags&discord.MessageFlagEphemeral == 0 {
		t.Fatalf("not a copy of its own: %#v", reply)
	}
	if _, err := press(t, newGuild(), homeID, "6"); err == nil {
		t.Fatal("a member who isn't an admin")
	}

	// Every card on the way has the way back: beside its last buttons
	// when there's room, on a row of its own when not.
	g := newGuild()
	p := planOn(t, g, nil)
	for name, card := range map[string]discord.ContainerComponent{
		"preview": p.preview(""),
		"result":  p.result(1, 2, nil),
		"failed":  p.result(1, 2, errors.New("x")),
		"fleet":   render(guild, []row{{Name: "cygnet"}}, true),
	} {
		if !strings.Contains(customIDs([]discord.LayoutComponent{card}), homeID+" ") {
			t.Errorf("%s has no way back", name)
		}
	}
	full := discord.NewContainer(discord.NewActionRow(
		discord.NewSecondaryButton("1", "a"), discord.NewSecondaryButton("2", "b"), discord.NewSecondaryButton("3", "c"),
		discord.NewSecondaryButton("4", "d"), discord.NewSecondaryButton("5", "e")))
	if n := len(withBack(full).Components); n != 2 {
		t.Fatalf("a full row took the button: %d rows", n)
	}
	menu := discord.NewContainer(discord.NewActionRow(discord.NewStringSelectMenu("x", "y")))
	if n := len(withBack(menu).Components); n != 2 {
		t.Fatalf("a select's row took the button: %d rows", n)
	}
}

// customIDs is every button's and select's custom ID, each followed by a
// space.
func customIDs(cs []discord.LayoutComponent) string {
	var b strings.Builder
	for _, c := range cs {
		box, ok := c.(discord.ContainerComponent)
		if !ok {
			continue
		}
		for _, sub := range box.Components {
			row, ok := sub.(discord.ActionRowComponent)
			if !ok {
				continue
			}
			for _, i := range row.Components {
				switch v := i.(type) {
				case discord.ButtonComponent:
					b.WriteString(v.CustomID + " ")
				case discord.StringSelectMenuComponent:
					b.WriteString(v.CustomID + " ")
				}
			}
		}
	}
	return b.String()
}
