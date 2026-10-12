package provision

import (
	"context"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core"
)

// lasts is how long a fleet card ticks itself off: an interaction's token is
// good for 15 minutes, and the last one is left for the edit.
const lasts = 14 * time.Minute

// watch is one fleet card ticking itself off: the interaction that drew it,
// which can edit it until its token runs out.
type watch struct {
	app   snowflake.ID
	token string
	until time.Time
}

// live is the fleet cards open in each guild, and whether cygnet hears
// members join, which is what ticks them.
type live struct {
	mu    sync.Mutex
	cards map[snowflake.ID][]watch
	// hears is whether the server members intent was identified with.
	hears func() bool
	// pending is each guild's redraw waiting out a burst of events, as an
	// apply's run of role updates, so the burst makes one.
	pending map[snowflake.ID]*time.Timer
}

// burst is how long a guild's events settle before its cards are redrawn.
const burst = time.Second

// soon runs f once guild has gone burst without another event, if it has a
// card open; it's apart from drift's timers, so neither cancels the other.
func (l *live) soon(guild snowflake.ID, f func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.cards[guild]) == 0 {
		return
	}
	if l.pending == nil {
		l.pending = map[snowflake.ID]*time.Timer{}
	}
	if t, ok := l.pending[guild]; ok {
		t.Stop()
	}
	l.pending[guild] = time.AfterFunc(burst, f)
}

// Live gives the module whether cygnet identified with the server members
// intent; without it a fleet card doesn't tick by itself, and says so.
func (m *Module) Live(hears func() bool) *Module {
	m.live.hears = hears
	return m
}

func (l *live) on() bool { return l.hears != nil && l.hears() }

// open keeps a fleet card drawn by an interaction, to redraw as birds join.
func (l *live) open(guild, app snowflake.ID, token string, now time.Time) {
	if !l.on() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cards == nil {
		l.cards = map[snowflake.ID][]watch{}
	}
	l.cards[guild] = append(l.fresh(guild, now), watch{app: app, token: token, until: now.Add(lasts)})
}

// fresh is guild's cards whose tokens still work; call it holding mu.
func (l *live) fresh(guild snowflake.ID, now time.Time) []watch {
	var out []watch
	for _, w := range l.cards[guild] {
		if now.Before(w.until) {
			out = append(out, w)
		}
	}
	return out
}

// Edits is what redrawing an open card calls on Discord.
type Edits interface {
	Rest
	UpdateInteractionResponse(applicationID snowflake.ID, interactionToken string, messageUpdate discord.MessageUpdate, opts ...rest.RequestOpt) (*discord.Message, error)
}

// tick redraws guild's open fleet cards after a bot joined it or a role
// there changed, which is how a re-authorised bird's new permissions land.
// One redraw serves every card in the guild.
func (m *Module) tick(ctx context.Context, r Edits, guild snowflake.ID, now time.Time) {
	m.live.mu.Lock()
	cards := m.live.fresh(guild, now)
	if len(cards) == 0 {
		delete(m.live.cards, guild)
	} else {
		m.live.cards[guild] = cards
	}
	m.live.mu.Unlock()
	if len(cards) == 0 {
		return
	}
	card := m.card(ctx, r, guild, cards[0].app)
	for _, w := range cards {
		if _, err := r.UpdateInteractionResponse(w.app, w.token, discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()), rest.WithCtx(ctx)); err != nil {
			m.warn("redrawing a fleet card", guild, err)
		}
	}
}

// heard is whether ev is something an open fleet card ticks on, and where.
func heard(ev bot.Event) (snowflake.ID, *bot.Client, bool) {
	switch e := ev.(type) {
	case *events.GuildMemberJoin:
		return e.GuildID, e.Client(), e.Member.User.Bot
	case *events.RoleUpdate:
		return e.GuildID, e.Client(), true
	}
	return 0, nil, false
}

// hearing is the note under a fleet card on whether it ticks by itself.
func (m *Module) hearing(self snowflake.ID) discord.ContainerSubComponent {
	if m.live.on() {
		return discord.NewTextDisplay("-# this card ticks itself off as each bird joins, for the next 14 minutes")
	}
	return discord.NewSection(discord.NewTextDisplay("-# turn on the server members intent for cygnet and this card ticks itself off as birds join")).
		WithAccessory(discord.NewLinkButton("portal", "https://discord.com/developers/applications/"+self.String()+"/bot"))
}
