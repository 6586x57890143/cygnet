package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Launches is the campaigns cygnet runs at cygnet.lol (SPEC.md, What a
// member sees): each launch's line, the invites its people give, and the
// ledger every change to them lands in, in the same transaction. The
// ledger's key names people by a keyed hash, and chains each entry to the
// one before it.
type Launches struct {
	pool *pgxpool.Pool
	key  []byte
}

func NewLaunches(pool *pgxpool.Pool, key []byte) *Launches { return &Launches{pool: pool, key: key} }

var (
	// ErrNoLaunch: no launch by that name, or none for that server.
	ErrNoLaunch = errors.New("store: no such launch")
	// ErrTaken: the name is another server's launch, or the server has one.
	ErrTaken = errors.New("store: that name or server already has a launch")
	// ErrOnList: they are on the list already, or hold another way in.
	ErrOnList = errors.New("store: already on the list")
	// ErrNotOnList: they aren't on the list.
	ErrNotOnList = errors.New("store: not on the list")
	// ErrClosed: the launch isn't taking anyone new that way.
	ErrClosed = errors.New("store: the launch is closed")
)

// Stages a launch's campaign moves through: people get on the list while
// it's gathering, nobody new while it's closed, and once it's open the
// valve lets the list in.
const (
	StageGathering = "gathering"
	StageClosed    = "closed"
	StageOpen      = "open"
)

// Launch is one server's campaign.
type Launch struct {
	Name    string
	Guild   snowflake.ID
	Stage   string
	Opens   time.Time
	Ceiling int
	Paused  bool
}

const launchCols = "name, guild_id, stage, opens, ceiling, paused"

func scanLaunch(row pgx.Row) (Launch, error) {
	var l Launch
	var guild int64
	var opens *time.Time
	err := row.Scan(&l.Name, &guild, &l.Stage, &opens, &l.Ceiling, &l.Paused)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNoLaunch
	}
	l.Guild = snowflake.ID(guild)
	if opens != nil {
		l.Opens = *opens
	}
	return l, err
}

// Get is the launch called name.
func (s *Launches) Get(ctx context.Context, name string) (Launch, error) {
	return scanLaunch(s.pool.QueryRow(ctx, "select "+launchCols+" from launch where name = $1", name))
}

// Of is the launch guild has.
func (s *Launches) Of(ctx context.Context, guild snowflake.ID) (Launch, error) {
	return scanLaunch(s.pool.QueryRow(ctx, "select "+launchCols+" from launch where guild_id = $1", int64(guild)))
}

// Register makes name guild's launch. Registering it again is a no-op; a
// name another server has, or a second name for one server, is ErrTaken.
func (s *Launches) Register(ctx context.Context, name string, guild snowflake.ID, now time.Time) error {
	tag, err := s.pool.Exec(ctx, "insert into launch (name, guild_id, made) values ($1, $2, $3) on conflict do nothing", name, int64(guild), now)
	if err != nil || tag.RowsAffected() == 1 {
		return err
	}
	l, err := s.Get(ctx, name)
	switch {
	case errors.Is(err, ErrNoLaunch):
		return ErrTaken
	case err != nil:
		return err
	case l.Guild != guild:
		return ErrTaken
	}
	return nil
}

// Who is a Discord ID as the ledger names it.
func (s *Launches) Who(user snowflake.ID) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("user|" + user.String()))
	return m.Sum(nil)
}

// Pseudonym is the ID that stands where someone forgotten stood: from the
// ledger's name for them, so the two still match, and negative, so it is
// never a Discord ID. ponytail: 63 bits of an HMAC, a collision between two
// forgotten people about 2^-63 likely; a column of its own if that matters.
func Pseudonym(who []byte) int64 { return int64(binary.BigEndian.Uint64(who) | 1<<63) }

// Forgotten is whether id is a Pseudonym: someone who asked to be
// forgotten, in a column that held their Discord ID.
func Forgotten(id snowflake.ID) bool { return int64(id) < 0 }

// entry is one ledger line before it's chained.
type entry struct {
	kind           string
	actor, subject []byte
	ref            string
	at             time.Time
}

// mac is an entry's place in its launch's chain: keyed over the one before
// it, then the entry.
func (s *Launches) mac(prev []byte, seq int64, launch string, e entry) []byte {
	m := hmac.New(sha256.New, s.key)
	_, _ = fmt.Fprintf(m, "%x|%d|%s|%s|%x|%x|%s|%d", prev, seq, launch, e.kind, e.actor, e.subject, e.ref, e.at.UnixMicro())
	return m.Sum(nil)
}

