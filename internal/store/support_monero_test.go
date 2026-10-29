package store

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

func xmrStore(t *testing.T) (*Support, string, snowflake.ID) {
	s, _, user := support(t)
	wallet := fmt.Sprintf("wallet-%d", user)
	addresses := []SupportAddress{}
	for n := range 8 {
		addresses = append(addresses, SupportAddress{Wallet: wallet, Address: fmt.Sprintf("%s-%d", wallet, n+1), Minor: uint32(n + 1)})
	}
	if err := s.AddAddresses(context.Background(), addresses); err != nil {
		t.Fatal(err)
	}
	return s, wallet, user
}

func xmrQuote(t *testing.T, s *Support, wallet string, user snowflake.ID) Invoice {
	t.Helper()
	i, err := s.Quote(context.Background(), Ask{User: user, Wallet: wallet, Rail: "monero:XMR", Months: 1, Cents: 500}, t0, 20*time.Minute, func(int) (*big.Int, string, error) { return big.NewInt(100), "200.00", nil })
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestMoneroAllocationsAreAtomicPermanentAndIndependentOfAmount(t *testing.T) {
	s, wallet, user := xmrStore(t)
	ctx := context.Background()
	i := xmrQuote(t, s, wallet, user)
	j := xmrQuote(t, s, wallet, user+1)
	if i.Destination == j.Destination || i.Amount.Cmp(j.Amount) != 0 {
		t.Fatalf("amount unexpectedly identifies checkout: %+v %+v", i, j)
	}
	if again := xmrQuote(t, s, wallet, user); again.ID != i.ID {
		t.Fatal("repeated quote allocated another address")
	}
	if count, err := s.FreeAddresses(ctx, wallet); err != nil || count != 6 {
		t.Fatalf("pool count %d %v", count, err)
	}
	if err := s.Sweep(ctx, t0.Add(49*time.Hour), 48*time.Hour); err != nil {
		t.Fatal(err)
	}
	k := xmrQuote(t, s, wallet, user+2)
	if k.Destination == i.Destination || k.Destination == j.Destination {
		t.Fatal("reused expired address")
	}
	archived, err := s.WatchedAddresses(ctx, wallet, 0, false)
	if err != nil || len(archived) != 2 {
		t.Fatalf("forgot expired addresses: %v %v", archived, err)
	}
	active, err := s.WatchedAddresses(ctx, wallet, 0, true)
	if err != nil || len(active) != 1 || active[0].Address != k.Destination {
		t.Fatalf("active watch: %v %v", active, err)
	}
	if err := s.AddAddresses(ctx, []SupportAddress{{Wallet: wallet, Address: i.Destination, Minor: 1}}); err != nil {
		t.Fatal("restoring same allocation failed", err)
	}
	if err := s.AddAddresses(ctx, []SupportAddress{{Wallet: wallet, Address: "different", Minor: 1}}); err == nil {
		t.Fatal("changed restored allocation")
	}
	for _, sql := range []string{"delete from support_address where wallet=$1", "update support_address set invoice=null where wallet=$1 and invoice is not null"} {
		if _, err := s.pool.Exec(ctx, sql, wallet); err == nil {
			t.Fatal("allocation history was mutable")
		}
	}
}

func TestMoneroPartialReceiptsOverpaymentAndReplayGrantOnce(t *testing.T) {
	s, wallet, user := xmrStore(t)
	ctx := context.Background()
	i := xmrQuote(t, s, wallet, user)
	first := Payment{Rail: i.Rail, Tx: fmt.Sprintf("partial-%d", user), Event: "subaddress:1", To: i.Destination, Amount: big.NewInt(40), Sent: t0.Add(time.Minute), Seen: t0.Add(time.Hour)}
	r, err := s.Record(ctx, first)
	if err != nil || !r.New || r.Paid() {
		t.Fatalf("partial paid early: %+v %v", r, err)
	}
	if again, err := s.Record(ctx, first); err != nil || again.New {
		t.Fatalf("duplicate receipt: %+v %v", again, err)
	}
	second := first
	second.Tx += "-second"
	second.Amount = big.NewInt(80)
	second.Sent = t0.Add(2 * time.Minute)
	r, err = s.Record(ctx, second)
	if err != nil || !r.Paid() || r.Invoice.ID != i.ID {
		t.Fatalf("sum/overpayment failed: %+v %v", r, err)
	}
	if again, err := s.Record(ctx, second); err != nil || again.New || again.Paid() {
		t.Fatal("payment granted twice", err)
	}
	status, err := s.Status(ctx, user, first.Seen)
	if err != nil || !status.Active || !status.Until.Equal(first.Seen.AddDate(0, 1, 0)) {
		t.Fatalf("backing: %+v %v", status, err)
	}
}

func TestMoneroLateAndWrongAddressPaymentsNeverTopUpAnotherInvoice(t *testing.T) {
	s, wallet, user := xmrStore(t)
	ctx := context.Background()
	i := xmrQuote(t, s, wallet, user)
	for n, p := range []Payment{
		{To: "not-our-checkout", Sent: t0.Add(time.Minute)},
		{To: i.Destination, Sent: t0.Add(31 * time.Minute)},
		{To: i.Destination, Sent: t0.Add(-time.Minute)},
	} {
		p.Rail = i.Rail
		p.Tx = fmt.Sprintf("wrong-%d-%d", user, n)
		p.Amount = big.NewInt(100)
		p.Seen = t0.Add(time.Hour)
		if r, err := s.Record(ctx, p); err != nil || !r.New || r.Paid() {
			t.Fatalf("bad payment credited: %+v %v", r, err)
		}
	}
}

func TestConcurrentMoneroReceiptsSerializeOnTheInvoice(t *testing.T) {
	s, wallet, user := xmrStore(t)
	ctx := context.Background()
	i := xmrQuote(t, s, wallet, user)
	var wg sync.WaitGroup
	var mu sync.Mutex
	paid := 0
	for n := range 10 {
		wg.Go(func() {
			r, err := s.Record(ctx, Payment{Rail: i.Rail, Tx: fmt.Sprintf("race-%d-%d", user, n), To: i.Destination, Event: "subaddress:1", Amount: big.NewInt(20), Sent: t0.Add(time.Minute), Seen: t0.Add(time.Hour)})
			if err != nil {
				t.Error(err)
			}
			if r.Paid() {
				mu.Lock()
				paid++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if paid != 1 {
		t.Fatalf("granted %d times", paid)
	}
}
