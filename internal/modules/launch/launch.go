// Package launch is /launch: a server's page at cygnet.lol where people get
// in line, and the campaign that lets them in. The command opens one panel,
// and everything is done on it: naming the page, the campaign's stage and
// releasing invites to the front of the line. cygnet keeps the line, its
// invites and their ledger (store.Launches); the Worker at cygnet.lol asks
// her for them over the hub (verbs.go) and keeps only what a sign up needs.
package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/intents"
	"github.com/6586x57890143/cygnet/internal/store"
)

// Edge is the Worker at cygnet.lol as cygnet sends it what its pages show:
// its address and the bearer its admin endpoints take. cygnet reads a
// server's name and picture with its own token and sends them, so the
// Worker never needs one (SPEC.md, Principles 6).
type Edge struct {
	URL, Token string
	Client     *http.Client
	// Breakglass is cygnet's breakglass admin, who may see every launch's
	// list beside the server's owner.
	Breakglass snowflake.ID
}

// The panel's custom IDs: "launch" redraws it, "launch:stage:<stage>" and
// "launch:release:<n>" run the campaign, "launch:name" opens the form and
// "launch-name" is the form.
const (
	id     = "launch"
	nameID = "launch-name"
)

var slug = regexp.MustCompile(`^[a-z0-9-]{2,32}$`)

type Module struct {
	// edge is nil when cygnet isn't connected to cygnet.lol.
	edge *Edge
	// store is nil without a database, or without a ledger key.
	store *store.Launches
	// guild is a server as cygnet's cache has it.
	guild func(snowflake.ID) (discord.Guild, bool)
	admin func(discord.Interaction) bool
	log   *slog.Logger
	now   func() time.Time
	// expiring starts the daily sweep once.
	expiring sync.Once
	// flock is the birds sent to cygnet.lol's footer; nil without the hub.
	flock *flock
	// opener opens sealed sign ins; nil without CYGNET_SEAL_KEY.
	opener *Opener
	// door lets a wave's people in (invite's Let); secret is the app's
	// OAuth secret and app its ID, to refresh a sealed sign in with.
	door   Door
	secret string
	app    snowflake.ID
	// joins counts who joined each server, for the valve; flowing starts
	// it once.
	joins   joins
	flowing sync.Once
}

func New(edge *Edge, s *store.Launches, guild func(snowflake.ID) (discord.Guild, bool), admin func(discord.Interaction) bool, log *slog.Logger) *Module {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Module{edge: edge, store: s, guild: guild, admin: admin, log: log, now: time.Now}
}

func (*Module) Name() string { return "launch" }

// Want is guilds, so a page follows its server's name and picture, and
// server members, so the valve sees joins a minute.
func (*Module) Want() intents.Want {
	return intents.Want{Required: gateway.IntentGuilds | gateway.IntentGuildMembers}
}

// Perms is none: everything a launch does happens at cygnet.lol.
func (*Module) Perms() discord.Permissions { return 0 }

func (*Module) Help() core.Help {
	return core.Help{
		Color: brand.ColorPrimary,
		Line:  "a page at cygnet.lol where people get in line",
		About: "/launch opens this server's launch. the first time, you name its page and it lives at cygnet.lol/<name> with the server's name and picture, which follow the server when they change. after that the panel is the campaign: how many are on the list and in line, and the invites given and taken. gathering lets people get on the list, closed stops new sign ups and open means the server is open. release gives the next people in line their invites. every change lands in a ledger cygnet checks each time the panel opens. who's on the list is on the web, behind a discord sign in, for the server's owner only",
	}
}

func (m *Module) Commands() []core.Command {
	return []core.Command{{
		Create: discord.SlashCommandCreate{
			Name:        "launch",
			Description: "this server's page at cygnet.lol and the campaign that lets people in",
			Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		},
		Tier: core.Admin,
		Run:  m.open,
	}}
}

func (m *Module) Components() []core.Component {
	return []core.Component{{ID: id, Run: m.click}}
}

func (m *Module) Modals() []core.Modal {
	return []core.Modal{{ID: nameID, Run: m.named}}
}

// OnEvent sends a launch's server again when its name or picture changes,
// and starts sending the flock and expiring old lists once cygnet is
// connected.
func (m *Module) OnEvent(ev bot.Event) {
	switch e := ev.(type) {
	case *events.Ready:
		m.app = e.Client().ApplicationID
		m.fly(e.Client().Rest, e.Client().ApplicationID)
		m.expiring.Do(func() { go m.expire() })
		m.flowing.Do(func() { go m.flow() })
	case *events.GuildMemberJoin:
		m.joins.add(e.GuildID, m.now())
	case *events.GuildUpdate:
		if e.Guild.Name != e.OldGuild.Name || fmt.Sprint(e.Guild.Icon) != fmt.Sprint(e.OldGuild.Icon) {
			go m.refresh(e.Guild)
		}
	}
}