// commit runs fn in one transaction with the launch's chain held, so every
// entry fn appends lands with the change it records, in order.
func (s *Launches) commit(ctx context.Context, launch string, fn func(tx pgx.Tx, add func(entry) error) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, "ledger ", launch); err != nil {
			return err
		}
		var prev []byte
		err := tx.QueryRow(ctx, "select mac from ledger where launch = $1 order by seq desc limit 1", launch).Scan(&prev)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return fn(tx, func(e entry) error {
			var seq int64
			if err := tx.QueryRow(ctx, "select nextval('ledger_seq_seq')").Scan(&seq); err != nil {
				return err
			}
			e.at = e.at.Truncate(time.Microsecond)
			e.actor, e.subject = append([]byte{}, e.actor...), append([]byte{}, e.subject...)
			prev = s.mac(prev, seq, launch, e)
			_, err := tx.Exec(ctx, "insert into ledger (seq, launch, kind, actor, subject, ref, at, mac) values ($1, $2, $3, $4, $5, $6, $7, $8)",
				seq, launch, e.kind, e.actor, e.subject, e.ref, e.at, prev)
			return err
		})
	})
}

// stageOf is the launch's stage, read inside its chain's turn.
func stageOf(ctx context.Context, tx pgx.Tx, launch string) (string, error) {
	var stage string
	err := tx.QueryRow(ctx, "select stage from launch where name = $1", launch).Scan(&stage)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNoLaunch
	}
	return stage, err
}

// SetStage moves a launch's campaign on, or back, by admin. The first time
// it opens is kept: it starts the clock on deleting the launch's list.
func (s *Launches) SetStage(ctx context.Context, name, stage string, admin snowflake.ID, now time.Time) error {
	return s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		tag, err := tx.Exec(ctx, `update launch set stage = $2, opens = coalesce(opens, case when $2 = 'open' then $3::timestamptz end)
			where name = $1 and stage <> $2`, name, stage, now)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return add(entry{kind: "stage", actor: s.Who(admin), ref: stage, at: now})
	})
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Join puts user at the back of the launch's line while it's gathering:
// held, with why, when they look like someone already on it. It returns
// their place.
func (s *Launches) Join(ctx context.Context, name string, user snowflake.ID, held string, sealed []byte, now time.Time) (int, error) {
	var place int
	err := s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		stage, err := stageOf(ctx, tx, name)
		if err != nil {
			return err
		}
		if stage != StageGathering {
			return ErrClosed
		}
		err = tx.QueryRow(ctx, `insert into line (launch, user_id, place, held, sealed, at)
			select $1, $2, coalesce(max(place), 0) + 1, $3, $4, $5 from line where launch = $1
			on conflict do nothing returning place`, name, int64(user), nullable(held), sealed, now).Scan(&place)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOnList
		}
		if err != nil {
			return err
		}
		ref := ""
		if held != "" {
			ref = "held"
		}
		return add(entry{kind: "join", actor: s.Who(user), subject: s.Who(user), ref: ref, at: now})
	})
	return place, err
}

// Place is where someone stands in a launch: their place, and their rank
// among those waiting on their own (0 for someone brought in by a code,
// who comes in with whoever gave it), who gave it, what holds their
// invites, how many they may give and have left, whether cygnet may add
// them herself, when their wave let them in, and the invites they made.
type Place struct {
	Place, Rank    int
	Sponsor        snowflake.ID
	Held           string
	Released, Left int
	Sealed         bool
	Admitted       time.Time
	Invites        []Code
}

// open counts owner's ($2) invites in launch ($1) at $3 that are out or
// used: one taken back, or run out unused, is back in their hands.
const open = `(select count(*) from code where launch = $1 and owner_id = $2
	and revoked is null and (spent_by is not null or expires > $3))`

// Where is where user stands in the launch at now.
func (s *Launches) Where(ctx context.Context, name string, user snowflake.ID, now time.Time) (Place, error) {
	var p Place
	var held *string
	var admitted *time.Time
	var sponsor int64
	err := s.pool.QueryRow(ctx, `select l.place, l.held, l.released, l.sealed is not null, l.admitted, coalesce(c.owner_id, 0), `+open+`
		from line l left join code c on c.hash = l.code where l.launch = $1 and l.user_id = $2`,
		name, int64(user), now).Scan(&p.Place, &held, &p.Released, &p.Sealed, &admitted, &sponsor, &p.Left)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotOnList
	}
	if err != nil {
		return p, err
	}
	p.Left = max(0, p.Released-p.Left)
	p.Sponsor = snowflake.ID(sponsor)
	if held != nil {
		p.Held = *held
	}
	if admitted != nil {
		p.Admitted = *admitted
	}
	if sponsor == 0 {
		if err := s.pool.QueryRow(ctx, "select count(*) from line where launch = $1 and code is null and place <= $2", name, p.Place).Scan(&p.Rank); err != nil {
			return p, err
		}
	}
	rows, _ := s.pool.Query(ctx, "select "+codeCols+" from code where launch = $1 and owner_id = $2 order by made desc", name, int64(user))
	p.Invites, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Code, error) { return scanCode(row) })
	return p, err
}

