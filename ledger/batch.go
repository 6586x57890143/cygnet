package ledger

import (
	"context"
	"sync"
	"time"
)

// Defaults for a Batcher left at its zero values.
const (
	DefaultWindow = 10 * time.Second
	DefaultMax    = 25
)

// Batcher posts entries to a log channel, folding a run of the same action
// by the same actor into one card that edits itself as it grows, the way a
// live readout does (UX.md). A mod jailing five people is one card saying
// five, not five cards; a sweep releasing three is one. The entries are
// already stored one row each before they get here: batching changes only
// what the channel shows.
//
// It knows nothing about Discord's client: the bird gives it Post and Edit,
// and decides where they go (the guild's log channel) and how they fail.
type Batcher struct {
	Brand Brand
	// Window is how long after its first entry a card keeps taking more.
	Window time.Duration
	// Max is how many entries a card takes before the next starts a new one.
	Max int
	// Post sends a new card to guild's log channel and returns its message ID.
	Post func(ctx context.Context, guild string, card Card) (string, error)
	// Edit replaces the card at message.
	Edit func(ctx context.Context, guild, message string, card Card) error
	// Now is the clock, for tests.
	Now func() time.Time

	// ponytail: one lock across every guild, held through the Discord call
	// so an edit never races the post it extends. Fine at log volume; key
	// the lock by guild if a slow channel ever holds the others up.
	mu   sync.Mutex
	open map[batchKey]*openCard
}

type batchKey struct{ guild, action, actor string }

type openCard struct {
	message string
	started time.Time
	entries []Entry
}

// Add shows e in its guild's log channel: as an edit to the open card for
// the same action and actor if there is one, else as a new card.
//
// A batch shows its entries' summaries, not their buttons: an undo belongs
// to one entry, and only a card holding one entry carries them.
func (b *Batcher) Add(ctx context.Context, e Entry) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	window, limit := b.Window, b.Max
	if window <= 0 {
		window = DefaultWindow
	}
	if limit <= 0 {
		limit = DefaultMax
	}
	if b.open == nil {
		b.open = map[batchKey]*openCard{}
	}
	for k, c := range b.open {
		if now.Sub(c.started) >= window {
			delete(b.open, k)
		}
	}

	key := batchKey{e.Guild, e.Action, string(e.Actor.Kind) + ":" + e.Actor.ID}
	if c, ok := b.open[key]; ok && len(c.entries) < limit {
		entries := append(c.entries, e)
		if err := b.Edit(ctx, e.Guild, c.message, Render(b.Brand, entries...)); err == nil {
			c.entries = entries
			return nil
		}
		// The card is gone or will not take the edit: start a fresh one
		// rather than lose this entry from the channel.
		delete(b.open, key)
	}
	msg, err := b.Post(ctx, e.Guild, Render(b.Brand, e))
	if err != nil {
		return err
	}
	b.open[key] = &openCard{message: msg, started: now, entries: []Entry{e}}
	return nil
}

func (b *Batcher) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}
