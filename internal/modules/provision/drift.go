package provision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/guard"
)

// Kept is a server's layout, how far cygnet goes on her own when the server
// drifts from it, and what its admins chose to leave alone: changes, by
// their line in the preview, that cygnet no longer makes there.
type Kept struct {
	Layout, Mode string
	Leave        []string
}

// The modes, from least to most on her own.
const (
	Watch   = "watch"
	Propose = "propose"
	Act     = "act"
)

var modes = []string{Watch, Propose, Act}

// settle is how long a server has to stop changing before cygnet checks it
// against its layout, so an admin's run of edits makes one report.
const settle = 5 * time.Second

// Keep gives the module the servers already provisioned and where to save
// them; without it they last until a restart.
func (m *Module) Keep(kept map[snowflake.ID]Kept, save func(context.Context, snowflake.ID, Kept) error, log *slog.Logger) *Module {
	for g, k := range kept {
		m.kept.Store(g, k)
	}
	m.save, m.log = save, log
	return m
}

// OnEvent watches the roles and channels of every provisioned server.
func (m *Module) OnEvent(ev bot.Event) {
	if g, c, ok := heard(ev); ok {
		m.live.soon(g, func() { m.tick(context.Background(), c.Rest, g, time.Now()) })
	}
	var guild snowflake.ID
	var client *bot.Client
	switch e := ev.(type) {
	case *events.Ready:
		// Reports from the birds come in over the hub, not an interaction:
		// they're answered with the client cygnet connected with.
		m.grants.mu.Lock()
		m.grants.rest, m.grants.self = e.Client().Rest, e.Client().ApplicationID
		m.grants.mu.Unlock()
		return
	case *events.RoleCreate:
		// A bot joining makes its role, which wants the bots' colour, and
		// a bird its overwrites.
		guild, client = e.GuildID, e.Client()
	case *events.RoleUpdate:
		guild, client = e.GuildID, e.Client()
	case *events.RoleDelete:
		guild, client = e.GuildID, e.Client()
	case *events.GuildChannelUpdate:
		guild, client = e.GuildID, e.Client()
	case *events.GuildChannelDelete:
		guild, client = e.GuildID, e.Client()
	default:
		return
	}
	if _, ok := m.kept.Load(guild); !ok {
		return
	}
	m.soon(guild, func() { m.check(context.Background(), client.Rest, guild, client.ApplicationID) })
}

// soon runs f once guild has gone settle without another change.
//
// ponytail: a timer a provisioned server with a pending check; fine for any
// number of servers cygnet will keep.
func (m *Module) soon(guild snowflake.ID, f func()) {
	t := time.AfterFunc(m.settle, func() {
		m.timers.Delete(guild)
		f()
	})
	if old, loaded := m.timers.Swap(guild, t); loaded {
		old.(*time.Timer).Stop()
	}
}

// check plans guild against its layout and reports drift to the layout's
// report channel, once per distinct drift, putting it back first in act.
func (m *Module) check(ctx context.Context, r rest.Rest, guild, self snowflake.ID) {
	v, ok := m.kept.Load(guild)
	if !ok {
		return
	}
	k := v.(Kept)
	l, ok := layout(k.Layout)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	p, err := m.read(ctx, r, guild, self, l, false)
	if err != nil {
		m.warn("checking for drift", guild, err)
		return
	}
	if len(p.changes) == 0 {
		m.posted.Delete(guild)
		return
	}
	d := digest(p.changes)
	if last, _ := m.posted.Load(guild); last == d {
		return
	}
	reports, ok := p.s.named(l.Reports)
	if !ok {
		m.warn("reporting drift", guild, errNoReports)
		return
	}
	m.posted.Store(guild, d)
	var card record
	switch {
	case k.Mode == Act && p.short == 0 && p.todo() > 0:
		if _, busy := m.applying.LoadOrStore(guild, true); busy {
			return
		}
		done, err := apply(ctx, r, m.guard, p.s, l, p.changes, func(int) {})
		m.applying.Delete(guild)
		if err == nil {
			// Put back, it's no longer a drift: the same one again is news.
			m.posted.Delete(guild)
		}
		card = p.putBack(done, err)
	default:
		card = p.drifted(k.Mode)
	}
	if err := m.post(ctx, r, guild, reports, card.create()); err != nil {
		m.warn("reporting drift", guild, err)
	}
}

// report posts card to l's report channel, wherever it is now.
func (m *Module) report(ctx context.Context, r rest.Rest, guild snowflake.ID, l Layout, card discord.ContainerComponent) {
	channels, err := r.GetGuildChannels(guild, rest.WithCtx(ctx))
	if err != nil {
		m.warn("reporting a layout", guild, err)
		return
	}
	reports, ok := state{channels: channels}.named(l.Reports)
	if !ok {
		m.warn("reporting a layout", guild, errNoReports)
		return
	}
	if err := m.post(ctx, r, guild, reports, asPanel(card)); err != nil {
		m.warn("reporting a layout", guild, err)
	}
}

