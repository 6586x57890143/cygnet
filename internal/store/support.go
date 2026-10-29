package store

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Support is who backs the birds (docs/support.md): the lengths and rates
// on offer, each checkout with the exact amount that pays it, every
// transfer seen arrive, and the periods those bought. cygnet holds no
// wallet key; this is only the record.
type Support struct {
	pool *pgxpool.Pool
}

func NewSupport(pool *pgxpool.Pool) *Support { return &Support{pool: pool} }

var (
	// ErrNotOffered: that length or rate isn't on offer.
	ErrNotOffered = errors.New("store: that length or rate isn't on offer")
	// ErrTooMany: they have as many live checkouts as one person may.
	ErrTooMany = errors.New("store: too many checkouts open")
	// ErrNoInvoice: no checkout by that id.
	ErrNoInvoice = errors.New("store: no such checkout")
	// ErrNoAmount: no free amount was found on that rail; the rail is busy
	// beyond what its change can tell apart.
	ErrNoAmount = errors.New("store: no free amount on that rail")
)

// Invoice states.
const (
	InvoiceOpen   = "open"
	InvoiceLapsed = "lapsed"
	InvoicePaid   = "paid"
	InvoiceVoid   = "void"
)

// openPerUser is how many open checkouts one person may hold: enough for a
// change of mind between rails. heldPerUser caps those and the lapsed ones
// together, which keep their amounts for the grace: too few to tie up a
// rail's amounts, enough that a few abandoned checkouts never lock anyone
// out.
const (
	openPerUser = 3
	heldPerUser = 9
)

// Plan is one length on offer: months, and what it costs at the monthly
// rate, in hundredths of a month.
type Plan struct {
	Months, Worth int
}

// Offer is what can be bought: the lengths and the monthly rates, in US
// cents, both in ascending order. Rates[0] is the plain supporter.
type Offer struct {
	Plans []Plan
	Rates []int
}

// Price is what months cost at cents a month, in cents.
func (p Plan) Price(cents int) int { return cents * p.Worth / 100 }

// Offer reads what is on offer now.
func (s *Support) Offer(ctx context.Context) (Offer, error) {
	var o Offer
	rows, err := s.pool.Query(ctx, "select months, worth from support_plan where offered order by months")
	if err != nil {
		return o, err
	}
	o.Plans, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Plan, error) {
		var p Plan
		return p, r.Scan(&p.Months, &p.Worth)
	})
	if err != nil {
		return o, err
	}
	rows, err = s.pool.Query(ctx, "select cents from support_rate where offered order by cents")
	if err != nil {
		return o, err
	}
	o.Rates, err = pgx.CollectRows(rows, pgx.RowTo[int])
	return o, err
}

// Invoice is one checkout.
type Invoice struct {
	ID          string
	Destination string
	User        snowflake.ID
	Months      int
	// Cents is the monthly rate, Price what the whole length costs, in US
	// cents. Gold is paying more than the plain rate.
	Cents, Price int
	Gold         bool
	Rail         string
	// Amount is the exact amount that pays it, in the rail's smallest unit.
	Amount *big.Int
	// Quote is the USD price a volatile asset was quoted at, or "".
	Quote         string
	Made, Expires time.Time
	State         string
	Paid          time.Time
	// PeriodEnds is when what it bought ends, once it's paid.
	PeriodEnds time.Time
}

const invoiceCols = "i.id, i.user_id, i.months, i.cents, i.price, i.gold, i.rail, i.amount::text, coalesce(i.quote, ''), i.made, i.expires, i.state, i.paid, p.ends, i.destination"

func scanInvoice(row pgx.Row) (Invoice, error) {
	var i Invoice
	var user int64
	var amount string
	var paid, ends *time.Time
	err := row.Scan(&i.ID, &user, &i.Months, &i.Cents, &i.Price, &i.Gold, &i.Rail, &amount, &i.Quote, &i.Made, &i.Expires, &i.State, &paid, &ends, &i.Destination)
	if errors.Is(err, pgx.ErrNoRows) {
		return i, ErrNoInvoice
	}
	if err != nil {
		return i, err
	}
	i.User = snowflake.ID(user)
	i.Amount, _ = new(big.Int).SetString(amount, 10)
	if paid != nil {
		i.Paid = *paid
	}
	if ends != nil {
		i.PeriodEnds = *ends
	}
	return i, nil
}

