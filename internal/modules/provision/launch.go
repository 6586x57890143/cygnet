package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Edge is the Worker at cygnet.lol as cygnet runs launches on it: its
// address and the bearer its admin endpoints take. cygnet reads a server's
// name and picture with its own token and sends them, so the Worker never
// needs one (SPEC.md, Principles 6).
type Edge struct {
	URL, Token string
	Client     *http.Client
	// Breakglass is cygnet's breakglass admin, who may see every launch's
	// list beside the server's owner.
	Breakglass snowflake.ID
}

// Launch gives the module the Worker to run launches on; without it the
// launch commands say it isn't connected.
func (m *Module) Launch(e *Edge) *Module {
	m.edge = e
	return m
}

const campaignID = "provision-campaign"

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
	card := brand.Card(brand.ColorOK, "provision", "launch", "✓ **"+g.Name+"** has its page at "+m.edge.page(name)+"\n-# its name and picture follow the server when they change; /provision campaign runs it")
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// refresh sends a launch's server again after it changed, if it's one: the
// Worker says which launch a server has, so this holds across restarts.
func (m *Module) refresh(g discord.Guild) {
	if m.edge == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, err := m.edge.launchOf(ctx, g.ID)
	if err == nil && name != "" {
		err = m.edge.push(ctx, name, g)
	}
	if err != nil {
		m.warn("refreshing a launch", g.ID, err)
	}
}

// stats is a launch's campaign as the Worker counts it.
type stats struct {
	Launch         string `json:"launch"`
	Stage          string `json:"stage"`
	Opened         string `json:"opened"`
	Total          int    `json:"total"`
	InLine         int    `json:"in_line"`
	ThroughInvites int    `json:"through_invites"`
	Held           int    `json:"held"`
	ReleasedTo     int    `json:"released_to"`
	Waiting        int    `json:"waiting"`
	InvitesMade    int    `json:"invites_made"`
	InvitesUsed    int    `json:"invites_used"`
	InvitesOpen    int    `json:"invites_open"`
}

