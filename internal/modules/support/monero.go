package support

import (
	"context"
	"strconv"

	"github.com/6586x57890143/cygnet/internal/chain"
	"github.com/6586x57890143/cygnet/internal/store"
)

func (m *Module) prepareMonero(ctx context.Context) error {
	wallet := m.reader.Monero
	if err := wallet.Check(ctx, m.now()); err != nil {
		return err
	}
	count, err := m.store.FreeAddresses(ctx, wallet.Primary)
	if err != nil {
		return err
	}
	if count >= 8 {
		return nil
	}
	created, err := wallet.CreateAddresses(ctx)
	if err != nil {
		return err
	}
	addresses := make([]store.SupportAddress, 0, len(created))
	for _, a := range created {
		addresses = append(addresses, store.SupportAddress{Wallet: wallet.Primary, Address: a.Address, Minor: a.Index})
	}
	return m.store.AddAddresses(ctx, addresses)
}

func (m *Module) scanMonero(ctx context.Context, r chain.Rail) error {
	wallet := m.reader.Monero
	// Active invoices always get their own scan budget; a long history of
	// paid or expired addresses cannot delay a current checkout behind it.
	for _, active := range []bool{true, false} {
		key := r.Key + ":" + wallet.Primary + ":" + strconv.FormatBool(active)
		c, err := m.store.Cursor(ctx, key, m.now())
		if err != nil {
			return err
		}
		after, _ := strconv.ParseUint(c.At, 10, 32)
		addresses, err := m.store.WatchedAddresses(ctx, wallet.Primary, uint32(after), active)
		if err != nil {
			return m.failed(ctx, key, err)
		}
		if len(addresses) == 0 && after != 0 {
			addresses, err = m.store.WatchedAddresses(ctx, wallet.Primary, 0, active)
		}
		if err != nil {
			return m.failed(ctx, key, err)
		}
		watched := map[uint32]string{}
		for _, a := range addresses {
			watched[a.Minor] = a.Address
		}
		got, err := wallet.Incoming(ctx, watched)
		if err != nil {
			wallet.Invalidate()
			return m.failed(ctx, key, err)
		}
		write, cancel := writes(ctx)
		for _, payment := range got {
			var record store.Recorded
			record, err = m.store.Record(write, store.Payment{Rail: r.Key, Tx: payment.Tx, Event: payment.Event, To: payment.To, Amount: payment.Amount, Sent: payment.At, Seen: m.now()})
			if err != nil {
				break
			}
			if record.Paid() {
				m.log.Info("a checkout was paid", "rail", r.Key, "months", record.Invoice.Months, "gold", record.Invoice.Gold)
			}
		}
		if err == nil {
			next := "0"
			if len(addresses) > 0 {
				next = strconv.FormatUint(uint64(addresses[len(addresses)-1].Minor), 10)
			}
			err = m.store.Advance(write, key, next, m.now())
		}
		cancel()
		if err != nil {
			return m.failed(ctx, key, err)
		}
	}
	return nil
}