// Seal keeps user's sealed sign in, for cygnet to add them with when their
// wave opens, or with none forgets it. Once they're let in it can't change.
func (s *Launches) Seal(ctx context.Context, name string, user snowflake.ID, sealed []byte, now time.Time) error {
	return s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		tag, err := tx.Exec(ctx, "update line set sealed = $3 where launch = $1 and user_id = $2 and admitted is null", name, int64(user), sealed)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotOnList
		}
		kind := "seal"
		if sealed == nil {
			kind = "unseal"
		}
		return add(entry{kind: kind, actor: s.Who(user), subject: s.Who(user), at: now})
	})
}

// Release gives each invites to each of the first count people waiting on
// their own who haven't had any and aren't held: the front of the line.
// It returns how many it gave them to.
func (s *Launches) Release(ctx context.Context, name string, count, each int, admin snowflake.ID, now time.Time) (int, error) {
	n := 0
	err := s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		rows, _ := tx.Query(ctx, `update line set released = $3 where launch = $1 and user_id in (
				select user_id from line where launch = $1 and code is null and held is null and released = 0 order by place limit $2)
			returning user_id`, name, count, each)
		users, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		for _, u := range users {
			if err := add(entry{kind: "release", actor: s.Who(admin), subject: s.Who(snowflake.ID(u)), ref: strconv.Itoa(each), at: now}); err != nil {
				return err
			}
		}
		n = len(users)
		return nil
	})
	return n, err
}

// Lift lets a held person's invites be released, and says whether they
// were held.
func (s *Launches) Lift(ctx context.Context, name string, user, admin snowflake.ID, now time.Time) (bool, error) {
	lifted := false
	err := s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		tag, err := tx.Exec(ctx, "update line set held = null where launch = $1 and user_id = $2 and held is not null", name, int64(user))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		lifted = true
		return add(entry{kind: "lift", actor: s.Who(admin), subject: s.Who(user), at: now})
	})
	return lifted, err
}

// Issue keeps a new invite from user, by its code's hash, if they have one
// left to give and aren't held: ErrNoneLeft otherwise.
func (s *Launches) Issue(ctx context.Context, name string, hash []byte, user snowflake.ID, now, expires time.Time) error {
	return s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		tag, err := tx.Exec(ctx, `insert into code (hash, guild_id, owner_id, made, expires, launch)
			select $4, l.guild_id, $2, $3, $5, $1 from launch l join line m on m.launch = l.name
			where l.name = $1 and m.user_id = $2 and m.held is null and m.released > `+open,
			name, int64(user), now, hash, expires)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoneLeft
		}
		return add(entry{kind: "issue", actor: s.Who(user), ref: hex.EncodeToString(hash), at: now})
	})
}

// Revoke takes back user's invite if nobody spent it, and says whether it
// did.
func (s *Launches) Revoke(ctx context.Context, name string, hash []byte, user snowflake.ID, now time.Time) (bool, error) {
	done := false
	err := s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		tag, err := tx.Exec(ctx, "update code set revoked = $4 where hash = $2 and launch = $1 and owner_id = $3 and spent_by is null and revoked is null",
			name, hash, int64(user), now)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		done = true
		return add(entry{kind: "revoke", actor: s.Who(user), ref: hex.EncodeToString(hash), at: now})
	})
	return done, err
}

