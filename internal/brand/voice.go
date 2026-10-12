package brand

// Status is cygnet's line in the member list, one per re-probe tick, round
// robin so the same line never shows twice running. These are her flavour,
// where she may sound like cygnet.lol does: light, water, feathers, a sky
// that isn't hers (UX.md, Voice). voice_test holds every line to the rules.
var Status = []string{
	"counting stars over the reeds",
	"where the light bends",
	"keeping watch on still water",
	"reading the tide",
	"feathers drying in the dusk",
	"out past the coral with peregrine",
	"merlin brought the fire",
	"under a borrowed sky",
	"writing on the water",
	"somewhere the light is late",
	"listening to the brine",
	"back before the shadows move",
}

// StatusAt is the line for tick n.
func StatusAt(n int) string { return Status[n%len(Status)] }