// campaignCmd shows this server's launch campaign, with what runs it.
func (m *Module) campaignCmd(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	card, err := m.campaignCard(ctx, *e.GuildID(), "")
	if err != nil {
		return err
	}
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// campaignPressed runs a button on the campaign card: a stage, a release,
// or a fresh look, then redraws it.
func (m *Module) campaignPressed(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if e.GuildID() == nil || !m.admin(e) {
		return core.Tell("only this server's admins can use /provision")
	}
	if m.edge == nil {
		return core.Tell("cygnet isn't connected to cygnet.lol yet; set CYGNET_EDGE_URL and CYGNET_EDGE_TOKEN")
	}
	name, err := m.edge.launchOf(ctx, *e.GuildID())
	if err != nil || name == "" {
		return m.edgeTell(err, "this server has no launch yet; /provision launch makes one")
	}
	note := ""
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) == 3 {
		switch parts[1] {
		case "stage":
			err = m.edge.do(ctx, http.MethodPost, "/admin/"+name+"/stage", map[string]any{"stage": parts[2]}, nil)
			note = "✓ the campaign is " + parts[2] + " now"
		case "release":
			var n int
			if _, err = fmt.Sscan(parts[2], &n); err == nil {
				var out struct {
					Released int `json:"released"`
				}
				err = m.edge.do(ctx, http.MethodPost, "/admin/"+name+"/release", map[string]any{"count": n, "invites": invitesEach}, &out)
				note = fmt.Sprintf("✓ released %d invites each to %s", invitesEach, people(out.Released))
			}
		}
		if err != nil {
			return m.edgeTell(err, "")
		}
	}
	card, err := m.campaignCard(ctx, *e.GuildID(), note)
	if err != nil {
		return err
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

// invitesEach is how many invites a release gives each person.
const invitesEach = 3

// campaignCard is the campaign in numbers, the stage buttons with the
// current one off, and the releases.
func (m *Module) campaignCard(ctx context.Context, guild snowflake.ID, note string) (discord.ContainerComponent, error) {
	if m.edge == nil {
		return discord.ContainerComponent{}, core.Tell("cygnet isn't connected to cygnet.lol yet; set CYGNET_EDGE_URL and CYGNET_EDGE_TOKEN")
	}
	name, err := m.edge.launchOf(ctx, guild)
	if err != nil || name == "" {
		return discord.ContainerComponent{}, m.edgeTell(err, "this server has no launch yet; /provision launch makes one")
	}
	var s stats
	if err := m.edge.do(ctx, http.MethodGet, "/admin/"+name, nil, &s); err != nil {
		return discord.ContainerComponent{}, m.edgeTell(err, "")
	}
	var b strings.Builder
	if note != "" {
		b.WriteString(note + "\n")
	}
	b.WriteString("-# " + m.edge.page(name) + "\n")
	fmt.Fprintf(&b, "```\nstage     %s\non list   %d · %d held\nin line   %d · %d waiting\nreleased  %s\ninvites   %d made · %d used · %d open\n```",
		s.Stage, s.Total, s.Held, s.InLine, s.Waiting, people(s.ReleasedTo), s.InvitesMade, s.InvitesUsed, s.InvitesOpen)
	b.WriteString("\n-# gathering lets people get on the list; closed stops new sign ups; open means the server is open, invites still bring friends in, and the clock on deleting the list starts")
	if s.Opened != "" {
		b.WriteString("\n-# opened " + s.Opened[:10])
	}
	stage := func(st string) discord.InteractiveComponent {
		btn := discord.NewSecondaryButton(st, campaignID+":stage:"+st)
		if st == s.Stage {
			btn = btn.AsDisabled()
		}
		return btn
	}
	release := func(n int) discord.InteractiveComponent {
		btn := discord.NewPrimaryButton(fmt.Sprintf("release to next %d", n), fmt.Sprintf("%s:release:%d", campaignID, n))
		if s.Waiting == 0 {
			btn = btn.AsDisabled()
		}
		return btn
	}
	return brand.Card(brand.ColorInfo, "provision", "campaign", b.String(),
		discord.NewSeparator(discord.SeparatorSpacingSizeSmall),
		discord.NewActionRow(stage("gathering"), stage("closed"), stage("open")),
		discord.NewActionRow(release(10), release(50), discord.NewSecondaryButton("check again", campaignID)),
		// The list itself is on the web, behind a Discord sign in, for the
		// server's owner and the breakglass admin only: never in a channel.
		discord.NewActionRow(discord.NewLinkButton("who's on the list", m.edge.page(name)+"/list")),
	), nil
}

// edgeTell is why the Worker didn't answer as hoped, in an admin's words.
func (m *Module) edgeTell(err error, missing string) error {
	if err == nil {
		return core.Tell(missing)
	}
	return fmt.Errorf("%w: %w", core.Tell(refusalEdge(err)), err)
}

func (e *Edge) page(name string) string { return strings.TrimRight(e.URL, "/") + "/" + name }

// push sends a launch's server as cygnet sees it, with who may see its
// list: the server's owner, and the breakglass admin.
func (e *Edge) push(ctx context.Context, name string, g discord.Guild) error {
	admins := []string{}
	for _, id := range []snowflake.ID{g.OwnerID, e.Breakglass} {
		if id != 0 {
			admins = append(admins, id.String())
		}
	}
	return e.do(ctx, http.MethodPut, "/admin/"+name, map[string]any{"guild": g.ID.String(), "title": g.Name, "icon": icon(g.ID, g.Icon), "admins": admins}, nil)
}

// launchOf is the launch a server has on the Worker, or "".
func (e *Edge) launchOf(ctx context.Context, guild snowflake.ID) (string, error) {
	var out struct {
		Launch string `json:"launch"`
	}
	err := e.do(ctx, http.MethodGet, "/admin/guild/"+guild.String(), nil, &out)
	if errors.Is(err, edgeStatus(http.StatusNotFound)) {
		return "", nil
	}
	return out.Launch, err
}

// do calls one of the Worker's admin endpoints, sending body as JSON and
// reading the answer into out.
func (e *Edge) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(e.URL, "/")+path, r)
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
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return edgeStatus(res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
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

// refusalEdge is why the Worker didn't take a request, in an admin's words.
func refusalEdge(err error) string {
	switch {
	case errors.Is(err, edgeStatus(http.StatusConflict)):
		return "that name is already another server's launch; pick another"
	case errors.Is(err, edgeStatus(http.StatusNotFound)):
		return "cygnet.lol didn't take cygnet's token; check CYGNET_EDGE_TOKEN matches the Worker's ADMIN_TOKEN"
	}
	return "cygnet.lol didn't answer; try again in a moment"
}

// people is n people, or 1 person.
func people(n int) string {
	if n == 1 {
		return "1 person"
	}
	return fmt.Sprintf("%d people", n)
}
