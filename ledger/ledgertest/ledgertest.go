// Package ledgertest is the contract every ledger.Store keeps, and an
// in-memory store that keeps it, for a bird's own tests.
package ledgertest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/6586x57890143/cygnet/ledger"
)

// Mem is a ledger.Store in memory.
type Mem struct {
	mu   sync.Mutex
	rows map[string][]ledger.Record
}

// NewMem is an empty Mem.
func NewMem() *Mem { return &Mem{rows: map[string][]ledger.Record{}} }

func (m *Mem) Append(_ context.Context, e ledger.Entry) (ledger.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last ledger.Record
	if rows := m.rows[e.Guild]; len(rows) > 0 {
		last = rows[len(rows)-1]
	}
	r := ledger.Next(last, ledger.Number(e, last.Seq+1))
	m.rows[e.Guild] = append(m.rows[e.Guild], r)
	return r, nil
}

func (m *Mem) Query(_ context.Context, f ledger.Filter) ([]ledger.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ledger.Record
	rows := m.rows[f.Guild]
	for i := len(rows) - 1; i >= 0 && len(out) < f.Size(); i-- {
		if f.Matches(rows[i]) {
			out = append(out, rows[i])
		}
	}
	return out, nil
}

func (m *Mem) Range(_ context.Context, guild string, after int64, n int) ([]ledger.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ledger.Record
	for _, r := range m.rows[guild] {
		if r.Seq > after && len(out) < n {
			out = append(out, r)
		}
	}
	return out, nil
}

// Entry is a valid entry for guild, numbered n so tests can tell them apart.
func Entry(guild string, n int) ledger.Entry {
	return ledger.Entry{
		Bird: "tester", Guild: guild, At: time.Date(2026, 10, 17, 12, 0, n, 123456789, time.UTC),
		Actor: ledger.Member("175928847299117063"), Action: "roles.jail", Mood: ledger.MoodOK,
		Subjects: []ledger.Ref{ledger.User(fmt.Sprint(1000 + n))},
		Summary:  fmt.Sprintf("jailed for %dh", n+1),
	}
}

// Run holds a Store to the contract. newStore returns an empty store each
// call; guild names are the test's own, so a shared database is fine.
func Run(t *testing.T, newStore func(t *testing.T) ledger.Store) {
	ctx := context.Background()
	guild := func(t *testing.T) string { return t.Name() }

	t.Run("chain", func(t *testing.T) {
		s, g := newStore(t), guild(t)
		var prev ledger.Hash
		for i := range 3 {
			r, err := s.Append(ctx, Entry(g, i))
			if err != nil {
				t.Fatal(err)
			}
			if r.Seq != int64(i+1) || r.Prev != prev || r.Entry.ID == "" {
				t.Fatalf("append %d: seq %d, prev linked %v, id %q", i, r.Seq, r.Prev == prev, r.Entry.ID)
			}
			prev = r.Hash
		}
		rs, err := s.Range(ctx, g, 0, 10)
		if err != nil || len(rs) != 3 {
			t.Fatalf("range: %d records, %v", len(rs), err)
		}
		if err := ledger.Verify(ledger.Hash{}, rs); err != nil {
			t.Fatalf("a stored chain must verify as read back: %v", err)
		}
		if tail, _ := s.Range(ctx, g, 1, 1); len(tail) != 1 || tail[0].Seq != 2 {
			t.Fatalf("range after 1, size 1: %+v", tail)
		}
	})

	t.Run("guilds are separate chains", func(t *testing.T) {
		s, g := newStore(t), guild(t)
		a, _ := s.Append(ctx, Entry(g+"a", 0))
		b, _ := s.Append(ctx, Entry(g+"b", 0))
		if a.Seq != 1 || b.Seq != 1 || b.Prev != (ledger.Hash{}) {
			t.Fatalf("each guild starts its own chain: %d %d", a.Seq, b.Seq)
		}
	})

	t.Run("concurrent appends never fork", func(t *testing.T) {
		s, g := newStore(t), guild(t)
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Go(func() {
				if _, err := s.Append(ctx, Entry(g, i)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		rs, _ := s.Range(ctx, g, 0, 100)
		if len(rs) != 20 {
			t.Fatalf("%d records, want 20", len(rs))
		}
		if err := ledger.Verify(ledger.Hash{}, rs); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("query", func(t *testing.T) {
		s, g := newStore(t), guild(t)
		for i := range 5 {
			e := Entry(g, i)
			if i == 3 {
				e.Action = "rotation.archive"
			}
			if _, err := s.Append(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		check := func(name string, f ledger.Filter, want ...int64) {
			t.Helper()
			f.Guild = g
			rs, err := s.Query(ctx, f)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			var got []int64
			for _, r := range rs {
				got = append(got, r.Seq)
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("%s: seqs %v, want %v", name, got, want)
			}
		}
		check("newest first", ledger.Filter{}, 5, 4, 3, 2, 1)
		check("limit", ledger.Filter{Limit: 2}, 5, 4)
		check("cursor", ledger.Filter{Before: 4, Limit: 2}, 3, 2)
		check("exact action", ledger.Filter{Action: "rotation.archive"}, 4)
		check("module", ledger.Filter{Action: "roles."}, 5, 3, 2, 1)
		check("subject", ledger.Filter{Subject: "1002"}, 3)
		check("actor", ledger.Filter{Subject: "175928847299117063", Limit: 1}, 5)
	})
}
