package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type SupportAddress struct {
	Wallet, Address string
	Minor           uint32
}

func (s *Support) FreeAddresses(ctx context.Context, wallet string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, "select count(*) from support_address where wallet = $1 and invoice is null", wallet).Scan(&count)
	return count, err
}

func (s *Support) AddAddresses(ctx context.Context, addresses []SupportAddress) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, a := range addresses {
		// A restored wallet may revisit an existing index. Accept only the
		// same allocation, never an address belonging to another wallet.
		var actual string
		err := tx.QueryRow(ctx, `insert into support_address (wallet,address,minor) values ($1,$2,$3)
		 on conflict (wallet,minor) do update set minor=excluded.minor returning address`, a.Wallet, a.Address, int64(a.Minor)).Scan(&actual)
		if err != nil {
			return err
		}
		if actual != a.Address {
			return fmt.Errorf("store: Monero subaddress allocation changed")
		}
	}
	return tx.Commit(ctx)
}

// WatchedAddresses pages all addresses ever assigned, including expired
// invoices, so late funds remain attributable. An empty page wraps around.
func (s *Support) WatchedAddresses(ctx context.Context, wallet string, after uint32, active bool) ([]SupportAddress, error) {
	rows, err := s.pool.Query(ctx, `select a.wallet,a.address,a.minor from support_address a join support_invoice i on i.id=a.invoice
	 where a.wallet=$1 and a.minor>$2 and (i.state in ('open','lapsed'))=$3 order by a.minor limit 32`, wallet, int64(after), active)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SupportAddress, error) {
		var a SupportAddress
		var minor int64
		err := row.Scan(&a.Wallet, &a.Address, &minor)
		a.Minor = uint32(minor)
		return a, err
	})
}
