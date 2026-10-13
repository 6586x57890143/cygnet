package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// codes is a migrated store with guild's rows cleared, for one test.
func codes(t *testing.T, guild snowflake.ID) *Codes {
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
	for _, q := range []string{"delete from joined where guild_id = $1", "delete from code where guild_id = $1", "delete from invites where guild_id = $1"} {
		if _, err := pool.Exec(ctx, q, int64(guild)); err != nil {
			t.Fatal(err)
		}
	}
	return NewCodes(pool)
}

// pending is Pending in guild alone: other tests' codes share the table.
func pending(s *Codes, guild snowflake.ID, since time.Time) ([]Code, error) {
	all, err := s.Pending(context.Background(), since)
	var out []Code
	for _, c := range all {
		if c.Guild == guild {
			out = append(out, c)
		}
	}
	return out, err
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

var t0 = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func TestMintKeepsToTheLimit(t *testing.T) {
	const guild, owner = 801, 1
	s, ctx := codes(t, guild), context.Background()
	life := t0.Add(48 * time.Hour)
	for i := range 2 {
		if err := s.Mint(ctx, hash(fmt.Sprint("m", i)), guild, owner, t0, life, 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Mint(ctx, hash("m2"), guild, owner, t0, life, 2); !errors.Is(err, ErrNoneLeft) {
		t.Fatalf("a third link under a limit of 2: %v", err)
	}
	// One taken back is back in hand.
	if ok, err := s.Revoke(ctx, hash("m0"), owner, t0); !ok || err != nil {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	if left, _ := s.Left(ctx, guild, owner, t0, 2); left != 1 {
		t.Fatalf("left %d after taking one back, want 1", left)
	}
	if err := s.Mint(ctx, hash("m2"), guild, owner, t0, life, 2); err != nil {
		t.Fatal(err)
	}
	// One that ran out unused is back too; one that was used isn't.
	if _, err := s.Spend(ctx, hash("m1"), 9, t0); err != nil {
		t.Fatal(err)
	}
	later := life.Add(time.Minute)
	if left, _ := s.Left(ctx, guild, owner, later, 2); left != 1 {
		t.Fatalf("left %d once m2 ran out, want 1 (m1 was used)", left)
	}
	if err := s.Mint(ctx, hash("m3"), guild, owner, t0, life, -1); err != nil {
		t.Fatalf("no limit: %v", err)
	}
	mine, err := s.Mine(ctx, guild, owner, t0, 10, true)
	if err != nil || len(mine) != 4 {
		t.Fatalf("mine: %d %v", len(mine), err)
	}
	// Without all: open and used, not taken back (m0); later, m2 and m3
	// have run out and only the used m1 is left.
	if live, _ := s.Mine(ctx, guild, owner, t0, 10, false); len(live) != 3 {
		t.Fatalf("live now: %d", len(live))
	}
	if live, _ := s.Mine(ctx, guild, owner, later, 10, false); len(live) != 1 || live[0].SpentBy != 9 {
		t.Fatalf("live once m2 ran out: %d", len(live))
	}
	if ok, _ := s.Revoke(ctx, hash("m1"), owner, t0); ok {
		t.Fatal("took back a spent link")
	}
	if ok, _ := s.Revoke(ctx, hash("m2"), owner+1, t0); ok {
		t.Fatal("took back someone else's link")
	}
}

// Twenty mints at once under a limit of one make one link.
func TestMintRaceMakesOne(t *testing.T) {
	const guild, owner = 802, 1
	s, ctx := codes(t, guild), context.Background()
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			err := s.Mint(ctx, hash(fmt.Sprint("r", i)), guild, owner, t0, t0.Add(time.Hour), 1)
			if err == nil {
				ok.Add(1)
			} else if !errors.Is(err, ErrNoneLeft) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d links made, want 1", ok.Load())
	}
}

// Fifty people spending one code at once: one gets in. SPEC experiment 3.
func TestSpendRaceHasOneWinner(t *testing.T) {
	const guild, owner = 803, 1
	s, ctx := codes(t, guild), context.Background()
	h := hash("race")
	if err := s.Mint(ctx, h, guild, owner, t0, t0.Add(time.Hour), -1); err != nil {
		t.Fatal(err)
	}
	var won atomic.Int64
	var wg sync.WaitGroup
	for user := range snowflake.ID(50) {
		wg.Go(func() {
			c, err := s.Spend(ctx, h, 100+user, t0)
			switch {
			case err == nil:
				if c.SpentBy != 100+user {
					t.Errorf("spent by %d, returned to %d", c.SpentBy, 100+user)
				}
				won.Add(1)
			case !errors.Is(err, ErrGone):
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("%d spent it, want 1", won.Load())
	}
}

func TestSpend(t *testing.T) {
	const guild, owner = 804, 1
	s, ctx := codes(t, guild), context.Background()
	for _, n := range []string{"a", "b", "c"} {
		if err := s.Mint(ctx, hash(n), guild, owner, t0, t0.Add(time.Hour), -1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Spend(ctx, hash("a"), owner, t0); !errors.Is(err, ErrOwn) {
		t.Fatalf("owner spending: %v", err)
	}
	c, err := s.Spend(ctx, hash("a"), 7, t0)
	if err != nil || c.SpentBy != 7 || c.Owner != owner || c.Guild != guild || !c.SpentAt.Equal(t0) {
		t.Fatalf("spend: %+v %v", c, err)
	}
	// The same person again, later: theirs, as it was.
	if again, err := s.Spend(ctx, hash("a"), 7, t0.Add(time.Minute)); err != nil || !again.SpentAt.Equal(t0) {
		t.Fatalf("a retry: %+v %v", again, err)
	}
	if _, err := s.Spend(ctx, hash("a"), 8, t0); !errors.Is(err, ErrGone) {
		t.Fatalf("someone else after: %v", err)
	}
	if _, err := s.Spend(ctx, hash("b"), 8, t0.Add(time.Hour)); !errors.Is(err, ErrGone) {
		t.Fatalf("at its expiry: %v", err)
	}
	if _, err := s.Revoke(ctx, hash("c"), owner, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(ctx, hash("c"), 8, t0); !errors.Is(err, ErrGone) {
		t.Fatalf("taken back: %v", err)
	}
	if _, err := s.Spend(ctx, hash("nope"), 8, t0); !errors.Is(err, ErrGone) {
		t.Fatalf("no such code: %v", err)
	}
	if f, err := s.Find(ctx, hash("b")); err != nil || !f.Open(t0) || f.Open(t0.Add(time.Hour)) {
		t.Fatalf("find: %+v %v", f, err)
	}
}

func TestArriveTracesTheCause(t *testing.T) {
	const guild, owner = 805, 1
	s, ctx := codes(t, guild), context.Background()
	if err := s.Mint(ctx, hash("x"), guild, owner, t0, t0.Add(time.Hour), -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(ctx, hash("x"), 7, t0); err != nil {
		t.Fatal(err)
	}
	if p, _ := pending(s, guild, t0.Add(-time.Hour)); len(p) != 1 || p[0].SpentBy != 7 {
		t.Fatalf("pending: %+v", p)
	}
	if p, _ := pending(s, guild, t0); len(p) != 0 {
		t.Fatalf("pending since after it was spent: %+v", p)
	}
	at := t0.Add(time.Minute)
	if sp, err := s.Arrive(ctx, guild, 7, at); err != nil || sp != owner {
		t.Fatalf("arrive: %d %v", sp, err)
	}
	// The gateway's event for the same join changes nothing.
	if sp, err := s.Arrive(ctx, guild, 7, at.Add(time.Second)); err != nil || sp != 0 {
		t.Fatalf("again: %d %v", sp, err)
	}
	if p, _ := pending(s, guild, t0.Add(-time.Hour)); len(p) != 0 {
		t.Fatalf("still pending after arriving: %+v", p)
	}
	if sp, err := s.Arrive(ctx, guild, 8, at); err != nil || sp != 0 {
		t.Fatalf("untraced: %d %v", sp, err)
	}
	if err := s.Leave(ctx, guild, 7, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	tree, err := s.Tree(ctx, guild)
	if err != nil || len(tree) != 2 || tree[0].User != 7 || tree[0].Sponsor != owner || tree[0].Left.IsZero() || tree[1].Sponsor != 0 {
		t.Fatalf("tree: %+v %v", tree, err)
	}
	// Back on their own: untraced, the code not spent twice.
	if sp, _ := s.Arrive(ctx, guild, 7, at.Add(2*time.Hour)); sp != 0 {
		t.Fatalf("rejoin attributed to %d", sp)
	}
	if tree, _ := s.Tree(ctx, guild); tree[1].User != 7 || tree[1].Sponsor != 0 || !tree[1].Left.IsZero() {
		t.Fatalf("after rejoining: %+v", tree)
	}
}

// A member already in who spends a code doesn't leave it pending forever.
func TestArriveWhileInSettlesTheCode(t *testing.T) {
	const guild, owner = 806, 1
	s, ctx := codes(t, guild), context.Background()
	if _, err := s.Arrive(ctx, guild, 7, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Mint(ctx, hash("y"), guild, owner, t0, t0.Add(time.Hour), -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(ctx, hash("y"), 7, t0); err != nil {
		t.Fatal(err)
	}
	if sp, err := s.Arrive(ctx, guild, 7, t0.Add(time.Minute)); err != nil || sp != 0 {
		t.Fatalf("arrive: %d %v", sp, err)
	}
	if p, _ := pending(s, guild, t0.Add(-time.Hour)); len(p) != 0 {
		t.Fatalf("pending: %+v", p)
	}
}

func TestEach(t *testing.T) {
	const guild = 807
	s, ctx := codes(t, guild), context.Background()
	if n, err := s.Each(ctx, guild); n != 0 || err != nil {
		t.Fatalf("unset: %d %v", n, err)
	}
	for _, n := range []int{3, 5} {
		if err := s.SetEach(ctx, guild, n); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.Each(ctx, guild); n != 5 {
		t.Fatalf("each %d, want 5", n)
	}
	if s.SetEach(ctx, guild, 11) == nil {
		t.Fatal("11 each was kept")
	}
}

// One way in at a time: a second link spends nothing while the first
// waits, so its owner keeps it.
func TestSpendHoldsOneWayIn(t *testing.T) {
	const guild = 808
	s, ctx := codes(t, guild), context.Background()
	for i, n := range []string{"first", "second"} {
		if err := s.Mint(ctx, hash(n), guild, snowflake.ID(i+1), t0, t0.Add(time.Hour), -1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Spend(ctx, hash("first"), 7, t0); err != nil {
		t.Fatal(err)
	}
	c, err := s.Spend(ctx, hash("second"), 7, t0)
	if err != nil || c.Owner != 1 {
		t.Fatalf("second link: %+v %v, want the first back", c, err)
	}
	if f, _ := s.Find(ctx, hash("second")); !f.Open(t0) {
		t.Fatal("the second link was spent")
	}
	if left, _ := s.Left(ctx, guild, 2, t0, -1); left != -1 {
		t.Fatalf("no limit reads %d, want -1", left)
	}
	// Once in, the next server visit is theirs to spend again.
	if _, err := s.Arrive(ctx, guild, 7, t0); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Spend(ctx, hash("second"), 7, t0); err != nil || c.Owner != 2 {
		t.Fatalf("after arriving: %+v %v", c, err)
	}
}

// A direct add and its gateway event, many times over at once: one join,
// traced to its code.
func TestArriveRace(t *testing.T) {
	const guild, owner = 809, 1
	s, ctx := codes(t, guild), context.Background()
	if err := s.Mint(ctx, hash("z"), guild, owner, t0, t0.Add(time.Hour), -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(ctx, hash("z"), 7, t0); err != nil {
		t.Fatal(err)
	}
	var credited atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			sp, err := s.Arrive(ctx, guild, 7, t0)
			if err != nil {
				t.Error(err)
			}
			if sp == owner {
				credited.Add(1)
			}
		})
	}
	wg.Wait()
	tree, _ := s.Tree(ctx, guild)
	if credited.Load() != 1 || len(tree) != 1 || tree[0].Sponsor != owner {
		t.Fatalf("credited %d times, tree %+v", credited.Load(), tree)
	}
}
