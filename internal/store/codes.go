package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Codes is the links members make for their servers and the joins they
// cause (SPEC.md, Membership), kept by their SHA-256: a code itself is
// never stored.
type Codes struct{ pool *pgxpool.Pool }

func NewCodes(pool *pgxpool.Pool) *Codes { return &Codes{pool: pool} }

var (
	// ErrNoneLeft: the member has as many links out or used as they may.
	ErrNoneLeft = errors.New("store: no links left to make")
	// ErrGone: no such code, or it was spent, taken back or ran out.
	ErrGone = errors.New("store: the code can't be spent")
	// ErrOwn: the code's owner tried to spend it.
	ErrOwn = errors.New("store: a code's owner can't spend it")
)

// Code is one link. The zero time and ID mean not yet: not spent, not taken
// back, nobody arrived.
type Code struct {
	Hash                            []byte
	Guild, Owner, SpentBy           snowflake.ID
	Made, Expires, SpentAt, Revoked time.Time
	Arrived                         time.Time
}

// Open is whether c can still be spent at now.
func (c Code) Open(now time.Time) bool {
	return c.SpentBy == 0 && c.Revoked.IsZero() && now.Before(c.Expires)
}

const codeCols = "hash, guild_id, owner_id, made, expires, spent_by, spent_at, revoked, arrived"

func scanCode(row pgx.Row) (Code, error) {
	var c Code
	var guild, owner int64
	var by *int64
	var spent, revoked, arrived *time.Time
	err := row.Scan(&c.Hash, &guild, &owner, &c.Made, &c.Expires, &by, &spent, &revoked, &arrived)
	c.Guild, c.Owner = snowflake.ID(guild), snowflake.ID(owner)
	if by != nil {
		c.SpentBy = snowflake.ID(*by)
	}
	for _, t := range []struct {
		dst *time.Time
		src *time.Time
	}{{&c.SpentAt, spent}, {&c.Revoked, revoked}, {&c.Arrived, arrived}} {
		if t.src != nil {
			*t.dst = *t.src
		}
	}
	return c, err
}

// counted is how many links owner ($2) has out or used in guild ($1) at
// $3: one taken back, or run out unused, is back in their hands.
const counted = `(select count(*) from code where guild_id = $1 and owner_id = $2
	and revoked is null and (spent_by is not null or expires > $3))`

// lock takes a transaction's turn on one key, made of parts: a mint's
// owner, an arrival's member.
func lock(ctx context.Context, tx pgx.Tx, parts ...any) error {
	_, err := tx.Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1, 0))", fmt.Sprint(parts...))
	return err
}

// Mint keeps a new code for owner in guild, unless they already have limit
// links out or used. A negative limit is no limit. Two mints at once take
// turns on the owner, so neither can slip past it.
func (s *Codes) Mint(ctx context.Context, hash []byte, guild, owner snowflake.ID, now, expires time.Time, limit int) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, "mint ", guild, " ", owner); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `insert into code (hash, guild_id, owner_id, made, expires)
			select $4, $1, $2, $3, $5 where $6 < 0 or `+counted+` < $6`,
			int64(guild), int64(owner), now, hash, expires, limit)
		if err == nil && tag.RowsAffected() == 0 {
			err = ErrNoneLeft
		}
		return err
	})
}

// Left is how many more links owner may make in guild under limit: -1 for
// no limit, and 0 when it can't be read.
func (s *Codes) Left(ctx context.Context, guild, owner snowflake.ID, now time.Time, limit int) (int, error) {
	if limit < 0 {
		return -1, nil
	}
	var n int
	if err := s.pool.QueryRow(ctx, "select "+counted, int64(guild), int64(owner), now).Scan(&n); err != nil {
		return 0, err
	}
	return max(0, limit-n), nil
}

// Mine is owner's last n codes in guild, newest first.
func (s *Codes) Mine(ctx context.Context, guild, owner snowflake.ID, n int) ([]Code, error) {
	rows, _ := s.pool.Query(ctx, "select "+codeCols+" from code where guild_id = $1 and owner_id = $2 order by made desc limit $3",
		int64(guild), int64(owner), n)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Code, error) { return scanCode(row) })
}

// Find is the code hash names, or ErrGone.
func (s *Codes) Find(ctx context.Context, hash []byte) (Code, error) {
	c, err := scanCode(s.pool.QueryRow(ctx, "select "+codeCols+" from code where hash = $1", hash))
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrGone
	}
	return c, err
}

// Revoke takes back owner's code if nobody spent it, and says whether it
// did.
func (s *Codes) Revoke(ctx context.Context, hash []byte, owner snowflake.ID, now time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, "update code set revoked = $3 where hash = $1 and owner_id = $2 and spent_by is null and revoked is null",
		hash, int64(owner), now)
	return tag.RowsAffected() == 1, err
}

