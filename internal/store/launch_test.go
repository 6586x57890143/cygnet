package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// launches is a migrated store with the launch name cleared and registered
// for guild, for one test.
func launches(t testing.TB, name string, guild snowflake.ID) (*Launches, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	c := codes(t, guild)
	pool := c.pool
	for _, q := range []string{
		"insert into gone (launch, at) values ($1, now()) on conflict do nothing",
		"delete from ledger where launch = $1",
		"delete from gone where launch = $1",
		"delete from line where launch = $1",
		"delete from joined where wave in (select id from wave where launch = $1)",
		"delete from wave where launch = $1",
		"delete from code where launch = $1",
		"delete from launch where name = $1",
	} {
		if _, err := pool.Exec(ctx, q, name); err != nil {
			t.Fatal(q, err)
		}
	}
	s := NewLaunches(pool, []byte("test key"))
	if err := s.Register(ctx, name, guild, t0); err != nil {
		t.Fatal(err)
	}
	return s, pool
}

const admin = 7

func TestRegisterKeepsANameToItsServer(t *testing.T) {
	s, _ := launches(t, "reg", 901)
	ctx := context.Background()
	if err := s.Register(ctx, "reg", 901, t0); err != nil {
		t.Fatalf("registering again: %v", err)
	}
	if err := s.Register(ctx, "reg", 902, t0); !errors.Is(err, ErrTaken) {
		t.Fatalf("another server taking the name: %v", err)
	}
	if err := s.Register(ctx, "reg-two", 901, t0); !errors.Is(err, ErrTaken) {
		t.Fatalf("a second name for one server: %v", err)
	}
	l, err := s.Of(ctx, 901)
	if err != nil || l.Name != "reg" || l.Stage != StageGathering || l.Ceiling != 10 {
		t.Fatalf("Of: %+v %v", l, err)
	}
	if _, err := s.Get(ctx, "nope"); !errors.Is(err, ErrNoLaunch) {
		t.Fatalf("Get of nothing: %v", err)
	}
}