var (
	errOffline = core.Tell("cygnet isn't connected to cygnet.lol yet; set CYGNET_EDGE_URL and CYGNET_EDGE_TOKEN")
	errNoStore = core.Tell("a launch keeps its list in cygnet's database; set CYGNET_DATABASE_URL and CYGNET_LEDGER_KEY")
)

// ready is why launches can't run here, or nil.
func (m *Module) ready() error {
	switch {
	case m.edge == nil:
		return errOffline
	case m.store == nil:
		return errNoStore
	}
	return nil
}

// keepList is how long a launch's list is kept after it first opens.
const keepList = 365 * 24 * time.Hour

// expire deletes each launch's list once keepList has passed since it
// opened, checking daily.
func (m *Module) expire() {
	if m.store == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		if err := m.store.Expire(ctx, m.now(), keepList); err != nil {
			m.log.Warn("expiring launch lists", "err", err)
		}
		cancel()
		time.Sleep(24 * time.Hour)
	}
}

func (m *Module) open(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	card, err := m.panel(ctx, *e.GuildID(), "")
	if err != nil {
		return err
	}
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// click runs a button on the panel: the form, a stage, a release, or a
// fresh look, then redraws it. The custom ID came from the member's
// client, so who they are is checked again here.
func (m *Module) click(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if e.GuildID() == nil || !m.admin(e) {
		return core.Tell("only this server's admins can run its launch")
	}
	if err := m.ready(); err != nil {
		return err
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) == 2 && parts[1] == "name" {
		return e.Modal(form())
	}
	l, err := m.store.Of(ctx, *e.GuildID())
	switch {
	case errors.Is(err, store.ErrNoLaunch):
		// A button from before the launch was named: the panel is drawn
		// again as it stands.
		parts = nil
	case err != nil:
		return err
	}
	note := ""
	switch {
	case len(parts) == 2 && parts[1] == "ceiling":
		v := e.StringSelectMenuInteractionData().Values
		var c int
		if len(v) != 1 || !slices.Contains(ceilings, atoi(v[0])) {
			return core.Tell("pick one of the ceilings the panel offers")
		}
		c = atoi(v[0])
		if err := m.store.Valve(ctx, l.Name, c, l.Paused, e.User().ID, m.now()); err != nil {
			return err
		}
		note = fmt.Sprintf("✓ waves keep under %d joins a minute", c)
	case len(parts) == 3 && parts[1] == "valve" && (parts[2] == "pause" || parts[2] == "run"):
		if err := m.store.Valve(ctx, l.Name, l.Ceiling, parts[2] == "pause", e.User().ID, m.now()); err != nil {
			return err
		}
		note = map[string]string{"pause": "✓ the valve is paused; nobody new is let in until it runs", "run": "✓ the valve runs"}[parts[2]]
	case len(parts) == 3 && parts[1] == "stage" && slices.Contains([]string{store.StageGathering, store.StageClosed, store.StageOpen}, parts[2]):
		if err := m.store.SetStage(ctx, l.Name, parts[2], e.User().ID, m.now()); err != nil {
			return err
		}
		l.Stage = parts[2]
		note = "✓ the campaign is " + parts[2] + " now" + m.sync(ctx, l, *e.GuildID())
	case len(parts) == 3 && parts[1] == "release":
		var n int
		if _, err := fmt.Sscan(parts[2], &n); err != nil || n < 1 || n > 100 {
			return core.Tell("that isn't a release the panel offers")
		}
		got, err := m.store.Release(ctx, l.Name, n, invitesEach, e.User().ID, m.now())
		if err != nil {
			return err
		}
		note = fmt.Sprintf("✓ released %d invites each to %s", invitesEach, people(got))
	case len(parts) == 1:
		// check again also sends the page what it shows, in case a send was
		// missed.
		note = strings.TrimPrefix(m.sync(ctx, l, *e.GuildID()), "\n")
	}
	card, err := m.panel(ctx, *e.GuildID(), note)
	if err != nil {
		return err
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

// form asks for the page's name.
func form() discord.ModalCreate {
	return discord.ModalCreate{
		CustomID: nameID,
		Title:    "name this server's page",
		Components: []discord.LayoutComponent{
			discord.NewLabel("cygnet.lol/", discord.TextInputComponent{
				CustomID: "name", Style: discord.TextInputStyleShort, Required: true, MaxLength: 32,
				Placeholder: "lowercase letters, digits and dashes",
			}),
		},
	}
}

// named registers this server as the launch the form names, so it has a
// page at cygnet.lol/<name>, and redraws the panel as its campaign.
func (m *Module) named(ctx context.Context, e *events.ModalSubmitInteractionCreate) error {
	if e.GuildID() == nil || !m.admin(e) {
		return core.Tell("only this server's admins can run its launch")
	}
	if err := m.ready(); err != nil {
		return err
	}
	name := strings.ToLower(strings.TrimSpace(e.Data.Text("name")))
	if !slug.MatchString(name) {
		return core.Tell("a launch name is 2 to 32 lowercase letters, digits or dashes")
	}
	g, ok := e.Client().Caches.Guild(*e.GuildID())
	if !ok {
		return core.Tell("cygnet can't see this server yet; try again in a moment")
	}
	switch err := m.store.Register(ctx, name, g.ID, m.now()); {
	case errors.Is(err, store.ErrTaken):
		return core.Tell("that name is another server's launch, or this server has one already")
	case err != nil:
		return err
	}
	l, err := m.store.Get(ctx, name)
	if err != nil {
		return err
	}
	if err := m.edge.push(ctx, l, g); err != nil {
		return fmt.Errorf("%w: %w", core.Tell(refusalEdge(err)), err)
	}
	card, err := m.panel(ctx, g.ID, "✓ **"+g.Name+"** has its page; its name and picture follow the server when they change")
	if err != nil {
		return err
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

// refresh sends a launch's server again after it changed, if it's one.
func (m *Module) refresh(g discord.Guild) {
	if m.ready() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	l, err := m.store.Of(ctx, g.ID)
	if err == nil {
		err = m.edge.push(ctx, l, g)
	}
	if err != nil && !errors.Is(err, store.ErrNoLaunch) {
		m.log.Warn("refreshing a launch", "guild", g.ID, "err", err)
	}
}

// sync sends cygnet.lol what l's page shows, and says so on a line of its
// own if it couldn't: the change itself is kept, and check again sends it
// again.
func (m *Module) sync(ctx context.Context, l store.Launch, guild snowflake.ID) string {
	g, ok := m.guild(guild)
	if !ok {
		return ""
	}
	if err := m.edge.push(ctx, l, g); err != nil {
		m.log.Warn("sending a launch to cygnet.lol", "launch", l.Name, "err", err)
		return "\n-# ⚠ " + refusalEdge(err) + "; its page shows what it had until check again sends it"
	}
	return ""
}

// invitesEach is how many invites a release gives each person.
const invitesEach = 3

// panel is the launch: the button that names its page while it has none,
// then the campaign in numbers, the stage buttons with the current one
// off, and the releases.
func (m *Module) panel(ctx context.Context, guild snowflake.ID, note string) (discord.ContainerComponent, error) {
	if err := m.ready(); err != nil {
		return discord.ContainerComponent{}, err
	}
	l, err := m.store.Of(ctx, guild)
	sep := discord.NewSeparator(discord.SeparatorSpacingSizeSmall)
	switch {
	case errors.Is(err, store.ErrNoLaunch):
		return brand.Card(brand.ColorInfo, "launch", "",
			"▸ this server has no page yet\n-# name it and people can get in line at cygnet.lol/<name>; nobody is let in until you release invites",
			sep, discord.NewActionRow(discord.NewPrimaryButton("name its page", id+":name"))), nil
	case err != nil:
		return discord.ContainerComponent{}, err
	}
	s, err := m.store.Count(ctx, l.Name, m.now())
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	// ponytail: the whole ledger walked on every draw; keep a checked head
	// if a launch's ledger grows past what a panel can wait for.
	v, err := m.store.Verify(ctx, l.Name)
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	var b strings.Builder
	if note != "" {
		b.WriteString(note + "\n")
	}
	b.WriteString("-# " + m.edge.page(l.Name) + "\n")
	fmt.Fprintf(&b, "```\nstage     %s\non list   %d · %d held\nin line   %d · %d waiting\nreleased  %s\ninvites   %d made · %d used · %d open\nledger    %s\n```",
		l.Stage, s.Total, s.Held, s.InLine, s.Waiting, people(s.ReleasedTo), s.InvitesMade, s.InvitesUsed, s.InvitesOpen, ledger(v))
	w, err := m.store.Last(ctx, l.Name)
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	b.WriteString("\n" + valveLine(l, w, s.Admitted))
	b.WriteString("\n-# gathering lets people get on the list; closed stops new sign ups; open lets the line in by waves, invites still bring friends in, and the clock on deleting the list starts")
	if !l.Opens.IsZero() {
		b.WriteString("\n-# opened " + l.Opens.UTC().Format(time.DateOnly))
	}
	stage := func(st string) discord.InteractiveComponent {
		btn := discord.NewSecondaryButton(st, id+":stage:"+st)
		if st == l.Stage {
			btn = btn.AsDisabled()
		}
		return btn
	}
	release := func(n int) discord.InteractiveComponent {
		btn := discord.NewPrimaryButton(fmt.Sprintf("release to next %d", n), fmt.Sprintf("%s:release:%d", id, n))
		if s.Waiting == 0 {
			btn = btn.AsDisabled()
		}
		return btn
	}
	return brand.Card(brand.ColorInfo, "launch", l.Name, b.String(), sep,
		discord.NewActionRow(stage(store.StageGathering), stage(store.StageClosed), stage(store.StageOpen)),
		discord.NewActionRow(release(10), release(50), discord.NewSecondaryButton("check again", id)),
		discord.NewActionRow(ceilingMenu(l.Ceiling)),
		discord.NewActionRow(valveButton(l.Paused)),
		// The list itself is on the web, behind a Discord sign in, for the
		// server's owner and the breakglass admin only: never in a channel.
		discord.NewActionRow(discord.NewLinkButton("who's on the list", m.edge.page(l.Name)+"/list")),
	), nil
}

// ceilings is what an admin can keep waves under, in joins a minute.
var ceilings = []int{5, 10, 20, 50, 100}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// valveLine is the valve on the panel: what it's set to, the last wave and
// how it's going, and why it was the size it was.
func valveLine(l store.Launch, w store.Wave, admitted int) string {
	state := "runs once the launch is open"
	switch {
	case l.Paused:
		state = "paused"
	case l.Stage == store.StageOpen:
		state = "runs"
	}
	line := fmt.Sprintf("▸ the valve %s, under %d joins a minute", state, l.Ceiling)
	if w.ID == 0 {
		return line + "\n-# no wave yet"
	}
	return fmt.Sprintf("%s\n-# wave %d let in %s, %d of them here; %s let in so far\n-# %s", line, w.ID, people(w.Admitted), w.Arrived, people(admitted), w.Why)
}

func ceilingMenu(c int) discord.InteractiveComponent {
	var opts []discord.StringSelectMenuOption
	for _, n := range ceilings {
		o := discord.StringSelectMenuOption{Label: fmt.Sprintf("under %d joins a minute", n), Value: strconv.Itoa(n)}
		o.Default = n == c
		opts = append(opts, o)
	}
	return discord.NewStringSelectMenu(id+":ceiling", "how fast waves come in", opts...)
}

func valveButton(paused bool) discord.InteractiveComponent {
	if paused {
		return discord.NewPrimaryButton("run the valve", id+":valve:run")
	}
	return discord.NewSecondaryButton("pause the valve", id+":valve:pause")
}

// ledger is what a walk of the ledger found, in a few words.
func ledger(v store.Verified) string {
	switch {
	case v.Broken != 0:
		return fmt.Sprintf("✗ broken at entry %d", v.Broken)
	case len(v.Mismatches) > 0:
		return fmt.Sprintf("✗ %d counts it doesn't account for", len(v.Mismatches))
	}
	return fmt.Sprintf("✓ %d entries", v.Entries)
}

func (e *Edge) page(name string) string { return strings.TrimRight(e.URL, "/") + "/" + name }

// admins is who may see a launch's list on the web: the server's owner,
// and the breakglass admin.
func (e *Edge) admins(g discord.Guild) []snowflake.ID {
	var out []snowflake.ID
	for _, id := range []snowflake.ID{g.OwnerID, e.Breakglass} {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
}

// push sends what a launch's page shows: its server as cygnet sees it, its
// stage and when it first opened (which starts the clock on the Worker's
// hashes), and who may see its list. The Worker keeps it as sent, and asks
// cygnet for everything else.
func (e *Edge) push(ctx context.Context, l store.Launch, g discord.Guild) error {
	admins := []string{}
	for _, id := range e.admins(g) {
		admins = append(admins, id.String())
	}
	body := map[string]any{"guild": g.ID.String(), "title": g.Name, "icon": icon(g.ID, g.Icon), "admins": admins, "stage": l.Stage}
	if !l.Opens.IsZero() {
		body["opens"] = l.Opens.UnixMilli()
	}
	return e.do(ctx, http.MethodPut, "/admin/"+l.Name, body, nil)
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
		return "cygnet.lol knows that name as another server's launch"
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