// Vouch spends a launch's invite for user and puts them on its line under
// whoever gave it, in one transaction: the invite is spent only if they
// get on, and they get on only if it was theirs to spend. Vouching again
// with the invite they spent returns its launch, so the Worker can retry.
// It refuses with ErrGone (no such invite, or it's spent, taken back, run
// out, or its giver left), ErrOwn, ErrOnList or ErrClosed.
func (s *Launches) Vouch(ctx context.Context, hash []byte, user snowflake.ID, held string, now time.Time) (string, error) {
	c, err := (&Codes{s.pool}).Find(ctx, hash)
	if err != nil {
		return "", err
	}
	if c.Launch == "" {
		return "", ErrGone
	}
	return c.Launch, s.commit(ctx, c.Launch, func(tx pgx.Tx, add func(entry) error) error {
		var by []byte
		err := tx.QueryRow(ctx, "select coalesce(code, '') from line where launch = $1 and user_id = $2", c.Launch, int64(user)).Scan(&by)
		switch {
		case err == nil && hmac.Equal(by, hash):
			return nil
		case err == nil:
			return ErrOnList
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		switch stage, err := stageOf(ctx, tx, c.Launch); {
		case err != nil:
			return err
		case stage == StageClosed:
			return ErrClosed
		}
		var owner int64
		err = tx.QueryRow(ctx, `update code set spent_by = $3, spent_at = $4
			where hash = $1 and launch = $2 and spent_by is null and revoked is null and expires > $4 and owner_id <> $3
			and exists (select 1 from line where launch = $2 and user_id = code.owner_id)
			returning owner_id`, hash, c.Launch, int64(user), now).Scan(&owner)
		switch pe, ok := errors.AsType[*pgconn.PgError](err); {
		// 23505: they hold a way into the server already, by another code.
		case ok && pe.Code == "23505":
			return ErrOnList
		case errors.Is(err, pgx.ErrNoRows) && c.Owner == user:
			return ErrOwn
		case errors.Is(err, pgx.ErrNoRows):
			return ErrGone
		case err != nil:
			return err
		}
		if _, err := tx.Exec(ctx, `insert into line (launch, user_id, place, code, held, at)
			select $1, $2, coalesce(max(place), 0) + 1, $3, $4, $5 from line where launch = $1`,
			c.Launch, int64(user), hash, nullable(held), now); err != nil {
			return err
		}
		return add(entry{kind: "redeem", actor: s.Who(user), subject: s.Who(snowflake.ID(owner)), ref: hex.EncodeToString(hash), at: now})
	})
}

// Leave takes user off the launch's list and back every invite of theirs
// nobody used, and leaves nothing that names them: where their ID stood in
// the record of who vouched for whom it becomes their Pseudonym. Whoever
// they brought in stays, under that.
func (s *Launches) Leave(ctx context.Context, name string, user snowflake.ID, now time.Time) error {
	return s.commit(ctx, name, func(tx pgx.Tx, add func(entry) error) error {
		tag, err := tx.Exec(ctx, "delete from line where launch = $1 and user_id = $2", name, int64(user))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		who := s.Who(user)
		p := Pseudonym(who)
		for _, q := range []struct {
			sql string
			arg any
		}{
			{"update code set revoked = $3 where launch = $1 and owner_id = $2 and spent_by is null and revoked is null", now},
			{"update code set owner_id = $3 where launch = $1 and owner_id = $2", p},
			{"update code set spent_by = $3 where launch = $1 and spent_by = $2", p},
		} {
			if _, err := tx.Exec(ctx, q.sql, name, int64(user), q.arg); err != nil {
				return err
			}
		}
		return add(entry{kind: "leave", actor: who, subject: who, at: now})
	})
}

// Mark is one ledger entry about someone, as they may read it: what
// happened, when, and whether they did it.
type Mark struct {
	Kind  string
	At    time.Time
	ByYou bool
}

// History is every ledger entry in the launch about user.
func (s *Launches) History(ctx context.Context, name string, user snowflake.ID) ([]Mark, error) {
	rows, _ := s.pool.Query(ctx, "select kind, at, actor = $2 from ledger where launch = $1 and (actor = $2 or subject = $2) order by seq", name, s.Who(user))
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Mark, error) {
		var m Mark
		return m, row.Scan(&m.Kind, &m.At, &m.ByYou)
	})
}

// Row is one person on a launch's list, for its admins.
type Row struct {
	User, Sponsor   snowflake.ID
	Place, Released int
	Held            string
	At, Admitted    time.Time
}

// List is everyone on the launch's list, in place order.
func (s *Launches) List(ctx context.Context, name string) ([]Row, error) {
	rows, _ := s.pool.Query(ctx, `select l.user_id, coalesce(c.owner_id, 0), l.place, l.released, coalesce(l.held, ''), l.at, l.admitted
		from line l left join code c on c.hash = l.code where l.launch = $1 order by l.place`, name)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Row, error) {
		var r Row
		var user, sponsor int64
		var admitted *time.Time
		err := row.Scan(&user, &sponsor, &r.Place, &r.Released, &r.Held, &r.At, &admitted)
		r.User, r.Sponsor = snowflake.ID(user), snowflake.ID(sponsor)
		if admitted != nil {
			r.Admitted = *admitted
		}
		return r, err
	})
}