// Spend spends the code for user, in one conditional UPDATE: however many
// tabs race for a code, one wins. It is the idempotency key of a
// redemption: spending a code user already spent returns it again, so a
// retry, or a second go after an invite they never used, finds it theirs.
// Someone holds one way into a server at a time: while a code they spent
// there waits for them to arrive, spending another spends nothing and
// returns that one, so nobody uses up two members' links to get in once.
func (s *Codes) Spend(ctx context.Context, hash []byte, user snowflake.ID, now time.Time) (Code, error) {
	c, err := scanCode(s.pool.QueryRow(ctx, `update code set spent_by = $2, spent_at = $3
		where hash = $1 and spent_by is null and revoked is null and expires > $3 and owner_id <> $2
		and not exists (select 1 from code p where p.guild_id = code.guild_id and p.spent_by = $2 and p.arrived is null)
		returning `+codeCols, hash, int64(user), now))
	// 23505, unique_violation: two first spends into one server at once.
	pe, unique := errors.AsType[*pgconn.PgError](err)
	if unique = unique && pe.Code == "23505"; !unique && !errors.Is(err, pgx.ErrNoRows) {
		return c, err
	}
	c, err = s.Find(ctx, hash)
	if err != nil {
		return Code{}, err
	}
	held, err := scanCode(s.pool.QueryRow(ctx, "select "+codeCols+" from code where guild_id = $1 and spent_by = $2 and arrived is null",
		int64(c.Guild), int64(user)))
	switch {
	case err == nil:
		return held, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return Code{}, err
	case c.SpentBy == user:
		return c, nil
	case c.Owner == user:
		return Code{}, ErrOwn
	}
	return Code{}, ErrGone
}

// Arrive records user joining guild at at, caused by the code they spent
// there and haven't arrived by yet, or untraced if there is none: the
// cause is read, never guessed. A member already recorded as in keeps
// their row, so a direct add and its gateway event record one join; a code
// they spent meanwhile is marked arrived, so it isn't pending forever. It
// returns who vouched for them, or 0.
func (s *Codes) Arrive(ctx context.Context, guild, user snowflake.ID, at time.Time) (snowflake.ID, error) {
	var sponsor *int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// A direct add and its gateway event arrive at once: one at a time,
		// or the second would read the first as not in yet.
		if err := lock(ctx, tx, "arrive ", guild, " ", user); err != nil {
			return err
		}
		var in bool
		if err := tx.QueryRow(ctx, "select exists (select 1 from joined where guild_id = $1 and user_id = $2 and left_at is null)",
			int64(guild), int64(user)).Scan(&in); err != nil {
			return err
		}
		if in {
			_, err := tx.Exec(ctx, "update code set arrived = $3 where guild_id = $1 and spent_by = $2 and arrived is null",
				int64(guild), int64(user), at)
			return err
		}
		var hash []byte
		err := tx.QueryRow(ctx, `update code set arrived = $3 where hash = (
				select hash from code where guild_id = $1 and spent_by = $2 and arrived is null
				order by spent_at desc limit 1 for update)
			returning hash, owner_id`, int64(guild), int64(user), at).Scan(&hash, &sponsor)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `insert into joined (guild_id, user_id, at, code, sponsor) values ($1, $2, $3, $4, $5)
			on conflict (guild_id, user_id) do update set at = excluded.at, code = excluded.code, sponsor = excluded.sponsor, left_at = null`,
			int64(guild), int64(user), at, hash, sponsor)
		return err
	})
	if err != nil || sponsor == nil {
		return 0, err
	}
	return snowflake.ID(*sponsor), nil
}

// Leave records user leaving guild. Their row stays: the tree is the
// record of who vouched for whom.
func (s *Codes) Leave(ctx context.Context, guild, user snowflake.ID, at time.Time) error {
	_, err := s.pool.Exec(ctx, "update joined set left_at = $3 where guild_id = $1 and user_id = $2 and left_at is null",
		int64(guild), int64(user), at)
	return err
}

// Pending is every code spent since since by someone not yet seen
// arriving: the reconcile sweep's list, for joins missed while cygnet was
// away. The window keeps it short, and there is at most one per person and
// server.
func (s *Codes) Pending(ctx context.Context, since time.Time) ([]Code, error) {
	rows, _ := s.pool.Query(ctx, "select "+codeCols+" from code where spent_by is not null and arrived is null and spent_at > $1 order by spent_at", since)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Code, error) { return scanCode(row) })
}

// Join is one member's line in the tree: when they joined, who vouched for
// them (0 for a join cygnet can't trace) and when they left, if they did.
type Join struct {
	User, Sponsor snowflake.ID
	At, Left      time.Time
}

// Tree is everyone cygnet saw join guild, in the order they joined.
func (s *Codes) Tree(ctx context.Context, guild snowflake.ID) ([]Join, error) {
	rows, _ := s.pool.Query(ctx, "select user_id, coalesce(sponsor, 0), at, left_at from joined where guild_id = $1 order by at, user_id", int64(guild))
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Join, error) {
		var j Join
		var user, sponsor int64
		var left *time.Time
		err := row.Scan(&user, &sponsor, &j.At, &left)
		j.User, j.Sponsor = snowflake.ID(user), snowflake.ID(sponsor)
		if left != nil {
			j.Left = *left
		}
		return j, err
	})
}

// Each is how many links each member of guild may have out or used; 0
// when its admins haven't said.
func (s *Codes) Each(ctx context.Context, guild snowflake.ID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, "select each from invites where guild_id = $1", int64(guild)).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// SetEach keeps how many links each member of guild may have.
func (s *Codes) SetEach(ctx context.Context, guild snowflake.ID, n int) error {
	_, err := s.pool.Exec(ctx, "insert into invites (guild_id, each) values ($1, $2) on conflict (guild_id) do update set each = excluded.each",
		int64(guild), n)
	return err
}