// Ask is a checkout someone wants: who, for how long, at which monthly
// rate, on which rail.
type Ask struct {
	User        snowflake.ID
	Destination string
	Months      int
	Cents       int
	Rail        string
}

// Amount picks the exact amount a checkout of price cents asks for: the
// price with change no other live checkout on its rail has, and the USD
// price it was quoted at for a volatile asset. Each call picks again, so a
// taken amount is retried with another.
type Amount func(price int) (*big.Int, string, error)

// amountTries is how many fresh amounts a quote tries before giving up.
// With 9999 kinds of change on a stablecoin rail and a handful live at
// once, the second try already all but always lands.
const amountTries = 8

// reusableQuery is the open checkout a quote hands back instead of a new
// one: the same thing, with at least half its life left.
const reusableQuery = "select " + invoiceCols + ` from support_invoice i left join support_period p on p.invoice = i.id
	where i.user_id = $1 and i.months = $2 and i.cents = $3 and i.rail = $4 and i.state = 'open' and i.expires > $5
	order by i.made desc limit 1`

// Reusable is the checkout Quote would hand back for a without making a
// new one, or ErrNoInvoice: asked first, so a quote that needs a price
// reads one only when it will use it.
func (s *Support) Reusable(ctx context.Context, a Ask, now time.Time, life time.Duration) (Invoice, error) {
	return scanInvoice(s.pool.QueryRow(ctx, reusableQuery, int64(a.User), a.Months, a.Cents, a.Rail, now.Add(life/2)))
}

// Quote opens a checkout for a, priced from what is on offer, with an
// amount from amount, good until life from now. Someone who already has a
// live one for the same thing, with at least half its life left, gets that
// one back instead of a second.
func (s *Support) Quote(ctx context.Context, a Ask, now time.Time, life time.Duration, amount Amount) (Invoice, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invoice{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// One person's quotes take turns, so two tabs can't both slip under
	// the limit.
	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1, 0))", "support|"+a.User.String()); err != nil {
		return Invoice{}, err
	}
	var worth, plain int
	err = tx.QueryRow(ctx, `select p.worth, (select min(cents) from support_rate where offered)
		from support_plan p, support_rate r
		where p.months = $1 and p.offered and r.cents = $2 and r.offered`, a.Months, a.Cents).Scan(&worth, &plain)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrNotOffered
	}
	if err != nil {
		return Invoice{}, err
	}
	if a.Cents*worth%100 != 0 {
		return Invoice{}, fmt.Errorf("%w: %d months at %d cents isn't a whole number of cents", ErrNotOffered, a.Months, a.Cents)
	}
	price := Plan{a.Months, worth}.Price(a.Cents)

	same, err := scanInvoice(tx.QueryRow(ctx, reusableQuery, int64(a.User), a.Months, a.Cents, a.Rail, now.Add(life/2)))
	switch {
	case err == nil:
		return same, tx.Commit(ctx)
	case !errors.Is(err, ErrNoInvoice):
		return Invoice{}, err
	}
	var open int
	var held int
	if err := tx.QueryRow(ctx, `select count(*) filter (where state = 'open' and expires > $2), count(*)
		from support_invoice where user_id = $1 and state in ('open', 'lapsed')`, int64(a.User), now).Scan(&open, &held); err != nil {
		return Invoice{}, err
	}
	if open >= openPerUser || held >= heldPerUser {
		return Invoice{}, ErrTooMany
	}

	id, err := invoiceID()
	if err != nil {
		return Invoice{}, err
	}
	for range amountTries {
		amt, quote, err := amount(price)
		if err != nil {
			return Invoice{}, err
		}
		// Each try in its own savepoint: a taken amount fails only that
		// insert, and the transaction carries on to the next try.
		sp, err := tx.Begin(ctx)
		if err != nil {
			return Invoice{}, err
		}
		_, err = sp.Exec(ctx, `insert into support_invoice (id, user_id, months, cents, price, gold, rail, amount, quote, made, expires, destination)
			values ($1, $2, $3, $4, $5, $6, $7, $8::numeric, nullif($9, ''), $10, $11, $12)`,
			id, int64(a.User), a.Months, a.Cents, price, a.Cents > plain, a.Rail, amt.String(), quote, now, now.Add(life), a.Destination)
		if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "23505" && pe.ConstraintName == "support_invoice_amount" {
			_ = sp.Rollback(ctx)
			continue
		}
		if err != nil {
			return Invoice{}, err
		}
		if err := sp.Commit(ctx); err != nil {
			return Invoice{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Invoice{}, err
		}
		return s.Invoice(ctx, id)
	}
	return Invoice{}, ErrNoAmount
}

