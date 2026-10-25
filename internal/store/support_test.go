package store

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// support is a migrated store, a rail and a person no other test or run
// shares: payments are never deleted, so tests keep apart by name instead.
func support(t testing.TB) (*Support, string, snowflake.ID) {
	t.Helper()
	ctx := context.Background()
	pool, err := Open(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return NewSupport(pool), fmt.Sprintf("test%d:USDC", rand.Uint64()), snowflake.ID(rand.Int64N(1<<62) + 1<<60)
}

// fixed is an Amount that hands out each of amounts in turn, then fails.
func fixed(amounts ...int64) Amount {
	var mu sync.Mutex
	return func(price int) (*big.Int, string, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(amounts) == 0 {
			return nil, "", errors.New("out of amounts")
		}
		a := amounts[0]
		amounts = amounts[1:]
		return big.NewInt(int64(price)*10_000 + a), "", nil
	}
}

func TestOfferIsThePlansAndRatesOnOffer(t *testing.T) {
	s, _, _ := support(t)
	o, err := s.Offer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(o.Plans) != "[{1 100} {3 280} {6 540} {12 1000}]" || fmt.Sprint(o.Rates) != "[500 1000 2000]" {
		t.Fatalf("offer %v", o)
	}
	for months, want := range map[int]int{1: 500, 3: 1400, 6: 2700, 12: 5000} {
		for _, p := range o.Plans {
			if p.Months == months && p.Price(500) != want {
				t.Errorf("%d months at $5 costs %d cents, want %d", months, p.Price(500), want)
			}
		}
	}
}

func TestQuotePricesTheAskAndKeepsItsAmountToItself(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	i, err := s.Quote(ctx, Ask{User: user, Months: 3, Cents: 500, Rail: rail}, t0, time.Hour, fixed(17))
	if err != nil {
		t.Fatal(err)
	}
	if i.Price != 1400 || i.Gold || i.State != InvoiceOpen || i.Amount.Int64() != 1400*10_000+17 || !i.Expires.Equal(t0.Add(time.Hour)) || len(i.ID) != 26 {
		t.Fatalf("quote %+v", i)
	}
	// Asking again for the same thing is the same checkout.
	again, err := s.Quote(ctx, Ask{User: user, Months: 3, Cents: 500, Rail: rail}, t0.Add(time.Minute), time.Hour, fixed(99))
	if err != nil || again.ID != i.ID {
		t.Fatalf("asking again: %v %v, want %s", again.ID, err, i.ID)
	}
	// Someone else whose first try lands on that amount gets another.
	other := user + 1
	j, err := s.Quote(ctx, Ask{User: other, Months: 3, Cents: 500, Rail: rail}, t0, time.Hour, fixed(17, 18))
	if err != nil || j.Amount.Int64() != 1400*10_000+18 {
		t.Fatalf("a taken amount: %+v %v", j, err)
	}
	// Every try taken: the rail is too busy to tell checkouts apart.
	if _, err := s.Quote(ctx, Ask{User: other + 1, Months: 3, Cents: 500, Rail: rail}, t0, time.Hour, fixed(17, 18, 17, 18, 17, 18, 17, 18)); !errors.Is(err, ErrNoAmount) {
		t.Fatalf("every amount taken: %v", err)
	}
	gold, err := s.Quote(ctx, Ask{User: user, Months: 12, Cents: 2000, Rail: rail}, t0, time.Hour, fixed(1))
	if err != nil || !gold.Gold || gold.Price != 20000 {
		t.Fatalf("paying more: %+v %v", gold, err)
	}
	for _, a := range []Ask{{User: user, Months: 2, Cents: 500, Rail: rail}, {User: user, Months: 1, Cents: 700, Rail: rail}} {
		if _, err := s.Quote(ctx, a, t0, time.Hour, fixed(2)); !errors.Is(err, ErrNotOffered) {
			t.Errorf("%+v: %v", a, err)
		}
	}
	if _, err := s.Invoice(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, ErrNoInvoice) {
		t.Fatalf("no such checkout: %v", err)
	}
}

func TestOnePersonHoldsAFewCheckoutsAtOnce(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	for months := range []int{1, 3, 6} {
		if _, err := s.Quote(ctx, Ask{User: user, Months: []int{1, 3, 6}[months], Cents: 500, Rail: rail}, t0, time.Hour, fixed(int64(months+1))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Quote(ctx, Ask{User: user, Months: 12, Cents: 500, Rail: rail}, t0, time.Hour, fixed(9)); !errors.Is(err, ErrTooMany) {
		t.Fatalf("a fourth: %v", err)
	}
	// Lapsed, they no longer count as open, but they still hold their
	// amounts: heldPerUser caps those.
	if err := s.Sweep(ctx, t0.Add(2*time.Hour), 48*time.Hour); err != nil {
		t.Fatal(err)
	}
	at := t0.Add(2 * time.Hour)
	for n := range heldPerUser - openPerUser {
		if _, err := s.Quote(ctx, Ask{User: user, Months: 12, Cents: 500, Rail: rail}, at, time.Minute, fixed(int64(10+n))); err != nil {
			t.Fatalf("checkout %d beside lapsed ones: %v", openPerUser+n+1, err)
		}
		at = at.Add(2 * time.Minute)
		if err := s.Sweep(ctx, at, 48*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Quote(ctx, Ask{User: user, Months: 1, Cents: 500, Rail: rail}, at, time.Hour, fixed(99)); !errors.Is(err, ErrTooMany) {
		t.Fatalf("past heldPerUser: %v", err)
	}
}

func TestAQuotedCheckoutPaysOnlyWhatWasSentInTime(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	quoted := func(n int64) Amount {
		return func(int) (*big.Int, string, error) { return big.NewInt(n), "150.00", nil }
	}
	life := 20 * time.Minute
	onTime, err := s.Quote(ctx, Ask{User: user, Months: 1, Cents: 500, Rail: rail}, t0, life, quoted(5))
	if err != nil {
		t.Fatal(err)
	}
	stale, err := s.Quote(ctx, Ask{User: user + 1, Months: 1, Cents: 500, Rail: rail}, t0, life, quoted(6))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx, t0.Add(3*time.Hour), 48*time.Hour); err != nil {
		t.Fatal(err)
	}
	// Sent in its last minute, read hours later: still pays.
	if r, err := s.Record(ctx, Payment{Rail: rail, Tx: "just", Amount: onTime.Amount, Sent: t0.Add(19 * time.Minute), Seen: t0.Add(3 * time.Hour)}); err != nil || !r.Paid() {
		t.Fatalf("a payment sent in time: %+v %v", r, err)
	}
	// Sent an hour after its price was good: kept, but pays nothing.
	if r, err := s.Record(ctx, Payment{Rail: rail, Tx: "bet", Amount: stale.Amount, Sent: t0.Add(80 * time.Minute), Seen: t0.Add(3 * time.Hour)}); err != nil || !r.New || r.Paid() {
		t.Fatalf("paying a stale quote: %+v %v", r, err)
	}
}

func TestQuotesRaceForOneAmount(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, 8)
	for n := range 8 {
		wg.Go(func() {
			// Everyone's first pick is 1: one gets it, the rest move on.
			i, err := s.Quote(ctx, Ask{User: user + snowflake.ID(n), Months: 1, Cents: 500, Rail: rail}, t0, time.Hour, fixed(1, int64(n+2)))
			ids[n], errs[n] = i.ID, err
			if err == nil && i.Amount.Int64() != 500*10_000+1 && i.Amount.Int64() != 500*10_000+int64(n+2) {
				errs[n] = fmt.Errorf("amount %v", i.Amount)
			}
		})
	}
	wg.Wait()
	for n, err := range errs {
		if err != nil {
			t.Fatalf("quote %d: %v", n, err)
		}
	}
}

func TestAPaymentPaysItsCheckoutOnceAndBuysItsPeriod(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	i, err := s.Quote(ctx, Ask{User: user, Months: 3, Cents: 500, Rail: rail}, t0, time.Hour, fixed(42))
	if err != nil {
		t.Fatal(err)
	}
	paid := t0.Add(10 * time.Minute)
	p := Payment{Rail: rail, Tx: "0xabc", Amount: i.Amount, Sender: "0xfrom", Seen: paid}
	r, err := s.Record(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if !r.New || !r.Paid() || r.Invoice.State != InvoicePaid || !r.Invoice.Paid.Equal(paid) {
		t.Fatalf("recorded %+v", r)
	}
	if !r.Period.Starts.Equal(paid) || !r.Period.Ends.Equal(paid.AddDate(0, 3, 0)) || r.Period.Gold {
		t.Fatalf("period %+v", r.Period)
	}
	if !r.Invoice.PeriodEnds.Equal(r.Period.Ends) {
		t.Fatalf("checkout's period ends %v, want %v", r.Invoice.PeriodEnds, r.Period.Ends)
	}
	// Seen again: nothing new.
	again, err := s.Record(ctx, p)
	if err != nil || again.New || again.Paid() {
		t.Fatalf("seen again: %+v %v", again, err)
	}
	// The same amount again in another transaction pays nothing: its
	// checkout is paid.
	dup, err := s.Record(ctx, Payment{Rail: rail, Tx: "0xdef", Amount: i.Amount, Seen: paid})
	if err != nil || !dup.New || dup.Paid() {
		t.Fatalf("paid twice: %+v %v", dup, err)
	}
	un, err := s.Unmatched(ctx, 100)
	if err != nil || !hasTx(un, "0xdef") || hasTx(un, "0xabc") {
		t.Fatalf("unmatched %v %v", un, err)
	}
	st, err := s.Status(ctx, user, paid.Add(time.Hour))
	if err != nil || !st.Active || st.Gold || !st.Until.Equal(paid.AddDate(0, 3, 0)) || !st.Since.Equal(paid) {
		t.Fatalf("status %+v %v", st, err)
	}
	got, err := s.Received(ctx, t0)
	if err != nil || got[rail].Int64() != 2*i.Amount.Int64() {
		t.Fatalf("received %v %v", got, err)
	}
}

func hasTx(ps []Payment, tx string) bool {
	for _, p := range ps {
		if p.Tx == tx {
			return true
		}
	}
	return false
}

func TestPayingAgainAddsOnAndGoldIsWhatCoversNow(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	pay := func(months, cents int, at time.Time, change int64) Recorded {
		t.Helper()
		i, err := s.Quote(ctx, Ask{User: user, Months: months, Cents: cents, Rail: rail}, at, time.Hour, fixed(change))
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.Record(ctx, Payment{Rail: rail, Tx: fmt.Sprint("tx", change), Amount: i.Amount, Seen: at.Add(time.Minute)})
		if err != nil || !r.Paid() {
			t.Fatalf("paying: %+v %v", r, err)
		}
		return r
	}
	first := pay(1, 500, t0, 1)
	second := pay(1, 1000, t0.Add(24*time.Hour), 2)
	if !second.Period.Starts.Equal(first.Period.Ends) || !second.Period.Gold {
		t.Fatalf("second period %+v after %+v", second.Period, first.Period)
	}
	st, _ := s.Status(ctx, user, t0.Add(48*time.Hour))
	if !st.Active || st.Gold {
		t.Fatalf("in the plain month: %+v", st)
	}
	st, _ = s.Status(ctx, user, first.Period.Ends.Add(time.Hour))
	if !st.Active || !st.Gold || !st.Until.Equal(second.Period.Ends) {
		t.Fatalf("in the gold month: %+v", st)
	}
	st, _ = s.Status(ctx, user, second.Period.Ends)
	if st.Active || st.Gold || !st.Until.Equal(second.Period.Ends) {
		t.Fatalf("after it ran out: %+v", st)
	}
	if st, _ := s.Status(ctx, user+1000, t0); st.Active || !st.Until.IsZero() {
		t.Fatalf("someone who never backed them: %+v", st)
	}
	all, err := s.Supporters(ctx, first.Period.Ends.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, x := range all {
		if x.User == user {
			found = x.Gold && x.Until.Equal(second.Period.Ends)
		}
	}
	if !found {
		t.Fatalf("supporters %v", all)
	}
}

func TestALatePaymentStillPaysUntilItsCheckoutIsVoid(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	late, err := s.Quote(ctx, Ask{User: user, Months: 1, Cents: 500, Rail: rail}, t0, time.Hour, fixed(5))
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.Quote(ctx, Ask{User: user + 1, Months: 1, Cents: 500, Rail: rail}, t0, time.Hour, fixed(6))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx, t0.Add(2*time.Hour), 48*time.Hour); err != nil {
		t.Fatal(err)
	}
	if i, _ := s.Invoice(ctx, late.ID); i.State != InvoiceLapsed {
		t.Fatalf("after its hour: %s", i.State)
	}
	r, err := s.Record(ctx, Payment{Rail: rail, Tx: "late", Amount: late.Amount, Seen: t0.Add(3 * time.Hour)})
	if err != nil || !r.Paid() {
		t.Fatalf("a late payment: %+v %v", r, err)
	}
	if err := s.Sweep(ctx, t0.Add(50*time.Hour), 48*time.Hour); err != nil {
		t.Fatal(err)
	}
	if i, _ := s.Invoice(ctx, gone.ID); i.State != InvoiceVoid {
		t.Fatalf("after its grace: %s", i.State)
	}
	if i, _ := s.Invoice(ctx, late.ID); i.State != InvoicePaid {
		t.Fatalf("a paid checkout swept: %s", i.State)
	}
	r, err = s.Record(ctx, Payment{Rail: rail, Tx: "too late", Amount: gone.Amount, Seen: t0.Add(51 * time.Hour)})
	if err != nil || !r.New || r.Paid() {
		t.Fatalf("paying a void checkout: %+v %v", r, err)
	}
	// Its amount is free again.
	if _, err := s.Quote(ctx, Ask{User: user + 2, Months: 1, Cents: 500, Rail: rail}, t0.Add(51*time.Hour), time.Hour, fixed(6)); err != nil {
		t.Fatalf("reusing a void amount: %v", err)
	}
}

func TestAPaymentIsNeverChangedOrDeleted(t *testing.T) {
	s, rail, _ := support(t)
	ctx := context.Background()
	if _, err := s.Record(ctx, Payment{Rail: rail, Tx: "kept", Amount: big.NewInt(1), Seen: t0}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"update support_payment set amount = 2 where rail = $1", "delete from support_payment where rail = $1"} {
		if _, err := s.pool.Exec(ctx, q, rail); err == nil {
			t.Errorf("%s: allowed", q)
		}
	}
}

func TestAGiftAddsOnLikeAPayment(t *testing.T) {
	s, _, user := support(t)
	ctx := context.Background()
	p, err := s.Gift(ctx, user, 2, true, 7, t0)
	if err != nil || !p.Starts.Equal(t0) || !p.Ends.Equal(t0.AddDate(0, 2, 0)) || !p.Gold {
		t.Fatalf("gift %+v %v", p, err)
	}
	q, err := s.Gift(ctx, user, 1, false, 7, t0)
	if err != nil || !q.Starts.Equal(p.Ends) {
		t.Fatalf("second gift %+v %v", q, err)
	}
	if _, err := s.Gift(ctx, user, 0, false, 7, t0); !errors.Is(err, ErrNotOffered) {
		t.Fatalf("no months: %v", err)
	}
}

func TestACursorStartsWhereItWasFirstRead(t *testing.T) {
	s, rail, _ := support(t)
	ctx := context.Background()
	c, err := s.Cursor(ctx, rail, t0)
	if err != nil || c.At != "" || !c.Since.Equal(t0) || !c.Read.IsZero() {
		t.Fatalf("new cursor %+v %v", c, err)
	}
	if err := s.Advance(ctx, rail, "123", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Failed(ctx, rail, "rpc down"); err != nil {
		t.Fatal(err)
	}
	c, err = s.Cursor(ctx, rail, t0.Add(time.Hour))
	if err != nil || c.At != "123" || !c.Since.Equal(t0) || c.Err != "rpc down" || !c.Read.Equal(t0.Add(time.Minute)) {
		t.Fatalf("cursor %+v %v", c, err)
	}
	if err := s.Advance(ctx, rail, "124", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	all, err := s.Cursors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range all {
		if x.Key == rail && (x.At != "124" || x.Err != "") {
			t.Fatalf("cursor after a good read %+v", x)
		}
	}
}
