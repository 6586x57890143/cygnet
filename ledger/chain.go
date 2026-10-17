package ledger

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// Hash is one link of a guild's chain.
type Hash [32]byte

// Record is an entry as stored: its place in the guild's chain and the hash
// that commits to it and everything before it.
type Record struct {
	Seq   int64 `json:"seq"`
	Entry Entry `json:"entry"`
	Prev  Hash  `json:"prev"`
	Hash  Hash  `json:"hash"`
}

// Chain is the hash of e following prev: sha256 of prev and e's canonical
// encoding. Every bird hashes the same way, so any bird's range can be
// checked by any other's code.
func Chain(prev Hash, e Entry) Hash {
	h := sha256.New()
	h.Write(prev[:])
	h.Write(canonical(e))
	return Hash(h.Sum(nil))
}

// canonical is e as JSON with its time in UTC to the microsecond, which is
// what Postgres keeps: hashing nanoseconds and a zone would make a row read
// back from the database fail its own hash.
func canonical(e Entry) []byte {
	e.At = e.At.UTC().Truncate(time.Microsecond)
	b, err := json.Marshal(e)
	if err != nil {
		// Entry is strings, slices and a time: Marshal cannot fail on it.
		panic(fmt.Sprintf("ledger: marshal entry: %v", err))
	}
	return b
}

// Next is the record e becomes after prev, the zero Record for a guild's
// first entry.
func Next(prev Record, e Entry) Record {
	r := Record{Seq: prev.Seq + 1, Entry: e, Prev: prev.Hash}
	r.Hash = Chain(r.Prev, e)
	return r
}

// Verify checks that records are one unbroken stretch of chain starting
// after prev: each one's Prev is the hash before it, and each Hash is what
// its entry hashes to. It names the first record that fails.
func Verify(prev Hash, records []Record) error {
	for _, r := range records {
		if r.Prev != prev {
			return fmt.Errorf("ledger: record %d does not follow the one before it", r.Seq)
		}
		if Chain(r.Prev, r.Entry) != r.Hash {
			return fmt.Errorf("ledger: record %d was changed after it was written", r.Seq)
		}
		prev = r.Hash
	}
	return nil
}
