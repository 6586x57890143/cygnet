package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

func TestWavesLetTheFrontInWithWhoeverTheyBrought(t *testing.T) {
	s, _ := launches(t, "wav", 913)
	ctx := context.Background()
	line(t, s, "wav", 1, 101, 102, 103)
	_ = s.Issue(ctx, "wav", hash("wav0"), 101, t0, t0.Add(48*time.Hour))
	if _, err := s.Vouch(ctx, hash("wav0"), 104, "", t0); err != nil {
		t.Fatal(err)
	}
	// 104 brought 105 in turn.
	if _, err := s.Release(ctx, "wav", 1, 1, admin, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "update line set released = 1 where launch = 'wav' and user_id = 104"); err != nil {
		t.Fatal(err)
	}
	if err := s.Issue(ctx, "wav", hash("wav1"), 104, t0, t0.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Vouch(ctx, hash("wav1"), 105, "", t0); err != nil {
		t.Fatal(err)
	}

	if w, err := s.Last(ctx, "wav"); err != nil || w.ID != 0 {
		t.Fatalf("no waves yet: %+v %v", w, err)
	}
	if err := s.SetStage(ctx, "wav", StageOpen, admin, t0); err != nil {
		t.Fatal(err)
	}
	flowing, _ := s.Flowing(ctx)
	if !hasLaunch(flowing, "wav") {
		t.Fatal("an open launch isn't flowing")
	}
	w, err := s.Admit(ctx, "wav", 2, "two to start", t0)
	if err != nil || w.ID == 0 || w.Size != 2 || w.Admitted != 4 {
		t.Fatalf("first wave: %+v %v", w, err)
	}
	for _, u := range []int64{101, 102, 104, 105} {
		if p, _ := s.Where(ctx, "wav", snowflakeOf(u), t0); p.Admitted.IsZero() {
			t.Fatalf("%d not let in", u)
		}
	}
	if p, _ := s.Where(ctx, "wav", 103, t0); !p.Admitted.IsZero() {
		t.Fatal("103 let in early")
	}

	// They arrive; the wave counts them.
	c := &Codes{s.pool}
	for _, u := range []int64{101, 104} {
		if _, err := c.Arrive(ctx, 913, snowflakeOf(u), t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	last, _ := s.Last(ctx, "wav")
	if last.ID != w.ID || last.Admitted != 4 || last.Arrived != 2 || last.Why != "two to start" {
		t.Fatalf("last %+v", last)
	}
	var wave *int64
	var code []byte
	_ = s.pool.QueryRow(ctx, "select wave, code from joined where guild_id = 913 and user_id = 101").Scan(&wave, &code)
	if wave == nil || *wave != w.ID || code != nil {
		t.Fatalf("101 traced to wave %v, code %x", wave, code)
	}
	_ = s.pool.QueryRow(ctx, "select wave, code from joined where guild_id = 913 and user_id = 104").Scan(&wave, &code)
	if wave != nil || code == nil {
		t.Fatalf("104 traced to wave %v, code %x: their code is their cause", wave, code)
	}
	if err := s.Settle(ctx, w.ID, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if last, _ = s.Last(ctx, "wav"); last.Settled.IsZero() {
		t.Fatal("not settled")
	}

	// The next wave takes who's left; then there's nobody.
	if w2, _ := s.Admit(ctx, "wav", 5, "the rest", t0); w2.Size != 1 {
		t.Fatalf("second wave %+v", w2)
	}
	if w3, err := s.Admit(ctx, "wav", 5, "nobody", t0); err != nil || w3.ID != 0 {
		t.Fatalf("a wave with nobody: %+v %v", w3, err)
	}
	if last, _ = s.Last(ctx, "wav"); last.Why != "the rest" {
		t.Fatalf("an empty wave was kept: %+v", last)
	}

	// An invite taken after its giver is in follows them.
	if _, err := s.pool.Exec(ctx, "update line set released = 2 where launch = 'wav' and user_id = 102"); err != nil {
		t.Fatal(err)
	}
	if err := s.Issue(ctx, "wav", hash("wav2"), 102, t0, t0.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Vouch(ctx, hash("wav2"), 106, "", t0); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Follow(ctx, "wav", t0); err != nil || n != 1 {
		t.Fatalf("follow: %d %v", n, err)
	}
	if v, _ := s.Verify(ctx, "wav"); v.Broken != 0 {
		t.Fatalf("ledger broken at %d", v.Broken)
	}
}

func TestValveAndSealedSignIns(t *testing.T) {
	s, _ := launches(t, "val", 914)
	ctx := context.Background()
	line(t, s, "val", 0, 111, 112)
	_ = s.Seal(ctx, "val", 112, []byte("sealed"), t0)
	if err := s.Valve(ctx, "val", 20, true, admin, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Valve(ctx, "val", 20, true, admin, t0); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Get(ctx, "val")
	if l.Ceiling != 20 || !l.Paused {
		t.Fatalf("valve %+v", l)
	}
	_ = s.SetStage(ctx, "val", StageOpen, admin, t0)
	if flowing, _ := s.Flowing(ctx); hasLaunch(flowing, "val") {
		t.Fatal("a paused launch flows")
	}
	if got, _ := s.Sealed(ctx, "val", 10); len(got) != 0 {
		t.Fatalf("sealed before anyone's in: %v", got)
	}
	if _, err := s.Admit(ctx, "val", 2, "both", t0); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Sealed(ctx, "val", 10)
	if len(got) != 1 || got[0] != 112 {
		t.Fatalf("sealed %v", got)
	}
	ct, err := s.Unseal(ctx, "val", 112, t0)
	if err != nil || string(ct) != "sealed" {
		t.Fatalf("unseal: %q %v", ct, err)
	}
	if _, err := s.Unseal(ctx, "val", 112, t0); !errors.Is(err, ErrNotOnList) {
		t.Fatalf("unsealing twice: %v", err)
	}
	if got, _ := s.Sealed(ctx, "val", 10); len(got) != 0 {
		t.Fatalf("still sealed: %v", got)
	}
}

func hasLaunch(ls []Launch, name string) bool {
	for _, l := range ls {
		if l.Name == name {
			return true
		}
	}
	return false
}

func snowflakeOf(u int64) snowflake.ID { return snowflake.ID(u) }