// Poster is what posting a report takes.
type Poster interface {
	CreateMessage(channelID snowflake.ID, messageCreate discord.MessageCreate, opts ...rest.RequestOpt) (*discord.Message, error)
}

func (m *Module) post(ctx context.Context, r Poster, guild, channel snowflake.ID, msg discord.MessageCreate) error {
	if err := m.guard.Allow(guild, guard.MessageSend); err != nil {
		return err
	}
	msg.AllowedMentions = core.NoPings()
	_, err := r.CreateMessage(channel, msg, rest.WithCtx(ctx))
	m.guard.Report(guild, struggling(err))
	return err
}

// asPanel is card as a message of its own.
func asPanel(card discord.ContainerComponent) discord.MessageCreate {
	return discord.MessageCreate{Components: []discord.LayoutComponent{card}, Flags: discord.MessageFlagIsComponentsV2}
}

// asPanelUpdate is card replacing the message a button is on. A drift
// report is a record until pressed, and Discord turns a message into
// Components V2 on an edit only when the edit empties its embeds; its
// files go with them.
func asPanelUpdate(card discord.ContainerComponent) discord.MessageUpdate {
	u := discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings())
	u.Embeds = &[]discord.Embed{}
	u.Attachments = &[]discord.AttachmentUpdate{}
	return u
}

// record is a report of something that happened (DESIGN.md's A record):
// an embed wearing cygnet's face for color and her rule, the files those
// take, and the rows of controls under it.
type record struct {
	embed discord.Embed
	files []*discord.File
	rows  []discord.LayoutComponent
}

// newRecord is a record titled title, its facts in sections (who and
// where, then what) and notes in the footer, with controls in row.
func newRecord(color int, title string, sections [][][2]string, notes []string, row ...discord.InteractiveComponent) record {
	e, files := brand.Embed(color, "provision", title, "")
	e.Fields = brand.Fields(sections...)
	if len(notes) > 0 {
		e.Footer = &discord.EmbedFooter{Text: strings.Join(notes, " · ")}
	}
	return record{embed: e, files: files}.with(row...)
}

// automatic is cygnet as the actor of what she did on her own.
const automatic = "cygnet (automatic)"

// text is r as a panel's lines: its title, then a line per fact with its
// key in bold, for a message that can't hold an embed.
func (r record) text() string {
	out := "**" + r.embed.Title + "**"
	for _, f := range r.embed.Fields {
		if strings.TrimSpace(f.Name) != "" && f.Name != "\u200b" {
			out += "\n**" + f.Name + "** " + strings.ReplaceAll(f.Value, "\n", ", ")
		}
	}
	return out
}

// with is r with another row of controls under it, or r for none.
func (r record) with(row ...discord.InteractiveComponent) record {
	if len(row) > 0 {
		r.rows = append(slices.Clone(r.rows), discord.NewActionRow(row...))
	}
	return r
}

// note is r with one more line in its footer.
func (r record) note(s string) record {
	if r.embed.Footer == nil {
		r.embed.Footer = &discord.EmbedFooter{Text: s}
		return r
	}
	r.embed.Footer = &discord.EmbedFooter{Text: r.embed.Footer.Text + " · " + s}
	return r
}

func (r record) components() []discord.LayoutComponent {
	return append([]discord.LayoutComponent{}, r.rows...)
}

// create is r as a new message.
func (r record) create() discord.MessageCreate {
	return discord.MessageCreate{Embeds: []discord.Embed{r.embed}, Files: r.files, Components: r.components()}
}

// update is r replacing a message that was a record: its files are sent
// again and the old ones dropped, so the embed never points at nothing.
func (r record) update() discord.MessageUpdate {
	c := r.components()
	return discord.MessageUpdate{
		Embeds:          &[]discord.Embed{r.embed},
		Components:      &c,
		Files:           r.files,
		Attachments:     &[]discord.AttachmentUpdate{},
		AllowedMentions: core.NoPings(),
	}
}

var errNoReports = errors.New("the layout's report channel is gone")

func (m *Module) warn(what string, guild snowflake.ID, err error) {
	if m.log != nil {
		m.log.Warn(what, "guild", guild, "err", err)
	}
}

// named is the text channel called name, styled or not, in any category.
func (s state) named(name string) (snowflake.ID, bool) {
	for _, c := range s.channels {
		if c.Type() == discord.ChannelTypeGuildText && strings.EqualFold(bare(c.Name()), name) {
			return c.ID(), true
		}
	}
	return 0, false
}

