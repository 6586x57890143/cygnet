package ledger

import (
	"context"
	"slices"
	"strconv"
	"strings"
)

// Store is where a bird keeps its own entries: Postgres for most, bbolt for
// whichever bird has no Postgres. ledgertest.Run is the contract; every
// implementation passes it.
type Store interface {
	// Append writes e as the next record in its guild's chain, numbering it,
	// giving it an ID if it has none (its sequence number) and hashing it,
	// all as one step: two appends to one guild never share a Prev.
	Append(ctx context.Context, e Entry) (Record, error)
	// Query is the records matching f, newest first, at most f.Limit.
	Query(ctx context.Context, f Filter) ([]Record, error)
	// Range is guild's records after seq after, oldest first, at most n:
	// what Verify replays.
	Range(ctx context.Context, guild string, after int64, n int) ([]Record, error)
}

// Filter narrows a Query. It is also the body of "<bird>.log.query", so a
// fleet view asks every bird the same question.
type Filter struct {
	Guild string `json:"guild"`
	// Action is an exact action ("roles.jail") or a module ("roles.").
	Action string `json:"action,omitempty"`
	// Subject is a user, channel or role ID matched against the actor and
	// every subject: "everything about this member".
	Subject string `json:"subject,omitempty"`
	// Before is a cursor: only records with a lower sequence. Zero is the
	// newest.
	Before int64 `json:"before,omitempty"`
	Limit  int   `json:"limit,omitempty"`
}

// DefaultLimit is a Query's limit when Filter.Limit is unset or too high.
const DefaultLimit = 50

// Size is f's limit, bounded.
func (f Filter) Size() int {
	if f.Limit <= 0 || f.Limit > DefaultLimit {
		return DefaultLimit
	}
	return f.Limit
}

// Matches reports whether r answers f, so a store that filters in SQL can
// be held to the same answer as one that filters in Go.
func (f Filter) Matches(r Record) bool {
	e := r.Entry
	switch {
	case e.Guild != f.Guild:
		return false
	case f.Before > 0 && r.Seq >= f.Before:
		return false
	case f.Action != "" && e.Action != f.Action && (!strings.HasSuffix(f.Action, ".") || !strings.HasPrefix(e.Action, f.Action)):
		return false
	case f.Subject != "" && e.Actor.ID != f.Subject && !slices.ContainsFunc(e.Subjects, func(s Ref) bool {
		id, _, _ := strings.Cut(s.ID, "/")
		return s.ID == f.Subject || id == f.Subject
	}):
		return false
	}
	return true
}

// Number gives e its sequence number as its ID when it has none, for a
// Store's Append.
func Number(e Entry, seq int64) Entry {
	if e.ID == "" {
		e.ID = strconv.FormatInt(seq, 10)
	}
	return e
}
