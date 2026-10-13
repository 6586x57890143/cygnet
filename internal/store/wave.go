package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
)

// Waves (SPEC.md, The valve): a launch that's open lets its line in by
// waves, each from the front of the line, with everyone those people
// brought in. A wave keeps its size and why, in words, and who it let in
// keeps it on their line.

// Wave is one lot let in: how many were let in and how many of them have
// joined the server since.
type Wave struct {
	ID                int64
	Size              int
	Opened, Settled   time.Time
	Why               string
	Admitted, Arrived int
}

// Valve is how a launch's valve is set: the joins a minute it keeps
// under, and whether it's paused.
func (s *Launches) Valve(ctx context.Context, name string, ceiling int, paused bool, admin snowflake.ID, now time.Time) error {
	return s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		tag, err := tx.Exec(ctx, "update launch set ceiling = $2, paused = $3 where name = $1 and (ceiling <> $2 or paused <> $3)", name, ceiling, paused)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		ref := strconv.Itoa(ceiling)
		if paused {
			ref += " paused"
		}
		return add(entry{kind: "valve", actor: s.Who(admin), ref: ref, at: now})
	})
}

// Flowing is every launch whose valve runs: open, and not paused.
func (s *Launches) Flowing(ctx context.Context) ([]Launch, error) {
	rows, _ := s.pool.Query(ctx, "select "+launchCols+" from launch where stage = 'open' and not paused order by name")
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Launch, error) { return scanLaunch(row) })
}

// Last is the launch's latest wave, with how many it let in and how many
// of them have joined since; ID 0 when it has had none.
func (s *Launches) Last(ctx context.Context, name string) (Wave, error) {
	var w Wave
	var settled *time.Time
	err := s.pool.QueryRow(ctx, `select w.id, w.size, w.opened, w.settled, w.why,
			(select count(*) from line l where l.wave = w.id),
			(select count(*) from line l join launch x on x.name = l.launch
				where l.wave = w.id and exists (select 1 from joined j where j.guild_id = x.guild_id and j.user_id = l.user_id and j.at >= w.opened))
		from wave w where w.launch = $1 order by w.id desc limit 1`, name).Scan(&w.ID, &w.Size, &w.Opened, &settled, &w.Why, &w.Admitted, &w.Arrived)
	if errors.Is(err, pgx.ErrNoRows) {
		return Wave{}, nil
	}
	if settled != nil {
		w.Settled = *settled
	}
	return w, err
}

// Settle marks a wave settled: the next may follow.
func (s *Launches) Settle(ctx context.Context, id int64, now time.Time) error {
	_, err := s.pool.Exec(ctx, "update wave set settled = $2 where id = $1 and settled is null", id, now)
	return err
}

// follow lets in, with whoever brought them, everyone on the line whose
// giver is in: into the giver's wave, however deep the tree goes.
func (s *Launches) follow(ctx context.Context, tx pgx.Tx, name string, now time.Time, add func(entry) error) (int, error) {
	n := 0
	for {
		rows, _ := tx.Query(ctx, `update line l set wave = g.wave, admitted = $2
			from code c, line g
			where l.launch = $1 and l.admitted is null and c.hash = l.code and g.launch = $1 and g.user_id = c.owner_id and g.admitted is not null
			returning l.user_id, l.wave`, name, now)
		let, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]int64, error) {
			var r [2]int64
			return r, row.Scan(&r[0], &r[1])
		})
		if err != nil || len(let) == 0 {
			return n, err
		}
		for _, r := range let {
			if err := add(entry{kind: "admit", subject: s.Who(snowflake.ID(r[0])), ref: strconv.FormatInt(r[1], 10), at: now}); err != nil {
				return n, err
			}
		}
		n += len(let)
	}
}

// Follow lets in everyone brought in by someone already let in: an invite
// taken after its giver's wave opened comes in with that wave.
func (s *Launches) Follow(ctx context.Context, name string, now time.Time) (int, error) {
	n := 0
	err := s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		var err error
		n, err = s.follow(ctx, tx, name, now, add)
		return err
	})
	return n, err
}

// Admit opens a wave: the next size people waiting on their own, by place,
// and everyone they brought in, with why it was that size. It returns the
// wave, ID 0 when nobody was waiting.
func (s *Launches) Admit(ctx context.Context, name string, size int, why string, now time.Time) (Wave, error) {
	var w Wave
	errNobody := errors.New("nobody waiting")
	err := s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		if err := tx.QueryRow(ctx, "insert into wave (launch, size, opened, why) values ($1, $2, $3, $4) returning id", name, size, now, why).Scan(&w.ID); err != nil {
			return err
		}
		rows, _ := tx.Query(ctx, `update line set wave = $3, admitted = $4 where launch = $1 and user_id in (
				select user_id from line where launch = $1 and code is null and admitted is null order by place limit $2)
			returning user_id`, name, size, w.ID, now)
		let, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		if len(let) == 0 {
			return errNobody
		}
		for _, u := range let {
			if err := add(entry{kind: "admit", subject: s.Who(snowflake.ID(u)), ref: strconv.FormatInt(w.ID, 10), at: now}); err != nil {
				return err
			}
		}
		followed, err := s.follow(ctx, tx, name, now, add)
		if err != nil {
			return err
		}
		w.Size, w.Opened, w.Why, w.Admitted = len(let), now, why, len(let)+followed
		_, err = tx.Exec(ctx, "update wave set size = $2 where id = $1", w.ID, len(let))
		return err
	})
	if errors.Is(err, errNobody) {
		return Wave{}, nil
	}
	return w, err
}

// Sealed is up to n people let in who left a sealed sign in, in place
// order: the ones cygnet adds herself.
func (s *Launches) Sealed(ctx context.Context, name string, n int) ([]snowflake.ID, error) {
	rows, _ := s.pool.Query(ctx, "select user_id from line where launch = $1 and admitted is not null and sealed is not null order by place limit $2", name, n)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (snowflake.ID, error) {
		var u int64
		return snowflake.ID(u), row.Scan(&u)
	})
}

// Unseal takes user's sealed sign in, once: whoever takes it is the only
// one who has it, and it's gone from the line whatever they do with it.
func (s *Launches) Unseal(ctx context.Context, name string, user snowflake.ID, now time.Time) ([]byte, error) {
	var ct []byte
	err := s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		err := tx.QueryRow(ctx, `update line l set sealed = null from (
				select launch, user_id, sealed from line where launch = $1 and user_id = $2 and sealed is not null for update) o
			where l.launch = o.launch and l.user_id = o.user_id returning o.sealed`, name, int64(user)).Scan(&ct)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotOnList
		}
		if err != nil {
			return err
		}
		return add(entry{kind: "opened", subject: s.Who(user), at: now})
	})
	return ct, err
}