// invoiceID is a checkout's public name: 16 random bytes, lowercase
// base32, 26 characters. It is in the checkout's URL, so it can't be
// guessed from another one.
func invoiceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])), nil
}

// Invoice is the checkout called id, with when what it bought ends once
// it is paid.
func (s *Support) Invoice(ctx context.Context, id string) (Invoice, error) {
	return scanInvoice(s.pool.QueryRow(ctx, "select "+invoiceCols+" from support_invoice i left join support_period p on p.invoice = i.id where i.id = $1", id))
}

// Payment is one transfer seen arrive on a rail: its transaction and its
// place in it, the amount in the rail's smallest unit, and who sent it as
// the chain names them.
type Payment struct {
	Rail   string
	Event  string
	To     string
	Tx     string
	Idx    int
	Amount *big.Int
	Sender string
	// Sent is when the chain took it, where known; Seen is when cygnet
	// read it.
	Sent, Seen time.Time
}

// Period is a stretch someone backs the birds for.
type Period struct {
	User         snowflake.ID
	Starts, Ends time.Time
	Gold         bool
}

// Recorded is what recording a payment did: whether it was new, and if it
// paid a checkout, which, and the period it bought.
type Recorded struct {
	New     bool
	Invoice Invoice
	Period  Period
}

// Paid is whether recording paid a checkout.
func (r Recorded) Paid() bool { return r.Invoice.ID != "" }

// Record keeps a payment, once: one seen again is not new and changes
// nothing. One whose amount is a live checkout's on its rail pays it and
// buys its period, after any the person already has, in the same
// transaction, so a payment and what it bought can't come apart.
func (s *Support) Record(ctx context.Context, p Payment) (Recorded, error) {
	var out Recorded
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id *string
	var user int64
	var months int
	var gold bool
	// A quoted (volatile) checkout pays only for a payment the chain took
	// within VolatileSlack of its time, however late cygnet reads it; one
	// sent later is kept, unmatched, for the owner. Without a chain time,
	// when cygnet read it stands in.
	sent := p.Sent
	if sent.IsZero() {
		sent = p.Seen
	}
	err = tx.QueryRow(ctx, `select id, user_id, months, gold from support_invoice
		where rail = $1 and amount = $2::numeric and state in ('open', 'lapsed')
		and (quote is null or expires > $3)
		and (destination = '' or destination = $4)
		and made <= $5 for update`, p.Rail, p.Amount.String(), sent.Add(-VolatileSlack), p.To, sent).Scan(&id, &user, &months, &gold)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		id = nil
	case err != nil:
		return out, err
	}
	tag, err := tx.Exec(ctx, `insert into support_payment (rail, tx, idx, amount, sender, seen, invoice, event)
		values ($1, $2, $3, $4::numeric, $5, $6, $7, $8) on conflict do nothing`, p.Rail, p.Tx, p.Idx, p.Amount.String(), p.Sender, p.Seen, id, p.Event)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() == 0 {
		return out, nil
	}
	out.New = true
	if id == nil {
		return out, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, "update support_invoice set state = 'paid', paid = $2 where id = $1", *id, p.Seen); err != nil {
		return out, err
	}
	period, err := s.extend(ctx, tx, snowflake.ID(user), months, gold, p.Seen, id, 0)
	if err != nil {
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, err
	}
	out.Period = period
	out.Invoice, err = s.Invoice(ctx, *id)
	return out, err
}

