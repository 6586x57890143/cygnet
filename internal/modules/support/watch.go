package support

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/6586x57890143/cygnet/internal/chain"
	"github.com/6586x57890143/cygnet/internal/store"
)

const (
	// every is how often the chains are read for payments.
	every = time.Minute
	// grace is how long a checkout past its time still takes a payment
	// and keeps its amount: long enough for a transfer someone started at
	// the last minute, or one an exchange held for a while.
	grace = 48 * time.Hour
	// railTime bounds reading one rail, so a slow one can't take the
	// round from the rest.
	railTime = 30 * time.Second
	// writeTime bounds the writes after a read, on their own clock: a read
	// that ran into railTime still keeps what it got.
	writeTime = 10 * time.Second
)

// watch reads every rail for payments each minute until ctx ends.
func (m *Module) watch(ctx context.Context) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		m.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tick is one round: lapse and void what ran out, then read each rail,
// each on its own clock. One rail failing or running long leaves its
// cursor where it was and the rest read on.
func (m *Module) tick(ctx context.Context) {
	sctx, cancel := context.WithTimeout(ctx, railTime)
	if err := m.store.Sweep(sctx, m.now(), grace); err != nil {
		m.log.Warn("sweeping checkouts", "err", err)
	}
	cancel()
	for _, r := range m.rails() {
		rctx, cancel := context.WithTimeout(ctx, railTime)
		if err := m.scan(rctx, r); err != nil {
			m.log.Warn("reading a rail", "rail", r.Key, "err", err)
		}
		cancel()
	}
}

// scan reads one rail from where it left off, records what arrived, and
// only then moves its cursor on: a crash between the two reads the same
// transfers again, which the store takes as already seen.
func (m *Module) scan(ctx context.Context, r chain.Rail) error {
	to := m.wallets.of(r.Family)
	switch r.Family {
	case chain.EVM, chain.Tron:
		return m.cursor(ctx, r.Key, m.now(), func(c store.Cursor) ([]chain.Transfer, string, error) {
			at, _ := strconv.ParseInt(c.At, 10, 64)
			if r.Family == chain.EVM {
				got, next, err := m.reader.EVM(ctx, r, to, uint64(at))
				return got, strconv.FormatUint(next, 10), err
			}
			got, next, err := m.reader.Tron(ctx, r, to, at, m.now())
			return got, strconv.FormatInt(next, 10), err
		}, r)
	case chain.Solana:
		// The rail's own cursor is when it was first watched. A token
		// account found later starts from then, not from when it was
		// found: a wallet that never held the token gets its account from
		// the first payment, which lands before the account can be listed.
		// The rail's cursor keeps the wallet it watches. A different one
		// starts from now, so its history before cygnet watched it is
		// never taken for payments.
		rc, err := m.store.Cursor(ctx, r.Key, m.now())
		if err != nil {
			return err
		}
		if rc.At != "" && rc.At != to {
			rc.Since = m.now()
			if err := m.store.Restart(ctx, r.Key, rc.Since); err != nil {
				return err
			}
		}
		accounts, err := m.reader.Accounts(ctx, r, to)
		if err != nil {
			return m.failed(ctx, r.Key, err)
		}
		keys := make([]string, len(accounts))
		for i, a := range accounts {
			keys[i] = r.Key + ":" + a
		}
		// An account no longer listed (closed, or spam that went) keeps no
		// error on the panel.
		w, cancel := writes(ctx)
		defer cancel()
		if err := m.store.ClearErrors(w, r.Key+":", keys); err != nil {
			return err
		}
		// Anyone can open a token account the wallet owns, so the accounts
		// share the rail's clock in an order that turns every round: a
		// flooded one slows the round down, never stops the others for
		// good, and never holds up the other rails.
		var errs []error
		turn := int(m.rounds.Add(1))
		for n := range accounts {
			i := (n + turn) % len(accounts)
			err := m.cursor(ctx, keys[i], rc.Since, func(c store.Cursor) ([]chain.Transfer, string, error) {
				return m.reader.Solana(ctx, r, to, accounts[i], c.At, c.Since)
			}, r)
			if err != nil {
				errs = append(errs, err)
			}
		}
		if err := errors.Join(errs...); err != nil {
			return err
		}
		return m.store.Advance(w, r.Key, to, m.now())
	}
	return fmt.Errorf("no reader for %s", r.Family)
}

// cursor reads key from its cursor with next, records each transfer as a
// payment on rail, and moves the cursor on. A key never read before
// starts at since.
func (m *Module) cursor(ctx context.Context, key string, since time.Time, next func(store.Cursor) ([]chain.Transfer, string, error), r chain.Rail) error {
	c, err := m.store.Cursor(ctx, key, since)
	if err != nil {
		return err
	}
	// A reader that fails part way may still return what it read and how
	// far (the EVM reader does, span by span): that is kept, and only then
	// is the error reported, or a rail behind on a slow node would restart
	// from the same block every round. A first read (no cursor yet) moves
	// nothing on an error: its "how far" can be the zero it started from.
	got, at, err := next(c)
	if err != nil && (c.At == "" || at == c.At) && len(got) == 0 {
		return m.failed(ctx, key, err)
	}
	w, cancel := writes(ctx)
	defer cancel()
	for _, t := range got {
		rec, err := m.store.Record(w, store.Payment{Rail: r.Key, Tx: t.Tx, Idx: t.Idx, Amount: t.Amount, Sender: t.From, Sent: t.At, Seen: m.now()})
		if err != nil {
			return m.failed(ctx, key, err)
		}
		switch {
		case rec.Paid():
			m.log.Info("a checkout was paid", "checkout", rec.Invoice.ID, "rail", r.Key, "months", rec.Invoice.Months, "gold", rec.Invoice.Gold)
		case rec.New:
			m.log.Info("a payment matched no checkout", "rail", r.Key, "tx", t.Tx, "amount", r.Format(t.Amount))
		}
	}
	if err != nil {
		if c.At != "" && at != c.At {
			if aerr := m.store.Advance(w, key, at, m.now()); aerr != nil {
				return aerr
			}
		}
		return m.failed(ctx, key, err)
	}
	return m.store.Advance(w, key, at, m.now())
}

// writes is ctx for the writes after a read: not cancelled with the read,
// bounded by writeTime.
func writes(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), writeTime)
}

// failed keeps why reading key failed, for the panel, and returns it.
func (m *Module) failed(ctx context.Context, key string, err error) error {
	w, cancel := writes(ctx)
	defer cancel()
	_ = m.store.Failed(w, key, err.Error())
	return err
}