func TestJoinTakesTheNextPlaceWhileGathering(t *testing.T) {
	s, _ := launches(t, "join", 903)
	ctx := context.Background()
	for i, u := range []snowflake.ID{11, 12} {
		p, err := s.Join(ctx, "join", u, "", nil, t0)
		if err != nil || p != i+1 {
			t.Fatalf("join %d: place %d %v", u, p, err)
		}
	}
	if _, err := s.Join(ctx, "join", 11, "", nil, t0); !errors.Is(err, ErrOnList) {
		t.Fatalf("joining twice: %v", err)
	}
	if _, err := s.Join(ctx, "join", 13, "same connection", []byte("sealed"), t0); err != nil {
		t.Fatal(err)
	}
	p, err := s.Where(ctx, "join", 13, t0)
	if err != nil || p.Rank != 3 || p.Held != "same connection" || !p.Sealed {
		t.Fatalf("held and sealed: %+v %v", p, err)
	}
	if err := s.SetStage(ctx, "join", StageClosed, admin, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Join(ctx, "join", 14, "", nil, t0); !errors.Is(err, ErrClosed) {
		t.Fatalf("joining while closed: %v", err)
	}
	if _, err := s.Join(ctx, "nope", 14, "", nil, t0); !errors.Is(err, ErrNoLaunch) {
		t.Fatalf("joining nothing: %v", err)
	}
	if _, err := s.Where(ctx, "join", 14, t0); !errors.Is(err, ErrNotOnList) {
		t.Fatalf("where of someone not on it: %v", err)
	}
}

func TestStageKeepsTheFirstOpening(t *testing.T) {
	s, _ := launches(t, "stage", 904)
	ctx := context.Background()
	for i, st := range []string{StageOpen, StageClosed, StageOpen, StageOpen} {
		if err := s.SetStage(ctx, "stage", st, admin, t0.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := s.Get(ctx, "stage")
	if l.Stage != StageOpen || !l.Opens.Equal(t0) {
		t.Fatalf("stage %s opens %v, want open at t0", l.Stage, l.Opens)
	}
	v, err := s.Verify(ctx, "stage")
	if err != nil || v.Entries != 3 || v.Broken != 0 {
		t.Fatalf("an unchanged stage writes nothing: %+v %v", v, err)
	}
}

// line puts users on name's list in order and releases each invites to them.
func line(t *testing.T, s *Launches, name string, each int, users ...snowflake.ID) {
	t.Helper()
	ctx := context.Background()
	for _, u := range users {
		if _, err := s.Join(ctx, name, u, "", nil, t0); err != nil {
			t.Fatal(err)
		}
	}
	if each > 0 {
		if _, err := s.Release(ctx, name, len(users), each, admin, t0); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReleaseGivesTheFrontOfTheLine(t *testing.T) {
	s, _ := launches(t, "rel", 905)
	ctx := context.Background()
	line(t, s, "rel", 0, 21, 22, 23)
	if _, err := s.Join(ctx, "rel", 24, "same network", nil, t0); err != nil {
		t.Fatal(err)
	}
	n, err := s.Release(ctx, "rel", 2, 3, admin, t0)
	if err != nil || n != 2 {
		t.Fatalf("released to %d: %v", n, err)
	}
	// The next release skips who had some, and who's held.
	n, _ = s.Release(ctx, "rel", 10, 3, admin, t0)
	if n != 1 {
		t.Fatalf("second release to %d, want 1", n)
	}
	if ok, err := s.Lift(ctx, "rel", 24, admin, t0); !ok || err != nil {
		t.Fatalf("lift: %v %v", ok, err)
	}
	if ok, _ := s.Lift(ctx, "rel", 24, admin, t0); ok {
		t.Fatal("lifting twice")
	}
	if n, _ = s.Release(ctx, "rel", 10, 3, admin, t0); n != 1 {
		t.Fatalf("released to %d after the lift, want 1", n)
	}
	st, err := s.Count(ctx, "rel", t0)
	if err != nil || st.Total != 4 || st.ReleasedTo != 4 || st.Waiting != 0 {
		t.Fatalf("stats %+v %v", st, err)
	}
}

func TestIssueKeepsToWhatWasReleased(t *testing.T) {
	s, _ := launches(t, "iss", 906)
	ctx := context.Background()
	line(t, s, "iss", 2, 31)
	life := t0.Add(48 * time.Hour)
	for i := range 2 {
		if err := s.Issue(ctx, "iss", hash(fmt.Sprint("iss", i)), 31, t0, life); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Issue(ctx, "iss", hash("iss2"), 31, t0, life); !errors.Is(err, ErrNoneLeft) {
		t.Fatalf("a third invite of 2: %v", err)
	}
	if ok, err := s.Revoke(ctx, "iss", hash("iss0"), 31, t0); !ok || err != nil {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	if ok, _ := s.Revoke(ctx, "iss", hash("iss0"), 31, t0); ok {
		t.Fatal("revoking twice")
	}
	p, _ := s.Where(ctx, "iss", 31, t0)
	if p.Left != 1 || len(p.Invites) != 2 {
		t.Fatalf("left %d of %d invites, want 1 of 2", p.Left, len(p.Invites))
	}
	// Someone with nothing released, or not on the list, makes none.
	line(t, s, "iss", 0, 32)
	if err := s.Issue(ctx, "iss", hash("iss3"), 32, t0, life); !errors.Is(err, ErrNoneLeft) {
		t.Fatalf("issue with none released: %v", err)
	}
	// A launch's invites don't count against /invite's links.
	if left, _ := (&Codes{s.pool}).Left(ctx, 906, 31, t0, 1); left != 1 {
		t.Fatalf("server links left %d, want 1", left)
	}
}

func TestVouchPutsThemUnderWhoeverGaveIt(t *testing.T) {
	s, _ := launches(t, "vou", 907)
	ctx := context.Background()
	line(t, s, "vou", 2, 41)
	life := t0.Add(48 * time.Hour)
	_ = s.Issue(ctx, "vou", hash("vou0"), 41, t0, life)
	_ = s.Issue(ctx, "vou", hash("vou1"), 41, t0, life)
	if _, err := s.Vouch(ctx, hash("vou0"), 41, "", t0); !errors.Is(err, ErrOnList) {
		t.Fatalf("the giver taking their own, already on the list: %v", err)
	}
	name, err := s.Vouch(ctx, hash("vou0"), 42, "", t0)
	if err != nil || name != "vou" {
		t.Fatalf("vouch: %q %v", name, err)
	}
	if _, err := s.Vouch(ctx, hash("vou0"), 42, "", t0); err != nil {
		t.Fatalf("a retry: %v", err)
	}
	if _, err := s.Vouch(ctx, hash("vou1"), 42, "", t0); !errors.Is(err, ErrOnList) {
		t.Fatalf("a second invite for someone on the list: %v", err)
	}
	if _, err := s.Vouch(ctx, hash("vou0"), 43, "", t0); !errors.Is(err, ErrGone) {
		t.Fatalf("a spent invite: %v", err)
	}
	if _, err := s.Vouch(ctx, hash("none"), 43, "", t0); !errors.Is(err, ErrGone) {
		t.Fatalf("no such invite: %v", err)
	}
	if _, err := s.Vouch(ctx, hash("vou1"), 43, "", life); !errors.Is(err, ErrGone) {
		t.Fatalf("a run out invite: %v", err)
	}
	p, err := s.Where(ctx, "vou", 42, t0)
	if err != nil || p.Sponsor != 41 || p.Rank != 0 {
		t.Fatalf("the invitee: %+v %v", p, err)
	}
	rows, _ := s.List(ctx, "vou")
	if len(rows) != 2 || rows[1].Sponsor != 41 {
		t.Fatalf("list %+v", rows)
	}
	if err := s.SetStage(ctx, "vou", StageClosed, admin, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Vouch(ctx, hash("vou1"), 44, "", t0); !errors.Is(err, ErrClosed) {
		t.Fatalf("vouch while closed: %v", err)
	}
	// A server's link isn't a launch's.
	c := &Codes{s.pool}
	_ = c.Mint(ctx, hash("srv"), 907, 41, t0, life, -1)
	if _, err := s.Vouch(ctx, hash("srv"), 44, "", t0); !errors.Is(err, ErrGone) {
		t.Fatalf("a server's link: %v", err)
	}
}

func TestVouchRefusesTheGiversOwn(t *testing.T) {
	s, pool := launches(t, "own", 908)
	ctx := context.Background()
	line(t, s, "own", 1, 51)
	_ = s.Issue(ctx, "own", hash("own0"), 51, t0, t0.Add(time.Hour))
	// Off the list, so only the ownership refuses it.
	if _, err := pool.Exec(ctx, "delete from line where launch = 'own' and user_id = 51"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Vouch(ctx, hash("own0"), 51, "", t0); !errors.Is(err, ErrOwn) {
		t.Fatalf("the giver's own: %v", err)
	}
}

func TestLeaveForgetsThemAndKeepsTheTree(t *testing.T) {
	s, _ := launches(t, "lea", 909)
	ctx := context.Background()
	line(t, s, "lea", 2, 61)
	_ = s.Issue(ctx, "lea", hash("lea0"), 61, t0, t0.Add(time.Hour))
	_ = s.Issue(ctx, "lea", hash("lea1"), 61, t0, t0.Add(time.Hour))
	if _, err := s.Vouch(ctx, hash("lea0"), 62, "", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Leave(ctx, "lea", 61, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Leave(ctx, "lea", 61, t0); err != nil {
		t.Fatalf("leaving twice: %v", err)
	}
	if _, err := s.Where(ctx, "lea", 61, t0); !errors.Is(err, ErrNotOnList) {
		t.Fatalf("still on it: %v", err)
	}
	p, _ := s.Where(ctx, "lea", 62, t0)
	if !Forgotten(p.Sponsor) || int64(p.Sponsor) != Pseudonym(s.Who(61)) {
		t.Fatalf("the invitee's sponsor is %d, want the giver's pseudonym", int64(p.Sponsor))
	}
	c, _ := (&Codes{s.pool}).Find(ctx, hash("lea1"))
	if c.Revoked.IsZero() || int64(c.Owner) != Pseudonym(s.Who(61)) {
		t.Fatalf("the unused invite: %+v", c)
	}
	v, err := s.Verify(ctx, "lea")
	if err != nil || v.Broken != 0 || len(v.Mismatches) != 0 {
		t.Fatalf("verify after a leave: %+v %v", v, err)
	}
	h, _ := s.History(ctx, "lea", 61)
	if len(h) != 6 || h[len(h)-1].Kind != "leave" || !h[0].ByYou {
		t.Fatalf("history %+v", h)
	}
}

func TestLedgerIsAppendOnlyAndTamperShows(t *testing.T) {
	s, pool := launches(t, "led", 910)
	ctx := context.Background()
	line(t, s, "led", 2, 71, 72)
	_ = s.Issue(ctx, "led", hash("led0"), 71, t0, t0.Add(time.Hour))
	v, err := s.Verify(ctx, "led")
	if err != nil || v.Entries != 5 || v.Broken != 0 || len(v.Mismatches) != 0 {
		t.Fatalf("a clean ledger: %+v %v", v, err)
	}
	if _, err := pool.Exec(ctx, "update ledger set ref = '9' where launch = 'led'"); err == nil {
		t.Fatal("the ledger took an update")
	}
	if _, err := pool.Exec(ctx, "delete from ledger where launch = 'led'"); err == nil {
		t.Fatal("the ledger took a delete")
	}
	// A grant made around the code shows as a mismatch.
	if _, err := pool.Exec(ctx, "update line set released = 9 where launch = 'led' and user_id = 72"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "insert into code (hash, guild_id, owner_id, made, expires, launch) values ($1, 910, 72, $2, $2, 'led')", hash("forged"), t0); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Verify(ctx, "led")
	if len(v.Mismatches) != 2 {
		t.Fatalf("mismatches %v, want the release and the invite", v.Mismatches)
	}
	// An entry changed behind the trigger breaks the chain from there.
	tamper := func(q string) {
		t.Helper()
		for _, step := range []string{"alter table ledger disable trigger ledger_append_only", q, "alter table ledger enable trigger ledger_append_only"} {
			if _, err := pool.Exec(ctx, step); err != nil {
				t.Fatal(step, err)
			}
		}
	}
	tamper("update ledger set ref = '9' where seq = (select seq from ledger where launch = 'led' and kind = 'release' order by seq limit 1)")
	v, _ = s.Verify(ctx, "led")
	if v.Broken == 0 {
		t.Fatal("an edited entry went unseen")
	}
}

func TestExpireDropsTheListAndKeepsTheTree(t *testing.T) {
	s, pool := launches(t, "exp", 911)
	ctx := context.Background()
	line(t, s, "exp", 2, 81)
	_ = s.Issue(ctx, "exp", hash("exp0"), 81, t0, t0.Add(time.Hour))
	_ = s.Issue(ctx, "exp", hash("exp1"), 81, t0, t0.Add(time.Hour))
	_, _ = s.Vouch(ctx, hash("exp0"), 82, "", t0)
	if _, err := (&Codes{s.pool}).Arrive(ctx, 911, 82, t0); err != nil {
		t.Fatal(err)
	}
	_ = s.SetStage(ctx, "exp", StageOpen, admin, t0)
	year := 365 * 24 * time.Hour
	if err := s.Expire(ctx, t0.Add(year/2), year); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.List(ctx, "exp"); len(rows) != 2 {
		t.Fatal("expired too soon")
	}
	if err := s.Expire(ctx, t0.Add(2*year), year); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.List(ctx, "exp"); len(rows) != 0 {
		t.Fatalf("the list stayed: %v", rows)
	}
	var ledger, kept int
	_ = pool.QueryRow(ctx, "select count(*) from ledger where launch = 'exp'").Scan(&ledger)
	_ = pool.QueryRow(ctx, "select count(*) from code where launch = 'exp'").Scan(&kept)
	if ledger != 0 || kept != 1 {
		t.Fatalf("ledger %d, codes %d: want the ledger gone and the one arrived by kept", ledger, kept)
	}
	if err := s.Expire(ctx, t0.Add(3*year), year); err != nil {
		t.Fatalf("expiring again: %v", err)
	}
}

func TestPseudonymIsNeverADiscordID(t *testing.T) {
	s := NewLaunches(nil, []byte("k"))
	for u := range snowflake.ID(100) {
		if !Forgotten(snowflake.ID(Pseudonym(s.Who(u)))) || Forgotten(u) {
			t.Fatalf("pseudonym of %d is not negative", u)
		}
	}
}

func TestSealIsKeptUntilTheyAreIn(t *testing.T) {
	s, pool := launches(t, "seal", 912)
	ctx := context.Background()
	line(t, s, "seal", 0, 91)
	if err := s.Seal(ctx, "seal", 91, []byte("sealed"), t0); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Where(ctx, "seal", 91, t0); !p.Sealed {
		t.Fatal("not sealed")
	}
	if err := s.Seal(ctx, "seal", 91, nil, t0); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Where(ctx, "seal", 91, t0); p.Sealed {
		t.Fatal("still sealed after never mind")
	}
	if err := s.Seal(ctx, "seal", 92, []byte("x"), t0); !errors.Is(err, ErrNotOnList) {
		t.Fatalf("sealing for someone not on the list: %v", err)
	}
	if _, err := pool.Exec(ctx, "insert into wave (launch, size, opened, why) values ('seal', 1, $1, 'test')", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "update line set wave = (select max(id) from wave where launch = 'seal'), admitted = $1 where launch = 'seal'", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(ctx, "seal", 91, []byte("late"), t0); !errors.Is(err, ErrNotOnList) {
		t.Fatalf("sealing after they're in: %v", err)
	}
	h, _ := s.History(ctx, "seal", 91)
	if len(h) != 3 || h[1].Kind != "seal" || h[2].Kind != "unseal" {
		t.Fatalf("history %+v", h)
	}
}

func TestForgetLeavesNothingThatNamesThem(t *testing.T) {
	s, pool := launches(t, "fgt", 915)
	ctx := context.Background()
	line(t, s, "fgt", 1, 121)
	_ = s.Issue(ctx, "fgt", hash("fgt0"), 121, t0, t0.Add(time.Hour))
	if _, err := s.Vouch(ctx, hash("fgt0"), 122, "", t0); err != nil {
		t.Fatal(err)
	}
	// A server link of theirs, used and unused, and joins either way.
	c := &Codes{pool}
	_ = c.Mint(ctx, hash("fgt1"), 915, 121, t0, t0.Add(time.Hour), -1)
	_ = c.Mint(ctx, hash("fgt2"), 915, 121, t0, t0.Add(time.Hour), -1)
	if _, err := c.Spend(ctx, hash("fgt1"), 123, t0); err != nil {
		t.Fatal(err)
	}
	for _, u := range []int64{121, 123} {
		if _, err := c.Arrive(ctx, 915, snowflakeOf(u), t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Forget(ctx, 121, t0); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"select count(*) from line where user_id = 121",
		"select count(*) from code where owner_id = 121 or spent_by = 121",
		"select count(*) from joined where user_id = 121 or sponsor = 121",
	} {
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d %v", q, n, err)
		}
	}
	tree, _ := c.Tree(ctx, 915)
	if len(tree) != 2 {
		t.Fatalf("tree %+v", tree)
	}
	for _, j := range tree {
		if j.User == 123 && (!Forgotten(j.Sponsor) || int64(j.Sponsor) != Pseudonym(s.Who(121))) {
			t.Fatalf("123's sponsor is %d", int64(j.Sponsor))
		}
	}
	if left, _ := c.Find(ctx, hash("fgt2")); left.Revoked.IsZero() {
		t.Fatal("their unused link wasn't taken back")
	}
	if p, _ := s.Where(ctx, "fgt", 122, t0); !Forgotten(p.Sponsor) {
		t.Fatal("the invitee's sponsor still names them")
	}
	if v, _ := s.Verify(ctx, "fgt"); v.Broken != 0 || len(v.Mismatches) != 0 {
		t.Fatalf("verify after forgetting: %+v", v)
	}
	if err := s.Forget(ctx, 121, t0); err != nil {
		t.Fatalf("forgetting twice: %v", err)
	}
}
