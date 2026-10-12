package invite

import (
	"context"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

const (
	// sweepWindow is how far back the sweep looks for codes spent by
	// someone not seen arriving: past it, they never came.
	sweepWindow = 7 * 24 * time.Hour
	// eventWithin bounds the database work one gateway event starts.
	eventWithin = 10 * time.Second
)

// OnEvent traces joins (SPEC.md, Principles 11). A member who joins is
// recorded with the code they spent there, or as a join cygnet can't
// trace; one who leaves keeps their place in the tree. Once the servers
// are loaded, the sweep finds the joins missed while cygnet was away.
// Each runs off the gateway's goroutine.
func (m *Module) OnEvent(ev bot.Event) {
	switch e := ev.(type) {
	case *events.GuildMemberJoin:
		if e.Member.User.Bot {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), eventWithin)
			defer cancel()
			m.arrive(ctx, e.GuildID, e.Member.User.ID, m.joined(e.Member))
		}()
	case *events.GuildMemberLeave:
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), eventWithin)
			defer cancel()
			if err := m.store.Leave(ctx, e.GuildID, e.User.ID, m.now()); err != nil {
				m.log.Warn("recording a leave", "guild", e.GuildID, "err", err)
			}
		}()
	case *events.GuildsReady:
		go m.sweep(context.Background())
	}
}

// sweep records the arrival of everyone who spent a code lately and is in
// its server now, asking Discord one member at a time.
func (m *Module) sweep(ctx context.Context) {
	codes, err := m.store.Pending(ctx, m.now().Add(-sweepWindow))
	if err != nil {
		m.log.Warn("sweeping for missed joins", "err", err)
		return
	}
	found := 0
	for _, c := range codes {
		if !m.on(c.Guild) {
			continue
		}
		mctx, cancel := context.WithTimeout(ctx, eventWithin)
		mem, err := m.member(mctx, c.Guild, c.SpentBy)
		if err == nil && mem != nil {
			m.arrive(mctx, c.Guild, c.SpentBy, m.joined(*mem))
			found++
		}
		cancel()
		if err != nil {
			m.log.Warn("sweeping for missed joins", "guild", c.Guild, "err", err)
		}
	}
	m.log.Info("swept for missed joins", "pending", len(codes), "arrived", found)
}

// joined is when mem joined, as Discord says, or now if it doesn't.
func (m *Module) joined(mem discord.Member) time.Time {
	if mem.JoinedAt != nil {
		return *mem.JoinedAt
	}
	return m.now()
}
