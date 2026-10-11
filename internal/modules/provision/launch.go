package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
)

// Edge is the Worker at cygnet.lol as cygnet registers launches with it:
// its address and the bearer its admin endpoints take. cygnet reads a
// server's name and picture with its own token and sends them, so the
// Worker never needs one (SPEC.md, Principles 6).
type Edge struct {
	URL, Token string
	Client     *http.Client
}

// Launch gives the module the Worker to register launches with; without
// it /provision launch says it isn't connected.
func (m *Module) Launch(e *Edge) *Module {
	m.edge = e
	return m
}

var slug = regexp.MustCompile(`^[a-z0-9-]{2,32}$`)

// launchCmd registers this server as a launch called name, so it has a
// page at cygnet.lol/<name>, and keeps it fresh as the server changes.
func (m *Module) launchCmd(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	if m.edge == nil {
		return core.Tell("cygnet isn't connected to cygnet.lol yet; set CYGNET_EDGE_URL and CYGNET_EDGE_TOKEN")
	}
	name := strings.ToLower(e.SlashCommandInteractionData().String("name"))
	if !slug.MatchString(name) {
		return core.Tell("a launch name is 2 to 32 lowercase letters, digits or dashes")
	}
	g, ok := e.Client().Caches.Guild(*e.GuildID())
	if !ok {
		return core.Tell("cygnet can't see this server yet; try again in a moment")
	}
	if err := m.edge.push(ctx, name, g); err != nil {
		return fmt.Errorf("%w: %w", core.Tell(refusalEdge(err)), err)
	}
	m.launches.Store(g.ID, name)
	page := strings.TrimRight(m.edge.URL, "/") + "/" + name
	card := brand.Card(brand.ColorOK, "provision", "launch", "✓ **"+g.Name+"** has its page at "+page+"\n-# its name and picture follow the server when they change")
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// refresh sends a launch's server again after it changed, if it's one.
//
// ponytail: which servers are launches lives in memory, so after a restart
// a server's change reaches the page once /provision launch runs again; a
// table if launches change often.
func (m *Module) refresh(g discord.Guild) {
	v, ok := m.launches.Load(g.ID)
	if !ok || m.edge == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.edge.push(ctx, v.(string), g); err != nil {
		m.warn("refreshing a launch", g.ID, err)
	}
}

func (e *Edge) push(ctx context.Context, name string, g discord.Guild) error {
	body, _ := json.Marshal(map[string]string{"guild": g.ID.String(), "title": g.Name, "icon": icon(g.ID, g.Icon)})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(e.URL, "/")+"/admin/"+name, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "Bearer "+e.Token)
	req.Header.Set("content-type", "application/json")
	c := e.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return edgeStatus(res.StatusCode)
	}
	return nil
}

// icon is a server's picture on Discord's CDN, or "" if it has none.
func icon(guild snowflake.ID, hash *string) string {
	if hash == nil || *hash == "" {
		return ""
	}
	return fmt.Sprintf("https://cdn.discordapp.com/icons/%s/%s.webp?size=256", guild, *hash)
}

type edgeStatus int

func (s edgeStatus) Error() string { return fmt.Sprintf("cygnet.lol answered %d", int(s)) }

// refusalEdge is why the Worker didn't take a launch, in an admin's words.
func refusalEdge(err error) string {
	switch err {
	case edgeStatus(http.StatusConflict):
		return "that name is already another server's launch; pick another"
	case edgeStatus(http.StatusNotFound):
		return "cygnet.lol didn't take cygnet's token; check CYGNET_EDGE_TOKEN matches the Worker's ADMIN_TOKEN"
	}
	return "cygnet.lol didn't answer; try again in a moment"
}
