package link

// Manifest is what a bird answers to "<bot>.manifest": what it asks Discord
// for and what it offers, so cygnet provisions servers from what is running
// rather than from a copy that drifts.
type Manifest struct {
	Build string `json:"build,omitempty"`
	// Perms is the union of the channel permissions its running modules use,
	// as Discord's permission bitfield in decimal, the way Discord writes it.
	Perms string `json:"perms"`
	// Intents is the gateway intents it identifies with.
	Intents  uint64    `json:"intents"`
	Commands []Command `json:"commands"`
}

// Command is one top-level command and who may run it.
type Command struct {
	Name string `json:"name"`
	Tier Tier   `json:"tier"`
}

// Tier is who may run a command, in the birds' shared terms.
type Tier string

const (
	// Public is anyone in the server.
	Public Tier = "public"
	// Admin is the server's admins (Administrator, or the owner).
	Admin Tier = "admin"
	// BreakGlass is the bot's bootstrap admin alone.
	BreakGlass Tier = "breakglass"
)

// ManifestMethod is the verb every bird serves its Manifest on.
const ManifestMethod = "manifest"
