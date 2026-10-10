package brand

import (
	"testing"

	"github.com/disgoorg/disgo/discord"
)

// A card's head is the module's emoji once synced, text alone before, and
// it carries what it was given after its text.
func TestCard(t *testing.T) {
	unsynced(t)
	row := discord.NewActionRow(discord.NewSecondaryButton("x", "x"))
	c := Card(ColorOK, "status", "stop", "✓ done", row)
	if c.AccentColor != ColorOK || len(c.Components) != 2 {
		t.Fatalf("card %+v", c)
	}
	if got := c.Components[0].(discord.TextDisplayComponent).Content; got != "**status** · stop\n✓ done" {
		t.Errorf("unsynced head %q", got)
	}
	ids := map[string]emojiRef{"mod_status": {7, "mod_status_ab"}}
	emojiIDs.Store(&ids)
	if got := Card(ColorOK, "status", "", "body").Components[0].(discord.TextDisplayComponent).Content; got != "<:mod_status_ab:7> **status**\nbody" {
		t.Errorf("synced head %q", got)
	}
}

func TestArt(t *testing.T) {
	if len(Art("cygnet_avatar")) == 0 || Art("nobody") != nil {
		t.Fatal("art")
	}
}
