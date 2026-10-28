package store

import (
	"context"
	"testing"
	"time"
)

func TestPaymentDestinationAndCreationTimeArePartOfMatching(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	i, err := s.Quote(ctx, Ask{User: user, Months: 1, Cents: 500, Rail: rail, Destination: "wallet-one"}, t0, time.Hour, fixed(42))
	if err != nil || i.Destination != "wallet-one" {
		t.Fatalf("destination not pinned: %+v %v", i, err)
	}
	for _, p := range []Payment{
		{Rail: rail, Tx: "wrong-wallet", To: "wallet-two", Amount: i.Amount, Seen: t0.Add(time.Minute)},
		{Rail: rail, Tx: "old", To: "wallet-one", Amount: i.Amount, Sent: t0.Add(-time.Second), Seen: t0.Add(time.Minute)},
	} {
		r, err := s.Record(ctx, p)
		if err != nil || !r.New || r.Paid() {
			t.Fatalf("wrong receipt matched: %+v %v", r, err)
		}
	}
	r, err := s.Record(ctx, Payment{Rail: rail, Tx: "right", To: "wallet-one", Amount: i.Amount, Seen: t0.Add(time.Minute)})
	if err != nil || !r.Paid() {
		t.Fatalf("right receipt rejected: %+v %v", r, err)
	}
}

func TestIndexedEventsShareATransactionWithoutColliding(t *testing.T) {
	s, rail, user := support(t)
	ctx := context.Background()
	i, err := s.Quote(ctx, Ask{User: user, Months: 1, Cents: 500, Rail: rail}, t0, time.Hour, fixed(42))
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Quote(ctx, Ask{User: user + 1, Months: 1, Cents: 500, Rail: rail}, t0, time.Hour, fixed(43))
	if err != nil {
		t.Fatal(err)
	}
	a := Payment{Rail: rail, Tx: "one-transaction", Event: "trace:0", Amount: i.Amount, Seen: t0.Add(time.Minute)}
	b := Payment{Rail: rail, Tx: a.Tx, Event: "trace:1", Amount: j.Amount, Seen: a.Seen}
	for _, p := range []Payment{a, b} {
		r, err := s.Record(ctx, p)
		if err != nil || !r.Paid() {
			t.Fatalf("distinct receipt lost: %+v %v", r, err)
		}
		again, err := s.Record(ctx, p)
		if err != nil || again.New || again.Paid() {
			t.Fatalf("receipt counted twice: %+v %v", again, err)
		}
	}
}