// drifted is the report in watch and propose: what would put it back, and
// in propose the buttons that do.
func (p planned) drifted(mode string) record {
	r := newRecord(brand.ColorWarn, "layout drifted", [][][2]string{{{"layout", p.l.Name}, {"mode", mode}}, p.facts()}, p.waiting)
	if mode != Watch {
		r = r.with(p.buttons()...)
	}
	return p.leavable(r, "")
}

// putBack is the report in act: what drifted, and that cygnet put it back
// or where she stopped.
func (p planned) putBack(done int, err error) record {
	if err != nil {
		why := refusal(err)
		if tell, ok := asTell(err); ok {
			why = tell
		}
		who := [][2]string{{"actor", automatic}, {"layout", p.l.Name}}
		what := [][2]string{{"put back", fmt.Sprintf("%d of %d", done, p.todo())}, {"progress", bar(done, p.todo())}, {"why", why}}
		return newRecord(brand.ColorError, "putting the layout back stopped", [][][2]string{who, what}, nil,
			discord.NewSecondaryButton("check again", previewID+":"+p.l.Name))
	}
	who := [][2]string{{"actor", automatic}, {"layout", p.l.Name}, {"put back", count(done, "change")}}
	r := newRecord(brand.ColorOK, "layout put back", [][][2]string{who, p.facts()}, p.waiting)
	return p.leavable(r, "if one was on purpose, leave it alone and make the change again; cygnet won't put it back")
}

// leaveID is the select on a drift report that leaves changes alone.
const leaveID = "provision-leave"

// leavable adds the select that leaves some of p's changes alone to a
// drift report, with why in its footer. A fresh start's removals aren't
// drift, so they're never offered.
func (p planned) leavable(r record, why string) record {
	var opts []discord.StringSelectMenuOption
	seen := map[string]bool{}
	for _, c := range p.changes {
		key := c.line()
		if c.kind == remove || c.kind == archive || seen[key] || len(opts) == 25 {
			continue
		}
		seen[key] = true
		opts = append(opts, discord.StringSelectMenuOption{Label: key, Value: key})
	}
	if len(opts) == 0 {
		return r
	}
	if why != "" {
		r = r.note(why)
	}
	return r.with(discord.NewStringSelectMenu(leaveID, "leave some alone", opts...).WithMinValues(1).WithMaxValues(len(opts)))
}

// most is how many changes a server can leave alone.
const most = 100

// leavePressed leaves the changes picked on a drift report alone in this
// server from now on. The values came from the member's client: each is
// only ever compared to a change's line, so a forged one leaves nothing
// alone but itself, and their size and number are capped.
func (m *Module) leavePressed(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if e.GuildID() == nil || !m.admin(e) {
		return core.Tell("only this server's admins can use /provision")
	}
	guild := *e.GuildID()
	v, ok := m.kept.Load(guild)
	if !ok {
		return core.Tell("cygnet doesn't keep this server to a layout anymore")
	}
	k := v.(Kept)
	picked := e.StringSelectMenuInteractionData().Values
	leave := slices.Clone(k.Leave)
	for _, key := range picked {
		if len(key) > 100 {
			return core.Tell("that report is from an older cygnet; run /provision again")
		}
		if !slices.Contains(leave, key) {
			leave = append(leave, key)
		}
	}
	if len(leave) > most {
		return core.Tell(fmt.Sprintf("this server leaves %d changes alone, which is the most it can; forget them on /provision first", most))
	}
	k.Leave = leave
	m.store(ctx, guild, k)
	reply := newRecord(brand.ColorOK, "changes left alone",
		[][][2]string{{{"left", strings.Join(picked, "\n")}}}, []string{"/provision shows what's left alone and forgets it"}).create()
	reply.Flags = discord.MessageFlagEphemeral
	reply.AllowedMentions = core.NoPings()
	return e.CreateMessage(reply)
}

// keep remembers that guild now follows l, keeping its mode, and what it
// leaves alone unless this was a fresh start.
func (m *Module) keep(ctx context.Context, guild snowflake.ID, l string, fresh bool) {
	k := Kept{Layout: l, Mode: Propose}
	if v, ok := m.kept.Load(guild); ok {
		k.Mode = v.(Kept).Mode
		if !fresh {
			k.Leave = v.(Kept).Leave
		}
	}
	m.store(ctx, guild, k)
}

func (m *Module) store(ctx context.Context, guild snowflake.ID, k Kept) {
	m.kept.Store(guild, k)
	m.posted.Delete(guild)
	if m.save != nil {
		if err := m.save(ctx, guild, k); err != nil {
			m.warn("saving a provisioned server", guild, err)
		}
	}
}