// extend adds months to user's backing, from the end of what they have or
// from now, whichever is later: paid by invoice, or given by gift.
func (s *Support) extend(ctx context.Context, tx pgx.Tx, user snowflake.ID, months int, gold bool, now time.Time, invoice *string, gift snowflake.ID) (Period, error) {
	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1, 0))", "support|"+user.String()); err != nil {
		return Period{}, err
	}
	p := Period{User: user, Gold: gold}
	var giver *int64
	if gift != 0 {
		g := int64(gift)
		giver = &g
	}
	err := tx.QueryRow(ctx, `insert into support_period (user_id, starts, ends, gold, invoice, gift, made)
		select $1, s, s + make_interval(months => $2), $3, $4, $5, $6
		from (select greatest($6::timestamptz, coalesce(max(ends), $6)) as s from support_period where user_id = $1) last
		returning starts, ends`, int64(user), months, gold, invoice, giver, now).Scan(&p.Starts, &p.Ends)
	return p, err
}

// Gift gives user months of backing from the owner (by), after any they
// have: for a payment that matched no checkout, or a thank you.
func (s *Support) Gift(ctx context.Context, user snowflake.ID, months int, gold bool, by snowflake.ID, now time.Time) (Period, error) {
	if months < 1 || months > 36 {
		return Period{}, ErrNotOffered
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Period{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	p, err := s.extend(ctx, tx, user, months, gold, now, nil, by)
	if err != nil {
		return p, err
	}
	return p, tx.Commit(ctx)
}

// VolatileSlack is how long past its time a quoted (volatile) checkout
// takes a payment, by when the chain took it: enough for one sent in its
// last minute to finalize, not the two days in which a quote would be a
// free bet on the price.
const VolatileSlack = 10 * time.Minute

// Sweep lapses checkouts past their time, and voids lapsed ones grace
// after that, which frees their amounts for new checkouts. A quoted one
// keeps its amount for the grace too, so a payment sent in time but read
// late still finds it.
func (s *Support) Sweep(ctx context.Context, now time.Time, grace time.Duration) error {
	_, err := s.pool.Exec(ctx, `update support_invoice set state = case when expires <= $2 then 'void' else 'lapsed' end
		where state in ('open', 'lapsed') and expires <= $1 and (state = 'open' or expires <= $2)`, now, now.Add(-grace))
	return err
}

// Status is someone's backing: whether it covers now and in gold, when it
// first started, and when what they have runs out.
type Status struct {
	Active, Gold bool
	Since, Until time.Time
}

// Status is user's backing at now.
func (s *Support) Status(ctx context.Context, user snowflake.ID, now time.Time) (Status, error) {
	var st Status
	var since, until *time.Time
	var gold *bool
	err := s.pool.QueryRow(ctx, `select min(starts), max(ends),
		(select gold from support_period where user_id = $1 and starts <= $2 and ends > $2 order by gold desc limit 1)
		from support_period where user_id = $1`, int64(user), now).Scan(&since, &until, &gold)
	if err != nil || since == nil {
		return st, err
	}
	st.Since, st.Until = *since, *until
	st.Active = gold != nil
	st.Gold = gold != nil && *gold
	return st, nil
}

// Supporter is one person backing the birds now.
type Supporter struct {
	User  snowflake.ID
	Gold  bool
	Until time.Time
}

// Supporters is everyone backing the birds at now, in order of who has
// backed them longest.
func (s *Support) Supporters(ctx context.Context, now time.Time) ([]Supporter, error) {
	rows, err := s.pool.Query(ctx, `select user_id, bool_or(gold and starts <= $1 and ends > $1), max(ends) from support_period
		where user_id in (select user_id from support_period where starts <= $1 and ends > $1)
		group by user_id order by min(starts), user_id`, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Supporter, error) {
		var x Supporter
		var user int64
		err := r.Scan(&user, &x.Gold, &x.Until)
		x.User = snowflake.ID(user)
		return x, err
	})
}

// Unmatched is the latest payments that paid no checkout, newest first.
func (s *Support) Unmatched(ctx context.Context, limit int) ([]Payment, error) {
	rows, err := s.pool.Query(ctx, `select rail, tx, idx, amount::text, sender, seen from support_payment
		where invoice is null order by seen desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Payment, error) {
		var p Payment
		var amount string
		err := r.Scan(&p.Rail, &p.Tx, &p.Idx, &amount, &p.Sender, &p.Seen)
		p.Amount, _ = new(big.Int).SetString(amount, 10)
		return p, err
	})
}

// Received is what arrived on each rail since, matched or not, in each
// rail's smallest unit.
func (s *Support) Received(ctx context.Context, since time.Time) (map[string]*big.Int, error) {
	rows, err := s.pool.Query(ctx, "select rail, sum(amount)::text from support_payment where seen >= $1 group by rail", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*big.Int{}
	for rows.Next() {
		var rail, sum string
		if err := rows.Scan(&rail, &sum); err != nil {
			return nil, err
		}
		out[rail], _ = new(big.Int).SetString(sum, 10)
	}
	return out, rows.Err()
}

// Cursor is where reading one watched account resumes.
type Cursor struct {
	Key   string
	At    string
	Since time.Time
	Read  time.Time
	Err   string
}

// Cursor is key's cursor, started at now if it has none: nothing from
// before the first start is read.
func (s *Support) Cursor(ctx context.Context, key string, now time.Time) (Cursor, error) {
	c := Cursor{Key: key}
	var read *time.Time
	err := s.pool.QueryRow(ctx, `insert into support_cursor (key, since) values ($1, $2)
		on conflict (key) do update set key = excluded.key returning at, since, read, err`, key, now).Scan(&c.At, &c.Since, &read, &c.Err)
	if read != nil {
		c.Read = *read
	}
	return c, err
}

// Advance moves key's cursor on to at after a read at now, and clears its
// error.
func (s *Support) Advance(ctx context.Context, key, at string, now time.Time) error {
	_, err := s.pool.Exec(ctx, "update support_cursor set at = $2, read = $3, err = '' where key = $1", key, at, now)
	return err
}

// Restart starts key's cursor again from since, forgetting where it was.
func (s *Support) Restart(ctx context.Context, key string, since time.Time) error {
	_, err := s.pool.Exec(ctx, "update support_cursor set at = '', since = $2 where key = $1", key, since)
	return err
}

// Failed keeps why reading key failed, for the panel, leaving where it
// resumes as it was.
func (s *Support) Failed(ctx context.Context, key, why string) error {
	_, err := s.pool.Exec(ctx, "update support_cursor set err = $2 where key = $1", key, why)
	return err
}

// ClearErrors clears the error on every cursor under prefix but keep: an
// account no longer listed has nothing left to fail.
func (s *Support) ClearErrors(ctx context.Context, prefix string, keep []string) error {
	_, err := s.pool.Exec(ctx, "update support_cursor set err = '' where starts_with(key, $1) and err <> '' and not (key = any($2))", prefix, keep)
	return err
}

// Cursors is every watched account's cursor, by key.
func (s *Support) Cursors(ctx context.Context) ([]Cursor, error) {
	rows, err := s.pool.Query(ctx, "select key, at, since, read, err from support_cursor order by key")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Cursor, error) {
		var c Cursor
		var read *time.Time
		err := r.Scan(&c.Key, &c.At, &c.Since, &read, &c.Err)
		if read != nil {
			c.Read = *read
		}
		return c, err
	})
}