// Stats is a launch's campaign in numbers.
type Stats struct {
	Total, InLine, ThroughInvites, Held, ReleasedTo, Waiting, Admitted int
	InvitesMade, InvitesUsed, InvitesOpen                              int
}

// Count is the launch's campaign in numbers at now.
func (s *Launches) Count(ctx context.Context, name string, now time.Time) (Stats, error) {
	var st Stats
	err := s.pool.QueryRow(ctx, `select count(*),
			count(*) filter (where code is null),
			count(*) filter (where code is not null),
			count(*) filter (where held is not null),
			count(*) filter (where released > 0),
			count(*) filter (where code is null and held is null and released = 0),
			count(*) filter (where admitted is not null),
			(select count(*) from code where launch = $1),
			(select count(*) from code where launch = $1 and spent_by is not null),
			(select count(*) from code where launch = $1 and spent_by is null and revoked is null and expires > $2)
		from line where launch = $1`, name, now).Scan(
		&st.Total, &st.InLine, &st.ThroughInvites, &st.Held, &st.ReleasedTo, &st.Waiting, &st.Admitted,
		&st.InvitesMade, &st.InvitesUsed, &st.InvitesOpen)
	return st, err
}

// Verified is what a walk of a launch's ledger found: how many entries,
// the first whose mac doesn't follow from the one before (0 for none), and
// every count the list holds that the ledger doesn't account for.
type Verified struct {
	Entries    int
	Broken     int64
	Mismatches []string
}

// Verify walks the launch's ledger and checks every mac against the one
// before it, then that each person's released invites and each giver's
// invites match what the ledger says happened. Anything edited, dropped or
// added outside this code shows up as a broken seq or a mismatch.
func (s *Launches) Verify(ctx context.Context, name string) (Verified, error) {
	var v Verified
	rows, _ := s.pool.Query(ctx, "select seq, kind, actor, subject, ref, at, mac from ledger where launch = $1 order by seq", name)
	released := map[string]int{}
	issued := map[string]int{}
	pseudo := map[int64]string{}
	var prev []byte
	for rows.Next() {
		var seq int64
		var e entry
		var mac []byte
		if err := rows.Scan(&seq, &e.kind, &e.actor, &e.subject, &e.ref, &e.at, &mac); err != nil {
			return v, err
		}
		v.Entries++
		if v.Broken != 0 {
			continue
		}
		if !hmac.Equal(mac, s.mac(prev, seq, name, e)) {
			v.Broken = seq
			continue
		}
		prev = mac
		switch e.kind {
		case "release":
			released[string(e.subject)], _ = strconv.Atoi(e.ref)
		case "issue":
			issued[string(e.actor)]++
			pseudo[Pseudonym(e.actor)] = string(e.actor)
		}
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	people, err := s.List(ctx, name)
	if err != nil {
		return v, err
	}
	for _, p := range people {
		if released[string(s.Who(p.User))] != p.Released {
			v.Mismatches = append(v.Mismatches, fmt.Sprintf("released for place %d", p.Place))
		}
	}
	rows, _ = s.pool.Query(ctx, "select owner_id, count(*) from code where launch = $1 group by owner_id", name)
	seen := map[string]bool{}
	for rows.Next() {
		var owner int64
		var n int
		if err := rows.Scan(&owner, &n); err != nil {
			return v, err
		}
		h := pseudo[owner]
		if owner > 0 {
			h = string(s.Who(snowflake.ID(owner)))
		}
		seen[h] = true
		if issued[h] != n {
			v.Mismatches = append(v.Mismatches, fmt.Sprintf("invites for %x", h))
		}
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	for h := range issued {
		if !seen[h] {
			v.Mismatches = append(v.Mismatches, fmt.Sprintf("invites for %x", h))
		}
	}
	return v, nil
}

// Expire deletes the list, the unused invites and the ledger of every
// launch that opened more than keep before now. The launch, its waves and
// the invites someone came into the server by stay: those are the server's
// record of who vouched for whom.
func (s *Launches) Expire(ctx context.Context, now time.Time, keep time.Duration) error {
	rows, _ := s.pool.Query(ctx, "select name from launch where opens < $1 and name not in (select launch from gone)", now.Add(-keep))
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, name := range names {
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "insert into gone (launch, at) values ($1, $2)", name, now); err != nil {
				return err
			}
			for _, q := range []string{
				"delete from ledger where launch = $1",
				"delete from line where launch = $1",
				"delete from code where launch = $1 and not exists (select 1 from joined where joined.code = code.hash)",
			} {
				if _, err := tx.Exec(ctx, q, name); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
