package provision

import (
	"context"
	"errors"
	"log/slog"
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

// Kept is a server's layout and how far cygnet goes on her own when the
// server drifts from it.
type Kept struct{ Layout, Mode string }

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
	var guild snowflake.ID
	var client *bot.Client
	switch e := ev.(type) {
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
	var card discord.ContainerComponent
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
	if err := m.post(ctx, r, guild, reports, card); err != nil {
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
	if err := m.post(ctx, r, guild, reports, card); err != nil {
		m.warn("reporting a layout", guild, err)
	}
}

func (m *Module) post(ctx context.Context, r rest.Rest, guild, channel snowflake.ID, card discord.ContainerComponent) error {
	if err := m.guard.Allow(guild, guard.MessageSend); err != nil {
		return err
	}
	_, err := r.CreateMessage(channel, discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	}, rest.WithCtx(ctx))
	m.guard.Report(guild, struggling(err))
	return err
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

// drifted is the report in watch and propose: what changed, and in propose
// the button that puts it back.
func (p planned) drifted(mode string) discord.ContainerComponent {
	note := "this server drifted from **" + p.l.Name + "**; here's what would put it back"
	if mode == Watch {
		return p.card(note, false)
	}
	return p.card(note, true)
}

// putBack is the report in act: what drifted, and that cygnet put it back.
func (p planned) putBack(done int, err error) discord.ContainerComponent {
	if err != nil {
		return p.result(done, p.todo(), err)
	}
	body := "✓ this server drifted from **" + p.l.Name + "**, so cygnet put back " + count(done, "change") + "\n" + p.diff()
	return brand.Card(brand.ColorOK, "provision", "drift", body)
}

// keep remembers that guild now follows l, keeping its mode.
func (m *Module) keep(ctx context.Context, guild snowflake.ID, l string) {
	k := Kept{Layout: l, Mode: Propose}
	if v, ok := m.kept.Load(guild); ok {
		k.Mode = v.(Kept).Mode
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

// modeCmd sets how far cygnet goes on her own when this server drifts.
func (m *Module) modeCmd(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	guild := *e.GuildID()
	v, ok := m.kept.Load(guild)
	if !ok {
		return core.Tell("apply a layout here first; cygnet watches only what she set up")
	}
	k := v.(Kept)
	k.Mode = e.SlashCommandInteractionData().String("level")
	if k.Mode == "" {
		k.Mode = Propose
	}
	m.store(ctx, guild, k)
	l, _ := layout(k.Layout)
	what := map[string]string{
		Watch:   "say so in #" + l.Reports,
		Propose: "say so in #" + l.Reports + ", with a button to put it back",
		Act:     "put it back and say what she did in #" + l.Reports,
	}[k.Mode]
	card := brand.Card(brand.ColorOK, "provision", "mode", "✓ when this server drifts from **"+k.Layout+"**, cygnet will "+what)
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}
