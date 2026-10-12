package store

import (
	"context"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Provisioned is one server's layout, how far cygnet goes when it drifts,
// and what its admins chose to leave alone.
type Provisioned struct {
	Guild        snowflake.ID
	Layout, Mode string
	Leave        []string
}

// AllProvisioned is every provisioned server, for the provision module at
// boot.
func AllProvisioned(ctx context.Context, pool *pgxpool.Pool) ([]Provisioned, error) {
	rows, _ := pool.Query(ctx, "select guild_id, layout, mode, leave from provision")
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Provisioned, error) {
		var p Provisioned
		var guild int64
		err := row.Scan(&guild, &p.Layout, &p.Mode, &p.Leave)
		p.Guild = snowflake.ID(guild)
		return p, err
	})
}

// SaveProvisioned keeps p, replacing what its server had.
func SaveProvisioned(pool *pgxpool.Pool) func(context.Context, Provisioned) error {
	return func(ctx context.Context, p Provisioned) error {
		_, err := pool.Exec(ctx, `insert into provision (guild_id, layout, mode, leave) values ($1, $2, $3, $4)
			on conflict (guild_id) do update set layout = excluded.layout, mode = excluded.mode, leave = excluded.leave`,
			int64(p.Guild), p.Layout, p.Mode, append([]string{}, p.Leave...))
		return err
	}
}
